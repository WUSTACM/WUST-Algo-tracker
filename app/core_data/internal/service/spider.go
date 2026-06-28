package service

import (
	"context"
	"cwxu-algo/api/core/v1/spider"
	"cwxu-algo/app/common/permission"
	"cwxu-algo/app/common/utils/auth"
	"cwxu-algo/app/core_data/internal/data"
	"cwxu-algo/app/core_data/internal/data/model"
	"cwxu-algo/app/core_data/task"
	stderrors "errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

var (
	SetForbidden             = errors.Forbidden("权限错误", "权限不允许，设置失败")
	InternalError            = errors.InternalServer("内部错误", "内部错误，操作失败")
	UpdateForbidden          = errors.Forbidden("权限错误", "权限不允许，不允许手动申请全量更新他人数据")
	RateLimitError           = errors.New(429, "TOO_MANY_REQUESTS", "请求过于频繁，请稍后再试")
	CodeforcesRateLimitError = errors.New(429, "TOO_MANY_REQUESTS", "Codeforces 官方 API 限流较严格，请不要频繁刷新，建议 30 分钟后再试")
)

const (
	staleAfter                    = 24 * time.Hour
	codeforcesPlatformName        = "CodeForces"
	codeforcesManualRefreshWindow = 30 * time.Minute
	defaultManualRefreshWindow    = 60 * time.Second
)

type SpiderService struct {
	spider.UnimplementedSpiderServer
	db         *gorm.DB
	rdb        *redis.Client
	spider     *task.SpiderTask
	limiterMap sync.Map // map[string]*rate.Limiter
}

type RetryJobReply struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
	JobId   int64  `json:"jobId"`
}

type RebuildAllReply struct {
	Code       int64   `json:"code"`
	Message    string  `json:"message"`
	TotalUsers int64   `json:"totalUsers"`
	QueuedJobs int64   `json:"queuedJobs"`
	Skipped    int64   `json:"skipped"`
	JobIds     []int64 `json:"jobIds"`
}

type HealthReply struct {
	Code        int64  `json:"code"`
	Message     string `json:"message"`
	Status      string `json:"status"`
	Queued      int64  `json:"queued"`
	Running     int64  `json:"running"`
	FailedToday int64  `json:"failedToday"`
	GeneratedAt int64  `json:"generatedAt"`
}

func (s *SpiderService) getLimiter(userId int64, interval time.Duration) *rate.Limiter {
	return s.getLimiterByKey(fmt.Sprintf("user:%d", userId), interval)
}

func (s *SpiderService) getLimiterByKey(key string, interval time.Duration) *rate.Limiter {
	if l, ok := s.limiterMap.Load(key); ok {
		return l.(*rate.Limiter)
	}
	// 1 request per interval, burst 1
	l := rate.NewLimiter(rate.Every(interval), 1)
	actual, _ := s.limiterMap.LoadOrStore(key, l)
	return actual.(*rate.Limiter)
}

func isCodeforcesPlatform(platform string) bool {
	return strings.EqualFold(strings.TrimSpace(platform), codeforcesPlatformName)
}

func samePlatformUsername(platform string, current string, next string) bool {
	current = strings.TrimSpace(current)
	next = strings.TrimSpace(next)
	if isCodeforcesPlatform(platform) {
		return strings.EqualFold(current, next)
	}
	return current == next
}

func manualRefreshWindow(platform string) time.Duration {
	if isCodeforcesPlatform(platform) {
		return codeforcesManualRefreshWindow
	}
	return defaultManualRefreshWindow
}

func (s *SpiderService) platformRefreshStartedWithin(userId int64, platform string, window time.Duration) bool {
	if window <= 0 {
		return false
	}
	var status model.SpiderSyncStatus
	if err := s.db.Where("user_id = ? AND platform = ?", userId, platform).First(&status).Error; err != nil {
		return false
	}
	if status.LastStartedAt == nil {
		return false
	}
	return time.Since(*status.LastStartedAt) < window
}

func (s *SpiderService) Update(ctx context.Context, req *spider.UpdateReq) (*spider.UpdateRes, error) {
	//if !auth.VerifyById(ctx, uint(req.UserId)) {
	//	return nil, UpdateForbidden
	//}
	platform := strings.TrimSpace(req.GetPlatform())
	if platform == "" {
		if header, ok := transport.FromServerContext(ctx); ok {
			platform = strings.TrimSpace(header.RequestHeader().Get("X-Spider-Platform"))
		}
	}
	limiterKey := fmt.Sprintf("update:%d:all", req.UserId)
	if platform != "" {
		limiterKey = fmt.Sprintf("update:%d:%s", req.UserId, platform)
	}
	if isCodeforcesPlatform(platform) && s.platformRefreshStartedWithin(req.UserId, platform, codeforcesManualRefreshWindow) {
		return &spider.UpdateRes{
			Code:    0,
			Message: "Codeforces 官方 API 限流较严格，最近已刷新过，请 30 分钟后再试",
			JobId:   0,
		}, nil
	}
	limiter := s.getLimiterByKey(limiterKey, manualRefreshWindow(platform))
	if !limiter.Allow() {
		if isCodeforcesPlatform(platform) {
			return nil, CodeforcesRateLimitError
		}
		return nil, RateLimitError
	}
	current := auth.GetCurrentUser(ctx)
	requesterId := int64(0)
	if current != nil {
		requesterId = int64(current.UserID)
	}
	jobId, err := s.spider.Do(req.UserId, true, "manual", requesterId, platform) // 全量或单平台更新
	if err != nil {
		if stderrors.Is(err, task.ErrActiveRefreshJob) {
			return &spider.UpdateRes{
				Code:    0,
				Message: "已有抓取任务正在进行，请稍等",
				JobId:   jobId,
			}, nil
		}
		return nil, InternalError
	}
	message := "更新成功，请稍等片刻，您的全量OJ数据正在更新"
	if platform != "" {
		message = fmt.Sprintf("更新成功，请稍等片刻，%s 数据正在更新", platform)
	}
	recordCoreOperation(ctx, s.db, "spider.update", "user", req.UserId, map[string]any{
		"platform": platform,
		"jobId":    jobId,
		"source":   "manual",
	})
	return &spider.UpdateRes{
		Code:    0,
		Message: message,
		JobId:   jobId,
	}, nil
}

func (s *SpiderService) GetSpider(ctx context.Context, req *spider.GetSpiderReq) (*spider.GetSpiderRep, error) {
	var plats []model.Platform
	err := s.db.Where("user_id = ?", req.UserId).Find(&plats).Error
	if err != nil {
		return nil, InternalError
	}
	res := make([]*spider.GetSpiderRep_Data, 0)
	for _, v := range plats {
		res = append(res, &spider.GetSpiderRep_Data{
			Platform: v.Platform,
			Username: v.Username,
		})
	}
	return &spider.GetSpiderRep{
		Data: res,
	}, nil
}

func toUnix(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.Unix()
}

func isCoachOrAdmin(ctx context.Context) bool {
	current := auth.GetCurrentUser(ctx)
	if current == nil {
		return false
	}
	return canManageCoreOps(current.RoleID)
}

func canViewUserDetail(ctx context.Context, userId int64) bool {
	current := auth.GetCurrentUser(ctx)
	if current == nil {
		return false
	}
	return canOperateUserDetail(int64(current.UserID), current.RoleID, userId)
}

func canManageCoreOps(roleID int) bool {
	return roleID == permission.RoleAdmin || roleID == permission.RoleCoach
}

func canOperateUserDetail(currentUserID int64, roleID int, targetUserID int64) bool {
	return currentUserID == targetUserID || canManageCoreOps(roleID)
}

func jobToPb(job model.SpiderRefreshJob, canViewError bool) *spider.JobInfo {
	errText := job.Error
	if !canViewError {
		errText = ""
	}
	return &spider.JobInfo{
		JobId:             int64(job.ID),
		UserId:            job.UserID,
		RequesterId:       job.RequesterID,
		Source:            job.Source,
		Status:            job.Status,
		NeedAll:           job.NeedAll,
		CurrentPlatform:   job.CurrentPlatform,
		TotalPlatforms:    job.TotalPlatforms,
		FinishedPlatforms: job.FinishedPlatforms,
		Error:             errText,
		CreatedAt:         job.CreatedAt.Unix(),
		StartedAt:         toUnix(job.StartedAt),
		FinishedAt:        toUnix(job.FinishedAt),
		UpdatedAt:         job.UpdatedAt.Unix(),
	}
}

func (s *SpiderService) Job(ctx context.Context, req *spider.JobReq) (*spider.JobRes, error) {
	if req.JobId <= 0 {
		return nil, errors.BadRequest("参数错误", "jobId不能为空")
	}
	var job model.SpiderRefreshJob
	if err := s.db.Where("id = ?", req.JobId).First(&job).Error; err != nil {
		return nil, errors.NotFound("任务不存在", "未找到抓取任务")
	}
	if !canViewUserDetail(ctx, job.UserID) {
		return nil, errors.Forbidden("权限错误", "无权查看该任务")
	}
	return &spider.JobRes{
		Code:    0,
		Message: "获取抓取任务成功",
		Data:    jobToPb(job, true),
	}, nil
}

func (s *SpiderService) Jobs(ctx context.Context, req *spider.JobsReq) (*spider.JobsRes, error) {
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	current := auth.GetCurrentUser(ctx)
	if current == nil {
		return nil, errors.Unauthorized("未登录", "请先登录")
	}

	query := s.db.Model(&model.SpiderRefreshJob{})
	scope := req.Scope
	if scope == "" {
		scope = "mine"
	}
	if scope == "all" {
		if !isCoachOrAdmin(ctx) {
			return nil, errors.Forbidden("权限错误", "无权查看全站抓取任务")
		}
		if req.UserId > 0 {
			query = query.Where("user_id = ?", req.UserId)
		}
	} else {
		query = query.Where("user_id = ? OR requester_id = ?", current.UserID, current.UserID)
	}
	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, InternalError
	}
	var jobs []model.SpiderRefreshJob
	if err := query.Order("updated_at DESC").Offset(int((page - 1) * pageSize)).Limit(int(pageSize)).Find(&jobs).Error; err != nil {
		return nil, InternalError
	}
	list := make([]*spider.JobInfo, 0, len(jobs))
	for _, job := range jobs {
		list = append(list, jobToPb(job, canViewUserDetail(ctx, job.UserID)))
	}
	return &spider.JobsRes{
		Code:    0,
		Message: "获取抓取任务列表成功",
		Data:    list,
		Total:   total,
	}, nil
}

func (s *SpiderService) Health(ctx context.Context) (*HealthReply, error) {
	if auth.GetCurrentUser(ctx) == nil {
		return nil, errors.Unauthorized("未登录", "请先登录")
	}

	countByStatus := func(status string) (int64, error) {
		var count int64
		err := s.db.Model(&model.SpiderRefreshJob{}).Where("status = ?", status).Count(&count).Error
		return count, err
	}

	queued, err := countByStatus("queued")
	if err != nil {
		return nil, InternalError
	}
	running, err := countByStatus("running")
	if err != nil {
		return nil, InternalError
	}

	var failedToday int64
	since := time.Now().Add(-24 * time.Hour)
	if err := s.db.Model(&model.SpiderRefreshJob{}).
		Where("status = ? AND updated_at >= ?", "failed", since).
		Count(&failedToday).Error; err != nil {
		return nil, InternalError
	}

	return &HealthReply{
		Code:        0,
		Message:     "Spider 队列可查询",
		Status:      "ok",
		Queued:      queued,
		Running:     running,
		FailedToday: failedToday,
		GeneratedAt: time.Now().Unix(),
	}, nil
}

func (s *SpiderService) Retry(ctx context.Context, jobId int64) (*RetryJobReply, error) {
	if jobId <= 0 {
		return nil, errors.BadRequest("参数错误", "jobId不能为空")
	}
	current := auth.GetCurrentUser(ctx)
	if current == nil {
		return nil, errors.Unauthorized("未登录", "请先登录")
	}
	var job model.SpiderRefreshJob
	if err := s.db.Where("id = ?", jobId).First(&job).Error; err != nil {
		return nil, errors.NotFound("任务不存在", "未找到抓取任务")
	}
	if job.Status != "failed" {
		return nil, errors.BadRequest("状态错误", "只能重试失败任务")
	}
	if !canViewUserDetail(ctx, job.UserID) {
		return nil, errors.Forbidden("权限错误", "无权重试该任务")
	}
	platform := job.CurrentPlatform
	if job.TotalPlatforms != 1 {
		platform = ""
	}
	newJobId, err := s.spider.Do(job.UserID, job.NeedAll, "retry", int64(current.UserID), platform)
	if err != nil {
		if stderrors.Is(err, task.ErrActiveRefreshJob) {
			return &RetryJobReply{
				Code:    0,
				Message: "已有抓取任务正在进行，请稍等",
				JobId:   newJobId,
			}, nil
		}
		return nil, InternalError
	}
	recordCoreOperation(ctx, s.db, "spider.retry", "spider_job", jobId, map[string]any{
		"userId":   job.UserID,
		"platform": platform,
		"newJobId": newJobId,
	})
	return &RetryJobReply{
		Code:    0,
		Message: "重试任务已加入队列",
		JobId:   newJobId,
	}, nil
}

func (s *SpiderService) RebuildAll(ctx context.Context) (*RebuildAllReply, error) {
	current := auth.GetCurrentUser(ctx)
	if current == nil {
		return nil, errors.Unauthorized("未登录", "请先登录")
	}
	if !canManageCoreOps(current.RoleID) {
		return nil, errors.Forbidden("权限错误", "只有管理员和教练可以触发全站重爬")
	}

	var userIds []int64
	if err := s.db.Model(&model.Platform{}).
		Distinct("user_id").
		Order("user_id ASC").
		Pluck("user_id", &userIds).Error; err != nil {
		return nil, InternalError
	}

	reply := &RebuildAllReply{
		Code:       0,
		Message:    "全站全量重爬任务已提交",
		TotalUsers: int64(len(userIds)),
		JobIds:     make([]int64, 0, len(userIds)),
	}
	for _, userId := range userIds {
		jobId, err := s.spider.Do(userId, true, "admin_rebuild", int64(current.UserID), "")
		if err != nil {
			if stderrors.Is(err, task.ErrActiveRefreshJob) {
				reply.Skipped++
				if jobId > 0 {
					reply.JobIds = append(reply.JobIds, jobId)
				}
				continue
			}
			return nil, InternalError
		}
		reply.QueuedJobs++
		reply.JobIds = append(reply.JobIds, jobId)
	}
	recordCoreOperation(ctx, s.db, "spider.rebuild_all", "spider_job", 0, map[string]any{
		"totalUsers": reply.TotalUsers,
		"queuedJobs": reply.QueuedJobs,
		"skipped":    reply.Skipped,
	})
	return reply, nil
}

func (s *SpiderService) Status(ctx context.Context, req *spider.StatusReq) (*spider.StatusRes, error) {
	if req.UserId <= 0 {
		return nil, errors.BadRequest("参数错误", "userId不能为空")
	}
	if !auth.VerifySelfOrAbove(ctx, uint(req.UserId)) && !isCoachOrAdmin(ctx) {
		return nil, errors.Forbidden("权限错误", "无权查看该用户抓取状态")
	}

	var platforms []model.Platform
	if err := s.db.Where("user_id = ?", req.UserId).Find(&platforms).Error; err != nil {
		return nil, InternalError
	}
	var statuses []model.SpiderSyncStatus
	if err := s.db.Where("user_id = ?", req.UserId).Find(&statuses).Error; err != nil {
		return nil, InternalError
	}
	statusByPlatform := make(map[string]model.SpiderSyncStatus, len(statuses))
	for _, item := range statuses {
		statusByPlatform[item.Platform] = item
	}

	now := time.Now()
	canViewError := canViewUserDetail(ctx, req.UserId)
	result := make([]*spider.SyncStatusInfo, 0, len(platforms))
	for _, plat := range platforms {
		item, ok := statusByPlatform[plat.Platform]
		statusText := "never"
		lastStartedAt := int64(0)
		lastFinishedAt := int64(0)
		lastSuccessAt := int64(0)
		lastFetchedCount := int64(0)
		updatedAt := int64(0)
		lastError := ""
		isStale := true
		if ok {
			statusText = item.Status
			lastStartedAt = toUnix(item.LastStartedAt)
			lastFinishedAt = toUnix(item.LastFinishedAt)
			lastSuccessAt = toUnix(item.LastSuccessAt)
			lastFetchedCount = item.LastFetchedCount
			updatedAt = item.UpdatedAt.Unix()
			if canViewError {
				lastError = item.LastError
			}
			isStale = item.LastSuccessAt == nil || now.Sub(*item.LastSuccessAt) > staleAfter
		}
		result = append(result, &spider.SyncStatusInfo{
			Platform:         plat.Platform,
			Username:         plat.Username,
			Status:           statusText,
			LastStartedAt:    lastStartedAt,
			LastFinishedAt:   lastFinishedAt,
			LastSuccessAt:    lastSuccessAt,
			LastError:        lastError,
			LastFetchedCount: lastFetchedCount,
			IsStale:          isStale,
			CanViewError:     canViewError,
			UpdatedAt:        updatedAt,
		})
	}

	return &spider.StatusRes{
		Code:              0,
		Message:           "获取抓取状态成功",
		Data:              result,
		StaleAfterSeconds: int64(staleAfter.Seconds()),
		GeneratedAt:       now.Unix(),
	}, nil
}

func (s *SpiderService) SetSpider(ctx context.Context, req *spider.SetSpiderReq) (*spider.SetSpiderRep, error) {
	// 校验JWT：只能设置自己的 spider，或者管理员可以设置任何人
	if !auth.VerifySelfOrAbove(ctx, uint(req.UserId)) {
		return nil, SetForbidden
	}
	req.Platform = strings.TrimSpace(req.Platform)
	req.Username = strings.TrimSpace(req.Username)
	// Rate limit
	limiter := s.getLimiter(req.UserId, 30*time.Second)
	if !limiter.Allow() {
		return nil, RateLimitError
	}
	var existing model.Platform
	hasExistingBinding := s.db.Where("user_id = ? AND platform = ?", req.UserId, req.Platform).First(&existing).Error == nil
	isSameBinding := hasExistingBinding && samePlatformUsername(req.Platform, existing.Username, req.Username)
	if isCodeforcesPlatform(req.Platform) && isSameBinding && s.platformRefreshStartedWithin(req.UserId, req.Platform, codeforcesManualRefreshWindow) {
		return &spider.SetSpiderRep{
			Code:    0,
			Message: "Codeforces 官方 API 限流较严格，该账号最近已刷新过，请 30 分钟后再试",
		}, nil
	}
	// 直接设置进去 构建Platform
	platform := model.Platform{
		UserID:   req.UserId,
		Platform: req.Platform,
		Username: req.Username,
	}
	if isCodeforcesPlatform(req.Platform) && hasExistingBinding && !isSameBinding {
		if err := s.db.Where("user_id = ? AND platform = ?", req.UserId, req.Platform).Delete(&model.SpiderSyncStatus{}).Error; err != nil {
			log.Errorf("SetSpider: delete Codeforces sync status failed: %v", err)
		}
	}
	if err := s.db.Where("user_id = ? AND platform = ?", req.UserId, req.Platform).Delete(&model.Platform{}).Error; err != nil {
		log.Errorf("SetSpider: delete platform failed: %v", err)
	}
	if err := s.db.Where("user_id = ? AND platform = ?", req.UserId, req.Platform).Delete(&model.SubmitLog{}).Error; err != nil {
		log.Errorf("SetSpider: delete submit_log failed: %v", err)
	}
	s.invalidateUserStatisticCache(ctx, req.UserId)
	err := s.db.Save(&platform).Error
	if err != nil {
		log.Errorf("SetSpider: save platform failed: %v", err)
		return nil, InternalError
	}
	if _, err := s.spider.Do(req.UserId, true, "bind", int64(auth.GetCurrentUserId(ctx)), req.Platform); err != nil {
		log.Errorf("SetSpider: enqueue spider task failed: %v", err)
	}
	recordCoreOperation(ctx, s.db, "spider.set_binding", "user", req.UserId, map[string]any{
		"platform": req.Platform,
	})
	return &spider.SetSpiderRep{
		Code:    0,
		Message: "设置成功，请稍等片刻，您的全量OJ数据正在更新",
	}, nil
}

func (s *SpiderService) invalidateUserStatisticCache(ctx context.Context, userId int64) {
	if err := s.rdb.Del(
		ctx,
		fmt.Sprintf("core:submit_log:user:%d", userId),
		"core:submit_log:user:-1",
		fmt.Sprintf("user:%d:lastSubmitTime", userId),
		fmt.Sprintf("statistic:period:%d", userId),
		"statistic:period:-1",
		fmt.Sprintf("statistic:platform-period:%d", userId),
		"statistic:platform-period:-1",
	).Err(); err != nil {
		log.Errorf("SetSpider: redis del failed: %v", err)
	}

	for _, pattern := range []string{
		fmt.Sprintf("statistic:heatmap:%d:*:*:*", userId),
		"statistic:heatmap:0:*:*:*",
	} {
		iter := s.rdb.Scan(ctx, 0, pattern, 200).Iterator()
		for iter.Next(ctx) {
			if err := s.rdb.Del(ctx, iter.Val()).Err(); err != nil {
				log.Errorf("SetSpider: redis del pattern=%s key=%s failed: %v", pattern, iter.Val(), err)
			}
		}
		if err := iter.Err(); err != nil {
			log.Errorf("SetSpider: redis scan pattern=%s failed: %v", pattern, err)
		}
	}
}

func NewSpiderService(data *data.Data, spider *task.SpiderTask) *SpiderService {
	return &SpiderService{
		db:     data.DB,
		rdb:    data.RDB,
		spider: spider,
	}
}

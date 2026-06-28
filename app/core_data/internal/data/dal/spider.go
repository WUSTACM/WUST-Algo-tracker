package dal

import (
	"context"
	"cwxu-algo/app/common/utils"
	"cwxu-algo/app/core_data/internal/data"
	"cwxu-algo/app/core_data/internal/data/model"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// SpiderDal 爬虫数据操作模块
type SpiderDal struct {
	db  *gorm.DB
	rdb *redis.Client
}

func NewSpiderDal(data *data.Data) *SpiderDal {
	return &SpiderDal{
		db:  data.DB,
		rdb: data.RDB,
	}
}

// GetByUserId 根据userId获取提交记录
// 设计思路: Redis 查 ID -> Redis 根据ID 查数据 -> 回源DB -> 降级
//
// 参数:
//   - userId 用户ID
//   - lastTimeUnix 上次获取的时间戳
//   - limit 获取数量
func (s *SpiderDal) GetByUserId(ctx context.Context, userId int64, lastTimeUnix int64, limit int64) ([]model.SubmitLog, error) {
	if lastTimeUnix == -1 {
		lastTimeUnix = 33325619029
	}

	cacheKey := fmt.Sprintf("core:submit_log:user:%d", userId)
	res := s.rdb.ZRevRangeByScore(ctx, cacheKey, &redis.ZRangeBy{
		Max:   fmt.Sprintf("(%d", lastTimeUnix),
		Min:   "-inf",
		Count: limit,
	})
	var sbLog []model.SubmitLog
	ids, err := res.Result()
	t := time.Unix(lastTimeUnix, 0)
	q := s.db.Order("time DESC")
	if userId != -1 {
		q = q.Where("user_id = ? AND time < ?", userId, t)
	} else {
		q = q.Where("time < ?", t)
	}
	dbFunc := func() ([]model.SubmitLog, error) {
		// 降级到纯db
		err := q.Limit(int(limit)).Find(&sbLog).Error
		go func() {
			ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			s.SetCache(ctx2, sbLog, userId)
		}()
		return sbLog, err
	}
	if err != nil {
		return dbFunc()
	}
	// 防止缓存忽悠人
	err = q.Limit(1).Find(&sbLog).Error
	if err != nil || len(ids) < int(limit) || len(sbLog) == 0 || strconv.Itoa(int(sbLog[0].ID)) != ids[0] {
		return dbFunc()
	}
	// 到 Redis 的 Global 查这些ID
	// 构建缓存key
	cacheKeys := make([]string, len(ids))
	for i, id := range ids {
		cacheKeys[i] = fmt.Sprintf("core:submit_log:detail:%s", id)
	}
	r := s.rdb.MGet(ctx, cacheKeys...)
	rVal, err := r.Result()

	// 由于缓存列不存在导致回源
	if err != nil || slices.Contains(rVal, nil) {
		return dbFunc()
	}
	// 命中，解析缓存
	sbLog = make([]model.SubmitLog, 0)
	for _, v := range rVal {
		var l model.SubmitLog
		s, ok := v.(string)
		if !ok {
			return dbFunc()
		}
		_ = utils.GobDecoder([]byte(s), &l)

		sbLog = append(sbLog, l)
	}
	log.Info(sbLog)
	return sbLog, nil
}

// SetCache 缓存提交记录
func (s *SpiderDal) SetCache(ctx context.Context, log []model.SubmitLog, userId int64) {
	pipe := s.rdb.Pipeline()
	// 根据 userId 构建 Zset
	for _, v := range log {
		cacheKey := fmt.Sprintf("core:submit_log:user:%d", userId)
		_ = pipe.ZAdd(ctx, cacheKey, redis.Z{
			Score:  float64(v.Time.Unix()),
			Member: v.ID,
		})
		// 构建缓存key
		cacheKey = fmt.Sprintf("core:submit_log:detail:%d", v.ID)
		_ = pipe.Expire(ctx, cacheKey, 24*time.Hour)
		// 缓存提交记录
		vByte, _ := utils.GobEncoder(v)
		_ = pipe.Set(ctx, cacheKey, vByte, 12*time.Hour)
	}
	_, _ = pipe.Exec(ctx)
}

// GetContestByUserId 获取用户比赛历史
func (s *SpiderDal) GetContestByUserId(ctx context.Context, userId int64, cursor int64, limit int64, platform string) ([]model.ContestLog, error) {
	if cursor == 0 {
		cursor = 33325619029
	}

	cacheKey := fmt.Sprintf("core:contest_log:user:%d", userId)
	if platform != "" {
		cacheKey = fmt.Sprintf("core:contest_log:user:%d:%s", userId, platform)
	}

	res := s.rdb.ZRevRangeByScore(ctx, cacheKey, &redis.ZRangeBy{
		Max:   fmt.Sprintf("(%d", cursor),
		Min:   "-inf",
		Count: limit,
	})
	var contestLogs []model.ContestLog
	ids, err := res.Result()
	t := time.Unix(cursor, 0)

	q := s.db.Order("time DESC")
	if userId != -1 {
		q = q.Where("user_id = ? AND time < ?", userId, t)
	} else {
		q = q.Where("time < ?", t)
	}
	if platform != "" {
		q = q.Where("platform = ?", platform)
	}

	dbFunc := func() ([]model.ContestLog, error) {
		err := q.Limit(int(limit)).Find(&contestLogs).Error
		go func() {
			ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			s.SetContestCache(ctx2, contestLogs, userId, platform)
		}()
		return contestLogs, err
	}

	if err != nil {
		return dbFunc()
	}

	err = q.Limit(1).Find(&contestLogs).Error
	if err != nil || len(ids) < int(limit) || (len(contestLogs) > 0 && strconv.Itoa(int(contestLogs[0].ID)) != ids[0]) {
		return dbFunc()
	}

	cacheKeys := make([]string, len(ids))
	for i, id := range ids {
		cacheKeys[i] = fmt.Sprintf("core:contest_log:detail:%s", id)
	}
	r := s.rdb.MGet(ctx, cacheKeys...)
	rVal, err := r.Result()

	if err != nil || slices.Contains(rVal, nil) {
		return dbFunc()
	}

	contestLogs = make([]model.ContestLog, 0)
	for _, v := range rVal {
		var l model.ContestLog
		s, ok := v.(string)
		if !ok {
			return dbFunc()
		}
		_ = utils.GobDecoder([]byte(s), &l)
		contestLogs = append(contestLogs, l)
	}
	return contestLogs, nil
}

// GetContestList 获取比赛列表（按 platform + contest_id 去重）
func (s *SpiderDal) GetContestList(_ context.Context, userId int64, offset int64, limit int64, platform string) ([]model.ContestLog, int64, error) {
	// 先构建基础条件
	baseQuery := s.db.Model(&model.ContestLog{})
	if userId != -1 {
		baseQuery = baseQuery.Where("user_id = ?", userId)
	}
	if platform != "" {
		baseQuery = baseQuery.Where("platform = ?", platform)
	}

	// 1. 计算去重后的总数
	var total int64
	countQuery := baseQuery.Select("COUNT(DISTINCT platform || E'\\x1f' || contest_id)")
	if err := countQuery.Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	// 2. 使用窗口函数获取每个 contest_id 最新的记录
	// 先获取去重且分页后的 platform + contest_id 列表
	type ContestKeyWithTime struct {
		Platform  string
		ContestId string
		MaxTime   time.Time
	}
	var contestKeyItems []ContestKeyWithTime

	// 构建子查询：按 platform + contest_id 分组，取最新的 time，然后分页
	paginateQuery := baseQuery.
		Select("platform, contest_id, MAX(time) as max_time").
		Group("platform, contest_id").
		Order("max_time DESC, platform ASC, contest_id DESC").
		Offset(int(offset)).
		Limit(int(limit))

	if err := paginateQuery.Scan(&contestKeyItems).Error; err != nil {
		return nil, 0, err
	}

	if len(contestKeyItems) == 0 {
		return []model.ContestLog{}, total, nil
	}

	// 3. 根据 contest_id 列表获取完整记录
	var contestLogs []model.ContestLog
	finalQuery := s.db.Model(&model.ContestLog{})
	keyConditions := make([]string, 0, len(contestKeyItems))
	keyArgs := make([]interface{}, 0, len(contestKeyItems)*2)
	for _, item := range contestKeyItems {
		keyConditions = append(keyConditions, "(platform = ? AND contest_id = ?)")
		keyArgs = append(keyArgs, item.Platform, item.ContestId)
	}
	finalQuery = finalQuery.Where("("+strings.Join(keyConditions, " OR ")+")", keyArgs...)
	if userId != -1 {
		finalQuery = finalQuery.Where("user_id = ?", userId)
	}
	if platform != "" {
		finalQuery = finalQuery.Where("platform = ?", platform)
	}

	if err := finalQuery.Find(&contestLogs).Error; err != nil {
		return nil, 0, err
	}

	// 4. 按照分页查询的顺序重新排列结果
	logMap := make(map[string]model.ContestLog)
	for _, item := range contestLogs {
		logMap[item.Platform+"\x1f"+item.ContestId] = item
	}

	result := make([]model.ContestLog, 0, len(contestKeyItems))
	for _, item := range contestKeyItems {
		if contestLog, ok := logMap[item.Platform+"\x1f"+item.ContestId]; ok {
			result = append(result, contestLog)
		}
	}

	return result, total, nil
}

// GetContestRanking 获取比赛排行榜
func (s *SpiderDal) GetContestRanking(_ context.Context, contestId string, platform string, offset int64, limit int64, userIds []int64) ([]model.ContestLog, int64, error) {
	var contestLogs []model.ContestLog
	var total int64

	q := s.db.Model(&model.ContestLog{}).Where("contest_id = ? and platform = ?", contestId, platform)

	if len(userIds) > 0 {
		q = q.Where("user_id IN ?", userIds)
	}

	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	if err := q.
		Order("CASE WHEN rank > 0 THEN 0 ELSE 1 END ASC").
		Order("rank ASC").
		Order("ac_count DESC").
		Order("total_count DESC").
		Order("user_id ASC").
		Offset(int(offset)).
		Limit(int(limit)).
		Find(&contestLogs).Error; err != nil {
		return nil, 0, err
	}

	return contestLogs, total, nil
}

// GetContestParticipants 获取比赛全部参赛记录，用于构建逐题矩阵和表头统计。
func (s *SpiderDal) GetContestParticipants(_ context.Context, contestId string, platform string, userIds []int64) ([]model.ContestLog, error) {
	var contestLogs []model.ContestLog
	q := s.db.Model(&model.ContestLog{}).Where("contest_id = ? and platform = ?", contestId, platform)
	if len(userIds) > 0 {
		q = q.Where("user_id IN ?", userIds)
	}
	if err := q.
		Order("CASE WHEN rank > 0 THEN 0 ELSE 1 END ASC").
		Order("rank ASC").
		Order("ac_count DESC").
		Order("total_count DESC").
		Order("user_id ASC").
		Find(&contestLogs).Error; err != nil {
		return nil, err
	}
	return contestLogs, nil
}

// SetContestCache 缓存比赛记录
func (s *SpiderDal) SetContestCache(ctx context.Context, logs []model.ContestLog, userId int64, platform string) {
	pipe := s.rdb.Pipeline()

	cacheKey := fmt.Sprintf("core:contest_log:user:%d", userId)
	if platform != "" {
		cacheKey = fmt.Sprintf("core:contest_log:user:%d:%s", userId, platform)
	}

	for _, v := range logs {
		_ = pipe.ZAdd(ctx, cacheKey, redis.Z{
			Score:  float64(v.Time.Unix()),
			Member: v.ID,
		})
		detailKey := fmt.Sprintf("core:contest_log:detail:%d", v.ID)
		_ = pipe.Expire(ctx, detailKey, 24*time.Hour)
		vByte, _ := utils.GobEncoder(v)
		_ = pipe.Set(ctx, detailKey, vByte, 12*time.Hour)
	}
	_, _ = pipe.Exec(ctx)
}

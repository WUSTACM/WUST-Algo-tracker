package service

import (
	"context"
	"cwxu-algo/api/core/v1/contest_log"
	"cwxu-algo/api/user/v1/profile"
	"cwxu-algo/app/common/discovery"
	"cwxu-algo/app/core_data/internal/data"
	"cwxu-algo/app/core_data/internal/data/dal"
	"cwxu-algo/app/core_data/internal/data/model"
	"cwxu-algo/app/core_data/internal/spider"
	"sort"
	"time"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/registry"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/redis/go-redis/v9"
	grpc2 "google.golang.org/grpc"
	"gorm.io/gorm"
)

type ContestLogService struct {
	contest_log.UnimplementedContestServer
	sbDal *dal.SpiderDal
	db    *gorm.DB
	rdb   *redis.Client
	reg   *registry.Registrar
}

func (c ContestLogService) userRPC() (*grpc2.ClientConn, error) {
	return grpc.DialInsecure(
		context.Background(),
		grpc.WithEndpoint("discovery:///user"),
		grpc.WithDiscovery((*c.reg).(registry.Discovery)),
		grpc.WithTimeout(20*time.Second),
	)
}

func (c ContestLogService) GetContestList(ctx context.Context, req *contest_log.GetContestListReq) (*contest_log.GetContestListRes, error) {
	logs, total, err := c.sbDal.GetContestList(ctx, req.UserId, req.Offset, req.Limit, req.Platform)
	if err != nil {
		return nil, errors.InternalServer("内部服务器错误", err.Error())
	}

	items := make([]*contest_log.ContestLog, 0, len(logs))
	for _, v := range logs {
		items = append(items, &contest_log.ContestLog{
			Id:          uint32(v.ID),
			Platform:    v.Platform,
			ContestId:   v.ContestId,
			ContestName: v.ContestName,
			ContestUrl:  v.ContestUrl,
			TotalCount:  int32(v.TotalCount),
			Time:        unixOrZero(v.Time),
			EndTime:     unixOrZero(v.EndTime),
		})
	}

	return &contest_log.GetContestListRes{
		Code:    0,
		Message: "OK",
		Data:    items,
		Total:   total,
	}, nil
}

func (c ContestLogService) GetContestRanking(ctx context.Context, req *contest_log.GetContestRankingReq) (*contest_log.GetContestRankingRes, error) {
	contest := model.ContestLog{}
	_ = c.db.Where("id = ?", req.ContestId).First(&contest)

	contestProto := &contest_log.ContestLog{
		Id:          uint32(contest.ID),
		Platform:    contest.Platform,
		ContestId:   contest.ContestId,
		ContestName: contest.ContestName,
		ContestUrl:  contest.ContestUrl,
		TotalCount:  int32(contest.TotalCount),
		Time:        unixOrZero(contest.Time),
		EndTime:     unixOrZero(contest.EndTime),
	}

	// 建立到 user 服务的共享连接，避免 N+1 连接开销
	conn, err := c.userRPC()
	if err != nil {
		log.Errorf("userRPC failed: %v", err)
		// 连接失败降级：仍然返回排名数据，只是没有用户信息
		conn = nil
	} else {
		defer conn.Close()
	}

	var userClient profile.ProfileClient
	if conn != nil {
		userClient = profile.NewProfileClient(conn)
	}

	var userIds []int64
	if req.GroupId != nil && userClient != nil {
		res, err := userClient.GetUserIdsByGroup(ctx, &profile.GetUserIdsByGroupReq{GroupId: *req.GroupId})
		if err != nil {
			log.Errorf("GetUserIdsByGroup failed: %v", err)
			return nil, errors.InternalServer("内部服务器错误", "获取用户组信息失败")
		}
		userIds = res.UserIds
		if len(userIds) == 0 {
			return &contest_log.GetContestRankingRes{
				Code:     0,
				Message:  "OK",
				Contest:  contestProto,
				Data:     make([]*contest_log.RankingItem, 0),
				Problems: make([]*contest_log.ProblemColumn, 0),
				Total:    0,
			}, nil
		}
	}

	allLogs, err := c.sbDal.GetContestParticipants(ctx, contest.ContestId, contest.Platform, userIds)
	if err != nil {
		return nil, errors.InternalServer("内部服务器错误", err.Error())
	}
	var logs []model.ContestLog
	total := int64(len(allLogs))
	if isContestPlatform(contest.Platform, spider.AtCoder) {
		c.applyAtCoderOfficialRanks(ctx, contest.ContestId, allLogs)
		sortContestLogs(allLogs)
		logs = paginateContestLogs(allLogs, req.Offset, req.Limit)
	} else if isContestPlatform(contest.Platform, spider.CodeForces) {
		c.applyCodeforcesOfficialRanks(ctx, contest.ContestId, allLogs)
		sortContestLogs(allLogs)
		logs = paginateContestLogs(allLogs, req.Offset, req.Limit)
	} else {
		logs, total, err = c.sbDal.GetContestRanking(ctx, contest.ContestId, contest.Platform, req.Offset, req.Limit, userIds)
		if err != nil {
			return nil, errors.InternalServer("内部服务器错误", err.Error())
		}
	}

	// 批量获取用户信息，一次 RPC 替代原来的 N 次 GetById
	nameMap := c.fetchUserNames(ctx, userClient, logs)
	matrix := c.buildContestMatrix(ctx, contest, logs, allLogs)
	if len(matrix.Problems) > 0 {
		contestProto.TotalCount = int32(len(matrix.Problems))
	}

	items := make([]*contest_log.RankingItem, 0, len(logs))
	for _, v := range logs {
		u := nameMap[v.UserID]
		items = append(items, &contest_log.RankingItem{
			Rank:           int64(v.Rank),
			UserId:         v.UserID,
			Username:       u.Username,
			Name:           u.Name,
			Avatar:         u.Avatar,
			AcCount:        int32(v.AcCount),
			TotalCount:     int32(v.TotalCount),
			Penalty:        matrix.PenaltyByUser[v.UserID],
			ProblemResults: matrix.ResultsByUser[v.UserID],
			GroupId:        u.GroupID,
		})
	}

	return &contest_log.GetContestRankingRes{
		Code:           0,
		Message:        "OK",
		Contest:        contestProto,
		Data:           items,
		Total:          total,
		Problems:       matrix.Problems,
		DegradedReason: matrix.DegradedReason,
	}, nil
}

func sortContestLogs(logs []model.ContestLog) {
	sort.SliceStable(logs, func(i, j int) bool {
		leftRanked := logs[i].Rank > 0
		rightRanked := logs[j].Rank > 0
		if leftRanked != rightRanked {
			return leftRanked
		}
		if logs[i].Rank != logs[j].Rank {
			if logs[i].Rank <= 0 {
				return false
			}
			if logs[j].Rank <= 0 {
				return true
			}
			return logs[i].Rank < logs[j].Rank
		}
		if logs[i].AcCount != logs[j].AcCount {
			return logs[i].AcCount > logs[j].AcCount
		}
		if logs[i].TotalCount != logs[j].TotalCount {
			return logs[i].TotalCount > logs[j].TotalCount
		}
		return logs[i].UserID < logs[j].UserID
	})
}

func paginateContestLogs(logs []model.ContestLog, offset int64, limit int64) []model.ContestLog {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = int64(len(logs))
	}
	start := int(offset)
	if start >= len(logs) {
		return []model.ContestLog{}
	}
	end := start + int(limit)
	if end > len(logs) {
		end = len(logs)
	}
	return logs[start:end]
}

type userInfo struct {
	Avatar   string
	Name     string
	Username string
	GroupID  int64
}

// fetchUserNames 批量获取用户姓名和头像，一次 RPC 调用
func (c ContestLogService) fetchUserNames(ctx context.Context, client profile.ProfileClient, logs []model.ContestLog) map[int64]userInfo {
	result := map[int64]userInfo{}
	if client == nil || len(logs) == 0 {
		return result
	}

	// 去重收集 userId
	idSet := map[int64]struct{}{}
	for _, v := range logs {
		if v.UserID != 0 {
			idSet[v.UserID] = struct{}{}
		}
	}
	userIds := make([]int64, 0, len(idSet))
	for id := range idSet {
		userIds = append(userIds, id)
	}

	res, err := client.GetByIds(ctx, &profile.GetByIdsReq{UserIds: userIds})
	if err != nil {
		log.Errorf("GetByIds batch failed: %v", err)
		return result
	}
	for _, p := range res.Profiles {
		result[p.UserId] = userInfo{Name: p.Name, Username: p.Username, Avatar: p.Avatar, GroupID: p.GroupId}
	}
	return result
}

func (c ContestLogService) GetUserContestHistory(ctx context.Context, req *contest_log.GetUserContestHistoryReq) (*contest_log.GetUserContestHistoryRes, error) {
	logs, err := c.sbDal.GetContestByUserId(ctx, req.UserId, req.Cursor, req.Limit, req.Platform)
	if err != nil {
		return nil, errors.InternalServer("内部服务器错误", err.Error())
	}

	items := make([]*contest_log.ContestLog, 0, len(logs))
	for _, v := range logs {
		items = append(items, &contest_log.ContestLog{
			Id:          uint32(v.ID),
			Platform:    v.Platform,
			ContestId:   v.ContestId,
			ContestName: v.ContestName,
			ContestUrl:  v.ContestUrl,
			TotalCount:  int32(v.TotalCount),
			Time:        unixOrZero(v.Time),
			EndTime:     unixOrZero(v.EndTime),
		})
	}

	return &contest_log.GetUserContestHistoryRes{
		Code:    0,
		Message: "OK",
		Data:    items,
	}, nil
}

func unixOrZero(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}

func NewContestLogService(sbDal *dal.SpiderDal, data *data.Data, reg *discovery.Register) *ContestLogService {
	return &ContestLogService{
		sbDal: sbDal,
		db:    data.DB,
		rdb:   data.RDB,
		reg:   &reg.Reg,
	}
}

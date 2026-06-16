package server

import (
	"context"
	"cwxu-algo/api/core/v1/bulletin"
	"cwxu-algo/api/core/v1/contest_log"
	"cwxu-algo/api/core/v1/spider"
	statistic2 "cwxu-algo/api/core/v1/statistic"
	"cwxu-algo/api/core/v1/submit_log"
	"cwxu-algo/app/common/conf"
	authutil "cwxu-algo/app/common/utils/auth"
	"cwxu-algo/app/core_data/internal/service"
	"encoding/json"
	nethttp "net/http"
	"strconv"
	"strings"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/auth/jwt"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/selector"
	"github.com/go-kratos/kratos/v2/transport/http"
	jwt2 "github.com/golang-jwt/jwt/v5"
)

func NewWhiteListMatcher() selector.MatchFunc {
	whiteList := map[string]string{
		"/api.core.v1.submit_log.Submit/GetSubmitLog":        "",
		"/api.core.v1.contest_log.Contest/GetContestList":    "",
		"/api.core.v1.contest_log.Contest/GetContestRanking": "",
		"/api.core.v1.spider.Spider/GetSpider":               "",
		"/api.core.v1.statistic.Statistic/Heatmap":           "",
		"/api.core.v1.statistic.Statistic/PeriodCount":       "",
		"/v1/core/statistic/platform-period":                 "",
		"/v1/core/statistic/team-period":                     "",
		"/v1/core/statistic/explanation":                     "",
		"/v1/core/statistic/platform-detail":                 "",
		"/v1/core/achievement/global-snapshot":               "",
		"/api.core.v1.bulletin.Bulletin/Get":                 "",
		"/api.core.v1.bulletin.Bulletin/List":                "",
	}
	return func(ctx context.Context, operation string) bool {
		//log.Info(operation)
		if _, ok := whiteList[operation]; ok {
			return false
		}
		return true
	}
}

// NewHTTPServer new an HTTP server.
func NewHTTPServer(c *conf.Server, logger log.Logger, submitService *service.SubmitLogService, spiderService *service.SpiderService, statisticService *service.StatisticService, contestLogService *service.ContestLogService, bulletinService *service.BulletinService) *http.Server {
	jwtSecret, err := authutil.JWTSecretBytes(c)
	if err != nil {
		panic(err)
	}
	var opts = []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			selector.Server(jwt.Server(func(token *jwt2.Token) (interface{}, error) {
				return jwtSecret, nil
			})).Match(NewWhiteListMatcher()).Build(),
		),
	}
	if c.Http.Network != "" {
		opts = append(opts, http.Network(c.Http.Network))
	}
	if c.Http.Addr != "" {
		opts = append(opts, http.Address(c.Http.Addr))
	}
	if c.Http.Timeout != nil {
		opts = append(opts, http.Timeout(c.Http.Timeout.AsDuration()))
	}
	srv := http.NewServer(opts...)
	submit_log.RegisterSubmitHTTPServer(srv, submitService)
	spider.RegisterSpiderHTTPServer(srv, spiderService)
	statistic2.RegisterStatisticHTTPServer(srv, statisticService)
	contest_log.RegisterContestHTTPServer(srv, contestLogService)
	bulletin.RegisterBulletinHTTPServer(srv, bulletinService)
	srv.HandleFunc("/v1/core/statistic/platform-period", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}

		userId := int64(-1)
		if raw := r.URL.Query().Get("userId"); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				w.WriteHeader(nethttp.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 400, "message": "userId参数错误"})
				return
			}
			userId = parsed
		}

		data, err := statisticService.PlatformPeriod(r.Context(), userId)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(nethttp.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "message": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	})
	srv.HandleFunc("/v1/core/statistic/team-period", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}
		userIds, err := parseIntCSVQuery(r, "userIds")
		if err != nil || len(userIds) == 0 {
			writeHTTPError(w, nethttp.StatusBadRequest, "userIds参数错误")
			return
		}
		if len(userIds) > 100 {
			writeHTTPError(w, nethttp.StatusBadRequest, "团队成员数量过多")
			return
		}
		res, err := statisticService.TeamPeriod(r.Context(), userIds)
		writeHTTPJSON(w, res, err)
	})
	srv.HandleFunc("/v1/core/statistic/explanation", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statisticService.Explanation())
	})
	srv.HandleFunc("/v1/core/statistic/platform-detail", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}
		userId, err := parseIntQuery(r, "userId", 0)
		if err != nil {
			writeHTTPError(w, nethttp.StatusBadRequest, "userId参数错误")
			return
		}
		page, err := parseIntQuery(r, "page", 1)
		if err != nil {
			writeHTTPError(w, nethttp.StatusBadRequest, "page参数错误")
			return
		}
		pageSize, err := parseIntQuery(r, "pageSize", 30)
		if err != nil {
			writeHTTPError(w, nethttp.StatusBadRequest, "pageSize参数错误")
			return
		}
		res, err := statisticService.PlatformDetail(r.Context(), userId, r.URL.Query().Get("platform"), r.URL.Query().Get("mode"), page, pageSize)
		writeHTTPJSON(w, res, err)
	})
	srv.HandleFunc("/v1/core/achievement/global-snapshot", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}
		res, err := statisticService.GetAchievementGlobalSnapshot(r.Context())
		writeHTTPJSON(w, res, err)
	})
	srv.HandleFunc("/v1/core/spider/audit", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}
		userId, err := parseIntQuery(r, "userId", 0)
		if err != nil {
			writeHTTPError(w, nethttp.StatusBadRequest, "userId参数错误")
			return
		}
		res, err := statisticService.SpiderAudit(r.Context(), userId)
		writeHTTPJSON(w, res, err)
	})
	srv.HandleFunc("/v1/core/statistic/cache-status", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}
		userId, err := parseIntQuery(r, "userId", -1)
		if err != nil {
			writeHTTPError(w, nethttp.StatusBadRequest, "userId参数错误")
			return
		}
		res, err := statisticService.CacheStatus(r.Context(), userId)
		writeHTTPJSON(w, res, err)
	})
	srv.HandleFunc("/v1/core/statistic/cache-clear", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}
		var req struct {
			UserID int64 `json:"userId"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		res, err := statisticService.ClearCache(r.Context(), req.UserID)
		writeHTTPJSON(w, res, err)
	})
	srv.HandleFunc("/v1/core/spider/retry", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}
		var req struct {
			JobId int64 `json:"jobId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(nethttp.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 400, "message": "请求参数错误"})
			return
		}
		res, err := spiderService.Retry(r.Context(), req.JobId)
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			statusCode := nethttp.StatusInternalServerError
			if se := kerrors.FromError(err); se != nil {
				statusCode = int(se.Code)
			}
			w.WriteHeader(statusCode)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": statusCode, "message": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(res)
	})
	srv.HandleFunc("/v1/core/spider/rebuild-all", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodPost {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		res, err := spiderService.RebuildAll(r.Context())
		if err != nil {
			statusCode := nethttp.StatusInternalServerError
			if se := kerrors.FromError(err); se != nil {
				statusCode = int(se.Code)
			}
			w.WriteHeader(statusCode)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": statusCode, "message": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(res)
	})
	srv.HandleFunc("/v1/core/operation-logs", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.Method != nethttp.MethodGet {
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
			return
		}
		page, err := parseIntQuery(r, "page", 1)
		if err != nil {
			writeHTTPError(w, nethttp.StatusBadRequest, "page参数错误")
			return
		}
		pageSize, err := parseIntQuery(r, "pageSize", 30)
		if err != nil {
			writeHTTPError(w, nethttp.StatusBadRequest, "pageSize参数错误")
			return
		}
		res, err := statisticService.OperationLogs(r.Context(), page, pageSize, r.URL.Query().Get("action"))
		writeHTTPJSON(w, res, err)
	})
	srv.HandleFunc("/v1/core/snapshot", func(w nethttp.ResponseWriter, r *nethttp.Request) {
		switch r.Method {
		case nethttp.MethodGet:
			userId, err := parseIntQuery(r, "userId", 0)
			if err != nil {
				writeHTTPError(w, nethttp.StatusBadRequest, "userId参数错误")
				return
			}
			res, err := statisticService.GetFeatureSnapshot(
				r.Context(),
				userId,
				r.URL.Query().Get("kind"),
				r.URL.Query().Get("sourceHash"),
			)
			writeHTTPJSON(w, res, err)
		case nethttp.MethodPost:
			var req service.SaveFeatureSnapshotRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeHTTPError(w, nethttp.StatusBadRequest, "请求参数错误")
				return
			}
			res, err := statisticService.SaveFeatureSnapshot(r.Context(), req)
			writeHTTPJSON(w, res, err)
		default:
			w.WriteHeader(nethttp.StatusMethodNotAllowed)
		}
	})
	return srv
}

func parseIntQuery(r *nethttp.Request, key string, fallback int64) (int64, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

func parseIntCSVQuery(r *nethttp.Request, key string) ([]int64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	seen := make(map[int64]bool)
	result := make([]int64, 0)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		value, err := strconv.ParseInt(part, 10, 64)
		if err != nil || value <= 0 {
			return nil, err
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result, nil
}

func writeHTTPError(w nethttp.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": statusCode, "message": message})
}

func writeHTTPJSON(w nethttp.ResponseWriter, data any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		statusCode := nethttp.StatusInternalServerError
		if se := kerrors.FromError(err); se != nil {
			statusCode = int(se.Code)
		}
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": statusCode, "message": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(data)
}

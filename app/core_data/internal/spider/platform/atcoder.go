package platform

import (
	"cwxu-algo/app/core_data/internal/data/model"
	"cwxu-algo/app/core_data/internal/spider"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type NewAtCoder struct{}
type atcJson struct {
	ID            int    `json:"id"`
	EpochSecond   int64  `json:"epoch_second"` // Unix 时间戳（秒）
	ProblemID     string `json:"problem_id"`
	ContestID     string `json:"contest_id"`
	UserID        string `json:"user_id"`
	Language      string `json:"language"`
	Result        string `json:"result"`         // 如 "AC", "WA" 等
	ExecutionTime int    `json:"execution_time"` // 执行时间（毫秒）
}

var (
	atCoderAPIBaseURL     = "https://atc.luckysan.top/atcoder/atcoder-api/v3/user/submissions"
	atCoderContestBaseURL = "https://atcoder.jp/contests"
	atCoderPageSize       = 500

	atCoderContestTitleRe = regexp.MustCompile(`<a[^>]+class=['"][^'"]*contest-title[^'"]*['"][^>]*>([^<]+)</a>`)
	atCoderContestTimeRe  = regexp.MustCompile(`<time[^>]+class=['"][^'"]*fixtime-full[^'"]*['"][^>]*>([^<]+)</time>`)
	atCoderContestMetaMap sync.Map
)

type atCoderContestMeta struct {
	Title string
	Start time.Time
	End   time.Time
}

func fetchAtCoderLog(client *http.Client, baseURL string, username string, fromSecond int64) ([]atcJson, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("user", username)
	q.Set("from_second", strconv.FormatInt(fromSecond, 10))
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "WUST-ACM-Tracker/1.1 (+https://github.com/WUSTACM/WUST-Algo-tracker)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发起http请求失败: %s", err.Error())
	}
	defer resp.Body.Close()
	// 校验状态码
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("请求响应码错误 %d, %s", resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("解析body错误: %s", err.Error())
	}
	var atc []atcJson
	if err := json.Unmarshal(body, &atc); err != nil {
		return nil, fmt.Errorf("解析json错误：%s", err.Error())
	}
	return atc, nil
}

func fetchAtCoderContestMeta(client *http.Client, contestID string) (atCoderContestMeta, error) {
	contestID = strings.TrimSpace(contestID)
	if contestID == "" {
		return atCoderContestMeta{}, fmt.Errorf("AtCoder contest_id 为空")
	}
	if cached, ok := atCoderContestMetaMap.Load(contestID); ok {
		return cached.(atCoderContestMeta), nil
	}

	u, err := url.Parse(strings.TrimRight(atCoderContestBaseURL, "/") + "/" + url.PathEscape(contestID))
	if err != nil {
		return atCoderContestMeta{}, err
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return atCoderContestMeta{}, err
	}
	req.Header.Set("User-Agent", "WUST-ACM-Tracker/1.1 (+https://github.com/WUSTACM/WUST-Algo-tracker)")

	resp, err := client.Do(req)
	if err != nil {
		return atCoderContestMeta{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return atCoderContestMeta{}, fmt.Errorf("AtCoder contest 页面响应码错误 %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return atCoderContestMeta{}, err
	}
	raw := string(body)

	meta := atCoderContestMeta{Title: contestID}
	if matches := atCoderContestTitleRe.FindStringSubmatch(raw); len(matches) >= 2 {
		meta.Title = strings.TrimSpace(html.UnescapeString(matches[1]))
	}
	timeMatches := atCoderContestTimeRe.FindAllStringSubmatch(raw, -1)
	if len(timeMatches) >= 1 && len(timeMatches[0]) >= 2 {
		start, err := time.Parse("2006-01-02 15:04:05-0700", strings.TrimSpace(timeMatches[0][1]))
		if err == nil {
			meta.Start = start
		}
	}
	if len(timeMatches) >= 2 && len(timeMatches[1]) >= 2 {
		end, err := time.Parse("2006-01-02 15:04:05-0700", strings.TrimSpace(timeMatches[1][1]))
		if err == nil {
			meta.End = end
		}
	}
	if meta.Start.IsZero() {
		return atCoderContestMeta{}, fmt.Errorf("AtCoder contest %s 未解析到开始时间", contestID)
	}
	atCoderContestMetaMap.Store(contestID, meta)
	return meta, nil
}

func atCoderRowsToSubmitLogs(userId int64, rows []atcJson) []model.SubmitLog {
	res := make([]model.SubmitLog, 0, len(rows))
	for _, v := range rows {
		res = append(res, model.SubmitLog{
			UserID:   userId,
			Platform: spider.AtCoder,
			SubmitID: strconv.Itoa(v.ID),
			Contest:  v.ContestID,
			Problem:  v.ProblemID,
			Lang:     v.Language,
			Status:   v.Result,
			Time:     time.Unix(v.EpochSecond, 0),
		})
	}
	return res
}

func fetchAtCoderRows(username string, needAll bool) (res []atcJson, err error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, fmt.Errorf("AtCoder 用户名不能为空")
	}
	fromSecond := int64(0)
	if !needAll {
		fromSecond = time.Now().Add(-60 * time.Minute).Unix()
	}
	client := &http.Client{Timeout: 30 * time.Second}
	seen := make(map[int]struct{})
	for page := 0; ; page++ {
		atc, err := fetchAtCoderLog(client, atCoderAPIBaseURL, username, fromSecond)
		if err != nil {
			return nil, err
		}
		if len(atc) == 0 {
			break
		}
		newRows := make([]atcJson, 0, len(atc))
		lastSecond := fromSecond
		for _, row := range atc {
			if row.EpochSecond > lastSecond {
				lastSecond = row.EpochSecond
			}
			if _, ok := seen[row.ID]; ok {
				continue
			}
			seen[row.ID] = struct{}{}
			newRows = append(newRows, row)
		}
		res = append(res, newRows...)
		if !needAll || len(atc) < atCoderPageSize {
			break
		}
		if len(newRows) == 0 || lastSecond <= fromSecond {
			return nil, fmt.Errorf("AtCoder 翻页没有前进，停止以避免重复抓取: user=%s from_second=%d", username, fromSecond)
		}
		fromSecond = lastSecond
		time.Sleep(300 * time.Millisecond)
	}
	return res, nil
}

func (p NewAtCoder) FetchSubmitLog(userId int64, username string, needAll bool) (res []model.SubmitLog, err error) {
	rows, err := fetchAtCoderRows(username, needAll)
	if err != nil {
		return nil, err
	}
	return atCoderRowsToSubmitLogs(userId, rows), nil
}

// FetchContestLog 基于 AtCoder 提交记录聚合比赛记录。
//
// AtCoder 的公开提交 API 不提供用户比赛排名，这里只展示真实可追溯的数据：
// contest_id、提交过题数、AC 过题数，并用 contest 页面解析官方开始时间。
func (p NewAtCoder) FetchContestLog(userId int64, username string, needAll bool) ([]model.ContestLog, error) {
	rows, err := fetchAtCoderRows(username, needAll)
	if err != nil {
		return nil, err
	}

	type contestAgg struct {
		log       model.ContestLog
		submitted map[string]struct{}
		accepted  map[string]struct{}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	type contestMetaResult struct {
		meta atCoderContestMeta
		err  error
	}
	metaResults := make(map[string]contestMetaResult)
	getMeta := func(contestID string) contestMetaResult {
		if result, ok := metaResults[contestID]; ok {
			return result
		}
		meta, err := fetchAtCoderContestMeta(client, contestID)
		result := contestMetaResult{meta: meta, err: err}
		metaResults[contestID] = result
		return result
	}

	aggs := make(map[string]*contestAgg)
	for _, row := range rows {
		contestID := strings.TrimSpace(row.ContestID)
		problemID := strings.TrimSpace(row.ProblemID)
		if contestID == "" || problemID == "" {
			continue
		}
		submitTime := time.Unix(row.EpochSecond, 0)
		metaResult := getMeta(contestID)
		if metaResult.err == nil && !metaResult.meta.End.IsZero() {
			// AtCoder submissions API also returns upsolves. Contest logs should
			// reflect only submissions made during the official contest window.
			if submitTime.Before(metaResult.meta.Start) || !submitTime.Before(metaResult.meta.End) {
				continue
			}
		}
		agg, ok := aggs[contestID]
		if !ok {
			contestName := contestID
			contestTime := submitTime
			if metaResult.err == nil {
				contestName = metaResult.meta.Title
				contestTime = metaResult.meta.Start
			}
			agg = &contestAgg{
				log: model.ContestLog{
					Platform:    spider.AtCoder,
					UserID:      userId,
					ContestId:   contestID,
					ContestName: contestName,
					ContestUrl:  "https://atcoder.jp/contests/" + contestID,
					Time:        contestTime,
				},
				submitted: make(map[string]struct{}),
				accepted:  make(map[string]struct{}),
			}
			aggs[contestID] = agg
		}
		agg.submitted[problemID] = struct{}{}
		if strings.EqualFold(strings.TrimSpace(row.Result), "AC") {
			agg.accepted[problemID] = struct{}{}
		}
		if metaResult.err != nil && row.EpochSecond < agg.log.Time.Unix() {
			agg.log.Time = submitTime
		}
	}

	result := make([]model.ContestLog, 0, len(aggs))
	for _, agg := range aggs {
		if meta, err := fetchAtCoderContestMeta(client, agg.log.ContestId); err == nil {
			agg.log.ContestName = meta.Title
			agg.log.Time = meta.Start
			agg.log.EndTime = meta.End
		}
		agg.log.TotalCount = len(agg.submitted)
		agg.log.AcCount = len(agg.accepted)
		result = append(result, agg.log)
	}
	return result, nil
}
func (p NewAtCoder) Name() string {
	return spider.AtCoder
}
func init() {
	// 注册到注册中心
	spider.Register(NewAtCoder{})
}

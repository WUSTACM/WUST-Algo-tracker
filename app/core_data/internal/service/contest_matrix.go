package service

import (
	"context"
	"cwxu-algo/api/core/v1/contest_log"
	"cwxu-algo/app/core_data/internal/data/dal"
	"cwxu-algo/app/core_data/internal/data/model"
	"cwxu-algo/app/core_data/internal/spider"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

type contestProblemMeta struct {
	Key     string
	Index   string
	Name    string
	URL     string
	Aliases []string
}

type contestMatrixResult struct {
	Problems       []*contest_log.ProblemColumn
	ResultsByUser  map[int64][]*contest_log.ProblemResult
	PenaltyByUser  map[int64]int32
	DegradedReason string
}

var (
	atCoderTaskLinkRe      = regexp.MustCompile(`<a[^>]+href=["']/contests/[^"']+/tasks/([^"']+)["'][^>]*>([^<]+)</a>`)
	atCoderMatrixTimeRe    = regexp.MustCompile(`<time[^>]+class=['"][^'"]*fixtime-full[^'"]*['"][^>]*>([^<]+)</time>`)
	atCoderMatrixTitleRe   = regexp.MustCompile(`<a[^>]+class=['"][^'"]*contest-title[^'"]*['"][^>]*>([^<]+)</a>`)
	atCoderTaskAlphabetRe  = regexp.MustCompile(`^[A-Za-z]+$`)
	nowCoderProblemIndexRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*$`)
	nowCoderProblemLinkRe  = regexp.MustCompile(`<a[^>]+href=["'](/acm/contest/(\d+)/([^"'/?#]+))["'][^>]*>([^<]*)</a>`)
	nowCoderStartTimeRe    = regexp.MustCompile(`"startTime"\s*:\s*(\d+)`)
	nowCoderEndTimeRe      = regexp.MustCompile(`"endTime"\s*:\s*(\d+)`)
	nowCoderNameRe         = regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)
)

var (
	atCoderMatrixMetaCache    = newTTLCache[atCoderMatrixMeta](6 * time.Hour)
	codeforcesMatrixMetaCache = newTTLCache[atCoderMatrixMeta](6 * time.Hour)
	codeforcesStandingsCache  = newTTLCache[codeforcesStandingsResponse](6 * time.Hour)
	nowCoderMatrixMetaCache   = newTTLCache[atCoderMatrixMeta](6 * time.Hour)
)

func (c ContestLogService) buildContestMatrix(ctx context.Context, contest model.ContestLog, pageLogs []model.ContestLog, allLogs []model.ContestLog) contestMatrixResult {
	result := contestMatrixResult{
		ResultsByUser: make(map[int64][]*contest_log.ProblemResult),
		PenaltyByUser: make(map[int64]int32),
	}
	if len(pageLogs) == 0 || contest.ContestId == "" || contest.Platform == "" {
		return result
	}

	start := contest.Time
	end := contest.EndTime
	metaProblems := make([]contestProblemMeta, 0)

	if isContestPlatform(contest.Platform, spider.AtCoder) {
		meta, err := fetchAtCoderMatrixMeta(contest.ContestId)
		if err == nil {
			if !meta.Start.IsZero() {
				start = meta.Start
			}
			if !meta.End.IsZero() {
				end = meta.End
			}
			metaProblems = meta.Problems
		} else {
			result.DegradedReason = "AtCoder 元数据获取失败，题目列按本站提交记录生成。"
		}
	}
	if isContestPlatform(contest.Platform, spider.CodeForces) {
		meta, err := fetchCodeforcesMatrixMeta(contest.ContestId)
		if err == nil {
			if !meta.Start.IsZero() {
				start = meta.Start
			}
			if !meta.End.IsZero() {
				end = meta.End
			}
			metaProblems = meta.Problems
		} else if result.DegradedReason == "" {
			result.DegradedReason = "Codeforces 元数据获取失败，题目列按本站提交记录生成。"
		}
	}
	if isContestPlatform(contest.Platform, spider.NowCoder) {
		meta, err := fetchNowCoderMatrixMeta(contest.ContestId)
		if err == nil {
			if !meta.Start.IsZero() {
				start = meta.Start
			}
			if !meta.End.IsZero() {
				end = meta.End
			}
			metaProblems = meta.Problems
		} else if result.DegradedReason == "" {
			result.DegradedReason = "NowCoder 元数据获取失败，题目列按本站提交记录生成。"
		}
	}

	allUserIDs := collectContestUserIDs(allLogs)
	pageUserIDs := collectContestUserIDs(pageLogs)
	hasReliableWindow := !start.IsZero() && !end.IsZero() && end.After(start)
	submissions := c.loadContestSubmissions(ctx, contest.Platform, contest.ContestId, allUserIDs, start, end)
	if !hasReliableWindow {
		if result.DegradedReason == "" {
			result.DegradedReason = "缺少官方比赛结束时间，赛后补题不会单独标记。"
		}
	}
	if isContestPlatform(contest.Platform, spider.NowCoder) && len(metaProblems) == 0 && contest.TotalCount > 0 {
		metaProblems = synthesizeNowCoderProblems(contest.ContestId, int(contest.TotalCount), submissions)
	}

	problems := normalizeContestProblems(contest.Platform, contest.ContestId, metaProblems, submissions)
	result.Problems = make([]*contest_log.ProblemColumn, 0, len(problems))
	for _, problem := range problems {
		result.Problems = append(result.Problems, &contest_log.ProblemColumn{
			Index:      problem.Index,
			Name:       problem.Name,
			ProblemKey: problem.Key,
			ProblemUrl: problem.URL,
		})
	}

	calculated := calculateContestProblemMatrix(start, end, hasReliableWindow, problems, submissions, pageUserIDs, allUserIDs)
	for i, problem := range result.Problems {
		if stat, ok := calculated.problemStats[problem.ProblemKey]; ok {
			result.Problems[i].ContestAccepted = int32(stat.contestAccepted)
			result.Problems[i].ContestAttempted = int32(stat.contestAttempted)
			result.Problems[i].UpsolveAccepted = int32(stat.upsolveAccepted)
		}
	}
	result.ResultsByUser = calculated.resultsByUser
	result.PenaltyByUser = calculated.penaltyByUser
	return result
}

func (c ContestLogService) loadContestSubmissions(ctx context.Context, platform string, contestID string, userIDs []int64, start time.Time, end time.Time) []model.SubmitLog {
	if len(userIDs) == 0 {
		return nil
	}
	var submissions []model.SubmitLog
	query := c.db.WithContext(ctx).
		Where("LOWER(platform) = LOWER(?) AND contest = ? AND user_id IN ?", platform, contestID, userIDs).
		Order("time ASC")
	if err := query.Find(&submissions).Error; err != nil {
		log.Errorf("load contest submissions failed: %v", err)
		return nil
	}
	if len(submissions) > 0 || start.IsZero() || end.IsZero() || !end.After(start) {
		return submissions
	}

	// Some legacy NowCoder submissions do not carry contest_id. Fall back to the
	// official contest window so those contests can still render a usable matrix.
	err := c.db.WithContext(ctx).
		Where("LOWER(platform) = LOWER(?) AND user_id IN ? AND time >= ? AND time < ?", platform, userIDs, start, end).
		Order("time ASC").
		Find(&submissions).Error
	if err != nil {
		log.Errorf("load contest submissions by window failed: %v", err)
		return nil
	}
	return submissions
}

type atCoderMatrixMeta struct {
	Title    string
	Start    time.Time
	End      time.Time
	Problems []contestProblemMeta
}

func fetchAtCoderMatrixMeta(contestID string) (atCoderMatrixMeta, error) {
	contestID = strings.TrimSpace(contestID)
	if contestID == "" {
		return atCoderMatrixMeta{}, fmt.Errorf("empty contest id")
	}
	cacheKey := strings.ToLower(contestID)
	if cached, ok := atCoderMatrixMetaCache.Get(cacheKey); ok {
		return cached, nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	baseURL := "https://atcoder.jp/contests/" + url.PathEscape(contestID)
	meta := atCoderMatrixMeta{Title: contestID}

	if err := fetchAtCoderContestPage(client, baseURL, &meta); err != nil {
		return atCoderMatrixMeta{}, err
	}
	tasks, err := fetchAtCoderTaskPage(client, baseURL+"/tasks", contestID)
	if err == nil {
		meta.Problems = tasks
	}
	atCoderMatrixMetaCache.Set(cacheKey, meta)
	return meta, nil
}

type codeforcesMatrixProblem struct {
	ContestID      int    `json:"contestId"`
	ProblemsetName string `json:"problemsetName"`
	Index          string `json:"index"`
	Name           string `json:"name"`
}

type codeforcesStandingsResponse struct {
	Status  string `json:"status"`
	Comment string `json:"comment"`
	Result  struct {
		Contest struct {
			ID              int    `json:"id"`
			Name            string `json:"name"`
			StartTime       int64  `json:"startTimeSeconds"`
			DurationSeconds int64  `json:"durationSeconds"`
		} `json:"contest"`
		Problems []codeforcesMatrixProblem `json:"problems"`
		Rows     []struct {
			Rank  int `json:"rank"`
			Party struct {
				Members []struct {
					Handle string `json:"handle"`
				} `json:"members"`
			} `json:"party"`
		} `json:"rows"`
	} `json:"result"`
}

func fetchCodeforcesMatrixMeta(contestID string) (atCoderMatrixMeta, error) {
	contestID = strings.TrimSpace(contestID)
	if contestID == "" {
		return atCoderMatrixMeta{}, fmt.Errorf("empty contest id")
	}
	cacheKey := strings.ToLower(contestID)
	if cached, ok := codeforcesMatrixMetaCache.Get(cacheKey); ok {
		return cached, nil
	}
	parsed, err := fetchCodeforcesStandings(contestID)
	if err != nil {
		return atCoderMatrixMeta{}, err
	}
	meta := codeforcesStandingsToMeta(contestID, parsed)
	codeforcesMatrixMetaCache.Set(cacheKey, meta)
	return meta, nil
}

func fetchCodeforcesStandings(contestID string) (codeforcesStandingsResponse, error) {
	contestID = strings.TrimSpace(contestID)
	if contestID == "" {
		return codeforcesStandingsResponse{}, fmt.Errorf("empty contest id")
	}
	cacheKey := strings.ToLower(contestID)
	if cached, ok := codeforcesStandingsCache.Get(cacheKey); ok {
		return cached, nil
	}
	client := &http.Client{Timeout: 15 * time.Second}
	u := "https://codeforces.com/api/contest.standings?contestId=" + url.QueryEscape(contestID)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return codeforcesStandingsResponse{}, err
	}
	req.Header.Set("User-Agent", "WUST-ACM-Tracker/1.1 (+https://github.com/WUSTACM/WUST-Algo-tracker)")
	resp, err := client.Do(req)
	if err != nil {
		return codeforcesStandingsResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return codeforcesStandingsResponse{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return codeforcesStandingsResponse{}, fmt.Errorf("codeforces standings status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed codeforcesStandingsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return codeforcesStandingsResponse{}, err
	}
	if parsed.Status != "OK" {
		return codeforcesStandingsResponse{}, fmt.Errorf("codeforces standings failed: %s", parsed.Comment)
	}
	codeforcesStandingsCache.SetWithTTL(cacheKey, parsed, codeforcesStandingsTTL(parsed))
	return parsed, nil
}

func codeforcesStandingsTTL(parsed codeforcesStandingsResponse) time.Duration {
	startTime := parsed.Result.Contest.StartTime
	duration := parsed.Result.Contest.DurationSeconds
	if startTime <= 0 || duration <= 0 {
		return time.Minute
	}
	end := time.Unix(startTime, 0).Add(time.Duration(duration) * time.Second)
	if time.Now().Before(end.Add(10 * time.Minute)) {
		return time.Minute
	}
	return 6 * time.Hour
}

func codeforcesStandingsToMeta(contestID string, parsed codeforcesStandingsResponse) atCoderMatrixMeta {
	meta := atCoderMatrixMeta{Title: parsed.Result.Contest.Name}
	if parsed.Result.Contest.StartTime > 0 {
		meta.Start = time.Unix(parsed.Result.Contest.StartTime, 0)
	}
	if !meta.Start.IsZero() && parsed.Result.Contest.DurationSeconds > 0 {
		meta.End = meta.Start.Add(time.Duration(parsed.Result.Contest.DurationSeconds) * time.Second)
	}
	meta.Problems = make([]contestProblemMeta, 0, len(parsed.Result.Problems))
	for _, problem := range parsed.Result.Problems {
		key := codeforcesMatrixProblemKey(contestID, problem)
		if key == "" {
			continue
		}
		index := strings.TrimSpace(problem.Index)
		if index == "" {
			index = deriveProblemIndex(spider.CodeForces, contestID, key, len(meta.Problems))
		}
		name := strings.TrimSpace(problem.Name)
		if name == "" {
			name = key
		}
		meta.Problems = append(meta.Problems, contestProblemMeta{
			Key:   key,
			Index: index,
			Name:  name,
			URL:   codeforcesProblemURL(contestID, problem),
		})
	}
	return meta
}

func codeforcesMatrixProblemKey(contestID string, problem codeforcesMatrixProblem) string {
	problemsetName := strings.TrimSpace(problem.ProblemsetName)
	problemContestID := strings.TrimSpace(contestID)
	if problem.ContestID != 0 {
		problemContestID = fmt.Sprintf("%d", problem.ContestID)
	}
	problemIndex := strings.TrimSpace(problem.Index)
	problemName := strings.TrimSpace(problem.Name)
	parts := make([]string, 0, 3)
	if problemsetName != "" {
		parts = append(parts, problemsetName)
	}
	if problemContestID != "" {
		parts = append(parts, problemContestID)
	}
	if problemIndex != "" {
		parts = append(parts, problemIndex)
	}
	prefix := strings.Join(parts, "-")
	switch {
	case prefix != "" && problemName != "":
		return prefix + " " + problemName
	case prefix != "":
		return prefix
	default:
		return problemName
	}
}

func fetchNowCoderMatrixMeta(contestID string) (atCoderMatrixMeta, error) {
	contestID = strings.TrimSpace(contestID)
	if contestID == "" {
		return atCoderMatrixMeta{}, fmt.Errorf("empty contest id")
	}
	cacheKey := strings.ToLower(contestID)
	if cached, ok := nowCoderMatrixMetaCache.Get(cacheKey); ok {
		return cached, nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	pageURL := "https://ac.nowcoder.com/acm/contest/" + url.PathEscape(contestID)
	body, err := fetchContestHTML(client, pageURL)
	if err != nil {
		return atCoderMatrixMeta{}, err
	}
	raw := string(body)
	meta := atCoderMatrixMeta{Title: contestID}
	if matches := nowCoderNameRe.FindStringSubmatch(raw); len(matches) >= 2 {
		meta.Title = strings.TrimSpace(html.UnescapeString(matches[1]))
	}
	if matches := nowCoderStartTimeRe.FindStringSubmatch(raw); len(matches) >= 2 {
		if ms, err := strconv.ParseInt(matches[1], 10, 64); err == nil && ms > 0 {
			meta.Start = time.Unix(ms/1000, 0)
		}
	}
	if matches := nowCoderEndTimeRe.FindStringSubmatch(raw); len(matches) >= 2 {
		if ms, err := strconv.ParseInt(matches[1], 10, 64); err == nil && ms > 0 {
			meta.End = time.Unix(ms/1000, 0)
		}
	}
	meta.Problems = parseNowCoderProblems(raw, contestID)
	if meta.Start.IsZero() || meta.End.IsZero() {
		return atCoderMatrixMeta{}, fmt.Errorf("missing nowcoder contest window")
	}
	nowCoderMatrixMetaCache.Set(cacheKey, meta)
	return meta, nil
}

func fetchAtCoderContestPage(client *http.Client, pageURL string, meta *atCoderMatrixMeta) error {
	body, err := fetchContestHTML(client, pageURL)
	if err != nil {
		return err
	}
	raw := string(body)
	if matches := atCoderMatrixTitleRe.FindStringSubmatch(raw); len(matches) >= 2 {
		meta.Title = strings.TrimSpace(html.UnescapeString(matches[1]))
	}
	timeMatches := atCoderMatrixTimeRe.FindAllStringSubmatch(raw, -1)
	if len(timeMatches) >= 1 && len(timeMatches[0]) >= 2 {
		if start, err := time.Parse("2006-01-02 15:04:05-0700", strings.TrimSpace(timeMatches[0][1])); err == nil {
			meta.Start = start
		}
	}
	if len(timeMatches) >= 2 && len(timeMatches[1]) >= 2 {
		if end, err := time.Parse("2006-01-02 15:04:05-0700", strings.TrimSpace(timeMatches[1][1])); err == nil {
			meta.End = end
		}
	}
	if meta.Start.IsZero() {
		return fmt.Errorf("missing contest start time")
	}
	return nil
}

func fetchAtCoderTaskPage(client *http.Client, tasksURL string, contestID string) ([]contestProblemMeta, error) {
	body, err := fetchContestHTML(client, tasksURL)
	if err != nil {
		return nil, err
	}
	matches := atCoderTaskLinkRe.FindAllStringSubmatch(string(body), -1)
	seen := map[string]struct{}{}
	tasks := make([]contestProblemMeta, 0)
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		key := strings.TrimSpace(html.UnescapeString(match[1]))
		title := strings.TrimSpace(html.UnescapeString(match[2]))
		if key == "" || title == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tasks = append(tasks, contestProblemMeta{
			Key:   key,
			Index: deriveProblemIndex(spider.AtCoder, contestID, key, len(tasks)),
			Name:  title,
			URL:   atCoderProblemURL(contestID, key),
		})
	}
	return tasks, nil
}

func parseNowCoderProblems(raw string, contestID string) []contestProblemMeta {
	contestID = strings.TrimSpace(contestID)
	if contestID == "" {
		return nil
	}
	matches := nowCoderProblemLinkRe.FindAllStringSubmatch(raw, -1)
	seen := map[string]struct{}{}
	problems := make([]contestProblemMeta, 0)
	for _, match := range matches {
		if len(match) < 5 || strings.TrimSpace(match[2]) != contestID {
			continue
		}
		index := strings.TrimSpace(html.UnescapeString(match[3]))
		if !nowCoderProblemIndexRe.MatchString(index) {
			continue
		}
		title := strings.TrimSpace(html.UnescapeString(match[4]))
		if title == "" {
			title = index
		}
		url := "https://ac.nowcoder.com" + strings.TrimSpace(match[1])
		key := title
		if key == "" || key == index {
			key = index
		}
		if _, ok := seen[index]; ok {
			continue
		}
		seen[index] = struct{}{}
		problems = append(problems, contestProblemMeta{
			Key:     key,
			Index:   strings.ToUpper(index),
			Name:    title,
			URL:     url,
			Aliases: nowCoderProblemAliases(index, title),
		})
	}
	sort.SliceStable(problems, func(i, j int) bool {
		return naturalProblemOrder(problems[i].Index, problems[j].Index, problems[i].Key, problems[j].Key)
	})
	return problems
}

func synthesizeNowCoderProblems(contestID string, totalCount int, submissions []model.SubmitLog) []contestProblemMeta {
	if totalCount <= 0 {
		return nil
	}
	seen := map[string]struct{}{}
	problems := make([]contestProblemMeta, 0, totalCount)
	for _, sub := range submissions {
		key := strings.TrimSpace(sub.Problem)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		index := deriveProblemIndex(spider.NowCoder, contestID, key, len(problems))
		problems = append(problems, contestProblemMeta{
			Key:     key,
			Index:   index,
			Name:    key,
			URL:     nowCoderProblemURL(contestID, index),
			Aliases: nowCoderProblemAliases(index, key),
		})
		if len(problems) >= totalCount {
			break
		}
	}
	for len(problems) < totalCount {
		index := string(rune('A' + len(problems)%26))
		key := "__nowcoder_" + contestID + "_" + index
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		problems = append(problems, contestProblemMeta{
			Key:     key,
			Index:   index,
			Name:    index,
			URL:     nowCoderProblemURL(contestID, index),
			Aliases: []string{index},
		})
	}
	return problems
}

func fetchContestHTML(client *http.Client, pageURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "WUST-ACM-Tracker/1.1 (+https://github.com/WUSTACM/WUST-Algo-tracker)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("contest metadata status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func collectContestUserIDs(logs []model.ContestLog) []int64 {
	seen := map[int64]struct{}{}
	ids := make([]int64, 0, len(logs))
	for _, item := range logs {
		if item.UserID == 0 {
			continue
		}
		if _, ok := seen[item.UserID]; ok {
			continue
		}
		seen[item.UserID] = struct{}{}
		ids = append(ids, item.UserID)
	}
	return ids
}

func normalizeContestProblems(platform string, contestID string, metaProblems []contestProblemMeta, submissions []model.SubmitLog) []contestProblemMeta {
	seen := map[string]struct{}{}
	problems := make([]contestProblemMeta, 0, len(metaProblems))
	for _, problem := range metaProblems {
		key := strings.TrimSpace(problem.Key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if problem.Index == "" {
			problem.Index = deriveProblemIndex(platform, contestID, key, len(problems))
		}
		if problem.Name == "" {
			problem.Name = key
		}
		if problem.URL == "" {
			problem.URL = problemURL(platform, contestID, problem.Index, key)
		}
		problem.Key = key
		problem.Aliases = append(problem.Aliases, problem.Index, problem.Name)
		problems = append(problems, problem)
	}
	for _, sub := range submissions {
		key := strings.TrimSpace(sub.Problem)
		if key == "" {
			key = strings.TrimSpace(sub.SubmitID)
		}
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		problems = append(problems, contestProblemMeta{
			Key:   key,
			Index: deriveProblemIndex(platform, contestID, key, len(problems)),
			Name:  key,
			URL:   problemURL(platform, contestID, deriveProblemIndex(platform, contestID, key, len(problems)), key),
		})
	}
	if len(metaProblems) == 0 {
		sort.SliceStable(problems, func(i, j int) bool {
			return naturalProblemOrder(problems[i].Index, problems[j].Index, problems[i].Key, problems[j].Key)
		})
	}
	return problems
}

func problemURL(platform string, contestID string, index string, key string) string {
	switch {
	case isContestPlatform(platform, spider.AtCoder):
		taskID := strings.TrimSpace(key)
		if taskID == "" {
			taskID = strings.TrimSpace(index)
		}
		return atCoderProblemURL(contestID, taskID)
	case isContestPlatform(platform, spider.CodeForces):
		return codeforcesProblemURLFromIndex(contestID, index)
	case isContestPlatform(platform, spider.NowCoder):
		return nowCoderProblemURL(contestID, index)
	default:
		return ""
	}
}

func atCoderProblemURL(contestID string, taskID string) string {
	contestID = strings.TrimSpace(contestID)
	taskID = strings.TrimSpace(taskID)
	if contestID == "" || taskID == "" {
		return ""
	}
	return "https://atcoder.jp/contests/" + url.PathEscape(contestID) + "/tasks/" + url.PathEscape(taskID)
}

func codeforcesProblemURL(contestID string, problem codeforcesMatrixProblem) string {
	problemContestID := strings.TrimSpace(contestID)
	if problem.ContestID != 0 {
		problemContestID = fmt.Sprintf("%d", problem.ContestID)
	}
	return codeforcesProblemURLFromIndex(problemContestID, problem.Index)
}

func codeforcesProblemURLFromIndex(contestID string, index string) string {
	contestID = strings.TrimSpace(contestID)
	index = strings.TrimSpace(index)
	if contestID == "" || index == "" {
		return ""
	}
	return "https://codeforces.com/contest/" + url.PathEscape(contestID) + "/problem/" + url.PathEscape(index)
}

func nowCoderProblemURL(contestID string, index string) string {
	contestID = strings.TrimSpace(contestID)
	index = strings.TrimSpace(index)
	if contestID == "" || index == "" {
		return ""
	}
	return "https://ac.nowcoder.com/acm/contest/" + url.PathEscape(contestID) + "/" + url.PathEscape(index)
}

func nowCoderProblemAliases(index string, title string) []string {
	index = strings.TrimSpace(index)
	title = strings.TrimSpace(title)
	aliases := make([]string, 0, 4)
	if index != "" {
		aliases = append(aliases, index, strings.ToUpper(index))
	}
	if title != "" {
		aliases = append(aliases, title)
		if index != "" {
			aliases = append(aliases, index+" "+title, strings.ToUpper(index)+" "+title)
		}
	}
	return aliases
}

func buildProblemAliasMap(problems []contestProblemMeta) map[string]string {
	aliases := make(map[string]string, len(problems)*3)
	for _, problem := range problems {
		registerProblemAlias(aliases, problem.Key, problem.Key)
		registerProblemAlias(aliases, problem.Index, problem.Key)
		registerProblemAlias(aliases, problem.Name, problem.Key)
		for _, alias := range problem.Aliases {
			registerProblemAlias(aliases, alias, problem.Key)
		}
	}
	return aliases
}

func registerProblemAlias(aliases map[string]string, alias string, canonical string) {
	alias = strings.TrimSpace(alias)
	canonical = strings.TrimSpace(canonical)
	if alias == "" || canonical == "" {
		return
	}
	aliases[alias] = canonical
	aliases[strings.ToLower(alias)] = canonical
}

func canonicalProblemKey(raw string, aliases map[string]string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return ""
	}
	if canonical, ok := aliases[key]; ok {
		return canonical
	}
	if canonical, ok := aliases[strings.ToLower(key)]; ok {
		return canonical
	}
	return key
}

func deriveProblemIndex(platform string, contestID string, problem string, fallback int) string {
	problem = strings.TrimSpace(problem)
	contestID = strings.TrimSpace(contestID)
	if isContestPlatform(platform, spider.AtCoder) && contestID != "" {
		prefix := contestID + "_"
		if strings.HasPrefix(problem, prefix) {
			suffix := strings.TrimPrefix(problem, prefix)
			if atCoderTaskAlphabetRe.MatchString(suffix) {
				return strings.ToUpper(suffix)
			}
		}
	}
	if isContestPlatform(platform, spider.CodeForces) && contestID != "" {
		prefix := contestID + "-"
		if strings.HasPrefix(problem, prefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(problem, prefix))
			if rest != "" {
				return strings.Fields(rest)[0]
			}
		}
	}
	if fields := strings.Fields(problem); len(fields) > 0 && len([]rune(fields[0])) <= 4 {
		return fields[0]
	}
	return string(rune('A' + fallback%26))
}

func isContestPlatform(platform string, target string) bool {
	return strings.EqualFold(strings.TrimSpace(platform), strings.TrimSpace(target))
}

func naturalProblemOrder(leftIndex, rightIndex, leftKey, rightKey string) bool {
	leftIndex = strings.ToUpper(strings.TrimSpace(leftIndex))
	rightIndex = strings.ToUpper(strings.TrimSpace(rightIndex))
	if leftIndex != "" && rightIndex != "" && leftIndex != rightIndex {
		return leftIndex < rightIndex
	}
	return leftKey < rightKey
}

type contestProblemCalculation struct {
	resultsByUser map[int64][]*contest_log.ProblemResult
	penaltyByUser map[int64]int32
	problemStats  map[string]problemColumnStat
}

type problemColumnStat struct {
	contestAccepted  int
	contestAttempted int
	upsolveAccepted  int
}

type problemUserState struct {
	hasContestSubmit bool
	hasContestAC     bool
	contestMinute    int32
	wrongBeforeAC    int32
	contestWrong     int32
	upsolveAC        bool
	upsolveWrong     int32
}

func calculateContestProblemMatrix(start time.Time, end time.Time, hasReliableWindow bool, problems []contestProblemMeta, submissions []model.SubmitLog, pageUserIDs []int64, allUserIDs []int64) contestProblemCalculation {
	problemSet := map[string]struct{}{}
	for _, problem := range problems {
		problemSet[problem.Key] = struct{}{}
	}
	problemAliases := buildProblemAliasMap(problems)
	stateByUser := map[int64]map[string]*problemUserState{}
	ensureState := func(userID int64, problemKey string) *problemUserState {
		if stateByUser[userID] == nil {
			stateByUser[userID] = map[string]*problemUserState{}
		}
		if stateByUser[userID][problemKey] == nil {
			stateByUser[userID][problemKey] = &problemUserState{}
		}
		return stateByUser[userID][problemKey]
	}

	for _, sub := range submissions {
		key := strings.TrimSpace(sub.Problem)
		if key == "" {
			key = strings.TrimSpace(sub.SubmitID)
		}
		key = canonicalProblemKey(key, problemAliases)
		if _, ok := problemSet[key]; !ok {
			continue
		}
		state := ensureState(sub.UserID, key)
		accepted := dal.IsAcceptedStatus(sub.Status)
		inContest := true
		afterContest := false
		if hasReliableWindow {
			inContest = !sub.Time.Before(start) && sub.Time.Before(end)
			afterContest = !sub.Time.Before(end)
		}
		switch {
		case inContest:
			state.hasContestSubmit = true
			if accepted {
				if !state.hasContestAC {
					state.hasContestAC = true
					state.contestMinute = elapsedMinute(start, sub.Time)
				}
			} else if !state.hasContestAC {
				state.wrongBeforeAC++
				state.contestWrong++
			}
		case afterContest:
			if accepted && !state.hasContestAC && !state.upsolveAC {
				state.upsolveAC = true
			} else if !accepted && !state.hasContestAC && !state.upsolveAC {
				state.upsolveWrong++
			}
		}
	}

	stats := map[string]problemColumnStat{}
	penaltyByUser := map[int64]int32{}
	for _, userID := range allUserIDs {
		for _, problem := range problems {
			state := ensureState(userID, problem.Key)
			stat := stats[problem.Key]
			if state.hasContestSubmit {
				stat.contestAttempted++
			}
			if state.hasContestAC {
				stat.contestAccepted++
				penaltyByUser[userID] += state.contestMinute + state.wrongBeforeAC*20
			}
			if state.upsolveAC {
				stat.upsolveAccepted++
			}
			stats[problem.Key] = stat
		}
	}

	resultsByUser := map[int64][]*contest_log.ProblemResult{}
	for _, userID := range pageUserIDs {
		results := make([]*contest_log.ProblemResult, 0, len(problems))
		for _, problem := range problems {
			state := ensureState(userID, problem.Key)
			status := "none"
			switch {
			case state.hasContestAC:
				status = "contest_ac"
			case state.upsolveAC:
				status = "upsolve_ac"
			case state.hasContestSubmit:
				status = "contest_failed"
			}
			results = append(results, &contest_log.ProblemResult{
				ProblemKey:     problem.Key,
				Status:         status,
				AcceptedMinute: state.contestMinute,
				WrongBeforeAc:  state.wrongBeforeAC,
				WrongAttempts:  state.contestWrong + state.upsolveWrong,
				Upsolved:       state.upsolveAC,
			})
		}
		resultsByUser[userID] = results
	}
	return contestProblemCalculation{
		resultsByUser: resultsByUser,
		penaltyByUser: penaltyByUser,
		problemStats:  stats,
	}
}

func elapsedMinute(start time.Time, submitTime time.Time) int32 {
	if start.IsZero() || submitTime.Before(start) {
		return 0
	}
	return int32(submitTime.Sub(start).Minutes())
}

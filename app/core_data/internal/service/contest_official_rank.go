package service

import (
	"context"
	"cwxu-algo/app/core_data/internal/data/model"
	"cwxu-algo/app/core_data/internal/spider"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

var (
	atCoderUserHistoryCache     = newTTLCache[[]atCoderUserHistoryItem](6 * time.Hour)
	codeforcesOfficialRankCache = newTTLCache[map[string]int](6 * time.Hour)
)

type atCoderUserHistoryItem struct {
	Place             int    `json:"Place"`
	ContestScreenName string `json:"ContestScreenName"`
	ContestName       string `json:"ContestName"`
	EndTime           string `json:"EndTime"`
}

func (c ContestLogService) applyAtCoderOfficialRanks(ctx context.Context, contestID string, logs []model.ContestLog) {
	if contestID == "" || len(logs) == 0 {
		return
	}
	userIDs := collectContestUserIDs(logs)
	if len(userIDs) == 0 {
		return
	}

	var platforms []model.Platform
	if err := c.db.WithContext(ctx).
		Where("LOWER(platform) = LOWER(?) AND user_id IN ?", spider.AtCoder, userIDs).
		Find(&platforms).Error; err != nil {
		log.Errorf("load atcoder bindings failed: %v", err)
		return
	}
	usernameByUser := map[int64]string{}
	for _, item := range platforms {
		username := strings.TrimSpace(item.Username)
		if username != "" {
			usernameByUser[item.UserID] = username
		}
	}
	if len(usernameByUser) == 0 {
		return
	}

	rankByUser := map[int64]int{}
	client := &http.Client{Timeout: 8 * time.Second}
	for _, userID := range userIDs {
		username := usernameByUser[userID]
		if username == "" {
			continue
		}
		rank, ok := fetchAtCoderOfficialRank(client, contestID, username)
		if ok {
			rankByUser[userID] = rank
		}
	}
	if len(rankByUser) == 0 {
		return
	}
	for i := range logs {
		if rank, ok := rankByUser[logs[i].UserID]; ok && rank > 0 {
			logs[i].Rank = rank
		}
	}
}

func (c ContestLogService) applyCodeforcesOfficialRanks(ctx context.Context, contestID string, logs []model.ContestLog) {
	if contestID == "" || len(logs) == 0 {
		return
	}
	userIDs := collectContestUserIDs(logs)
	if len(userIDs) == 0 {
		return
	}

	var platforms []model.Platform
	if err := c.db.WithContext(ctx).
		Where("LOWER(platform) = LOWER(?) AND user_id IN ?", spider.CodeForces, userIDs).
		Find(&platforms).Error; err != nil {
		log.Errorf("load codeforces bindings failed: %v", err)
		return
	}
	handleByUser := map[int64]string{}
	for _, item := range platforms {
		handle := normalizeCodeforcesHandle(item.Username)
		if handle != "" {
			handleByUser[item.UserID] = handle
		}
	}
	if len(handleByUser) == 0 {
		return
	}

	rankByHandle, err := fetchCodeforcesOfficialRankMap(contestID)
	if err != nil {
		log.Errorf("fetch codeforces official rank map failed contest=%s: %v", contestID, err)
		return
	}
	if len(rankByHandle) == 0 {
		return
	}
	for i := range logs {
		handle := handleByUser[logs[i].UserID]
		if rank, ok := rankByHandle[handle]; ok && rank > 0 {
			logs[i].Rank = rank
		} else {
			// Codeforces official standings are the source of truth for contest
			// rank. If a bound handle is not in the official standings, the user
			// only has local/upsolve records and must not keep a stale crawled rank.
			logs[i].Rank = 0
		}
	}
}

func fetchCodeforcesOfficialRankMap(contestID string) (map[string]int, error) {
	cacheKey := strings.ToLower(strings.TrimSpace(contestID))
	if cached, ok := codeforcesOfficialRankCache.Get(cacheKey); ok {
		return cached, nil
	}
	standings, err := fetchCodeforcesStandings(contestID)
	if err != nil {
		return nil, err
	}
	rankByHandle := map[string]int{}
	for _, row := range standings.Result.Rows {
		if row.Rank <= 0 {
			continue
		}
		for _, member := range row.Party.Members {
			handle := normalizeCodeforcesHandle(member.Handle)
			if handle == "" {
				continue
			}
			if _, exists := rankByHandle[handle]; !exists {
				rankByHandle[handle] = row.Rank
			}
		}
	}
	codeforcesOfficialRankCache.SetWithTTL(cacheKey, rankByHandle, codeforcesStandingsTTL(standings))
	return rankByHandle, nil
}

func normalizeCodeforcesHandle(handle string) string {
	return strings.ToLower(strings.TrimSpace(handle))
}

func fetchAtCoderOfficialRank(client *http.Client, contestID string, username string) (int, bool) {
	history, err := fetchAtCoderUserHistory(client, username)
	if err != nil {
		log.Errorf("fetch atcoder user history failed user=%s: %v", username, err)
		return 0, false
	}
	for _, item := range history {
		if item.Place <= 0 {
			continue
		}
		if atCoderHistoryMatchesContest(contestID, item) {
			return item.Place, true
		}
	}
	return 0, false
}

func fetchAtCoderUserHistory(client *http.Client, username string) ([]atCoderUserHistoryItem, error) {
	cacheKey := strings.ToLower(strings.TrimSpace(username))
	if cached, ok := atCoderUserHistoryCache.Get(cacheKey); ok {
		return cached, nil
	}
	u := "https://atcoder.jp/users/" + url.PathEscape(username) + "/history/json"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "WUST-ACM-Tracker/1.1 (+https://github.com/WUSTACM/WUST-Algo-tracker)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("history status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var history []atCoderUserHistoryItem
	if err := json.Unmarshal(body, &history); err != nil {
		return nil, err
	}
	atCoderUserHistoryCache.Set(cacheKey, history)
	return history, nil
}

func atCoderHistoryMatchesContest(contestID string, item atCoderUserHistoryItem) bool {
	contestID = strings.ToLower(strings.TrimSpace(contestID))
	if contestID == "" {
		return false
	}
	screenName := strings.ToLower(strings.TrimSpace(item.ContestScreenName))
	return screenName == contestID+".contest.atcoder.jp" ||
		strings.HasPrefix(screenName, contestID+".")
}

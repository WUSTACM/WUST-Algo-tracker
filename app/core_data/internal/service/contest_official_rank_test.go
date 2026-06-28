package service

import (
	"cwxu-algo/app/core_data/internal/data/model"
	"testing"
)

func TestAtCoderHistoryMatchesContest(t *testing.T) {
	item := atCoderUserHistoryItem{
		Place:             42,
		ContestScreenName: "abc464.contest.atcoder.jp",
	}
	if !atCoderHistoryMatchesContest("abc464", item) {
		t.Fatal("expected abc464 history item to match contest id")
	}
	if atCoderHistoryMatchesContest("abc463", item) {
		t.Fatal("different contest id should not match")
	}
}

func TestSortContestLogsUsesOfficialRankFirst(t *testing.T) {
	logs := []model.ContestLog{
		{UserID: 1, Rank: 0, AcCount: 5},
		{UserID: 2, Rank: 106, AcCount: 4},
		{UserID: 3, Rank: 3, AcCount: 1},
	}
	sortContestLogs(logs)
	if logs[0].UserID != 3 || logs[1].UserID != 2 || logs[2].UserID != 1 {
		t.Fatalf("unexpected order: %+v", logs)
	}
}

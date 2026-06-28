package platform

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAtCoderFetchSubmitLogPaginatesAndDeduplicatesBoundary(t *testing.T) {
	originalBaseURL := atCoderAPIBaseURL
	originalPageSize := atCoderPageSize
	defer func() {
		atCoderAPIBaseURL = originalBaseURL
		atCoderPageSize = originalPageSize
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fromSecond := r.URL.Query().Get("from_second")
		rows := make([]atcJson, 0)
		switch fromSecond {
		case "0":
			for i := 1; i <= 500; i++ {
				rows = append(rows, atcJson{
					ID:          i,
					EpochSecond: int64(999 + i),
					ProblemID:   "abc_" + strconv.Itoa(i),
					ContestID:   "abc",
					UserID:      "tester",
					Language:    "C++",
					Result:      "AC",
				})
			}
		case "1499":
			rows = append(rows,
				atcJson{ID: 500, EpochSecond: 1499, ProblemID: "abc_500", ContestID: "abc", UserID: "tester", Language: "C++", Result: "AC"},
				atcJson{ID: 501, EpochSecond: 1500, ProblemID: "abc_501", ContestID: "abc", UserID: "tester", Language: "C++", Result: "WA"},
			)
		default:
			rows = []atcJson{}
		}
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer server.Close()

	atCoderAPIBaseURL = server.URL
	atCoderPageSize = 500

	logs, err := NewAtCoder{}.FetchSubmitLog(9, "tester", true)
	if err != nil {
		t.Fatalf("FetchSubmitLog returned error: %v", err)
	}
	if len(logs) != 501 {
		t.Fatalf("logs length = %d, want 501", len(logs))
	}
	if logs[500].SubmitID != "501" || logs[500].Status != "WA" {
		t.Fatalf("last log = (%s,%s), want (501,WA)", logs[500].SubmitID, logs[500].Status)
	}
}

func TestAtCoderFetchContestLogAggregatesFromSubmissions(t *testing.T) {
	originalBaseURL := atCoderAPIBaseURL
	originalContestBaseURL := atCoderContestBaseURL
	originalPageSize := atCoderPageSize
	defer func() {
		atCoderAPIBaseURL = originalBaseURL
		atCoderContestBaseURL = originalContestBaseURL
		atCoderPageSize = originalPageSize
		atCoderContestMetaMap = sync.Map{}
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/contests/") {
			contestID := strings.TrimPrefix(r.URL.Path, "/contests/")
			start := "2020-01-01 21:00:00+0900"
			end := "2020-01-01 22:40:00+0900"
			title := "AtCoder Beginner Contest 001"
			if contestID == "abc002" {
				start = "2020-01-08 21:00:00+0900"
				end = "2020-01-08 22:40:00+0900"
				title = "AtCoder Beginner Contest 002"
			}
			_, _ = w.Write([]byte(`<a class="contest-title" href="/contests/` + contestID + `">` + title + `</a><time class='fixtime fixtime-full'>` + start + `</time><time class='fixtime fixtime-full'>` + end + `</time>`))
			return
		}
		abc001Start := mustParseAtCoderTestTime(t, "2020-01-01 21:00:00+0900").Unix()
		abc001End := mustParseAtCoderTestTime(t, "2020-01-01 22:40:00+0900").Unix()
		abc002Start := mustParseAtCoderTestTime(t, "2020-01-08 21:00:00+0900").Unix()
		abc002End := mustParseAtCoderTestTime(t, "2020-01-08 22:40:00+0900").Unix()
		rows := []atcJson{
			{ID: 1, EpochSecond: abc001Start + 60, ProblemID: "abc001_a", ContestID: "abc001", UserID: "tester", Language: "C++", Result: "WA"},
			{ID: 2, EpochSecond: abc001Start + 120, ProblemID: "abc001_a", ContestID: "abc001", UserID: "tester", Language: "C++", Result: "AC"},
			{ID: 3, EpochSecond: abc001Start + 180, ProblemID: "abc001_b", ContestID: "abc001", UserID: "tester", Language: "C++", Result: "AC"},
			{ID: 4, EpochSecond: abc001End + 60, ProblemID: "abc001_c", ContestID: "abc001", UserID: "tester", Language: "C++", Result: "AC"},
			{ID: 5, EpochSecond: abc002Start + 60, ProblemID: "abc002_a", ContestID: "abc002", UserID: "tester", Language: "C++", Result: "WA"},
			{ID: 6, EpochSecond: abc002End + 60, ProblemID: "abc002_a", ContestID: "abc002", UserID: "tester", Language: "C++", Result: "AC"},
		}
		_ = json.NewEncoder(w).Encode(rows)
	}))
	defer server.Close()

	atCoderAPIBaseURL = server.URL
	atCoderContestBaseURL = server.URL + "/contests"
	atCoderPageSize = 500

	logs, err := NewAtCoder{}.FetchContestLog(9, "tester", true)
	if err != nil {
		t.Fatalf("FetchContestLog returned error: %v", err)
	}
	if len(logs) != 2 {
		t.Fatalf("contest logs length = %d, want 2", len(logs))
	}

	byID := map[string]struct {
		ac    int
		total int
	}{}
	for _, log := range logs {
		byID[log.ContestId] = struct {
			ac    int
			total int
		}{ac: log.AcCount, total: log.TotalCount}
	}
	if byID["abc001"].ac != 2 || byID["abc001"].total != 2 {
		t.Fatalf("abc001 aggregate = %+v, want ac=2 total=2", byID["abc001"])
	}
	if byID["abc002"].ac != 0 || byID["abc002"].total != 1 {
		t.Fatalf("abc002 aggregate = %+v, want ac=0 total=1", byID["abc002"])
	}
	for _, log := range logs {
		if log.ContestName == log.ContestId {
			t.Fatalf("contest name was not enriched: %+v", log)
		}
		if log.Time.Year() != 2020 {
			t.Fatalf("contest time = %v, want official 2020 start time", log.Time)
		}
	}
}

func mustParseAtCoderTestTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02 15:04:05-0700", value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}

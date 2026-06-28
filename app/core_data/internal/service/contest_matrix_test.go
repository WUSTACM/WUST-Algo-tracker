package service

import (
	"cwxu-algo/app/core_data/internal/data/model"
	"testing"
	"time"
)

func TestCalculateContestProblemMatrix(t *testing.T) {
	start := time.Date(2026, 6, 28, 20, 0, 0, 0, time.UTC)
	end := start.Add(100 * time.Minute)
	problems := []contestProblemMeta{
		{Key: "abc001_a", Index: "A", Name: "A"},
		{Key: "abc001_b", Index: "B", Name: "B"},
		{Key: "abc001_c", Index: "C", Name: "C"},
	}
	submissions := []model.SubmitLog{
		{UserID: 1, Problem: "abc001_a", Status: "WA", Time: start.Add(4 * time.Minute)},
		{UserID: 1, Problem: "abc001_a", Status: "AC", Time: start.Add(10 * time.Minute)},
		{UserID: 1, Problem: "abc001_b", Status: "WA", Time: start.Add(20 * time.Minute)},
		{UserID: 1, Problem: "abc001_b", Status: "WA", Time: start.Add(21 * time.Minute)},
		{UserID: 1, Problem: "abc001_b", Status: "AC", Time: end.Add(5 * time.Minute)},
		{UserID: 2, Problem: "abc001_a", Status: "AC", Time: start.Add(8 * time.Minute)},
		{UserID: 2, Problem: "abc001_c", Status: "WA", Time: start.Add(30 * time.Minute)},
	}

	got := calculateContestProblemMatrix(start, end, true, problems, submissions, []int64{1, 2}, []int64{1, 2})
	if got.penaltyByUser[1] != 30 {
		t.Fatalf("user 1 penalty = %d, want 30", got.penaltyByUser[1])
	}
	if got.penaltyByUser[2] != 8 {
		t.Fatalf("user 2 penalty = %d, want 8", got.penaltyByUser[2])
	}
	aStat := got.problemStats["abc001_a"]
	if aStat.contestAccepted != 2 || aStat.contestAttempted != 2 {
		t.Fatalf("A stat = %+v, want accepted=2 attempted=2", aStat)
	}
	bStat := got.problemStats["abc001_b"]
	if bStat.contestAccepted != 0 || bStat.contestAttempted != 1 || bStat.upsolveAccepted != 1 {
		t.Fatalf("B stat = %+v, want accepted=0 attempted=1 upsolve=1", bStat)
	}
	userOne := got.resultsByUser[1]
	if userOne[0].Status != "contest_ac" || userOne[0].AcceptedMinute != 10 || userOne[0].WrongBeforeAc != 1 {
		t.Fatalf("user 1 A result = %+v", userOne[0])
	}
	if userOne[1].Status != "upsolve_ac" || !userOne[1].Upsolved || userOne[1].WrongAttempts != 2 {
		t.Fatalf("user 1 B result = %+v", userOne[1])
	}
	userTwo := got.resultsByUser[2]
	if userTwo[2].Status != "contest_failed" || userTwo[2].WrongAttempts != 1 {
		t.Fatalf("user 2 C result = %+v", userTwo[2])
	}
}

func TestContestPlatformMatchingIgnoresCase(t *testing.T) {
	if !isContestPlatform("Codeforces", "CodeForces") {
		t.Fatal("Codeforces platform names should match case-insensitively")
	}
	if got := deriveProblemIndex("Codeforces", "2226", "2226-C Mental Monumental (Easy Version)", 0); got != "C" {
		t.Fatalf("deriveProblemIndex with legacy Codeforces casing = %q, want C", got)
	}
}

func TestProblemAliasesMatchSubmissionTitles(t *testing.T) {
	start := time.Date(2026, 6, 28, 20, 0, 0, 0, time.UTC)
	problems := []contestProblemMeta{
		{
			Key:     "A",
			Index:   "A",
			Name:    "签到题",
			Aliases: []string{"签到题"},
		},
	}
	submissions := []model.SubmitLog{
		{UserID: 1, Problem: "签到题", Status: "AC", Time: start.Add(3 * time.Minute)},
	}

	got := calculateContestProblemMatrix(start, start.Add(time.Hour), true, problems, submissions, []int64{1}, []int64{1})
	user := got.resultsByUser[1]
	if len(user) != 1 || user[0].Status != "contest_ac" || user[0].AcceptedMinute != 3 {
		t.Fatalf("alias result = %+v, want contest_ac at minute 3", user)
	}
}

func TestSynthesizeNowCoderProblemsKeepsTotalCount(t *testing.T) {
	problems := synthesizeNowCoderProblems("12345", 3, []model.SubmitLog{
		{Problem: "A 题目一"},
	})
	if len(problems) != 3 {
		t.Fatalf("len(problems) = %d, want 3", len(problems))
	}
	if problems[0].Key != "A 题目一" || problems[0].URL == "" {
		t.Fatalf("first problem = %+v, want submitted problem with URL", problems[0])
	}
	if problems[2].Index != "C" || problems[2].URL == "" {
		t.Fatalf("third problem = %+v, want synthesized C with URL", problems[2])
	}
}

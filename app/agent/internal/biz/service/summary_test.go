package service

import (
	"context"
	"errors"
	"testing"
	"time"

	profile "cwxu-algo/api/user/v1/profile"
	"cwxu-algo/app/agent/internal/agent/tool"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

func TestPersonalLastDay_CoachSkipsDailyReportBeforeMonday(t *testing.T) {
	var weeklyCalled bool
	uc := &SummaryUseCase{
		now: fixedTime(time.Date(2026, 4, 21, 9, 0, 0, 0, time.Local)),
		userProfileFn: func(userId int64) *profile.GetByIdRes {
			return &profile.GetByIdRes{RoleId: 2, EmailEnabled: true}
		},
		weeklyReportForCoachFn: func(coachUserId int64) error {
			weeklyCalled = true
			return nil
		},
	}

	if err := uc.PersonalLastDay(23); err != nil {
		t.Fatalf("PersonalLastDay() error = %v", err)
	}
	if weeklyCalled {
		t.Fatal("coach weekly report should not run before Monday")
	}
}

func TestPersonalLastDay_CoachRunsWeeklyReportOnMonday(t *testing.T) {
	var gotCoachUserId int64
	uc := &SummaryUseCase{
		now: fixedTime(time.Date(2026, 4, 20, 9, 0, 0, 0, time.Local)),
		userProfileFn: func(userId int64) *profile.GetByIdRes {
			return &profile.GetByIdRes{RoleId: 2, EmailEnabled: true}
		},
		weeklyReportForCoachFn: func(coachUserId int64) error {
			gotCoachUserId = coachUserId
			return nil
		},
	}

	if err := uc.PersonalLastDay(23); err != nil {
		t.Fatalf("PersonalLastDay() error = %v", err)
	}
	if gotCoachUserId != 23 {
		t.Fatalf("weekly report user id = %d, want 23", gotCoachUserId)
	}
}

func TestPersonalLastDay_ReturnsWeeklyReportError(t *testing.T) {
	wantErr := errors.New("weekly report failed")
	uc := &SummaryUseCase{
		now: fixedTime(time.Date(2026, 4, 20, 9, 0, 0, 0, time.Local)),
		userProfileFn: func(userId int64) *profile.GetByIdRes {
			return &profile.GetByIdRes{RoleId: 2, EmailEnabled: true}
		},
		weeklyReportForCoachFn: func(coachUserId int64) error {
			return wantErr
		},
	}

	if err := uc.PersonalLastDay(23); !errors.Is(err, wantErr) {
		t.Fatalf("PersonalLastDay() error = %v, want %v", err, wantErr)
	}
}

func TestPersonalLastDay_SkipsWhenEmailDisabled(t *testing.T) {
	var weeklyCalled bool
	uc := &SummaryUseCase{
		now: fixedTime(time.Date(2026, 4, 20, 9, 0, 0, 0, time.Local)),
		userProfileFn: func(userId int64) *profile.GetByIdRes {
			return &profile.GetByIdRes{RoleId: 2, EmailEnabled: false}
		},
		weeklyReportForCoachFn: func(coachUserId int64) error {
			weeklyCalled = true
			return nil
		},
	}

	if err := uc.PersonalLastDay(23); err != nil {
		t.Fatalf("PersonalLastDay() error = %v", err)
	}
	if weeklyCalled {
		t.Fatal("weekly report should not run when email is disabled")
	}
}

func TestPersonalRecentRunsWhenEmailDisabled(t *testing.T) {
	var profileCalled bool
	var chatCalled bool
	var gotKey string
	var gotValue string
	uc := &SummaryUseCase{
		userProfileFn: func(userId int64) *profile.GetByIdRes {
			profileCalled = true
			return &profile.GetByIdRes{RoleId: 0, EmailEnabled: false}
		},
		chatFn: func(messages []*model.ChatCompletionMessage, tools ...tool.AgentToolFactory) (string, error) {
			chatCalled = true
			return `{"msg":["继续加油"],"updateTime":1}`, nil
		},
		redisExistsFn: func(ctx context.Context, key string) (int64, error) {
			return 0, nil
		},
		redisSetFn: func(ctx context.Context, key string, value string) error {
			gotKey = key
			gotValue = value
			return nil
		},
	}

	if err := uc.PersonalRecent(23); err != nil {
		t.Fatalf("PersonalRecent() error = %v", err)
	}
	if profileCalled {
		t.Fatal("PersonalRecent() should not check the email preference")
	}
	if !chatCalled {
		t.Fatal("PersonalRecent() should generate the page summary")
	}
	if gotKey != "agent:summary:23:recent" {
		t.Fatalf("redis key = %q, want agent:summary:23:recent", gotKey)
	}
	if gotValue == "" {
		t.Fatal("redis value should not be empty")
	}
}

func TestWeeklyReportDateRangeUsesSevenCalendarDays(t *testing.T) {
	now := time.Date(2026, 4, 20, 9, 0, 0, 0, time.Local)
	lastWeekStart := now.AddDate(0, 0, -7).Format("20060102")
	lastWeekEnd := now.AddDate(0, 0, -1).Format("20060102")

	startDate, err := time.Parse("20060102", lastWeekStart)
	if err != nil {
		t.Fatalf("parse start date: %v", err)
	}
	endDate, err := time.Parse("20060102", lastWeekEnd)
	if err != nil {
		t.Fatalf("parse end date: %v", err)
	}
	if daysDiff := endDate.Sub(startDate).Hours() / 24; daysDiff != 6 {
		t.Fatalf("weekly range day difference = %v, want 6", daysDiff)
	}
}

func fixedTime(t time.Time) func() time.Time {
	return func() time.Time {
		return t
	}
}

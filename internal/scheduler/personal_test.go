package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

type personalFixture struct {
	enabled  bool
	accounts []model.Account
}

func (f *personalFixture) Config() model.Config                         { return model.Config{Enabled: f.enabled} }
func (f *personalFixture) AccountsForUser(string, bool) []model.Account { return f.accounts }
func (f *personalFixture) AccountRunnable(id string) (model.Account, bool) {
	for _, a := range f.accounts {
		if a.ID == id {
			return a, !a.Disabled
		}
	}
	return model.Account{}, false
}
func (f *personalFixture) AddLogForUser(string, string, string) error { return nil }

func TestPersonalSchedulesWorkWithoutFallbackAndDoNotReplayMissedMinutes(t *testing.T) {
	now := time.Date(2026, 9, 11, 1, 30, 10, 0, time.UTC)
	f := &personalFixture{enabled: true, accounts: []model.Account{
		{ID: "due", TaskSettings: &model.AccountTaskSettings{Automatic: true, Schedule: &model.AccountSchedule{Time: "09:30", Timezone: "Asia/Shanghai"}}},
		{ID: "later", TaskSettings: &model.AccountTaskSettings{Automatic: true, Schedule: &model.AccountSchedule{Time: "09:31", Timezone: "Asia/Shanghai"}}},
		{ID: "fallback", TaskSettings: &model.AccountTaskSettings{Automatic: true}},
	}}
	for i := range f.accounts {
		f.accounts[i].TaskSettings.Features.GameCheckin = true
		f.accounts[i].TaskSettings.Games.Enabled = []string{"genshin"}
	}
	f.accounts = append(f.accounts, model.Account{ID: "empty", TaskSettings: &model.AccountTaskSettings{Automatic: true, Schedule: &model.AccountSchedule{Time: "09:30", Timezone: "Asia/Shanghai"}}})
	var calls []string
	p := NewPersonal(f, func(ctx context.Context, id string) error { calls = append(calls, id); return nil })
	p.tick(context.Background(), now)
	p.tick(context.Background(), now.Add(10*time.Second))
	if len(calls) != 1 || calls[0] != "due" {
		t.Fatal("personal schedule duplicated or used fallback", calls)
	}
	p.tick(context.Background(), now.Add(time.Minute))
	if len(calls) != 2 || calls[1] != "later" {
		t.Fatal("personal time not used", calls)
	}
	f.enabled = false
	p.tick(context.Background(), now.AddDate(0, 0, 1))
	if len(calls) != 2 {
		t.Fatal("site pause ignored")
	}
	f.enabled = true
	p.tick(context.Background(), now.Add(10*time.Minute))
	if len(calls) != 2 {
		t.Fatal("missed schedules replayed")
	}
}

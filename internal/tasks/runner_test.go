package tasks

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
	"github.com/eleost04/miyohub/internal/store"
)

func TestRunnerReservesAccountsAndStops(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "two"} {
		if err := s.AddAccountForUser(u.ID, model.Account{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	accounts := s.AccountsForUser(u.ID, false)
	r := NewRunner(s)
	if _, _, _, err := r.begin(context.Background(), []string{}); err == nil {
		t.Fatal("empty selection reserved all accounts")
	}
	ctx, a, cancel, err := r.begin(context.Background(), []string{accounts[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := r.begin(context.Background(), []string{accounts[0].ID}); err == nil {
		t.Fatal("duplicate run accepted")
	}
	_, b, cancelB, err := r.begin(context.Background(), []string{accounts[1].ID})
	if err != nil {
		t.Fatal("unrelated account blocked", err)
	}
	r.end(b, cancelB)
	stopped := make(chan struct{})
	go func() { r.Stop(); close(stopped) }()
	<-ctx.Done()
	r.end(a, cancel)
	<-stopped
	if r.Running() {
		t.Fatal("runner still running after stop")
	}
	if _, _, _, err := r.begin(context.Background(), nil); err == nil {
		t.Fatal("run accepted after stop")
	}
}

func TestRunnerAccountCancellationAndGenerationSafeCleanup(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one", "two"} {
		if err := s.AddAccountForUser(u.ID, model.Account{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	r := NewRunner(s)
	defer r.Stop()
	_, accounts, cleanup, err := r.begin(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	first, second := r.contexts[accounts[0].ID], r.contexts[accounts[1].ID]
	r.CancelAccount(accounts[0].ID)
	if first.Err() == nil || second.Err() != nil {
		r.end(accounts, cleanup)
		t.Fatal("cancelling one account affected the whole batch")
	}
	if len(r.ProgressForUser(u)) != 2 || len(r.ProgressForUser(model.User{ID: "foreign", Role: "user"})) != 0 {
		r.end(accounts, cleanup)
		t.Fatal("progress is not scoped to authorized accounts")
	}
	r.releaseAccount(accounts[0].ID, first)
	_, newAccounts, newCleanup, err := r.begin(context.Background(), []string{accounts[0].ID})
	if err != nil {
		r.end(accounts, cleanup)
		t.Fatal("completed account could not start another run", err)
	}
	defer r.end(newAccounts, newCleanup)
	newContext := r.contexts[accounts[0].ID]
	r.end(accounts, cleanup)
	if newContext.Err() != nil || !r.Running() || len(r.ProgressForUser(u)) != 1 {
		t.Fatal("old batch cleanup removed a newer account reservation")
	}
}

func TestRunnerRenewsCookieAndPersistsDetailedFailure(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "main", Cookie: "stuid=100;stoken=st;mid=mid;cookie_token=old"}); err != nil {
		t.Fatal(err)
	}
	account := s.AccountsForUser(u.ID, false)[0]
	cfg := s.Config()
	cfg.Features = model.Features{GameCheckin: true}
	cfg.Games.Enabled = []string{"genshin", "starrail"}
	cfg.BBS.DelaySeconds = []int{0, 0}
	if err := s.ReplaceConfig(cfg); err != nil {
		t.Fatal(err)
	}
	renewals, roleCalls := 0, 0
	r := NewRunner(s)
	defer r.Stop()
	notifications := 0
	r.Notify = func(event notify.Event) bool {
		notifications++
		persisted, _ := s.AccountForUser(u.ID, false, account.ID)
		if persisted.LastTaskAt.IsZero() || persisted.TaskResults["games"].Failed != 1 {
			t.Error("notification ran before task results were saved")
		}
		if event.UserID != u.ID || event.AccountID != account.ID || event.Kind != notify.TaskEventKind || event.Success {
			t.Error("incorrect task notification ownership or result")
		}
		if strings.Contains(event.Message, "cookie_token=renewed") {
			t.Error("notification included renewed credentials")
		}
		return false // An unavailable outbox must not undo task results.
	}
	r.newClient = func() *mihoyo.Client {
		client := mihoyo.NewClient("")
		client.HTTP.Transport = roundTrip(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case "/auth/api/getCookieAccountInfoBySToken":
				renewals++
				return reply(`{"retcode":0,"data":{"cookie_token":"renewed"}}`), nil
			case mihoyo.AccountRolesPath:
				roleCalls++
				if !strings.Contains(request.Header.Get("Cookie"), "cookie_token=renewed") {
					t.Error("task did not use renewed cookie")
				}
				return reply(`{"retcode":-100,"message":"expired"}`), nil
			default:
				t.Error("unexpected request after expired credentials", request.URL.Path)
				return nil, errors.New("unexpected request")
			}
		})
		return client
	}
	if err := r.Run(context.Background(), []string{account.ID}); err == nil {
		t.Fatal("failed tasks were reported as a successful run")
	}
	got, _ := s.AccountForUser(u.ID, false, account.ID)
	result := got.TaskResults["games"]
	if renewals != 1 || roleCalls != 1 || notifications != 1 || result.Failed != 1 || len(result.Details) == 0 || got.LastTaskAt.IsZero() || got.Status != "expired" {
		t.Fatalf("task results were not persisted: %+v; requests %d/%d", result, renewals, roleCalls)
	}
	if r.Running() || len(r.ProgressForUser(u)) != 0 {
		t.Fatal("completed task left a stale reservation")
	}
}

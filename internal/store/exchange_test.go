package store

import (
	"github.com/eleost04/miyohub/internal/model"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExchangeClaimIsAtomicAndSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	u, _, _ := s.CreateAdmin("admin", "password123")
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(u.ID, false)[0]
	p, err := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "g", Enabled: true, State: "success", LastRun: "forged"})
	if err != nil {
		t.Fatal(err)
	}
	if p.State != "pending" || p.LastRun != "" {
		t.Fatal("accepted server-owned fields")
	}
	if _, _, err := s.ClaimExchangePlan("other", false, p.ID, false); err == nil {
		t.Fatal("foreign user claimed plan")
	}
	var count atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := s.ClaimExchangePlan(u.ID, false, p.ID, false); err == nil {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal("duplicate claim", count.Load())
	}
	if _, err := s.UpdateExchangePlan(u.ID, false, p); err == nil {
		t.Fatal("edited running plan")
	}
	if err := s.DeleteExchangePlan(u.ID, false, p.ID); err == nil {
		t.Fatal("deleted running plan")
	}
	reloaded, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	recovered, _ := reloaded.ExchangePlanForUser(u.ID, false, p.ID)
	if recovered.State != "unknown" {
		t.Fatal("interrupted request not recovered")
	}
	if _, _, err := reloaded.ClaimExchangePlan(u.ID, false, p.ID, false); err == nil {
		t.Fatal("replayed interrupted exchange")
	}
	if err := s.FinishExchange(p.ID, "wrong-key", "success", 1, "forged"); err == nil {
		t.Fatal("accepted incorrect attempt key")
	}
}
func TestExchangeSkipsOldSchedulesAndDisabledUsers(t *testing.T) {
	s, _ := New(filepath.Join(t.TempDir(), "state.json"))
	u, _, _ := s.CreateAdmin("admin", "password123")
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "a"})
	a := s.AccountsForUser(u.ID, false)[0]
	p, _ := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "g", Enabled: true, Auto: true, ExchangeAt: time.Now().Add(time.Hour).Unix()})
	cfg := s.Config()
	cfg.Shop.Plans[0].ExchangeAt = time.Now().Add(-2 * time.Minute).Unix()
	_ = s.ReplaceConfig(cfg)
	if _, _, err := s.ClaimExchangePlan(u.ID, false, p.ID, true); err == nil {
		t.Fatal("replayed expired schedule")
	}
	got, _ := s.ExchangePlanForUser(u.ID, false, p.ID)
	if got.State != "missed" {
		t.Fatal("old plan not marked missed")
	}
}

func TestDifferentGoodsCanPrepareTogetherButDuplicateGoodCannot(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, _ := s.CreateAdmin("admin", "password123")
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "main"}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(u.ID, false)[0]
	for i, good := range []string{"one", "two", "one"} {
		p, err := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: good, Enabled: true, Auto: true, ExchangeAt: time.Now().Add(time.Minute).Unix()})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = s.ClaimExchangePlanAt(u.ID, false, p.ID, true, time.Now(), 3*time.Minute)
		if (i == 2) != (err != nil) {
			t.Fatalf("incorrect claim for good %s: %v", good, err)
		}
	}
}

func TestExchangeRecoversOnlyUnsentScheduledPreparation(t *testing.T) {
	for _, tc := range []struct {
		name, phase, want string
		auto              bool
		attempt           int
	}{
		{"preparing", "preparing", "pending", true, 0},
		{"waiting", "waiting", "pending", true, 0},
		{"sent", "exchanging", "unknown", true, 1},
		{"legacy", "", "unknown", true, 0},
		{"manual", "preparing", "unknown", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := model.State{Config: model.Config{Shop: model.ShopConfig{Plans: []model.ExchangePlan{{State: "running", Phase: tc.phase, Auto: tc.auto, Attempt: tc.attempt}}}}}
			recoverExchangePlans(&state)
			p := state.Config.Shop.Plans[0]
			if p.State != tc.want || p.Phase != "" || p.Attempt != tc.attempt {
				t.Fatalf("unsafe recovery: %+v", p)
			}
		})
	}
}

func TestSentExchangeCannotBeResetToPending(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "main"}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(u.ID, false)[0]
	p, err := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "g", Enabled: true, Auto: true, ExchangeAt: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err = s.ClaimExchangePlanAt(u.ID, false, p.ID, true, time.Now(), 3*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ExchangeProgress(p.ID, p.AttemptKey, 1, "sending"); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []int{0, 1} {
		if err := s.FinishExchange(p.ID, p.AttemptKey, "pending", attempt, "retry"); err == nil {
			t.Fatal("a sent exchange was reset for replay")
		}
	}
	got, _ := s.ExchangePlanForUser(u.ID, false, p.ID)
	if got.State != "running" || got.Attempt != 1 {
		t.Fatal("rejected reset still changed the plan")
	}
	if err := s.FinishExchange(p.ID, p.AttemptKey, "unknown", 1, "review upstream history"); err != nil {
		t.Fatal(err)
	}
}

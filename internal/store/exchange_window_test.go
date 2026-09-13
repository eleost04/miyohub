package store

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func TestExchangeClaimUsesConfiguredWindowInsteadOfFixedGrace(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := s.CreateAdmin("admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "fixture"}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(u.ID, false)[0]
	opening := time.Now().Add(time.Hour).Truncate(time.Second)
	for index, tc := range []struct {
		window, elapsed float64
		allowed         bool
	}{{120, 70, true}, {120, 120, false}, {10, 10, false}, {0, 30, true}, {0, 60, false}} {
		cfg := s.Config()
		cfg.Shop.RetrySeconds = tc.window
		if err := s.ReplaceConfig(cfg); err != nil {
			t.Fatal(err)
		}
		p, err := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: fmt.Sprint(index), Enabled: true, Auto: true, ExchangeAt: opening.Unix()})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = s.ClaimExchangePlanAt(u.ID, false, p.ID, true, opening.Add(time.Duration(tc.elapsed*float64(time.Second))), 0)
		if (err == nil) != tc.allowed {
			t.Fatal("claim did not follow the configured window", tc, err)
		}
		got, _ := s.ExchangePlanForUser(u.ID, false, p.ID)
		if !tc.allowed && (got.State != "missed" || got.Attempt != 0) {
			t.Fatal("missed window lost its no-request state", got.State, got.Attempt)
		}
	}
}

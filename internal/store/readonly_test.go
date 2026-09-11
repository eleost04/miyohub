package store

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

func TestReadOnlyDoesNotRewriteOrRecoverInFlightPlans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "main", Cookie: "private-cookie"}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(u.ID, false)[0]
	p, err := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "good", Enabled: true, Auto: true, ExchangeAt: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClaimExchangePlanAt(u.ID, false, p.ID, true, time.Now(), 3*time.Minute); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	read, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := read.ExchangePlanForUser(u.ID, false, p.ID); got.State != "running" {
		t.Fatal("read-only access recovered an in-flight plan")
	}
	if err := read.AddLog("test", "must not be written"); err == nil {
		t.Fatal("read-only store accepted a write")
	}
	if len(read.LogsForUser("", true)) != len(s.LogsForUser("", true)) {
		t.Fatal("rejected write mutated the read-only snapshot")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("read-only access rewrote state")
	}
}

func TestReadOnlyNeverInitializesOrMigrates(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "state.json")
	if _, err := OpenReadOnly(path); err == nil {
		t.Fatal("missing state was initialized")
	}
	plain := []byte(`{"config":{"enabled":false},"users":[]}`)
	if err := os.WriteFile(path, plain, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenReadOnly(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".key"); !os.IsNotExist(err) {
		t.Fatal("read-only summary created a key")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(plain, after) {
		t.Fatal("read-only access migrated a plaintext state")
	}
}

package shop

import (
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestEngineRevocationDuringPreparationPreventsEveryExchangeRequest(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, _, _ := s.CreateAdmin("admin", "test-password")
	u, _ := s.CreateUser("member", "test-password", "user")
	if err := s.UpdateUserPermissions(admin, u.ID, model.UserPermissions{Exchange: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "test", Cookie: "test-only"}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(u.ID, false)[0]
	p, err := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "g", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	client := mihoyo.NewClient("")
	client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Path != mihoyo.MallDetailPath {
			t.Error("revocation did not stop subsequent requests", r.URL.Path)
			return nil, errors.New("unexpected request")
		}
		if err := s.UpdateUserPermissions(admin, u.ID, model.UserPermissions{}); err != nil {
			t.Error(err)
		}
		return response(`{"retcode":0,"data":{"goods_id":"g","goods_name":"gift","type":2,"price":10,"total":1,"status":"online"}}`), nil
	})
	e := NewEngine(s, client)
	defer e.Stop()
	if _, err := e.Run(u.ID, false, p.ID, false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := s.ExchangePlanForUser(u.ID, false, p.ID)
		if got.State != "running" {
			if got.Attempt != 0 || got.State == "success" || calls.Load() != 1 {
				t.Fatal("revoked run sent an exchange or lost attempt safety")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("revoked run did not finish")
}

func TestSchedulerDoesNotDispatchPlansWhoseOwnerLostPermission(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, _, _ := s.CreateAdmin("admin", "test-password")
	u, _ := s.CreateUser("member", "test-password", "user")
	if err := s.UpdateUserPermissions(admin, u.ID, model.UserPermissions{Exchange: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "test", Cookie: "test-only"}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(u.ID, false)[0]
	p, err := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "g", Enabled: true, Auto: true, ExchangeAt: time.Now().Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUserPermissions(admin, u.ID, model.UserPermissions{}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	client := mihoyo.NewClient("")
	client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected network request")
	})
	e := NewEngine(s, client)
	e.Start()
	time.Sleep(40 * time.Millisecond)
	e.Stop()
	got, _ := s.ExchangePlanForUser(u.ID, false, p.ID)
	if got.State != "pending" || calls.Load() != 0 || e.Status(u).NextRun != 0 || e.Status(u).Enabled {
		t.Fatal("scheduler or status ignored owner permissions")
	}
}

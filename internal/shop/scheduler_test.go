package shop

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
	"github.com/eleost04/miyohub/internal/store"
)

func TestConcurrentPreparationSerializesOnlyActualRequests(t *testing.T) {
	s, u, a, first := scheduledFixture(t, 3*time.Second)
	second, err := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "second", Enabled: true, Auto: true, ExchangeAt: first.ExchangeAt})
	if err != nil {
		t.Fatal(err)
	}
	client := mihoyo.NewClient("")
	e := NewEngine(s, client)
	defer e.Stop()
	started := make(chan struct{}, 2)
	releaseFirst := make(chan struct{})
	var active, requests atomic.Int32
	client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == mihoyo.MallDetailPath && r.Header.Get("Cookie") == "" {
			reply := response(`{"retcode":0}`)
			reply.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
			return reply, nil
		}
		switch r.URL.Path {
		case mihoyo.MallGoodsPath:
			reply := response(`{"retcode":0}`)
			reply.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
			return reply, nil
		case mihoyo.MallDetailPath:
			return response(`{"retcode":0,"data":{"goods_id":"` + r.URL.Query().Get("goods_id") + `","type":2,"price":10,"total":2,"status":"online"}}`), nil
		case mihoyo.MallPointPath:
			return response(`{"retcode":0,"data":{"points":100}}`), nil
		case "/mall/v1/web/goods/exchange":
			if active.Add(1) != 1 {
				t.Error("same-account requests overlapped")
			}
			defer active.Add(-1)
			if e.clock.Now().Before(time.Unix(first.ExchangeAt, 0)) {
				t.Error("early request")
			}
			n := requests.Add(1)
			started <- struct{}{}
			if n == 1 {
				select {
				case <-releaseFirst:
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
			}
			return response(`{"retcode":0}`), nil
		default:
			return nil, errors.New("unexpected path")
		}
	})
	e.Start()
	waitForPlan(t, s, first, func(p model.ExchangePlan) bool { return p.Phase == "waiting" })
	waitForPlan(t, s, second, func(p model.ExchangePlan) bool { return p.Phase == "waiting" })
	if requests.Load() != 0 {
		t.Fatal("plans were not both ready before opening")
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("exchange never started")
	}
	if requests.Load() != 1 {
		t.Fatal("waiting request bypassed gate")
	}
	close(releaseFirst)
	waitForPlan(t, s, first, func(p model.ExchangePlan) bool { return p.State == "success" })
	waitForPlan(t, s, second, func(p model.ExchangePlan) bool { return p.State == "success" })
	if requests.Load() != 2 {
		t.Fatal("a different good was starved")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.requestGate(a.ID)(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled waiter acquired gate", err)
	}
}

func scheduledFixture(t *testing.T, delay time.Duration) (*store.Store, model.User, model.Account, model.ExchangePlan) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "main", Cookie: "test-cookie"}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(u.ID, false)[0]
	if err := s.SetAccountDeviceFP(a.ID, a.Device.ID, "fp"); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "g", GoodsName: "测试兑换", Price: 10, Enabled: true, Auto: true, ExchangeAt: time.Now().Add(delay).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return s, u, a, p
}

func waitForPlan(t *testing.T, s *store.Store, p model.ExchangePlan, predicate func(model.ExchangePlan) bool) model.ExchangePlan {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		got, ok := s.ExchangePlanForUser("", true, p.ID)
		if ok && predicate(got) {
			return got
		}
		select {
		case <-deadline.C:
			t.Fatalf("plan did not reach expected state: %+v", got)
		case <-ticker.C:
		}
	}
}

func TestEnginePreparesAndWaitsBeforeScheduledExchange(t *testing.T) {
	s, u, _, p := scheduledFixture(t, 3*time.Second)
	client := mihoyo.NewClient("")
	e := NewEngine(s, client)
	defer e.Stop()
	var exchanged, synchronized atomic.Int32
	var notifications atomic.Int32
	e.Notify = func(event notify.Event) bool {
		notifications.Add(1)
		persisted, _ := s.ExchangePlanForUser(u.ID, false, p.ID)
		if persisted.State != "success" || !event.Success || event.UserID != u.ID || event.AccountID != p.AccountID || event.Kind != notify.ExchangeEventKind {
			t.Error("scheduled success did not notify its owner after persistence")
		}
		return true
	}
	client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == mihoyo.MallDetailPath && r.Header.Get("Cookie") == "" {
			synchronized.Add(1)
			if r.Header.Get("x-rpc-device_id") != "" {
				t.Error("clock request leaked account device")
			}
			reply := response(`{"retcode":0}`)
			reply.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
			return reply, nil
		}
		switch r.URL.Path {
		case mihoyo.MallGoodsPath:
			synchronized.Add(1)
			if r.Header.Get("Cookie") != "" || r.Header.Get("x-rpc-device_id") != "" {
				t.Error("clock synchronization leaked account credentials")
			}
			reply := response(`{"retcode":0,"data":{"list":[]}}`)
			reply.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
			return reply, nil
		case mihoyo.MallDetailPath:
			return response(`{"retcode":0,"data":{"goods_id":"g","goods_name":"gift","type":2,"price":10,"total":1,"status":"online"}}`), nil
		case mihoyo.MallPointPath:
			return response(`{"retcode":0,"data":{"points":100}}`), nil
		case "/mall/v1/web/goods/exchange":
			exchanged.Add(1)
			if e.clock.Now().Before(time.Unix(p.ExchangeAt, 0)) {
				t.Error("sent an exchange before its scheduled time")
			}
			return response(`{"retcode":0,"data":{}}`), nil
		default:
			t.Error("unexpected request", r.URL.Path)
			return nil, errors.New("unexpected request")
		}
	})
	e.Start()
	waiting := waitForPlan(t, s, p, func(p model.ExchangePlan) bool { return p.Phase == "waiting" })
	if waiting.State != "running" || waiting.Attempt != 0 || exchanged.Load() != 0 || !e.clock.Now().Before(time.Unix(p.ExchangeAt, 0)) {
		t.Fatal("scheduled preparation did not finish before exchange time")
	}
	status := e.Status(u)
	if status.Running != 1 || status.NextRun != p.ExchangeAt || status.Clock.SyncedAt.IsZero() {
		t.Fatal("preparation status is missing", status)
	}
	foreign := e.Status(model.User{ID: "foreign", Role: "user"})
	if foreign.Running != 0 || foreign.NextRun != 0 {
		t.Fatal("exchange status leaked another user's plans")
	}
	done := waitForPlan(t, s, p, func(p model.ExchangePlan) bool { return p.State == "success" })
	e.Stop()
	if done.Attempt != 1 || done.Phase != "" || exchanged.Load() != 1 || synchronized.Load() != 1 || notifications.Load() != 1 {
		t.Fatalf("incorrect execution result: %+v", done)
	}
}

func TestEngineShutdownOnlyRestoresUncancelledUnsentSchedules(t *testing.T) {
	for _, manualCancel := range []bool{false, true} {
		name := "shutdown"
		if manualCancel {
			name = "cancel then shutdown"
		}
		t.Run(name, func(t *testing.T) {
			s, u, _, p := scheduledFixture(t, time.Minute)
			started := make(chan struct{})
			client := mihoyo.NewClient("")
			client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == mihoyo.MallGoodsPath || r.URL.Path == mihoyo.MallDetailPath && r.Header.Get("Cookie") == "" {
					reply := response(`{"retcode":0}`)
					reply.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
					return reply, nil
				}
				if r.URL.Path == mihoyo.MallDetailPath {
					close(started)
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				t.Error("cancelled preparation sent a request", r.URL.Path)
				return nil, errors.New("unexpected request")
			})
			e := NewEngine(s, client)
			defer e.Stop()
			var notifications atomic.Int32
			e.Notify = func(event notify.Event) bool {
				notifications.Add(1)
				if !manualCancel || event.Success {
					t.Error("resumable shutdown emitted a final notification")
				}
				return true
			}
			if _, err := e.Run(u.ID, false, p.ID, true); err != nil {
				t.Fatal(err)
			}
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("preparation did not start")
			}
			if manualCancel {
				e.Cancel(p.ID)
			}
			e.Stop()
			got, _ := s.ExchangePlanForUser(u.ID, false, p.ID)
			want := "pending"
			if manualCancel {
				want = "cancelled"
			}
			if got.State != want || got.Attempt != 0 {
				t.Fatalf("incorrect restart state: %+v, want %s", got, want)
			}
			if manualCancel && notifications.Load() != 1 || !manualCancel && notifications.Load() != 0 {
				t.Fatal("cancel/shutdown notification count was incorrect")
			}
		})
	}
}

func TestPrepareRejectsChangedPriceBeforeAnyAccountOperation(t *testing.T) {
	client := mihoyo.NewClient("")
	calls := 0
	client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != mihoyo.MallDetailPath {
			t.Error("price mismatch proceeded to account operations", r.URL.Path)
		}
		return response(`{"retcode":0,"data":{"goods_id":"g","type":2,"price":200,"total":1,"status":"online"}}`), nil
	})
	_, err := (Service{Client: client, Account: model.Account{Cookie: "test-cookie"}}).Prepare(t.Context(), model.ExchangePlan{GoodsID: "g", Price: 100}, false)
	if err == nil || calls != 1 {
		t.Fatal("changed price was accepted", err, calls)
	}
}

func TestMissedPlanCannotBeReplayedOrReachNetwork(t *testing.T) {
	s, u, _, p := scheduledFixture(t, time.Minute)
	cfg := s.Config()
	cfg.Shop.Plans[0].ExchangeAt = time.Now().Add(-2 * time.Minute).Unix()
	if err := s.ReplaceConfig(cfg); err != nil {
		t.Fatal(err)
	}
	client := mihoyo.NewClient("")
	client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
		t.Error("missed plan reached network")
		return nil, errors.New("unexpected")
	})
	e := NewEngine(s, client)
	defer e.Stop()
	for i := 0; i < 2; i++ {
		if _, err := e.Run(u.ID, false, p.ID, true); err == nil {
			t.Fatal("missed plan was claimed")
		}
	}
	if stored, _ := s.ExchangePlanForUser(u.ID, false, p.ID); stored.State != "missed" || stored.Attempt != 0 {
		t.Fatal("missed plan was not persisted without attempts", stored.State, stored.Attempt)
	}
}

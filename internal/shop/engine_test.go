package shop

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
	"github.com/eleost04/miyohub/internal/store"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func TestExchangeDoesNotRetryUncertainOrPermanentFailures(t *testing.T) {
	for _, body := range []string{`{"data":{}}`, `{"retcode":"bad"}`, `{"retcode":-100,"message":"expired"}`, `{"retcode":-1,"message":"米游币不足"}`, "network"} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			client := mihoyo.NewClient("")
			client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if body == "network" {
					return nil, io.ErrUnexpectedEOF
				}
				return response(body), nil
			})
			service := Service{Client: client, Config: model.Config{Shop: model.ShopConfig{RetrySeconds: 2, RetryInterval: 0.05}}}
			result, err := service.ExchangeWithRetry(context.Background(), model.ExchangePlan{GoodsID: "g", DeviceFP: "fp"}, nil)
			if calls != 1 {
				t.Fatalf("unsafe retry %d", calls)
			}
			if body == "network" || strings.Contains(body, "bad") || body == `{"data":{}}` {
				if !errors.Is(err, ErrUncertain) {
					t.Fatal("expected uncertain result", err)
				}
			} else if err != nil || result["ok"] == true {
				t.Fatal("rejection treated as success")
			}
		})
	}
}
func TestExchangeRetriesExplicitTemporaryRejection(t *testing.T) {
	calls := 0
	client := mihoyo.NewClient("")
	client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return response(`{"retcode":-1,"message":"兑换未开始"}`), nil
		}
		return response(`{"retcode":0}`), nil
	})
	result, err := (Service{Client: client, Config: model.Config{Shop: model.ShopConfig{RetrySeconds: 1, RetryInterval: 0.05}}}).ExchangeWithRetry(context.Background(), model.ExchangePlan{GoodsID: "g", DeviceFP: "fp"}, nil)
	if err != nil || calls != 2 || result["ok"] != true {
		t.Fatal(result, err, calls)
	}
}
func TestEngineStopsActiveRequestAndPreventsReplay(t *testing.T) {
	s, _ := store.New(filepath.Join(t.TempDir(), "state.json"))
	u, _, _ := s.CreateAdmin("admin", "password123")
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "a", Cookie: "private"})
	a := s.AccountsForUser(u.ID, false)[0]
	_ = s.SetAccountDeviceFP(a.ID, a.Device.ID, "fp")
	p, _ := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "g", Enabled: true})
	started := make(chan struct{})
	var exchanges atomic.Int32
	client := mihoyo.NewClient("")
	client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "private" || r.Header.Get("X-Rpc-Device_id") != a.Device.ID {
			t.Error("request did not use account identity")
		}
		switch r.URL.Path {
		case mihoyo.MallDetailPath:
			return response(`{"retcode":0,"data":{"goods_id":"g","goods_name":"gift","type":2,"price":10,"total":1,"status":"online"}}`), nil
		case mihoyo.MallPointPath:
			return response(`{"retcode":0,"data":{"points":100}}`), nil
		case "/mall/v1/web/goods/exchange":
			exchanges.Add(1)
			close(started)
			<-r.Context().Done()
			return nil, r.Context().Err()
		default:
			t.Error("unexpected request", r.URL.Path)
			return nil, errors.New("unexpected request")
		}
	})
	e := NewEngine(s, client)
	defer e.Stop()
	var notifications atomic.Int32
	e.Notify = func(event notify.Event) bool {
		notifications.Add(1)
		persisted, _ := s.ExchangePlanForUser(u.ID, false, p.ID)
		if persisted.State != "unknown" || event.Kind != notify.ExchangeEventKind || event.UserID != u.ID || event.AccountID != a.ID || event.Success {
			t.Error("exchange notification preceded persistence or used incorrect ownership/result")
		}
		return false
	}
	if _, err := e.Run(u.ID, false, p.ID, false); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("exchange did not start")
	}
	if _, err := e.Run(u.ID, false, p.ID, false); err == nil {
		t.Fatal("duplicate start accepted")
	}
	e.Stop()
	got, _ := s.ExchangePlanForUser(u.ID, false, p.ID)
	if exchanges.Load() != 1 || notifications.Load() != 1 || got.State != "unknown" {
		t.Fatalf("unexpected stopped result %+v requests %d", got, exchanges.Load())
	}
}
func TestPrepareRejectsForeignDelivery(t *testing.T) {
	client := mihoyo.NewClient("")
	client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == mihoyo.MallDetailPath {
			return response(`{"retcode":0,"data":{"goods_id":"g","type":1,"price":1}}`), nil
		}
		if r.URL.Path == mihoyo.MallAddressPath {
			return response(`{"retcode":0,"data":{"list":[{"id":"mine"}]}}`), nil
		}
		t.Error("validation proceeded with foreign address")
		return nil, errors.New("unexpected")
	})
	_, err := (Service{Client: client, Account: model.Account{Cookie: "cookie"}}).Prepare(context.Background(), model.ExchangePlan{GoodsID: "g", AddressID: "foreign"}, false)
	if err == nil {
		t.Fatal("foreign delivery accepted")
	}
}

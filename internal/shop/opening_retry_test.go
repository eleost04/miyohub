package shop

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func TestScheduledWindowStaysAnchoredToUpstreamOpening(t *testing.T) {
	for _, offset := range []int64{-3600000, 3600000} {
		synctest.Test(t, func(t *testing.T) {
			clock := &serverClock{status: clockStatus{OffsetMillis: offset}}
			calls := 0
			client := mihoyo.NewClient("")
			client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return response(`{"retcode":-1,"message":"兑换失败"}`), nil
			})
			service := Service{Client: client, Config: model.Config{Shop: model.ShopConfig{RetrySeconds: 1, RetryInterval: .2}}, clock: clock, scheduledAt: clock.Now().Add(-800 * time.Millisecond)}
			got, err := service.ExchangeWithRetry(t.Context(), model.ExchangePlan{GoodsID: "fixture", DeviceFP: "fp"}, nil)
			if err != nil || calls != 1 || got["retry_stop"] != "已到达重试时限" {
				t.Fatal("late preparation reset the window or mixed host/upstream time", got, err, calls)
			}
		})
	}
}

func TestScheduledWindowWaitsBeforeSendAndCountsOnlyRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		clock := &serverClock{status: clockStatus{OffsetMillis: 500, UncertaintyMillis: 500}}
		calls, progress := 0, 0
		client := mihoyo.NewClient("")
		client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
			calls++
			if time.Since(start) < time.Second {
				t.Error("request preceded the lower-bound opening")
			}
			return response(`{"retcode":0}`), nil
		})
		service := Service{Client: client, Config: model.Config{Shop: model.ShopConfig{RetrySeconds: 5}}, clock: clock, scheduledAt: start.Add(time.Second)}
		got, err := service.ExchangeWithRetry(t.Context(), model.ExchangePlan{GoodsID: "fixture", DeviceFP: "fp"}, func(n int, _ string) error { progress = n; return nil })
		if err != nil || got["ok"] != true || calls != 1 || progress != 1 {
			t.Fatal(got, err, calls, progress)
		}
	})
}

func TestExpiredScheduledWindowNeverSendsFirstRequest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		client := mihoyo.NewClient("")
		client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
			t.Error("expired window sent a request")
			return response(`{"retcode":0}`), nil
		})
		service := Service{Client: client, Config: model.Config{Shop: model.ShopConfig{RetrySeconds: 1}}, scheduledAt: time.Now().Add(-time.Second)}
		_, err := service.ExchangeWithRetry(t.Context(), model.ExchangePlan{GoodsID: "fixture", DeviceFP: "fp"}, func(int, string) error { t.Error("unsent request counted"); return nil })
		if !errors.Is(err, ErrRetryWindowElapsed) {
			t.Fatal(err)
		}
	})
}

func TestScheduledQueueUsesHostDurationNotUpstreamWallTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clock := &serverClock{status: clockStatus{OffsetMillis: 3600000}}
		start := time.Now()
		service := Service{Config: model.Config{Shop: model.ShopConfig{RetrySeconds: 2}}, clock: clock, scheduledAt: clock.Now(), AcquireExchange: func(ctx context.Context) (func(), error) { <-ctx.Done(); return nil, ctx.Err() }}
		_, err := service.ExchangeWithRetry(t.Context(), model.ExchangePlan{GoodsID: "fixture", DeviceFP: "fp"}, nil)
		if !errors.Is(err, ErrRetryWindowElapsed) || time.Since(start) != 2*time.Second {
			t.Fatal("queue used the wrong clock", err, time.Since(start))
		}
	})
}

func TestOpeningWaitDoesNotTreatClockEstimateAsExact(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		clock := serverClock{status: clockStatus{OffsetMillis: 500, UncertaintyMillis: 500}}
		if err := clock.wait(t.Context(), start.Add(time.Second), func() bool { return true }); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) < time.Second {
			t.Fatal("half-second clock estimate allowed an early request", time.Since(start))
		}
	})
}

func TestNotOpenRefusalsContinueUntilSuccessOrSoldOut(t *testing.T) {
	for _, ending := range []string{`{"retcode":0}`, `{"retcode":-1,"message":"库存不足"}`} {
		synctest.Test(t, func(t *testing.T) {
			calls := 0
			client := mihoyo.NewClient("")
			client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if calls <= 2 {
					return response(`{"retcode":-1,"message":"未到兑换时间"}`), nil
				}
				if calls == 3 {
					return response(`{"retcode":-1,"message":"系统繁忙，请稍后重试"}`), nil
				}
				return response(ending), nil
			})
			service := Service{Client: client, Config: model.Config{Shop: model.ShopConfig{RetrySeconds: 5, RetryInterval: .2}}}
			got, err := service.ExchangeWithRetry(t.Context(), model.ExchangePlan{GoodsID: "fixture", DeviceFP: "fp"}, nil)
			if err != nil || calls != 4 || got["attempt"] != 4 {
				t.Fatal("not-open refusal stopped the window", got, err, calls)
			}
		})
	}
}

func TestRetryWindowDoesNotStopAtSixtyAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		client := mihoyo.NewClient("")
		client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return response(`{"retcode":-1,"message":"兑换失败"}`), nil
		})
		service := Service{Client: client, Config: model.Config{Shop: model.ShopConfig{RetrySeconds: 15, RetryInterval: .2}}}
		got, err := service.ExchangeWithRetry(t.Context(), model.ExchangePlan{GoodsID: "fixture", DeviceFP: "fp"}, nil)
		if err != nil || calls != 75 || got["retry_stop"] != "已到达重试时限" {
			t.Fatal("window was cut short by unrelated attempt cap", got, err, calls)
		}
	})
}

func TestKnownFutureOpeningOverridesStaleOnlineFlag(t *testing.T) {
	const now = 1800000000
	good := normalizeGood(map[string]any{"goods_id": "fixture", "status": "online", "total": 4, "sale_start_time": now + 10, "now_time": now})
	if good["display_status"] != "scheduled" || good["exchange_timestamp"] != now+10 {
		t.Fatal("future opening was ignored", good)
	}
	if err := validateAvailability(good, model.ExchangePlan{}, true, false); err == nil {
		t.Fatal("immediate exchange allowed before published opening")
	}
}

// Keep the transport uncertainty boundary independent from retry wording.
func TestNotOpenThenLostResponseDoesNotReplay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		client := mihoyo.NewClient("")
		client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return response(`{"retcode":-1,"message":"未到兑换时间"}`), nil
			}
			return nil, errors.New("response lost after send")
		})
		_, err := (Service{Client: client, Config: model.Config{Shop: model.ShopConfig{RetrySeconds: 10, RetryInterval: .2}}}).ExchangeWithRetry(t.Context(), model.ExchangePlan{GoodsID: "fixture", DeviceFP: "fp"}, nil)
		if !errors.Is(err, ErrUncertain) || calls != 2 {
			t.Fatal("uncertain result was retried", err, calls)
		}
	})
}

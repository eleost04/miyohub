package shop

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func TestRetryClassification(t *testing.T) {
	for _, tc := range []struct {
		code    int
		message string
		retry   bool
	}{
		{-1, "兑换失败", true}, {-1, "服务繁忙，请稍后重试", true}, {-1, "请求过于频繁，请稍后再试", true},
		{-1, "兑换未开始", true}, {-1, "米游币不足，请稍后重试", false}, {-1, "需要安全验证，请稍后再试", false},
		{-1, "库存不足", false}, {-1, "已达到限购上限", false}, {-100, "兑换失败", false},
		{0, "兑换失败", false}, {-999999, "兑换失败", false}, {-3000, "unknown refusal", false},
	} {
		if got := retryable(map[string]any{"retcode": tc.code, "message": tc.message}); got != tc.retry {
			t.Errorf("code=%d message=%s got=%v", tc.code, tc.message, got)
		}
	}
}

func TestGenericRejectionRetriesAndKeepsCode(t *testing.T) {
	client := mihoyo.NewClient("")
	calls := 0
	client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
		calls++
		if calls < 3 {
			return response(`{"retcode":-1}`), nil
		}
		return response(`{"retcode":0}`), nil
	})
	var logs []string
	service := Service{Client: client, Config: model.Config{Shop: model.ShopConfig{RetrySeconds: 2, RetryInterval: .2}}, Emit: func(message string) { logs = append(logs, message) }}
	result, err := service.ExchangeWithRetry(t.Context(), model.ExchangePlan{GoodsID: "g", DeviceFP: "fp"}, nil)
	if err != nil || calls != 3 || result["ok"] != true || len(logs) != 2 || !strings.Contains(logs[1], "错误码 -1") {
		t.Fatal("generic refusal stopped after one attempt", err, result, calls, logs)
	}
}

func TestRetryWindowKeepsLastKnownRejection(t *testing.T) {
	client := mihoyo.NewClient("")
	calls := 0
	client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return response(`{"retcode":-1,"message":"兑换失败"}`), nil
	})
	service := Service{Client: client, Config: model.Config{Shop: model.ShopConfig{RetrySeconds: .35, RetryInterval: .2}}}
	result, err := service.ExchangeWithRetry(t.Context(), model.ExchangePlan{GoodsID: "g", DeviceFP: "fp"}, nil)
	if err != nil || calls != 2 || result["retry_stop"] != "已到达重试时限" || !strings.Contains(exchangeResultText(result), "错误码 -1") {
		t.Fatal("last refusal was lost", err, result, calls)
	}
}

func TestRetryWindowDoesNotCancelInflightSuccess(t *testing.T) {
	client := mihoyo.NewClient("")
	client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		timer := time.NewTimer(40 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return nil, r.Context().Err()
		case <-timer.C:
			return response(`{"retcode":0}`), nil
		}
	})
	result, err := (Service{Client: client, Config: model.Config{Shop: model.ShopConfig{RetrySeconds: .01, RetryInterval: .2}}}).ExchangeWithRetry(t.Context(), model.ExchangePlan{GoodsID: "g", DeviceFP: "fp"}, nil)
	if err != nil || result["ok"] != true {
		t.Fatal("window boundary interrupted an in-flight result", result, err)
	}
}

func TestCancelledQueueDoesNotCountOrSendARequest(t *testing.T) {
	client := mihoyo.NewClient("")
	client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) {
		t.Error("cancelled queue sent request")
		return nil, errors.New("unexpected")
	})
	service := Service{Client: client, Config: model.Config{Shop: model.ShopConfig{RetrySeconds: .01}}, AcquireExchange: func(ctx context.Context) (func(), error) { <-ctx.Done(); return nil, ctx.Err() }}
	_, err := service.ExchangeWithRetry(t.Context(), model.ExchangePlan{GoodsID: "g", DeviceFP: "fp"}, func(int, string) error { t.Error("queued request counted as sent"); return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

package shop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
)

func TestServerClockSynchronizesAndThrottlesAnonymousRequests(t *testing.T) {
	calls := 0
	client := mihoyo.NewClient("")
	client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != mihoyo.MallGoodsPath || r.Header.Get("Cookie") != "" {
			t.Error("clock must use anonymous catalog requests")
		}
		reply := response(`{"retcode":0}`)
		reply.Header.Set("Date", time.Now().Add(10*time.Second).UTC().Format(http.TimeFormat))
		return reply, nil
	})
	clock := serverClock{}
	if err := clock.sync(context.Background(), client); err != nil {
		t.Fatal(err)
	}
	offset := clock.Now().Sub(time.Now())
	if offset < 9*time.Second || offset > 11*time.Second || clock.status.SyncedAt.IsZero() {
		t.Fatal("incorrect server offset", offset)
	}
	if err := clock.sync(context.Background(), client); err != nil || calls != 1 {
		t.Fatal("clock synchronization was not throttled", err, calls)
	}
}

func TestClockPrefersFreshMiHoYoNowTimeAndSharesInflightSample(t *testing.T) {
	client := mihoyo.NewClient("")
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	client.HTTP.Transport = testTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != mihoyo.MallDetailPath || r.URL.Query().Get("goods_id") != "public-good" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Rpc-Device_id") != "" || r.Header.Get("Cache-Control") != "no-cache" {
			t.Error("incorrect anonymous clock request")
		}
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		serverTime := time.Now().Add(10 * time.Second)
		reply := response(fmt.Sprintf(`{"retcode":0,"data":{"now_time":%d}}`, serverTime.Unix()))
		reply.Header.Set("Date", serverTime.UTC().Format(http.TimeFormat))
		return reply, nil
	})
	clock := serverClock{}
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := clock.sync(t.Context(), client, "public-good"); err != nil {
				t.Error(err)
			}
		}()
	}
	<-started
	close(release)
	wg.Wait()
	if calls.Load() != 1 || clock.status.Source != "mihoyo_now_time" || clock.status.UncertaintyMillis < 500 {
		t.Fatal("clock did not share a correctly labeled sample", calls.Load(), clock.status)
	}
}

func TestClockRejectsStaleOrInconsistentSamples(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		name, age string
		stamp     int64
	}{
		{"cached", "5", now.Unix()}, {"stale body", "", now.Add(-time.Minute).Unix()}, {"milliseconds not seconds", "", now.UnixMilli()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{"Date": {now.Format(http.TimeFormat)}}
			headers.Set("Age", tc.age)
			_, _, err := clockSample(map[string]any{"retcode": 0, "data": map[string]any{"now_time": tc.stamp}}, headers)
			if err == nil {
				t.Fatal("unsafe clock sample accepted")
			}
		})
	}
}

func TestClockRefreshFailureDescribesCachedOffsetAccurately(t *testing.T) {
	clock := serverClock{status: clockStatus{OffsetMillis: 10000, SyncedAt: time.Now()}}
	client := mihoyo.NewClient("")
	client.HTTP.Transport = testTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })
	if err := clock.sync(t.Context(), client); err == nil || clock.status.OffsetMillis != 10000 || clock.status.Error != "米哈游校时失败，沿用上次校时" {
		t.Fatal("cached fallback is incorrect", err, clock.status)
	}
	clock.attempted = time.Time{}
	clock.status.SyncedAt = time.Now().Add(-11 * time.Minute)
	if err := clock.sync(t.Context(), client); err == nil || clock.status.OffsetMillis != 0 || clock.status.Source != "" {
		t.Fatal("stale fallback was retained", err, clock.status)
	}
}

func TestServerClockWaitIsCancellableAndChecksEligibility(t *testing.T) {
	clock := serverClock{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := clock.wait(ctx, time.Now().Add(time.Hour), func() bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled wait continued", err)
	}
	if err := clock.wait(context.Background(), time.Now().Add(time.Hour), func() bool { return false }); err == nil {
		t.Fatal("disabled account continued waiting")
	}
}

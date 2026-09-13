package shop

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

const preparationWindow = 180 * time.Second

type clockStatus struct {
	OffsetMillis      int64     `json:"offset_ms"`
	RTTMillis         int64     `json:"rtt_ms"`
	Source            string    `json:"source"`
	UncertaintyMillis int64     `json:"uncertainty_ms"`
	SyncedAt          time.Time `json:"synced_at"`
	Error             string    `json:"error"`
}

type serverClock struct {
	mu        sync.RWMutex
	status    clockStatus
	attempted time.Time
	inflight  chan struct{}
}

func (c *serverClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().Add(time.Duration(c.status.OffsetMillis) * time.Millisecond)
}

// Upstream timestamps have second resolution. A midpoint estimate can be
// ahead of the actual clock; use its lower bound when waiting for a sale.
func (c *serverClock) earliestNow() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().Add(time.Duration(c.status.OffsetMillis-max(0, c.status.UncertaintyMillis)) * time.Millisecond)
}

func (c *serverClock) sync(ctx context.Context, client *mihoyo.Client, goodsID ...string) error {
	c.mu.Lock()
	if pending := c.inflight; pending != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-pending:
			c.mu.RLock()
			defer c.mu.RUnlock()
			if c.status.Error != "" {
				return errors.New(c.status.Error)
			}
			return nil
		}
	}
	if time.Since(c.attempted) < time.Minute {
		lastError := c.status.Error
		c.mu.Unlock()
		if lastError != "" {
			return errors.New(lastError)
		}
		return nil
	}
	c.attempted = time.Now()
	c.inflight = make(chan struct{})
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	before := time.Now()
	var payload map[string]any
	path := mihoyo.MallGoodsPath
	query := url.Values{"app_id": {"1"}, "point_sn": {"myb"}, "page_size": {"1"}, "page": {"1"}, "_t": {strconv.FormatInt(before.UnixMilli(), 10)}}
	if len(goodsID) > 0 && goodsID[0] != "" {
		path = mihoyo.MallDetailPath
		query.Del("page_size")
		query.Del("page")
		query.Set("goods_id", goodsID[0])
	}
	headers, err := client.JSONWithHeaders(ctx, http.MethodGet, mihoyo.TakumiAPI+path, query, nil, http.Header{"x-rpc-client_type": {"5"}, "Referer": {"https://user.mihoyo.com/"}, "Cache-Control": {"no-cache"}, "Pragma": {"no-cache"}}, &payload)
	after := time.Now()
	var serverTime time.Time
	source := ""
	if err == nil {
		serverTime, source, err = clockSample(payload, headers)
	}
	offset := serverTime.Sub(before.Add(after.Sub(before) / 2))
	if err == nil && (offset > 24*time.Hour || offset < -24*time.Hour) {
		err = errors.New("服务器与本地时间相差超过一天，请检查系统时钟")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	defer func() { close(c.inflight); c.inflight = nil }()
	if err != nil {
		c.status.Error = "米哈游校时失败，沿用上次校时"
		if c.status.SyncedAt.IsZero() || time.Since(c.status.SyncedAt) > 10*time.Minute {
			c.status = clockStatus{Error: "米哈游校时失败，暂用服务器本地时间"}
		}
		return err
	}
	c.status = clockStatus{OffsetMillis: offset.Milliseconds(), RTTMillis: after.Sub(before).Milliseconds(), SyncedAt: after, Source: source, UncertaintyMillis: 500 + after.Sub(before).Milliseconds()/2}
	return nil
}

// Both verified upstream sources have one-second resolution. Estimate the
// middle of that second, then compensate for half the measured round trip.
// This is an estimate, not a claim of millisecond synchronization.
func clockSample(payload map[string]any, headers http.Header) (time.Time, string, error) {
	if age, _ := strconv.Atoi(headers.Get("Age")); age > 0 {
		return time.Time{}, "", errors.New("校时响应来自缓存")
	}
	date, dateErr := http.ParseTime(headers.Get("Date"))
	data := dataMap(payload)
	stamp := int64(intValue(data["now_time"]))
	if stamp == 0 {
		for _, item := range maps(data["list"]) {
			if stamp = int64(intValue(item["now_time"])); stamp > 0 {
				break
			}
		}
	}
	if retcode(payload) == 0 && stamp > 0 {
		serverTime := time.Unix(stamp, 0)
		if dateErr == nil && (serverTime.Sub(date) > 5*time.Second || date.Sub(serverTime) > 5*time.Second) {
			return time.Time{}, "", errors.New("米哈游时间与响应日期不一致")
		}
		return serverTime.Add(500 * time.Millisecond), "mihoyo_now_time", nil
	}
	if dateErr != nil {
		return time.Time{}, "", errors.New("米哈游未返回可用的校时时间")
	}
	return date.Add(500 * time.Millisecond), "mihoyo_http_date", nil
}

func (c *serverClock) wait(ctx context.Context, target time.Time, allowed func() bool) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !allowed() {
			return errors.New("账号或兑换功能已停用")
		}
		remaining := target.Sub(c.earliestNow())
		if remaining <= 0 {
			return nil
		}
		timer := time.NewTimer(min(remaining, time.Second))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type EngineStatus struct {
	Enabled    bool        `json:"enabled"`
	Running    int         `json:"running"`
	NextRun    int64       `json:"next_run"`
	ServerTime time.Time   `json:"server_time"`
	Clock      clockStatus `json:"clock"`
}

func (e *Engine) Status(user model.User) EngineStatus {
	status := EngineStatus{Enabled: e.store.Config().Shop.Enabled && e.store.UserCanExchange(user.ID), ServerTime: e.clock.Now()}
	e.clock.mu.RLock()
	status.Clock = e.clock.status
	e.clock.mu.RUnlock()
	for _, plan := range e.store.ExchangePlansForUser(user.ID, user.Role == "admin") {
		if plan.State == "running" {
			status.Running++
		}
		if !plan.Enabled || !plan.Auto || plan.ExchangeAt <= 0 || (plan.State != "pending" && plan.State != "running") {
			continue
		}
		if !e.store.AccountCanExchange(plan.AccountID) {
			continue
		}
		if status.NextRun == 0 || plan.ExchangeAt < status.NextRun {
			status.NextRun = plan.ExchangeAt
		}
	}
	return status
}

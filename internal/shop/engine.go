package shop

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
	"github.com/eleost04/miyohub/internal/store"
)

var errEngineStopped = errors.New("exchange engine stopped")

type Engine struct {
	Notify           func(notify.Event) bool
	store            *store.Store
	client           *mihoyo.Client
	mu               sync.Mutex
	ctx              context.Context
	cancel           context.CancelCauseFunc
	wake             chan struct{}
	wg               sync.WaitGroup
	started, stopped bool
	running          map[string]context.CancelFunc
	accountRequests  map[string]chan struct{}
	clock            serverClock
}

func NewEngine(s *store.Store, client *mihoyo.Client) *Engine {
	ctx, cancel := context.WithCancelCause(context.Background())
	return &Engine{store: s, client: client, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1), running: map[string]context.CancelFunc{}, accountRequests: map[string]chan struct{}{}}
}
func (e *Engine) Start() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started || e.stopped {
		return
	}
	e.started = true
	e.wg.Add(1)
	go func() { defer e.wg.Done(); e.loop() }()
}
func (e *Engine) Wake() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}
func (e *Engine) Stop() {
	e.mu.Lock()
	e.stopped = true
	e.cancel(errEngineStopped)
	e.mu.Unlock()
	e.wg.Wait()
}
func (e *Engine) Cancel(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if cancel := e.running[id]; cancel != nil {
		cancel()
	}
}
func (e *Engine) CancelAccount(id string) {
	for _, p := range e.store.ExchangePlansForUser("", true) {
		if p.AccountID == id {
			e.Cancel(p.ID)
		}
	}
}
func (e *Engine) Run(userID string, admin bool, id string, scheduled bool) (model.ExchangePlan, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return model.ExchangePlan{}, errors.New("服务正在停止")
	}
	p, a, err := e.store.ClaimExchangePlanAt(userID, admin, id, scheduled, e.clock.Now(), preparationWindow)
	if err != nil {
		return p, err
	}
	ctx, cancel := context.WithTimeout(e.ctx, 7*time.Minute)
	e.running[p.ID] = cancel
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer cancel()
		defer func() { e.mu.Lock(); delete(e.running, p.ID); e.mu.Unlock(); e.Wake() }()
		e.execute(ctx, p, a, scheduled)
	}()
	return p, nil
}
func (e *Engine) execute(ctx context.Context, p model.ExchangePlan, a model.Account, scheduled bool) {
	state, message, attempt := "failed", "兑换准备失败", 0
	defer func() {
		if errors.Is(context.Cause(ctx), errEngineStopped) && attempt == 0 && scheduled {
			state, message = "pending", "服务已停止，重启后将继续准备兑换"
		}
		if err := e.store.FinishExchange(p.ID, p.AttemptKey, state, attempt, message); err != nil {
			e.store.ExchangeFailureLog(p, a.UserID, err)
		} else if state != "pending" && e.Notify != nil {
			e.Notify(notify.ExchangeEvent(e.store.Config(), a, p, state, message, attempt, time.Now()))
		}
		_ = e.store.AddLogForUser(a.UserID, "exchange", fmt.Sprintf("%s: %s（共请求 %d 次）", p.GoodsName, message, attempt))
	}()
	allowed := func() bool {
		latest, ok := e.store.AccountRunnable(a.ID)
		return ok && e.store.AccountCanExchange(a.ID) && e.store.Config().Shop.Enabled && latest.Stoken == a.Stoken && latest.Mid == a.Mid && latest.Device.ID == a.Device.ID
	}
	client := *e.client
	httpClient := *client.HTTP
	base := httpClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	httpClient.Transport = permissionTransport{base: base, allowed: allowed}
	client.HTTP = &httpClient
	service := Service{Client: &client, Config: e.store.Config(), Account: a, AcquireExchange: e.requestGate(a.ID)}
	service.Emit = func(message string) { _ = e.store.AddLogForUser(a.UserID, "exchange", p.GoodsName+": "+message) }
	if scheduled {
		if err := e.clock.sync(ctx, e.client, p.GoodsID); err != nil {
			_ = e.store.AddLogForUser(a.UserID, "exchange", p.GoodsName+": 米哈游校时未成功，将按当前可用时钟执行；可在兑换页面查看校时状态")
		}
	}
	var prepared model.ExchangePlan
	var err error
	if scheduled {
		prepared, err = service.PrepareScheduled(ctx, p)
	} else {
		prepared, err = service.Prepare(ctx, p, true)
	}
	if err != nil {
		message = err.Error()
		if ctx.Err() != nil {
			state = "cancelled"
			message = "兑换已取消，未发送兑换请求"
		}
		return
	}
	if err = e.store.SetAccountDeviceFP(a.ID, a.Device.ID, prepared.DeviceFP); err != nil {
		message = "设备信息保存失败，未发送兑换请求"
		return
	}
	if scheduled {
		if err = e.store.ExchangePhase(p.ID, p.AttemptKey, "waiting", "准备完成，等待兑换时间"); err == nil {
			target := time.Unix(p.ExchangeAt, 0)
			// Refresh a three-minute-old sample shortly before opening. Calls
			// from concurrent plans share one request and a one-minute cooldown.
			if err = e.clock.wait(ctx, target.Add(-10*time.Second), allowed); err == nil {
				_ = e.clock.sync(ctx, e.client, p.GoodsID)
				err = e.clock.wait(ctx, target, allowed)
			}
		}
		if err != nil {
			state, message = "cancelled", "兑换已停止，未发送兑换请求"
			return
		}
		if e.clock.Now().Unix()-p.ExchangeAt > 60 {
			message = "已错过兑换时间，请重新创建计划"
			return
		}
	}
	if latest, ok := e.store.AccountRunnable(a.ID); ok && allowed() {
		service.Account = latest
	}
	result, err := service.ExchangeWithRetry(ctx, prepared, func(n int, msg string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !allowed() {
			return errors.New("账号凭据或兑换权限已变化，请检查后重新创建计划")
		}
		if err := e.store.ExchangeProgress(p.ID, p.AttemptKey, n, fmt.Sprintf("第 %d 次：%s", n, msg)); err != nil {
			return err
		}
		attempt = n
		return nil
	})
	if err != nil {
		message = err.Error()
		if errors.Is(err, ErrUncertain) {
			state = "unknown"
		} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			state = "cancelled"
			message = "已停止后续请求"
		}
		return
	}
	attempt = intValue(result["attempt"])
	message = exchangeResultText(result)
	if boolValue(result["ok"]) {
		state = "success"
		message = "兑换成功，请在米游社查看兑换记录"
	}
}
func (e *Engine) loop() {
	for {
		if e.ctx.Err() != nil {
			return
		}
		next := e.clock.Now().Add(time.Minute)
		cfg := e.store.Config()
		if cfg.Shop.Enabled {
			sort.SliceStable(cfg.Shop.Plans, func(i, j int) bool { return cfg.Shop.Plans[i].ExchangeAt < cfg.Shop.Plans[j].ExchangeAt })
			for _, p := range cfg.Shop.Plans {
				if !p.Enabled || !p.Auto || p.State != "pending" || p.ExchangeAt <= 0 {
					continue
				}
				if !e.store.AccountCanExchange(p.AccountID) {
					continue
				}
				target := time.Unix(p.ExchangeAt, 0).Add(-preparationWindow)
				if !target.After(e.clock.Now()) {
					_, _ = e.Run("", true, p.ID, true)
					// A duplicate good may still be running. Retry dispatch after it ends.
					if retry := e.clock.Now().Add(time.Second); retry.Before(next) {
						next = retry
					}
				} else if target.Before(next) {
					next = target
				}
			}
		}
		timer := time.NewTimer(max(time.Millisecond, next.Sub(e.clock.Now())))
		select {
		case <-e.ctx.Done():
			timer.Stop()
			return
		case <-e.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (e *Engine) requestGate(accountID string) func(context.Context) (func(), error) {
	e.mu.Lock()
	gate := e.accountRequests[accountID]
	if gate == nil {
		gate = make(chan struct{}, 1)
		e.accountRequests[accountID] = gate
	}
	e.mu.Unlock()
	return func(ctx context.Context) (func(), error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case gate <- struct{}{}:
			if err := ctx.Err(); err != nil {
				<-gate
				return nil, err
			}
			return func() { <-gate }, nil
		}
	}
}

type permissionTransport struct {
	base    http.RoundTripper
	allowed func() bool
}

func (t permissionTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Context().Err() != nil {
		return nil, r.Context().Err()
	}
	if !t.allowed() {
		return nil, errors.New("账号或兑换权限已变化，已停止后续请求")
	}
	return t.base.RoundTrip(r)
}

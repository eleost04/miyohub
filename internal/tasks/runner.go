package tasks

import (
	"context"
	"errors"
	"fmt"
	"github.com/eleost04/miyohub/internal/auth"
	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
	"github.com/eleost04/miyohub/internal/store"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Runner struct {
	Notify    func(notify.Event) bool
	store     *store.Store
	mu        sync.Mutex
	running   map[string]context.CancelFunc
	contexts  map[string]context.Context
	progress  map[string]model.TaskProgress
	batches   map[string]context.CancelFunc
	newClient func() *mihoyo.Client
	wg        sync.WaitGroup
	stopped   bool
}

func NewRunner(s *store.Store) *Runner {
	return &Runner{store: s, running: map[string]context.CancelFunc{}, contexts: map[string]context.Context{}, progress: map[string]model.TaskProgress{}, batches: map[string]context.CancelFunc{}, newClient: func() *mihoyo.Client { return mihoyo.NewClient("") }}
}

var errNoTasks = errors.New("没有匹配且已启用的账号任务，请在账号的签到设置中选择任务")

func (r *Runner) begin(ctx context.Context, ids []string, filters ...RunOptions) (context.Context, []model.Account, context.CancelFunc, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return nil, nil, nil, fmt.Errorf("服务正在停止")
	}
	if !r.store.Config().Enabled {
		return nil, nil, nil, fmt.Errorf("任务已停用")
	}
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	accounts := []model.Account{}
	options := RunOptions{}
	if len(filters) > 0 {
		options = filters[0]
	}
	for _, a := range r.store.AccountsForUser("", true) {
		if ids != nil && !wanted[a.ID] {
			continue
		}
		a, ok := r.store.AccountRunnable(a.ID)
		if !ok {
			continue
		}
		if options.Automatic && (a.TaskSettings == nil || !a.TaskSettings.Automatic) {
			continue
		}
		cfg, ok := r.store.ConfigForAccount(a.ID)
		if !ok || !options.available(cfg) {
			continue
		}
		if r.running[a.ID] != nil {
			if options.Automatic {
				continue
			}
			return nil, nil, nil, fmt.Errorf("所选账号有任务正在执行")
		}
		if options.Automatic {
			claimed, err := r.store.ClaimAutomaticRun(a.ID, a.TaskSettings.Revision, time.Now())
			if err != nil {
				_ = r.store.AddLogForUser(a.UserID, "scheduler", "无法保存自动签到状态，本次未启动，请检查磁盘空间。")
				continue
			}
			if !claimed {
				continue
			}
		}
		accounts = append(accounts, a)
	}
	if len(accounts) == 0 {
		return nil, nil, nil, errNoTasks
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	reserved := map[string]context.Context{}
	for _, a := range accounts {
		accountCtx, stop := context.WithCancel(ctx)
		r.running[a.ID] = stop
		r.contexts[a.ID] = accountCtx
		reserved[a.ID] = accountCtx
		r.progress[a.ID] = model.TaskProgress{AccountID: a.ID, State: "queued", Current: "等待执行", StartedAt: time.Now()}
	}
	batchID := fmt.Sprintf("%p", ctx)
	r.batches[batchID] = cancel
	r.wg.Add(1)
	return ctx, accounts, func() {
		cancel()
		r.mu.Lock()
		defer r.mu.Unlock()
		for id, accountCtx := range reserved {
			if r.contexts[id] == accountCtx {
				r.running[id]()
				delete(r.running, id)
				delete(r.contexts, id)
				delete(r.progress, id)
			}
		}
		delete(r.batches, batchID)
	}, nil
}
func (r *Runner) Run(ctx context.Context, ids []string) error {
	return r.RunWithOptions(ctx, ids, RunOptions{})
}
func (r *Runner) RunScheduled(ctx context.Context) error {
	ids := []string{}
	for _, a := range r.store.AccountsForUser("", true) {
		if a.TaskSettings != nil && a.TaskSettings.Schedule == nil {
			ids = append(ids, a.ID)
		}
	}
	return r.RunWithOptions(ctx, ids, RunOptions{Automatic: true})
}
func (r *Runner) RunWithOptions(ctx context.Context, ids []string, options RunOptions) error {
	options.Games = append([]string(nil), options.Games...)
	if err := options.Validate(); err != nil {
		return err
	}
	ctx, a, cancel, err := r.begin(ctx, ids, options)
	if err != nil {
		if options.Automatic && errors.Is(err, errNoTasks) {
			return nil
		}
		return err
	}
	defer r.end(a, cancel)
	return r.run(ctx, a, options)
}
func (r *Runner) Start(ctx context.Context, ids []string) error {
	return r.StartWithOptions(ctx, ids, RunOptions{})
}
func (r *Runner) StartWithOptions(ctx context.Context, ids []string, options RunOptions) error {
	options.Games = append([]string(nil), options.Games...)
	if err := options.Validate(); err != nil {
		return err
	}
	ctx, a, cancel, err := r.begin(context.WithoutCancel(ctx), ids, options)
	if err != nil {
		return err
	}
	go func() { defer r.end(a, cancel); _ = r.run(ctx, a, options) }()
	return nil
}
func (r *Runner) end(accounts []model.Account, cancel context.CancelFunc) {
	cancel()
	r.wg.Done()
}
func (r *Runner) Stop() {
	r.mu.Lock()
	r.stopped = true
	for _, cancel := range r.batches {
		cancel()
	}
	r.mu.Unlock()
	r.wg.Wait()
}
func (r *Runner) run(ctx context.Context, accounts []model.Account, options RunOptions) error {
	failed := 0
	for _, a := range accounts {
		if err := ctx.Err(); err != nil {
			return err
		}
		account, ok := r.store.AccountRunnable(a.ID)
		if !ok {
			continue
		}
		cfg, ok := r.store.ConfigForAccount(a.ID)
		if !ok || !options.available(cfg) || options.Automatic && (account.TaskSettings == nil || !account.TaskSettings.Automatic) {
			continue
		}
		cfg = options.apply(cfg)
		r.mu.Lock()
		accountCtx := r.contexts[a.ID]
		r.mu.Unlock()
		if accountCtx.Err() != nil {
			r.updateProgress(a.ID, "cancelled", "已停止")
			r.releaseAccount(a.ID, accountCtx)
			continue
		}
		client := r.newClient()
		base := client.HTTP.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		allowed := func() bool {
			latest, ok := r.store.AccountRunnable(account.ID)
			return ok && r.store.Config().Enabled && latest.Cookie == account.Cookie && latest.Stoken == account.Stoken && latest.Device.ID == account.Device.ID && latest.TaskSettings != nil && account.TaskSettings != nil && latest.TaskSettings.Revision == account.TaskSettings.Revision
		}
		client.HTTP.Transport = authorizedTransport{base: base, allowed: allowed}
		captchaAllowed := cfg.Captcha.Allowed
		cfg.Captcha.Allowed = func() bool { return allowed() && (captchaAllowed == nil || captchaAllowed()) }
		if !cfg.Enabled {
			break
		}
		if account.Device.ID != "" {
			cfg.Device = account.Device
		}
		emit := func(message string) {
			_ = r.store.AddLogForUser(account.UserID, "task", account.Name+": "+message)
			if strings.Contains(message, "登录凭据失效") {
				_ = r.store.RecordAccountCheck(account.ID, "expired")
			}
		}
		r.updateProgress(a.ID, "running", "检查登录凭据")
		if account.Stoken != "" && account.Stuid != "" && (cfg.Features.GameCheckin || cfg.Features.BBSTasks) {
			if cookie, err := auth.RefreshCookie(accountCtx, client, account); err == nil {
				updated, err := r.store.RenewAccountCookie(account, cookie)
				if err != nil {
					emit("凭据保存失败：" + err.Error())
					failed++
					r.releaseAccount(a.ID, accountCtx)
					continue
				}
				account = updated
			} else if accountCtx.Err() == nil {
				emit("登录凭据未续期，使用已保存凭据继续")
			}
		}
		emit("开始执行任务")
		results := map[string]model.TaskSummary{}
		perform := func(key, label string, enabled bool, run func(func(string)) model.TaskSummary) {
			if !enabled || accountCtx.Err() != nil {
				return
			}
			r.updateProgress(a.ID, "running", label)
			details := []string{}
			result := run(func(message string) {
				if len(details) < 200 {
					details = append(details, message)
				}
				_ = r.store.AddLogForUser(account.UserID, key, account.Name+": "+message)
				if strings.Contains(message, "登录凭据失效") {
					_ = r.store.RecordAccountCheck(account.ID, "expired")
				}
			})
			result.Details = details
			results[key] = result
			failed += result.Failed
		}
		perform("games", "游戏签到", cfg.Features.GameCheckin, func(log func(string)) model.TaskSummary {
			return (GameCheckin{Client: client, Config: cfg, Account: account, Emit: log}).Run(accountCtx)
		})
		perform("cloud", "云游戏签到", cfg.Features.CloudGameCheckin, func(log func(string)) model.TaskSummary {
			return (CloudCheckin{Client: client, Config: cfg, Account: account, Emit: log}).Run(accountCtx)
		})
		perform("bbs", "米游币任务", cfg.Features.BBSTasks, func(log func(string)) model.TaskSummary {
			return (BBSCheckin{Client: client, Config: cfg, Account: account, Emit: log}).Run(accountCtx)
		})
		if err := r.store.RecordTasks(account.ID, results); err != nil {
			return err
		}
		if r.Notify != nil {
			r.Notify(notify.TaskEvent(cfg, account, results, accountCtx.Err() != nil, time.Now()))
		}
		if accountCtx.Err() != nil {
			emit("任务已停止，已完成的结果已保存")
			r.updateProgress(a.ID, "cancelled", "已停止")
		} else {
			emit("任务执行结束")
			r.updateProgress(a.ID, "done", "执行完成")
		}
		r.releaseAccount(a.ID, accountCtx)
	}
	if failed > 0 && ctx.Err() == nil {
		return fmt.Errorf("%d 项操作未完成，请查看账号结果", failed)
	}
	return ctx.Err()
}

func (r *Runner) releaseAccount(id string, ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.contexts[id] == ctx {
		r.running[id]()
		delete(r.running, id)
		delete(r.contexts, id)
		delete(r.progress, id)
	}
}

func (r *Runner) updateProgress(id, state, current string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if progress, ok := r.progress[id]; ok {
		progress.State, progress.Current = state, current
		r.progress[id] = progress
	}
}

func (r *Runner) ProgressForUser(u model.User) []model.TaskProgress {
	accounts := r.store.AccountsForUser(u.ID, u.Role == "admin")
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []model.TaskProgress{}
	for _, account := range accounts {
		if p, ok := r.progress[account.ID]; ok {
			result = append(result, p)
		}
	}
	return result
}
func (r *Runner) Running() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.running) > 0 }
func (r *Runner) RunningForUser(u model.User) bool {
	accounts := r.store.AccountsForUser(u.ID, u.Role == "admin")
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range accounts {
		if r.running[a.ID] != nil {
			return true
		}
	}
	return false
}

func (r *Runner) CancelAccount(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cancel := r.running[id]; cancel != nil {
		cancel()
	}
}

type authorizedTransport struct {
	base    http.RoundTripper
	allowed func() bool
}

func (t authorizedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if !t.allowed() {
		return nil, errors.New("账号、所属用户或任务已停用")
	}
	return t.base.RoundTrip(request)
}

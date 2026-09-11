package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

// Personal checks only local settings. It never contacts the upstream unless
// an enabled account reaches its own scheduled minute. Missed times are not
// replayed on restart; the runner persists a shared daily claim before work.
type Personal struct {
	store     personalStore
	run       func(context.Context, string) error
	mu        sync.Mutex
	cancel    context.CancelFunc
	done      chan struct{}
	attempted map[string]string
}

type personalStore interface {
	Config() model.Config
	AccountsForUser(string, bool) []model.Account
	AccountRunnable(string) (model.Account, bool)
	AddLogForUser(string, string, string) error
}

func NewPersonal(s personalStore, run func(context.Context, string) error) *Personal {
	return &Personal{store: s, run: run, attempted: map[string]string{}}
}
func (p *Personal) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.done = cancel, make(chan struct{})
	done := p.done
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		p.tick(ctx, time.Now())
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				p.tick(ctx, now)
			}
		}
	}()
}
func (p *Personal) Stop() {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.cancel = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
func (p *Personal) tick(ctx context.Context, now time.Time) {
	if ctx.Err() != nil || !p.store.Config().Enabled {
		return
	}
	nextAttempted := map[string]string{}
	for _, account := range p.store.AccountsForUser("", true) {
		a, ok := p.store.AccountRunnable(account.ID)
		if !ok || !a.CustomRunDue(now) || !a.TaskSettings.HasTasks() {
			continue
		}
		key := now.UTC().Format("2006-01-02T15:04")
		nextAttempted[a.ID] = key
		if p.attempted[a.ID] == key {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		if err := p.run(ctx, a.ID); err != nil {
			_ = p.store.AddLogForUser(a.UserID, "scheduler", "个人定时签到未启动：账号正在执行或没有可执行任务，请检查签到设置。")
		}
	}
	p.attempted = nextAttempted
}

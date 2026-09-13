package notify

import (
	"context"
	"sync"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

type Outbox interface {
	QueuePushEvent(Event) (bool, error)
	ClaimPushDelivery() (model.PushDelivery, model.PushChannel, bool, error)
	FinishPushDelivery(string, Result) error
	RecoverPushDeliveries() error
	AddLogForUser(string, string, string) error
}
type calendarOutbox interface{ QueueDueCalendarReminders(time.Time) (bool, error) }

// The encrypted outbox survives restarts. Ambiguous in-flight deliveries are
// never replayed automatically, because a provider may already have accepted them.
type Dispatcher struct {
	store            Outbox
	sender           Deliverer
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.Mutex
	wg               sync.WaitGroup
	started, stopped bool
	wake             chan struct{}
}

func NewDispatcher(s Outbox, sender Deliverer) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &Dispatcher{store: s, sender: sender, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
}
func (d *Dispatcher) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started || d.stopped {
		return nil
	}
	if err := d.store.RecoverPushDeliveries(); err != nil {
		return err
	}
	d.started = true
	if calendar, ok := d.store.(calendarOutbox); ok {
		d.wg.Add(1)
		go d.calendarWork(calendar)
	}
	for i := 0; i < 2; i++ {
		d.wg.Add(1)
		go d.work()
	}
	return nil
}
func (d *Dispatcher) Enqueue(event Event) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return false
	}
	event.Title, event.Message = truncate(event.Title, 100), truncate(event.Message, 6000)
	queued, err := d.store.QueuePushEvent(event)
	if err != nil {
		_ = d.store.AddLogForUser(event.UserID, "push", "通知未入队："+err.Error()+"；任务结果仍已独立保存")
	}
	if err != nil || !queued {
		return false
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
	return true
}
func (d *Dispatcher) Stop() { d.mu.Lock(); d.stopped = true; d.cancel(); d.mu.Unlock(); d.wg.Wait() }
func (d *Dispatcher) calendarWork(calendar calendarOutbox) {
	defer d.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	warned := false
	for d.ctx.Err() == nil {
		queued, err := calendar.QueueDueCalendarReminders(time.Now())
		if err != nil && !warned {
			_ = d.store.AddLogForUser("", "scheduler", "日历提醒暂未入队，请检查存储或队列状态；稍后重试")
		}
		warned = err != nil
		if queued {
			select {
			case d.wake <- struct{}{}:
			default:
			}
		}
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (d *Dispatcher) work() {
	defer d.wg.Done()
	for d.ctx.Err() == nil {
		entry, channel, ok, err := d.store.ClaimPushDelivery()
		if err == nil && ok {
			cfg := model.PushConfig{Enabled: true, Channels: []model.PushChannel{channel}}
			results := d.sender.Send(d.ctx, cfg, entry.Title, entry.Message, entry.Success)
			result := Result{ChannelID: channel.ID, Provider: channel.Provider, Error: "服务已停止，送达状态未确认", Uncertain: true}
			if len(results) > 0 {
				result = results[0]
			}
			_ = d.store.FinishPushDelivery(entry.ID, result)
			continue
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-d.ctx.Done():
			timer.Stop()
			return
		case <-d.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

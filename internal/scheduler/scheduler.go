package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
	_ "time/tzdata"

	"github.com/eleost04/miyohub/internal/model"
)

// RunFunc executes one complete scheduled task run.
type RunFunc func(context.Context) error

type Status struct {
	Enabled   bool           `json:"enabled"`
	Running   bool           `json:"running"`
	NextRun   string         `json:"next_run"`
	LastRun   string         `json:"last_run"`
	LastError string         `json:"last_error"`
	Schedule  model.Schedule `json:"schedule"`
}

type Scheduler struct {
	mu       sync.Mutex
	schedule model.Schedule
	run      RunFunc
	log      func(string)

	started bool
	stop    chan struct{}
	wake    chan struct{}
	done    chan struct{}
	cancel  context.CancelFunc

	running   bool
	nextRun   time.Time
	lastRun   time.Time
	lastError string
}

func New(schedule model.Schedule, run RunFunc, logFn func(string)) *Scheduler {
	if logFn == nil {
		logFn = func(string) {}
	}
	return &Scheduler{schedule: schedule, run: run, log: logFn}
}

func (s *Scheduler) Start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	if _, err := nextScheduledRun(s.schedule, time.Now()); err != nil {
		s.mu.Unlock()
		return err
	}
	s.stop = make(chan struct{})
	s.wake = make(chan struct{}, 1)
	s.done = make(chan struct{})
	s.started = true
	stop, done := s.stop, s.done
	s.mu.Unlock()
	go s.loop(stop, done)
	return nil
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return
	}
	stop, done, cancel := s.stop, s.done, s.cancel
	s.started = false
	close(stop)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	<-done
}

func (s *Scheduler) Reload(schedule model.Schedule) error {
	if _, err := nextScheduledRun(schedule, time.Now()); err != nil {
		return err
	}
	s.mu.Lock()
	s.schedule = schedule
	s.nextRun = time.Time{}
	wake := s.wake
	s.mu.Unlock()
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	return nil
}

func (s *Scheduler) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{
		Enabled:   s.schedule.Enabled,
		Running:   s.running,
		NextRun:   formatTime(s.nextRun),
		LastRun:   formatTime(s.lastRun),
		LastError: s.lastError,
		Schedule:  s.schedule,
	}
}

func (s *Scheduler) loop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	s.mu.Lock()
	runOnStart := s.schedule.RunOnStart
	s.mu.Unlock()
	if runOnStart {
		s.runOnce(stop)
	}

	for {
		s.mu.Lock()
		schedule := s.schedule
		next := s.nextRun
		s.mu.Unlock()
		if !schedule.Enabled {
			s.setNextRun(time.Time{})
			if !waitForSignal(stop, s.wake, 30*time.Second) {
				return
			}
			continue
		}
		if next.IsZero() {
			computed, err := nextScheduledRun(schedule, time.Now())
			if err != nil {
				s.setError(err)
				if !waitForSignal(stop, s.wake, time.Minute) {
					return
				}
				continue
			}
			next = computed
			s.setNextRun(next)
			s.log(fmt.Sprintf("下次自动执行时间: %s", next.Format(time.DateTime)))
		}

		wait := time.Until(next)
		if wait > 0 {
			if !waitForSignal(stop, s.wake, wait) {
				return
			}
			continue
		}
		s.runOnce(stop)
		s.setNextRun(time.Time{})
	}
}

func (s *Scheduler) runOnce(stop <-chan struct{}) {
	s.mu.Lock()
	if s.running || isStopped(stop) {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.running = true
	s.cancel = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.running = false
		s.cancel = nil
		s.mu.Unlock()
	}()

	s.log("开始执行定时任务")
	if s.run == nil {
		s.setError(errors.New("未配置任务执行器"))
		return
	}
	if err := s.run(ctx); err != nil {
		if !isStopped(stop) {
			s.setError(err)
			s.log("定时任务失败: " + err.Error())
		}
		return
	}
	s.mu.Lock()
	s.lastRun = time.Now()
	s.lastError = ""
	s.mu.Unlock()
	s.log("定时任务执行完成")
}

func (s *Scheduler) setNextRun(value time.Time) {
	s.mu.Lock()
	s.nextRun = value
	s.mu.Unlock()
}

func (s *Scheduler) setError(err error) {
	s.mu.Lock()
	s.lastError = err.Error()
	s.mu.Unlock()
}

func waitForSignal(stop <-chan struct{}, wake <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-stop:
		return false
	case <-wake:
		return true
	case <-timer.C:
		return true
	}
}

func isStopped(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

func nextScheduledRun(schedule model.Schedule, now time.Time) (time.Time, error) {
	hour, minute, err := parseTime(schedule.Time)
	if err != nil {
		return time.Time{}, err
	}
	timezone := schedule.Timezone
	if timezone == "" {
		timezone = model.DefaultTimezone
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("schedule.timezone 无效: %s", timezone)
	}
	localNow := now.In(location)
	target := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, location)
	if !target.After(now) {
		target = time.Date(localNow.Year(), localNow.Month(), localNow.Day()+1, hour, minute, 0, 0, location)
	}
	if schedule.JitterMins > 0 {
		jitter := rand.Intn(schedule.JitterMins*60 + 1)
		target = target.Add(time.Duration(jitter) * time.Second)
	}
	return target, nil
}

func Validate(schedule model.Schedule) error {
	_, err := nextScheduledRun(schedule, time.Now())
	return err
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(time.RFC3339)
}

func parseTime(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, 0, fmt.Errorf("schedule.time 必须是 HH:MM 格式")
	}
	return parsed.Hour(), parsed.Minute(), nil
}

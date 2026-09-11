package store

import (
	"errors"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

var ErrCaptchaProbeCooldown = errors.New("测试间隔至少为 60 秒，请等待倒计时结束")

func (s *Store) CaptchaProbeForUser(userID string) *model.CaptchaProbe {
	s.mu.RLock()
	defer s.mu.RUnlock()
	probe, ok := s.data.CaptchaProbes[userID]
	if !ok {
		return nil
	}
	return &probe
}

// Repeated POSTs return the in-flight job. The cooldown survives restarts,
// including unsuccessful probes. Interrupted jobs are never replayed.
func (s *Store) BeginCaptchaProbe(userID string) (model.CaptchaProbe, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(userID)
	if i < 0 || s.data.Users[i].Status != "active" {
		return model.CaptchaProbe{}, false, errors.New("用户未激活或已删除")
	}
	now := time.Now()
	previous, exists := s.data.CaptchaProbes[userID]
	if previous.Status == "running" {
		return previous, false, nil
	}
	if previous.RetryAt.After(now) {
		return previous, false, ErrCaptchaProbeCooldown
	}
	probe := model.CaptchaProbe{ID: randomID("probe_"), Status: "running", StartedAt: now, RetryAt: now.Add(time.Minute), Message: "正在后台获取匿名验证码并测试自定义服务，可离开此页面。"}
	if s.data.CaptchaProbes == nil {
		s.data.CaptchaProbes = map[string]model.CaptchaProbe{}
	}
	s.data.CaptchaProbes[userID] = probe
	if err := s.saveLocked(); err != nil {
		if exists {
			s.data.CaptchaProbes[userID] = previous
		} else {
			delete(s.data.CaptchaProbes, userID)
		}
		return model.CaptchaProbe{}, false, err
	}
	return probe, true, nil
}

func (s *Store) FinishCaptchaProbe(userID, id, status, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	probe, ok := s.data.CaptchaProbes[userID]
	if !ok || probe.ID != id || probe.Status != "running" {
		return nil
	}
	s.finishCaptchaProbeLocked(userID, probe, status, message)
	return s.saveLocked()
}

func (s *Store) finishCaptchaProbeLocked(userID string, probe model.CaptchaProbe, status, message string) {
	probe.Status, probe.Message, probe.FinishedAt = status, message, time.Now()
	probe.DurationMS = probe.FinishedAt.Sub(probe.StartedAt).Milliseconds()
	s.data.CaptchaProbes[userID] = probe
	s.data.Logs = append(s.data.Logs, model.LogEntry{At: probe.FinishedAt, UserID: userID, Component: "captcha", Message: "自定义服务后台测试 · " + message})
	if len(s.data.Logs) > 500 {
		s.data.Logs = s.data.Logs[len(s.data.Logs)-500:]
	}
}

func (s *Store) RecoverCaptchaProbes() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for userID, probe := range s.data.CaptchaProbes {
		if probe.Status == "running" {
			s.finishCaptchaProbeLocked(userID, probe, "interrupted", "服务重启，测试已中断；未自动重试，请在冷却结束后手动测试。")
			changed = true
		}
	}
	if changed {
		return s.saveLocked()
	}
	return nil
}

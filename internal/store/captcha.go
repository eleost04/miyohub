package store

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/eleost04/miyohub/internal/captcha"
	"github.com/eleost04/miyohub/internal/model"
)

var ErrCaptchaRevision = errors.New("打码配置已被修改，请重新加载后保存")

type CaptchaSettings struct {
	model.UserCaptchaConfig
	SiteAllowed   bool                   `json:"site_allowed"`
	SiteAvailable bool                   `json:"site_available"`
	Activity      []model.CaptchaAttempt `json:"activity"`
}

func PublicCaptcha(config model.CaptchaConfig) model.CaptchaConfig {
	config = clone(config)
	for i := range config.Channels {
		c := &config.Channels[i]
		c.Configured = []string{}
		if c.UserKey != "" {
			c.Configured = append(c.Configured, "userkey")
		}
		if c.Token != "" {
			c.Configured = append(c.Configured, "token")
		}
		c.UserKey, c.Token, c.ClearToken = "", "", false
	}
	return config
}

func validateCaptchaConfig(next *model.CaptchaConfig, old model.CaptchaConfig, personal bool) error {
	if next.MaxRetries < 0 || next.MaxRetries > 10 {
		return errors.New("验证码重试次数应在 0–10 之间")
	}
	if len(next.Channels) > 10 {
		return errors.New("验证码渠道不能超过 10 个")
	}
	for i := range next.Channels {
		ch := &next.Channels[i]
		ch.Endpoint, ch.Provider = strings.TrimSpace(ch.Endpoint), strings.TrimSpace(ch.Provider)
		ch.UserKey, ch.Token = strings.TrimSpace(ch.UserKey), strings.TrimSpace(ch.Token)
	}
	before, after := model.Config{Captcha: old}, model.Config{Captcha: *next}
	preserveConfigSecrets(&before, &after)
	*next = after.Captcha
	seen := map[string]bool{}
	for i := range next.Channels {
		ch := &next.Channels[i]
		if ch.ID == "" {
			ch.ID = randomID("captcha_")
		}
		if seen[ch.ID] {
			return errors.New("验证码渠道 ID 重复")
		}
		seen[ch.ID] = true
		if err := captcha.ValidateChannel(*ch); err != nil {
			return err
		}
		if personal {
			if err := captcha.ValidatePersonalChannel(*ch); err != nil {
				return err
			}
		}
		ch.Configured, ch.ClearToken = nil, false
	}
	if next.Channels == nil {
		next.Channels = []model.CaptchaChannel{}
	}
	return nil
}

func (s *Store) userCaptchaLocked(userID string) model.UserCaptchaConfig {
	if p, ok := s.data.UserCaptcha[userID]; ok {
		return p
	}
	// Existing administrators retain use of their deployed site service. New
	// ordinary users are opt-in and cannot inherit the operator's paid keys.
	source := "off"
	if i := s.userIndexLocked(userID); i >= 0 && isAdmin(s.data.Users[i]) {
		source = "site"
	}
	return model.UserCaptchaConfig{Source: source, Revision: 1, MaxRetries: 3, Channels: []model.CaptchaChannel{}}
}

func (s *Store) CaptchaSettingsForUser(userID string) CaptchaSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := clone(s.userCaptchaLocked(userID))
	p.Channels = PublicCaptcha(model.CaptchaConfig{Channels: p.Channels}).Channels
	i := s.userIndexLocked(userID)
	view := CaptchaSettings{UserCaptchaConfig: p, SiteAllowed: i >= 0 && s.data.Users[i].CanUseSiteCaptcha()}
	view.Activity = append([]model.CaptchaAttempt{}, s.data.CaptchaActivity[userID]...)
	for _, ch := range s.data.Config.Captcha.Channels {
		if ch.Enabled && s.data.Config.Captcha.MaxRetries > 0 {
			view.SiteAvailable = true
		}
	}
	return view
}

func (s *Store) UpdateUserCaptcha(userID string, p model.UserCaptchaConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(userID)
	if i < 0 || s.data.Users[i].Status != "active" {
		return errors.New("用户未激活或已删除")
	}
	if p.Source != "off" && p.Source != "personal" && p.Source != "site" {
		return errors.New("打码来源无效")
	}
	if p.Source == "site" && !s.data.Users[i].CanUseSiteCaptcha() {
		return ErrSiteCaptchaPermission
	}
	previous := s.userCaptchaLocked(userID)
	if p.Revision != previous.Revision {
		return ErrCaptchaRevision
	}
	p = clone(p)
	next := model.CaptchaConfig{MaxRetries: p.MaxRetries, Channels: p.Channels}
	if err := validateCaptchaConfig(&next, model.CaptchaConfig{Channels: previous.Channels}, true); err != nil {
		return err
	}
	p.MaxRetries, p.Channels, p.Revision = next.MaxRetries, next.Channels, previous.Revision+1
	if s.data.UserCaptcha == nil {
		s.data.UserCaptcha = map[string]model.UserCaptchaConfig{}
	}
	s.data.UserCaptcha[userID] = p
	return s.saveLocked()
}

func (s *Store) resolvedCaptchaLocked(userID string) (model.CaptchaConfig, int, error) {
	i := s.userIndexLocked(userID)
	if i < 0 || s.data.Users[i].Status != "active" {
		return model.CaptchaConfig{}, 0, errors.New("用户已停用")
	}
	p := s.userCaptchaLocked(userID)
	var cfg model.CaptchaConfig
	switch p.Source {
	case "site":
		if !s.data.Users[i].CanUseSiteCaptcha() {
			return cfg, p.Revision, ErrSiteCaptchaPermission
		}
		cfg = clone(s.data.Config.Captcha)
		cfg.MaxRetries = min(cfg.MaxRetries, p.MaxRetries)
	case "personal":
		cfg = model.CaptchaConfig{MaxRetries: p.MaxRetries, Channels: clone(p.Channels), PublicOnly: true}
	default:
		cfg = model.CaptchaConfig{Channels: []model.CaptchaChannel{}}
	}
	if cfg.MaxRetries <= 0 {
		cfg.Channels = []model.CaptchaChannel{}
	}
	return cfg, p.Revision, nil
}

// The callback is rechecked immediately before every solver HTTP request. A
// queued run cannot keep using a revoked grant or replaced service credentials.
func (s *Store) CaptchaForUser(userID string) model.CaptchaConfig {
	s.mu.RLock()
	cfg, revision, err := s.resolvedCaptchaLocked(userID)
	source := s.userCaptchaLocked(userID).Source
	s.mu.RUnlock()
	before := cfg
	cfg.Allowed = func() bool {
		s.mu.RLock()
		defer s.mu.RUnlock()
		current, latestRevision, latestErr := s.resolvedCaptchaLocked(userID)
		return err == nil && latestErr == nil && revision == latestRevision && reflect.DeepEqual(before, current)
	}
	cfg.Observe = func(attempt model.CaptchaAttempt) {
		attempt.Source = source
		_ = s.recordCaptchaAttempt(userID, attempt)
	}
	return cfg
}

func (s *Store) recordCaptchaAttempt(userID string, attempt model.CaptchaAttempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.userIndexLocked(userID) < 0 {
		return nil
	}
	if s.data.CaptchaActivity == nil {
		s.data.CaptchaActivity = map[string][]model.CaptchaAttempt{}
	}
	attempt.At = time.Now()
	items := append(s.data.CaptchaActivity[userID], attempt)
	if len(items) > 50 {
		items = items[len(items)-50:]
	}
	s.data.CaptchaActivity[userID] = items
	provider, result, purpose := "自定义服务", "识别失败", "任务验证"
	if attempt.Provider == "damagou" {
		provider = "打码狗"
	}
	if attempt.OK {
		result = "已返回识别结果，最终验证以任务结果为准"
	}
	if attempt.Kind == "test" {
		purpose = "匿名测试（未执行签到）"
	}
	s.data.Logs = append(s.data.Logs, model.LogEntry{At: attempt.At, UserID: userID, Component: "captcha", Message: fmt.Sprintf("%s · %s · %s · %d ms", purpose, provider, result, attempt.DurationMS)})
	if len(s.data.Logs) > 500 {
		s.data.Logs = s.data.Logs[len(s.data.Logs)-500:]
	}
	return s.saveLocked()
}

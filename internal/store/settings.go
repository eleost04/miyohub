package store

import (
	"encoding/json"
	"errors"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/scheduler"
	"math"
)

func clone[T any](v T) T {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		panic(err)
	}
	return result
}

// SettingsPatch deliberately excludes accounts, plans, devices and push secrets.
type SettingsPatch struct {
	Enabled  *bool                `json:"enabled"`
	Captcha  *model.CaptchaConfig `json:"captcha"`
	Schedule *model.Schedule      `json:"schedule"`
	Network  *model.NetworkConfig `json:"network"`
	Shop     *struct {
		Enabled       bool    `json:"enable"`
		RetrySeconds  float64 `json:"retry_seconds"`
		RetryInterval float64 `json:"retry_interval"`
	} `json:"shop_exchange"`
}

func (s *Store) UpdateSettings(p SettingsPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.data.Config)
	if p.Enabled != nil {
		next.Enabled = *p.Enabled
	}
	if p.Captcha != nil {
		next.Captcha = clone(*p.Captcha)
	}
	if p.Schedule != nil {
		next.Schedule = *p.Schedule
	}
	if p.Network != nil {
		next.Network = clone(*p.Network)
	}
	if retries := next.Network.BBSStateRetries; retries != nil && (*retries < 0 || *retries > 10) {
		return errors.New("米游币状态查询重试次数应在 0–10 之间")
	}
	if p.Shop != nil {
		next.Shop.Enabled = p.Shop.Enabled
		next.Shop.RetrySeconds = p.Shop.RetrySeconds
		next.Shop.RetryInterval = p.Shop.RetryInterval
	}
	if next.Schedule.Timezone == "" {
		next.Schedule.Timezone = model.DefaultTimezone
	}
	if err := scheduler.Validate(next.Schedule); err != nil {
		return err
	}
	if next.Schedule.JitterMins < 0 || next.Schedule.JitterMins > 720 {
		return errors.New("随机延迟应在 0–720 分钟之间")
	}
	if err := validateCaptchaConfig(&next.Captcha, s.data.Config.Captcha, false); err != nil {
		return err
	}
	if math.IsNaN(next.Shop.RetrySeconds) || math.IsInf(next.Shop.RetrySeconds, 0) || next.Shop.RetrySeconds < 0 || next.Shop.RetrySeconds > 120 || next.Shop.RetryInterval < .05 || next.Shop.RetryInterval > 30 {
		return errors.New("兑换重试窗口应为 0–120 秒，间隔应为 0.05–30 秒")
	}
	s.data.Config = clone(next)
	normalizeState(&s.data)
	return s.saveLocked()
}

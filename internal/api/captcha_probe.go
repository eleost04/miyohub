package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/eleost04/miyohub/internal/captcha"
	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

type captchaProbes struct {
	mu      sync.Mutex
	wg      sync.WaitGroup
	cancels map[string]context.CancelFunc
	stopped bool
}

func (p *captchaProbes) stop() {
	p.mu.Lock()
	p.stopped = true
	for _, cancel := range p.cancels {
		cancel()
	}
	p.mu.Unlock()
	p.wg.Wait()
}
func (p *captchaProbes) cancelUser(userID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if cancel := p.cancels[userID]; cancel != nil {
		cancel()
	}
}

// GET only reads owner-scoped state; POST starts one anonymous, custom-only
// test. Neither endpoint can sign in, send SMS/push, exchange or use Damagou.
func (s *Server) captchaTest(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method == http.MethodGet {
		writeJSON(w, 200, map[string]any{"ok": true, "data": map[string]any{"probe": s.store.CaptchaProbeForUser(user.ID)}})
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct{}
	if !decodeJSON(w, r, &input) {
		return
	}
	cfg := s.store.CaptchaForUser(user.ID)
	if cfg.Allowed != nil && !cfg.Allowed() {
		writeError(w, 403, errors.New("当前打码来源未获授权，请先检查个人打码配置"))
		return
	}
	var selected *model.CaptchaChannel
	for _, ch := range cfg.Channels {
		if ch.Enabled && ch.Provider == "custom" {
			selected = &ch
			break
		}
	}
	if selected == nil || cfg.MaxRetries <= 0 {
		writeError(w, 400, errors.New("请先保存并启用一个自定义渠道；此测试不会调用打码狗"))
		return
	}
	s.probes.mu.Lock()
	defer s.probes.mu.Unlock()
	if s.probes.stopped {
		writeError(w, 503, errors.New("服务正在停止，请稍后重试"))
		return
	}
	probe, created, err := s.store.BeginCaptchaProbe(user.ID)
	if err != nil {
		if errors.Is(err, store.ErrCaptchaProbeCooldown) {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(time.Until(probe.RetryAt).Seconds())+1)))
			writeError(w, 429, err)
		} else {
			writeError(w, 500, errors.New("无法保存测试状态，尚未调用打码服务"))
		}
		return
	}
	if created {
		ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
		if s.probes.cancels == nil {
			s.probes.cancels = map[string]context.CancelFunc{}
		}
		s.probes.cancels[user.ID] = cancel
		cfg.Channels, cfg.Purpose = []model.CaptchaChannel{*selected}, "test"
		s.probes.wg.Add(1)
		go func() {
			defer s.probes.wg.Done()
			defer cancel()
			status, message := s.runCaptchaProbe(ctx, cfg)
			s.probes.mu.Lock()
			defer s.probes.mu.Unlock()
			_ = s.store.FinishCaptchaProbe(user.ID, probe.ID, status, message)
			delete(s.probes.cancels, user.ID)
		}()
	}
	writeJSON(w, 202, map[string]any{"ok": true, "data": map[string]any{"probe": probe}})
}

func (s *Server) runCaptchaProbe(ctx context.Context, cfg model.CaptchaConfig) (string, string) {
	client := *s.shopClient
	transport := *client.HTTP
	transport.Jar = nil
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client.HTTP = &transport
	var challenge struct {
		Retcode int `json:"retcode"`
		Data    struct {
			GT        string `json:"gt"`
			Challenge string `json:"challenge"`
		} `json:"data"`
	}
	if err := client.JSON(ctx, http.MethodGet, mihoyo.BBSAPI+"/misc/api/createVerification", url.Values{"is_high": {"true"}}, nil, nil, &challenge); err != nil {
		return probeFailure(ctx, "获取匿名验证码失败，尚未调用打码服务；请检查服务器到米游社的网络。")
	}
	if challenge.Retcode != 0 || challenge.Data.GT == "" || challenge.Data.Challenge == "" {
		return "failed", "上游未提供有效的匿名验证码，尚未调用打码服务。"
	}
	if _, err := captcha.SolveConfigured(ctx, client.HTTP, cfg, challenge.Data.GT, challenge.Data.Challenge, nil); err != nil {
		if errors.Is(err, captcha.ErrAuthorization) {
			return "failed", "打码配置或授权已变化，测试未完成；请检查当前配置。"
		}
		// Upstream errors may echo secrets or query strings. Persist fixed messages only.
		return probeFailure(ctx, "测试未通过：自定义服务未返回有效校验参数。请检查服务日志、网络和超时设置。")
	}
	return "succeeded", "测试成功：自定义服务已返回校验参数。未执行账号任务，实际验证结果仍以签到或登录结果为准。"
}

func probeFailure(ctx context.Context, fallback string) (string, string) {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "failed", "测试超时（最长 65 秒），未自动重试；请检查上游网络和打码服务。"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "interrupted", "服务停止或用户权限发生变化，测试已中断；未自动重试。"
	}
	return "failed", fallback
}

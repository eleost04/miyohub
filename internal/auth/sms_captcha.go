package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func smsPublicState(current *smsSession) SMSState {
	state := current.state
	if state.Challenge != nil {
		challenge := *state.Challenge
		state.Challenge = &challenge
	}
	return state
}

func (m *SMSManager) State(userID string) SMSState {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.sessions[userID]
	if current == nil {
		return SMSState{Status: "idle"}
	}
	if current.pending != nil && !current.pending.public.ExpiresAt.After(time.Now()) {
		current.pending, current.state.Challenge = nil, nil
		current.state.Status, current.state.Message = "failed", "人机验证已过期，请重新获取短信验证码。"
		if current.action != "" {
			current.state.Status, current.state.Message = "sent", "人机验证已过期，请重新提交短信验证码。"
		}
	}
	return smsPublicState(current)
}

func (m *SMSManager) failSend(userID string, current *smsSession) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[userID] == current {
		current.pending, current.state.Challenge = nil, nil
		current.state.Status, current.state.Message = "failed", "发送未完成或结果未确认，请等待冷却结束后重试。"
	}
}

func (m *SMSManager) requireCaptcha(ctx context.Context, userID string, current *smsSession, path string, body map[string]any, raw, message string) error {
	var envelope map[string]any
	if json.Unmarshal([]byte(raw), &envelope) != nil {
		return errors.New("安全验证参数异常，请重新获取或使用扫码登录")
	}
	payload := dataMap(envelope["data"])
	if nested, ok := envelope["data"].(string); ok {
		_ = json.Unmarshal([]byte(nested), &payload)
	}
	if len(payload) == 0 {
		payload = dataMap(envelope["mmt_data"])
	}
	gt, challenge, sessionID := text(payload["gt"], ""), text(payload["challenge"], ""), text(envelope["session_id"], "")
	if !captchaAnswerPattern.MatchString(gt) || !captchaAnswerPattern.MatchString(challenge) || sessionID == "" || len(sessionID) > 512 || strings.ContainsAny(sessionID, ";\r\n\x00") {
		return errors.New("上游未返回受支持的人机验证参数，请重新获取或使用扫码登录")
	}
	rawID := make([]byte, 16)
	if _, err := rand.Read(rawID); err != nil {
		return errors.New("无法创建安全验证会话")
	}
	operation := "send"
	if path == smsVerifyPath {
		operation = "verify"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[userID] != current || ctx.Err() != nil {
		return errors.New("短信登录已取消")
	}
	if current.captchaAttempts >= 5 {
		return errors.New("人机验证尝试次数过多，请重新获取短信验证码")
	}
	current.captchaAttempts++
	public := SMSChallenge{ID: hex.EncodeToString(rawID), GT: gt, Challenge: challenge, NewCaptcha: true, ExpiresAt: time.Now().Add(2 * time.Minute), Operation: operation}
	if value, ok := payload["new_captcha"].(bool); ok {
		public.NewCaptcha = value
	}
	current.pending = &smsPending{public: public, sessionID: sessionID, path: path, body: body}
	current.state.Status, current.state.Message, current.state.Challenge = "captcha_required", message, &public
	return ErrSMSCaptchaRequired
}

// A short-lived random ID binds the solution to this user, phone, device and
// original server-owned request. Browser input can never select a URL/session,
// change the phone, request a login for another owner, or replay a solution.
func (m *SMSManager) CompleteCaptcha(parent context.Context, userID string, solution SMSCaptchaSolution) (SMSState, error) {
	if !captchaAnswerPattern.MatchString(solution.ID) || !captchaAnswerPattern.MatchString(solution.Challenge) || !captchaAnswerPattern.MatchString(solution.Validate) {
		return m.State(userID), errors.New("人机验证结果格式无效，请重新验证")
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	m.mu.Lock()
	current := m.sessions[userID]
	if m.stopped || current == nil || current.pending == nil || current.pending.public.ID != solution.ID || !current.pending.public.ExpiresAt.After(time.Now()) || !current.state.ExpiresAt.After(time.Now()) {
		m.mu.Unlock()
		return m.State(userID), errors.New("安全验证会话无效或已过期，请重新获取")
	}
	if current.busy {
		m.mu.Unlock()
		return m.State(userID), errors.New("正在提交验证，请勿重复操作")
	}
	pending := current.pending
	current.pending, current.state.Challenge = nil, nil
	current.busy, current.cancel = true, cancel
	current.state.Status, current.state.Message = "verifying", "正在提交人机验证结果…"
	m.mu.Unlock()
	defer func() { m.mu.Lock(); current.busy, current.cancel = false, nil; m.mu.Unlock() }()
	answer, _ := json.Marshal(map[string]string{"geetest_challenge": solution.Challenge, "geetest_validate": solution.Validate, "geetest_seccode": solution.Validate + "|jordan"})
	data, err := m.request(ctx, userID, current, pending.path, pending.body, pending.sessionID+";"+base64.StdEncoding.EncodeToString(answer))
	if err != nil {
		if errors.Is(err, ErrSMSCaptchaRequired) {
			return m.State(userID), nil
		}
		m.mu.Lock()
		current.state.Status, current.state.Message = "failed", "验证未完成，请重新获取短信验证码。"
		if current.action != "" {
			current.state.Status, current.state.Message = "sent", "验证未完成，请重新提交短信验证码。"
		}
		m.mu.Unlock()
		return m.State(userID), err
	}
	if pending.path == smsSendPath {
		return m.finishSend(ctx, userID, current, data)
	}
	if err := m.finishVerify(ctx, userID, current, data); err != nil {
		return m.State(userID), err
	}
	return SMSState{Status: "verified", Message: "米游社账号已绑定"}, nil
}

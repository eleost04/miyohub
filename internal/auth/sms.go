package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/eleost04/miyohub/internal/captcha"
	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

const smsSendPath = "/account/ma-cn-verifier/verifier/createLoginCaptcha"
const smsVerifyPath = "/account/ma-cn-passport/app/loginByMobileCaptcha"

var phonePattern = regexp.MustCompile(`^1[3-9][0-9]{9}$`)
var codePattern = regexp.MustCompile(`^[0-9]{4,8}$`)

type SMSState struct {
	Status    string        `json:"status"`
	Message   string        `json:"message"`
	Phone     string        `json:"phone"`
	RetryAt   time.Time     `json:"retry_at"`
	ExpiresAt time.Time     `json:"expires_at"`
	Challenge *SMSChallenge `json:"challenge,omitempty"`
}

type SMSChallenge struct {
	ID         string    `json:"id"`
	Version    int       `json:"version"`
	GT         string    `json:"gt"`
	Challenge  string    `json:"challenge,omitempty"`
	RiskType   string    `json:"risk_type,omitempty"`
	SessionID  string    `json:"session_id,omitempty"`
	NewCaptcha bool      `json:"new_captcha"`
	ExpiresAt  time.Time `json:"expires_at"`
	Operation  string    `json:"operation"`
}
type SMSCaptchaSolution struct {
	ID            string `json:"id"`
	Challenge     string `json:"challenge,omitempty"`
	Validate      string `json:"validate,omitempty"`
	CaptchaID     string `json:"captcha_id,omitempty"`
	LotNumber     string `json:"lot_number,omitempty"`
	CaptchaOutput string `json:"captcha_output,omitempty"`
	PassToken     string `json:"pass_token,omitempty"`
	GenTime       string `json:"gen_time,omitempty"`
}
type smsPending struct {
	public          SMSChallenge
	sessionID, path string
	body            map[string]any
}

var ErrSMSCaptchaRequired = errors.New("请先完成人机验证")
var captchaAnswerPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`)

type smsSession struct {
	state                       SMSState
	phone, name, target, action string
	device                      model.Device
	busy                        bool
	attempts                    int
	cancel                      context.CancelFunc
	manual                      bool
	pending                     *smsPending
	captchaAttempts             int
}
type SMSManager struct {
	store    *store.Store
	client   *mihoyo.Client
	mu       sync.Mutex
	sessions map[string]*smsSession
	phones   map[string]time.Time
	stopped  bool
}

func NewSMSManager(s *store.Store) *SMSManager {
	return &SMSManager{store: s, client: mihoyo.NewClient("", s.NetworkConfig), sessions: map[string]*smsSession{}, phones: map[string]time.Time{}}
}

func (m *SMSManager) Send(parent context.Context, userID, phone, name, target string, verificationMode ...string) (SMSState, error) {
	mode := "manual"
	if len(verificationMode) > 0 && verificationMode[0] != "" {
		mode = verificationMode[0]
	}
	if mode != "manual" && mode != "auto" {
		return SMSState{}, errors.New("请选择手动验证或已配置的打码服务")
	}
	phone = strings.TrimSpace(phone)
	phone = strings.TrimPrefix(strings.TrimPrefix(phone, "+86"), "0086")
	name = strings.TrimSpace(name)
	if !phonePattern.MatchString(phone) {
		return SMSState{}, errors.New("请输入有效的中国大陆手机号")
	}
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return SMSState{}, errors.New("请输入 1–64 个字符的账号名称")
	}
	if target != "" {
		a, ok := m.store.AccountForUser(userID, false, target)
		if !ok {
			return SMSState{}, errors.New("只能重新绑定自己的账号")
		}
		name = a.Name
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	now := time.Now()
	current := &smsSession{phone: phone, name: name, target: target, manual: mode == "manual", busy: true, cancel: cancel, device: model.Device{ID: mihoyo.DeviceID(), FP: mihoyo.DeviceFP(), Name: "MiyoHub", Model: "MiyoHub"}, state: SMSState{Status: "sending", Phone: phone[:3] + "****" + phone[7:], RetryAt: now.Add(time.Minute), ExpiresAt: now.Add(10 * time.Minute)}}
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return SMSState{}, errors.New("服务正在停止")
	}
	if old := m.sessions[userID]; old != nil {
		if old.busy {
			m.mu.Unlock()
			return SMSState{}, errors.New("正在处理短信请求，请稍候")
		}
		if old.pending != nil && old.pending.public.ExpiresAt.After(now) {
			state := smsPublicState(old)
			m.mu.Unlock()
			return state, nil
		}
		if old.state.RetryAt.After(now) {
			message := "上次发送未完成，请等待冷却结束后重试"
			if old.action != "" {
				message = "短信验证码已发送，请等待倒计时结束后重发"
			}
			state := smsPublicState(old)
			m.mu.Unlock()
			return state, errors.New(message)
		}
	}
	for key, until := range m.phones {
		if !until.After(now) {
			delete(m.phones, key)
		}
	}
	for key, s := range m.sessions {
		if !s.busy && s.state.ExpiresAt.Before(now) {
			delete(m.sessions, key)
		}
	}
	if m.phones[phone].After(now) {
		m.mu.Unlock()
		return SMSState{}, errors.New("该手机号发送频繁，请稍后再试")
	}
	m.phones[phone] = current.state.RetryAt
	m.sessions[userID] = current
	m.mu.Unlock()
	defer func() { m.mu.Lock(); current.busy = false; current.cancel = nil; m.mu.Unlock() }()
	body, err := smsBody(phone)
	if err != nil {
		return SMSState{}, err
	}
	data, err := m.request(ctx, userID, current, smsSendPath, body, "")
	if err != nil {
		if errors.Is(err, ErrSMSCaptchaRequired) {
			return m.State(userID), nil
		}
		m.failSend(userID, current)
		return m.State(userID), err
	}
	return m.finishSend(ctx, userID, current, data)
}

func (m *SMSManager) finishSend(ctx context.Context, userID string, current *smsSession, data map[string]any) (SMSState, error) {
	action := text(data["action_type"], "")
	if action == "" {
		m.failSend(userID, current)
		return m.State(userID), errors.New("短信接口没有返回验证信息，请稍后重试")
	}
	seconds, _ := strconv.Atoi(text(data["countdown"], "60"))
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[userID] != current || ctx.Err() != nil {
		return SMSState{}, errors.New("短信登录已取消")
	}
	current.action = action
	current.pending = nil
	current.state.Status, current.state.Challenge = "sent", nil
	current.state.Message = "短信验证码已发送至 " + current.state.Phone
	current.state.ExpiresAt = time.Now().Add(10 * time.Minute)
	current.state.RetryAt = time.Now().Add(time.Duration(max(60, min(300, seconds))) * time.Second)
	m.phones[current.phone] = current.state.RetryAt
	return smsPublicState(current), nil
}

func (m *SMSManager) Verify(parent context.Context, userID, code string) error {
	if !codePattern.MatchString(strings.TrimSpace(code)) {
		return errors.New("请输入短信中的数字验证码")
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	m.mu.Lock()
	current := m.sessions[userID]
	if m.stopped || current == nil || current.action == "" || current.state.ExpiresAt.Before(time.Now()) {
		m.mu.Unlock()
		return errors.New("请先重新获取短信验证码")
	}
	if current.busy {
		m.mu.Unlock()
		return errors.New("正在处理登录，请稍候")
	}
	if current.pending != nil && current.pending.public.ExpiresAt.After(time.Now()) {
		m.mu.Unlock()
		return ErrSMSCaptchaRequired
	}
	if current.attempts >= 5 {
		m.mu.Unlock()
		return errors.New("尝试次数过多，请重新获取验证码")
	}
	current.busy, current.cancel = true, cancel
	current.attempts++
	phone, action := current.phone, current.action
	m.mu.Unlock()
	defer func() { m.mu.Lock(); current.busy = false; current.cancel = nil; m.mu.Unlock() }()
	body, err := smsBody(phone)
	if err != nil {
		return err
	}
	body["captcha"], body["action_type"] = strings.TrimSpace(code), action
	data, err := m.request(ctx, userID, current, smsVerifyPath, body, "")
	if err != nil {
		return err
	}
	return m.finishVerify(ctx, userID, current, data)
}

func (m *SMSManager) finishVerify(ctx context.Context, userID string, current *smsSession, data map[string]any) error {
	user, token := dataMap(data["user_info"]), dataMap(data["token"])
	a, err := loginAccount(ctx, m.client, current.device, current.name, text(user["aid"], ""), text(token["token"], ""), text(user["mid"], ""))
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[userID] != current || ctx.Err() != nil {
		return errors.New("短信登录已取消")
	}
	if err := m.store.BindAccountForUser(userID, current.target, a); err != nil {
		return err
	}
	delete(m.sessions, userID)
	return nil
}

func (m *SMSManager) Cancel(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[userID]; s != nil {
		if s.cancel != nil {
			s.cancel()
		}
		delete(m.sessions, userID)
	}
}
func (m *SMSManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	for _, s := range m.sessions {
		if s.cancel != nil {
			s.cancel()
		}
	}
	m.sessions = map[string]*smsSession{}
}

func (m *SMSManager) request(ctx context.Context, userID string, current *smsSession, path string, body map[string]any, suppliedAnswer string) (map[string]any, error) {
	headers := qrHeaders(model.Config{Device: current.device})
	headers.Del("DS")
	headers.Set("x-rpc-client_type", "2")
	headers.Set("x-rpc-app_version", "2.106.2")
	headers.Set("User-Agent", mihoyo.DefaultMobileUA)
	if path == smsSendPath {
		headers.Set("Referer", "https://user.miyoushe.com/")
		headers.Set("x-rpc-game_biz", "hk4e_cn")
	}
	config := m.store.CaptchaForUser(userID)
	if suppliedAnswer != "" {
		headers.Set("x-rpc-aigis", suppliedAnswer)
	}
	for attempt := 0; ; attempt++ {
		var result map[string]any
		replyHeaders, err := m.client.JSONWithHeaders(ctx, http.MethodPost, passportAPI+path, nil, body, headers, &result)
		if err != nil {
			return nil, err
		}
		if retcode(result) == 0 {
			return dataMap(result["data"]), nil
		}
		aigis := replyHeaders.Get("x-rpc-aigis")
		if aigis == "" {
			return nil, fmt.Errorf("短信登录失败：%s (%d)", text(result["message"], "请稍后重试"), retcode(result))
		}
		verification, err := parseAigis(aigis)
		if err != nil {
			return nil, err
		}
		if verification.Version == 4 {
			// Configured /pass_nine providers implement V3, not V4. Do not
			// send an incomplete challenge to them or spend their quota.
			return nil, m.requireCaptcha(ctx, userID, current, path, body, aigis, "此安全验证需要在页面完成，验证后继续当前短信请求。")
		}
		if current.manual || suppliedAnswer != "" || attempt >= min(3, config.MaxRetries) {
			return nil, m.requireCaptcha(ctx, userID, current, path, body, aigis, "请在页面完成人机验证，验证通过后继续当前短信请求。")
		}
		answer, err := solveAigis(ctx, m.client.HTTP, config, aigis)
		if err != nil {
			return nil, m.requireCaptcha(ctx, userID, current, path, body, aigis, "自动识别未完成，请在页面手动验证后继续。")
		}
		headers.Set("x-rpc-aigis", answer)
	}
}

func solveAigis(ctx context.Context, client *http.Client, config model.CaptchaConfig, raw string) (string, error) {
	verification, err := parseAigis(raw)
	if err != nil {
		return "", err
	}
	if verification.Version != 3 {
		return "", errors.New("此安全验证需要在页面手动完成")
	}
	solution, err := captcha.SolveConfigured(ctx, client, config, verification.GT, verification.Challenge, nil)
	if err != nil {
		return "", err
	}
	answer, _ := json.Marshal(map[string]string{"geetest_challenge": solution.Challenge, "geetest_validate": solution.Validate, "geetest_seccode": solution.Validate + "|jordan"})
	return verification.SessionID + ";" + base64.StdEncoding.EncodeToString(answer), nil
}

func smsBody(phone string) (map[string]any, error) {
	area, err := passportEncrypt("+86")
	if err != nil {
		return nil, err
	}
	mobile, err := passportEncrypt(phone)
	return map[string]any{"area_code": area, "mobile": mobile}, err
}
func passportEncrypt(value string) (string, error) {
	modulus, _ := new(big.Int).SetString("c3bde91d3cc1cddc06219bfbe4b494fe609afb708e4372c34aa9db31e43657d200ee585b888f377006eb6b2183cd9912751bcc9b0c817ba035b6784a66e6c31b2fdcecf44c5709dbeaae7e75a842dbaa3d17c6d3132296821c5488e743df3e94c557d5edfe19b2570a24a0e5c59401200a7f900a01ace766c5a1832dca2fb111", 16)
	raw, err := rsa.EncryptPKCS1v15(rand.Reader, &rsa.PublicKey{N: modulus, E: 65537}, []byte(value))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

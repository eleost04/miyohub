package notify

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	qrcode "github.com/skip2/go-qrcode"
)

type BindingState struct {
	SessionID string    `json:"session_id"`
	Provider  string    `json:"provider"`
	ChannelID string    `json:"channel_id"`
	Revision  int       `json:"revision"`
	Running   bool      `json:"running"`
	Status    string    `json:"status"`
	QRImage   string    `json:"qr_image"`
	QRURL     string    `json:"qr_url"`
	Message   string    `json:"message"`
	ExpiresAt time.Time `json:"expires_at"`
}
type bindingSession struct {
	state        BindingState
	userID       string
	revision     int
	ctx          context.Context
	cancel       context.CancelFunc
	verify       chan string
	key          []byte
	ticket, base string
}
type BindingStore interface {
	BindPushChannel(string, string, int, model.PushChannel) (string, error)
}
type Bindings struct {
	OnBound      func()
	store        BindingStore
	sender       Sender
	mu           sync.Mutex
	sessions     map[string]*bindingSession
	stopped      bool
	wg           sync.WaitGroup
	pollInterval time.Duration
}

func NewBindings(store BindingStore, sender Sender) *Bindings {
	return &Bindings{store: store, sender: sender, sessions: map[string]*bindingSession{}, pollInterval: 2 * time.Second}
}

func (m *Bindings) State(userID string) BindingState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[userID]; s != nil {
		return s.state
	}
	return BindingState{Status: "idle"}
}
func (m *Bindings) Start(userID, provider, channelID string, revision int) (BindingState, error) {
	if provider != "qqbot" && provider != "wechat_claw" {
		return BindingState{}, errors.New("仅 QQ 官方机器人和微信支持此扫码入口")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return BindingState{}, errors.New("服务正在停止")
	}
	if old := m.sessions[userID]; old != nil {
		if old.state.Running && old.ctx.Err() == nil && time.Now().Before(old.state.ExpiresAt) && old.state.Provider == provider && old.state.ChannelID == channelID && old.revision == revision {
			// Reloading a page or retrying a lost HTTP response must not revoke
			// the task still open in the official mobile application.
			return old.state, nil
		}
		old.cancel()
	}
	// Bound memory use without allowing one user to cancel another user's login.
	for key, s := range m.sessions {
		if time.Now().After(s.state.ExpiresAt) {
			s.cancel()
			delete(m.sessions, key)
		}
	}
	if len(m.sessions) >= 200 && m.sessions[userID] == nil {
		return BindingState{}, errors.New("扫码会话繁忙，请稍后重试")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return BindingState{}, errors.New("无法创建安全扫码会话")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	s := &bindingSession{userID: userID, revision: revision, ctx: ctx, cancel: cancel, verify: make(chan string, 1), base: weixinBase, state: BindingState{SessionID: hex.EncodeToString(id[:]), Provider: provider, ChannelID: channelID, Revision: revision, Running: true, Status: "starting", Message: "正在获取官方二维码", ExpiresAt: time.Now().Add(5 * time.Minute)}}
	m.sessions[userID] = s
	m.wg.Add(1)
	go m.run(s)
	return s.state, nil
}
func (m *Bindings) Cancel(userID, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.sessions[userID]; s != nil && (sessionID == "" || s.state.SessionID == sessionID) {
		s.cancel()
		s.state.Running = false
		s.state.Status = "cancelled"
		s.state.Message = "扫码已取消"
		s.state.QRImage = ""
		s.state.QRURL = ""
		s.key = nil
	}
}

var verificationCode = regexp.MustCompile(`^[0-9]{1,12}$`)

func (m *Bindings) Verify(userID, sessionID, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.sessions[userID]
	if s == nil || s.state.SessionID != sessionID || s.ctx.Err() != nil || s.state.Status != "need_verifycode" {
		return errors.New("当前没有等待确认的微信扫码会话")
	}
	if !verificationCode.MatchString(code) {
		return errors.New("请填写手机微信显示的数字配对码")
	}
	select {
	case s.verify <- code:
		s.state.Status = "scanned"
		s.state.Message = "配对码已提交，正在确认"
	default:
		return errors.New("配对码正在确认，请稍候")
	}
	return nil
}
func (m *Bindings) Stop() {
	m.mu.Lock()
	m.stopped = true
	for _, s := range m.sessions {
		s.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
}
func (m *Bindings) update(s *bindingSession, status, message string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[s.userID] != s || s.ctx.Err() != nil {
		return false
	}
	s.state.Status = status
	s.state.Message = message
	return true
}
func (m *Bindings) showQR(s *bindingSession, link string) error {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || u.User != nil || len(link) > 4096 || !(u.Hostname() == "q.qq.com" || u.Hostname() == "weixin.qq.com" || strings.HasSuffix(u.Hostname(), ".weixin.qq.com")) {
		return errors.New("官方返回的二维码地址无效")
	}
	png, err := qrcode.Encode(link, qrcode.Medium, 288)
	if err != nil {
		return errors.New("二维码生成失败")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[s.userID] != s || s.ctx.Err() != nil {
		return context.Canceled
	}
	s.state.Status = "waiting"
	s.state.Message = "请使用手机扫码，并在官方页面确认绑定"
	s.state.QRURL = link
	s.state.QRImage = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	return nil
}
func (m *Bindings) run(s *bindingSession) {
	defer m.wg.Done()
	defer s.cancel()
	var channel model.PushChannel
	var err error
	if s.state.Provider == "qqbot" {
		channel, err = m.qq(s)
	} else {
		channel, err = m.weixin(s)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[s.userID] != s {
		return
	}
	s.key = nil
	s.ticket = ""
	s.state.QRImage = ""
	s.state.QRURL = ""
	s.state.Running = false
	if s.ctx.Err() != nil {
		if s.state.Status != "cancelled" {
			s.state.Status = "expired"
			s.state.Message = "二维码已过期或服务已停止，请重新扫码"
		}
		return
	}
	if err == nil {
		var id string
		id, err = m.store.BindPushChannel(s.userID, s.state.ChannelID, s.revision, channel)
		if err == nil {
			s.state.ChannelID = id
			s.state.Status = "confirmed"
			s.state.Message = "绑定成功；请在消息推送中开启自动通知"
			if channel.Provider == "qqbot" {
				s.state.Message = "QQ 机器人凭据已保存，正在建立官方连接。可返回渠道配置查看上线状态；自动通知由推送开关控制。"
			}
			if channel.Provider == "wechat_claw" {
				s.state.Message = "已绑定微信。请先给机器人发送一条消息，建立接收会话"
			}
			if channel.OpenID == "" {
				s.state.Message = "机器人凭据已绑定，但官方未返回接收者 OpenID；请补充 OpenID 后启用渠道"
			}
			if m.OnBound != nil {
				m.OnBound()
			}
			return
		}
	}
	s.state.Status = "error"
	s.state.Message = err.Error()
}
func (m *Bindings) qq(s *bindingSession) (model.PushChannel, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return model.PushChannel{}, errors.New("无法创建安全扫码会话")
	}
	// Keep the AES key local to this goroutine; cancellation cannot race its use.
	raw, err := m.sender.post(s.ctx, "https://q.qq.com/lite/create_bind_task", map[string]string{"key": base64.StdEncoding.EncodeToString(key)}, nil)
	if err != nil {
		return model.PushChannel{}, err
	}
	var created struct {
		Code *int `json:"retcode"`
		Data struct {
			ID string `json:"task_id"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &created) != nil || created.Code == nil || *created.Code != 0 || created.Data.ID == "" {
		return model.PushChannel{}, errors.New("QQ 官方未能创建扫码任务，请稍后重试")
	}
	link := "https://q.qq.com/qqbot/openclaw/connect.html?task_id=" + url.QueryEscape(created.Data.ID) + "&source=miyohub&_wv=2"
	if err = m.showQR(s, link); err != nil {
		return model.PushChannel{}, err
	}
	for s.ctx.Err() == nil {
		if !pause(s.ctx, m.pollInterval) {
			break
		}
		raw, err = m.sender.post(s.ctx, "https://q.qq.com/lite/poll_bind_result", map[string]string{"task_id": created.Data.ID}, nil)
		if err != nil {
			m.update(s, "waiting", "QQ 连接暂时中断，正在重试；二维码仍有时效")
			continue
		}
		var result struct {
			Code *int `json:"retcode"`
			Data struct {
				Status int             `json:"status"`
				AppID  json.RawMessage `json:"bot_appid"`
				Secret string          `json:"bot_encrypt_secret"`
				OpenID string          `json:"user_openid"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &result) != nil || result.Code == nil || *result.Code != 0 {
			return model.PushChannel{}, errors.New("QQ 官方返回异常，请重新扫码")
		}
		switch result.Data.Status {
		case 1:
			m.update(s, "scanned", "请在手机 QQ 官方页面创建或选择机器人，并确认连接")
		case 3:
			return model.PushChannel{}, errors.New("QQ 二维码已过期，请刷新后重试")
		case 2:
			secret, err := decryptQQSecret(result.Data.Secret, key)
			if err != nil {
				return model.PushChannel{}, err
			}
			var appID string
			if json.Unmarshal(result.Data.AppID, &appID) != nil {
				var n json.Number
				if json.Unmarshal(result.Data.AppID, &n) != nil {
					return model.PushChannel{}, errors.New("QQ 返回了无效 AppID")
				}
				appID = n.String()
			}
			return model.PushChannel{Provider: "qqbot", AppID: appID, ClientSecret: secret, OpenID: result.Data.OpenID, BindingState: "connecting"}, nil
		}
	}
	return model.PushChannel{}, s.ctx.Err()
}
func decryptQQSecret(encrypted string, key []byte) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil || len(raw) < 28 || len(key) != 32 {
		return "", errors.New("QQ 绑定密文无效，请重新扫码")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", errors.New("QQ 绑定密钥无效")
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", errors.New("QQ 绑定解密失败")
	}
	plain, err := gcm.Open(nil, raw[:12], raw[12:], nil)
	if err != nil || len(plain) == 0 || len(plain) > 4096 || strings.ContainsAny(string(plain), "\r\n\x00") {
		return "", errors.New("QQ 绑定凭据校验失败，请重新扫码")
	}
	return string(plain), nil
}
func (m *Bindings) weixin(s *bindingSession) (model.PushChannel, error) {
	raw, err := m.sender.post(s.ctx, weixinBase+"/ilink/bot/get_bot_qrcode?bot_type=3", map[string]any{"local_token_list": []string{}}, weixinHeaders(""))
	if err != nil {
		return model.PushChannel{}, err
	}
	var created struct {
		QR  string `json:"qrcode"`
		URL string `json:"qrcode_img_content"`
	}
	if json.Unmarshal(raw, &created) != nil || created.QR == "" || len(created.QR) > 4096 {
		return model.PushChannel{}, errors.New("微信官方未返回有效二维码，请稍后重试")
	}
	if err = m.showQR(s, created.URL); err != nil {
		return model.PushChannel{}, err
	}
	base, code := weixinBase, ""
	for s.ctx.Err() == nil {
		endpoint := base + "/ilink/bot/get_qrcode_status?qrcode=" + url.QueryEscape(created.QR)
		if code != "" {
			endpoint += "&verify_code=" + url.QueryEscape(code)
		}
		callCtx, cancel := context.WithTimeout(s.ctx, 35*time.Second)
		raw, err = m.sender.request(callCtx, http.MethodGet, endpoint, nil, weixinHeaders(""))
		cancel()
		if s.ctx.Err() != nil {
			break
		}
		if err != nil {
			if !pause(s.ctx, m.pollInterval) {
				break
			}
			continue
		}
		var result struct {
			Status   string `json:"status"`
			Token    string `json:"bot_token"`
			BotID    string `json:"ilink_bot_id"`
			BaseURL  string `json:"baseurl"`
			UserID   string `json:"ilink_user_id"`
			Redirect string `json:"redirect_host"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return model.PushChannel{}, errors.New("微信扫码状态响应无效")
		}
		switch result.Status {
		case "wait":
			m.update(s, "waiting", "等待微信扫码")
		case "scaned":
			code = ""
			m.update(s, "scanned", "已扫码，请在手机微信上确认连接")
		case "need_verifycode":
			message := "请输入手机微信显示的数字配对码"
			if code != "" {
				message = "配对码不匹配，请重新输入手机微信显示的数字"
			}
			if !m.update(s, "need_verifycode", message) {
				return model.PushChannel{}, context.Canceled
			}
			select {
			case code = <-s.verify:
				continue
			case <-s.ctx.Done():
				return model.PushChannel{}, s.ctx.Err()
			}
		case "verify_code_blocked":
			return model.PushChannel{}, errors.New("微信配对码尝试次数已达限制，请稍后重新扫码")
		case "expired":
			return model.PushChannel{}, errors.New("微信二维码已过期，请刷新重试")
		case "binded_redirect":
			return model.PushChannel{}, errors.New("此微信机器人已连接，请检查原绑定渠道；需要迁移时请先在微信解除原连接")
		case "scaned_but_redirect":
			candidate := "https://" + result.Redirect
			if !WeixinEndpoint(candidate) {
				return model.PushChannel{}, errors.New("微信返回了非可信的跳转地址，已停止绑定")
			}
			base = strings.TrimRight(candidate, "/")
		case "confirmed":
			if result.BaseURL == "" {
				result.BaseURL = base
			}
			if result.Token == "" || result.BotID == "" || result.UserID == "" || !WeixinEndpoint(result.BaseURL) {
				return model.PushChannel{}, errors.New("微信绑定资料不完整或服务地址不可信，请重新扫码")
			}
			return model.PushChannel{Provider: "wechat_claw", Mode: "ilink", Token: result.Token, BotID: result.BotID, OpenID: result.UserID, APIURL: result.BaseURL, BindingState: "waiting_message"}, nil
		default:
			return model.PushChannel{}, errors.New("微信返回未知扫码状态，请稍后重新扫码")
		}
		if !pause(s.ctx, m.pollInterval) {
			break
		}
	}
	return model.PushChannel{}, s.ctx.Err()
}

package notify

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eleost04/miyohub/internal/buildinfo"
	"github.com/eleost04/miyohub/internal/model"
)

const weixinBase = "https://ilinkai.weixin.qq.com"

func weixinHeaders(token string) map[string]string {
	var raw [4]byte
	_, _ = rand.Read(raw[:])
	h := map[string]string{"AuthorizationType": "ilink_bot_token", "X-WECHAT-UIN": base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(binary.BigEndian.Uint32(raw[:])), 10))), "iLink-App-Id": "bot", "iLink-App-ClientVersion": "132104"}
	if token != "" {
		h["Authorization"] = "Bearer " + token
	}
	return h
}

func weixinInfo() map[string]string {
	return map[string]string{"channel_version": "2.4.8", "bot_agent": "MiyoHub/" + buildinfo.Version()}
}

func (s Sender) sendWeixin(ctx context.Context, c model.PushChannel, message string) error {
	if !WeixinEndpoint(c.APIURL) {
		return errors.New("微信 iLink 地址无效，请重新扫码")
	}
	if c.ContextToken == "" {
		return errors.New("微信尚未建立消息会话，请先在微信给已绑定机器人发送一条消息")
	}
	var id [16]byte
	_, _ = rand.Read(id[:])
	raw, err := s.post(ctx, strings.TrimRight(c.APIURL, "/")+"/ilink/bot/sendmessage", map[string]any{
		"base_info": weixinInfo(), "msg": map[string]any{"from_user_id": "", "to_user_id": c.OpenID,
			"client_id": "miyohub-" + hex.EncodeToString(id[:]), "message_type": 2, "message_state": 2,
			"context_token": c.ContextToken, "item_list": []any{map[string]any{"type": 1, "text_item": map[string]string{"text": truncate(message, 2000)}}}},
	}, weixinHeaders(c.Token))
	if err != nil {
		return err
	}
	return weixinResponse(raw)
}

func weixinResponse(raw []byte) error {
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || body == nil {
		return errors.New("微信响应格式异常，送达状态未确认")
	}
	confirmed := false
	for _, key := range []string{"ret", "errcode"} {
		if value, exists := body[key]; exists {
			var code int
			if json.Unmarshal(value, &code) != nil {
				return errors.New("微信响应格式异常，送达状态未确认")
			}
			confirmed = true
			if code != 0 {
				return fmt.Errorf("微信接口错误码 %d，请检查绑定状态或重新向机器人发送消息；登录过期时需重新扫码", code)
			}
		}
	}
	if !confirmed {
		return errors.New("微信未确认接收，送达状态未确认")
	}
	return nil
}

type WeixinStore interface {
	PrivatePushConfigs() map[string]model.PushConfig
	UpdateWeixinSession(string, model.PushChannel, string, string, string, string) bool
}

type weixinWorker struct {
	channel model.PushChannel
	cancel  context.CancelFunc
}

// This receiver only retains the bound user's context token and sync cursor.
// It does not retain chat contents or respond to incoming messages.
type WeixinMonitor struct {
	store            WeixinStore
	sender           Sender
	mu               sync.Mutex
	ctx              context.Context
	cancel           context.CancelFunc
	workers          map[string]weixinWorker
	wg               sync.WaitGroup
	started, stopped bool
}

func NewWeixinMonitor(s WeixinStore, sender Sender) *WeixinMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	// Long polling has a server-suggested deadline, unlike message delivery.
	// Give this receiver its own client/transport so ordinary sends keep their
	// short timeout and never get replayed.
	client := *NewHTTPClient()
	if sender.HTTP != nil {
		client = *sender.HTTP
	}
	client.Timeout = 0
	if transport, ok := client.Transport.(*http.Transport); ok {
		copyTransport := transport.Clone()
		copyTransport.ResponseHeaderTimeout = 0
		client.Transport = copyTransport
	}
	sender.HTTP = &client
	return &WeixinMonitor{store: s, sender: sender, ctx: ctx, cancel: cancel, workers: map[string]weixinWorker{}}
}
func (m *WeixinMonitor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started || m.stopped {
		return
	}
	m.started = true
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		timer := time.NewTicker(3 * time.Second)
		defer timer.Stop()
		for {
			m.reconcile()
			select {
			case <-m.ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
}
func (m *WeixinMonitor) Stop() {
	m.mu.Lock()
	m.stopped = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	m.sender.HTTP.CloseIdleConnections()
}
func (m *WeixinMonitor) reconcile() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	wanted := map[string]bool{}
	for userID, cfg := range m.store.PrivatePushConfigs() {
		for _, c := range cfg.Channels {
			if !c.Enabled || c.Provider != "wechat_claw" || c.Mode != "ilink" || c.Token == "" || c.BindingState == "expired" || !WeixinEndpoint(c.APIURL) {
				continue
			}
			key := userID + ":" + c.ID
			wanted[key] = true
			if worker, exists := m.workers[key]; exists {
				old := worker.channel
				if old.Token == c.Token && old.OpenID == c.OpenID && old.APIURL == c.APIURL {
					continue
				}
				worker.cancel()
			}
			ctx, cancel := context.WithCancel(m.ctx)
			m.workers[key] = weixinWorker{channel: c, cancel: cancel}
			m.wg.Add(1)
			go func() { defer m.wg.Done(); m.poll(ctx, userID, c) }()
		}
	}
	for key, worker := range m.workers {
		if !wanted[key] {
			worker.cancel()
			delete(m.workers, key)
		}
	}
}

type weixinUpdates struct {
	Ret             int    `json:"ret"`
	ErrCode         int    `json:"errcode"`
	Cursor          string `json:"get_updates_buf"`
	LongPollTimeout int    `json:"longpolling_timeout_ms"`
	Messages        []struct {
		From    string `json:"from_user_id"`
		Type    int    `json:"message_type"`
		Context string `json:"context_token"`
	} `json:"msgs"`
}

func weixinPollTimeout(milliseconds int) time.Duration {
	if milliseconds <= 0 {
		return 35 * time.Second
	}
	return time.Duration(max(5000, min(120000, milliseconds))) * time.Millisecond
}

func (m *WeixinMonitor) getUpdates(ctx context.Context, channel model.PushChannel, cursor string, timeout time.Duration) (weixinUpdates, error) {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var wrote, received atomic.Bool
	callCtx = httptrace.WithClientTrace(callCtx, &httptrace.ClientTrace{
		WroteRequest:         func(info httptrace.WroteRequestInfo) { wrote.Store(info.Err == nil) },
		GotFirstResponseByte: func() { received.Store(true) },
	})
	raw, err := m.sender.post(callCtx, strings.TrimRight(channel.APIURL, "/")+"/ilink/bot/getupdates", map[string]any{"get_updates_buf": cursor, "base_info": weixinInfo()}, weixinHeaders(channel.Token))
	if ctx.Err() != nil {
		return weixinUpdates{}, ctx.Err()
	}
	// An idle poll is normal only after sending the request successfully.
	// DNS, TCP/TLS timeouts and stalled response bodies are still real errors.
	if err != nil && errors.Is(callCtx.Err(), context.DeadlineExceeded) && wrote.Load() && !received.Load() {
		return weixinUpdates{Cursor: cursor}, nil
	}
	if err != nil {
		return weixinUpdates{}, err
	}
	var data *weixinUpdates
	if json.Unmarshal(raw, &data) != nil || data == nil {
		return weixinUpdates{}, errors.New("微信接收响应格式异常")
	}
	// ret and errcode are optional in the iLink getupdates protocol. This is
	// NOT a sendmessage acknowledgement, which remains strictly validated.
	return *data, nil
}

func weixinPollError(err error) string {
	var dns *net.DNSError
	var netErr net.Error
	switch {
	case errors.Is(err, ErrPrivateNetwork):
		return "微信接收地址被网络安全策略阻止，请检查 DNS 解析"
	case errors.As(err, &dns):
		return "微信接收服务 DNS 解析失败，稍后自动重连"
	case errors.As(err, &netErr) && netErr.Timeout(), errors.Is(err, context.DeadlineExceeded):
		return "连接微信接收服务超时，请检查服务器网络；稍后自动重连"
	default:
		// Sender errors are sanitized; do not use upstream errmsg or raw URLs.
		return "微信接收连接异常：" + err.Error()
	}
}

func (m *WeixinMonitor) poll(ctx context.Context, userID string, channel model.PushChannel) {
	cursor, token, state := channel.SyncCursor, channel.ContextToken, channel.BindingState
	timeout := weixinPollTimeout(0)
	for ctx.Err() == nil {
		data, err := m.getUpdates(ctx, channel, cursor, timeout)
		if ctx.Err() != nil {
			return
		}
		if err != nil || data.Ret != 0 || data.ErrCode != 0 {
			state = "reconnecting"
			detail := ""
			if err != nil {
				detail = weixinPollError(err)
			} else {
				code := data.ErrCode
				if code == 0 {
					code = data.Ret
				}
				detail = fmt.Sprintf("微信接收接口错误码 %d，稍后自动重连", code)
			}
			if data.Ret == -14 || data.ErrCode == -14 {
				state = "expired"
				detail = "微信登录已过期（-14），请重新扫码绑定"
			}
			if !m.store.UpdateWeixinSession(userID, channel, cursor, "", state, detail) {
				return
			}
			if state == "expired" {
				return // Rebinding supplies a new token; do not poll an expired one.
			}
			if !pause(ctx, 15*time.Second) {
				return
			}
			continue
		}
		if data.LongPollTimeout > 0 {
			timeout = weixinPollTimeout(data.LongPollTimeout)
		}
		if data.Cursor != "" {
			cursor = data.Cursor
		}
		for _, msg := range data.Messages {
			if msg.From == channel.OpenID && msg.Type == 1 && msg.Context != "" {
				token = msg.Context
			}
		}
		state = "waiting_message"
		if token != "" {
			state = "ready"
		}
		if !m.store.UpdateWeixinSession(userID, channel, cursor, token, state, "") {
			return
		}
		if !pause(ctx, time.Second) {
			return
		}
	}
}

func pause(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

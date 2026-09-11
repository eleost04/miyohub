package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/gorilla/websocket"
)

type QQStore interface {
	PrivatePushConfigs() map[string]model.PushConfig
	UpdateQQConnection(string, model.PushChannel, string, string) bool
}

type qqWorker struct {
	channel model.PushChannel
	cancel  context.CancelFunc
	done    chan struct{}
}

// QQ's official connect page waits for an authenticated gateway session, not
// merely stored REST credentials. The monitor keeps bound bots online without
// sending chat replies, changing recipients or storing incoming chat content.
type QQMonitor struct {
	store            QQStore
	sender           Sender
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.Mutex
	workers          map[string]qqWorker
	wg               sync.WaitGroup
	wake             chan struct{}
	started, stopped bool
	dial             func(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error)
}

func NewQQMonitor(store QQStore, sender Sender) *QQMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	dialer := &websocket.Dialer{NetDialContext: safeDial, HandshakeTimeout: 10 * time.Second, ReadBufferSize: 4096, WriteBufferSize: 2048}
	return &QQMonitor{store: store, sender: sender, ctx: ctx, cancel: cancel, workers: map[string]qqWorker{}, wake: make(chan struct{}, 1), dial: dialer.DialContext}
}

func (m *QQMonitor) Start() {
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
			case <-m.wake:
			case <-timer.C:
			}
		}
	}()
}

func (m *QQMonitor) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *QQMonitor) Stop() {
	m.mu.Lock()
	m.stopped = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *QQMonitor) CancelUser(userID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, worker := range m.workers {
		if strings.HasPrefix(key, userID+":") {
			worker.cancel()
		}
	}
}

func (m *QQMonitor) reconcile() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	wanted := map[string]bool{}
	for userID, cfg := range m.store.PrivatePushConfigs() {
		for _, channel := range cfg.Channels {
			// Gateway connectivity is independent of automatic notifications. It
			// is also needed before an OpenID can be supplied manually. Deleting
			// the channel/credential or disabling the owner closes the connection.
			if channel.Provider != "qqbot" || !numericID.MatchString(channel.AppID) || channel.ClientSecret == "" {
				continue
			}
			key := userID + ":" + channel.ID
			wanted[key] = true
			if previous, ok := m.workers[key]; ok {
				select {
				case <-previous.done:
					delete(m.workers, key)
				default:
					if previous.channel.AppID != channel.AppID || previous.channel.ClientSecret != channel.ClientSecret {
						previous.cancel()
					}
					// Finish the old worker before replacing it, even when the
					// same credentials are disabled and quickly re-enabled.
					// Otherwise its late disconnect could overwrite READY.
					continue
				}
			}
			if len(m.workers) >= 200 {
				m.store.UpdateQQConnection(userID, channel, "error", "QQ 连接数量达到站点上限，请联系管理员")
				continue
			}
			ctx, cancel := context.WithCancel(m.ctx)
			done := make(chan struct{})
			m.workers[key] = qqWorker{channel: channel, cancel: cancel, done: done}
			m.wg.Add(1)
			go func() {
				defer m.wg.Done()
				defer m.Wake()
				defer close(done)
				m.run(ctx, userID, channel)
			}()
		}
	}
	for key, worker := range m.workers {
		if !wanted[key] {
			worker.cancel()
			select {
			case <-worker.done:
				delete(m.workers, key)
			default:
			}
		}
	}
}

type qqSession struct {
	token, sessionID string
	expires          time.Time
	sequence         *int64
}

type qqGatewayError struct {
	message string
	wait    time.Duration
}

func (e *qqGatewayError) Error() string { return e.message }

func (m *QQMonitor) run(ctx context.Context, userID string, channel model.PushChannel) {
	state := qqSession{}
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		if !m.store.UpdateQQConnection(userID, channel, "connecting", "正在建立 QQ 官方连接") {
			return
		}
		gateway, err := m.credentials(ctx, channel, &state)
		connected := false
		if err == nil {
			err = m.session(ctx, gateway, channel, &state, func() bool {
				connected = true
				return m.store.UpdateQQConnection(userID, channel, "ready", "")
			})
		}
		if ctx.Err() != nil {
			break
		}
		if connected {
			backoff = 2 * time.Second
		}
		delay, message := backoff, "QQ 网关连接中断，将自动重连"
		var detail *qqGatewayError
		if errors.As(err, &detail) {
			message, delay = detail.message, max(delay, detail.wait)
		}
		if !m.store.UpdateQQConnection(userID, channel, "reconnecting", fmt.Sprintf("%s（%d 秒后重试）", message, int(delay.Seconds()))) {
			return
		}
		if !pause(ctx, delay) {
			break
		}
		backoff = min(backoff*2, time.Minute)
	}
	_ = m.store.UpdateQQConnection(userID, channel, "disconnected", "QQ 官方连接已停止")
}

func (m *QQMonitor) credentials(ctx context.Context, channel model.PushChannel, state *qqSession) (string, error) {
	if state.token == "" || time.Until(state.expires) < 30*time.Second {
		raw, err := m.sender.post(ctx, "https://bots.qq.com/app/getAppAccessToken", map[string]string{"appId": channel.AppID, "clientSecret": channel.ClientSecret}, nil)
		if err != nil {
			return "", &qqGatewayError{message: "无法取得 QQ 授权，请检查服务器网络与机器人凭据", wait: 30 * time.Second}
		}
		var token struct {
			Value   string          `json:"access_token"`
			Expires json.RawMessage `json:"expires_in"`
		}
		if json.Unmarshal(raw, &token) != nil || token.Value == "" || len(token.Value) > 4096 || strings.ContainsAny(token.Value, "\r\n") {
			return "", &qqGatewayError{message: "QQ 授权失败，请检查 AppID、ClientSecret 和机器人权限", wait: time.Minute}
		}
		seconds, _ := strconv.Atoi(strings.Trim(string(token.Expires), `"`))
		if seconds <= 0 {
			seconds = 300
		}
		state.token, state.expires = token.Value, time.Now().Add(time.Duration(min(seconds, 7200))*time.Second)
	}
	raw, err := m.sender.request(ctx, http.MethodGet, "https://api.sgroup.qq.com/gateway/bot", nil, map[string]string{"Authorization": "QQBot " + state.token, "X-Union-Appid": channel.AppID})
	if err != nil {
		state.token = ""
		return "", &qqGatewayError{message: "无法取得 QQ 网关，请检查服务器网络与机器人权限", wait: 30 * time.Second}
	}
	var result struct {
		URL   string `json:"url"`
		Limit struct {
			Remaining *int  `json:"remaining"`
			ResetMS   int64 `json:"reset_after"`
		} `json:"session_start_limit"`
	}
	if json.Unmarshal(raw, &result) != nil || !qqGatewayURL(result.URL) {
		return "", &qqGatewayError{message: "QQ 未返回有效的官方网关地址", wait: 30 * time.Second}
	}
	if state.sessionID == "" && result.Limit.Remaining != nil && *result.Limit.Remaining <= 0 {
		return "", &qqGatewayError{message: "QQ 网关会话配额已用完，等待官方配额恢复", wait: time.Duration(max(60000, min(86400000, result.Limit.ResetMS))) * time.Millisecond}
	}
	return result.URL, nil
}

func qqGatewayURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && u.Scheme == "wss" && u.User == nil && u.Fragment == "" && (u.Port() == "" || u.Port() == "443") && (u.Hostname() == "api.sgroup.qq.com" || strings.HasSuffix(u.Hostname(), ".api.sgroup.qq.com"))
}

type qqPacket struct {
	Op       int             `json:"op"`
	Type     string          `json:"t"`
	Sequence *int64          `json:"s"`
	Data     json.RawMessage `json:"d"`
}

func (m *QQMonitor) session(ctx context.Context, gateway string, channel model.PushChannel, state *qqSession, ready func() bool) error {
	conn, response, err := m.dial(ctx, gateway, http.Header{"User-Agent": {"MiyoHub"}})
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil {
		return &qqGatewayError{message: "QQ WebSocket 握手失败，请检查服务器出站网络", wait: 0}
	}
	conn.SetReadLimit(64 << 10)
	readCtx, stopRead := context.WithCancel(ctx)
	packets, failures, readDone := make(chan qqPacket, 8), make(chan error, 1), make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			var packet qqPacket
			if err := conn.ReadJSON(&packet); err != nil {
				select {
				case failures <- err:
				default:
				}
				return
			}
			// Sequence is needed for resumption; bodies of chat events are not.
			if packet.Op == 0 && packet.Type != "READY" && packet.Type != "RESUMED" {
				packet.Data = nil
			}
			select {
			case packets <- packet:
			case <-readCtx.Done():
				return
			}
		}
	}()
	defer func() { stopRead(); conn.Close(); <-readDone }()
	write := func(op int, data any) error {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteJSON(map[string]any{"op": op, "d": data})
	}
	heartbeat := time.NewTimer(30 * time.Second)
	defer heartbeat.Stop()
	readyTimeout := time.NewTimer(30 * time.Second)
	defer readyTimeout.Stop()
	expiry := time.NewTimer(max(time.Second, time.Until(state.expires)-15*time.Second))
	defer expiry.Stop()
	interval, identified, waitingACK := 30*time.Second, false, false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-expiry.C:
			state.token = ""
			return &qqGatewayError{message: "QQ 授权即将到期，正在更新连接"}
		case <-readyTimeout.C:
			return &qqGatewayError{message: "QQ 网关未确认上线，将重新连接"}
		case err := <-failures:
			var closed *websocket.CloseError
			if errors.As(err, &closed) && closed.Code >= 4000 && closed.Code < 5000 {
				state.sessionID, state.sequence, state.token = "", nil, ""
				return &qqGatewayError{message: fmt.Sprintf("QQ 关闭网关连接（代码 %d），请检查机器人权限", closed.Code), wait: time.Minute}
			}
			return &qqGatewayError{message: "QQ WebSocket 连接中断"}
		case <-heartbeat.C:
			if !identified || waitingACK {
				return &qqGatewayError{message: "QQ 网关心跳未确认，将重新连接"}
			}
			if err := write(1, state.sequence); err != nil {
				return err
			}
			waitingACK = true
			heartbeat.Reset(interval)
		case packet := <-packets:
			if packet.Sequence != nil {
				state.sequence = packet.Sequence
			}
			switch packet.Op {
			case 10: // HELLO
				if identified {
					return errors.New("duplicate QQ HELLO")
				}
				var hello struct {
					Milliseconds int `json:"heartbeat_interval"`
				}
				if json.Unmarshal(packet.Data, &hello) != nil || hello.Milliseconds < 1000 || hello.Milliseconds > 120000 {
					return &qqGatewayError{message: "QQ 网关心跳参数无效"}
				}
				interval = time.Duration(hello.Milliseconds) * time.Millisecond
				if state.sessionID != "" && state.sequence != nil {
					err = write(6, map[string]any{"token": "QQBot " + state.token, "session_id": state.sessionID, "seq": *state.sequence})
				} else {
					err = write(2, map[string]any{"token": "QQBot " + state.token, "intents": 1 << 25, "shard": []int{0, 1}, "properties": map[string]string{"os": "linux", "browser": "miyohub", "device": "miyohub"}})
				}
				if err != nil {
					return err
				}
				identified = true
				heartbeat.Reset(interval)
			case 0: // DISPATCH
				if !identified {
					return errors.New("QQ dispatch before authentication")
				}
				if packet.Type == "READY" {
					var data struct {
						ID string `json:"session_id"`
					}
					if json.Unmarshal(packet.Data, &data) != nil || data.ID == "" || len(data.ID) > 512 || strings.ContainsAny(data.ID, "\r\n\x00") {
						return &qqGatewayError{message: "QQ 网关会话信息无效"}
					}
					state.sessionID = data.ID
				}
				if packet.Type == "READY" || packet.Type == "RESUMED" {
					readyTimeout.Stop()
					if !ready() {
						return context.Canceled
					}
				}
			case 1:
				if err := write(1, state.sequence); err != nil {
					return err
				}
				waitingACK = true
				heartbeat.Reset(interval)
			case 11:
				waitingACK = false
			case 7:
				return &qqGatewayError{message: "QQ 官方要求恢复网关连接"}
			case 9:
				state.sessionID, state.sequence = "", nil
				return &qqGatewayError{message: "QQ 网关会话失效，将重新鉴权", wait: 5 * time.Second}
			}
		}
	}
}

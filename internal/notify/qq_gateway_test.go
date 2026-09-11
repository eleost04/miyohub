package notify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/gorilla/websocket"
)

type qqSink struct {
	mu      sync.Mutex
	channel model.PushChannel
	active  bool
	states  chan string
	details []string
}

func (s *qqSink) PrivatePushConfigs() map[string]model.PushConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return map[string]model.PushConfig{}
	}
	return map[string]model.PushConfig{"owner": {Enabled: false, Channels: []model.PushChannel{s.channel}}}
}
func (s *qqSink) UpdateQQConnection(owner string, expected model.PushChannel, state, detail string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active || owner != "owner" || s.channel.ID != expected.ID || s.channel.ClientSecret != expected.ClientSecret {
		return false
	}
	s.channel.BindingState, s.channel.BindingError = state, detail
	s.details = append(s.details, detail)
	select {
	case s.states <- state:
	default:
	}
	return true
}

func qqLocalDial(server *httptest.Server) func(context.Context, string, http.Header) (*websocket.Conn, *http.Response, error) {
	return func(ctx context.Context, _ string, headers http.Header) (*websocket.Conn, *http.Response, error) {
		return websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), headers)
	}
}

func TestQQGatewayGoesOnlineResumesAndNeverStoresOrRepliesToChat(t *testing.T) {
	sink := &qqSink{active: true, channel: model.PushChannel{ID: "qq", Provider: "qqbot", AppID: "123456", ClientSecret: "fixture-client-secret", OpenID: "owner-openid"}, states: make(chan string, 32)}
	var connects, tokens, gateways, heartbeats atomic.Int32
	closed := make(chan struct{}, 4)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { conn.Close(); closed <- struct{}{} }()
		n := connects.Add(1)
		_ = conn.SetReadDeadline(time.Now().Add(6 * time.Second))
		_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 1000}})
		var auth struct {
			Op   int            `json:"op"`
			Data map[string]any `json:"d"`
		}
		if err := conn.ReadJSON(&auth); err != nil {
			t.Error(err)
			return
		}
		if auth.Data["token"] != "QQBot fixture-access-token" {
			t.Error("wrong gateway token")
		}
		if n == 1 {
			if auth.Op != 2 || auth.Data["intents"] != float64(1<<25) {
				t.Error("missing gateway identify")
			}
			_ = conn.WriteJSON(map[string]any{"op": 0, "t": "READY", "s": 1, "d": map[string]string{"session_id": "private-session"}})
			_ = conn.WriteJSON(map[string]any{"op": 0, "t": "C2C_MESSAGE_CREATE", "s": 2, "d": map[string]string{"content": "PRIVATE-CHAT-BODY", "openid": "foreign-sender"}})
		} else {
			if auth.Op != 6 || auth.Data["session_id"] != "private-session" || auth.Data["seq"] != float64(2) {
				t.Error("connection did not resume the last sequence")
			}
			_ = conn.WriteJSON(map[string]any{"op": 0, "t": "RESUMED", "s": 3, "d": map[string]any{}})
		}
		for {
			var heartbeat struct {
				Op   int `json:"op"`
				Data int `json:"d"`
			}
			if err := conn.ReadJSON(&heartbeat); err != nil {
				return
			}
			if heartbeat.Op != 1 {
				t.Error("gateway sent an unsolicited chat response")
				return
			}
			heartbeats.Add(1)
			_ = conn.WriteJSON(map[string]any{"op": 11})
			if n == 1 {
				if heartbeat.Data != 2 {
					t.Error("heartbeat did not follow dispatch sequence")
				}
				_ = conn.WriteJSON(map[string]any{"op": 7})
			}
		}
	}))
	defer server.Close()
	m := NewQQMonitor(sink, Sender{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/app/getAppAccessToken":
			tokens.Add(1)
			return response(`{"access_token":"fixture-access-token","expires_in":"7200"}`), nil
		case "/gateway/bot":
			gateways.Add(1)
			if r.Method != "GET" || r.Header.Get("Authorization") != "QQBot fixture-access-token" || r.Header.Get("X-Union-Appid") != "123456" {
				t.Error("gateway query not authenticated")
			}
			return response(`{"url":"wss://api.sgroup.qq.com/websocket","session_start_limit":{"remaining":10}}`), nil
		default:
			t.Error("gateway performed an unexpected REST action", r.URL.Path)
			return nil, errors.New("no notifications may be sent")
		}
	})}})
	m.dial = qqLocalDial(server)
	defer m.Stop()
	m.Start()
	ready := 0
	deadline := time.After(6 * time.Second)
	for ready < 2 {
		select {
		case state := <-sink.states:
			if state == "ready" {
				ready++
			}
		case <-deadline:
			t.Fatal("gateway never completed identify/resume")
		}
	}
	if tokens.Load() != 1 || gateways.Load() != 2 || connects.Load() != 2 || heartbeats.Load() < 1 {
		t.Fatal("unexpected gateway token/reconnect requests")
	}
	sink.mu.Lock()
	sink.active = false
	for _, text := range sink.details {
		for _, secret := range []string{"PRIVATE-CHAT-BODY", "foreign-sender", "private-session", "fixture-access-token", "fixture-client-secret"} {
			if strings.Contains(text, secret) {
				t.Error("gateway logged private payload")
			}
		}
	}
	if sink.channel.OpenID != "owner-openid" || sink.channel.Enabled {
		t.Error("incoming event changed recipient or enabled notifications")
	}
	sink.mu.Unlock()
	m.reconcile()
	m.Stop()
	for range 2 {
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("revoked owner left an open gateway")
		}
	}
}

func TestQQGatewayMissingHeartbeatACKForcesReconnect(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(4 * time.Second))
		_ = conn.WriteJSON(map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 1000}})
		var packet qqPacket
		if conn.ReadJSON(&packet) != nil {
			return
		}
		_ = conn.WriteJSON(map[string]any{"op": 0, "t": "READY", "s": 1, "d": map[string]string{"session_id": "fixture-session"}})
		for conn.ReadJSON(&packet) == nil {
		} // Deliberately never ACK.
	}))
	defer server.Close()
	m := NewQQMonitor(nil, Sender{})
	m.dial = qqLocalDial(server)
	state := qqSession{token: "fixture-token", expires: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	err := m.session(ctx, "wss://api.sgroup.qq.com/websocket", model.PushChannel{}, &state, func() bool { return true })
	if err == nil || !strings.Contains(err.Error(), "心跳") || ctx.Err() != nil {
		t.Fatal("missing ACK did not terminate the stale session", err)
	}
}

func TestQQGatewayWaitsForCancelledWorkerBeforeReconnectingSameBot(t *testing.T) {
	sink := &qqSink{active: true, channel: model.PushChannel{ID: "qq", Provider: "qqbot", AppID: "123456", ClientSecret: "fixture-secret"}, states: make(chan string, 32)}
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	m := NewQQMonitor(sink, Sender{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		entered <- struct{}{}
		<-r.Context().Done()
		<-release
		return nil, r.Context().Err()
	})}})
	defer m.Stop()
	defer releaseOnce.Do(func() { close(release) })
	m.reconcile()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("worker never started")
	}
	old := m.workers["owner:qq"]
	m.CancelUser("owner")
	m.reconcile()
	if m.workers["owner:qq"].done != old.done {
		t.Fatal("replacement started before cancelled worker finished")
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-old.done:
	case <-time.After(time.Second):
		t.Fatal("cancelled worker did not finish")
	}
	m.reconcile()
	if m.workers["owner:qq"].done == old.done {
		t.Fatal("finished worker prevented a new connection")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("new worker never started")
	}
}

func TestQQGatewayURLQuotaAndTokenErrorBoundaries(t *testing.T) {
	for _, raw := range []string{"ws://api.sgroup.qq.com", "wss://api.sgroup.qq.com.evil.example", "wss://127.0.0.1", "wss://user:pass@api.sgroup.qq.com", "wss://api.sgroup.qq.com:8080", "wss://api.sgroup.qq.com/#fragment"} {
		if qqGatewayURL(raw) {
			t.Fatal("untrusted QQ gateway accepted")
		}
	}
	for _, tc := range []struct {
		body    string
		minimum time.Duration
	}{
		{`{"url":"wss://api.sgroup.qq.com/websocket","session_start_limit":{"remaining":0,"reset_after":3600000}}`, time.Hour},
		{`{"url":"wss://api.sgroup.qq.com.evil.example/PRIVATE-TOKEN"}`, 30 * time.Second},
	} {
		m := NewQQMonitor(nil, Sender{HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return response(tc.body), nil })}})
		state := qqSession{token: "private-token", expires: time.Now().Add(time.Hour)}
		_, err := m.credentials(t.Context(), model.PushChannel{AppID: "123"}, &state)
		var detail *qqGatewayError
		if !errors.As(err, &detail) || detail.wait < tc.minimum || strings.Contains(err.Error(), "PRIVATE-TOKEN") {
			t.Fatal("quota or endpoint protection failed", err)
		}
	}
	var packet qqPacket
	if err := json.Unmarshal([]byte(`{"op":0,"t":"C2C_MESSAGE_CREATE","s":1,"d":{"content":"fixture"}}`), &packet); err != nil || packet.Type != "C2C_MESSAGE_CREATE" {
		t.Fatal("wire packet decoding failed")
	}
}

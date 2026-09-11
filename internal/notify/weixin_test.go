package notify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
)

type monitorStore struct {
	mu      sync.Mutex
	channel model.PushChannel
	updates chan string
}

func TestWeixinPollAcceptsOptionalCodesButRejectsInvalidJSON(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{}`, true},
		{`{"msgs":[],"get_updates_buf":"next","longpolling_timeout_ms":75000}`, true},
		{`{"errcode":0}`, true},
		{`null`, false}, {`[]`, false}, {`not-json`, false}, {`{"ret":"0"}`, false},
	} {
		sender := Sender{HTTP: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) { return response(tc.body), nil })}}
		m := NewWeixinMonitor(&monitorStore{}, sender)
		_, err := m.getUpdates(t.Context(), model.PushChannel{APIURL: weixinBase}, "previous", time.Second)
		m.Stop()
		if (err == nil) != tc.valid {
			t.Fatalf("body %s: %v", tc.body, err)
		}
	}
}

func TestWeixinOnlyTreatsWrittenIdlePollsAsNormalTimeouts(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		wrote, received, idle bool
	}{
		{"idle", true, false, true},
		{"connect-timeout", false, false, false},
		{"response-body-timeout", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := Sender{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				trace := httptrace.ContextClientTrace(r.Context())
				if tc.wrote {
					trace.WroteRequest(httptrace.WroteRequestInfo{})
				}
				if tc.received {
					trace.GotFirstResponseByte()
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}}
			m := NewWeixinMonitor(&monitorStore{}, sender)
			defer m.Stop()
			data, err := m.getUpdates(t.Context(), model.PushChannel{APIURL: weixinBase}, "keep-cursor", 10*time.Millisecond)
			if (err == nil) != tc.idle || tc.idle && data.Cursor != "keep-cursor" {
				t.Fatal("idle poll misclassified", data, err)
			}
		})
	}
}

func TestWeixinExternalCancellationIsNotAnIdlePoll(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	sender := Sender{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		httptrace.ContextClientTrace(r.Context()).WroteRequest(httptrace.WroteRequestInfo{})
		cancel()
		return nil, r.Context().Err()
	})}}
	m := NewWeixinMonitor(&monitorStore{}, sender)
	defer m.Stop()
	if _, err := m.getUpdates(ctx, model.PushChannel{APIURL: weixinBase}, "keep", time.Second); !errors.Is(err, context.Canceled) {
		t.Fatal("external cancellation reported success", err)
	}
}

func TestWeixinLongPollClientDoesNotChangeDeliveryTimeouts(t *testing.T) {
	client := NewHTTPClient()
	m := NewWeixinMonitor(&monitorStore{}, Sender{HTTP: client})
	defer m.Stop()
	if client.Timeout != 40*time.Second || client.Transport.(*http.Transport).ResponseHeaderTimeout != 38*time.Second {
		t.Fatal("delivery client changed")
	}
	if m.sender.HTTP.Timeout != 0 || m.sender.HTTP.Transport.(*http.Transport).ResponseHeaderTimeout != 0 {
		t.Fatal("long polls still have an independent short deadline")
	}
	for _, tc := range []struct {
		ms   int
		want time.Duration
	}{{0, 35 * time.Second}, {-1, 35 * time.Second}, {1, 5 * time.Second}, {75000, 75 * time.Second}, {300000, 120 * time.Second}} {
		if got := weixinPollTimeout(tc.ms); got != tc.want {
			t.Fatal("server deadline not bounded", got, tc.want)
		}
	}
}

func (s *monitorStore) PrivatePushConfigs() map[string]model.PushConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]model.PushConfig{"owner": {Channels: []model.PushChannel{s.channel}}}
}
func (s *monitorStore) UpdateWeixinSession(_ string, expected model.PushChannel, cursor, token, state, detail string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.channel.Token != expected.Token {
		return false
	}
	s.channel.SyncCursor, s.channel.ContextToken, s.channel.BindingState = cursor, token, state
	s.channel.BindingError = detail
	s.updates <- state
	return true
}

func TestWeixinExpiredCredentialsStayStoppedUntilRebinding(t *testing.T) {
	sink := &monitorStore{channel: model.PushChannel{ID: "wx", Provider: "wechat_claw", Mode: "ilink", Enabled: true, Token: "expired-token", APIURL: weixinBase, OpenID: "receiver", BindingState: "waiting_message"}, updates: make(chan string, 4)}
	var calls atomic.Int32
	sender := Sender{HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("Authorization") == "Bearer expired-token" {
			return response(`{"ret":-14}`), nil
		}
		return response(`{"ret":0,"msgs":[{"from_user_id":"receiver","message_type":1,"context_token":"new-context"}]}`), nil
	})}}
	m := NewWeixinMonitor(sink, sender)
	defer m.Stop()
	waitState := func(wanted string) {
		t.Helper()
		select {
		case got := <-sink.updates:
			if got != wanted {
				t.Fatalf("wrong binding state %q; want %q", got, wanted)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("monitor did not update binding state")
		}
	}
	// Drive one poll directly so a regressed expired-token loop cannot pass by
	// merely being cancelled at the next reconciliation.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan struct{})
	go func() { m.poll(ctx, "owner", sink.channel); close(finished) }()
	waitState("expired")
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("expired credentials continued polling")
	}
	m.reconcile()
	m.mu.Lock()
	workers := len(m.workers)
	m.mu.Unlock()
	if workers != 0 || calls.Load() != 1 {
		t.Fatal("reconciliation restarted an expired session")
	}
	// A freshly confirmed QR clears the expired state and supplies a new token.
	sink.mu.Lock()
	sink.channel.Token, sink.channel.BindingState = "new-token", "waiting_message"
	sink.mu.Unlock()
	m.reconcile()
	waitState("ready")
	m.Stop()
	if calls.Load() != 2 {
		t.Fatal("new binding did not start exactly one receiver")
	}
}

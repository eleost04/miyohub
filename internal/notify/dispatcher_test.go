package notify_test

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/notify"
	"github.com/eleost04/miyohub/internal/store"
)

type dispatcherTransport func(*http.Request) (*http.Response, error)

func (f dispatcherTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDispatcherRestoresQueueIsolatesChannelsAndStopsWithoutReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := s.CreateAdmin("admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateUser("member", "test-password", "user")
	if err != nil {
		t.Fatal(err)
	}
	for id, destinations := range map[string][]string{owner.ID: {"slow", "fast"}, member.ID: {"rejected"}} {
		patch := store.PushSettingsPatch{Enabled: true, Tasks: true, Exchange: true, Revision: 1}
		for _, name := range destinations {
			patch.Channels = append(patch.Channels, store.PushChannelPatch{PushChannel: model.PushChannel{Name: name, Provider: "webhook", Enabled: true, Webhook: "https://example.invalid/" + name}})
		}
		if _, err := s.UpdatePushSettings(id, patch); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	sender := notify.Sender{HTTP: &http.Client{Transport: dispatcherTransport(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		body := `{"ok":true}`
		if r.URL.Path == "/rejected" {
			body = `{"ok":false,"code":403}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	queued := notify.NewDispatcher(s, sender)
	for _, id := range []string{owner.ID, member.ID} {
		if !queued.Enqueue(notify.Event{Kind: notify.TaskEventKind, UserID: id, Title: "task result", Message: "safe summary", Success: true}) {
			t.Fatal("task event was not queued")
		}
	}
	queued.Stop()
	if requests.Load() != 0 {
		t.Fatal("enqueue sent synchronously instead of persisting the event")
	}
	reopened, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	d := notify.NewDispatcher(reopened, sender)
	defer d.Stop()
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		accepted := false
		for _, entry := range reopened.PushHistory(owner.ID) {
			if entry.ChannelName == "fast" && entry.Status == "accepted" {
				accepted = true
			}
		}
		memberHistory := reopened.PushHistory(member.ID)
		if accepted && len(memberHistory) == 1 && memberHistory[0].Status == "failed" {
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("slow channel blocked another channel or a provider failure was misreported")
		}
	}
	d.Stop()
	if requests.Load() != 3 || len(reopened.PushHistory(owner.ID)) != 2 {
		t.Fatal("notification repeated or history crossed users")
	}
	for _, entry := range reopened.PushHistory(owner.ID) {
		if entry.ChannelName == "slow" && entry.Status != "unknown" {
			t.Fatal("interrupted delivery did not preserve uncertainty")
		}
	}
	if err := reopened.RecoverPushDeliveries(); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := reopened.ClaimPushDelivery(); ok || err != nil {
		t.Fatal("finished or uncertain delivery was replayed")
	}
	if d.Enqueue(notify.Event{UserID: owner.ID, Kind: notify.TaskEventKind}) {
		t.Fatal("stopped dispatcher accepted new work")
	}
}

package gamerecord

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func TestRecordCacheCooldownAndIdentityIsolation(t *testing.T) {
	var calls atomic.Int32
	var risk atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == mihoyo.AccountRolesPath {
			_, _ = w.Write([]byte(`{"retcode":0,"data":{"list":[{"game_uid":"70201","region":"cn_gf01","nickname":"fixture"}]}}`))
			return
		}
		if risk.Load() {
			_, _ = w.Write([]byte(`{"retcode":1034}`))
			return
		}
		_, _ = w.Write([]byte(`{"retcode":0,"data":{"current_resin":5,"max_resin":200}}`))
	}))
	defer server.Close()
	s := New(mihoyo.NewClient(server.URL))
	defer s.Stop()
	s.interval = 0
	now := time.Now()
	s.now = func() time.Time { return now }
	a := model.Account{ID: "fixture-account", UserID: "owner", Cookie: "synthetic-cookie"}
	got, err := s.Note(context.Background(), a, "genshin", "", "")
	if err != nil || got.Status != "ok" || got.Note == nil || calls.Load() != 2 {
		t.Fatal("initial snapshot failed", err)
	}
	got, err = s.Note(context.Background(), a, "genshin", "", "")
	if err != nil || !got.Cached || calls.Load() != 2 {
		t.Fatal("fresh cache queried upstream")
	}
	if _, err := s.Note(context.Background(), a, "genshin", "77777", "cn_gf01"); err == nil || calls.Load() != 2 {
		t.Fatal("arbitrary role queried")
	}
	now = now.Add(4 * time.Minute)
	risk.Store(true)
	got, err = s.Note(context.Background(), a, "genshin", "", "")
	if err != nil || got.Status != "verification" || !got.Stale || got.Note == nil || calls.Load() != 3 || got.RefreshAt.Sub(now) != 6*time.Hour {
		t.Fatal("risk did not preserve labeled stale snapshot", err)
	}
	got, _ = s.Note(context.Background(), a, "genshin", "", "")
	if got.Note == nil || !got.Stale || calls.Load() != 3 {
		t.Fatal("cooldown lost cached note")
	}
	got, _ = s.Note(context.Background(), a, "zzz", "", "")
	if got.Status != "verification" || calls.Load() != 3 {
		t.Fatal("switching games bypassed cooldown")
	}
	a.Cookie = "changed-synthetic-cookie"
	s.Invalidate(a.ID)
	got, _ = s.Note(context.Background(), a, "genshin", "", "")
	if got.Status != "verification" || got.Note != nil || calls.Load() != 3 {
		t.Fatal("credential change bypassed cooldown or retained private cache")
	}
	other := a
	other.ID = "other-account"
	other.UserID = "other-owner"
	risk.Store(false)
	got, err = s.Note(context.Background(), other, "genshin", "", "")
	if err != nil || got.Status != "ok" || got.Cached || calls.Load() != 5 {
		t.Fatal("independent owner affected by cache")
	}
}

func TestCalendarReminderRequiresFreshOwnedSnapshot(t *testing.T) {
	now := time.Now()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == mihoyo.AccountRolesPath {
			_, _ = w.Write([]byte(`{"retcode":0,"data":{"list":[{"game_uid":"70101","region":"cn_gf01"}]}}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"retcode":0,"data":{"act_list":[{"name":"fixture","time_info":{"start_ts":%d,"end_ts":%d}}]}}`, time.Now().Unix()+3600, time.Now().Unix()+86400)
	}))
	defer server.Close()
	s := New(mihoyo.NewClient(server.URL))
	defer s.Stop()
	s.interval = 0
	s.now = func() time.Time { return now }
	a := model.Account{ID: "fixture", UserID: "owner", Cookie: "synthetic"}
	snapshot, err := s.Calendar(context.Background(), a, "genshin", "", "")
	if err != nil || snapshot.Calendar == nil || len(snapshot.Calendar.Events) != 1 {
		t.Fatal("fixture calendar failed", err)
	}
	id := snapshot.Calendar.Events[0].ID
	if _, _, err := s.CalendarEvent(a, "genshin", id); err != nil || calls.Load() != 2 {
		t.Fatal("snapshot lookup queried upstream", err)
	}
	other := a
	other.UserID = "other"
	if _, _, err := s.CalendarEvent(other, "genshin", id); err == nil {
		t.Fatal("snapshot crossed owner")
	}
	if _, _, err := s.CalendarEvent(a, "zzz", id); err == nil {
		t.Fatal("snapshot crossed game")
	}
	now = now.Add(31 * time.Minute)
	if _, _, err := s.CalendarEvent(a, "genshin", id); err == nil || calls.Load() != 2 {
		t.Fatal("expired snapshot accepted or fetched automatically")
	}
}

func TestRecordQueriesSerializeAndRespondToRevocation(t *testing.T) {
	arrived := make(chan struct{})
	released := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(arrived)
		<-r.Context().Done()
		close(released)
	}))
	defer server.Close()
	s := New(mihoyo.NewClient(server.URL))
	defer s.Stop()
	a := model.Account{ID: "fixture", UserID: "owner", Cookie: "synthetic"}
	result := make(chan error, 1)
	go func() { _, err := s.Note(context.Background(), a, "genshin", "", ""); result <- err }()
	<-arrived
	got, err := s.Note(context.Background(), a, "starrail", "", "")
	if err != nil || got.Status != "busy" || calls.Load() != 1 {
		t.Fatal("same account sent simultaneous reads")
	}
	s.Invalidate(a.ID)
	if err := <-result; err == nil {
		t.Fatal("revoked request completed")
	}
	<-released
}

func TestRecordCancelledRequestDoesNotLoad(t *testing.T) {
	s := New(mihoyo.NewClient("http://127.0.0.1:1"))
	defer s.Stop()
	a := model.Account{ID: "fixture", UserID: "owner", Cookie: "synthetic"}
	s.entries[a.ID] = &entry{identity: identity(a), cache: map[string]model.RecordSnapshot{}, next: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Note(ctx, a, "genshin", "", ""); err == nil {
		t.Fatal("cancellation ignored")
	}
	if s.active != 0 || s.entries[a.ID].busy {
		t.Fatal("cancelled request leaked reservation")
	}
}

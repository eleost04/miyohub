package api

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestCustomCalendarAPI(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, token, _ := s.CreateAdmin("owner", "test-password")
	other, _ := s.CreateUser("other", "test-password", "user")
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "fixture", Stuid: "102001"})
	_ = s.AddAccountForUser(other.ID, model.Account{Name: "private", Stuid: "102002"})
	a := s.AccountsForUser(u.ID, false)[0]
	b := s.AccountsForUser(other.ID, false)[0]
	server := NewServer(s)
	defer server.Stop()
	h := server.Handler()
	input, _ := json.Marshal(map[string]any{"account_id": a.ID, "game": "zzz", "title": "示例更新", "kind": "version", "start_at": time.Now().Add(time.Hour)})
	if w := callAPI(h, "POST", "/api/v1/calendar/custom", "", string(input)); w.Code != 401 {
		t.Fatal("anonymous calendar mutation")
	}
	w := callAPI(h, "POST", "/api/v1/calendar/custom", token, string(input))
	if w.Code != 201 {
		t.Fatal("valid event rejected", w.Code)
	}
	w = callAPI(h, "GET", "/api/v1/calendar/custom?game=zzz&account_id="+b.ID, token, "")
	if w.Code != 404 {
		t.Fatal("admin calendar boundary")
	}
	w = callAPI(h, "GET", "/api/v1/game-record/calendar?game=zzz&account_id="+a.ID, token, "")
	if w.Code != 200 {
		t.Fatal("unintegrated source not handled")
	}
	if server.runner.Running() {
		t.Fatal("calendar started tasks")
	}
}

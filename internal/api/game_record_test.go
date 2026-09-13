package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eleost04/miyohub/internal/gamerecord"
	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestGameNoteAPIAccountBoundaryAndPrivacy(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, token, _ := s.CreateAdmin("owner", "test-password")
	other, _ := s.CreateUser("other", "test-password", "user")
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "fixture", Cookie: "stuid=80101;cookie_token=synthetic-secret"})
	_ = s.AddAccountForUser(other.ID, model.Account{Name: "private", Stuid: "80102"})
	a := s.AccountsForUser(u.ID, false)[0]
	b := s.AccountsForUser(other.ID, false)[0]
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == mihoyo.AccountRolesPath {
			_, _ = w.Write([]byte(`{"retcode":0,"data":{"list":[{"game_uid":"80201","region":"cn_gf01"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"retcode":0,"data":{"current_resin":12,"max_resin":200,"cookie":"synthetic-secret"}}`))
	}))
	defer upstream.Close()
	server := NewServer(s)
	defer server.Stop()
	server.records.Stop()
	server.records = gamerecord.New(mihoyo.NewClient(upstream.URL))
	h := server.Handler()
	path := "/api/v1/game-record/note?account_id=" + a.ID + "&game=genshin"
	if w := callAPI(h, "GET", path, "", ""); w.Code != 401 {
		t.Fatal("anonymous read")
	}
	if w := callAPI(h, "GET", strings.Replace(path, a.ID, b.ID, 1), token, ""); w.Code != 404 {
		t.Fatal("admin crossed ownership")
	}
	if w := callAPI(h, "GET", path+"&game=zzz", token, ""); w.Code != 400 {
		t.Fatal("duplicate parameters accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized upstream read")
	}
	w := callAPI(h, "GET", path, token, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "synthetic-secret") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") || calls.Load() != 2 {
		t.Fatal("unsafe note response", w.Code)
	}
	if w := callAPI(h, "GET", path+"&role_id=89999&server=cn_gf01", token, ""); w.Code != 400 || calls.Load() != 2 {
		t.Fatal("arbitrary role read")
	}
	_ = callAPI(h, "GET", "/api/v1/status", token, "")
	_ = callAPI(h, "GET", "/api/v1/bootstrap", token, "")
	if calls.Load() != 2 {
		t.Fatal("status poll triggered upstream records")
	}
}

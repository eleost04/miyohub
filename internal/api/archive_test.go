package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
	"github.com/eleost04/miyohub/internal/transfer"
)

func TestArchiveAPIAuthAndEncryptedResponse(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, token, _ := s.CreateAdmin("owner", "test-password")
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "fixture-account", Cookie: "stuid=50101;cookie_token=synthetic-marker"})
	server := NewServer(s)
	defer server.Stop()
	h := server.Handler()
	input := `{"password":"test-password","passphrase":"synthetic-transfer-passphrase"}`
	if w := callAPI(h, "POST", "/api/v1/profile/archive/export", "", input); w.Code != 401 {
		t.Fatal("auth missing")
	}
	if w := callAPI(h, "POST", "/api/v1/profile/archive/export", token, strings.Replace(input, "test-password", "wrong", 1)); w.Code != 400 {
		t.Fatal("password bypass")
	}
	r := httptest.NewRequest("POST", "/api/v1/profile/archive/export", strings.NewReader(input))
	secureTestRequest(r)
	r.Header.Set("Origin", "https://foreign.invalid")
	r.AddCookie(&http.Cookie{Name: "miyohub_session", Value: token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("CSRF bypass")
	}
	w = callAPI(h, "POST", "/api/v1/profile/archive/export", token, input)
	if w.Code != 200 || strings.Contains(w.Body.String(), "synthetic-marker") || strings.Contains(w.Body.String(), "fixture-account") || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("unsafe export", w.Code)
	}
	var result struct {
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &result)
	archive, err := transfer.Open(result.Data, "synthetic-transfer-passphrase")
	if err != nil || len(archive.Accounts) != 1 {
		t.Fatal("export not decryptable", err)
	}
	previewBody, _ := json.Marshal(map[string]any{"archive": result.Data, "passphrase": "synthetic-transfer-passphrase"})
	w = callAPI(h, "POST", "/api/v1/profile/archive/preview", token, string(previewBody))
	var p struct {
		Data struct {
			ID      string               `json:"id"`
			Preview model.ArchivePreview `json:"preview"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if w.Code != 200 || p.Data.ID == "" || p.Data.Preview.Skipped != 1 || strings.Contains(w.Body.String(), "synthetic-marker") {
		t.Fatal("bad preview", w.Code)
	}
	if w := callAPI(h, "POST", "/api/v1/profile/archive/import", token, `{"id":"`+p.Data.ID+`"}`); w.Code != 400 {
		t.Fatal("implicit confirmation")
	}
	apply := `{"id":"` + p.Data.ID + `","confirm":true}`
	if w := callAPI(h, "POST", "/api/v1/profile/archive/import", token, apply); w.Code != 200 {
		t.Fatal("import rejected", w.Code)
	}
	if w := callAPI(h, "POST", "/api/v1/profile/archive/import", token, apply); w.Code != 400 {
		t.Fatal("replayed preview")
	}
	if server.runner.Running() {
		t.Fatal("migration started tasks")
	}
	for i := 0; i < 7; i++ {
		w = callAPI(h, "POST", "/api/v1/profile/archive/export", token, `{}`)
	}
	if w.Code != 429 {
		t.Fatal("missing expensive-operation rate limit")
	}
}

func TestArchivePreviewLifecycleAndSingleConsumer(t *testing.T) {
	var sessions archiveSessions
	defer sessions.clear()
	p, err := sessions.reserve("owner")
	if err != nil {
		t.Fatal(err)
	}
	a := model.AccountArchive{Version: 1}
	if !sessions.complete("owner", p.id, a) {
		t.Fatal("preview not completed")
	}
	if _, err := sessions.consume("other", p.id); err == nil {
		t.Fatal("preview crossed ownership")
	}
	sessions.cancel("owner", "")
	if sessions.complete("owner", p.id, a) {
		t.Fatal("revocation revived by late completion")
	}
	p, _ = sessions.reserve("owner")
	newer, _ := sessions.reserve("owner")
	sessions.cancel("owner", p.id)
	if sessions.complete("owner", p.id, a) || !sessions.complete("owner", newer.id, a) {
		t.Fatal("stale request invalidated new preview")
	}
	var wg sync.WaitGroup
	success := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Go(func() { _, err := sessions.consume("owner", newer.id); success <- err == nil })
	}
	wg.Wait()
	close(success)
	count := 0
	for ok := range success {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatal("preview consumed more than once")
	}
	p, _ = sessions.reserve("owner")
	sessions.complete("owner", p.id, a)
	sessions.mu.Lock()
	expired := sessions.pending["owner"]
	expired.expires = time.Now().Add(-time.Second)
	sessions.pending["owner"] = expired
	sessions.mu.Unlock()
	if _, err := sessions.consume("owner", p.id); err == nil {
		t.Fatal("expired preview consumed")
	}
	if !sessions.begin() || !sessions.begin() || sessions.begin() {
		t.Fatal("expensive-operation concurrency bound")
	}
	sessions.end()
	sessions.end()
	sessions.clear()
	if sessions.begin() || sessions.complete("owner", p.id, a) {
		t.Fatal("stopped sessions revived")
	}
	if _, err := sessions.reserve("owner"); err == nil {
		t.Fatal("stopped sessions reserved")
	}
}

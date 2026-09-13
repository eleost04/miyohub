package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/transfer"
)

type archivePreview struct {
	id      string
	expires time.Time
	archive model.AccountArchive
	timer   *time.Timer
}
type archiveSessions struct {
	mu      sync.Mutex
	active  int
	stopped bool
	pending map[string]archivePreview
}

func (a *archiveSessions) begin() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped || a.active >= 2 {
		return false
	}
	a.active++
	return true
}
func (a *archiveSessions) end() { a.mu.Lock(); a.active--; a.mu.Unlock() }
func (a *archiveSessions) clear() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopped = true
	for _, p := range a.pending {
		p.timer.Stop()
	}
	clear(a.pending)
}

// Empty id is reserved for server-side revocation. Browser cancellation is
// scoped to a preview so a delayed DELETE cannot cancel a newer one.
func (a *archiveSessions) cancel(userID, id string) {
	a.mu.Lock()
	if p, ok := a.pending[userID]; ok && (id == "" || p.id == id) {
		p.timer.Stop()
		delete(a.pending, userID)
	}
	a.mu.Unlock()
}

func (a *archiveSessions) reserve(userID string) (archivePreview, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	old, exists := a.pending[userID]
	if a.stopped || !exists && len(a.pending) >= 100 {
		return archivePreview{}, errors.New("迁移预览暂忙，请稍后重试")
	}
	if exists {
		old.timer.Stop()
	}
	if a.pending == nil {
		a.pending = map[string]archivePreview{}
	}
	p := archivePreview{id: rand.Text(), expires: time.Now().Add(5 * time.Minute)}
	p.timer = time.AfterFunc(time.Until(p.expires), func() { a.cancel(userID, p.id) })
	a.pending[userID] = p
	return p, nil
}

func (a *archiveSessions) complete(userID, id string, archive model.AccountArchive) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[userID]
	if a.stopped || !ok || p.id != id || !p.expires.After(time.Now()) {
		return false
	}
	p.archive = archive
	a.pending[userID] = p
	return true
}

func (a *archiveSessions) consume(userID, id string) (model.AccountArchive, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.pending[userID]
	if a.stopped || !ok || p.id != id || p.archive.Version != 1 || !p.expires.After(time.Now()) {
		return model.AccountArchive{}, errors.New("预览已失效，请重新选择迁移文件")
	}
	p.timer.Stop()
	delete(a.pending, userID)
	return p.archive, nil
}

func (s *Server) archiveGate(w http.ResponseWriter, user model.User) bool {
	if !s.limits.allow("archive:"+user.ID, 6, time.Minute) || !s.archives.begin() {
		rateError(w)
		return false
	}
	return true
}

func (s *Server) archiveExport(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.archiveGate(w, user) {
		return
	}
	defer s.archives.end()
	var input struct {
		Password   string `json:"password"`
		Passphrase string `json:"passphrase"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := transfer.ValidatePassphrase(input.Passphrase); err != nil {
		writeError(w, 400, err)
		return
	}
	archive, err := s.store.ExportAccountArchive(user.ID, input.Password)
	input.Password = ""
	if err != nil {
		writeError(w, 400, err)
		return
	}
	envelope, err := transfer.Seal(archive, input.Passphrase)
	input.Passphrase = ""
	if err != nil {
		writeError(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "data": envelope})
}

func (s *Server) archivePreview(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method == http.MethodDelete {
		var input struct {
			ID string `json:"id"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if input.ID == "" {
			writeError(w, 400, errors.New("缺少预览标识"))
			return
		}
		s.archives.cancel(user.ID, input.ID)
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	if !s.archiveGate(w, user) {
		return
	}
	defer s.archives.end()
	var input struct {
		Archive    json.RawMessage `json:"archive"`
		Passphrase string          `json:"passphrase"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	pending, err := s.archives.reserve(user.ID)
	if err != nil {
		writeError(w, 503, err)
		return
	}
	completed := false
	defer func() {
		if !completed {
			s.archives.cancel(user.ID, pending.id)
		}
	}()
	archive, err := transfer.Open(input.Archive, input.Passphrase)
	input.Passphrase = ""
	if err != nil {
		writeError(w, 400, err)
		return
	}
	preview, err := s.store.PreviewAccountArchive(user.ID, archive)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	if r.Context().Err() != nil || !s.archives.complete(user.ID, pending.id, archive) {
		writeError(w, 409, errors.New("迁移预览已取消，请重新预览"))
		return
	}
	completed = true
	writeJSON(w, 200, map[string]any{"ok": true, "data": map[string]any{"id": pending.id, "expires_at": pending.expires, "preview": preview}})
}

func (s *Server) archiveImport(w http.ResponseWriter, r *http.Request, user model.User) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var input struct {
		ID      string `json:"id"`
		Confirm bool   `json:"confirm"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !input.Confirm {
		writeError(w, 400, errors.New("请确认导入的账号默认停用且不自动执行"))
		return
	}
	archive, err := s.archives.consume(user.ID, input.ID)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	result, err := s.store.ImportAccountArchive(user.ID, archive)
	if err != nil {
		writeError(w, 400, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "data": result})
}

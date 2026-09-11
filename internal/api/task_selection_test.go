package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
)

func TestTemporaryTaskSelectionValidationAndReadback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	u, token, err := s.CreateAdmin("admin", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "main", Cookie: "test-cookie"}); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(u.ID, false)[0]
	server := NewServer(s)
	defer server.Stop()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"games_only":true,"bbs_only":true}`, `{"games":["invalid"]}`, `{"bbs_only":true,"games":["genshin"]}`} {
		res := callAPI(server.Handler(), http.MethodPost, "/api/v1/run", token, body)
		if res.Code != 400 || server.runner.Running() {
			t.Fatal("invalid selection reserved tasks", res.Code)
		}
	}
	// A filter must not turn on this account's disabled BBS family.
	if res := callAPI(server.Handler(), http.MethodPost, "/api/v1/run", token, `{"bbs_only":true}`); res.Code != 409 || server.runner.Running() {
		t.Fatal("selection enabled a disabled feature", res.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/run", strings.NewReader(`{"account_ids":["`+a.ID+`"],"games_only":true,"games":["genshin"]}`))
	input, ok := server.readTaskSelection(httptest.NewRecorder(), req, u)
	if !ok || !input.GamesOnly || !reflect.DeepEqual(input.Games, []string{"genshin"}) || !reflect.DeepEqual(input.AccountIDs, []string{a.ID}) {
		t.Fatal("task filter did not survive API decoding")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("validating temporary selection modified state")
	}
}

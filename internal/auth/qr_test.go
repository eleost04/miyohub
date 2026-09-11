package auth

import (
	"context"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestQRNestedTokensAndGeneration(t *testing.T) {
	s, _ := store.New(filepath.Join(t.TempDir(), "state.json"))
	u, _, _ := s.CreateAdmin("admin", "password123")
	m := NewQRManager(s)
	old := &qrSession{state: model.LoginState{Running: true}}
	current := &qrSession{state: model.LoginState{Running: true}}
	m.sessions[u.ID] = current
	m.finish(u.ID, old, "error", nil)
	if !m.State(u.ID).Running {
		t.Fatal("old generation overwrote new login")
	}
	m.client.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		body := `{"retcode":0,"data":{"ltoken":"lt","cookie_token":"ct"}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	payload := map[string]any{"user_info": map[string]any{"mid": "mid", "aid": "100"}, "tokens": []any{map[string]any{"token": "st"}}}
	if err := m.complete(context.Background(), u.ID, current, model.Config{}, "main", payload); err != nil {
		t.Fatal(err)
	}
	a := s.AccountsForUser(u.ID, false)
	if len(a) != 1 || a[0].Stoken != "st" || !strings.Contains(a[0].Cookie, "cookie_token=ct") {
		t.Fatal("QR did not save nested tokens")
	}
}

func TestQRHeadersUseCanonicalKeysForSignatureReplacement(t *testing.T) {
	for _, headers := range []http.Header{qrHeaders(model.Config{}), qrQueryHeaders(model.Config{}, "ticket")} {
		for key := range headers {
			if http.CanonicalHeaderKey(key) != key {
				t.Errorf("non-canonical header %s can bypass Set/Del", key)
			}
		}
		if len(headers.Values("DS")) != 1 {
			t.Fatal("missing or duplicated signature")
		}
	}
}

func TestQRManagerRejectsStartAfterStop(t *testing.T) {
	m, u := smsFixture(t)
	qr := NewQRManager(m.store)
	qr.Stop()
	if _, err := qr.Start(context.Background(), u.ID, "test"); err == nil {
		t.Fatal("QR login started after shutdown")
	}
}

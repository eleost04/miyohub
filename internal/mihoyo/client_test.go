package mihoyo

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fakeTransport func(*http.Request) (*http.Response, error)

func (f fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestErrorsDoNotExposeQueryCredentials(t *testing.T) {
	client := NewClient("")
	client.HTTP.Transport = fakeTransport(func(r *http.Request) (*http.Response, error) { return nil, errors.New("connection refused") })
	var out map[string]any
	err := client.JSON(context.Background(), "GET", "https://api.example.test/path", url.Values{"stoken": {"PRIVATE-TOKEN"}}, nil, nil, &out)
	if err == nil || strings.Contains(err.Error(), "PRIVATE-TOKEN") || strings.Contains(err.Error(), "https://") {
		t.Fatal("unsafe network error", err)
	}
}
func TestClientRejectsRedirectAndLargeResponse(t *testing.T) {
	for _, large := range []bool{false, true} {
		client := NewClient("")
		calls := 0
		client.HTTP.Transport = fakeTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if large {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Repeat(" ", (4<<20)+1)))}, nil
			}
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://other.example.test/"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		})
		var out map[string]any
		err := client.JSON(context.Background(), "GET", "https://api.example.test/path", nil, nil, http.Header{"Cookie": {"secret"}}, &out)
		if err == nil || calls != 1 {
			t.Fatal("response/redirect not rejected", err, calls)
		}
	}
}

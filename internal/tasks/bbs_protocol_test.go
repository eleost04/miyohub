package tasks

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func TestBBSUsesEndpointSpecificHeadersAndSignatures(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		body               any
		web, bodySignature bool
	}{
		{"mission-state", "GET", "/apihub/wapi/getUserMissionsState", nil, true, false},
		{"share", "GET", "/apihub/api/getShareConf", nil, true, false},
		{"community-sign", "POST", mihoyo.BBSSignPath, map[string]string{"gids": "5"}, false, true},
		{"like", "POST", "/apihub/sapi/upvotePost", map[string]any{"post_id": "p1", "is_cancel": false}, false, false},
		{"compatible-like", "POST", "/post/api/post/upvote", map[string]any{"post_id": "p1", "is_cancel": true, "gids": "2"}, false, false},
		{"captcha-verify", "POST", "/misc/api/verifyVerification", map[string]string{"geetest_validate": "fixture"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := mihoyo.NewClient("")
			client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				if tc.web {
					if r.Header.Get("Cookie") != "cookie_token=fixture-web" || !strings.Contains(r.UserAgent(), "Mozilla/") || r.Header.Get("Origin") != "https://webstatic.mihoyo.com" || r.Header.Get("X-Requested-With") != "com.mihoyo.hyperion" || r.Header.Get("DS") != "" || r.Header.Get("x-rpc-client_type") != "" {
						t.Error("web request was mixed with app credentials/headers")
					}
				} else {
					if r.Header.Get("Cookie") != "stuid=100;stoken=v2_fixture;mid=fixture-mid" || r.UserAgent() != "okhttp/4.9.3" || r.Header.Get("x-rpc-h265_supported") != "1" {
						t.Error("app request did not retain its own protocol profile")
					}
					parts := strings.Split(r.Header.Get("DS"), ",")
					if len(parts) != 3 || len(r.Header.Values("DS")) != 1 {
						t.Fatal("missing or duplicate DS signature")
					}
					salt := mihoyo.BBSSalt
					if tc.bodySignature {
						salt = mihoyo.BBSX6Salt
					}
					signed := fmt.Sprintf("salt=%s&t=%s&r=%s", salt, parts[0], parts[1])
					if tc.bodySignature {
						raw, _ := io.ReadAll(r.Body)
						signed += "&b=" + string(raw) + "&q="
					}
					if parts[2] != fmt.Sprintf("%x", md5.Sum([]byte(signed))) {
						t.Error("wrong DS profile or signature does not match wire body")
					}
				}
				return reply(`{"retcode":0,"data":{}}`), nil
			})
			account := model.Account{Cookie: "cookie_token=fixture-web", Stuid: "100", Stoken: "v2_fixture", Mid: "fixture-mid"}
			_, err := (BBSCheckin{Client: client, Account: account}).request(t.Context(), tc.method, tc.path, nil, tc.body, false)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBBSUpvoteCompatibilityNeverReplaysUncertainOrRejectedWrites(t *testing.T) {
	for _, tc := range []struct {
		name            string
		status          int
		body            string
		transportError  bool
		wantAlternative bool
	}{
		{"primary-ok", 200, `{"retcode":0,"data":{}}`, false, false},
		{"missing-route", 404, `{}`, false, true},
		{"gone-route", 410, `{}`, false, true},
		{"network-unknown", 0, "", true, false},
		{"invalid-json-unknown", 200, `{`, false, false},
		{"forbidden", 403, `{}`, false, false},
		{"rate-limited", 429, `{}`, false, false},
		{"server-error", 503, `{}`, false, false},
		{"credentials", 200, `{"retcode":-100}`, false, false},
		{"captcha", 200, `{"retcode":1034}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			primary, alternative := 0, 0
			client := mihoyo.NewClient("")
			client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || r.Method != "POST" || body["post_id"] != "fixture-post" || body["is_cancel"] != false {
					t.Fatal("unexpected vote request")
				}
				switch r.URL.Path {
				case mihoyo.BBSUpvotePath:
					primary++
					if len(body) != 2 {
						t.Error("primary route used the wrong payload profile")
					}
					if tc.transportError {
						return nil, io.ErrUnexpectedEOF
					}
					result := reply(tc.body)
					result.StatusCode = tc.status
					return result, nil
				case mihoyo.BBSPostVotePath:
					alternative++
					if body["gids"] != "2" || len(body) != 3 {
						t.Error("compatibility route omitted its forum ID")
					}
					return reply(`{"retcode":0,"data":{}}`), nil
				default:
					t.Fatal("unexpected fallback or solver call", r.URL.Path)
					return nil, nil
				}
			})
			_, err := (BBSCheckin{Client: client}).upvote(t.Context(), "fixture-post", "2", false)
			wantAlternative := 0
			if tc.wantAlternative {
				wantAlternative = 1
			}
			if primary != 1 || alternative != wantAlternative || (err == nil) != (tc.name == "primary-ok" || tc.wantAlternative) {
				t.Fatal("vote fallback or error classification mismatch", primary, alternative, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (BBSCheckin{}).upvote(ctx, "fixture", "2", true); err != context.Canceled {
		t.Fatal("cancelled operation reached a vote endpoint")
	}
}

func TestBBSAbsentMissionListDoesNotEnableUnlistedInteractions(t *testing.T) {
	for _, state := range []map[string]any{{}, {"states": nil}, {"states": []any{}}, {"states": "malformed"}} {
		got := missions(state, model.BBSConfig{Checkin: true, Read: true, Like: true, Share: true})
		if got[0].remaining != 1 {
			t.Fatal("missing list suppressed selected community sign-in")
		}
		for _, item := range got[1:] {
			if item.remaining != 0 {
				t.Fatal("unlisted interaction became a task")
			}
		}
	}
}

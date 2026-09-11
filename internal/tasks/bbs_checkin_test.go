package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestBBSStateRetriesOnlyTransportFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body            string
		transient, diagnostic bool
		wantCalls             int
	}{
		{"transient-recovers", `{"retcode":0,"data":{"can_get_points":0}}`, true, false, 2},
		{"credentials", `{"retcode":-100,"message":"expired"}`, false, false, 1},
		{"captcha-is-not-network", `{"retcode":1034,"message":"verification"}`, false, false, 1},
		{"diagnostic-stays-single-read", ``, true, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := mihoyo.NewClient("")
			client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/apihub/wapi/getUserMissionsState" {
					t.Error("unexpected action during retry")
				}
				if tc.transient && calls == 1 {
					return nil, &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}
				}
				return reply(tc.body), nil
			})
			var details []string
			b := BBSCheckin{Client: client, Emit: func(message string) { details = append(details, message) }}
			if tc.diagnostic {
				_, _ = b.InspectState(t.Context())
			} else {
				_ = b.Run(t.Context())
			}
			if calls != tc.wantCalls {
				t.Fatal("incorrect read retries", calls)
			}
			recovered := strings.Contains(strings.Join(details, "\n"), "状态查询已恢复（已自动重试 1 次）")
			if recovered != (tc.name == "transient-recovers") {
				t.Fatal("retry recovery log does not match the actual query result", details)
			}
		})
	}
}

func TestBBSStateRetryBackoffIsCancellableAndWritesAreNotRetried(t *testing.T) {
	client := mihoyo.NewClient("")
	calls := 0
	client.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) { calls++; return nil, io.ErrUnexpectedEOF })
	b := BBSCheckin{Client: client}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if _, err := b.stateWithRetry(ctx); !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
		t.Fatal("retry backoff ignored cancellation", err, calls)
	}
	_, err := b.request(t.Context(), http.MethodPost, mihoyo.BBSSignPath, nil, map[string]any{"gids": 2}, true)
	if err == nil || calls != 2 {
		t.Fatal("uncertain sign-in was repeated", err, calls)
	}
}

func TestBBSStateRetryLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, retries := range []*int{nil, new(0), new(2), new(5), new(10)} {
			client := mihoyo.NewClient("")
			calls := 0
			client.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) { calls++; return nil, io.ErrUnexpectedEOF })
			cfg := model.Config{Network: model.NetworkConfig{BBSStateRetries: retries}}
			_, err := (BBSCheckin{Client: client, Config: cfg}).stateWithRetry(t.Context())
			if err == nil || calls != cfg.Network.StateRetries()+1 || !strings.Contains(err.Error(), "这不是验证码错误") {
				t.Fatal("unbounded retry or misleading error", calls, err)
			}
		}
	})
}

func TestBBSStateRetryHonorsHTTPBackoffAndDoesNotRetryPermanentErrors(t *testing.T) {
	for _, tc := range []struct {
		name, retryAfter string
		status           int
		wantCalls        int
		minWait          time.Duration
	}{
		{"temporary", "", 503, 2, time.Second},
		{"rate-limit", "45", 429, 2, 45 * time.Second},
		{"rate-limit-without-header", "", 429, 2, 30 * time.Second},
		{"long-upstream-cooldown", "120", 429, 1, 0},
		{"auth", "", 401, 1, 0},
		{"forbidden", "", 403, 1, 0},
		{"invalid-request", "", 400, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				client, calls := mihoyo.NewClient(""), 0
				client.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
					calls++
					r := reply(`{"retcode":0,"data":{}}`)
					if calls == 1 {
						r.StatusCode = tc.status
						r.Header.Set("Retry-After", tc.retryAfter)
					}
					return r, nil
				})
				start := time.Now()
				_, _ = (BBSCheckin{Client: client}).stateWithRetry(t.Context())
				if calls != tc.wantCalls || time.Since(start) < tc.minWait {
					t.Fatal("HTTP retry policy mismatch", calls, time.Since(start))
				}
			})
		})
	}
}
func TestBBSRunsOnlyRemainingMissions(t *testing.T) {
	calls := map[string]int{}
	cancelled := 0
	client := mihoyo.NewClient("")
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls[r.URL.Path]++
		switch r.URL.Path {
		case "/apihub/wapi/getUserMissionsState":
			if r.Header.Get("Cookie") != "cookie" {
				t.Fatal("task state requires web cookie")
			}
			if calls[r.URL.Path] > 1 {
				return reply(`{"retcode":0,"data":{"can_get_points":0,"already_received_points":100,"states":[{"mission_id":58,"is_get_award":true},{"mission_id":59,"is_get_award":true},{"mission_id":60,"is_get_award":true},{"mission_id":61,"is_get_award":true}]}}`), nil
			}
			return reply(`{"retcode":0,"data":{"can_get_points":20,"already_received_points":80,"states":[{"mission_id":58,"is_get_award":true},{"mission_id":59,"happened_times":2},{"mission_id":60,"happened_times":4},{"mission_id":61,"is_get_award":true}]}}`), nil
		case "/post/api/getForumPostList":
			if r.URL.Query().Get("forum_id") != "26" {
				t.Fatal("wrong forum ID")
			}
			return reply(`{"retcode":0,"data":{"list":[{"post":{"post_id":"p1","subject":"test"}}]}}`), nil
		case mihoyo.BBSUpvotePath:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["is_cancel"] == true {
				cancelled++
			}
			if !strings.Contains(r.Header.Get("Cookie"), "stoken=token") {
				t.Fatal("like requires stoken")
			}
		}
		return reply(`{"retcode":0,"data":{}}`), nil
	})
	cfg := model.Config{BBS: model.BBSConfig{Forums: []int{2}, Checkin: true, Read: true, Like: true, Share: true, CancelLike: true, PostLimit: 5, DelaySeconds: []int{0, 0}}}
	result := (BBSCheckin{Client: client, Config: cfg, Account: model.Account{Cookie: "cookie", Stuid: "100", Stoken: "token"}}).Run(context.Background())
	if result.Success != 3 || result.Failed != 0 || cancelled != 1 || calls["/post/api/getPostFull"] != 1 || calls[mihoyo.BBSSignPath] != 0 || calls["/apihub/api/getShareConf"] != 0 {
		t.Fatalf("unexpected execution %+v %#v", result, calls)
	}
}
func TestBBSStopsOnInvalidCredentials(t *testing.T) {
	calls := 0
	client := mihoyo.NewClient("")
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return reply(`{"retcode":-100,"message":"expired"}`), nil
	})
	result := (BBSCheckin{Client: client}).Run(context.Background())
	if calls != 1 || result.Failed != 1 {
		t.Fatal("invalid credentials must not run actions")
	}
}

func TestBBSAlreadyCompleteIsNotNewEarnings(t *testing.T) {
	calls := 0
	client := mihoyo.NewClient("")
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/apihub/wapi/getUserMissionsState" {
			t.Fatal("completed task triggered an action")
		}
		return reply(`{"retcode":0,"data":{"already_received_points":50,"can_get_points":0}}`), nil
	})
	result := (BBSCheckin{Client: client}).Run(t.Context())
	if calls != 1 || result.Success != 0 || result.Failed != 0 || result.Status != "already_complete" || !strings.Contains(result.Reason, "今日已领取 50") {
		t.Fatal("completion check was misreported as an action or failure", result, calls)
	}
}

func TestBBSOmitsRemovedInteractionMissions(t *testing.T) {
	for _, states := range []string{`[]`, `[{"mission_id":58,"is_get_award":true}]`} {
		t.Run(states, func(t *testing.T) {
			calls := 0
			signs := 0
			statesCalls := 0
			client := mihoyo.NewClient("")
			client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path == mihoyo.BBSSignPath {
					signs++
					return reply(`{"retcode":0,"data":{}}`), nil
				}
				if r.URL.Path != "/apihub/wapi/getUserMissionsState" {
					t.Error("removed mission triggered an interaction", r.URL.Path)
				}
				statesCalls++
				if statesCalls > 1 {
					return reply(`{"retcode":0,"data":{"can_get_points":0,"already_received_points":50,"states":` + states + `}}`), nil
				}
				return reply(`{"retcode":0,"data":{"can_get_points":20,"states":` + states + `}}`), nil
			})
			cfg := model.Config{BBS: model.BBSConfig{Forums: []int{2}, Checkin: true, Read: true, Like: true, Share: true, DelaySeconds: []int{0, 0}}}
			result := (BBSCheckin{Client: client, Config: cfg}).Run(context.Background())
			wantSign := 0
			if states == `[]` {
				wantSign = 1
			}
			if calls != 2+wantSign || signs != wantSign || result.Failed != 0 || result.Success != wantSign || result.Skipped != 4-wantSign {
				t.Fatal("removed missions were not skipped", result, calls)
			}
		})
	}
}

func TestBBSNewRulesOnboardingRowsDoNotSuppressCommunitySignIn(t *testing.T) {
	stateCalls, signs := 0, 0
	client := mihoyo.NewClient("")
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/apihub/wapi/getUserMissionsState" {
			stateCalls++
			if stateCalls == 1 {
				return reply(`{"retcode":0,"data":{"already_received_points":0,"can_get_points":50,"states":[{"mission_id":62,"mission_key":"bind_game_role_0","is_get_award":true},{"mission_id":64,"mission_key":"modify_signature_0","is_get_award":true}]}}`), nil
			}
			return reply(`{"retcode":0,"data":{"already_received_points":50,"can_get_points":0,"states":[]}}`), nil
		}
		if r.URL.Path != mihoyo.BBSSignPath {
			t.Error("removed interaction should not run", r.URL.Path)
		}
		signs++
		return reply(`{"retcode":0,"data":{}}`), nil
	})
	cfg := model.Config{BBS: model.BBSConfig{Forums: []int{5, 2}, Checkin: true, Read: true, Like: true, Share: false, DelaySeconds: []int{0, 0}}}
	result := (BBSCheckin{Client: client, Config: cfg}).Run(t.Context())
	if signs != 2 || result.Success != 2 || result.Failed != 0 {
		t.Fatal("daily community signs were suppressed by missing mission 58", result, signs)
	}
}

func TestBBSDiagnosticsExplainMissingMissionsWithoutDumpingUpstreamData(t *testing.T) {
	queries := 0
	messages := []string{}
	client := mihoyo.NewClient("")
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == mihoyo.BBSStatePath {
			queries++
			if queries == 1 {
				return reply(`{"retcode":0,"data":{"already_received_points":0,"can_get_points":50,"total_points":2000,"private_field":"DO_NOT_LOG_UPSTREAM","states":[{"mission_id":64,"name":"DO_NOT_LOG_NAME"},{"mission_id":62}]}}`), nil
			}
			return reply(`{"retcode":0,"data":{"already_received_points":50,"can_get_points":0,"total_points":2050,"states":[]}}`), nil
		}
		if r.URL.Path != mihoyo.BBSSignPath {
			t.Error("diagnostics triggered an interaction", r.URL.Path)
		}
		return reply(`{"retcode":0,"data":{}}`), nil
	})
	cfg := model.Config{BBS: model.BBSConfig{Forums: []int{2}, Checkin: true, Read: true, Like: true, Share: false, DelaySeconds: []int{0, 0}}}
	result := (BBSCheckin{Client: client, Config: cfg, Emit: func(line string) { messages = append(messages, line) }}).Run(t.Context())
	log := strings.Join(messages, "\n")
	for _, want := range []string{"任务 ID：62、64", "看帖开启", "点赞开启", "分享关闭", "看帖：任务列表未返回对应项目（ID 59），本次跳过", "点赞：任务列表未返回对应项目（ID 60），本次跳过", "分享：账号设置未开启，本次跳过", "米游币本次新增 50"} {
		if !strings.Contains(log, want) {
			t.Fatal("missing diagnostic reason", want, log)
		}
	}
	if strings.Contains(log, "DO_NOT_LOG") || queries != 2 || result.Success != 1 || result.Failed != 0 {
		t.Fatal("diagnostics changed execution or exposed upstream fields", result, queries)
	}
}

func TestBBSDoesNotClaimSuccessWithoutPointsOrRemainingMissionCompletion(t *testing.T) {
	client := mihoyo.NewClient("")
	messages := []string{}
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/apihub/wapi/getUserMissionsState" {
			return reply(`{"retcode":0,"data":{"already_received_points":0,"can_get_points":50,"states":[]}}`), nil
		}
		return reply(`{"retcode":0,"data":{}}`), nil
	})
	result := (BBSCheckin{Client: client, Config: model.Config{BBS: model.BBSConfig{Forums: []int{2}, Checkin: true, DelaySeconds: []int{0, 0}}}, Emit: func(s string) { messages = append(messages, s) }}).Run(t.Context())
	if result.Failed == 0 || !strings.Contains(strings.Join(messages, "\n"), "增币尚未确认") {
		t.Fatal("zero gain reported as complete", result, messages)
	}
}

func TestBBSHeadersReplaceSignatureWithoutDuplicateKeys(t *testing.T) {
	for _, body := range []any{nil, map[string]any{"gids": 2}} {
		headers := (BBSCheckin{}).headers(mihoyo.BBSSignPath, body)
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

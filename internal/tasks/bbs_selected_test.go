package tasks

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
)

func TestBBSSelectedModeDoesNotDependOnRewardMissions(t *testing.T) {
	for _, tc := range []struct {
		name, states                       string
		available, postLimit, reads, likes int
	}{
		{"no-rewards", `[]`, 0, 5, 3, 5},
		{"missing-missions", `[{"mission_id":62}]`, 30, 5, 3, 5},
		{"claimed-missions", `[{"mission_id":58,"is_get_award":true},{"mission_id":59,"is_get_award":true},{"mission_id":60,"is_get_award":true}]`, 0, 5, 3, 5},
		{"limited-posts", `[]`, 0, 2, 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, logs := map[string]int{}, []string{}
			client := mihoyo.NewClient("")
			client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				calls[r.URL.Path]++
				switch r.URL.Path {
				case mihoyo.BBSStatePath:
					return reply(fmt.Sprintf(`{"retcode":0,"data":{"can_get_points":%d,"already_received_points":50,"total_points":2050,"states":%s}}`, tc.available, tc.states)), nil
				case "/post/api/getForumPostList":
					return reply(`{"retcode":0,"data":{"list":[{"post":{"post_id":"1","subject":"fixture"}},{"post":{"post_id":"2"}},{"post":{"post_id":"3"}},{"post":{"post_id":"4"}},{"post":{"post_id":"5"}},{"post":{"post_id":"6"}}]}}`), nil
				case mihoyo.BBSSignPath, "/post/api/getPostFull", mihoyo.BBSUpvotePath:
					return reply(`{"retcode":0,"data":{}}`), nil
				default:
					t.Error("unselected or unexpected action", r.URL.Path)
					return reply(`{"retcode":-1}`), nil
				}
			})
			cfg := model.Config{BBS: model.BBSConfig{RunAllSelected: true, Forums: []int{2}, Checkin: true, Read: true, Like: true, Share: false, PostLimit: tc.postLimit, DelaySeconds: []int{0, 0}}}
			got := (BBSCheckin{Client: client, Config: cfg, Emit: func(s string) { logs = append(logs, s) }}).Run(t.Context())
			if calls[mihoyo.BBSStatePath] != 2 || calls[mihoyo.BBSSignPath] != 1 || calls["/post/api/getPostFull"] != tc.reads || calls[mihoyo.BBSUpvotePath] != tc.likes || got.Success != 1+tc.reads+tc.likes || got.Failed != 0 {
				t.Fatal("selected actions still depend on rewards", got, calls)
			}
			log := strings.Join(logs, "\n")
			if !strings.Contains(log, "按所选项目执行") || !strings.Contains(log, "米游币本次新增 0") || strings.Contains(log, "增币尚未确认") || strings.Contains(log, "未列出的互动项目不执行") {
				t.Fatal("selected-mode reporting is misleading", log)
			}
		})
	}
}

func TestBBSSelectedModeStillStopsOnExpiredCredentials(t *testing.T) {
	for _, expiredAt := range []string{mihoyo.BBSStatePath, mihoyo.BBSSignPath} {
		calls := 0
		client := mihoyo.NewClient("")
		client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.URL.Path == expiredAt {
				return reply(`{"retcode":-100,"message":"expired"}`), nil
			}
			if r.URL.Path != mihoyo.BBSStatePath {
				t.Error("continued after credential failure")
			}
			return reply(`{"retcode":0,"data":{"can_get_points":0}}`), nil
		})
		cfg := model.Config{BBS: model.BBSConfig{RunAllSelected: true, Forums: []int{2}, Checkin: true, Read: true, Like: true, DelaySeconds: []int{0, 0}}}
		got := (BBSCheckin{Client: client, Config: cfg}).Run(t.Context())
		want := 1
		if expiredAt == mihoyo.BBSSignPath {
			want = 2
		}
		if got.Failed != 1 || got.Success != 0 || calls != want {
			t.Fatal("credential failure was ignored", got, calls)
		}
	}
}

package api

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/store"
	"github.com/eleost04/miyohub/internal/tasks"
)

type taskTransport func(*http.Request) (*http.Response, error)

func (f taskTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Each local transport callback saves settings while a real Runner HTTP request
// is in flight. No production state, upstream account or notification is used.
func TestPreferenceSavesPreserveWorkButActualRevocationCancels(t *testing.T) {
	for _, kind := range []string{"unchanged-tasks", "schedule", "automatic", "unchanged-captcha", "unchanged-account", "rename-account", "changed-tasks", "changed-captcha", "stop", "disable-account", "disable-site"} {
		t.Run(kind, func(t *testing.T) {
			state, err := store.New(filepath.Join(t.TempDir(), "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			u, token, err := state.CreateAdmin("admin", "fixture-password")
			if err != nil {
				t.Fatal(err)
			}
			if err := state.AddAccountForUser(u.ID, model.Account{Name: "fixture", Cookie: "fixture-cookie"}); err != nil {
				t.Fatal(err)
			}
			a := state.AccountsForUser(u.ID, false)[0]
			p := *a.TaskSettings
			p.Features = model.Features{BBSTasks: true}
			p.BBS = model.BBSConfig{Forums: []int{5}, Checkin: true, PostLimit: 3, DelaySeconds: []int{0, 0}}
			a, err = state.UpdateAccountTasks(u, a.ID, p)
			if err != nil {
				t.Fatal(err)
			}
			server := NewServer(state)
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			defer server.Stop()
			handler := server.Handler()
			queries, signs := 0, 0
			wantStop := ""
			http.DefaultTransport = taskTransport(func(request *http.Request) (*http.Response, error) {
				body := `{"retcode":0,"data":{}}`
				switch request.URL.Path {
				case "/apihub/wapi/getUserMissionsState":
					queries++
					if queries == 1 {
						path := "/api/v1/accounts/tasks"
						var input any
						settings := *a.TaskSettings
						switch kind {
						case "schedule":
							settings.Schedule = &model.AccountSchedule{Time: "12:34", Timezone: "UTC"}
						case "automatic":
							settings.Automatic = false
						case "changed-tasks":
							settings.BBS.Checkin = false
							wantStop = "签到项目或任务参数已修改"
						}
						input = map[string]any{"id": a.ID, "task_settings": settings}
						method := http.MethodPut
						switch kind {
						case "unchanged-account", "rename-account":
							path = "/api/v1/accounts"
							name := a.Name
							if kind == "rename-account" {
								name = "renamed"
							}
							input = map[string]any{"id": a.ID, "name": name, "disabled": false}
						case "unchanged-captcha", "changed-captcha":
							path = "/api/v1/captcha/config"
							captcha := state.CaptchaSettingsForUser(u.ID).UserCaptchaConfig
							if kind == "changed-captcha" {
								captcha.Source = "off"
								wantStop = "验证码服务配置已修改"
							}
							input = captcha
						case "stop":
							path, method = "/api/v1/run/cancel", http.MethodPost
							input = map[string]any{"account_ids": []string{a.ID}}
							wantStop = "用户主动停止任务"
						case "disable-account":
							path, input = "/api/v1/accounts", map[string]any{"id": a.ID, "disabled": true}
							wantStop = "账号凭据或所属用户的状态、权限已变更"
						case "disable-site":
							path, input = "/api/v1/config", map[string]any{"enabled": false}
							wantStop = "管理员已关闭站点任务服务"
						}
						if res := callAPI(handler, method, path, token, jsonBody(t, input)); res.Code != 200 {
							t.Errorf("preference save failed: %s: %d", kind, res.Code)
						}
						body = `{"retcode":0,"data":{"can_get_points":30,"already_received_points":0,"states":[]}}`
					} else {
						body = `{"retcode":0,"data":{"can_get_points":0,"already_received_points":30,"states":[]}}`
					}
				case mihoyo.BBSSignPath:
					signs++
				default:
					t.Error("unexpected upstream action", request.URL.Path)
				}
				if request.Context().Err() != nil {
					return nil, request.Context().Err()
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			_ = server.runner.RunWithOptions(t.Context(), []string{a.ID}, tasks.RunOptions{Automatic: true})
			stored, _ := state.AccountForUser(u.ID, false, a.ID)
			result := stored.TaskResults["bbs"]
			if wantStop == "" {
				if result.Success != 1 || result.Failed != 0 || signs != 1 || queries != 2 {
					t.Fatal("equivalent or schedule-only save interrupted the task", result, signs, queries)
				}
			} else {
				if signs != 0 || result.Status != "cancelled" || !strings.Contains(result.Reason, wantStop) {
					t.Fatal("real revocation was ignored or stop reason was lost", result, signs)
				}
				logged := false
				for _, item := range state.LogsForUser(u.ID, false) {
					if strings.Contains(item.Message, "任务已停止："+wantStop) {
						logged = true
					}
				}
				if !logged {
					t.Fatal("cancellation reason not logged")
				}
			}
		})
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eleost04/miyohub/internal/mihoyo"
	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/processlock"
	"github.com/eleost04/miyohub/internal/store"
)

func cliFixture(t *testing.T) (string, *store.Store, model.User) {
	t.Helper()
	directory := t.TempDir()
	s, err := store.New(filepath.Join(directory, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	u, _, err := s.CreateAdmin("admin", "private-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "main", Cookie: "private-cookie", Stoken: "private-stoken", CloudTokens: map[string]string{"genshin": "private-cloud-token"}}); err != nil {
		t.Fatal(err)
	}
	return directory, s, u
}

func TestCLIHelpVersionAndInvalidArgsDoNotOpenState(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "not-created")
	t.Setenv("MIYOHUB_DATA_DIR", directory)
	for _, args := range [][]string{{"--help"}, {"version"}, {"--version"}, {"init", "--help"}, {"login", "--help"}, {"run", "--help"}, {"serve", "--help"}, {"show", "--help"}, {"logs", "--help"}} {
		var output bytes.Buffer
		if err := execute(t.Context(), args, strings.NewReader(""), &output, &output); err != nil || output.Len() == 0 {
			t.Fatal("help failed", args, err)
		}
	}
	for _, args := range [][]string{{"unknown"}, {"run", "--games-only", "--bbs-only"}, {"run", "--game", "unknown"}, {"run", "--bbs-only", "--game", "genshin"}, {"serve", "--port", "0"}, {"login", "--timeout", "121"}, {"init", "--admin", "admin"}, {"init", "--password-stdin"}, {"show", "unexpected"}, {"logs", "--tail", "0"}, {"logs", "--tail", "501"}} {
		if err := execute(t.Context(), args, strings.NewReader(""), io.Discard, io.Discard); err == nil {
			t.Fatal("invalid input accepted", args)
		}
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("help or validation initialized data")
	}
	o, err := parseCLI([]string{"--data-dir", "one", "run", "--data-dir", "two", "--game", "genshin", "--game", "zzz"}, io.Discard)
	if err != nil || o.dataDir != "two" || !reflect.DeepEqual(o.run.Games, []string{"genshin", "zzz"}) {
		t.Fatal("CLI arguments parsed incorrectly", o, err)
	}
	t.Setenv("MIYOHUB_PORT", "invalid")
	if o, err := parseCLI([]string{"serve", "--port", "5890"}, io.Discard); err != nil || o.port != 5890 {
		t.Fatal("explicit port did not override environment", err)
	}
}

func TestCLIInitNeverOverwritesAndShowIsReadOnlyRedacted(t *testing.T) {
	directory := t.TempDir()
	var output bytes.Buffer
	if err := execute(t.Context(), []string{"init", "--data-dir", directory, "--admin", "admin", "--password-stdin"}, strings.NewReader("private-password\n"), &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "state.json")
	s, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	u := s.ListUsers()[0]
	if err := s.AddAccountForUser(u.ID, model.Account{Name: "main", Cookie: "private-cookie", Stoken: "private-stoken", CloudTokens: map[string]string{"genshin": "private-cloud-token"}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := execute(t.Context(), []string{"init", "--data-dir", directory}, strings.NewReader(""), io.Discard, io.Discard); err == nil {
		t.Fatal("init overwrote existing state")
	}
	if err := execute(t.Context(), []string{"init", "--data-dir", directory, "--admin", "admin", "--password-stdin"}, strings.NewReader("replacement-password\n"), io.Discard, io.Discard); err == nil {
		t.Fatal("init reset administrator password")
	}
	for _, format := range []bool{false, true} {
		args := []string{"show", "--data-dir", directory}
		if format {
			args = append(args, "--json")
		}
		if err := execute(t.Context(), args, strings.NewReader(""), &output, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("show rewrote or migrated state")
	}
	for _, secret := range []string{"private-password", "private-cookie", "private-stoken", "private-cloud-token", "replacement-password"} {
		if strings.Contains(output.String(), secret) || bytes.Contains(after, []byte(secret)) {
			t.Fatal("summary or state exposed a secret")
		}
	}
	if len(s.ListUsers()) != 1 {
		t.Fatal("init created extra users")
	}
}

func TestCLIRejectsInUseOrMissingState(t *testing.T) {
	directory, _, _ := cliFixture(t)
	lock, err := processlock.Acquire(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := execute(t.Context(), []string{"run", "--data-dir", directory}, strings.NewReader(""), io.Discard, io.Discard); !errors.Is(err, processlock.ErrInUse) {
		t.Fatal("CLI bypassed live server lock", err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	for _, command := range []string{"show", "run", "login", "logs"} {
		if err := execute(t.Context(), []string{command, "--data-dir", missing}, strings.NewReader(""), io.Discard, io.Discard); err == nil {
			t.Fatal("command initialized missing state", command)
		}
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("missing-state check created a directory")
	}
}

func TestCLIShowUsesIndependentAccountPreferences(t *testing.T) {
	directory, s, admin := cliFixture(t)
	first := s.AccountsForUser(admin.ID, false)[0]
	p := *first.TaskSettings
	p.Automatic = false
	p.Features = model.Features{BBSTasks: true}
	p.BBS.Forums = []int{6}
	if _, err := s.UpdateAccountTasks(admin, first.ID, p); err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateUser("member", "private-password", "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(member.ID, model.Account{Name: "second", Cookie: "private-member-cookie"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUserPermissions(admin, member.ID, model.UserPermissions{Exchange: true}); err != nil {
		t.Fatal(err)
	}
	personal := s.CaptchaSettingsForUser(member.ID).UserCaptchaConfig
	personal.Source = "personal"
	personal.Channels = []model.CaptchaChannel{{Provider: "damagou", Enabled: true, Timeout: 60, UserKey: "private-summary-userkey"}}
	if err := s.UpdateUserCaptcha(member.ID, personal); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := execute(t.Context(), []string{"show", "--data-dir", directory, "--json"}, strings.NewReader(""), &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Accounts []accountSummary `json:"accounts"`
	}
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil || len(summary.Accounts) != 2 {
		t.Fatal("invalid account summary", err)
	}
	a, b := summary.Accounts[0], summary.Accounts[1]
	if a.TaskSettings == nil || a.TaskSettings.Automatic || a.TaskSettings.Features.GameCheckin || !a.TaskSettings.Features.BBSTasks || !reflect.DeepEqual(a.TaskSettings.BBS.Forums, []int{6}) || a.CaptchaSource != "site" || !a.ExchangeAllowed {
		t.Fatal("first account settings or access not reflected")
	}
	if b.TaskSettings == nil || !b.TaskSettings.Automatic || !b.TaskSettings.Features.GameCheckin || b.TaskSettings.Features.BBSTasks || b.CaptchaSource != "personal" || !b.ExchangeAllowed {
		t.Fatal("second account inherited first account preferences")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"features", "games", "cloud_games", "bbs"} {
		if _, ok := fields[key]; ok {
			t.Fatal("summary still advertised global task settings", key)
		}
	}
	if err := execute(t.Context(), []string{"show", "--data-dir", directory}, strings.NewReader(""), &output, io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "private-") || !strings.Contains(output.String(), "自动参与 false") || !strings.Contains(output.String(), "打码来源 personal") {
		t.Fatal("summary exposed credentials or omitted account preferences")
	}
}

func TestCLIAccountSelectionIsUnambiguousAndScoped(t *testing.T) {
	_, s, admin := cliFixture(t)
	member, err := s.CreateUser("member", "password123", "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(member.ID, model.Account{Name: "main", Cookie: "other-cookie"}); err != nil {
		t.Fatal(err)
	}
	if _, err := selectAccounts(s, "", "main"); err == nil {
		t.Fatal("ambiguous account name accepted")
	}
	ids, err := selectAccounts(s, "admin", "main")
	if err != nil || len(ids) != 1 {
		t.Fatal(err, ids)
	}
	a, _ := s.AccountForUser(admin.ID, false, ids[0])
	if a.Name != "main" {
		t.Fatal("selected wrong owner")
	}
	if _, err := selectAccounts(s, "member", a.ID); err == nil {
		t.Fatal("foreign account selected")
	}
	if _, err := selectUser(s, "", true); err == nil {
		t.Fatal("login guessed owner in multi-user state")
	}
	if _, err := selectUser(s, "nonexistent", false); err == nil {
		t.Fatal("missing user accepted")
	}
}

type cliTransport func(*http.Request) (*http.Response, error)

func (f cliTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCLIRunIsOneShotWithoutExchangeOrPushAndReportsFailure(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failure], func(t *testing.T) {
			directory, s, u := cliFixture(t)
			cfg := s.Config()
			cfg.Schedule.Enabled, cfg.Schedule.RunOnStart = true, true
			cfg.Push.Channels = []model.PushChannel{{Provider: "webhook", Enabled: true, Webhook: "https://push.invalid/private"}}
			if err := s.ReplaceConfig(cfg); err != nil {
				t.Fatal(err)
			}
			a := s.AccountsForUser(u.ID, false)[0]
			if _, err := s.CreateExchangePlan(u.ID, false, model.ExchangePlan{AccountID: a.ID, GoodsID: "test-good", Enabled: true, Auto: true, ExchangeAt: time.Now().Add(time.Minute).Unix()}); err != nil {
				t.Fatal(err)
			}
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			calls := 0
			http.DefaultTransport = cliTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.Path != mihoyo.AccountRolesPath || req.URL.Query().Get("game_biz") != "hk4e_cn" {
					t.Error("one-shot run reached an unselected task", req.URL.Path)
					return nil, errors.New("denied")
				}
				body := `{"retcode":0,"data":{"list":[]}}`
				if failure {
					body = `{"retcode":-100,"message":"expired"}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			var output bytes.Buffer
			err := execute(t.Context(), []string{"run", "--data-dir", directory, "--account", "main", "--games-only", "--game", "genshin", "--json"}, strings.NewReader(""), &output, io.Discard)
			if (err != nil) != failure {
				t.Fatal("wrong failure exit status", err)
			}
			if calls != 1 || strings.Contains(output.String(), "private-") {
				t.Fatal("unexpected request count or exposed secret", calls)
			}
			var report struct {
				OK       bool        `json:"ok"`
				Accounts []runResult `json:"accounts"`
			}
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.OK == failure || len(report.Accounts) != 1 || !report.Accounts[0].Updated {
				t.Fatal("incorrect JSON result")
			}
			after, err := store.OpenReadOnly(filepath.Join(directory, "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if after.Config().Shop.Plans[0].State != "pending" || !reflect.DeepEqual(cfg.Games, after.Config().Games) {
				t.Fatal("one-shot run changed plan or persisted temporary selection")
			}
		})
	}
}

type fakeCLIQR struct {
	owner, accountID string
	stopped          bool
	state            model.LoginState
	onStart          func()
}

func (q *fakeCLIQR) StartBinding(_ context.Context, owner, _ string, accountID string) (model.LoginState, error) {
	q.owner, q.accountID = owner, accountID
	if q.onStart != nil {
		q.onStart()
	}
	return model.LoginState{Running: true, QRURL: "https://example.invalid/temporary-qr"}, nil
}
func (q *fakeCLIQR) State(string) model.LoginState { return q.state }
func (q *fakeCLIQR) Stop()                         { q.stopped = true }

func TestCLIQRRebindCancellationAndTemporaryImageCleanup(t *testing.T) {
	directory, s, u := cliFixture(t)
	a := s.AccountsForUser(u.ID, false)[0]
	manager := &fakeCLIQR{state: model.LoginState{Status: "success"}}
	o := cliOptions{dataDir: directory, account: "main", timeout: 120}
	if err := loginAccountCLI(t.Context(), s, manager, o, io.Discard); err != nil {
		t.Fatal(err)
	}
	if manager.owner != u.ID || manager.accountID != a.ID || !manager.stopped {
		t.Fatal("CLI login did not preserve ownership or stop the session")
	}
	files, err := filepath.Glob(filepath.Join(directory, ".login-qr-*.png"))
	if err != nil || len(files) != 0 {
		t.Fatal("temporary QR image was not cleaned up")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	manager = &fakeCLIQR{state: model.LoginState{Running: true}, onStart: cancel}
	if err := loginAccountCLI(ctx, s, manager, o, io.Discard); !errors.Is(err, context.Canceled) || !manager.stopped {
		t.Fatal("CLI login ignored cancellation", err)
	}
}

func TestServeBindsBeforeRunOnStart(t *testing.T) {
	_, s, _ := cliFixture(t)
	cfg := s.Config()
	cfg.Schedule.RunOnStart = true
	if err := s.ReplaceConfig(cfg); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	o := cliOptions{host: "127.0.0.1", port: listener.Addr().(*net.TCPAddr).Port, webDir: t.TempDir()}
	if err := serve(t.Context(), s, o, io.Discard); err == nil {
		t.Fatal("serve accepted an occupied port")
	}
	if !s.Config().Accounts[0].LastTaskAt.IsZero() {
		t.Fatal("failed server startup ran tasks")
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
	"github.com/eleost04/miyohub/internal/processlock"
)

func TestCLILogsCanReadDuringServeWithoutWritingOrExposingCredentials(t *testing.T) {
	directory, s, owner := cliFixture(t)
	for _, row := range []struct{ component, message string }{
		{"bbs", "old entry"}, {"games", "unselected entry"}, {"bbs", "latest entry private-cookie private-stoken private-cloud-token"},
	} {
		if err := s.AddLogForUser(owner.ID, row.component, row.message); err != nil {
			t.Fatal(err)
		}
	}
	lock, err := processlock.Acquire(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	path := filepath.Join(directory, "state.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	keyBefore, err := os.ReadFile(path + ".key")
	if err != nil {
		t.Fatal(err)
	}
	for _, asJSON := range []bool{false, true} {
		args := []string{"logs", "--data-dir", directory, "--tail", "1", "--component", "bbs"}
		if asJSON {
			args = append(args, "--json")
		}
		var out bytes.Buffer
		if err := execute(t.Context(), args, strings.NewReader(""), &out, io.Discard); err != nil {
			t.Fatal("read-only logs could not coexist with writer lock", err)
		}
		if !strings.Contains(out.String(), "latest entry") || strings.Contains(out.String(), "old entry") || strings.Contains(out.String(), "unselected entry") || strings.Contains(out.String(), "private-") {
			t.Fatal("logs did not filter, tail or redact correctly")
		}
		if asJSON {
			var entries []model.LogEntry
			if json.Unmarshal(out.Bytes(), &entries) != nil || len(entries) != 1 || entries[0].Component != "bbs" {
				t.Fatal("invalid log JSON output")
			}
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("log inspection changed live state")
	}
	keyAfter, err := os.ReadFile(path + ".key")
	if err != nil || !bytes.Equal(keyBefore, keyAfter) {
		t.Fatal("log inspection changed encryption key")
	}
}

func TestCLIBBSInspectionOnlyQueriesStateOnce(t *testing.T) {
	directory, _, _ := cliFixture(t)
	path := filepath.Join(directory, "state.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := processlock.Acquire(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	calls := 0
	http.DefaultTransport = cliTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/apihub/wapi/getUserMissionsState" || r.URL.Query().Get("point_sn") != "myb" {
			t.Error("inspection attempted a task, renewal or other request", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"retcode":0,"data":{"already_received_points":0,"can_get_points":50,"private_field":"private-cookie","states":[{"mission_id":62,"mission_key":"bind_game_role_0","is_get_award":true,"private_field":"private-stoken"}]}}`))}, nil
	})
	var out bytes.Buffer
	if err := execute(t.Context(), []string{"logs", "--data-dir", directory, "--bbs-state", "--account", "main"}, strings.NewReader(""), &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(out.String(), "bind_game_role_0") || strings.Contains(out.String(), "private-cookie") || strings.Contains(out.String(), "private-stoken") {
		t.Fatal("inspection made additional requests or exposed private state")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("state inspection mutated the live account")
	}
}

func TestCLIBBSInspectionRejectsInactiveOwner(t *testing.T) {
	directory, s, _ := cliFixture(t)
	member, err := s.CreateUser("disabled-member", "private-password", "user")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(member.ID, model.Account{Name: "disabled-bbs", Cookie: "private-disabled-cookie"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateUserStatus(member.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	calls := 0
	http.DefaultTransport = cliTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"retcode":0,"data":{}}`))}, nil
	})
	if err := execute(t.Context(), []string{"logs", "--data-dir", directory, "--bbs-state", "--account", "disabled-bbs"}, strings.NewReader(""), io.Discard, io.Discard); err == nil || calls != 0 {
		t.Fatal("inspection queried credentials belonging to an inactive user")
	}
}

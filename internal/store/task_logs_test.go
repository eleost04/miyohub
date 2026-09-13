package store

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
)

func TestTaskLogsPersistRunMetadataAndPreserveOwnerIsolation(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	admin, _, _ := s.CreateAdmin("admin", "test-password")
	member, _ := s.CreateUser("member", "test-password", "user")
	for index, owner := range []model.User{admin, member} {
		if err := s.AddAccountForUser(owner.ID, model.Account{Name: "same display name", Stuid: strconv.Itoa(101 + index), Stoken: "PRIVATE_TASK_LOG_TOKEN"}); err != nil {
			t.Fatal(err)
		}
	}
	own := s.AccountsForUser(admin.ID, false)[0]
	other := s.AccountsForUser(member.ID, false)[0]
	if err := s.AddTaskLogForUser(admin.ID, other.ID, "wrong-owner", "bbs", "must not appear"); err == nil {
		t.Fatal("cross-owner task log was accepted")
	}
	if err := s.AddTaskLogForUser(admin.ID, own.ID, "", "bbs", "must not appear"); err == nil {
		t.Fatal("empty run identifier was accepted")
	}
	for _, row := range []struct{ user, account, run, message string }{
		{admin.ID, own.ID, "run-one", "own result PRIVATE_TASK_LOG_TOKEN"},
		{member.ID, other.ID, "run-one", "OTHER_OWNER_DETAIL"},
		{admin.ID, own.ID, "run-two", "second own result"},
	} {
		if err := s.AddTaskLogForUser(row.user, row.account, row.run, "bbs", row.message); err != nil {
			t.Fatal(err)
		}
	}
	read, err := OpenReadOnly(s.path)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range read.RedactedLogsForUser(admin.ID, true) {
		if strings.Contains(entry.Message, "OTHER_OWNER") || strings.Contains(entry.Message, "PRIVATE_TASK_LOG_TOKEN") || strings.Contains(entry.Message, "must not appear") {
			t.Fatal("task details crossed the privacy boundary")
		}
		if entry.RunID != "" {
			if entry.AccountID != own.ID {
				t.Fatal("wrong account metadata")
			}
			seen[entry.RunID] = true
		}
	}
	if len(seen) != 2 || !seen["run-one"] || !seen["run-two"] {
		t.Fatal("run metadata was not preserved")
	}
}

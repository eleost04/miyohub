package store

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
)

func TestAccountBatchAtomicOwnershipAndScheduling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	admin, _, _ := s.CreateAdmin("admin", "test-password")
	member, _ := s.CreateUser("member", "test-password", "user")
	for _, a := range []model.Account{{Name: "first", Stuid: "1001"}, {Name: "second", Stuid: "1002"}} {
		if err := s.AddAccountForUser(admin.ID, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddAccountForUser(member.ID, model.Account{Name: "private", Stuid: "1003"}); err != nil {
		t.Fatal(err)
	}
	owned := s.AccountsForUser(admin.ID, false)
	other := s.AccountsForUser(member.ID, false)[0]
	group := "日常组"
	before, _ := os.ReadFile(path)
	for _, ids := range [][]string{nil, {owned[0].ID, other.ID}, {owned[0].ID, "missing"}, {owned[0].ID, owned[0].ID}, make([]string, 51)} {
		if _, err := s.BatchUpdateAccounts(admin.ID, AccountBatchPatch{AccountIDs: ids, Group: &group}); err == nil {
			t.Fatal("invalid batch accepted")
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("rejected batch partially saved")
		}
	}
	ids := []string{owned[0].ID, owned[1].ID}
	result, err := s.BatchUpdateAccounts(admin.ID, AccountBatchPatch{AccountIDs: ids, Group: &group})
	if err != nil || len(result) != 2 || result[0].Group != group || !reflect.DeepEqual(result[0].TaskSettings, owned[0].TaskSettings) {
		t.Fatal("group changed work", err)
	}
	revisions := map[string]int{ids[0]: owned[0].TaskSettings.Revision, ids[1]: owned[1].TaskSettings.Revision}
	schedule := &model.AccountSchedule{Time: "08:35", Timezone: "Asia/Shanghai"}
	result, err = s.BatchUpdateAccounts(admin.ID, AccountBatchPatch{AccountIDs: ids, Schedule: schedule, Revisions: revisions})
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range result {
		if a.TaskSettings.Revision != revisions[a.ID]+1 || *a.TaskSettings.Schedule != *schedule || !a.TaskSettings.SameWork(owned[i].TaskSettings) {
			t.Fatal("batch lost individual tasks")
		}
	}
	if _, err := s.BatchUpdateAccounts(admin.ID, AccountBatchPatch{AccountIDs: ids, Schedule: schedule, Revisions: revisions}); err == nil {
		t.Fatal("stale schedule accepted")
	}
	for _, a := range result {
		revisions[a.ID] = a.TaskSettings.Revision
	}
	before, _ = os.ReadFile(path)
	if _, err := s.BatchUpdateAccounts(admin.ID, AccountBatchPatch{AccountIDs: ids, Schedule: schedule, Revisions: revisions}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("unchanged batch rewrote state")
	}
	if _, err := s.BatchUpdateAccounts(admin.ID, AccountBatchPatch{AccountIDs: ids, FollowDefault: true, Revisions: revisions}); err != nil {
		t.Fatal(err)
	}
	for _, a := range s.AccountsForUser(admin.ID, false) {
		if a.TaskSettings.Schedule != nil {
			t.Fatal("fallback not restored")
		}
	}
	if err := s.BindAccountForUser(admin.ID, ids[0], model.Account{Stuid: "1001", Name: "first"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.AccountForUser(admin.ID, false, ids[0]); got.Group != group {
		t.Fatal("rebind removed group")
	}
	if got, _ := s.AccountForUser(member.ID, false, other.ID); got.Group != "" {
		t.Fatal("other user changed")
	}
}

func TestAccountBatchValidation(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range []string{strings.Repeat("长", 33), "line\nbreak", "hidden\x00name"} {
		if _, err := s.BatchUpdateAccounts("any", AccountBatchPatch{AccountIDs: []string{"a"}, Group: &group}); err == nil {
			t.Fatal("bad group accepted")
		}
	}
	for _, schedule := range []model.AccountSchedule{{Time: "25:00", Timezone: "UTC"}, {Time: "9:00", Timezone: "UTC"}, {Time: "09:00", Timezone: "invalid"}} {
		if _, err := s.BatchUpdateAccounts("any", AccountBatchPatch{AccountIDs: []string{"a"}, Schedule: &schedule}); err == nil {
			t.Fatal("bad schedule accepted")
		}
	}
}

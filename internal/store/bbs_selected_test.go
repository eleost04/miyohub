package store

import (
	"testing"

	"github.com/eleost04/miyohub/internal/model"
)

func TestSelectedBBSModeIsOptInScopedAndPersistent(t *testing.T) {
	s, admin, alice, bob := preferenceFixture(t)
	a, b := s.AccountsForUser(alice.ID, false)[0], s.AccountsForUser(bob.ID, false)[0]
	if a.TaskSettings.BBS.RunAllSelected || b.TaskSettings.BBS.RunAllSelected {
		t.Fatal("existing users were opted in")
	}
	p := clone(*a.TaskSettings)
	p.BBS.RunAllSelected = true
	if p.SameWork(a.TaskSettings) {
		t.Fatal("mode change did not invalidate in-flight task configuration")
	}
	for _, actor := range []model.User{admin, bob} {
		if _, err := s.UpdateAccountTasks(actor, a.ID, p); err == nil {
			t.Fatal("another user changed the mode")
		}
	}
	if _, err := s.UpdateAccountTasks(alice, a.ID, p); err != nil {
		t.Fatal(err)
	}
	read, err := OpenReadOnly(s.path)
	if err != nil {
		t.Fatal(err)
	}
	acfg, _ := read.ConfigForAccount(a.ID)
	bcfg, _ := read.ConfigForAccount(b.ID)
	if !acfg.BBS.RunAllSelected || bcfg.BBS.RunAllSelected {
		t.Fatal("mode was lost or shared across accounts")
	}
}

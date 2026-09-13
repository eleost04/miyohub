package store

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
)

func TestAccountArchiveOwnershipAndDisabledImport(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner, _, _ := s.CreateAdmin("owner", "test-password")
	other, _ := s.CreateUser("other", "test-password", "user")
	device := model.Device{ID: "fixture-device", FP: "fixture-fp", Name: "MiyoHub", Model: "MiyoHub"}
	if err := s.AddAccountForUser(owner.ID, model.Account{Name: "portable", Group: "group", Stuid: "40101", Cookie: "stuid=40101;cookie_token=synthetic-value", Device: device}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddAccountForUser(other.ID, model.Account{Name: "other-only", Stuid: "40102"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExportAccountArchive(owner.ID, "wrong"); err == nil {
		t.Fatal("password bypass")
	}
	archive, err := s.ExportAccountArchive(owner.ID, "test-password")
	if err != nil || len(archive.Accounts) != 1 || archive.Accounts[0].Name != "portable" {
		t.Fatal("export crossed ownership", err)
	}
	before := s.Config()
	result, err := s.ImportAccountArchive(other.ID, archive)
	if err != nil || result.Add != 0 || result.Skipped != 1 || len(result.Names) != 0 || !reflect.DeepEqual(before, s.Config()) {
		t.Fatal("conflicting ownership overwritten", err)
	}
	dest, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, _ = dest.CreateAdmin("admin", "test-password")
	user, _ := dest.CreateUser("receiver", "test-password", "user")
	preview, err := dest.PreviewAccountArchive(user.ID, archive)
	if err != nil || preview.Add != 1 || len(dest.Config().Accounts) != 0 {
		t.Fatal("preview mutated state", err)
	}
	result, err = dest.ImportAccountArchive(user.ID, archive)
	if err != nil || result.Add != 1 {
		t.Fatal("import failed", err)
	}
	got := dest.AccountsForUser(user.ID, false)[0]
	original := s.AccountsForUser(owner.ID, false)[0]
	if got.ID == original.ID || got.UserID != user.ID || !got.Disabled || got.TaskSettings.Automatic || got.TaskSettings.Revision != 1 || !got.LastAutomaticAt.IsZero() || got.TaskResults != nil || got.ExchangeAllowed {
		t.Fatal("unsafe imported state")
	}
	if got.Group != original.Group || got.Cookie != original.Cookie || got.Device != device || !got.TaskSettings.SameWork(original.TaskSettings) {
		t.Fatal("portable fields lost")
	}
	result, err = dest.ImportAccountArchive(user.ID, archive)
	if err != nil || result.Add != 0 || result.Skipped != 1 {
		t.Fatal("duplicate import not idempotent", err)
	}
}

func TestAccountArchiveValidationIsAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	u, _, _ := s.CreateAdmin("owner", "test-password")
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "first", Stuid: "40201"})
	a, err := s.ExportAccountArchive(u.ID, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	second := clone(a.Accounts[0])
	second.Name = "new"
	second.UID = "40202"
	a.Accounts = []model.PortableAccount{second}
	before, _ := os.ReadFile(path)
	for _, mutate := range []func(*model.AccountArchive){
		func(a *model.AccountArchive) { a.Version = 99 },
		func(a *model.AccountArchive) { a.Accounts = nil },
		func(a *model.AccountArchive) { a.Accounts = append(a.Accounts, a.Accounts[0]) },
		func(a *model.AccountArchive) { a.Accounts[0].Cookie = "stuid=99999" },
		func(a *model.AccountArchive) { a.Accounts[0].Device.FP = "bad\nheader" },
		func(a *model.AccountArchive) {
			a.Accounts[0].TaskSettings.Schedule = &model.AccountSchedule{Time: "25:99", Timezone: "UTC"}
		},
		func(a *model.AccountArchive) {
			invalid := clone(a.Accounts[0])
			invalid.Name = "bad"
			invalid.UID = "no"
			a.Accounts = append(a.Accounts, invalid)
		},
	} {
		bad := clone(a)
		mutate(&bad)
		if _, err := s.ImportAccountArchive(u.ID, bad); err == nil {
			t.Fatal("bad archive accepted")
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("invalid import partially persisted")
		}
	}
	// Ownership must be rechecked at apply, not trusted from a prior preview.
	if _, err := s.PreviewAccountArchive(u.ID, a); err != nil {
		t.Fatal(err)
	}
	_ = s.AddAccountForUser(u.ID, model.Account{Name: "new", Stuid: "40203"})
	if got, err := s.ImportAccountArchive(u.ID, a); err != nil || got.Add != 0 || got.Skipped != 1 {
		t.Fatal("conflict after preview ignored", err)
	}
}

package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestCaptchaProbeRecoveryKeepsCooldownWithoutReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	u, _, _ := s.CreateAdmin("admin", "test-password")
	first, created, err := s.BeginCaptchaProbe(u.ID)
	if err != nil || !created {
		t.Fatal(err)
	}
	again, created, err := s.BeginCaptchaProbe(u.ID)
	if err != nil || created || first.ID != again.ID {
		t.Fatal("duplicate job")
	}
	restored, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.RecoverCaptchaProbes(); err != nil {
		t.Fatal(err)
	}
	if restored.CaptchaProbeForUser(u.ID).Status != "interrupted" {
		t.Fatal("job replayed after restart")
	}
	if _, _, err := restored.BeginCaptchaProbe(u.ID); !errors.Is(err, ErrCaptchaProbeCooldown) {
		t.Fatal("restart bypassed cooldown", err)
	}
	if err := restored.FinishCaptchaProbe(u.ID, first.ID, "succeeded", "late completion"); err != nil {
		t.Fatal(err)
	}
	if restored.CaptchaProbeForUser(u.ID).Status != "interrupted" {
		t.Fatal("stale worker replaced terminal result")
	}
}

package processlock

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLockProcessHelper(t *testing.T) {
	directory := os.Getenv("MIYOHUB_LOCK_TEST_DIR")
	if directory == "" {
		return
	}
	lock, err := Acquire(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	fmt.Println("locked")
	time.Sleep(10 * time.Second)
}

func TestExclusiveLockReleasedAfterProcessCrash(t *testing.T) {
	directory := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestLockProcessHelper$")
	cmd.Env = append(os.Environ(), "MIYOHUB_LOCK_TEST_DIR="+directory)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "locked" {
		t.Fatal("child did not acquire the lock")
	}
	if lock, err := Acquire(directory); !errors.Is(err, ErrInUse) {
		if lock != nil {
			lock.Close()
		}
		t.Fatal("a second process acquired the same store", err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(directory, alias); err == nil {
		if lock, err := Acquire(alias); !errors.Is(err, ErrInUse) {
			if lock != nil {
				lock.Close()
			}
			t.Fatal("a symlink bypassed the directory lock", err)
		}
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	lock, err := Acquire(directory)
	if err != nil {
		t.Fatal("crashed process left an unrecoverable lock", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err = Acquire(directory)
	if err != nil {
		t.Fatal("closed lock could not be acquired", err)
	}
	defer lock.Close()
}

func TestLockRejectsNonRegularFiles(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, ".miyohub.lock"), 0700); err != nil {
		t.Fatal(err)
	}
	if lock, err := Acquire(directory); err == nil {
		lock.Close()
		t.Fatal("directory used as a lock file")
	}
}

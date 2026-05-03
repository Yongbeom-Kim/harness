package dirlock

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAcquireCreatesPIDFileAndReleaseRemovesLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), DirName)

	lock := New(lockPath)
	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	pidBytes, err := os.ReadFile(filepath.Join(lockPath, pidFileName))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := strings.TrimSpace(string(pidBytes)), strconv.Itoa(os.Getpid()); got != want {
		t.Fatalf("pid file = %q, want %q", got, want)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock path still exists, stat err = %v", err)
	}
}

func TestAcquireRemovesStaleLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), DirName)
	if err := os.Mkdir(lockPath, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(lockPath, pidFileName), []byte("999999\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	lock := New(lockPath)
	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	t.Cleanup(func() {
		_ = lock.Release()
	})

	pidBytes, err := os.ReadFile(filepath.Join(lockPath, pidFileName))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := strings.TrimSpace(string(pidBytes)), strconv.Itoa(os.Getpid()); got != want {
		t.Fatalf("pid file = %q, want %q", got, want)
	}
}

func TestAcquireFailsWhenLockHeldByRunningPID(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), DirName)
	if err := os.Mkdir(lockPath, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(lockPath, pidFileName), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	lock := New(lockPath)
	err := lock.Acquire()
	if err == nil {
		t.Fatal("Acquire() error = nil, want lock-held error")
	}
	var heldErr LockHeldError
	if !errors.As(err, &heldErr) {
		t.Fatalf("Acquire() error = %T, want LockHeldError", err)
	}
	if heldErr.PID != os.Getpid() {
		t.Fatalf("LockHeldError PID = %d, want %d", heldErr.PID, os.Getpid())
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("Acquire() error = %v, want lock-held message", err)
	}
}

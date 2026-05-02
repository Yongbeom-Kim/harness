package dirlock

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	DirName     = ".harness_lock"
	pidFileName = "pid"
)

type Locker interface {
	Acquire() error
	Release() error
}

type LockHeldError struct {
	PID int
}

func (e LockHeldError) Error() string {
	return fmt.Sprintf("another harness command is already running (pid %d)", e.PID)
}

type Lock struct {
	path     string
	pidFile  string
	acquired bool
}

func NewInCurrentDirectory() (*Lock, error) {
	workingDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	return New(filepath.Join(workingDir, DirName)), nil
}

func New(path string) *Lock {
	return &Lock{
		path:    path,
		pidFile: filepath.Join(path, pidFileName),
	}
}

func (l *Lock) Acquire() error {
	for {
		if err := os.Mkdir(l.path, 0o755); err == nil {
			if writeErr := os.WriteFile(l.pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); writeErr != nil {
				_ = os.RemoveAll(l.path)
				return fmt.Errorf("write lock pid file: %w", writeErr)
			}
			l.acquired = true
			return nil
		} else if !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("create lock directory: %w", err)
		}

		stale, stalePID, err := removeIfStale(l)
		if err != nil {
			return err
		}
		if !stale {
			return LockHeldError{PID: stalePID}
		}
	}
}

func (l *Lock) Release() error {
	if l == nil || !l.acquired {
		return nil
	}
	l.acquired = false

	if err := os.RemoveAll(l.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove lock directory: %w", err)
	}
	return nil
}

func removeIfStale(lock *Lock) (bool, int, error) {
	pid, err := readPID(lock.pidFile)
	if err != nil {
		return false, 0, err
	}

	running, err := processRunning(pid)
	if err != nil {
		return false, 0, err
	}
	if running {
		return false, pid, nil
	}

	if err := os.RemoveAll(lock.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, 0, fmt.Errorf("remove stale lock for pid %d: %w", pid, err)
	}
	return true, pid, nil
}

func readPID(path string) (int, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, fmt.Errorf("lock directory exists without pid file: %s", path)
		}
		return 0, fmt.Errorf("read lock pid file: %w", err)
	}

	pidText := strings.TrimSpace(string(content))
	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("lock pid file is invalid: %s", path)
	}
	return pid, nil
}

func processRunning(pid int) (bool, error) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false, fmt.Errorf("find process %d: %w", pid, err)
	}
	err = process.Signal(syscall.Signal(0))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, syscall.EPERM):
		return true, nil
	case errors.Is(err, os.ErrProcessDone):
		return false, nil
	case errors.Is(err, syscall.ESRCH):
		return false, nil
	default:
		return false, fmt.Errorf("check process %d: %w", pid, err)
	}
}

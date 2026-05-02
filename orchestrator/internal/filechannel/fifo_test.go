package filechannel

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerReadsOneMessagePerOpenWriteCloseCycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "to_reviewer.pipe")

	manager, err := NewFIFOManager(FIFOConfig{Path: path})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	defer func() {
		if stopErr := manager.Stop(); stopErr != nil {
			t.Fatalf("Stop returned error: %v", stopErr)
		}
		if removeErr := manager.Remove(); removeErr != nil {
			t.Fatalf("Remove returned error: %v", removeErr)
		}
	}()

	writeFIFO(t, path, "first message")
	writeFIFO(t, path, "second message")

	got1 := awaitMessage(t, manager.Messages())
	got2 := awaitMessage(t, manager.Messages())
	if got1.Path != path || got1.Body != "first message" {
		t.Fatalf("unexpected first message: %#v", got1)
	}
	if got2.Path != path || got2.Body != "second message" {
		t.Fatalf("unexpected second message: %#v", got2)
	}
	if got2.ReceivedAt.Before(got1.ReceivedAt) {
		t.Fatalf("message timestamps should be monotonic: %#v %#v", got1, got2)
	}
}

func TestManagerStopAndRemoveCleanUpPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "to_reviewer.pipe")

	manager, err := NewFIFOManager(FIFOConfig{Path: path})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	if err := manager.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if err := manager.Remove(); err != nil {
		t.Fatalf("Remove returned error: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected FIFO removal, got err=%v", err)
	}
}

func TestManagerRecreatesStaleNonFIFOPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "to_reviewer.pipe")

	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale file: %v", err)
	}

	manager, err := NewFIFOManager(FIFOConfig{Path: path})
	if err != nil {
		t.Fatalf("NewManager returned error: %v", err)
	}
	defer func() {
		if stopErr := manager.Stop(); stopErr != nil {
			t.Fatalf("Stop returned error: %v", stopErr)
		}
		if removeErr := manager.Remove(); removeErr != nil {
			t.Fatalf("Remove returned error: %v", removeErr)
		}
	}()

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("expected FIFO after replace, got mode %v", info.Mode())
	}
}

func writeFIFO(t *testing.T, path string, body string) {
	t.Helper()

	done := make(chan error, 1)
	go func() {
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			done <- err
			return
		}
		_, writeErr := file.WriteString(body)
		done <- errors.Join(writeErr, file.Close())
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("write FIFO %s: %v", path, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out writing FIFO %s", path)
	}
}

func awaitMessage(t *testing.T, ch <-chan Message) Message {
	t.Helper()

	select {
	case msg := <-ch:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for FIFO message")
		return Message{}
	}
}

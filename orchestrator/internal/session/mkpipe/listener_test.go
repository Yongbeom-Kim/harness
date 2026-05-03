package mkpipe

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestStartResolvesDefaultAndExplicitPaths(t *testing.T) {
	dir := t.TempDir()
	absolute := filepath.Join(t.TempDir(), "absolute.pipe")

	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{
			name: "default",
			cfg:  Config{WorkingDir: dir, SessionName: "reviewer 1", DefaultBasename: "codex"},
			want: filepath.Join(dir, ".reviewer-1.mkpipe"),
		},
		{
			name: "relative",
			cfg:  Config{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex", RequestedPath: "./custom.pipe"},
			want: filepath.Join(dir, "custom.pipe"),
		},
		{
			name: "absolute",
			cfg:  Config{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex", RequestedPath: absolute},
			want: absolute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener, err := Start(tt.cfg)
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			defer listener.Close()
			if got := listener.Path(); got != tt.want {
				t.Fatalf("Path() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizeSessionBasenameFallsBackToDefault(t *testing.T) {
	if got := sanitizeSessionBasename("///", "codex"); got != "codex" {
		t.Fatalf("sanitizeSessionBasename fallback = %q, want %q", got, "codex")
	}
}

func TestStartRejectsMissingParentAndExistingTarget(t *testing.T) {
	dir := t.TempDir()
	existingFile := filepath.Join(dir, "already-there.pipe")
	if err := os.WriteFile(existingFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	existingFIFO := filepath.Join(dir, "stale.pipe")
	if err := syscall.Mkfifo(existingFIFO, 0o600); err != nil {
		t.Fatalf("Mkfifo: %v", err)
	}

	tests := []Config{
		{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex", RequestedPath: filepath.Join(dir, "missing", "child.pipe")},
		{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex", RequestedPath: existingFile},
		{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex", RequestedPath: existingFIFO},
	}

	for _, cfg := range tests {
		if _, err := Start(cfg); err == nil {
			t.Fatalf("expected Start(%+v) to fail", cfg)
		}
	}
}

func TestListenerNormalizesMessagesPreservesInternalNewlinesAndSuppressesWhitespace(t *testing.T) {
	dir := t.TempDir()
	listener, err := Start(Config{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer listener.Close()

	go writeFIFO(t, listener.Path(), "line one\nline two\n\n")
	select {
	case got := <-listener.Messages():
		if got != "line one\nline two\n" {
			t.Fatalf("message = %q, want %q", got, "line one\\nline two\\n")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}

	go writeFIFO(t, listener.Path(), " \t\n")
	select {
	case got := <-listener.Messages():
		t.Fatalf("unexpected whitespace-only message %q", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestCloseUnblocksReaderRemovesFIFOAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	listener, err := Start(Config{WorkingDir: dir, SessionName: "codex", DefaultBasename: "codex"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if err := listener.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := os.Stat(listener.Path()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected fifo removal, stat err = %v", err)
	}
	select {
	case _, ok := <-listener.Messages():
		if ok {
			t.Fatal("expected Messages channel to be closed")
		}
	default:
		t.Fatal("expected Messages channel to be closed after Close")
	}
	select {
	case _, ok := <-listener.Errors():
		if ok {
			t.Fatal("expected Errors channel to be closed")
		}
	default:
		t.Fatal("expected Errors channel to be closed after Close")
	}
}

func writeFIFO(t *testing.T, path, payload string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Errorf("OpenFile(%q): %v", path, err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(payload); err != nil {
		t.Errorf("WriteString: %v", err)
	}
}

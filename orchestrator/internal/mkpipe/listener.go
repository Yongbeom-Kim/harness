package mkpipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

type Config struct {
	WorkingDir      string
	SessionName     string
	DefaultBasename string
	RequestedPath   string
}

type Listener interface {
	Path() string
	Messages() <-chan string
	Errors() <-chan error
	Close() error
}

type listener struct {
	path     string
	messages chan string
	errors   chan error
	closing  chan struct{}
	done     chan struct{}

	closeOnce sync.Once
	closeErr  error
}

func Start(cfg Config) (Listener, error) {
	path, err := resolvePath(cfg)
	if err != nil {
		return nil, err
	}
	if err := validateTarget(path); err != nil {
		return nil, err
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		return nil, fmt.Errorf("create mkpipe fifo %q: %w", path, err)
	}

	l := &listener{
		path:     path,
		messages: make(chan string, 8),
		errors:   make(chan error, 8),
		closing:  make(chan struct{}),
		done:     make(chan struct{}),
	}
	go l.readLoop()
	return l, nil
}

func (l *listener) Path() string {
	return l.path
}

func (l *listener) Messages() <-chan string {
	return l.messages
}

func (l *listener) Errors() <-chan error {
	return l.errors
}

func (l *listener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closing)
		<-l.done
		if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
			l.closeErr = err
		}
	})
	return l.closeErr
}

func (l *listener) readLoop() {
	defer close(l.done)
	defer close(l.messages)
	defer close(l.errors)

	fd, err := syscall.Open(l.path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		if !l.isClosing() {
			l.sendError(fmt.Errorf("open mkpipe fifo %q: %w", l.path, err))
		}
		return
	}
	defer syscall.Close(fd)

	var pending []byte
	buf := make([]byte, 4096)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-l.closing:
			return
		default:
		}

		n, err := syscall.Read(fd, buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			continue
		}

		if err == nil {
			if len(pending) > 0 {
				message, ok := normalizeMessage(string(pending))
				pending = nil
				if ok {
					select {
					case l.messages <- message:
					case <-l.closing:
						return
					}
				}
			}
			l.wait(ticker)
			continue
		}
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK || err == syscall.EINTR {
			l.wait(ticker)
			continue
		}
		if l.isClosing() {
			return
		}
		l.sendError(fmt.Errorf("read mkpipe fifo %q: %w", l.path, err))
		return
	}
}

func (l *listener) sendError(err error) {
	select {
	case l.errors <- err:
	case <-l.closing:
	}
}

func (l *listener) isClosing() bool {
	select {
	case <-l.closing:
		return true
	default:
		return false
	}
}

func (l *listener) wait(ticker *time.Ticker) {
	select {
	case <-l.closing:
	case <-ticker.C:
	}
}

func resolvePath(cfg Config) (string, error) {
	workingDir := cfg.WorkingDir
	if workingDir == "" {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	workingDir, err := filepath.Abs(workingDir)
	if err != nil {
		return "", err
	}

	if cfg.RequestedPath != "" {
		if filepath.IsAbs(cfg.RequestedPath) {
			return filepath.Clean(cfg.RequestedPath), nil
		}
		return filepath.Abs(filepath.Join(workingDir, cfg.RequestedPath))
	}

	basename := sanitizeSessionBasename(cfg.SessionName, cfg.DefaultBasename)
	return filepath.Join(workingDir, "."+basename+".mkpipe"), nil
}

func validateTarget(path string) error {
	parent := filepath.Dir(path)
	info, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("mkpipe parent %q is not available: %w", parent, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("mkpipe parent %q is not a directory", parent)
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("mkpipe target %q already exists", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect mkpipe target %q: %w", path, err)
	}
	return nil
}

func normalizeMessage(raw string) (string, bool) {
	message := raw
	switch {
	case strings.HasSuffix(message, "\r\n"):
		message = strings.TrimSuffix(message, "\r\n")
	case strings.HasSuffix(message, "\n"):
		message = strings.TrimSuffix(message, "\n")
	case strings.HasSuffix(message, "\r"):
		message = strings.TrimSuffix(message, "\r")
	}
	if strings.TrimSpace(message) == "" {
		return "", false
	}
	return message, true
}

func sanitizeSessionBasename(sessionName, fallback string) string {
	var builder strings.Builder
	lastDash := false
	for _, r := range sessionName {
		allowed := r == '.' || r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
		if allowed && r < unicode.MaxASCII {
			builder.WriteRune(r)
			lastDash = r == '-'
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}

	result := strings.Trim(builder.String(), "-")
	if result != "" {
		return result
	}
	fallback = strings.Trim(sanitizeFallback(fallback), "-")
	if fallback != "" {
		return fallback
	}
	return "session"
}

func sanitizeFallback(fallback string) string {
	var builder strings.Builder
	lastDash := false
	for _, r := range fallback {
		allowed := r == '.' || r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
		if allowed && r < unicode.MaxASCII {
			builder.WriteRune(r)
			lastDash = r == '-'
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return builder.String()
}

package filechannel

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

type FIFOConfig struct {
	Path string
}

type fifoManager struct {
	path     string
	messages chan Message
	errors   chan error
	stopCh   chan struct{}

	stopOnce   sync.Once
	removeOnce sync.Once
	wg         sync.WaitGroup
}

func NewFIFOManager(cfg FIFOConfig) (ChannelManager, error) {
	if cfg.Path == "" {
		return nil, errors.New("file channel path must not be empty")
	}

	if err := prepareFIFO(cfg.Path); err != nil {
		return nil, err
	}

	m := &fifoManager{
		path:     cfg.Path,
		messages: make(chan Message, 1),
		errors:   make(chan error, 1),
		stopCh:   make(chan struct{}),
	}

	m.wg.Add(1)
	go m.readLoop()

	return m, nil
}

func (m *fifoManager) Messages() <-chan Message {
	return m.messages
}

func (m *fifoManager) Errors() <-chan error {
	return m.errors
}

func (m *fifoManager) Stop() error {
	m.stopOnce.Do(func() {
		close(m.stopCh)
		unblockReader(m.path)
		m.wg.Wait()
		close(m.messages)
		close(m.errors)
	})
	return nil
}

func (m *fifoManager) Remove() error {
	var removeErr error
	m.removeOnce.Do(func() {
		removeErr = removePath(m.path)
	})
	return removeErr
}

func (m *fifoManager) readLoop() {
	defer m.wg.Done()

	for {
		if m.isStopping() {
			return
		}

		body, err := readMessage(m.path)
		if err != nil {
			if m.isStopping() {
				return
			}
			m.emitError(&ReaderError{Path: m.path, Err: err})
			return
		}

		if m.isStopping() && body == "" {
			return
		}

		if !m.emitMessage(Message{
			Path:       m.path,
			Body:       body,
			ReceivedAt: time.Now().UTC(),
		}) {
			return
		}
	}
}

func (m *fifoManager) emitMessage(msg Message) bool {
	select {
	case <-m.stopCh:
		return false
	case m.messages <- msg:
		return true
	}
}

func (m *fifoManager) emitError(err error) {
	select {
	case <-m.stopCh:
	case m.errors <- err:
	}
}

func (m *fifoManager) isStopping() bool {
	select {
	case <-m.stopCh:
		return true
	default:
		return false
	}
}

func prepareFIFO(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale file channel %s: %w", path, err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		return fmt.Errorf("create file channel %s: %w", path, err)
	}
	return nil
}

func readMessage(path string) (string, error) {
	file, err := os.OpenFile(path, os.O_RDONLY, os.ModeNamedPipe)
	if err != nil {
		return "", err
	}
	defer file.Close()

	body, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func unblockReader(path string) {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return
	}
	_ = syscall.Close(fd)
}

func removePath(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove file channel %s: %w", path, err)
	}
	return nil
}

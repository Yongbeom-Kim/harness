package filechannel

import (
	"fmt"
	"time"
)

type Message struct {
	Path       string
	Body       string
	ReceivedAt time.Time
}

type ChannelManager interface {
	Messages() <-chan Message
	Errors() <-chan error
	Stop() error
	Remove() error
}

type ReaderError struct {
	Path string
	Err  error
}

func (e *ReaderError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("file channel reader %s failed: %v", e.Path, e.Err)
}

func (e *ReaderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

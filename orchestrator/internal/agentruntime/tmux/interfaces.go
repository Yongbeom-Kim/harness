// Tmux session and pane contracts; concrete [TmuxSession] / [TmuxPane] live in tmux.go.
package tmux

import "io"

type TmuxSessionLike interface {
	Name() string
	Attach(stdin io.Reader, stdout io.Writer, stderr io.Writer) error
	Close() error
	NewPane() (TmuxPaneLike, error)
}

type TmuxPaneLike interface {
	SendText(text string) error
	Capture() (string, error)
	Close() error
}

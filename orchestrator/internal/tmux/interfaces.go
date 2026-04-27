// Tmux session and pane contracts; concrete [TmuxSession] / [TmuxPane] live in tmux.go.
package tmux

import "io"

// TmuxSessionLike is the owning tmux session: identity, how operators attach, teardown, and
// a handle to the default [TmuxPaneLike] (single-pane model for this harness).
type TmuxSessionLike interface {
	Name() string
	AttachTarget() string
	Attach(stdin io.Reader, stdout io.Writer, stderr io.Writer) error
	Close() error
	NewPane() (TmuxPaneLike, error)
}

// TmuxPaneLike is pane-local I/O: send to the shell, read scrollback, and expose the -t target string.
type TmuxPaneLike interface {
	SendText(text string) error
	Capture() (string, error)
	Target() string
}

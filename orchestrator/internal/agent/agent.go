package agent

type Agent interface {
	Start() error
	WaitUntilReady() error
	SendPrompt(prompt string) error
	SessionName() string
	Close() error
}

var (
	_ Agent = (*CodexAgent)(nil)
	_ Agent = (*ClaudeAgent)(nil)
)

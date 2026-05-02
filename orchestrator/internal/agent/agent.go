package agent

type Agent interface {
	Start() error
	WaitUntilReady() error
	SessionName() string
	Close() error
}

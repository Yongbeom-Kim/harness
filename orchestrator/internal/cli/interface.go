package cli

type CliToolSendMessageOptions struct {
	Message          string
	CreateNewSession bool
}

type CliTool interface {
	SendMessage(options CliToolSendMessageOptions) (stdout string, stderr string, err error)
}

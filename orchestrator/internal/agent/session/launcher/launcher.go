package launcher

import "strings"

type Builder interface {
	Build(command string, args ...string) string
}

type SourcedShellBuilder struct{}

func NewSourcedShellBuilder() Builder {
	return SourcedShellBuilder{}
}

func (SourcedShellBuilder) Build(command string, args ...string) string {
	quotedCommand := make([]string, 0, 1+len(args))
	quotedCommand = append(quotedCommand, shellQuote(command))
	for _, arg := range args {
		quotedCommand = append(quotedCommand, shellQuote(arg))
	}
	script := `if [ -f "$HOME/.agentrc" ]; then . "$HOME/.agentrc"; fi; stty -echo; ` + strings.Join(quotedCommand, " ")
	return "bash -lc " + shellQuote(script)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

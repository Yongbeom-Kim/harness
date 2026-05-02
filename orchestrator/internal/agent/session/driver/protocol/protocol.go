package protocol

import (
	"fmt"
	"strings"
	"time"
)

type PreparedTurn struct {
	Prompt     string
	PromptBody string
	Marker     string
}

type Protocol interface {
	DecorateStartupPrompt(rolePrompt string, startupInstruction string) string
	PrepareTurn(prompt string) PreparedTurn
	ExtractTurnCapture(capture string, turn PreparedTurn) string
	IsTurnComplete(capture string, turn PreparedTurn) bool
	SanitizeTurnCapture(capture string, turn PreparedTurn) string
}

type PromiseDoneProtocol struct{}

const completionInstruction = "Finish your response with exactly <promise>done</promise>."

func NewPromiseDoneProtocol() Protocol {
	return PromiseDoneProtocol{}
}

func (PromiseDoneProtocol) DecorateStartupPrompt(rolePrompt string, startupInstruction string) string {
	return decorateTurnPrompt(strings.TrimSpace(rolePrompt + "\n\n" + startupInstruction))
}

func (PromiseDoneProtocol) PrepareTurn(prompt string) PreparedTurn {
	marker := fmt.Sprintf("<iwr:%x>", time.Now().UnixNano())
	promptBody := decorateTurnPrompt(prompt)
	return PreparedTurn{
		Prompt:     prependTurnMarker(promptBody, marker),
		PromptBody: promptBody,
		Marker:     marker,
	}
}

func (PromiseDoneProtocol) ExtractTurnCapture(capture string, turn PreparedTurn) string {
	if capture == "" {
		return ""
	}
	if turn.Marker == "" {
		return capture
	}
	index := strings.Index(capture, turn.Marker)
	if index < 0 {
		return ""
	}
	return stripTurnMarker(capture[index:], turn.Marker)
}

func (PromiseDoneProtocol) IsTurnComplete(capture string, turn PreparedTurn) bool {
	return lastNonEmptyLine(responseCapture(capture, turn.PromptBody)) == "<promise>done</promise>"
}

func (PromiseDoneProtocol) SanitizeTurnCapture(capture string, turn PreparedTurn) string {
	trimmed := strings.TrimLeft(responseCapture(capture, turn.PromptBody), "\r\n")
	trimmed = strings.TrimRight(trimmed, "\r\n")
	if trimmed == "" {
		return ""
	}
	return trimmed + "\n"
}

func decorateTurnPrompt(prompt string) string {
	base := strings.TrimRight(prompt, "\n")
	if base == "" {
		return completionInstruction
	}
	return base + "\n\n" + completionInstruction
}

func prependTurnMarker(prompt string, marker string) string {
	if marker == "" {
		return prompt
	}
	return marker + "\n" + prompt
}

func stripTurnMarker(capture string, marker string) string {
	if marker == "" {
		return capture
	}
	if strings.HasPrefix(capture, marker) {
		return capture[skipLineBreaks(capture, len(marker)):]
	}
	return capture
}

func responseCapture(capture string, promptBody string) string {
	if promptBody == "" {
		return capture
	}
	if strings.HasPrefix(capture, promptBody) {
		return capture[skipLineBreaks(capture, len(promptBody)):]
	}
	return capture
}

func skipLineBreaks(text string, index int) int {
	for index < len(text) && (text[index] == '\n' || text[index] == '\r') {
		index++
	}
	return index
}

func lastNonEmptyLine(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}

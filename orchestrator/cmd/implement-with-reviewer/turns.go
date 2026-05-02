package main

import (
	"fmt"
	"strings"
	"time"

	agentpkg "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent"
)

const completionInstruction = "Finish your response with exactly <promise>done</promise>."

type turnResult struct {
	Output     string
	RawCapture string
}

type preparedTurn struct {
	Prompt     string
	PromptBody string
	Marker     string
}

func newTurnMarker() string {
	return fmt.Sprintf("<iwr:%x>", time.Now().UnixNano())
}

func prepareTurn(roleContract string, body string) preparedTurn {
	marker := newTurnMarker()
	promptBody := decorateTurnBody(roleContract, body)
	return preparedTurn{
		Prompt:     decorateTurnPrompt(roleContract, body, marker),
		PromptBody: promptBody,
		Marker:     marker,
	}
}

func decorateTurnPrompt(roleContract string, body string, marker string) string {
	promptBody := decorateTurnBody(roleContract, body)
	if marker == "" {
		return promptBody
	}
	return marker + "\n" + promptBody
}

func decorateTurnBody(roleContract string, body string) string {
	parts := make([]string, 0, 3)
	if strings.TrimSpace(roleContract) != "" {
		parts = append(parts, strings.TrimSpace(roleContract))
	}
	if strings.TrimSpace(body) != "" {
		parts = append(parts, strings.TrimRight(body, "\n"))
	}
	parts = append(parts, completionInstruction)
	return strings.Join(parts, "\n\n")
}

func runAgentTurn(agent workflowAgent, role string, body string, idleTimeout time.Duration, pollInterval time.Duration) (turnResult, error) {
	prepared := prepareTurn(buildRoleContract(role), body)
	if err := agent.SendPrompt(prepared.Prompt); err != nil {
		return turnResult{}, err
	}
	rawCapture, err := waitForDone(agent, prepared, idleTimeout, pollInterval)
	if err != nil {
		return turnResult{}, err
	}
	return turnResult{
		Output:     sanitizeTurnCapture(rawCapture, prepared),
		RawCapture: rawCapture,
	}, nil
}

func waitForDone(agent workflowAgent, prepared preparedTurn, idleTimeout time.Duration, pollInterval time.Duration) (string, error) {
	if idleTimeout <= 0 {
		idleTimeout = 120 * time.Second
	}
	if pollInterval <= 0 {
		pollInterval = 250 * time.Millisecond
	}
	lastTurnCapture := ""
	lastChange := time.Now()
	sessionName := ""
	if agent != nil {
		sessionName = agent.SessionName()
	}

	for {
		capture, err := agent.Capture()
		if err != nil {
			return "", err
		}
		turnCapture := extractTurnCapture(capture, prepared.Marker)
		if turnCapture != lastTurnCapture {
			lastTurnCapture = turnCapture
			lastChange = time.Now()
		}
		if isTurnComplete(turnCapture, prepared) && time.Since(lastChange) >= pollInterval {
			return turnCapture, nil
		}
		if time.Since(lastChange) >= idleTimeout {
			return "", NewAgentTimeoutError(sessionName, turnCapture)
		}
		time.Sleep(pollInterval)
	}
}

func NewAgentTimeoutError(sessionName string, capture string) error {
	return agentpkg.NewAgentError(agentpkg.ErrorKindTimeout, sessionName, capture, fmt.Errorf("session %s timed out waiting for completion marker", sessionName))
}

func extractTurnCapture(capture string, marker string) string {
	if capture == "" {
		return ""
	}
	if marker == "" {
		return capture
	}
	index := strings.Index(capture, marker)
	if index < 0 {
		return ""
	}
	return stripTurnMarker(capture[index:], marker)
}

func isTurnComplete(capture string, turn preparedTurn) bool {
	return lastNonEmptyLine(responseCapture(capture, turn.PromptBody)) == doneMarker
}

func sanitizeTurnCapture(capture string, turn preparedTurn) string {
	trimmed := strings.TrimLeft(responseCapture(capture, turn.PromptBody), "\r\n")
	trimmed = strings.TrimRight(trimmed, "\r\n")
	if strings.HasSuffix(trimmed, doneMarker) {
		trimmed = strings.TrimRight(strings.TrimSuffix(trimmed, doneMarker), "\r\n")
	}
	if trimmed == "" {
		return ""
	}
	return trimmed + "\n"
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

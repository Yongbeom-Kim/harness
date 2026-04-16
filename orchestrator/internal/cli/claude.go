package cli

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
)

var claudeSessionIDReader io.Reader = rand.Reader

type ClaudeCliTool struct {
	existingSessionId *string
}

func NewClaudeCliTool(existingSessionID *string) *ClaudeCliTool {
	return &ClaudeCliTool{existingSessionId: existingSessionID}
}

func (c *ClaudeCliTool) ExistingSessionID() *string {
	return c.existingSessionId
}

func (c *ClaudeCliTool) SendMessage(options CliToolSendMessageOptions) (stdout string, stderr string, err error) {
	claudeArgs := []string{"-p"}
	var nextSessionID *string

	if c.existingSessionId == nil || options.CreateNewSession {
		sessionID, sessionErr := newClaudeSessionID()
		if sessionErr != nil {
			c.existingSessionId = nil
			return "", "", sessionErr
		}
		nextSessionID = &sessionID
		claudeArgs = append(claudeArgs, "--session-id", sessionID)
	} else {
		claudeArgs = append(claudeArgs, "--resume", *c.existingSessionId)
	}
	claudeArgs = append(claudeArgs, options.Message)

	cmd := execWithSourcedEnv("claude", claudeArgs...)
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err = cmd.Run()
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()
	if err != nil {
		if options.CreateNewSession || c.existingSessionId == nil {
			c.existingSessionId = nil
		}
		return
	}
	if nextSessionID != nil {
		c.existingSessionId = nextSessionID
	}
	return
}

func newClaudeSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(claudeSessionIDReader, b); err != nil {
		return "", fmt.Errorf("failed to generate Claude session ID: %w", err)
	}

	// RFC 4122 version 4 UUID.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	), nil
}

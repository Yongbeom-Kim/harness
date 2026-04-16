package cli

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var codexSessionIDPattern = regexp.MustCompile(`(?m)^session id:\s*(\S+)\s*$`)
var codexSessionIDJSONPattern = regexp.MustCompile(`"session_id"\s*:\s*"([^"]+)"`)
var execCommand = exec.Command

type CodexCliTool struct {
	existingSessionId *string
}

func NewCodexCliTool(existingSessionID *string) *CodexCliTool {
	return &CodexCliTool{existingSessionId: existingSessionID}
}

func (c *CodexCliTool) ExistingSessionID() *string {
	return c.existingSessionId
}

func (c *CodexCliTool) SendMessage(options CliToolSendMessageOptions) (stdout string, stderr string, err error) {
	needsSessionID := c.existingSessionId == nil || options.CreateNewSession
	if c.existingSessionId == nil {
		options.CreateNewSession = true
	}

	cmdArgs := []string{"exec"}
	if !options.CreateNewSession {
		cmdArgs = append(cmdArgs, "resume", *c.existingSessionId)
	}

	cmd := execWithSourcedEnv("codex", cmdArgs...)
	cmd.Stdin = strings.NewReader(options.Message)
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err = cmd.Run()
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	var extractedSessionID *string
	if sessionID, ok := extractCodexSessionID(stderr); ok {
		extractedSessionID = &sessionID
	}

	if err != nil {
		if options.CreateNewSession {
			c.existingSessionId = nil
		}
		return
	}

	if needsSessionID {
		if extractedSessionID == nil {
			c.existingSessionId = nil
			return stdout, stderr, fmt.Errorf("failed to extract Codex session ID")
		}
		c.existingSessionId = extractedSessionID
	} else if extractedSessionID != nil {
		c.existingSessionId = extractedSessionID
	}

	return
}

func extractCodexSessionID(stderr string) (string, bool) {
	plainSessionID, plainIndex, plainOK := extractLastPatternMatch(stderr, codexSessionIDPattern)
	jsonSessionID, jsonIndex, jsonOK := extractLastPatternMatch(stderr, codexSessionIDJSONPattern)

	if plainOK && (!jsonOK || plainIndex > jsonIndex) {
		return plainSessionID, true
	}
	if jsonOK {
		return jsonSessionID, true
	}
	return "", false
}

func extractLastPatternMatch(input string, pattern *regexp.Regexp) (string, int, bool) {
	matches := pattern.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return "", 0, false
	}

	lastMatch := matches[len(matches)-1]
	return input[lastMatch[2]:lastMatch[3]], lastMatch[0], true
}

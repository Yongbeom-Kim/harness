package main

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	workflowSessionPrefix       = "implement-with-reviewer-"
	markerImplementationReady   = "[IWR_IMPLEMENTATION_READY]"
	markerChangesRequested      = "[IWR_CHANGES_REQUESTED]"
	markerApproved              = "[IWR_APPROVED]"
	markerBlocked               = "[IWR_BLOCKED]"
	implementerMkpipeRoleSuffix = "implementer"
	reviewerMkpipeRoleSuffix    = "reviewer"
)

func generateSessionName(now time.Time) string {
	suffix := sanitizeSessionSuffix(now.UTC().Format("20060102-150405-000000000"))
	if suffix == "" {
		suffix = "session"
	}
	return workflowSessionPrefix + suffix
}

func buildImplementerPrompt(task string, reviewerPipePath string, sessionName string) string {
	return fmt.Sprintf(`You are the implementer agent in a shared tmux workflow.
Role: implementer
Shared tmux session: %s
Peer reviewer mkpipe: %s
Workspace context: both agents are live in the same workspace and tmux session.

Original task:
%s

Protocol markers:
- %s
- %s
- %s
- %s

Protocol rules:
- Begin implementation immediately.
- The Go harness owns all mkpipe listeners and delivers inbound peer messages into this chat.
- Do not create, open, tail, read from, or listen on any mkpipe yourself.
- When the change is reviewable, write to the reviewer mkpipe at %s.
- Your only mkpipe responsibility is writing outbound messages to that reviewer mkpipe path.
- Every mkpipe message must start with exactly one protocol marker on line 1.
- Use %s when handing off a reviewable implementation and summarize what changed.
- If the reviewer responds with %s, make the requested changes and send another %s handoff.
- If the reviewer responds with %s, stop autonomous pipe messaging and remain idle in your pane for human follow-up.
- If you are blocked, ask the reviewer first through mkpipe with %s.
`, sessionName, reviewerPipePath, task, markerImplementationReady, markerChangesRequested, markerApproved, markerBlocked, reviewerPipePath, markerImplementationReady, markerChangesRequested, markerImplementationReady, markerApproved, markerBlocked)
}

func buildReviewerPrompt(task string, implementerPipePath string, sessionName string) string {
	return fmt.Sprintf(`You are the reviewer agent in a shared tmux workflow.
Role: reviewer
Shared tmux session: %s
Peer implementer mkpipe: %s
Workspace context: both agents are live in the same workspace and tmux session.

Original task:
%s

Protocol markers:
- %s
- %s
- %s
- %s

Protocol rules:
- Do not produce an immediate review. Wait for the implementer handoff to be delivered into this chat by the harness first.
- The Go harness owns all mkpipe listeners and delivers inbound peer messages into this chat.
- Do not create, open, tail, read from, or listen on any mkpipe yourself.
- Expect implementer handoff messages to start with %s.
- Respond to reviewable changes by writing back to the implementer mkpipe at %s.
- Your only mkpipe responsibility is writing outbound messages to that implementer mkpipe path.
- Use %s when approval is withheld and include specific actionable feedback.
- Use %s for terminal approval; after sending it both agents remain idle for human follow-up.
- If you are blocked, ask the implementer first through mkpipe with %s.
`, sessionName, implementerPipePath, task, markerImplementationReady, markerChangesRequested, markerApproved, markerBlocked, markerImplementationReady, implementerPipePath, markerChangesRequested, markerApproved, markerBlocked)
}

func sanitizeSessionSuffix(raw string) string {
	var builder strings.Builder
	lastDash := false

	for _, r := range raw {
		allowed := r == '.' || r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
		if allowed && r < unicode.MaxASCII {
			builder.WriteRune(r)
			lastDash = r == '-'
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}

	return strings.Trim(builder.String(), "-")
}

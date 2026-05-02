package main

import (
	"fmt"
	"strings"
)

const (
	approvedMarker = "<promise>APPROVED</promise>"
	doneMarker     = "<promise>done</promise>"

	implementerRolePrompt = "You are an expert software implementer. When given a task or reviewer feedback, output only clean, working code. No explanations, no markdown fences unless the task explicitly requires a file."
	reviewerRolePrompt    = "You are a strict code reviewer. Review the implementation provided. If it is correct, complete, and handles edge cases properly, respond with <promise>APPROVED</promise> and then the required completion marker on the final line. Otherwise respond with specific, actionable feedback only, followed by the required completion marker. No praise, no filler."
)

func buildRoleContract(role string) string {
	var base string
	switch role {
	case roleImplementer:
		base = implementerRolePrompt
	case roleReviewer:
		base = reviewerRolePrompt
	default:
		base = fmt.Sprintf("You are the %s for this workflow.", role)
	}
	instructions := buildSideChannelInstructions(role)
	if instructions == "" {
		return base
	}
	return strings.TrimSpace(base) + "\n\n" + instructions
}

func BuildImplementerPrompt(task string) string {
	return task
}

func BuildReviewerPrompt(task string, implementation string) string {
	return fmt.Sprintf("Task given to implementer:\n%s\n\nImplementation:\n%s", task, implementation)
}

func BuildRewritePrompt(task string, implementation string, review string) string {
	return fmt.Sprintf(
		"Original task:\n%s\n\nYour previous implementation:\n%s\n\nReviewer feedback:\n%s\n\nRewrite addressing all feedback.",
		task,
		implementation,
		review,
	)
}

func isApproved(review string) bool {
	return strings.Contains(review, approvedMarker)
}

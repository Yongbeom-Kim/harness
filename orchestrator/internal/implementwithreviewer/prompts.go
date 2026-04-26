package implementwithreviewer

import (
	"fmt"
	"strings"
)

const (
	ApprovedMarker        = "<promise>APPROVED</promise>"
	DoneMarker            = "<promise>done</promise>"
	ImplementerRolePrompt = "You are an expert software implementer. When given a task or reviewer feedback, output only clean, working code. No explanations, no markdown fences unless the task explicitly requires a file."
	ReviewerRolePrompt    = "You are a strict code reviewer. Review the implementation provided. If it is correct, complete, and handles edge cases properly, respond with exactly: <promise>APPROVED</promise> - nothing else. Otherwise respond with specific, actionable feedback only. No praise, no filler."
)

func BuildInitialImplementerPrompt(task string) string {
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
	return strings.Contains(review, ApprovedMarker)
}

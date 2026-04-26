package implementwithreviewer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Yongbeom-Kim/harness/orchestrator/internal/cli"
	"github.com/google/uuid"
)

type sessionBinding struct {
	role    string
	backend string
	session cli.Session
}

type runner struct {
	cfg         RunConfig
	runID       string
	artifacts   ArtifactSink
	implementer sessionBinding
	reviewer    sessionBinding
}

func Run(ctx context.Context, cfg RunConfig) error {
	cfg = defaultRunConfig(cfg)
	if err := validateRunConfig(cfg); err != nil {
		writeRuntimeError(cfg.Stderr, err)
		return NewExitError(1, true, err)
	}

	r := &runner{cfg: cfg}
	r.printRunHeader()
	return r.run(ctx)
}

func NewRunID() (string, error) {
	runID, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("failed to generate run ID: %w", err)
	}
	return runID.String(), nil
}

func (r *runner) run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return r.finish(resultStatusFailed, 0, "", err)
	}

	runID, err := r.cfg.NewRunID()
	if err != nil {
		return r.finish(resultStatusFailed, 0, "", err)
	}
	r.runID = runID

	artifacts, err := r.cfg.NewArtifactWriter(runID)
	if err != nil {
		return r.finish(resultStatusFailed, 0, "", err)
	}
	r.artifacts = artifacts

	if err := r.appendTransition("run_started", 0, "", "", ""); err != nil {
		return r.finish(resultStatusFailed, 0, "", err)
	}

	implementer, err := r.newSession(RoleImplementer, r.cfg.Implementer)
	if err != nil {
		return r.finish(resultStatusFailed, 0, "", r.decorateRoleError(RoleImplementer, "session creation", err))
	}
	r.implementer = implementer

	reviewer, err := r.newSession(RoleReviewer, r.cfg.Reviewer)
	if err != nil {
		return r.finish(resultStatusFailed, 0, "", r.decorateRoleError(RoleReviewer, "session creation", err))
	}
	r.reviewer = reviewer

	if err := r.startSessions(); err != nil {
		return r.finish(resultStatusFailed, 0, "", err)
	}

	if err := r.writeMetadata(); err != nil {
		return r.finish(resultStatusFailed, 0, "", err)
	}

	implementation, err := r.executeTurn(0, r.implementer, BuildInitialImplementerPrompt(r.cfg.Task))
	if err != nil {
		return r.finish(resultStatusFailed, 0, "", err)
	}

	for iteration := 1; iteration <= r.cfg.MaxIterations; iteration++ {
		if err := ctx.Err(); err != nil {
			return r.finish(resultStatusFailed, iteration, implementation, err)
		}

		review, err := r.executeTurn(iteration, r.reviewer, BuildReviewerPrompt(r.cfg.Task, implementation))
		if err != nil {
			return r.finish(resultStatusFailed, iteration, implementation, err)
		}
		if isApproved(review) {
			if err := r.appendTransition("approved", iteration, r.reviewer.role, r.reviewer.backend, ""); err != nil {
				return r.finish(resultStatusFailed, iteration, implementation, err)
			}
			return r.finish(resultStatusApproved, iteration, implementation, nil)
		}

		implementation, err = r.executeTurn(iteration, r.implementer, BuildRewritePrompt(r.cfg.Task, implementation, review))
		if err != nil {
			return r.finish(resultStatusFailed, iteration, implementation, err)
		}
	}

	if err := r.appendTransition("non_converged", r.cfg.MaxIterations, "", "", ""); err != nil {
		return r.finish(resultStatusFailed, r.cfg.MaxIterations, implementation, err)
	}
	return r.finish(resultStatusNonConverged, r.cfg.MaxIterations, implementation, nil)
}

func (r *runner) newSession(role string, backend string) (sessionBinding, error) {
	session, err := r.cfg.NewSession(backend, cli.SessionOptions{
		RunID:       r.runID,
		Role:        role,
		IdleTimeout: r.cfg.IdleTimeout,
	})
	if err != nil {
		return sessionBinding{}, err
	}

	binding := sessionBinding{
		role:    role,
		backend: backend,
		session: session,
	}
	if err := r.appendTransition("session_created", 0, role, backend, session.SessionName()); err != nil {
		return sessionBinding{}, err
	}
	return binding, nil
}

func (r *runner) startSessions() error {
	for _, binding := range []sessionBinding{r.implementer, r.reviewer} {
		var rolePrompt string
		switch binding.role {
		case RoleImplementer:
			rolePrompt = ImplementerRolePrompt
		case RoleReviewer:
			rolePrompt = ReviewerRolePrompt
		default:
			return fmt.Errorf("unknown role %q", binding.role)
		}

		if err := binding.session.Start(rolePrompt); err != nil {
			if captureErr := r.writeFailureCapture(0, binding.role, err); captureErr != nil {
				err = errors.Join(err, captureErr)
			}
			if transitionErr := r.appendTransition("startup_failed", 0, binding.role, binding.backend, ""); transitionErr != nil {
				err = errors.Join(err, transitionErr)
			}
			return r.decorateRoleError(binding.role, "startup", err)
		}

		if err := r.appendTransition("session_started", 0, binding.role, binding.backend, ""); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) writeMetadata() error {
	return r.artifacts.WriteMetadata(RunMetadata{
		RunID:              r.runID,
		Task:               r.cfg.Task,
		Implementer:        r.cfg.Implementer,
		Reviewer:           r.cfg.Reviewer,
		MaxIterations:      r.cfg.MaxIterations,
		IdleTimeoutSeconds: int64(r.cfg.IdleTimeout / time.Second),
		CreatedAt:          time.Now().UTC(),
		Sessions: map[string]SessionMetadata{
			RoleImplementer: {
				Backend:         r.implementer.backend,
				TmuxSessionName: r.implementer.session.SessionName(),
			},
			RoleReviewer: {
				Backend:         r.reviewer.backend,
				TmuxSessionName: r.reviewer.session.SessionName(),
			},
		},
	})
}

func (r *runner) executeTurn(iteration int, binding sessionBinding, prompt string) (string, error) {
	printBanner(r.cfg.Stdout, bannerTitle(iteration, strings.ToUpper(binding.role), binding.backend))

	result, err := binding.session.RunTurn(prompt)
	if err != nil {
		if captureErr := r.writeFailureCapture(iteration, binding.role, err); captureErr != nil {
			err = errors.Join(err, captureErr)
		}
		if transitionErr := r.appendTransition("turn_failed", iteration, binding.role, binding.backend, ""); transitionErr != nil {
			err = errors.Join(err, transitionErr)
		}
		return "", r.decorateRoleError(binding.role, "turn", err)
	}

	writeTurnOutput(r.cfg.Stdout, result.Output)

	if err := r.artifacts.WriteCapture(successCaptureName(iteration, binding.role), result.RawCapture); err != nil {
		return "", err
	}
	if err := r.appendTransition("turn_completed", iteration, binding.role, binding.backend, ""); err != nil {
		return "", err
	}

	return result.Output, nil
}

func (r *runner) finish(status string, iterations int, implementation string, cause error) error {
	if closeErr := r.closeSessions(); closeErr != nil {
		cause = errors.Join(cause, closeErr)
		status = resultStatusFailed
	}

	if cause != nil {
		if transitionErr := r.appendTransition("failed", iterations, "", "", cause.Error()); transitionErr != nil {
			cause = errors.Join(cause, transitionErr)
		}
	}

	if r.artifacts != nil {
		result := r.resultFor(status, iterations, implementation, cause)
		if err := r.artifacts.WriteResult(result); err != nil {
			cause = errors.Join(cause, err)
			status = resultStatusFailed
		}
	}

	if cause != nil {
		writeRuntimeError(r.cfg.Stderr, cause)
		return NewExitError(1, true, cause)
	}

	switch status {
	case resultStatusApproved:
		r.writeSuccessSummary(iterations, implementation)
		return nil
	case resultStatusNonConverged:
		r.writeNonConvergenceSummary(iterations)
		return NewExitError(1, true, nil)
	default:
		return NewExitError(1, true, nil)
	}
}

func (r *runner) resultFor(status string, iterations int, implementation string, cause error) RunResult {
	result := RunResult{
		RunID:               r.runID,
		Status:              status,
		Approved:            status == resultStatusApproved,
		Iterations:          iterations,
		FinalImplementation: implementation,
		CompletedAt:         time.Now().UTC(),
	}
	if cause != nil {
		result.Error = cause.Error()
	}
	return result
}

func (r *runner) closeSessions() error {
	var closeErr error
	for _, binding := range []sessionBinding{r.implementer, r.reviewer} {
		if binding.session == nil {
			continue
		}
		if err := binding.session.Close(); err != nil {
			closeErr = errors.Join(closeErr, r.decorateRoleError(binding.role, "cleanup", err))
			if transitionErr := r.appendTransition("cleanup_failed", 0, binding.role, binding.backend, err.Error()); transitionErr != nil {
				closeErr = errors.Join(closeErr, transitionErr)
			}
			continue
		}
		if err := r.appendTransition("closed", 0, binding.role, binding.backend, ""); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	return closeErr
}

func (r *runner) appendTransition(state string, iteration int, role string, backend string, details string) error {
	if r.artifacts == nil {
		return nil
	}
	return r.artifacts.AppendTransition(StateTransition{
		At:        time.Now().UTC(),
		State:     state,
		Iteration: iteration,
		Role:      role,
		Backend:   backend,
		Details:   details,
	})
}

func (r *runner) writeFailureCapture(iteration int, role string, err error) error {
	if r.artifacts == nil {
		return nil
	}
	sessionErr, ok := cli.AsSessionError(err)
	if !ok || sessionErr.Capture() == "" {
		return nil
	}
	suffix := sessionErr.Kind()
	if suffix == "" {
		suffix = "capture"
	}
	return r.artifacts.WriteCapture(failureCaptureName(iteration, role, suffix), sessionErr.Capture())
}

func (r *runner) decorateRoleError(role string, action string, err error) error {
	return fmt.Errorf("%s %s failed: %w", role, action, err)
}

func (r *runner) printRunHeader() {
	fmt.Fprintf(r.cfg.Stdout, "Implementer : %s\n", r.cfg.Implementer)
	fmt.Fprintf(r.cfg.Stdout, "Reviewer    : %s\n", r.cfg.Reviewer)
	fmt.Fprintf(r.cfg.Stdout, "Task        : %s\n", r.cfg.Task)
}

func (r *runner) writeSuccessSummary(iterations int, implementation string) {
	fmt.Fprintln(r.cfg.Stdout)
	fmt.Fprintf(r.cfg.Stdout, "Approved after %d review round(s).\n", iterations)
	fmt.Fprintln(r.cfg.Stdout)
	fmt.Fprintln(r.cfg.Stdout, "Final implementation")
	fmt.Fprintln(r.cfg.Stdout, implementation)
}

func (r *runner) writeNonConvergenceSummary(iterations int) {
	fmt.Fprintln(r.cfg.Stdout)
	fmt.Fprintf(r.cfg.Stdout, "Did not converge after %d iterations.\n", iterations)
}

func printBanner(w io.Writer, title string) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "--- %s ---\n", title)
}

func writeTurnOutput(w io.Writer, output string) {
	if output == "" {
		return
	}
	fmt.Fprint(w, output)
	if !strings.HasSuffix(output, "\n") {
		fmt.Fprintln(w)
	}
}

func writeRuntimeError(w io.Writer, err error) {
	if err == nil {
		return
	}
	message := err.Error()
	if message == "" {
		return
	}
	fmt.Fprint(w, message)
	if !strings.HasSuffix(message, "\n") {
		fmt.Fprintln(w)
	}
}

func bannerTitle(iteration int, role string, backend string) string {
	return fmt.Sprintf("iter %d - %s (%s)", iteration, role, backend)
}

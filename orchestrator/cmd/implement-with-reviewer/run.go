package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	agentpkg "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent"
	"github.com/google/uuid"
)

type sessionBinding struct {
	role    string
	backend string
	session workflowAgent
}

type runner struct {
	cfg                          runConfig
	runID                        string
	artifacts                    artifactSink
	implementer                  sessionBinding
	reviewer                     sessionBinding
	communicationChannelManagers []channelManager
	sideChannel                  *sideChannelCoordinator
	sideChannelErrs              chan error
	sideChannelDone              chan struct{}
	sideChannelStop              chan struct{}
	sideChannelStopOnce          sync.Once
}

func runWorkflow(ctx context.Context, cfg runConfig) error {
	cfg = defaultRunConfig(cfg)
	if err := validateRunConfig(cfg); err != nil {
		writeRuntimeError(cfg.Stderr, err)
		return newExitError(1, true, err)
	}

	r := &runner{cfg: cfg}
	r.printRunHeader()
	return r.run(ctx)
}

func newRunID() (string, error) {
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

	if transitionErr := r.appendTransition("run_started", 0, "", "", ""); transitionErr != nil {
		return r.finish(resultStatusFailed, 0, "", transitionErr)
	}
	if startErr := r.startCommunicationChannelManagers(); startErr != nil {
		return r.finish(resultStatusFailed, 0, "", startErr)
	}

	implementer, err := r.newSession(roleImplementer, r.cfg.Implementer)
	if err != nil {
		return r.finish(resultStatusFailed, 0, "", r.decorateRoleError(roleImplementer, "session creation", err))
	}
	r.implementer = implementer

	reviewer, err := r.newSession(roleReviewer, r.cfg.Reviewer)
	if err != nil {
		return r.finish(resultStatusFailed, 0, "", r.decorateRoleError(roleReviewer, "session creation", err))
	}
	r.reviewer = reviewer

	r.startSideChannelLoop()

	if startErr := r.startSessions(); startErr != nil {
		return r.finish(resultStatusFailed, 0, "", startErr)
	}
	if metadataErr := r.writeMetadata(); metadataErr != nil {
		return r.finish(resultStatusFailed, 0, "", metadataErr)
	}

	implementation, err := r.executeTurn(0, r.implementer, BuildImplementerPrompt(r.cfg.Task))
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
			if transitionErr := r.appendTransition("approved", iteration, r.reviewer.role, r.reviewer.backend, ""); transitionErr != nil {
				return r.finish(resultStatusFailed, iteration, implementation, transitionErr)
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
	sessionName := buildSessionName(r.runID, role)
	session, err := r.cfg.NewAgent(backend, sessionName)
	if err != nil {
		return sessionBinding{}, err
	}
	binding := sessionBinding{role: role, backend: backend, session: session}
	if err := r.appendTransition("session_created", 0, role, backend, session.SessionName()); err != nil {
		return sessionBinding{}, err
	}
	return binding, nil
}

func (r *runner) startSessions() error {
	for _, binding := range []sessionBinding{r.implementer, r.reviewer} {
		if err := r.checkSideChannelError(); err != nil {
			return err
		}
		if err := binding.session.Start(); err != nil {
			if captureErr := r.writeFailureCapture(0, binding.role, err); captureErr != nil {
				err = errors.Join(err, captureErr)
			}
			if transitionErr := r.appendTransition("startup_failed", 0, binding.role, binding.backend, ""); transitionErr != nil {
				err = errors.Join(err, transitionErr)
			}
			return r.decorateRoleError(binding.role, "startup", err)
		}
		if err := binding.session.WaitUntilReady(); err != nil {
			if captureErr := r.writeFailureCapture(0, binding.role, err); captureErr != nil {
				err = errors.Join(err, captureErr)
			}
			if transitionErr := r.appendTransition("startup_failed", 0, binding.role, binding.backend, ""); transitionErr != nil {
				err = errors.Join(err, transitionErr)
			}
			return r.decorateRoleError(binding.role, "startup", err)
		}
		if r.sideChannel != nil {
			r.sideChannel.MarkReady(binding.role)
		}
		if err := r.appendTransition("session_started", 0, binding.role, binding.backend, ""); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) writeMetadata() error {
	return r.artifacts.WriteMetadata(runMetadata{
		RunID:              r.runID,
		Task:               r.cfg.Task,
		Implementer:        r.cfg.Implementer,
		Reviewer:           r.cfg.Reviewer,
		MaxIterations:      r.cfg.MaxIterations,
		IdleTimeoutSeconds: int64(r.cfg.IdleTimeout / time.Second),
		CreatedAt:          time.Now().UTC(),
		Sessions: map[string]sessionMetadata{
			roleImplementer: {Backend: r.implementer.backend, TmuxSessionName: r.implementer.session.SessionName()},
			roleReviewer:    {Backend: r.reviewer.backend, TmuxSessionName: r.reviewer.session.SessionName()},
		},
	})
}

func (r *runner) executeTurn(iteration int, binding sessionBinding, prompt string) (string, error) {
	if err := r.checkSideChannelError(); err != nil {
		return "", err
	}
	printBanner(r.cfg.Stdout, bannerTitle(iteration, strings.ToUpper(binding.role), binding.backend))

	result, err := runAgentTurn(binding.session, binding.role, prompt, r.cfg.IdleTimeout, 250*time.Millisecond)
	if err != nil {
		if captureErr := r.writeFailureCapture(iteration, binding.role, err); captureErr != nil {
			err = errors.Join(err, captureErr)
		}
		if transitionErr := r.appendTransition("turn_failed", iteration, binding.role, binding.backend, ""); transitionErr != nil {
			err = errors.Join(err, transitionErr)
		}
		return "", r.decorateRoleError(binding.role, "turn", err)
	}
	if err := r.checkSideChannelError(); err != nil {
		return "", err
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
	if sideChannelErr := r.checkSideChannelError(); sideChannelErr != nil {
		cause = errors.Join(cause, sideChannelErr)
		status = resultStatusFailed
	}
	if stopErr := r.stopCommunicationChannelManagers(); stopErr != nil {
		cause = errors.Join(cause, stopErr)
		status = resultStatusFailed
	}
	if sideChannelErr := r.checkSideChannelError(); sideChannelErr != nil {
		cause = errors.Join(cause, sideChannelErr)
		status = resultStatusFailed
	}
	if closeErr := r.closeSessions(); closeErr != nil {
		cause = errors.Join(cause, closeErr)
		status = resultStatusFailed
	}
	if removeErr := r.removeCommunicationChannelManagers(); removeErr != nil {
		cause = errors.Join(cause, removeErr)
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
		}
	}
	if cause != nil {
		writeRuntimeError(r.cfg.Stderr, cause)
		return newExitError(1, true, cause)
	}
	switch status {
	case resultStatusApproved:
		r.writeSuccessSummary(iterations, implementation)
		return nil
	case resultStatusNonConverged:
		r.writeNonConvergenceSummary(iterations)
		return newExitError(1, true, nil)
	default:
		return newExitError(1, true, nil)
	}
}

func (r *runner) resultFor(status string, iterations int, implementation string, cause error) runResult {
	result := runResult{
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
	return r.artifacts.AppendTransition(stateTransition{
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
	if agentErr, ok := agentpkg.AsAgentError(err); ok && agentErr.Capture != "" {
		suffix := agentErr.Kind
		if suffix == "" {
			suffix = "capture"
		}
		return r.artifacts.WriteCapture(failureCaptureName(iteration, role, suffix), agentErr.Capture)
	}
	return nil
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

func (r *runner) startCommunicationChannelManagers() error {
	paths := []string{toReviewerPipePath, toImplementerPipePath}
	managers := make([]channelManager, 0, len(paths))
	for _, path := range paths {
		manager, err := r.cfg.NewCommunicationChannelManager(channelConfig{Path: path})
		if err != nil {
			for _, created := range managers {
				_ = created.Stop()
				_ = created.Remove()
			}
			return fmt.Errorf("side-channel startup failed: %w", err)
		}
		managers = append(managers, manager)
	}
	r.communicationChannelManagers = managers
	return nil
}

func (r *runner) startSideChannelLoop() {
	if len(r.communicationChannelManagers) == 0 {
		return
	}
	r.sideChannel = newSideChannelCoordinator(r.artifacts, map[string]workflowAgent{
		roleImplementer: r.implementer.session,
		roleReviewer:    r.reviewer.session,
	})
	r.sideChannelErrs = make(chan error, 1)
	r.sideChannelDone = make(chan struct{})
	r.sideChannelStop = make(chan struct{})
	mergedMessages := make(chan channelMessage, len(r.communicationChannelManagers))
	mergedErrs := make(chan error, len(r.communicationChannelManagers))
	var forwarders sync.WaitGroup
	for _, manager := range r.communicationChannelManagers {
		forwarders.Add(1)
		go func(manager channelManager) {
			defer forwarders.Done()
			forwardCommunicationChannel(manager, mergedMessages, mergedErrs, r.sideChannelStop)
		}(manager)
	}
	go func() {
		forwarders.Wait()
		close(mergedMessages)
		close(mergedErrs)
	}()
	go func() {
		defer close(r.sideChannelDone)
		messages := mergedMessages
		errs := mergedErrs
		for messages != nil || errs != nil {
			select {
			case msg, ok := <-messages:
				if !ok {
					messages = nil
					continue
				}
				if err := r.sideChannel.HandleMessage(msg); err != nil {
					r.reportSideChannelError(err)
					return
				}
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				if err == nil {
					continue
				}
				if eventErr := r.recordReaderError(err); eventErr != nil {
					r.reportSideChannelError(errors.Join(err, eventErr))
					return
				}
				r.reportSideChannelError(fmt.Errorf("side-channel infrastructure failed: %w", err))
				return
			}
		}
	}()
}

func forwardCommunicationChannel(manager channelManager, messages chan<- channelMessage, errs chan<- error, stop <-chan struct{}) {
	managerMessages := manager.Messages()
	managerErrs := manager.Errors()
	for managerMessages != nil || managerErrs != nil {
		select {
		case <-stop:
			return
		case msg, ok := <-managerMessages:
			if !ok {
				managerMessages = nil
				continue
			}
			select {
			case <-stop:
				return
			case messages <- msg:
			}
		case err, ok := <-managerErrs:
			if !ok {
				managerErrs = nil
				continue
			}
			select {
			case <-stop:
				return
			case errs <- err:
			}
		}
	}
}

func (r *runner) reportSideChannelError(err error) {
	if err == nil || r.sideChannelErrs == nil {
		return
	}
	select {
	case r.sideChannelErrs <- err:
	default:
	}
}

func (r *runner) checkSideChannelError() error {
	if r.sideChannelErrs == nil {
		return nil
	}
	select {
	case err := <-r.sideChannelErrs:
		return err
	default:
		return nil
	}
}

func (r *runner) recordReaderError(err error) error {
	if r.artifacts == nil {
		return nil
	}
	return r.artifacts.AppendChannelEvent(readerEventFromError(err))
}

func (r *runner) stopCommunicationChannelManagers() error {
	if len(r.communicationChannelManagers) == 0 {
		return nil
	}
	var stopErr error
	for _, manager := range r.communicationChannelManagers {
		if err := manager.Stop(); err != nil {
			stopErr = errors.Join(stopErr, err)
		}
	}
	if stopErr != nil {
		r.stopSideChannelForwarders()
		return fmt.Errorf("side-channel stop failed: %w", stopErr)
	}
	if r.sideChannelDone != nil {
		select {
		case <-r.sideChannelDone:
		case <-time.After(100 * time.Millisecond):
			r.stopSideChannelForwarders()
			<-r.sideChannelDone
		}
	}
	return nil
}

func (r *runner) stopSideChannelForwarders() {
	if r.sideChannelStop == nil {
		return
	}
	r.sideChannelStopOnce.Do(func() {
		close(r.sideChannelStop)
	})
}

func (r *runner) removeCommunicationChannelManagers() error {
	if len(r.communicationChannelManagers) == 0 {
		return nil
	}
	var removeErr error
	for _, manager := range r.communicationChannelManagers {
		if err := manager.Remove(); err != nil {
			removeErr = errors.Join(removeErr, err)
		}
	}
	if removeErr != nil {
		return fmt.Errorf("side-channel cleanup failed: %w", removeErr)
	}
	return nil
}

func defaultRunConfig(cfg runConfig) runConfig {
	if cfg.Stdout == nil {
		cfg.Stdout = io.Discard
	}
	if cfg.Stderr == nil {
		cfg.Stderr = io.Discard
	}
	return cfg
}

func validateRunConfig(cfg runConfig) error {
	switch {
	case cfg.Task == "":
		return errors.New("task must not be empty")
	case cfg.Implementer == "":
		return errors.New("implementer backend must not be empty")
	case cfg.Reviewer == "":
		return errors.New("reviewer backend must not be empty")
	case cfg.MaxIterations < 1:
		return errors.New("max iterations must be >= 1")
	case cfg.IdleTimeout <= 0:
		return errors.New("idle timeout must be > 0")
	case cfg.NewAgent == nil:
		return errors.New("NewAgent must not be nil")
	case cfg.NewArtifactWriter == nil:
		return errors.New("NewArtifactWriter must not be nil")
	case cfg.NewCommunicationChannelManager == nil:
		return errors.New("NewCommunicationChannelManager must not be nil")
	case cfg.NewRunID == nil:
		return errors.New("NewRunID must not be nil")
	default:
		return nil
	}
}

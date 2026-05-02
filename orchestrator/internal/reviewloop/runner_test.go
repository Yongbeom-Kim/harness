package reviewloop

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	agentsession "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session"
)

type scriptedTurn struct {
	result TurnResult
	err    error
}

type fakeSession struct {
	name             string
	startErr         error
	closeErr         error
	injectErr        error
	turns            []scriptedTurn
	startPrompts     []string
	turnPrompts      []string
	injectedMessages []string
	events           *[]string
	role             string
}

type fakeArtifactWriter struct {
	metadata         []RunMetadata
	transitions      []StateTransition
	channelEvents    []ChannelEvent
	captures         map[string]string
	results          []RunResult
	writeMetadataErr error
	appendErr        error
	appendChannelErr error
	writeCaptureErr  error
	writeCaptureName string
	writeResultErr   error
}

type fakeCommunicationChannelManager struct {
	messages chan ChannelMessage
	errors   chan error
	stop     func() error
	remove   func() error
	stopped  bool
}

func (f *fakeSession) Start(rolePrompt string) error {
	f.startPrompts = append(f.startPrompts, rolePrompt)
	if f.events != nil {
		*f.events = append(*f.events, "start:"+f.role)
	}
	return f.startErr
}

func (f *fakeSession) RunTurn(prompt string) (TurnResult, error) {
	f.turnPrompts = append(f.turnPrompts, prompt)
	if f.events != nil {
		*f.events = append(*f.events, "turn:"+f.role)
	}
	if len(f.turns) == 0 {
		return TurnResult{}, errors.New("unexpected RunTurn call")
	}
	turn := f.turns[0]
	f.turns = f.turns[1:]
	return turn.result, turn.err
}

func (f *fakeSession) InjectSideChannel(message string) error {
	f.injectedMessages = append(f.injectedMessages, message)
	if f.events != nil {
		*f.events = append(*f.events, "inject:"+f.role)
	}
	return f.injectErr
}

func (f *fakeSession) SessionName() string {
	return f.name
}

func (f *fakeSession) Close() error {
	if f.events != nil {
		*f.events = append(*f.events, "close:"+f.role)
	}
	return f.closeErr
}

func (f *fakeArtifactWriter) WriteMetadata(metadata RunMetadata) error {
	f.metadata = append(f.metadata, metadata)
	return f.writeMetadataErr
}

func (f *fakeArtifactWriter) AppendTransition(transition StateTransition) error {
	f.transitions = append(f.transitions, transition)
	return f.appendErr
}

func (f *fakeArtifactWriter) AppendChannelEvent(event ChannelEvent) error {
	f.channelEvents = append(f.channelEvents, event)
	return f.appendChannelErr
}

func (f *fakeArtifactWriter) WriteCapture(name string, text string) error {
	if f.captures == nil {
		f.captures = make(map[string]string)
	}
	f.captures[name] = text
	if f.writeCaptureErr != nil && (f.writeCaptureName == "" || f.writeCaptureName == name) {
		return f.writeCaptureErr
	}
	return nil
}

func (f *fakeArtifactWriter) WriteResult(result RunResult) error {
	f.results = append(f.results, result)
	return f.writeResultErr
}

func (f *fakeCommunicationChannelManager) Messages() <-chan ChannelMessage {
	if f.messages == nil {
		f.messages = make(chan ChannelMessage)
	}
	return f.messages
}

func (f *fakeCommunicationChannelManager) Errors() <-chan error {
	if f.errors == nil {
		f.errors = make(chan error)
	}
	return f.errors
}

func (f *fakeCommunicationChannelManager) Stop() error {
	if f.stop != nil {
		return f.stop()
	}
	if !f.stopped {
		if f.messages == nil {
			f.messages = make(chan ChannelMessage)
		}
		if f.errors == nil {
			f.errors = make(chan error)
		}
		close(f.messages)
		close(f.errors)
		f.stopped = true
	}
	return nil
}

func (f *fakeCommunicationChannelManager) Remove() error {
	if f.remove != nil {
		return f.remove()
	}
	return nil
}

func TestRunStartsBothSessionsBeforeFirstTurn(t *testing.T) {
	var events []string
	writer := &fakeArtifactWriter{}
	implementer := &fakeSession{
		name:   "iwr-run-id-implementer",
		role:   RoleImplementer,
		events: &events,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "package demo\nconst Token = \"v1\"\n<promise>done</promise>\n", RawCapture: "package demo\nconst Token = \"v1\"\n<promise>done</promise>\n"}},
		},
	}
	reviewer := &fakeSession{
		name:   "iwr-run-id-reviewer",
		role:   RoleReviewer,
		events: &events,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "<promise>APPROVED</promise>\n<promise>done</promise>\n", RawCapture: "<promise>APPROVED</promise>\n<promise>done</promise>\n"}},
		},
	}

	stdout, stderr, err := runTestRunner(t, writer, nil, implementer, reviewer, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v\nstderr:\n%s\nstdout:\n%s", err, stderr, stdout)
	}

	if eventIndex(events, "start:"+RoleImplementer) == -1 || eventIndex(events, "start:"+RoleReviewer) == -1 {
		t.Fatalf("expected both sessions to start, got events=%v", events)
	}
	firstTurn := eventIndex(events, "turn:"+RoleImplementer)
	if firstTurn == -1 {
		t.Fatalf("expected implementer turn, got events=%v", events)
	}
	if eventIndex(events, "start:"+RoleImplementer) > firstTurn || eventIndex(events, "start:"+RoleReviewer) > firstTurn {
		t.Fatalf("expected both startups before first turn, got events=%v", events)
	}
	if !strings.Contains(stdout, "Approved after 1 review round(s).") {
		t.Fatalf("stdout missing approval summary:\n%s", stdout)
	}
}

func TestRunStartsCommunicationChannelManagerBeforeSessions(t *testing.T) {
	var events []string
	writer := &fakeArtifactWriter{}
	implementer := &fakeSession{
		name:   "iwr-run-id-implementer",
		role:   RoleImplementer,
		events: &events,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "impl\n<promise>done</promise>\n", RawCapture: "impl\n<promise>done</promise>\n"}},
		},
	}
	reviewer := &fakeSession{
		name:   "iwr-run-id-reviewer",
		role:   RoleReviewer,
		events: &events,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "<promise>APPROVED</promise>\n<promise>done</promise>\n", RawCapture: "<promise>APPROVED</promise>\n<promise>done</promise>\n"}},
		},
	}

	_, _, err := runTestRunner(t, writer, nil, implementer, reviewer, func(cfg *RunConfig) {
		cfg.NewCommunicationChannelManager = func(ChannelConfig) (ChannelManager, error) {
			events = append(events, "manager:create")
			return &fakeCommunicationChannelManager{}, nil
		}
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	managerCreate := eventIndex(events, "manager:create")
	startImplementer := eventIndex(events, "start:"+RoleImplementer)
	startReviewer := eventIndex(events, "start:"+RoleReviewer)
	if managerCreate == -1 || startImplementer == -1 || startReviewer == -1 {
		t.Fatalf("missing events: %v", events)
	}
	if managerCreate > startImplementer || managerCreate > startReviewer {
		t.Fatalf("expected manager create before both startups, got events=%v", events)
	}
	if countEvent(events, "manager:create") != 2 {
		t.Fatalf("expected one manager per FIFO path, got events=%v", events)
	}
}

func TestRunnerStopsManagerBeforeClosingSessions(t *testing.T) {
	var order []string
	writer := &fakeArtifactWriter{}
	toReviewerManager := &fakeCommunicationChannelManager{}
	toReviewerManager.stop = func() error {
		order = append(order, "manager.Stop:"+toReviewerPipePath)
		if toReviewerManager.messages == nil {
			toReviewerManager.messages = make(chan ChannelMessage)
		}
		if toReviewerManager.errors == nil {
			toReviewerManager.errors = make(chan error)
		}
		close(toReviewerManager.messages)
		close(toReviewerManager.errors)
		return nil
	}
	toReviewerManager.remove = func() error {
		order = append(order, "manager.Remove:"+toReviewerPipePath)
		return nil
	}
	toImplementerManager := &fakeCommunicationChannelManager{}
	toImplementerManager.stop = func() error {
		order = append(order, "manager.Stop:"+toImplementerPipePath)
		if toImplementerManager.messages == nil {
			toImplementerManager.messages = make(chan ChannelMessage)
		}
		if toImplementerManager.errors == nil {
			toImplementerManager.errors = make(chan error)
		}
		close(toImplementerManager.messages)
		close(toImplementerManager.errors)
		return nil
	}
	toImplementerManager.remove = func() error {
		order = append(order, "manager.Remove:"+toImplementerPipePath)
		return nil
	}
	implementer := &fakeSession{
		name:     "impl",
		role:     RoleImplementer,
		closeErr: nil,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "impl\n<promise>done</promise>\n", RawCapture: "impl\n<promise>done</promise>\n"}},
		},
	}
	implementer.events = &order
	reviewer := &fakeSession{
		name: "rev",
		role: RoleReviewer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "<promise>APPROVED</promise>\n<promise>done</promise>\n", RawCapture: "<promise>APPROVED</promise>\n<promise>done</promise>\n"}},
		},
	}
	reviewer.events = &order

	_, _, err := runTestRunner(t, writer, nil, implementer, reviewer, func(cfg *RunConfig) {
		cfg.NewCommunicationChannelManager = func(config ChannelConfig) (ChannelManager, error) {
			switch config.Path {
			case toReviewerPipePath:
				return toReviewerManager, nil
			case toImplementerPipePath:
				return toImplementerManager, nil
			default:
				return nil, errors.New("unexpected communication channel path")
			}
		}
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	stopReviewer := eventIndex(order, "manager.Stop:"+toReviewerPipePath)
	stopImplementer := eventIndex(order, "manager.Stop:"+toImplementerPipePath)
	closeImplementer := eventIndex(order, "close:"+RoleImplementer)
	closeReviewer := eventIndex(order, "close:"+RoleReviewer)
	removeReviewer := eventIndex(order, "manager.Remove:"+toReviewerPipePath)
	removeImplementer := eventIndex(order, "manager.Remove:"+toImplementerPipePath)
	if slices.Contains([]int{stopReviewer, stopImplementer, closeImplementer, closeReviewer, removeReviewer, removeImplementer}, -1) {
		t.Fatalf("teardown order missing events: %v", order)
	}
	if stopReviewer > closeImplementer || stopReviewer > closeReviewer || stopImplementer > closeImplementer || stopImplementer > closeReviewer {
		t.Fatalf("expected all manager stops before session close, got %v", order)
	}
	if removeReviewer < closeImplementer || removeReviewer < closeReviewer || removeImplementer < closeImplementer || removeImplementer < closeReviewer {
		t.Fatalf("expected all manager removals after session close, got %v", order)
	}
}

func TestRunFailsOnCommunicationChannelReaderError(t *testing.T) {
	writer := &fakeArtifactWriter{}
	toReviewerManager := &fakeCommunicationChannelManager{
		errors: make(chan error, 1),
	}
	toReviewerManager.errors <- &ChannelReaderError{Path: toReviewerPipePath, Err: errors.New("reader crashed")}
	implementer := &fakeSession{
		name: "iwr-run-id-implementer",
		role: RoleImplementer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "impl\n<promise>done</promise>\n", RawCapture: "impl\n<promise>done</promise>\n"}},
		},
	}
	reviewer := &fakeSession{
		name: "iwr-run-id-reviewer",
		role: RoleReviewer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "<promise>APPROVED</promise>\n<promise>done</promise>\n", RawCapture: "<promise>APPROVED</promise>\n<promise>done</promise>\n"}},
		},
	}

	_, stderr, err := runTestRunner(t, writer, nil, implementer, reviewer, func(cfg *RunConfig) {
		cfg.NewCommunicationChannelManager = func(config ChannelConfig) (ChannelManager, error) {
			switch config.Path {
			case toReviewerPipePath:
				return toReviewerManager, nil
			case toImplementerPipePath:
				return &fakeCommunicationChannelManager{}, nil
			default:
				return nil, errors.New("unexpected communication channel path")
			}
		}
	})
	exitErr, ok := AsExitError(err)
	if !ok || exitErr.Code() != 1 {
		t.Fatalf("expected exit code 1, got err=%v", err)
	}
	if !strings.Contains(stderr, "side-channel infrastructure failed") {
		t.Fatalf("stderr missing side-channel failure:\n%s", stderr)
	}
	if len(writer.channelEvents) == 0 || writer.channelEvents[0].Status != ChannelStatusReaderError {
		t.Fatalf("expected reader_error channel event, got %+v", writer.channelEvents)
	}
}

func TestRunStartupFailureDoesNotPrintStartupTranscript(t *testing.T) {
	writer := &fakeArtifactWriter{}
	implementer := &fakeSession{
		name:     "iwr-run-id-implementer",
		role:     RoleImplementer,
		startErr: agentsession.NewRunnerError(agentsession.RunnerErrorKindStartup, "iwr-run-id-implementer", "startup hidden\n<promise>done</promise>\n", errors.New("backend failed")),
	}
	reviewer := &fakeSession{
		name: "iwr-run-id-reviewer",
		role: RoleReviewer,
	}

	stdout, stderr, err := runTestRunner(t, writer, nil, implementer, reviewer, nil)
	exitErr, ok := AsExitError(err)
	if !ok || exitErr.Code() != 1 {
		t.Fatalf("expected exit code 1, got err=%v", err)
	}
	if strings.Contains(stdout, "startup hidden") {
		t.Fatalf("startup transcript should stay out of stdout:\n%s", stdout)
	}
	if !strings.Contains(stderr, "implementer startup failed") {
		t.Fatalf("stderr missing startup failure:\n%s", stderr)
	}
	if got := writer.captures["iter-0-implementer-startup.txt"]; got != "startup hidden\n<promise>done</promise>\n" {
		t.Fatalf("unexpected startup capture: %q", got)
	}
}

func TestRunKeepsDoneMarkerInFinalImplementation(t *testing.T) {
	writer := &fakeArtifactWriter{}
	implementer := &fakeSession{
		name: "iwr-run-id-implementer",
		role: RoleImplementer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "package demo\nconst Token = \"v1\"\n<promise>done</promise>\n", RawCapture: "package demo\nconst Token = \"v1\"\n<promise>done</promise>\n"}},
		},
	}
	reviewer := &fakeSession{
		name: "iwr-run-id-reviewer",
		role: RoleReviewer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "<promise>APPROVED</promise>\n<promise>done</promise>\n", RawCapture: "<promise>APPROVED</promise>\n<promise>done</promise>\n"}},
		},
	}

	stdout, _, err := runTestRunner(t, writer, nil, implementer, reviewer, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v\nstdout:\n%s", err, stdout)
	}
	if !strings.Contains(stdout, "Final implementation\npackage demo\nconst Token = \"v1\"\n<promise>done</promise>\n") {
		t.Fatalf("final implementation should retain done marker:\n%s", stdout)
	}
}

func TestRunTreatsClarificationAsOrdinaryOutput(t *testing.T) {
	writer := &fakeArtifactWriter{}
	implementer := &fakeSession{
		name: "iwr-run-id-implementer",
		role: RoleImplementer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "draft v1\n<promise>done</promise>\n", RawCapture: "draft v1\n<promise>done</promise>\n"}},
			{result: TurnResult{Output: "draft v2\n<promise>done</promise>\n", RawCapture: "draft v2\n<promise>done</promise>\n"}},
		},
	}
	reviewer := &fakeSession{
		name: "iwr-run-id-reviewer",
		role: RoleReviewer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "I need clarification about edge cases.\n<promise>done</promise>\n", RawCapture: "I need clarification about edge cases.\n<promise>done</promise>\n"}},
			{result: TurnResult{Output: "<promise>APPROVED</promise>\n<promise>done</promise>\n", RawCapture: "<promise>APPROVED</promise>\n<promise>done</promise>\n"}},
		},
	}

	stdout, _, err := runTestRunner(t, writer, nil, implementer, reviewer, func(cfg *RunConfig) {
		cfg.MaxIterations = 2
	})
	if err != nil {
		t.Fatalf("Run returned error: %v\nstdout:\n%s", err, stdout)
	}
	if len(implementer.turnPrompts) != 2 {
		t.Fatalf("expected 2 implementer prompts, got %d", len(implementer.turnPrompts))
	}
	if !strings.Contains(implementer.turnPrompts[1], "Reviewer feedback:\nI need clarification about edge cases.\n<promise>done</promise>\n") {
		t.Fatalf("rewrite prompt should include reviewer clarification output:\n%s", implementer.turnPrompts[1])
	}
}

func TestRunNonConvergence(t *testing.T) {
	writer := &fakeArtifactWriter{}
	implementer := &fakeSession{
		name: "iwr-run-id-implementer",
		role: RoleImplementer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "draft v1\n<promise>done</promise>\n", RawCapture: "draft v1\n<promise>done</promise>\n"}},
			{result: TurnResult{Output: "draft v2\n<promise>done</promise>\n", RawCapture: "draft v2\n<promise>done</promise>\n"}},
			{result: TurnResult{Output: "draft v3\n<promise>done</promise>\n", RawCapture: "draft v3\n<promise>done</promise>\n"}},
		},
	}
	reviewer := &fakeSession{
		name: "iwr-run-id-reviewer",
		role: RoleReviewer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "fix issue 1\n<promise>done</promise>\n", RawCapture: "fix issue 1\n<promise>done</promise>\n"}},
			{result: TurnResult{Output: "fix issue 2\n<promise>done</promise>\n", RawCapture: "fix issue 2\n<promise>done</promise>\n"}},
		},
	}

	stdout, stderr, err := runTestRunner(t, writer, nil, implementer, reviewer, func(cfg *RunConfig) {
		cfg.MaxIterations = 2
	})
	exitErr, ok := AsExitError(err)
	if !ok || exitErr.Code() != 1 {
		t.Fatalf("expected exit code 1, got err=%v", err)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr on non-convergence, got %q", stderr)
	}
	if !strings.Contains(stdout, "Did not converge after 2 iterations.") {
		t.Fatalf("stdout missing non-convergence summary:\n%s", stdout)
	}
	if len(writer.results) != 1 || writer.results[0].Status != resultStatusNonConverged {
		t.Fatalf("unexpected run result: %+v", writer.results)
	}
}

func TestRunWritesStableCaptureNames(t *testing.T) {
	writer := &fakeArtifactWriter{}
	implementer := &fakeSession{
		name: "iwr-run-id-implementer",
		role: RoleImplementer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "draft v1\n<promise>done</promise>\n", RawCapture: "draft v1\n<promise>done</promise>\n"}},
			{result: TurnResult{Output: "draft v2\n<promise>done</promise>\n", RawCapture: "draft v2\n<promise>done</promise>\n"}},
		},
	}
	reviewer := &fakeSession{
		name: "iwr-run-id-reviewer",
		role: RoleReviewer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "use v2\n<promise>done</promise>\n", RawCapture: "use v2\n<promise>done</promise>\n"}},
			{result: TurnResult{Output: "<promise>APPROVED</promise>\n<promise>done</promise>\n", RawCapture: "<promise>APPROVED</promise>\n<promise>done</promise>\n"}},
		},
	}

	_, _, err := runTestRunner(t, writer, nil, implementer, reviewer, func(cfg *RunConfig) {
		cfg.MaxIterations = 2
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	var names []string
	for name := range writer.captures {
		names = append(names, name)
	}
	sort.Strings(names)
	expected := []string{
		"iter-0-implementer.txt",
		"iter-1-implementer.txt",
		"iter-1-reviewer.txt",
		"iter-2-reviewer.txt",
	}
	if strings.Join(names, ",") != strings.Join(expected, ",") {
		t.Fatalf("unexpected capture names: got=%v want=%v", names, expected)
	}
}

func TestRunWritesPerRoleSessionMetadata(t *testing.T) {
	writer := &fakeArtifactWriter{}
	implementer := &fakeSession{
		name: "iwr-run-id-implementer",
		role: RoleImplementer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "impl\n<promise>done</promise>\n", RawCapture: "impl\n<promise>done</promise>\n"}},
		},
	}
	reviewer := &fakeSession{
		name: "iwr-run-id-reviewer",
		role: RoleReviewer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "<promise>APPROVED</promise>\n<promise>done</promise>\n", RawCapture: "<promise>APPROVED</promise>\n<promise>done</promise>\n"}},
		},
	}

	_, _, err := runTestRunner(t, writer, nil, implementer, reviewer, nil)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(writer.metadata) != 1 {
		t.Fatalf("expected one metadata write, got %d", len(writer.metadata))
	}
	metadata := writer.metadata[0]
	if metadata.Sessions[RoleImplementer].TmuxSessionName != "iwr-run-id-implementer" {
		t.Fatalf("unexpected implementer session metadata: %+v", metadata.Sessions[RoleImplementer])
	}
	if metadata.Sessions[RoleReviewer].TmuxSessionName != "iwr-run-id-reviewer" {
		t.Fatalf("unexpected reviewer session metadata: %+v", metadata.Sessions[RoleReviewer])
	}
}

func TestRunFailsWhenArtifactWriterFails(t *testing.T) {
	writer := &fakeArtifactWriter{writeResultErr: errors.New("disk full")}
	implementer := &fakeSession{
		name: "iwr-run-id-implementer",
		role: RoleImplementer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "impl\n<promise>done</promise>\n", RawCapture: "impl\n<promise>done</promise>\n"}},
		},
	}
	reviewer := &fakeSession{
		name: "iwr-run-id-reviewer",
		role: RoleReviewer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "<promise>APPROVED</promise>\n<promise>done</promise>\n", RawCapture: "<promise>APPROVED</promise>\n<promise>done</promise>\n"}},
		},
	}

	stdout, stderr, err := runTestRunner(t, writer, nil, implementer, reviewer, nil)
	exitErr, ok := AsExitError(err)
	if !ok || exitErr.Code() != 1 {
		t.Fatalf("expected exit code 1, got err=%v", err)
	}
	if !strings.Contains(stderr, "disk full") {
		t.Fatalf("stderr missing artifact failure:\n%s", stderr)
	}
	if strings.Contains(stdout, "Approved after 1 review round(s).") {
		t.Fatalf("approval summary should be suppressed when result persistence fails:\n%s", stdout)
	}
}

func TestRunFailsWhenCleanupFailsAfterApproval(t *testing.T) {
	writer := &fakeArtifactWriter{}
	implementer := &fakeSession{
		name:     "iwr-run-id-implementer",
		role:     RoleImplementer,
		closeErr: agentsession.NewRunnerError(agentsession.RunnerErrorKindClose, "iwr-run-id-implementer", "", errors.New("kill-session failed")),
		turns: []scriptedTurn{
			{result: TurnResult{Output: "impl\n<promise>done</promise>\n", RawCapture: "impl\n<promise>done</promise>\n"}},
		},
	}
	reviewer := &fakeSession{
		name: "iwr-run-id-reviewer",
		role: RoleReviewer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "<promise>APPROVED</promise>\n<promise>done</promise>\n", RawCapture: "<promise>APPROVED</promise>\n<promise>done</promise>\n"}},
		},
	}

	stdout, stderr, err := runTestRunner(t, writer, nil, implementer, reviewer, nil)
	exitErr, ok := AsExitError(err)
	if !ok || exitErr.Code() != 1 {
		t.Fatalf("expected exit code 1, got err=%v", err)
	}
	if !strings.Contains(stderr, "implementer cleanup failed") {
		t.Fatalf("stderr missing cleanup failure:\n%s", stderr)
	}
	if strings.Contains(stdout, "Approved after 1 review round(s).") {
		t.Fatalf("approval summary should be suppressed when cleanup fails:\n%s", stdout)
	}
	if len(writer.results) != 1 || writer.results[0].Status != resultStatusFailed {
		t.Fatalf("expected failed result after cleanup failure, got %+v", writer.results)
	}
	if hasTransition(writer.transitions, "closed", RoleImplementer) {
		t.Fatalf("implementer should not be marked closed after cleanup failure: %+v", writer.transitions)
	}
	if !hasTransition(writer.transitions, "cleanup_failed", RoleImplementer) {
		t.Fatalf("missing cleanup_failed transition for implementer: %+v", writer.transitions)
	}
	if !hasTransition(writer.transitions, "closed", RoleReviewer) {
		t.Fatalf("reviewer should still be marked closed after successful cleanup: %+v", writer.transitions)
	}
}

func TestRunWritesTimeoutCaptureAndFails(t *testing.T) {
	writer := &fakeArtifactWriter{}
	implementer := &fakeSession{
		name: "iwr-run-id-implementer",
		role: RoleImplementer,
		turns: []scriptedTurn{
			{result: TurnResult{Output: "impl\n<promise>done</promise>\n", RawCapture: "impl\n<promise>done</promise>\n"}},
		},
	}
	reviewer := &fakeSession{
		name: "iwr-run-id-reviewer",
		role: RoleReviewer,
		turns: []scriptedTurn{
			{err: agentsession.NewRunnerError(agentsession.RunnerErrorKindTimeout, "iwr-run-id-reviewer", "reviewer-stalled\n", errors.New("idle timeout"))},
		},
	}

	_, stderr, err := runTestRunner(t, writer, nil, implementer, reviewer, nil)
	exitErr, ok := AsExitError(err)
	if !ok || exitErr.Code() != 1 {
		t.Fatalf("expected exit code 1, got err=%v", err)
	}
	if !strings.Contains(stderr, "reviewer turn failed") {
		t.Fatalf("stderr missing timeout failure:\n%s", stderr)
	}
	if got := writer.captures["iter-1-reviewer-timeout.txt"]; got != "reviewer-stalled\n" {
		t.Fatalf("unexpected timeout capture: %q", got)
	}
	if len(writer.results) != 1 || writer.results[0].Status != resultStatusFailed {
		t.Fatalf("expected failed result, got %+v", writer.results)
	}
}

func runTestRunner(t *testing.T, writer *fakeArtifactWriter, newManager func(ChannelConfig) (ChannelManager, error), implementer *fakeSession, reviewer *fakeSession, mutate func(*RunConfig)) (string, string, error) {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if newManager == nil {
		newManager = func(ChannelConfig) (ChannelManager, error) {
			return &fakeCommunicationChannelManager{}, nil
		}
	}
	cfg := RunConfig{
		Task:          "Create a Go snippet that defines package demo and constant HarnessIntegrationToken. Return code only.",
		Implementer:   "codex",
		Reviewer:      "claude",
		MaxIterations: 1,
		IdleTimeout:   time.Second,
		Stdout:        &stdout,
		Stderr:        &stderr,
		NewRunID: func() (string, error) {
			return "run-id", nil
		},
		NewArtifactWriter: func(string) (ArtifactSink, error) {
			return writer, nil
		},
		NewCommunicationChannelManager: newManager,
		NewSession: func(name string, opts SessionOptions) (Session, error) {
			switch opts.Role {
			case RoleImplementer:
				return implementer, nil
			case RoleReviewer:
				return reviewer, nil
			default:
				return nil, errors.New("unexpected role")
			}
		},
	}
	if mutate != nil {
		mutate(&cfg)
	}
	err := Run(context.Background(), cfg)
	return stdout.String(), stderr.String(), err
}

func eventIndex(events []string, target string) int {
	for i, event := range events {
		if event == target {
			return i
		}
	}
	return -1
}

func countEvent(events []string, target string) int {
	count := 0
	for _, event := range events {
		if event == target {
			count++
		}
	}
	return count
}

func hasTransition(transitions []StateTransition, state string, role string) bool {
	for _, transition := range transitions {
		if transition.State == state && transition.Role == role {
			return true
		}
	}
	return false
}

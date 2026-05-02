package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

type runFakeAgent struct {
	name     string
	response string
	prompts  []string
}

func (a *runFakeAgent) Start() error          { return nil }
func (a *runFakeAgent) WaitUntilReady() error { return nil }
func (a *runFakeAgent) SendPrompt(prompt string) error {
	a.prompts = append(a.prompts, prompt)
	return nil
}
func (a *runFakeAgent) Capture() (string, error) {
	if len(a.prompts) == 0 {
		return "", nil
	}
	return a.prompts[len(a.prompts)-1] + "\n" + a.response + "\n<promise>done</promise>\n", nil
}
func (a *runFakeAgent) SessionName() string { return a.name }
func (a *runFakeAgent) Close() error        { return nil }

type runFakeArtifacts struct {
	results []runResult
}

func (a *runFakeArtifacts) WriteMetadata(runMetadata) error             { return nil }
func (a *runFakeArtifacts) AppendTransition(stateTransition) error      { return nil }
func (a *runFakeArtifacts) AppendChannelEvent(channelEvent) error       { return nil }
func (a *runFakeArtifacts) WriteCapture(name string, text string) error { return nil }
func (a *runFakeArtifacts) WriteResult(result runResult) error {
	a.results = append(a.results, result)
	return nil
}

type noopChannelManager struct{}

func (noopChannelManager) Messages() <-chan channelMessage { return closedChannel[channelMessage]() }
func (noopChannelManager) Errors() <-chan error            { return closedChannel[error]() }
func (noopChannelManager) Stop() error                     { return nil }
func (noopChannelManager) Remove() error                   { return nil }

func closedChannel[T any]() <-chan T {
	ch := make(chan T)
	close(ch)
	return ch
}

func TestRunUsesConcreteBackendFactories(t *testing.T) {
	var stdout bytes.Buffer
	artifacts := &runFakeArtifacts{}
	implementer := &runFakeAgent{name: "impl", response: "implementation"}
	reviewer := &runFakeAgent{name: "rev", response: "<promise>APPROVED</promise>"}
	var constructed []string

	cfg := runConfig{
		Task:          "task",
		Implementer:   "codex",
		Reviewer:      "claude",
		MaxIterations: 1,
		IdleTimeout:   time.Second,
		Stdout:        &stdout,
		Stderr:        io.Discard,
		NewAgent: func(backend string, sessionName string) (workflowAgent, error) {
			constructed = append(constructed, backend+":"+sessionName)
			if backend == "codex" {
				return implementer, nil
			}
			return reviewer, nil
		},
		NewArtifactWriter: func(string) (artifactSink, error) { return artifacts, nil },
		NewCommunicationChannelManager: func(channelConfig) (channelManager, error) {
			return noopChannelManager{}, nil
		},
		NewRunID: func() (string, error) { return "run-id", nil },
	}
	err := runWorkflow(context.Background(), cfg)
	if err != nil {
		t.Fatalf("runWorkflow: %v", err)
	}
	if len(constructed) != 2 || !strings.Contains(constructed[0], "codex:iwr-run-id-implementer") || !strings.Contains(constructed[1], "claude:iwr-run-id-reviewer") {
		t.Fatalf("unexpected constructed agents: %#v", constructed)
	}
	if len(artifacts.results) != 1 || !artifacts.results[0].Approved {
		t.Fatalf("expected approved result: %#v", artifacts.results)
	}
}

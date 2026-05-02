SHELL := /bin/bash
ROOT := $(shell dirname "$(realpath $(lastword $(MAKEFILE_LIST)))")
PATH_EXPORT := export PATH="$(ROOT)/bin:$$PATH"

.PHONY: setup build source_zshrc source_bashrc

setup:
	ln -sfn "$(ROOT)/scripts/.agentrc" "$$HOME/.agentrc"

build:
	cd orchestrator && go build -o ../bin/tmux_codex ./cmd/tmux_codex
	cd orchestrator && go build -o ../bin/tmux_claude ./cmd/tmux_claude
	cd orchestrator && go build -o ../bin/implement-with-reviewer ./cmd/implement-with-reviewer

clean:
	rm -rf "$(ROOT)/bin"/*

source_zshrc:
	@touch ~/.zshrc
	@grep -Fqx '$(PATH_EXPORT)' ~/.zshrc || echo '$(PATH_EXPORT)' >> ~/.zshrc
	@echo 'PATH entry ensured in ~/.zshrc; run: source ~/.zshrc'

source_bashrc:
	@touch ~/.bashrc
	@grep -Fqx '$(PATH_EXPORT)' ~/.bashrc || echo '$(PATH_EXPORT)' >> ~/.bashrc
	@echo 'PATH entry ensured in ~/.bashrc; run: source ~/.bashrc'

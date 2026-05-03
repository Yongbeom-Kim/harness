SHELL := /bin/bash
ROOT := $(shell dirname "$(realpath $(lastword $(MAKEFILE_LIST)))")
PATH_EXPORT := export PATH="$(ROOT)/bin:$$PATH"

.PHONY: setup build source_zshrc source_bashrc

setup:
	ln -sfn "$(ROOT)/scripts/.agentrc" "$$HOME/.agentrc"
	@if [ -e "$$HOME/.agent-bin" ] && [ ! -L "$$HOME/.agent-bin" ]; then \
		echo '$$HOME/.agent-bin already exists and is not a symlink; move it aside or replace it manually' >&2; \
		exit 1; \
	fi
	ln -sfn "$(ROOT)/scripts/bin" "$$HOME/.agent-bin"

build:
	@mkdir -p "$(ROOT)/bin"
	rm -f "$(ROOT)/bin"/*
	cd orchestrator && go build -o ../bin/tmux_codex ./cmd/tmux_codex
	cd orchestrator && go build -o ../bin/tmux_claude ./cmd/tmux_claude
	cd orchestrator && go build -o ../bin/tmux_cursor ./cmd/tmux_cursor
	cd orchestrator && go build -o ../bin/implement-with-reviewer ./cmd/implement-with-reviewer
	@rmdir "$(ROOT)/orchestrator/cmd/tmux_agent" 2>/dev/null || true

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

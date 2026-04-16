SHELL := /bin/bash
ROOT := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))
PATH_EXPORT := export PATH="$(ROOT)bin:$$PATH"

.PHONY: setup build source_zshrc source_bashrc

setup:
	ln -sfn "$(ROOT)scripts/.agentrc" "$$HOME/.agentrc"

build:
	cd orchestrator && go build -o ../bin/implement-with-reviewer cmd/implement-with-reviewer/main.go

source_zshrc:
	@touch ~/.zshrc
	@grep -Fqx '$(PATH_EXPORT)' ~/.zshrc || echo '$(PATH_EXPORT)' >> ~/.zshrc
	@echo 'PATH entry ensured in ~/.zshrc; run: source ~/.zshrc'

source_bashrc:
	@touch ~/.bashrc
	@grep -Fqx '$(PATH_EXPORT)' ~/.bashrc || echo '$(PATH_EXPORT)' >> ~/.bashrc
	@echo 'PATH entry ensured in ~/.bashrc; run: source ~/.bashrc'

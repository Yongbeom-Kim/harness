SHELL := /bin/bash
ROOT := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

.PHONY: setup build

setup:
	ln -sfn "$(ROOT)scripts/.agentrc" "$$HOME/.agentrc"

build:
	cd orchestrator && go build -o ../bin/implement-with-reviewer cmd/implement-with-reviewer/main.go
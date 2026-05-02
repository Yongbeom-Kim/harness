package session

import sessionprotocol "github.com/Yongbeom-Kim/harness/orchestrator/internal/agent/session/driver/protocol"

type PreparedTurn = sessionprotocol.PreparedTurn

type Protocol = sessionprotocol.Protocol

func NewPromiseDoneProtocol() Protocol {
	return sessionprotocol.NewPromiseDoneProtocol()
}

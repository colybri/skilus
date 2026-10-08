package app

import (
	"context"
	"fmt"

	"github.com/colybri/skilus/internal/domain/agent"
)

// ListAgents is the query behind `skilus agents`.
type ListAgents struct {
	Catalog  AgentCatalog
	Detector AgentDetector
}

// AgentStatus pairs an agent with whether it was found on this machine.
type AgentStatus struct {
	Agent     agent.Agent
	Installed bool
}

// Handle returns every catalog agent in catalog order.
func (q ListAgents) Handle(ctx context.Context) ([]AgentStatus, error) {
	agents, err := q.Catalog.Agents(ctx)
	if err != nil {
		return nil, fmt.Errorf("load agent catalog: %w", err)
	}
	out := make([]AgentStatus, 0, len(agents))
	for _, a := range agents {
		ok, err := q.Detector.Installed(ctx, a)
		if err != nil {
			return nil, fmt.Errorf("detect agent %s: %w", a.ID(), err)
		}
		out = append(out, AgentStatus{Agent: a, Installed: ok})
	}
	return out, nil
}

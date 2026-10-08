// Package app holds skilus's use cases and the ports they need. It depends
// on the domain only; adapters implement the ports and cli drives the use
// cases.
package app

import (
	"context"

	"github.com/colybri/skilus/internal/domain/agent"
)

// AgentCatalog lists the agents skilus knows about, with paths resolved for
// this machine.
type AgentCatalog interface {
	Agents(ctx context.Context) ([]agent.Agent, error)
}

// AgentDetector reports whether an agent is installed on this machine.
type AgentDetector interface {
	Installed(ctx context.Context, a agent.Agent) (bool, error)
}

// Ports still to come in phase 1, as the use cases that need them land:
// Fetcher, Store, LockRepository, ManifestRepository, Deployer, Prompter
// and Clock. They are added with their first use case, not ahead of it.

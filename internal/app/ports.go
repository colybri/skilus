// Package app holds skilus's use cases and the ports they need. It depends
// on the domain only; adapters implement the ports and cli drives the use
// cases.
package app

import (
	"context"

	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/skill"
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

// Fetcher reads the skills available at a source. Phase 1 starts with
// local directories; Git sources implement the same port.
type Fetcher interface {
	Fetch(ctx context.Context, source string) (Fetched, error)
}

// Fetched is what a Fetcher found at a source.
type Fetched struct {
	Source    string // normalized source, recorded in the lock
	Requested string // ref the user asked for; empty for local sources
	Commit    string // resolved commit; empty for local sources
	Skills    []FetchedSkill
}

// FetchedSkill is one skill inside a source.
type FetchedSkill struct {
	Path    string // directory of the skill inside the source, "." for the root
	Package skill.Package
}

// Store keeps skill content addressed by its tree hash and returns the
// directory that holds it. Putting the same package twice is a no-op.
type Store interface {
	Put(ctx context.Context, p skill.Package) (dir string, err error)
}

// Deployer places stored content into an agent's skills directory.
type Deployer interface {
	// Deploy creates dest from the stored directory. It fails with
	// domain.ErrConflict if dest already exists.
	Deploy(ctx context.Context, storeDir, dest string, mode agent.Mode) error
	// Remove deletes a destination created by Deploy.
	Remove(ctx context.Context, dest string) error
}

// LockRepository loads and saves the lockfile of a scope.
type LockRepository interface {
	Load(ctx context.Context, scope agent.Scope) (*lock.Lockfile, error)
	Save(ctx context.Context, scope agent.Scope, l *lock.Lockfile) error
}

// ManifestRepository records the user's intent in skilus.yaml.
type ManifestRepository interface {
	AddSkill(ctx context.Context, scope agent.Scope, e ManifestEntry) error
}

// ManifestEntry is one skill line in skilus.yaml.
type ManifestEntry struct {
	Name   skill.Name
	Source string
	Allow  []string
}

// Prompter shows the install plan to the user and asks for confirmation.
type Prompter interface {
	ConfirmInstall(ctx context.Context, plan InstallPlan) (bool, error)
}

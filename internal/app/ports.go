// Package app holds skilus's use cases and the ports they need. It depends
// on the domain only; adapters implement the ports and cli drives the use
// cases.
package app

import (
	"context"

	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/profile"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
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

// Fetcher reads the skills available at a source. There is one per
// source.Kind: local directories and Git repositories.
type Fetcher interface {
	Fetch(ctx context.Context, src source.Source) (Fetched, error)
}

// RefResolver finds the commit a ref of a Git source points to now,
// without downloading content. An empty ref means the default branch.
type RefResolver interface {
	Resolve(ctx context.Context, src source.Source) (commit string, err error)
}

// Fetched is what a Fetcher found at a source.
type Fetched struct {
	Source    string // normalized source, recorded in the lock
	Requested string // ref the user asked for; empty for the default branch and local sources
	Commit    string // resolved commit; empty for local sources
	Skills    []FetchedSkill
	// Invalid lists skill directories that could not be read, so one
	// broken skill does not hide the rest of a collection.
	Invalid []InvalidSkill
}

// InvalidSkill is a skill directory a Fetcher had to skip.
type InvalidSkill struct {
	Path string
	Err  error
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
	// Lookup returns the directory holding h, if the store has it. The
	// content is not checked; read it back to trust it.
	Lookup(ctx context.Context, h skill.TreeHash) (dir string, ok bool, err error)
	// Discard deletes the content stored under h, e.g. after finding it
	// damaged. A missing entry is not an error.
	Discard(ctx context.Context, h skill.TreeHash) error
}

// TreeReader reads back installed skill directories to check them.
type TreeReader interface {
	// ReadTree returns the files under dir, following dir itself when it
	// is a symlink to the store. It fails with domain.ErrNotFound when dir
	// does not exist.
	ReadTree(ctx context.Context, dir string) ([]skill.File, error)
	// ExecutableBits reports whether the file system keeps the executable
	// bit. When it does not (Windows), the lock's list is trusted instead.
	ExecutableBits() bool
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
	// RemoveSkill drops the entry for name; a missing entry is not an error.
	RemoveSkill(ctx context.Context, scope agent.Scope, name skill.Name) error
}

// ManifestReader reads what a scope's skilus.yaml declares.
type ManifestReader interface {
	// Manifest returns an empty Manifest when the file does not exist.
	Manifest(ctx context.Context, scope agent.Scope) (Manifest, error)
}

// Manifest is the part of skilus.yaml profiles need: the declared skills,
// with their sources, and the profiles, in file order.
type Manifest struct {
	Skills   []ManifestEntry
	Profiles []profile.Profile
	// Indexes are the URLs of curated skill indexes skilus search reads.
	Indexes []string
}

// Registry searches a public skill directory such as skills.sh.
type Registry interface {
	// Name identifies the registry in results, e.g. "skills.sh".
	Name() string
	Search(ctx context.Context, query, owner string, limit int) ([]Listing, error)
}

// IndexReader downloads a curated index: a file listing skills and their
// sources.
type IndexReader interface {
	Index(ctx context.Context, url string) ([]Listing, error)
}

// Listing is a skill a registry or an index points to. Everything in it
// comes from a third party: print it with care.
type Listing struct {
	Name        string
	Source      string // what skilus add takes, e.g. owner/repo or owner/repo@v1
	Description string
	Installs    int // 0 when the origin does not count them
}

// TrustList reads the trust: list of a scope's skilus.yaml.
type TrustList interface {
	Trust(ctx context.Context, scope agent.Scope) (policy.Trust, error)
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
	ConfirmUpdate(ctx context.Context, plan UpdatePlan) (bool, error)
	ConfirmProfile(ctx context.Context, plan ProfilePlan) (bool, error)
}

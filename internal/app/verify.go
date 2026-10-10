package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/skill"
)

// ErrDrift means installed content no longer matches the lock.
var ErrDrift = errors.New("installed skills differ from the lock")

// Verify is the query behind `skilus verify`.
type Verify struct {
	Scopes []agent.Scope
}

// Problem says what is wrong with an installed skill.
type Problem string

// Problems found by Verify. An empty Problem means the target is intact.
const (
	ProblemMissing    Problem = "missing"
	ProblemModified   Problem = "modified"
	ProblemUnreadable Problem = "unreadable"
)

// TargetCheck is the state of one skill in one agent.
type TargetCheck struct {
	Scope   agent.Scope
	Skill   skill.Name
	Target  agent.Target
	Dir     string // the skill's directory in the agent
	Problem Problem
	Detail  string // why it could not be read
	// Changes lists the files that differ, when the store still holds the
	// expected content to compare with.
	Changes []skill.Change
}

// ManifestProblem says what is wrong with a scope's skilus.yaml.
type ManifestProblem string

// Problems Verify finds in skilus.yaml.
const (
	// ManifestInvalid: the file cannot be read; Detail says why.
	ManifestInvalid ManifestProblem = "manifest-invalid"
	// NotInstalled: skills: declares a skill the lock does not hold.
	NotInstalled ManifestProblem = "not-installed"
	// NotDeclared: the lock holds a skill skills: does not declare.
	NotDeclared ManifestProblem = "not-declared"
	// ProfileUndeclared: a profile uses a skill skills: does not declare.
	ProfileUndeclared ManifestProblem = "profile-undeclared"
)

// ManifestCheck is one problem found in skilus.yaml.
type ManifestCheck struct {
	Scope   agent.Scope
	Problem ManifestProblem
	Skill   skill.Name // empty for ManifestInvalid
	Source  string     // for NotInstalled, where skills: says it comes from
	Profile string     // for ProfileUndeclared
	Detail  string
}

// VerifyResult lists every checked target, in lock order, and the problems
// found in skilus.yaml.
type VerifyResult struct {
	Checks   []TargetCheck
	Manifest []ManifestCheck
}

// ErrInvalidManifest means a skilus.yaml that Verify read is broken: it
// cannot be read, or a profile uses a skill skills: does not declare.
var ErrInvalidManifest = fmt.Errorf("skilus.yaml has errors: %w", domain.ErrInvalid)

// Problems returns the checks that failed.
func (r VerifyResult) Problems() []TargetCheck {
	var out []TargetCheck
	for _, c := range r.Checks {
		if c.Problem != "" {
			out = append(out, c)
		}
	}
	return out
}

// VerifyHandler runs Verify. It only reads.
type VerifyHandler struct {
	Catalog     AgentCatalog
	Store       Store
	Trees       TreeReader
	Locks       LockRepository
	Manifest    ManifestReader
	ProjectRoot string
}

// Handle checks every target of every skill in the scopes' locks, and
// that each scope's skilus.yaml can be read and agrees with its lock. It
// returns the full result together with ErrInvalidManifest when a
// skilus.yaml is broken, or else ErrDrift when any other check fails.
func (h VerifyHandler) Handle(ctx context.Context, q Verify) (VerifyResult, error) {
	byID, err := agentsByID(ctx, h.Catalog)
	if err != nil {
		return VerifyResult{}, err
	}
	var res VerifyResult
	for _, scope := range q.Scopes {
		lf, err := h.Locks.Load(ctx, scope)
		if err != nil {
			return VerifyResult{}, fmt.Errorf("load %s lock: %w", scope, err)
		}
		for _, e := range lf.Entries() {
			var expected []skill.File // read from the store on first need
			for _, t := range e.Targets {
				c := TargetCheck{Scope: scope, Skill: e.Skill, Target: t}
				a, ok := byID[t.Agent]
				if !ok {
					c.Problem, c.Detail = ProblemUnreadable, fmt.Sprintf("agent %s is no longer in the catalog", t.Agent)
					res.Checks = append(res.Checks, c)
					continue
				}
				c.Dir = filepath.Join(skillsDir(a, t.Scope, h.ProjectRoot), e.Skill.String())
				files, got, err := readInstalled(ctx, h.Trees, c.Dir, e)
				switch {
				case errors.Is(err, domain.ErrNotFound):
					c.Problem = ProblemMissing
				case err != nil:
					c.Problem, c.Detail = ProblemUnreadable, err.Error()
				case got != e.TreeHash:
					c.Problem = ProblemModified
					if expected == nil {
						expected = h.stored(ctx, e)
					}
					if expected != nil {
						c.Changes = skill.Diff(expected, files)
					}
				}
				res.Checks = append(res.Checks, c)
			}
		}
		res.Manifest = append(res.Manifest, h.checkManifest(ctx, scope, lf)...)
	}
	for _, c := range res.Manifest {
		if c.Problem == ManifestInvalid || c.Problem == ProfileUndeclared {
			return res, ErrInvalidManifest
		}
	}
	if len(res.Problems()) > 0 || len(res.Manifest) > 0 {
		return res, ErrDrift
	}
	return res, nil
}

// checkManifest compares a scope's skilus.yaml with its lock. When a
// profile is active the lock holds only that profile's skills, so the
// other declared skills are not missing.
func (h VerifyHandler) checkManifest(ctx context.Context, scope agent.Scope, lf *lock.Lockfile) []ManifestCheck {
	m, err := h.Manifest.Manifest(ctx, scope)
	if err != nil {
		return []ManifestCheck{{Scope: scope, Problem: ManifestInvalid, Detail: err.Error()}}
	}
	var out []ManifestCheck
	for _, p := range m.Profiles {
		for _, n := range undeclared(m, p) {
			out = append(out, ManifestCheck{Scope: scope, Problem: ProfileUndeclared, Skill: n, Profile: p.Name()})
		}
	}
	declared := map[skill.Name]bool{}
	for _, e := range m.Skills {
		declared[e.Name] = true
	}
	active := false
	for _, p := range m.Profiles {
		active = active || isActive(p, lf)
	}
	if !active {
		for _, e := range m.Skills {
			if _, ok := lf.Entry(e.Name); !ok {
				out = append(out, ManifestCheck{Scope: scope, Problem: NotInstalled, Skill: e.Name, Source: e.Source})
			}
		}
	}
	for _, e := range lf.Entries() {
		if !e.Dependency && !declared[e.Skill] {
			out = append(out, ManifestCheck{Scope: scope, Problem: NotDeclared, Skill: e.Skill})
		}
	}
	return out
}

// stored returns the entry's content from the store, or nil when the store
// does not hold it intact.
func (h VerifyHandler) stored(ctx context.Context, e lock.Entry) []skill.File {
	dir, ok, err := h.Store.Lookup(ctx, e.TreeHash)
	if err != nil || !ok {
		return nil
	}
	files, got, err := readInstalled(ctx, h.Trees, dir, e)
	if err != nil || got != e.TreeHash {
		return nil
	}
	return files
}

// readInstalled reads a directory back and hashes it, restoring the
// executable bits from the lock where the file system cannot keep them.
func readInstalled(ctx context.Context, r TreeReader, dir string, e lock.Entry) ([]skill.File, skill.TreeHash, error) {
	files, err := r.ReadTree(ctx, dir)
	if err != nil {
		return nil, skill.TreeHash{}, err
	}
	if !r.ExecutableBits() {
		files = skill.MarkExecutable(files, e.Executables)
	}
	h, err := skill.HashFiles(files)
	if err != nil {
		return nil, skill.TreeHash{}, err
	}
	return files, h, nil
}

func agentsByID(ctx context.Context, c AgentCatalog) (map[agent.ID]agent.Agent, error) {
	agents, err := c.Agents(ctx)
	if err != nil {
		return nil, fmt.Errorf("load agent catalog: %w", err)
	}
	byID := make(map[agent.ID]agent.Agent, len(agents))
	for _, a := range agents {
		byID[a.ID()] = a
	}
	return byID, nil
}

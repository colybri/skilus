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

// VerifyResult lists every checked target, in lock order.
type VerifyResult struct {
	Checks []TargetCheck
}

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
	ProjectRoot string
}

// Handle checks every target of every skill in the scopes' locks. It
// returns ErrDrift, together with the full result, when any check fails.
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
	}
	if len(res.Problems()) > 0 {
		return res, ErrDrift
	}
	return res, nil
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

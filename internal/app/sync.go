package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/source"
)

// Sync is the command behind `skilus sync`.
type Sync struct {
	Scopes []agent.Scope
	// Force replaces installed skills whose content differs from the lock.
	Force bool
}

// SyncAction is what Sync did, or would do, with one target.
type SyncAction string

// Sync actions.
const (
	SyncUnchanged SyncAction = "unchanged"
	SyncCreated   SyncAction = "created"
	SyncReplaced  SyncAction = "replaced"
	// SyncBlocked marks a modified target that --force would replace.
	SyncBlocked SyncAction = "blocked"
)

// SyncedTarget is one skill in one agent.
type SyncedTarget struct {
	Scope  agent.Scope
	Entry  lock.Entry
	Target agent.Target
	Dir    string
	Action SyncAction
}

// SyncResult reports every target, in lock order, and which skills had to
// be downloaded because the store did not hold them.
type SyncResult struct {
	Targets    []SyncedTarget
	Downloaded []lock.Entry
}

// SyncHandler runs Sync.
type SyncHandler struct {
	Catalog     AgentCatalog
	Fetchers    map[source.Kind]Fetcher
	Store       Store
	Deployer    Deployer
	Trees       TreeReader
	Locks       LockRepository
	ProjectRoot string
}

// Handle makes the agents' directories match the lock. It never edits the
// lock or skilus.yaml, and it changes nothing in the agents unless every
// skill could be obtained with its locked hash and no target is blocked.
func (h SyncHandler) Handle(ctx context.Context, cmd Sync) (SyncResult, error) {
	byID, err := agentsByID(ctx, h.Catalog)
	if err != nil {
		return SyncResult{}, err
	}

	var res SyncResult
	storeDirs := map[int]string{}   // index in res.Targets -> stored content
	sources := map[string]Fetched{} // one download per source and commit
	for _, scope := range cmd.Scopes {
		lf, err := h.Locks.Load(ctx, scope)
		if err != nil {
			return SyncResult{}, fmt.Errorf("load %s lock: %w", scope, err)
		}
		for _, e := range lf.Entries() {
			storeDir, downloaded, err := h.content(ctx, e, sources)
			if err != nil {
				return res, fmt.Errorf("skill %s: %w", e.Skill, err)
			}
			if downloaded {
				res.Downloaded = append(res.Downloaded, e)
			}
			for _, t := range e.Targets {
				a, ok := byID[t.Agent]
				if !ok {
					return res, fmt.Errorf("skill %s targets agent %s, which is no longer in the catalog: %w", e.Skill, t.Agent, domain.ErrConflict)
				}
				st := SyncedTarget{Scope: scope, Entry: e, Target: t, Dir: filepath.Join(skillsDir(a, t.Scope, h.ProjectRoot), e.Skill.String())}
				_, got, err := readInstalled(ctx, h.Trees, st.Dir, e)
				switch {
				case errors.Is(err, domain.ErrNotFound):
					st.Action = SyncCreated
				case err == nil && got == e.TreeHash:
					st.Action = SyncUnchanged
				case cmd.Force:
					st.Action = SyncReplaced
				default:
					st.Action = SyncBlocked
				}
				storeDirs[len(res.Targets)] = storeDir
				res.Targets = append(res.Targets, st)
			}
		}
	}

	var blocked []string
	for _, t := range res.Targets {
		if t.Action == SyncBlocked {
			blocked = append(blocked, t.Dir)
		}
	}
	if len(blocked) > 0 {
		return res, fmt.Errorf("modified by hand, use --force to replace: %s: %w", strings.Join(blocked, ", "), domain.ErrConflict)
	}

	for i, t := range res.Targets {
		switch t.Action {
		case SyncReplaced:
			if err := h.Deployer.Remove(ctx, t.Dir); err != nil {
				return res, fmt.Errorf("remove %s: %w", t.Dir, err)
			}
			fallthrough
		case SyncCreated:
			if err := h.Deployer.Deploy(ctx, storeDirs[i], t.Dir, t.Target.Mode); err != nil {
				return res, fmt.Errorf("deploy %s to %s: %w", t.Entry.Skill, t.Dir, err)
			}
		}
	}
	return res, nil
}

// content returns the store directory holding the entry's locked content,
// downloading it by its commit when the store lacks it or holds it damaged.
// Downloads are kept in sources so skills of one repository share them.
func (h SyncHandler) content(ctx context.Context, e lock.Entry, sources map[string]Fetched) (dir string, downloaded bool, err error) {
	dir, ok, err := h.Store.Lookup(ctx, e.TreeHash)
	if err != nil {
		return "", false, err
	}
	if ok {
		_, got, err := readInstalled(ctx, h.Trees, dir, e)
		if err == nil && got == e.TreeHash {
			return dir, false, nil
		}
		if err := h.Store.Discard(ctx, e.TreeHash); err != nil {
			return "", false, fmt.Errorf("discard damaged store entry: %w", err)
		}
	}

	src, err := source.Parse(e.Source)
	if err != nil {
		return "", false, err
	}
	if e.Commit != "" {
		src.Ref = e.Commit
	}
	fetched, ok := sources[src.String()]
	if !ok {
		fetcher, ok := h.Fetchers[src.Kind]
		if !ok {
			return "", false, fmt.Errorf("%s sources are not supported yet: %w", src.Kind, domain.ErrInvalid)
		}
		fetched, err = fetcher.Fetch(ctx, src)
		if err != nil {
			return "", false, fmt.Errorf("read source %s: %w", e.Source, err)
		}
		sources[src.String()] = fetched
	}
	for _, s := range fetched.Skills {
		if s.Path != e.Path {
			continue
		}
		if got := s.Package.TreeHash(); got != e.TreeHash {
			return "", false, fmt.Errorf("%s has content %s, but the lock expects %s: %w", e.Source, got.Short(), e.TreeHash.Short(), domain.ErrConflict)
		}
		dir, err := h.Store.Put(ctx, s.Package)
		if err != nil {
			return "", false, fmt.Errorf("store: %w", err)
		}
		return dir, true, nil
	}
	for _, bad := range fetched.Invalid {
		if bad.Path == e.Path {
			return "", false, fmt.Errorf("skill in %s: %w", bad.Path, bad.Err)
		}
	}
	return "", false, fmt.Errorf("%s has no skill in %s: %w", e.Source, e.Path, domain.ErrNotFound)
}

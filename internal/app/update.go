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
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

// Update is the command behind `skilus update`.
type Update struct {
	Names        []string // skills to update; empty means every skill in the scope
	Scope        agent.Scope
	Yes          bool // do not ask; new executables still need AllowScripts
	Strict       bool // treat warnings as blocking
	AllowScripts bool
	Force        bool // replace installed copies modified by hand
}

// UpdatePlan is what the user confirms.
type UpdatePlan struct {
	Skills []PlannedUpdate
	// Current lists the skills whose ref still points to the locked commit.
	Current []lock.Entry
}

// PlannedUpdate is one skill moving to a new commit.
type PlannedUpdate struct {
	Previous lock.Entry
	Commit   string // new commit; empty for local sources
	Package  skill.Package
	// Changes lists the files that differ from the installed version. It
	// is nil when the store no longer holds that version to compare with.
	Changes []skill.Change
	// Report inspects the new version. Executables the previous version
	// already had are not reported again.
	Report policy.Report
}

// ContentChanged reports whether the files change, not only the commit.
func (u PlannedUpdate) ContentChanged() bool {
	return u.Package.TreeHash() != u.Previous.TreeHash
}

// UpdateResult reports what was updated.
type UpdateResult struct {
	Plan    UpdatePlan
	Updated []lock.Entry
}

// UpdateHandler runs Update.
type UpdateHandler struct {
	Catalog     AgentCatalog
	Fetchers    map[source.Kind]Fetcher
	Store       Store
	Deployer    Deployer
	Trees       TreeReader
	Locks       LockRepository
	Prompter    Prompter
	Trust       TrustList // nil skips the trust check
	ProjectRoot string
	Limits      policy.Limits
}

// Handle moves skills to what their requested ref points to now. It shows
// the file changes and the inspection of the new version, asks, and only
// then replaces the installed copies and updates the lock. skilus.yaml does
// not change: it records the ref, not the commit.
func (h UpdateHandler) Handle(ctx context.Context, cmd Update) (UpdateResult, error) {
	lf, err := h.Locks.Load(ctx, cmd.Scope)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("load lock: %w", err)
	}
	entries, err := pickEntries(lf, cmd.Names, cmd.Scope)
	if err != nil {
		return UpdateResult{}, err
	}

	var plan UpdatePlan
	sources := map[string]Fetched{}
	for _, e := range entries {
		u, err := h.plan(ctx, e, cmd, sources)
		if err != nil {
			return UpdateResult{Plan: plan}, fmt.Errorf("skill %s: %w", e.Skill, err)
		}
		if u == nil {
			plan.Current = append(plan.Current, e)
			continue
		}
		plan.Skills = append(plan.Skills, *u)
	}
	if len(plan.Skills) == 0 {
		return UpdateResult{Plan: plan}, nil
	}

	if err := updateGate(plan, cmd); err != nil {
		return UpdateResult{Plan: plan}, err
	}
	dests, err := h.checkTargets(ctx, plan, cmd.Force)
	if err != nil {
		return UpdateResult{Plan: plan}, err
	}
	if !cmd.Yes {
		ok, err := h.Prompter.ConfirmUpdate(ctx, plan)
		if err != nil {
			return UpdateResult{Plan: plan}, err
		}
		if !ok {
			return UpdateResult{Plan: plan}, ErrCancelled
		}
	}

	var updated []lock.Entry
	for i, u := range plan.Skills {
		if u.ContentChanged() {
			storeDir, err := h.Store.Put(ctx, u.Package)
			if err != nil {
				return UpdateResult{Plan: plan}, fmt.Errorf("store %s: %w", u.Previous.Skill, err)
			}
			for _, t := range dests[i] {
				if err := h.Deployer.Remove(ctx, t.dir); err != nil {
					return UpdateResult{Plan: plan}, fmt.Errorf("remove %s: %w; run skilus sync to restore the locked version", t.dir, err)
				}
				if err := h.Deployer.Deploy(ctx, storeDir, t.dir, t.mode); err != nil {
					return UpdateResult{Plan: plan}, fmt.Errorf("deploy %s: %w; run skilus sync to restore the locked version", t.dir, err)
				}
			}
		}
		e := u.Previous
		e.Commit, e.TreeHash, e.Executables = u.Commit, u.Package.TreeHash(), skill.Executables(u.Package.Files())
		if err := lf.Update(e); err != nil {
			return UpdateResult{Plan: plan}, err
		}
		updated = append(updated, e)
	}
	if err := h.Locks.Save(ctx, cmd.Scope, lf); err != nil {
		return UpdateResult{Plan: plan}, fmt.Errorf("save lock: %w", err)
	}
	lf.PullEvents()
	return UpdateResult{Plan: plan, Updated: updated}, nil
}

// pickEntries returns the named entries, or all of them, checking every
// name first.
func pickEntries(lf *lock.Lockfile, names []string, scope agent.Scope) ([]lock.Entry, error) {
	if len(names) == 0 {
		return lf.Entries(), nil
	}
	var out []lock.Entry
	seen := map[string]bool{}
	for _, raw := range names {
		if seen[raw] {
			continue
		}
		seen[raw] = true
		n, err := skill.NewName(raw)
		if err != nil {
			return nil, err
		}
		e, ok := lf.Entry(n)
		if !ok {
			return nil, fmt.Errorf("skill %s is not installed in the %s scope: %w", n, scope, domain.ErrNotFound)
		}
		out = append(out, e)
	}
	return out, nil
}

// plan fetches the entry's requested ref and returns nil when nothing moved.
func (h UpdateHandler) plan(ctx context.Context, e lock.Entry, cmd Update, sources map[string]Fetched) (*PlannedUpdate, error) {
	src, err := source.Parse(e.Source)
	if err != nil {
		return nil, err
	}
	if src.Kind == source.KindGit {
		src.Ref = e.Requested
	}
	fetched, ok := sources[src.String()]
	if !ok {
		fetcher, ok := h.Fetchers[src.Kind]
		if !ok {
			return nil, fmt.Errorf("%s sources are not supported yet: %w", src.Kind, domain.ErrInvalid)
		}
		if fetched, err = fetcher.Fetch(ctx, src); err != nil {
			return nil, fmt.Errorf("read source %s: %w", src, err)
		}
		sources[src.String()] = fetched
	}

	var found *FetchedSkill
	for i := range fetched.Skills {
		if fetched.Skills[i].Path == e.Path {
			found = &fetched.Skills[i]
		}
	}
	if found == nil {
		for _, bad := range fetched.Invalid {
			if bad.Path == e.Path {
				return nil, fmt.Errorf("the new version in %s cannot be read: %w", bad.Path, bad.Err)
			}
		}
		return nil, fmt.Errorf("%s no longer has a skill in %s: %w", e.Source, e.Path, domain.ErrNotFound)
	}
	p := found.Package
	if p.Name() != e.Skill {
		return nil, fmt.Errorf("%s/%s is now named %s; remove it and add the new name: %w", e.Source, e.Path, p.Name(), domain.ErrConflict)
	}
	if p.TreeHash() == e.TreeHash && fetched.Commit == e.Commit {
		return nil, nil
	}

	u := &PlannedUpdate{Previous: e, Commit: fetched.Commit, Package: p}
	if u.ContentChanged() {
		if old := h.stored(ctx, e); old != nil {
			u.Changes = skill.Diff(old, p.Files())
		}
	}
	untrusted, err := trustFindings(ctx, h.Trust, e.Source, trustScopes(cmd.Scope)...)
	if err != nil {
		return nil, err
	}
	u.Report = policy.Inspect(p, h.Limits, policy.Allow{Scripts: cmd.AllowScripts})
	u.Report.Findings = append(append([]policy.Finding(nil), untrusted...), u.Report.Findings...)
	known := map[string]bool{}
	for _, x := range e.Executables {
		known[x] = true
	}
	kept := u.Report.Findings[:0]
	for _, f := range u.Report.Findings {
		if f.Code == policy.CodeExecutable && known[f.Path] {
			continue // accepted when the skill was installed
		}
		kept = append(kept, f)
	}
	u.Report.Findings = kept
	return u, nil
}

func (h UpdateHandler) stored(ctx context.Context, e lock.Entry) []skill.File {
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

// updateGate is gate for updates: under --yes only executables the skill
// did not have before need --allow-scripts.
func updateGate(plan UpdatePlan, cmd Update) error {
	var reasons []string
	for _, u := range plan.Skills {
		r, name := u.Report, u.Previous.Skill
		newScripts := false
		for _, f := range r.Findings {
			newScripts = newScripts || f.Code == policy.CodeExecutable
		}
		switch {
		case r.Blocking():
			reasons = append(reasons, fmt.Sprintf("%s: blocking findings", name))
		case cmd.Strict && r.Warnings() > 0:
			reasons = append(reasons, fmt.Sprintf("%s: %d warnings under --strict", name, r.Warnings()))
		case cmd.Yes && newScripts && !cmd.AllowScripts:
			reasons = append(reasons, fmt.Sprintf("%s: new executable files need --allow-scripts", name))
		}
	}
	if len(reasons) > 0 {
		return fmt.Errorf("%s: %w", strings.Join(reasons, "; "), ErrRejected)
	}
	return nil
}

type dest struct {
	dir  string
	mode agent.Mode
}

// checkTargets resolves where each changed skill is installed and refuses
// to go on when an installed copy no longer matches the lock, unless force.
func (h UpdateHandler) checkTargets(ctx context.Context, plan UpdatePlan, force bool) ([][]dest, error) {
	byID, err := agentsByID(ctx, h.Catalog)
	if err != nil {
		return nil, err
	}
	out := make([][]dest, len(plan.Skills))
	var modified []string
	for i, u := range plan.Skills {
		if !u.ContentChanged() {
			continue
		}
		e := u.Previous
		for _, t := range e.Targets {
			a, ok := byID[t.Agent]
			if !ok {
				return nil, fmt.Errorf("skill %s targets agent %s, which is no longer in the catalog: %w", e.Skill, t.Agent, domain.ErrConflict)
			}
			dir := filepath.Join(skillsDir(a, t.Scope, h.ProjectRoot), e.Skill.String())
			_, got, err := readInstalled(ctx, h.Trees, dir, e)
			if !force && !errors.Is(err, domain.ErrNotFound) && (err != nil || got != e.TreeHash) {
				modified = append(modified, dir)
			}
			out[i] = append(out[i], dest{dir: dir, mode: t.Mode})
		}
	}
	if len(modified) > 0 {
		return nil, fmt.Errorf("modified by hand, use --force to replace: %s: %w", strings.Join(modified, ", "), domain.ErrConflict)
	}
	return out, nil
}

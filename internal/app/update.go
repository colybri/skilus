package app

import (
	"context"
	"errors"
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
	// Dependencies installs what the new versions require and the lock
	// lacks, one plan per source.
	Dependencies []InstallPlan
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
	// Installed lists the requirements installed along the way.
	Installed []lock.Entry
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
		return UpdateResult{}, domain.Errorf("cargar el lock: %w", err)
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
			return UpdateResult{Plan: plan}, domain.Errorf("skill %s: %w", e.Skill, err)
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
	deps, err := h.dependencies(ctx, cmd, plan, sources, lf)
	if err != nil {
		return UpdateResult{Plan: plan}, err
	}
	for _, g := range deps {
		plan.Dependencies = append(plan.Dependencies, g.plan)
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

	var updated, installed []lock.Entry
	for i := len(deps) - 1; i >= 0; i-- {
		add := AddSkillHandler{Store: h.Store, Deployer: h.Deployer, Locks: h.Locks, Manifests: noManifest{}}
		got, err := add.install(ctx, AddSkill{Scope: cmd.Scope}, deps[i].fetched, deps[i].plan, lf)
		installed = append(installed, got...)
		if err != nil {
			return UpdateResult{Plan: plan, Installed: installed}, err
		}
	}
	for i, u := range plan.Skills {
		if u.ContentChanged() {
			storeDir, err := h.Store.Put(ctx, u.Package)
			if err != nil {
				return UpdateResult{Plan: plan}, domain.Errorf("guardar %s en el almacén: %w", u.Previous.Skill, err)
			}
			for _, t := range dests[i] {
				if err := h.Deployer.Remove(ctx, t.dir); err != nil {
					return UpdateResult{Plan: plan}, domain.Errorf("borrar %s: %w; ejecuta skilus sync para restaurar la versión del lock", t.dir, err)
				}
				if err := h.Deployer.Deploy(ctx, storeDir, t.dir, t.mode); err != nil {
					return UpdateResult{Plan: plan}, domain.Errorf("desplegar %s: %w; ejecuta skilus sync para restaurar la versión del lock", t.dir, err)
				}
			}
		}
		e := u.Previous
		e.Commit, e.TreeHash, e.Executables = u.Commit, u.Package.TreeHash(), skill.Executables(u.Package.Files())
		e.Requires = requiredNames(u.Package)
		if err := lf.Update(e); err != nil {
			return UpdateResult{Plan: plan}, err
		}
		updated = append(updated, e)
	}
	if err := h.Locks.Save(ctx, cmd.Scope, lf); err != nil {
		return UpdateResult{Plan: plan}, domain.Errorf("guardar el lock: %w", err)
	}
	lf.PullEvents()
	return UpdateResult{Plan: plan, Updated: updated, Installed: installed}, nil
}

// dependencies plans what the new versions require and the lock lacks.
// Each requirement goes to the agents of the skill that needs it.
func (h UpdateHandler) dependencies(ctx context.Context, cmd Update, plan UpdatePlan, sources map[string]Fetched, lf *lock.Lockfile) ([]planGroup, error) {
	byID, err := agentsByID(ctx, h.Catalog)
	if err != nil {
		return nil, err
	}
	var groups []planGroup
	for _, u := range plan.Skills {
		src, err := source.Parse(u.Previous.Source)
		if err != nil {
			return nil, err
		}
		if src.Kind == source.KindGit {
			src.Ref = u.Previous.Requested
		}
		var targets []PlannedTarget
		for _, t := range u.Previous.Targets {
			if a, ok := byID[t.Agent]; ok {
				targets = append(targets, PlannedTarget{Target: t, Dir: skillsDir(a, t.Scope, h.ProjectRoot)})
			}
		}
		fetched := sources[src.String()]
		groups = append(groups, planGroup{raw: src.String(), fetched: fetched, plan: InstallPlan{
			Source: fetched.Source, Commit: fetched.Commit, Targets: targets,
			Skills: []PlannedSkill{{Package: u.Package, Path: u.Previous.Path}},
		}})
	}
	add := AddSkillHandler{Fetchers: h.Fetchers, Trust: h.Trust, Limits: h.Limits, ProjectRoot: h.ProjectRoot}
	groups, err = add.resolver(cmd.Scope, cmd.AllowScripts, lf).resolve(ctx, groups, nil)
	if err != nil {
		return nil, err
	}
	// Keep only the requirements; the updated skills are planned apart.
	var out []planGroup
	for _, g := range groups {
		var deps []PlannedSkill
		for _, s := range g.plan.Skills {
			if s.Dependency {
				deps = append(deps, s)
			}
		}
		if len(deps) == 0 {
			continue
		}
		g.plan.Skills = deps
		if g.plan.Targets == nil {
			// A new source takes the agents of the first skill needing it.
			g.plan.Targets = groups[firstRequirer(groups, deps[0])].plan.Targets
		}
		out = append(out, g)
	}
	return out, nil
}

// firstRequirer returns the group holding the skill that required s.
func firstRequirer(groups []planGroup, s PlannedSkill) int {
	for i, g := range groups {
		for _, p := range g.plan.Skills {
			if len(s.RequiredBy) > 0 && p.Package.Name() == s.RequiredBy[0] {
				return i
			}
		}
	}
	return 0
}

// noManifest is for installs that must not touch skilus.yaml.
type noManifest struct{}

func (noManifest) AddSkill(context.Context, agent.Scope, ManifestEntry) error { return nil }
func (noManifest) RemoveSkill(context.Context, agent.Scope, skill.Name) error { return nil }

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
			return nil, domain.Errorf("la skill %s no está instalada en el ámbito %s: %w", n, scope, domain.ErrNotFound)
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
			return nil, domain.Errorf("los orígenes %s aún no están soportados: %w", src.Kind, domain.ErrInvalid)
		}
		if fetched, err = fetcher.Fetch(ctx, src); err != nil {
			return nil, domain.Errorf("leer el origen %s: %w", src, err)
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
				return nil, domain.Errorf("no se puede leer la nueva versión en %s: %w", bad.Path, bad.Err)
			}
		}
		return nil, domain.Errorf("%s ya no tiene ninguna skill en %s: %w", e.Source, e.Path, domain.ErrNotFound)
	}
	p := found.Package
	if p.Name() != e.Skill {
		return nil, domain.Errorf("%s/%s ahora se llama %s; quítala y añade el nuevo nombre: %w", e.Source, e.Path, p.Name(), domain.ErrConflict)
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
	var reasons domain.Errors
	for _, ip := range plan.Dependencies {
		if err := gate(ip, AddSkill{Yes: cmd.Yes, Strict: cmd.Strict, AllowScripts: cmd.AllowScripts}); err != nil {
			reasons = append(reasons, err)
		}
	}
	for _, u := range plan.Skills {
		r, name := u.Report, u.Previous.Skill
		newScripts := false
		for _, f := range r.Findings {
			newScripts = newScripts || f.Code == policy.CodeExecutable
		}
		switch {
		case r.Blocking():
			reasons = append(reasons, domain.Errorf("%s: hallazgos que bloquean", name))
		case cmd.Strict && r.Warnings() > 0:
			reasons = append(reasons, domain.Errorf("%s: %d avisos con --strict", name, r.Warnings()))
		case cmd.Yes && newScripts && !cmd.AllowScripts:
			reasons = append(reasons, domain.Errorf("%s: los ficheros ejecutables nuevos necesitan --allow-scripts", name))
		}
	}
	if len(reasons) > 0 {
		return domain.Errorf("%w: %w", reasons, ErrRejected)
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
				return nil, domain.Errorf("la skill %s apunta al agente %s, que ya no está en el catálogo: %w", e.Skill, t.Agent, domain.ErrConflict)
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
		return nil, domain.Errorf("modificadas a mano, usa --force para reemplazarlas: %s: %w", strings.Join(modified, ", "), domain.ErrConflict)
	}
	return out, nil
}

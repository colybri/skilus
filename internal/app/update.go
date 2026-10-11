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
	Names        []string // skills to update, install or remove; empty means every skill in the scope
	Scope        agent.Scope
	Yes          bool // do not ask; new executables still need AllowScripts
	Strict       bool // treat warnings as blocking
	AllowScripts bool
	Force        bool // replace installed copies modified by hand
}

// UpdatePlan is what the user confirms.
type UpdatePlan struct {
	// Install has one plan per source for the skills skilus.yaml declares
	// and the lock lacks, with what they require.
	Install []InstallPlan
	Skills  []PlannedUpdate
	// Current lists the skills whose ref still points to the locked commit.
	Current []lock.Entry
	// Dependencies installs what the new versions require and the lock
	// lacks, one plan per source.
	Dependencies []InstallPlan
	// Remove lists the skills skilus.yaml no longer declares and the
	// requirements nothing else needs once they go.
	Remove []lock.Entry
}

// Empty reports whether there is nothing to do.
func (p UpdatePlan) Empty() bool {
	return len(p.Install) == 0 && len(p.Skills) == 0 && len(p.Remove) == 0
}

// PlannedUpdate is one skill moving to a new commit, or to the source
// skilus.yaml now declares for it.
type PlannedUpdate struct {
	Previous lock.Entry
	// Source, Requested and Path say where the new version comes from, as
	// the lock records them.
	Source    string
	Requested string
	Path      string
	Commit    string // new commit; empty for local sources
	Package   skill.Package
	// Changes lists the files that differ from the installed version. It
	// is nil when the store no longer holds that version to compare with.
	Changes []skill.Change
	// Report inspects the new version. Executables the previous version
	// already had are not reported again, unless the source changed.
	Report policy.Report

	fetchKey string // the key of the fetched source in Handle's sources
}

// Moved reports whether skilus.yaml now declares another source or ref
// than the lock.
func (u PlannedUpdate) Moved() bool {
	return u.Source != u.Previous.Source || u.Requested != u.Previous.Requested
}

// ContentChanged reports whether the files change, not only the commit.
func (u PlannedUpdate) ContentChanged() bool {
	return u.Package.TreeHash() != u.Previous.TreeHash
}

// UpdateResult reports what was updated.
type UpdateResult struct {
	Plan    UpdatePlan
	Updated []lock.Entry
	// Added lists the skills installed because skilus.yaml declares them.
	Added []lock.Entry
	// Installed lists the requirements installed along the way.
	Installed []lock.Entry
	Removed   []lock.Entry
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
	// Manifest gives the skills skilus.yaml declares, with their source
	// and ref; nil keeps the lock as the only reference.
	Manifest ManifestReader
	// Detector and DefaultModes place the skills skilus.yaml adds, as
	// skilus add does without --agent.
	Detector     AgentDetector
	DefaultModes map[agent.Scope]agent.Mode
}

// Handle makes the scope match skilus.yaml and moves skills to what their
// requested ref points to now. It installs the skills skilus.yaml declares
// and the lock lacks, removes the ones it no longer declares and takes the
// source and ref from it when they differ from the lock. It shows the plan
// with the inspection of every new version, asks, and only then changes the
// agents and the lock. skilus.yaml does not change: it records the ref, not
// the commit.
func (h UpdateHandler) Handle(ctx context.Context, cmd Update) (UpdateResult, error) {
	lf, err := h.Locks.Load(ctx, cmd.Scope)
	if err != nil {
		return UpdateResult{}, domain.Errorf("cargar el lock: %w", err)
	}
	m, err := h.manifest(ctx, cmd.Scope)
	if err != nil {
		return UpdateResult{}, err
	}
	declared := sourcesOf(m)
	entries, missing, gone, err := h.pick(cmd, m, lf)
	if err != nil {
		return UpdateResult{}, err
	}

	var plan UpdatePlan
	var added []planGroup
	if len(missing) > 0 {
		if added, err = h.planAdded(ctx, cmd, m, missing, lf); err != nil {
			return UpdateResult{Plan: plan}, err
		}
		for _, g := range added {
			plan.Install = append(plan.Install, g.plan)
		}
	}
	if plan.Remove, err = removals(gone, lf); err != nil {
		return UpdateResult{Plan: plan}, err
	}
	removed := map[skill.Name]bool{}
	for _, e := range plan.Remove {
		removed[e.Skill] = true
	}
	sources := map[string]Fetched{}
	for _, e := range entries {
		if removed[e.Skill] {
			continue // a requirement going away with what needed it
		}
		u, err := h.plan(ctx, e, cmd, declared, sources)
		if err != nil {
			return UpdateResult{Plan: plan}, domain.Errorf("skill %s: %w", e.Skill, err)
		}
		if u == nil {
			plan.Current = append(plan.Current, e)
			continue
		}
		plan.Skills = append(plan.Skills, *u)
	}
	if plan.Empty() {
		return UpdateResult{Plan: plan}, nil
	}
	var deps []planGroup
	if len(plan.Skills) > 0 {
		if deps, err = h.dependencies(ctx, cmd, plan, sources, lf); err != nil {
			return UpdateResult{Plan: plan}, err
		}
		deps = withoutPlanned(deps, added)
	}
	for _, g := range deps {
		plan.Dependencies = append(plan.Dependencies, g.plan)
	}

	if err := updateGate(plan, cmd, m); err != nil {
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

	res := UpdateResult{Plan: plan}
	add := h.add()
	for i := len(added) - 1; i >= 0; i-- {
		got, err := add.install(ctx, AddSkill{Scope: cmd.Scope}, added[i].fetched, added[i].plan, lf)
		for _, e := range got {
			if e.Dependency {
				res.Installed = append(res.Installed, e)
			} else {
				res.Added = append(res.Added, e)
			}
		}
		if err != nil {
			return res, err
		}
	}
	for i := len(deps) - 1; i >= 0; i-- {
		got, err := add.install(ctx, AddSkill{Scope: cmd.Scope}, deps[i].fetched, deps[i].plan, lf)
		res.Installed = append(res.Installed, got...)
		if err != nil {
			return res, err
		}
	}
	for i, u := range plan.Skills {
		if u.ContentChanged() {
			storeDir, err := h.Store.Put(ctx, u.Package)
			if err != nil {
				return res, domain.Errorf("guardar %s en el almacén: %w", u.Previous.Skill, err)
			}
			for _, t := range dests[i] {
				if err := h.Deployer.Remove(ctx, t.dir); err != nil {
					return res, domain.Errorf("borrar %s: %w; ejecuta skilus sync para restaurar la versión del lock", t.dir, err)
				}
				if err := h.Deployer.Deploy(ctx, storeDir, t.dir, t.mode); err != nil {
					return res, domain.Errorf("desplegar %s: %w; ejecuta skilus sync para restaurar la versión del lock", t.dir, err)
				}
			}
		}
		e := u.Previous
		e.Source, e.Requested, e.Path = u.Source, u.Requested, u.Path
		e.Commit, e.TreeHash, e.Executables = u.Commit, u.Package.TreeHash(), skill.Executables(u.Package.Files())
		e.Requires = requiredNames(u.Package)
		if err := lf.Update(e); err != nil {
			return res, err
		}
		res.Updated = append(res.Updated, e)
	}
	res.Removed, err = removeEntries(ctx, h.Catalog, h.Deployer, h.ProjectRoot, plan.Remove, lf)
	if err != nil {
		if saveErr := h.Locks.Save(ctx, cmd.Scope, lf); saveErr != nil {
			err = errors.Join(err, domain.Errorf("guardar el lock: %w", saveErr))
		}
		return res, err
	}
	if err := h.Locks.Save(ctx, cmd.Scope, lf); err != nil {
		return res, domain.Errorf("guardar el lock: %w", err)
	}
	lf.PullEvents()
	return res, nil
}

// add returns the add handler update installs with. It never touches
// skilus.yaml, which already declares what update installs.
func (h UpdateHandler) add() AddSkillHandler {
	return AddSkillHandler{
		Catalog: h.Catalog, Detector: h.Detector, Fetchers: h.Fetchers, Store: h.Store,
		Deployer: h.Deployer, Locks: h.Locks, Manifests: noManifest{}, Trust: h.Trust,
		ProjectRoot: h.ProjectRoot, DefaultModes: h.DefaultModes, Limits: h.Limits,
	}
}

// manifest reads the scope's skilus.yaml. Without a reader or a file it
// returns nil: the lock is then the only reference, so a missing file does
// not remove every skill.
func (h UpdateHandler) manifest(ctx context.Context, scope agent.Scope) (*Manifest, error) {
	if h.Manifest == nil {
		return nil, nil
	}
	m, err := h.Manifest.Manifest(ctx, scope)
	if err != nil {
		return nil, domain.Errorf("leer skilus.yaml: %w", err)
	}
	if m.Missing {
		return nil, nil
	}
	return &m, nil
}

// pick splits the skills to work on: installed ones to update, declared
// ones to install and installed ones skilus.yaml no longer declares. With
// names, only those; otherwise every skill. Declared skills are only
// missing when no profile is active: then the lock holds the profile's.
func (h UpdateHandler) pick(cmd Update, m *Manifest, lf *lock.Lockfile) (update []lock.Entry, missing []skill.Name, gone []lock.Entry, err error) {
	declared := map[skill.Name]bool{}
	var wanted []skill.Name
	if m != nil {
		for _, e := range m.Skills {
			declared[e.Name] = true
		}
		if !profileActive(*m, lf) {
			for _, e := range m.Skills {
				if _, ok := lf.Entry(e.Name); !ok {
					wanted = append(wanted, e.Name)
				}
			}
		}
	}
	undeclared := func(e lock.Entry) bool { return m != nil && !e.Dependency && !declared[e.Skill] }
	if len(cmd.Names) == 0 {
		for _, e := range lf.Entries() {
			if undeclared(e) {
				gone = append(gone, e)
			} else {
				update = append(update, e)
			}
		}
		return update, wanted, gone, nil
	}
	seen := map[skill.Name]bool{}
	for _, raw := range cmd.Names {
		n, err := skill.NewName(raw)
		if err != nil {
			return nil, nil, nil, err
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		e, ok := lf.Entry(n)
		switch {
		case ok && undeclared(e):
			gone = append(gone, e)
		case ok:
			update = append(update, e)
		case declared[n]:
			missing = append(missing, n)
		default:
			return nil, nil, nil, domain.Errorf("la skill %s no está instalada en el ámbito %s: %w", n, cmd.Scope, domain.ErrNotFound)
		}
	}
	return update, missing, gone, nil
}

// planAdded plans installing the declared skills the lock lacks in the
// detected agents, as skilus add does.
func (h UpdateHandler) planAdded(ctx context.Context, cmd Update, m *Manifest, names []skill.Name, lf *lock.Lockfile) ([]planGroup, error) {
	add := h.add()
	targets, err := add.targets(ctx, AddSkill{Scope: cmd.Scope}, h.DefaultModes[cmd.Scope])
	if errors.Is(err, domain.ErrInvalid) {
		return nil, domain.Errorf("no se ha detectado ningún agente donde instalar %s: %w", joinNames(names), domain.ErrInvalid)
	}
	if err != nil {
		return nil, err
	}
	return add.planDeclared(ctx, cmd.Scope, *m, names, targets, cmd.AllowScripts, lf)
}

// removals adds to gone the requirements nothing else needs once gone is
// removed, and refuses when a skill that stays requires one of gone.
func removals(gone []lock.Entry, lf *lock.Lockfile) ([]lock.Entry, error) {
	if len(gone) == 0 {
		return nil, nil
	}
	names := map[skill.Name]bool{}
	for _, e := range gone {
		names[e.Skill] = true
	}
	var blocked domain.Errors
	for _, e := range gone {
		if deps := lf.Dependents(e.Skill, names); len(deps) > 0 {
			blocked = append(blocked, domain.Errorf("%s es necesaria para %s", e.Skill, joinNames(deps)))
		}
	}
	if len(blocked) > 0 {
		return nil, domain.Errorf("%w; vuelve a declararla en skills: o quita también las que la necesitan: %w", blocked, domain.ErrConflict)
	}
	return append(gone, lf.Unneeded(names)...), nil
}

// withoutPlanned drops from deps the skills the added groups already
// install, so a requirement shared by both is installed once.
func withoutPlanned(deps, added []planGroup) []planGroup {
	planned := map[skill.Name]bool{}
	for _, g := range added {
		for _, s := range g.plan.Skills {
			planned[s.Package.Name()] = true
		}
	}
	var out []planGroup
	for _, g := range deps {
		kept := g.plan.Skills[:0:0]
		for _, s := range g.plan.Skills {
			if !planned[s.Package.Name()] {
				kept = append(kept, s)
			}
		}
		if len(kept) > 0 {
			g.plan.Skills = kept
			out = append(out, g)
		}
	}
	return out
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
		var targets []PlannedTarget
		for _, t := range u.Previous.Targets {
			if a, ok := byID[t.Agent]; ok {
				targets = append(targets, PlannedTarget{Target: t, Dir: skillsDir(a, t.Scope, h.ProjectRoot)})
			}
		}
		fetched := sources[u.fetchKey]
		groups = append(groups, planGroup{raw: u.fetchKey, fetched: fetched, plan: InstallPlan{
			Source: fetched.Source, Commit: fetched.Commit, Targets: targets,
			Skills: []PlannedSkill{{Package: u.Package, Path: u.Path}},
		}})
	}
	groups, err = h.add().resolver(cmd.Scope, cmd.AllowScripts, lf).resolve(ctx, groups, nil)
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

// plan fetches the entry's requested ref, or the source skilus.yaml
// declares for it, and returns nil when nothing moved.
func (h UpdateHandler) plan(ctx context.Context, e lock.Entry, cmd Update, declared map[skill.Name]string, sources map[string]Fetched) (*PlannedUpdate, error) {
	src, err := source.Parse(e.Source)
	if err != nil {
		return nil, err
	}
	if src.Kind == source.KindGit {
		src.Ref = e.Requested
	}
	want, moved, err := changedSource(declared, e, h.ProjectRoot)
	if err != nil {
		return nil, err
	}
	if moved {
		src = want
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

	// The same source keeps the skill at its path; another one is searched
	// by name, since the skill may live elsewhere in it.
	var found *FetchedSkill
	for i := range fetched.Skills {
		if !moved && fetched.Skills[i].Path == e.Path || moved && fetched.Skills[i].Package.Name() == e.Skill {
			found = &fetched.Skills[i]
		}
	}
	if found == nil && moved {
		return nil, domain.Errorf("%s no tiene ninguna skill llamada %s: %w", src, e.Skill, domain.ErrNotFound)
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
	u := &PlannedUpdate{
		Previous: e, Source: fetched.Source, Requested: fetched.Requested, Path: found.Path,
		Commit: fetched.Commit, Package: p, fetchKey: src.String(),
	}
	if !u.Moved() && p.TreeHash() == e.TreeHash && fetched.Commit == e.Commit {
		return nil, nil
	}

	if u.ContentChanged() {
		if old := h.stored(ctx, e); old != nil {
			u.Changes = skill.Diff(old, p.Files())
		}
	}
	untrusted, err := trustFindings(ctx, h.Trust, u.Source, trustScopes(cmd.Scope)...)
	if err != nil {
		return nil, err
	}
	u.Report = policy.Inspect(p, h.Limits, policy.Allow{Scripts: cmd.AllowScripts})
	u.Report.Findings = append(append([]policy.Finding(nil), untrusted...), u.Report.Findings...)
	known := map[string]bool{}
	for _, x := range e.Executables {
		known[x] = u.Source == e.Source // another source has to be accepted again
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
func updateGate(plan UpdatePlan, cmd Update, m *Manifest) error {
	var reasons domain.Errors
	if len(plan.Install) > 0 {
		reasons = declaredGate(plan.Install, cmd.Yes, cmd.Strict, cmd.AllowScripts, *m)
	}
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

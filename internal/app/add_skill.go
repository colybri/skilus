package app

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"strings"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

// Errors returned by AddSkill besides the domain ones.
var (
	// ErrRejected means the inspection blocked the installation.
	ErrRejected = domain.NewError("rechazado por la inspección")
	// ErrCancelled means the user answered no.
	ErrCancelled = domain.NewError("cancelado")
)

// AllowScripts is the manifest allow value that accepts executable files.
const AllowScripts = "scripts"

// AddSkill is the command behind `skilus add`.
type AddSkill struct {
	Source       string
	Skills       []string // skill names to install; empty means every skill found
	Agents       []string // agent ids; empty means every detected agent
	Scope        agent.Scope
	Mode         agent.Mode // empty means the default for the scope
	Yes          bool       // do not ask; warnings are accepted except scripts
	Strict       bool       // treat warnings as blocking
	AllowScripts bool
}

// InstallPlan is what the user confirms.
type InstallPlan struct {
	Source  string
	Commit  string // resolved commit; empty for local sources
	Skills  []PlannedSkill
	Targets []PlannedTarget
	// Skipped are skills of the source that could not be read; they are
	// not installed.
	Skipped []InvalidSkill
	// More holds the other sources that requirements pull skills from.
	More []InstallPlan
}

// All returns the plan and the plans in More.
func (p InstallPlan) All() []InstallPlan {
	return append([]InstallPlan{p}, p.More...)
}

// PlannedSkill is one skill with its inspection report.
type PlannedSkill struct {
	Package skill.Package
	Path    string
	Report  policy.Report
	// Dependency marks a skill installed because others in RequiredBy need
	// it, not because it was asked for.
	Dependency bool
	RequiredBy []skill.Name
}

// PlannedTarget is one destination directory.
type PlannedTarget struct {
	Target agent.Target
	Dir    string // skills directory of the agent; the skill goes in Dir/<name>
}

// AddResult reports what was installed.
type AddResult struct {
	Plan      InstallPlan
	Installed []lock.Entry
}

// AddSkillHandler runs AddSkill.
type AddSkillHandler struct {
	Catalog     AgentCatalog
	Detector    AgentDetector
	Fetchers    map[source.Kind]Fetcher
	Store       Store
	Deployer    Deployer
	Locks       LockRepository
	Manifests   ManifestRepository
	Prompter    Prompter
	Trust       TrustList // nil skips the trust check
	ProjectRoot string
	// DefaultModes gives the mode per scope when the command sets none.
	DefaultModes map[agent.Scope]agent.Mode
	Limits       policy.Limits
}

// Handle installs the requested skills or changes nothing.
func (h AddSkillHandler) Handle(ctx context.Context, cmd AddSkill) (AddResult, error) {
	mode := cmd.Mode
	if mode == "" {
		mode = h.DefaultModes[cmd.Scope]
	}
	if _, err := agent.ParseMode(string(mode)); err != nil {
		return AddResult{}, err
	}

	targets, err := h.targets(ctx, cmd, mode)
	if err != nil {
		return AddResult{}, err
	}

	fetched, err := fetchSource(ctx, h.Fetchers, cmd.Source)
	if err != nil {
		return AddResult{}, err
	}
	selected, err := selectSkills(fetched, cmd.Skills)
	if err != nil {
		return AddResult{}, err
	}

	lf, err := h.Locks.Load(ctx, cmd.Scope)
	if err != nil {
		return AddResult{}, domain.Errorf("cargar el lock: %w", err)
	}

	fetched.Source = h.recordedSource(cmd.Scope, fetched.Source)
	plan := InstallPlan{Source: fetched.Source, Commit: fetched.Commit, Targets: targets}
	if len(cmd.Skills) == 0 {
		plan.Skipped = fetched.Invalid
	}
	untrusted, err := trustFindings(ctx, h.Trust, cmd.Source, trustScopes(cmd.Scope)...)
	if err != nil {
		return AddResult{}, err
	}
	for _, s := range selected {
		if _, ok := lf.Entry(s.Package.Name()); ok {
			return AddResult{}, domain.Errorf("la skill %s ya está instalada; usa skilus update: %w", s.Package.Name(), domain.ErrAlreadyExists)
		}
		report := policy.Inspect(s.Package, h.Limits, policy.Allow{Scripts: cmd.AllowScripts})
		report.Findings = append(append([]policy.Finding(nil), untrusted...), report.Findings...)
		plan.Skills = append(plan.Skills, PlannedSkill{Package: s.Package, Path: s.Path, Report: report})
	}
	groups, err := h.resolver(cmd.Scope, cmd.AllowScripts, lf).resolve(ctx, []planGroup{{raw: cmd.Source, fetched: fetched, plan: plan}}, targets)
	if err != nil {
		return AddResult{Plan: plan}, err
	}
	plan = groups[0].plan
	for _, g := range groups[1:] {
		plan.More = append(plan.More, g.plan)
	}

	if err := gate(plan, cmd); err != nil {
		return AddResult{Plan: plan}, err
	}
	if !cmd.Yes {
		ok, err := h.Prompter.ConfirmInstall(ctx, plan)
		if err != nil {
			return AddResult{Plan: plan}, err
		}
		if !ok {
			return AddResult{Plan: plan}, ErrCancelled
		}
	}

	installed, err := h.installGroups(ctx, cmd.Scope, groups, plan, lf)
	if err != nil {
		return AddResult{Plan: plan, Installed: installed}, err
	}
	return AddResult{Plan: plan, Installed: installed}, nil
}

// resolver returns the dependency resolver for an install in scope.
func (h AddSkillHandler) resolver(scope agent.Scope, allowScripts bool, lf *lock.Lockfile) depResolver {
	return depResolver{
		fetchers:     h.Fetchers,
		trust:        h.Trust,
		limits:       h.Limits,
		scope:        scope,
		allowScripts: func(skill.Name) bool { return allowScripts },
		record:       func(src string) string { return h.recordedSource(scope, src) },
		lf:           lf,
	}
}

// installGroups installs the groups, dependencies' sources first, with the
// plans as they were confirmed (final holds groups[0] and then More).
func (h AddSkillHandler) installGroups(ctx context.Context, scope agent.Scope, groups []planGroup, final InstallPlan, lf *lock.Lockfile) ([]lock.Entry, error) {
	plans := final.All()
	var installed []lock.Entry
	for i := len(groups) - 1; i >= 0; i-- {
		got, err := h.install(ctx, AddSkill{Scope: scope}, groups[i].fetched, plans[i], lf)
		installed = append(installed, got...)
		if err != nil {
			return installed, err
		}
	}
	return installed, nil
}

func (h AddSkillHandler) targets(ctx context.Context, cmd AddSkill, mode agent.Mode) ([]PlannedTarget, error) {
	agents, err := h.Catalog.Agents(ctx)
	if err != nil {
		return nil, domain.Errorf("cargar el catálogo de agentes: %w", err)
	}
	byID := make(map[agent.ID]agent.Agent, len(agents))
	known := make(map[string]agent.Agent, len(agents))
	for _, a := range agents {
		byID[a.ID()] = a
		known[a.ID().String()] = a
	}

	var chosen []agent.Agent
	if len(cmd.Agents) > 0 {
		for _, id := range cmd.Agents {
			a, ok := known[id]
			if !ok {
				return nil, domain.Errorf("agente %q desconocido; consulta skilus agents: %w", id, domain.ErrNotFound)
			}
			chosen = append(chosen, a)
		}
	} else {
		for _, a := range agents {
			ok, err := h.Detector.Installed(ctx, a)
			if err != nil {
				return nil, domain.Errorf("detectar el agente %s: %w", a.ID(), err)
			}
			if ok {
				chosen = append(chosen, a)
			}
		}
	}

	raw := make([]agent.Target, 0, len(chosen))
	for _, a := range chosen {
		raw = append(raw, agent.Target{Agent: a.ID(), Scope: cmd.Scope, Mode: mode})
	}
	var out []PlannedTarget
	for _, t := range agent.Dedupe(raw, byID) {
		out = append(out, PlannedTarget{Target: t, Dir: skillsDir(byID[t.Agent], t.Scope, h.ProjectRoot)})
	}
	if len(out) == 0 {
		return nil, domain.Errorf("no hay ningún agente elegido ni detectado; usa --agent: %w", domain.ErrInvalid)
	}
	return out, nil
}

// recordedSource keeps project files portable: a local source inside the
// project is recorded relative to its root, with forward slashes.
func (h AddSkillHandler) recordedSource(scope agent.Scope, source string) string {
	if scope != agent.ScopeProject || !filepath.IsAbs(source) || h.ProjectRoot == "" {
		return source
	}
	rel, err := filepath.Rel(h.ProjectRoot, source)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return source
	}
	if rel == "." {
		return "."
	}
	return "./" + filepath.ToSlash(rel)
}

// fetchSource parses raw and reads it with the fetcher for its kind.
func fetchSource(ctx context.Context, fetchers map[source.Kind]Fetcher, raw string) (Fetched, error) {
	src, err := source.Parse(raw)
	if err != nil {
		return Fetched{}, err
	}
	fetcher, ok := fetchers[src.Kind]
	if !ok {
		return Fetched{}, domain.Errorf("los orígenes %s aún no están soportados: %w", src.Kind, domain.ErrInvalid)
	}
	fetched, err := fetcher.Fetch(ctx, src)
	if err != nil {
		return Fetched{}, domain.Errorf("leer el origen %s: %w", raw, err)
	}
	return fetched, nil
}

func selectSkills(fetched Fetched, names []string) ([]FetchedSkill, error) {
	found := fetched.Skills
	if len(found) == 0 {
		if len(fetched.Invalid) > 0 {
			bad := fetched.Invalid[0]
			return nil, domain.Errorf("skill en %s: %w", bad.Path, bad.Err)
		}
		return nil, domain.Errorf("no hay ningún %s en el origen: %w", skill.ManifestFile, domain.ErrNotFound)
	}
	if len(names) == 0 {
		return found, nil
	}
	byName := make(map[string]FetchedSkill, len(found))
	for _, s := range found {
		byName[s.Package.Name().String()] = s
	}
	out := make([]FetchedSkill, 0, len(names))
	for _, n := range names {
		s, ok := byName[n]
		if !ok {
			for _, bad := range fetched.Invalid {
				if path.Base(bad.Path) == n {
					return nil, domain.Errorf("skill en %s: %w", bad.Path, bad.Err)
				}
			}
			return nil, domain.Errorf("la skill %q no está en el origen: %w", n, domain.ErrNotFound)
		}
		out = append(out, s)
	}
	return out, nil
}

// gate applies the rules that do not need the user: blocking findings,
// --strict, and scripts that were not explicitly allowed under --yes.
func gate(plan InstallPlan, cmd AddSkill) error {
	var reasons domain.Errors
	var skills []PlannedSkill
	for _, p := range plan.All() {
		skills = append(skills, p.Skills...)
	}
	for _, s := range skills {
		r := s.Report
		switch {
		case r.Blocking():
			reasons = append(reasons, domain.Errorf("%s: hallazgos que bloquean", s.Package.Name()))
		case cmd.Strict && r.Warnings() > 0:
			reasons = append(reasons, domain.Errorf("%s: %d avisos con --strict", s.Package.Name(), r.Warnings()))
		case cmd.Yes && len(r.Executables) > 0 && !cmd.AllowScripts:
			reasons = append(reasons, domain.Errorf("%s: los ficheros ejecutables necesitan --allow-scripts", s.Package.Name()))
		}
	}
	if len(reasons) > 0 {
		return domain.Errorf("%w: %w", reasons, ErrRejected)
	}
	return nil
}

func (h AddSkillHandler) install(ctx context.Context, cmd AddSkill, fetched Fetched, plan InstallPlan, lf *lock.Lockfile) (installed []lock.Entry, err error) {
	var deployed []string
	defer func() {
		if err == nil {
			return
		}
		for i := len(deployed) - 1; i >= 0; i-- {
			if rmErr := h.Deployer.Remove(ctx, deployed[i]); rmErr != nil {
				err = errors.Join(err, domain.Errorf("deshacer %s: %w", deployed[i], rmErr))
			}
		}
	}()

	targets := make([]agent.Target, 0, len(plan.Targets))
	for _, t := range plan.Targets {
		targets = append(targets, t.Target)
	}

	var manifest []ManifestEntry
	for _, s := range plan.Skills {
		storeDir, err := h.Store.Put(ctx, s.Package)
		if err != nil {
			return nil, domain.Errorf("guardar %s en el almacén: %w", s.Package.Name(), err)
		}
		for _, t := range plan.Targets {
			dest := filepath.Join(t.Dir, s.Package.Name().String())
			if err := h.Deployer.Deploy(ctx, storeDir, dest, t.Target.Mode); err != nil {
				return nil, domain.Errorf("desplegar %s en %s: %w", s.Package.Name(), dest, err)
			}
			deployed = append(deployed, dest)
		}
		e := lock.Entry{
			Skill:       s.Package.Name(),
			Source:      fetched.Source,
			Requested:   fetched.Requested,
			Commit:      fetched.Commit,
			Path:        s.Path,
			TreeHash:    s.Package.TreeHash(),
			Executables: skill.Executables(s.Package.Files()),
			Targets:     targets,
			Requires:    requiredNames(s.Package),
			Dependency:  s.Dependency,
		}
		if err := lf.Install(e); err != nil {
			return nil, err
		}
		installed = append(installed, e)
		if s.Dependency {
			continue // skilus.yaml records what was asked for; requirements follow from it
		}

		var allow []string
		if len(s.Report.Executables) > 0 {
			allow = []string{AllowScripts}
		}
		src := fetched.Source
		if fetched.Requested != "" {
			src += "@" + fetched.Requested
		}
		manifest = append(manifest, ManifestEntry{Name: s.Package.Name(), Source: src, Allow: allow})
	}

	if err := h.Locks.Save(ctx, cmd.Scope, lf); err != nil {
		return nil, domain.Errorf("guardar el lock: %w", err)
	}
	lf.PullEvents()
	for _, m := range manifest {
		if err := h.Manifests.AddSkill(ctx, cmd.Scope, m); err != nil {
			// The lock already records the installation; report without rolling back.
			return installed, domain.Errorf("instalado, pero no se pudo actualizar skilus.yaml: %w", err)
		}
	}
	return installed, nil
}

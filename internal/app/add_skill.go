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
)

// Errors returned by AddSkill besides the domain ones.
var (
	// ErrRejected means the inspection blocked the installation.
	ErrRejected = errors.New("rejected by inspection")
	// ErrCancelled means the user answered no.
	ErrCancelled = errors.New("cancelled")
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
	Skills  []PlannedSkill
	Targets []PlannedTarget
}

// PlannedSkill is one skill with its inspection report.
type PlannedSkill struct {
	Package skill.Package
	Path    string
	Report  policy.Report
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
	Fetcher     Fetcher
	Store       Store
	Deployer    Deployer
	Locks       LockRepository
	Manifests   ManifestRepository
	Prompter    Prompter
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

	fetched, err := h.Fetcher.Fetch(ctx, cmd.Source)
	if err != nil {
		return AddResult{}, fmt.Errorf("read source %s: %w", cmd.Source, err)
	}
	selected, err := selectSkills(fetched.Skills, cmd.Skills)
	if err != nil {
		return AddResult{}, err
	}

	lf, err := h.Locks.Load(ctx, cmd.Scope)
	if err != nil {
		return AddResult{}, fmt.Errorf("load lock: %w", err)
	}

	fetched.Source = h.recordedSource(cmd.Scope, fetched.Source)
	plan := InstallPlan{Source: fetched.Source, Targets: targets}
	for _, s := range selected {
		if _, ok := lf.Entry(s.Package.Name()); ok {
			return AddResult{}, fmt.Errorf("skill %s is already installed; use skilus update: %w", s.Package.Name(), domain.ErrAlreadyExists)
		}
		report := policy.Inspect(s.Package, h.Limits, policy.Allow{Scripts: cmd.AllowScripts})
		plan.Skills = append(plan.Skills, PlannedSkill{Package: s.Package, Path: s.Path, Report: report})
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

	installed, err := h.install(ctx, cmd, fetched, plan, lf)
	if err != nil {
		return AddResult{Plan: plan}, err
	}
	return AddResult{Plan: plan, Installed: installed}, nil
}

func (h AddSkillHandler) targets(ctx context.Context, cmd AddSkill, mode agent.Mode) ([]PlannedTarget, error) {
	agents, err := h.Catalog.Agents(ctx)
	if err != nil {
		return nil, fmt.Errorf("load agent catalog: %w", err)
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
				return nil, fmt.Errorf("unknown agent %q; see skilus agents: %w", id, domain.ErrNotFound)
			}
			chosen = append(chosen, a)
		}
	} else {
		for _, a := range agents {
			ok, err := h.Detector.Installed(ctx, a)
			if err != nil {
				return nil, fmt.Errorf("detect agent %s: %w", a.ID(), err)
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
		dir := byID[t.Agent].Dir(t.Scope)
		if t.Scope == agent.ScopeProject {
			dir = filepath.Join(h.ProjectRoot, filepath.FromSlash(dir))
		}
		out = append(out, PlannedTarget{Target: t, Dir: dir})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no agent selected or detected; pass --agent: %w", domain.ErrInvalid)
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

func selectSkills(found []FetchedSkill, names []string) ([]FetchedSkill, error) {
	if len(found) == 0 {
		return nil, fmt.Errorf("no %s found in the source: %w", skill.ManifestFile, domain.ErrNotFound)
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
			return nil, fmt.Errorf("skill %q is not in the source: %w", n, domain.ErrNotFound)
		}
		out = append(out, s)
	}
	return out, nil
}

// gate applies the rules that do not need the user: blocking findings,
// --strict, and scripts that were not explicitly allowed under --yes.
func gate(plan InstallPlan, cmd AddSkill) error {
	var reasons []string
	for _, s := range plan.Skills {
		r := s.Report
		switch {
		case r.Blocking():
			reasons = append(reasons, fmt.Sprintf("%s: blocking findings", s.Package.Name()))
		case cmd.Strict && r.Warnings() > 0:
			reasons = append(reasons, fmt.Sprintf("%s: %d warnings under --strict", s.Package.Name(), r.Warnings()))
		case cmd.Yes && len(r.Executables) > 0 && !cmd.AllowScripts:
			reasons = append(reasons, fmt.Sprintf("%s: executable files need --allow-scripts", s.Package.Name()))
		}
	}
	if len(reasons) > 0 {
		return fmt.Errorf("%s: %w", strings.Join(reasons, "; "), ErrRejected)
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
				err = errors.Join(err, fmt.Errorf("roll back %s: %w", deployed[i], rmErr))
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
			return nil, fmt.Errorf("store %s: %w", s.Package.Name(), err)
		}
		for _, t := range plan.Targets {
			dest := filepath.Join(t.Dir, s.Package.Name().String())
			if err := h.Deployer.Deploy(ctx, storeDir, dest, t.Target.Mode); err != nil {
				return nil, fmt.Errorf("deploy %s to %s: %w", s.Package.Name(), dest, err)
			}
			deployed = append(deployed, dest)
		}
		e := lock.Entry{
			Skill:     s.Package.Name(),
			Source:    fetched.Source,
			Requested: fetched.Requested,
			Commit:    fetched.Commit,
			Path:      s.Path,
			TreeHash:  s.Package.TreeHash(),
			Targets:   targets,
		}
		if err := lf.Install(e); err != nil {
			return nil, err
		}
		installed = append(installed, e)

		var allow []string
		if len(s.Report.Executables) > 0 {
			allow = []string{AllowScripts}
		}
		manifest = append(manifest, ManifestEntry{Name: s.Package.Name(), Source: fetched.Source, Allow: allow})
	}

	if err := h.Locks.Save(ctx, cmd.Scope, lf); err != nil {
		return nil, fmt.Errorf("save lock: %w", err)
	}
	lf.PullEvents()
	for _, m := range manifest {
		if err := h.Manifests.AddSkill(ctx, cmd.Scope, m); err != nil {
			// The lock already records the installation; report without rolling back.
			return installed, fmt.Errorf("installed, but could not update skilus.yaml: %w", err)
		}
	}
	return installed, nil
}

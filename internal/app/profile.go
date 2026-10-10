package app

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/profile"
	"github.com/colybri/skilus/internal/domain/skill"
)

// ListProfiles is the query behind `skilus profile list`.
type ListProfiles struct {
	Scope agent.Scope
}

// ProfileStatus is one profile and whether the lock holds exactly its
// skills.
type ProfileStatus struct {
	Profile profile.Profile
	Active  bool
	// Undeclared lists skills of the profile that skills: does not
	// declare, so the profile cannot be used until they are added.
	Undeclared []skill.Name
}

// ListProfilesHandler runs ListProfiles.
type ListProfilesHandler struct {
	Manifest ManifestReader
	Locks    LockRepository
}

// Handle returns the profiles in file order. The active profile is not
// stored anywhere: it is the one whose skills, with their requirements,
// are the installed ones.
func (h ListProfilesHandler) Handle(ctx context.Context, q ListProfiles) ([]ProfileStatus, error) {
	m, err := h.Manifest.Manifest(ctx, q.Scope)
	if err != nil {
		return nil, err
	}
	lf, err := h.Locks.Load(ctx, q.Scope)
	if err != nil {
		return nil, fmt.Errorf("load lock: %w", err)
	}
	installed := lockedNames(lf)
	out := make([]ProfileStatus, 0, len(m.Profiles))
	for _, p := range m.Profiles {
		// The profile's skills and what they need are exactly what is installed.
		want := needed(p.Skills(), lf, nil)
		active := len(want) == len(installed)
		for _, n := range installed {
			active = active && want[n]
		}
		out = append(out, ProfileStatus{Profile: p, Active: active, Undeclared: undeclared(m, p)})
	}
	return out, nil
}

// UseProfile is the command behind `skilus profile use`.
type UseProfile struct {
	Name         string
	Scope        agent.Scope
	Mode         agent.Mode // for new targets; empty means the default for the scope
	Yes          bool
	Strict       bool
	AllowScripts bool // on top of the skills whose skilus.yaml entry allows scripts
	DryRun       bool // build and return the plan, change nothing
}

// ProfilePlan is what the user confirms before switching profiles.
type ProfilePlan struct {
	Profile string
	// Install has one plan per source, for skills the lock does not hold.
	Install []InstallPlan
	// Retarget lists installed skills that move to the profile's agents.
	Retarget []Retarget
	// Remove lists installed skills the profile does not include.
	Remove []lock.Entry
	// Keep lists installed skills that stay as they are.
	Keep []lock.Entry
}

// Empty reports whether the scope already matches the profile.
func (p ProfilePlan) Empty() bool {
	return len(p.Install) == 0 && len(p.Retarget) == 0 && len(p.Remove) == 0
}

// Retarget is an installed skill whose agents change.
type Retarget struct {
	Entry   lock.Entry
	Targets []agent.Target // the entry's targets afterwards
	Add     []PlannedTarget
	Drop    []string // directories to delete
}

// UseProfileResult reports what changed.
type UseProfileResult struct {
	Plan       ProfilePlan
	Installed  []lock.Entry
	Retargeted []lock.Entry
	Removed    []lock.Entry
}

// UseProfileHandler runs UseProfile. It installs with the same rules as
// skilus add and reuses sync to redeploy locked content to new agents.
type UseProfileHandler struct {
	Add      AddSkillHandler
	Sync     SyncHandler
	Manifest ManifestReader
	Prompter Prompter
}

// Handle makes the scope hold exactly the profile's skills: it installs
// the missing ones from the sources skilus.yaml declares, moves installed
// ones to the profile's agents when it names any, and removes the rest
// from the agents and the lock. skilus.yaml is not edited, so switching
// back reinstalls the same sources. Nothing is removed unless every
// install succeeded.
func (h UseProfileHandler) Handle(ctx context.Context, cmd UseProfile) (UseProfileResult, error) {
	m, err := h.Manifest.Manifest(ctx, cmd.Scope)
	if err != nil {
		return UseProfileResult{}, err
	}
	p, err := findProfile(m, cmd.Name)
	if err != nil {
		return UseProfileResult{}, err
	}
	if missing := undeclared(m, p); len(missing) > 0 {
		return UseProfileResult{}, fmt.Errorf("profile %s uses skills that skills: in skilus.yaml does not declare: %s: %w", p.Name(), joinNames(missing), domain.ErrInvalid)
	}
	mode := cmd.Mode
	if mode == "" {
		mode = h.Add.DefaultModes[cmd.Scope]
	}
	if _, err := agent.ParseMode(string(mode)); err != nil {
		return UseProfileResult{}, err
	}
	lf, err := h.Add.Locks.Load(ctx, cmd.Scope)
	if err != nil {
		return UseProfileResult{}, fmt.Errorf("load lock: %w", err)
	}

	plan, groups, err := h.plan(ctx, cmd, m, p, mode, lf)
	if err != nil {
		return UseProfileResult{Plan: plan}, err
	}
	if err := profileGate(plan, cmd, m); err != nil {
		return UseProfileResult{Plan: plan}, err
	}
	if cmd.DryRun || plan.Empty() {
		return UseProfileResult{Plan: plan}, nil
	}
	if !cmd.Yes {
		ok, err := h.Prompter.ConfirmProfile(ctx, plan)
		if err != nil {
			return UseProfileResult{Plan: plan}, err
		}
		if !ok {
			return UseProfileResult{Plan: plan}, ErrCancelled
		}
	}
	return h.apply(ctx, cmd, plan, groups, lf)
}

func (h UseProfileHandler) plan(ctx context.Context, cmd UseProfile, m Manifest, p profile.Profile, mode agent.Mode, lf *lock.Lockfile) (ProfilePlan, []Fetched, error) {
	plan := ProfilePlan{Profile: p.Name()}

	// The profile's agents, or the detected ones; only resolved when needed
	// so a profile that only removes skills works without agents.
	var targets []PlannedTarget
	resolve := func() error {
		if targets != nil {
			return nil
		}
		ids := make([]string, 0, len(p.Agents()))
		for _, id := range p.Agents() {
			ids = append(ids, id.String())
		}
		var err error
		targets, err = h.Add.targets(ctx, AddSkill{Agents: ids, Scope: cmd.Scope}, mode)
		return err
	}

	// Skills to install, grouped by the source skilus.yaml records, in
	// the order the profile lists them, plus what they require.
	declared := map[skill.Name]ManifestEntry{}
	for _, e := range m.Skills {
		declared[e.Name] = e
	}
	var order []string
	bySource := map[string][]string{}
	for _, n := range p.Skills() {
		if _, ok := lf.Entry(n); ok {
			continue
		}
		src := declared[n].Source
		if _, ok := bySource[src]; !ok {
			order = append(order, src)
		}
		bySource[src] = append(bySource[src], n.String())
	}
	var groups []planGroup
	if len(order) > 0 {
		if err := resolve(); err != nil {
			return plan, nil, err
		}
	}
	for _, src := range order {
		fetched, err := fetchSource(ctx, h.Add.Fetchers, src)
		if err != nil {
			return plan, nil, err
		}
		selected, err := selectSkills(fetched, bySource[src])
		if err != nil {
			return plan, nil, fmt.Errorf("source %s: %w", src, err)
		}
		untrusted, err := trustFindings(ctx, h.Add.Trust, src, trustScopes(cmd.Scope)...)
		if err != nil {
			return plan, nil, err
		}
		fetched.Source = h.Add.recordedSource(cmd.Scope, fetched.Source)
		ip := InstallPlan{Source: fetched.Source, Commit: fetched.Commit, Targets: targets}
		for _, s := range selected {
			allow := policy.Allow{Scripts: cmd.AllowScripts || allowsScripts(declared[s.Package.Name()])}
			report := policy.Inspect(s.Package, h.Add.Limits, allow)
			report.Findings = append(append([]policy.Finding(nil), untrusted...), report.Findings...)
			ip.Skills = append(ip.Skills, PlannedSkill{Package: s.Package, Path: s.Path, Report: report})
		}
		groups = append(groups, planGroup{raw: src, fetched: fetched, plan: ip})
	}
	if len(groups) > 0 {
		r := h.Add.resolver(cmd.Scope, cmd.AllowScripts, lf)
		r.allowScripts = func(n skill.Name) bool { return cmd.AllowScripts || allowsScripts(declared[n]) }
		var err error
		if groups, err = r.resolve(ctx, groups, targets); err != nil {
			return plan, nil, err
		}
	}
	var fetched []Fetched
	for _, g := range groups {
		plan.Install = append(plan.Install, g.plan)
		fetched = append(fetched, g.fetched)
	}

	// What the profile's skills need stays, whether or not the profile
	// names it.
	keep := needed(p.Skills(), lf, groups)
	for _, e := range lf.Entries() {
		if keep[e.Skill] {
			plan.Keep = append(plan.Keep, e)
		} else {
			plan.Remove = append(plan.Remove, e)
		}
	}

	if len(p.Agents()) > 0 && len(plan.Keep) > 0 {
		if err := resolve(); err != nil {
			return plan, nil, err
		}
		kept := plan.Keep[:0:0]
		for _, e := range plan.Keep {
			r, ok, err := h.retarget(ctx, e, targets)
			if err != nil {
				return plan, nil, err
			}
			if ok {
				plan.Retarget = append(plan.Retarget, r)
			} else {
				kept = append(kept, e)
			}
		}
		plan.Keep = kept
	}
	return plan, fetched, nil
}

// retarget compares where e is deployed with where the profile wants it,
// by directory: agents that share a directory need no new copy.
func (h UseProfileHandler) retarget(ctx context.Context, e lock.Entry, want []PlannedTarget) (Retarget, bool, error) {
	byID, err := agentsByID(ctx, h.Add.Catalog)
	if err != nil {
		return Retarget{}, false, err
	}
	current := map[string]agent.Target{}
	for _, t := range e.Targets {
		a, ok := byID[t.Agent]
		if !ok {
			return Retarget{}, false, fmt.Errorf("skill %s targets agent %s, which is no longer in the catalog: %w", e.Skill, t.Agent, domain.ErrConflict)
		}
		current[skillsDir(a, t.Scope, h.Add.ProjectRoot)] = t
	}
	r := Retarget{Entry: e}
	wanted := map[string]bool{}
	for _, t := range want {
		wanted[t.Dir] = true
		if old, ok := current[t.Dir]; ok {
			r.Targets = append(r.Targets, agent.Target{Agent: t.Target.Agent, Scope: old.Scope, Mode: old.Mode})
			continue
		}
		r.Targets = append(r.Targets, t.Target)
		r.Add = append(r.Add, t)
	}
	for _, t := range e.Targets {
		dir := skillsDir(byID[t.Agent], t.Scope, h.Add.ProjectRoot)
		if !wanted[dir] {
			r.Drop = append(r.Drop, filepath.Join(dir, e.Skill.String()))
		}
	}
	same := len(r.Add) == 0 && len(r.Drop) == 0 && slices.Equal(r.Targets, e.Targets)
	return r, !same, nil
}

// profileGate applies add's rules per skill, where scripts are allowed by
// the flag or by the skill's allow: in skilus.yaml.
func profileGate(plan ProfilePlan, cmd UseProfile, m Manifest) error {
	declared := map[skill.Name]ManifestEntry{}
	for _, e := range m.Skills {
		declared[e.Name] = e
	}
	var reasons []string
	for _, ip := range plan.Install {
		for _, s := range ip.Skills {
			r, name := s.Report, s.Package.Name()
			switch {
			case r.Blocking():
				reasons = append(reasons, fmt.Sprintf("%s: blocking findings", name))
			case cmd.Strict && r.Warnings() > 0:
				reasons = append(reasons, fmt.Sprintf("%s: %d warnings under --strict", name, r.Warnings()))
			case cmd.Yes && len(r.Executables) > 0 && !cmd.AllowScripts && !allowsScripts(declared[name]):
				reasons = append(reasons, fmt.Sprintf("%s: executable files need --allow-scripts or allow: [scripts] in skilus.yaml", name))
			}
		}
	}
	if len(reasons) > 0 {
		return fmt.Errorf("%s: %w", strings.Join(reasons, "; "), ErrRejected)
	}
	return nil
}

func (h UseProfileHandler) apply(ctx context.Context, cmd UseProfile, plan ProfilePlan, groups []Fetched, lf *lock.Lockfile) (UseProfileResult, error) {
	res := UseProfileResult{Plan: plan}
	// Requirements from other sources come last in Install; install them first.
	for i := len(plan.Install) - 1; i >= 0; i-- {
		installed, err := h.Add.install(ctx, AddSkill{Scope: cmd.Scope}, groups[i], plan.Install[i], lf)
		res.Installed = append(res.Installed, installed...)
		if err != nil {
			return res, err
		}
	}

	sources := map[string]Fetched{}
	for _, r := range plan.Retarget {
		e := r.Entry
		if len(r.Add) > 0 {
			storeDir, _, err := h.Sync.content(ctx, e, sources)
			if err != nil {
				return res, fmt.Errorf("skill %s: %w", e.Skill, err)
			}
			for _, t := range r.Add {
				dest := filepath.Join(t.Dir, e.Skill.String())
				if err := h.Add.Deployer.Deploy(ctx, storeDir, dest, t.Target.Mode); err != nil {
					return res, fmt.Errorf("deploy %s to %s: %w", e.Skill, dest, err)
				}
			}
		}
		for _, dir := range r.Drop {
			if err := h.Add.Deployer.Remove(ctx, dir); err != nil {
				return res, fmt.Errorf("remove %s: %w", dir, err)
			}
		}
		e.Targets = r.Targets
		if err := lf.Update(e); err != nil {
			return res, err
		}
		res.Retargeted = append(res.Retargeted, e)
	}

	byID, err := agentsByID(ctx, h.Add.Catalog)
	if err != nil {
		return res, err
	}
	for _, e := range plan.Remove {
		for _, t := range e.Targets {
			a, ok := byID[t.Agent]
			if !ok {
				return res, fmt.Errorf("skill %s targets agent %s, which is no longer in the catalog: %w", e.Skill, t.Agent, domain.ErrConflict)
			}
			dest := filepath.Join(skillsDir(a, t.Scope, h.Add.ProjectRoot), e.Skill.String())
			if err := h.Add.Deployer.Remove(ctx, dest); err != nil {
				return res, fmt.Errorf("remove %s: %w", dest, err)
			}
		}
		if err := lf.Remove(e.Skill); err != nil {
			return res, err
		}
		res.Removed = append(res.Removed, e)
	}
	if err := h.Add.Locks.Save(ctx, cmd.Scope, lf); err != nil {
		return res, fmt.Errorf("save lock: %w", err)
	}
	lf.PullEvents()
	return res, nil
}

func findProfile(m Manifest, name string) (profile.Profile, error) {
	var names []string
	for _, p := range m.Profiles {
		if p.Name() == name {
			return p, nil
		}
		names = append(names, p.Name())
	}
	if len(names) == 0 {
		return profile.Profile{}, fmt.Errorf("skilus.yaml declares no profiles: %w", domain.ErrNotFound)
	}
	return profile.Profile{}, fmt.Errorf("profile %q is not in skilus.yaml; profiles: %s: %w", name, strings.Join(names, ", "), domain.ErrNotFound)
}

func undeclared(m Manifest, p profile.Profile) []skill.Name {
	var out []skill.Name
	for _, n := range p.Skills() {
		if !slices.ContainsFunc(m.Skills, func(e ManifestEntry) bool { return e.Name == n }) {
			out = append(out, n)
		}
	}
	return out
}

func allowsScripts(e ManifestEntry) bool { return slices.Contains(e.Allow, AllowScripts) }

func lockedNames(lf *lock.Lockfile) []skill.Name {
	var out []skill.Name
	for _, e := range lf.Entries() {
		out = append(out, e.Skill)
	}
	return out
}

func joinNames(ns []skill.Name) string {
	ss := make([]string, len(ns))
	for i, n := range ns {
		ss[i] = n.String()
	}
	return strings.Join(ss, ", ")
}

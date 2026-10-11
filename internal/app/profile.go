package app

import (
	"context"
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
		return nil, domain.Errorf("cargar el lock: %w", err)
	}
	out := make([]ProfileStatus, 0, len(m.Profiles))
	for _, p := range m.Profiles {
		out = append(out, ProfileStatus{Profile: p, Active: isActive(p, lf), Undeclared: undeclared(m, p)})
	}
	return out, nil
}

// isActive reports whether the profile's skills and what they need are
// exactly what the lock holds.
func isActive(p profile.Profile, lf *lock.Lockfile) bool {
	want := needed(p.Skills(), lf, nil)
	installed := lockedNames(lf)
	active := len(want) == len(installed)
	for _, n := range installed {
		active = active && want[n]
	}
	return active
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
// ones to the profile's agents, and removes the rest from the agents and
// the lock. A profile with skills and no agents is refused: there is
// nowhere to deploy it. skilus.yaml is not edited, so switching
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
		return UseProfileResult{}, domain.Errorf("el perfil %s usa skills que skills: de skilus.yaml no declara: %s: %w", p.Name(), joinNames(missing), domain.ErrInvalid)
	}
	if len(p.Agents()) == 0 && len(p.Skills()) > 0 {
		return UseProfileResult{}, domain.Errorf("el perfil %s no tiene agentes; añádelos con skilus profile add %s --agent <id>: %w", p.Name(), p.Name(), domain.ErrInvalid)
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
		return UseProfileResult{}, domain.Errorf("cargar el lock: %w", err)
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

	// The profile's agents, only resolved when needed.
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

	// Skills to install, in the order the profile lists them, plus what
	// they require.
	var missing []skill.Name
	for _, n := range p.Skills() {
		if _, ok := lf.Entry(n); !ok {
			missing = append(missing, n)
		}
	}
	var groups []planGroup
	if len(missing) > 0 {
		if err := resolve(); err != nil {
			return plan, nil, err
		}
		var err error
		if groups, err = h.Add.planDeclared(ctx, cmd.Scope, m, missing, targets, cmd.AllowScripts, lf); err != nil {
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

	if len(plan.Keep) > 0 {
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

// planDeclared plans installing the named skills from the sources
// skilus.yaml declares for them, one group per source in the order the
// names come, plus what they require. Scripts are allowed by the flag or
// by the skill's allow: in skilus.yaml.
func (h AddSkillHandler) planDeclared(ctx context.Context, scope agent.Scope, m Manifest, names []skill.Name, targets []PlannedTarget, allowScripts bool, lf *lock.Lockfile) ([]planGroup, error) {
	declared := declaredEntries(m)
	var order []string
	bySource := map[string][]string{}
	for _, n := range names {
		src := declared[n].Source
		if _, ok := bySource[src]; !ok {
			order = append(order, src)
		}
		bySource[src] = append(bySource[src], n.String())
	}
	var groups []planGroup
	for _, src := range order {
		fetched, err := fetchSource(ctx, h.Fetchers, src)
		if err != nil {
			return nil, err
		}
		selected, err := selectSkills(fetched, bySource[src])
		if err != nil {
			return nil, domain.Errorf("origen %s: %w", src, err)
		}
		untrusted, err := trustFindings(ctx, h.Trust, src, trustScopes(scope)...)
		if err != nil {
			return nil, err
		}
		fetched.Source = h.recordedSource(scope, fetched.Source)
		ip := InstallPlan{Source: fetched.Source, Commit: fetched.Commit, Targets: targets}
		for _, s := range selected {
			allow := policy.Allow{Scripts: allowScripts || allowsScripts(declared[s.Package.Name()])}
			report := policy.Inspect(s.Package, h.Limits, allow)
			report.Findings = append(append([]policy.Finding(nil), untrusted...), report.Findings...)
			ip.Skills = append(ip.Skills, PlannedSkill{Package: s.Package, Path: s.Path, Report: report})
		}
		groups = append(groups, planGroup{raw: src, fetched: fetched, plan: ip})
	}
	if len(groups) == 0 {
		return nil, nil
	}
	r := h.resolver(scope, allowScripts, lf)
	r.allowScripts = func(n skill.Name) bool { return allowScripts || allowsScripts(declared[n]) }
	return r.resolve(ctx, groups, targets)
}

// declaredEntries indexes skills: of skilus.yaml by name.
func declaredEntries(m Manifest) map[skill.Name]ManifestEntry {
	out := make(map[skill.Name]ManifestEntry, len(m.Skills))
	for _, e := range m.Skills {
		out[e.Name] = e
	}
	return out
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
			return Retarget{}, false, domain.Errorf("la skill %s apunta al agente %s, que ya no está en el catálogo: %w", e.Skill, t.Agent, domain.ErrConflict)
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
	reasons := declaredGate(plan.Install, cmd.Yes, cmd.Strict, cmd.AllowScripts, m)
	if len(reasons) > 0 {
		return domain.Errorf("%w: %w", reasons, ErrRejected)
	}
	return nil
}

// declaredGate returns why the plans of skills declared in skilus.yaml
// cannot be installed under add's rules, with scripts allowed by the flag
// or by the skill's allow:.
func declaredGate(plans []InstallPlan, yes, strict, allowScripts bool, m Manifest) domain.Errors {
	declared := declaredEntries(m)
	var reasons domain.Errors
	for _, ip := range plans {
		for _, s := range ip.Skills {
			r, name := s.Report, s.Package.Name()
			switch {
			case r.Blocking():
				reasons = append(reasons, domain.Errorf("%s: hallazgos que bloquean", name))
			case strict && r.Warnings() > 0:
				reasons = append(reasons, domain.Errorf("%s: %d avisos con --strict", name, r.Warnings()))
			case yes && len(r.Executables) > 0 && !allowScripts && !allowsScripts(declared[name]):
				reasons = append(reasons, domain.Errorf("%s: los ficheros ejecutables necesitan --allow-scripts o allow: [scripts] en skilus.yaml", name))
			}
		}
	}
	return reasons
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
				return res, domain.Errorf("skill %s: %w", e.Skill, err)
			}
			for _, t := range r.Add {
				dest := filepath.Join(t.Dir, e.Skill.String())
				if err := h.Add.Deployer.Deploy(ctx, storeDir, dest, t.Target.Mode); err != nil {
					return res, domain.Errorf("desplegar %s en %s: %w", e.Skill, dest, err)
				}
			}
		}
		for _, dir := range r.Drop {
			if err := h.Add.Deployer.Remove(ctx, dir); err != nil {
				return res, domain.Errorf("borrar %s: %w", dir, err)
			}
		}
		e.Targets = r.Targets
		if err := lf.Update(e); err != nil {
			return res, err
		}
		res.Retargeted = append(res.Retargeted, e)
	}

	removed, err := removeEntries(ctx, h.Add.Catalog, h.Add.Deployer, h.Add.ProjectRoot, plan.Remove, lf)
	res.Removed = removed
	if err != nil {
		return res, err
	}
	if err := h.Add.Locks.Save(ctx, cmd.Scope, lf); err != nil {
		return res, domain.Errorf("guardar el lock: %w", err)
	}
	lf.PullEvents()
	return res, nil
}

// removeEntries deletes the entries from their agents and from the lock,
// which the caller saves.
func removeEntries(ctx context.Context, c AgentCatalog, d Deployer, root string, entries []lock.Entry, lf *lock.Lockfile) ([]lock.Entry, error) {
	byID, err := agentsByID(ctx, c)
	if err != nil {
		return nil, err
	}
	var removed []lock.Entry
	for _, e := range entries {
		for _, t := range e.Targets {
			a, ok := byID[t.Agent]
			if !ok {
				return removed, domain.Errorf("la skill %s apunta al agente %s, que ya no está en el catálogo: %w", e.Skill, t.Agent, domain.ErrConflict)
			}
			dest := filepath.Join(skillsDir(a, t.Scope, root), e.Skill.String())
			if err := d.Remove(ctx, dest); err != nil {
				return removed, domain.Errorf("borrar %s: %w", dest, err)
			}
		}
		if err := lf.Remove(e.Skill); err != nil {
			return removed, err
		}
		removed = append(removed, e)
	}
	return removed, nil
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
		return profile.Profile{}, domain.Errorf("skilus.yaml no declara ningún perfil: %w", domain.ErrNotFound)
	}
	return profile.Profile{}, domain.Errorf("el perfil %q no está en skilus.yaml; perfiles: %s: %w", name, strings.Join(names, ", "), domain.ErrNotFound)
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

package app

import (
	"context"
	"fmt"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

// maxPlanned bounds how many skills one command installs, dependencies
// included, so a chain of requirements cannot grow without end.
const maxPlanned = 200

// planGroup is the content of one source and what will be installed from it.
type planGroup struct {
	raw     string // the source as given to fetchSource
	fetched Fetched
	plan    InstallPlan
}

// depResolver adds to an install the skills it requires that the lock
// lacks, from the same source or from the one each requirement names.
type depResolver struct {
	fetchers     map[source.Kind]Fetcher
	trust        TrustList
	limits       policy.Limits
	scope        agent.Scope
	allowScripts func(skill.Name) bool
	record       func(string) string // how a fetched source is written to the lock
	lf           *lock.Lockfile
}

// resolve walks the requirements of every planned skill, breadth first,
// and returns the groups with the dependencies added; new sources get new
// groups after the existing ones. A requirement already installed, or
// already planned, is met by name.
func (r depResolver) resolve(ctx context.Context, groups []planGroup, targets []PlannedTarget) ([]planGroup, error) {
	type item struct {
		group int
		skill skill.Name
	}
	planned := map[skill.Name][2]int{} // name -> group, index in plan.Skills
	var queue []item
	for gi, g := range groups {
		for si, s := range g.plan.Skills {
			planned[s.Package.Name()] = [2]int{gi, si}
			queue = append(queue, item{gi, s.Package.Name()})
		}
	}

	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		at := planned[it.skill]
		s := groups[at[0]].plan.Skills[at[1]]
		for _, req := range s.Package.Requires() {
			if _, ok := r.lf.Entry(req.Name); ok {
				continue
			}
			if p, ok := planned[req.Name]; ok {
				dep := &groups[p[0]].plan.Skills[p[1]]
				if dep.Dependency {
					dep.RequiredBy = append(dep.RequiredBy, it.skill)
				}
				continue
			}
			gi := it.group
			if req.Source != "" {
				var err error
				if groups, gi, err = r.group(ctx, groups, req.Source, targets); err != nil {
					return groups, fmt.Errorf("skill %s requires %s: %w", it.skill, req, err)
				}
			}
			found, ok := findSkill(groups[gi].fetched, req.Name)
			if !ok {
				return groups, fmt.Errorf("skill %s requires %s, which is not in %s: %w", it.skill, req.Name, groups[gi].plan.Source, domain.ErrNotFound)
			}
			if len(planned) >= maxPlanned {
				return groups, fmt.Errorf("more than %d skills to install; check the requirements for a loop: %w", maxPlanned, domain.ErrInvalid)
			}
			untrusted, err := trustFindings(ctx, r.trust, groups[gi].raw, trustScopes(r.scope)...)
			if err != nil {
				return groups, err
			}
			report := policy.Inspect(found.Package, r.limits, policy.Allow{Scripts: r.allowScripts(req.Name)})
			report.Findings = append(append([]policy.Finding(nil), untrusted...), report.Findings...)
			g := &groups[gi].plan
			g.Skills = append(g.Skills, PlannedSkill{Package: found.Package, Path: found.Path, Report: report, Dependency: true, RequiredBy: []skill.Name{it.skill}})
			planned[req.Name] = [2]int{gi, len(g.Skills) - 1}
			queue = append(queue, item{gi, req.Name})
		}
	}
	return groups, nil
}

// group returns the index of the group for raw, fetching it the first time.
func (r depResolver) group(ctx context.Context, groups []planGroup, raw string, targets []PlannedTarget) ([]planGroup, int, error) {
	for i, g := range groups {
		if g.raw == raw {
			return groups, i, nil
		}
	}
	src, err := source.Parse(raw)
	if err != nil {
		return groups, 0, err
	}
	if src.Kind == source.KindLocal {
		// A path in someone else's skill would point into this disk.
		return groups, 0, fmt.Errorf("a requirement cannot name a local directory (%s): %w", raw, domain.ErrInvalid)
	}
	fetched, err := fetchSource(ctx, r.fetchers, raw)
	if err != nil {
		return groups, 0, err
	}
	fetched.Source = r.record(fetched.Source)
	groups = append(groups, planGroup{raw: raw, fetched: fetched, plan: InstallPlan{Source: fetched.Source, Commit: fetched.Commit, Targets: targets}})
	return groups, len(groups) - 1, nil
}

func findSkill(f Fetched, n skill.Name) (FetchedSkill, bool) {
	for _, s := range f.Skills {
		if s.Package.Name() == n {
			return s, true
		}
	}
	return FetchedSkill{}, false
}

// requiredNames lists the names a package requires, for the lock.
func requiredNames(p skill.Package) []skill.Name {
	var out []skill.Name
	for _, r := range p.Requires() {
		out = append(out, r.Name)
	}
	return out
}

// needed returns the names in roots plus everything they require,
// following the lock's records and the packages about to be installed.
func needed(roots []skill.Name, lf *lock.Lockfile, planned []planGroup) map[skill.Name]bool {
	requires := map[skill.Name][]skill.Name{}
	for _, e := range lf.Entries() {
		requires[e.Skill] = e.Requires
	}
	for _, g := range planned {
		for _, s := range g.plan.Skills {
			requires[s.Package.Name()] = requiredNames(s.Package)
		}
	}
	out := map[skill.Name]bool{}
	queue := append([]skill.Name(nil), roots...)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if out[n] {
			continue
		}
		out[n] = true
		queue = append(queue, requires[n]...)
	}
	return out
}

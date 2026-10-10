package app

import (
	"context"

	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/source"
)

// Inspect is the query behind `skilus inspect`: the inspection of
// `skilus add` without installing anything.
type Inspect struct {
	Source       string
	Skills       []string // empty means every skill found
	Strict       bool
	AllowScripts bool
}

// InspectHandler runs Inspect. It reads the source and nothing else.
type InspectHandler struct {
	Fetchers map[source.Kind]Fetcher
	Trust    TrustList // nil skips the trust check
	Limits   policy.Limits
}

// Handle returns the plan add would show, without targets. It fails with
// ErrRejected, together with the plan, when add would refuse to install.
func (h InspectHandler) Handle(ctx context.Context, q Inspect) (InstallPlan, error) {
	fetched, err := fetchSource(ctx, h.Fetchers, q.Source)
	if err != nil {
		return InstallPlan{}, err
	}
	selected, err := selectSkills(fetched, q.Skills)
	if err != nil {
		return InstallPlan{}, err
	}
	plan := InstallPlan{Source: fetched.Source, Commit: fetched.Commit}
	if len(q.Skills) == 0 {
		plan.Skipped = fetched.Invalid
	}
	untrusted, err := trustFindings(ctx, h.Trust, q.Source, agent.ScopeProject, agent.ScopeGlobal)
	if err != nil {
		return InstallPlan{}, err
	}
	for _, s := range selected {
		report := policy.Inspect(s.Package, h.Limits, policy.Allow{Scripts: q.AllowScripts})
		report.Findings = append(append([]policy.Finding(nil), untrusted...), report.Findings...)
		plan.Skills = append(plan.Skills, PlannedSkill{Package: s.Package, Path: s.Path, Report: report})
	}
	return plan, gate(plan, AddSkill{Strict: q.Strict, AllowScripts: q.AllowScripts})
}

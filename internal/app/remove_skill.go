package app

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/skill"
)

// RemoveSkill is the command behind `skilus remove`.
type RemoveSkill struct {
	Names []string
	Scope agent.Scope
}

// RemoveSkillHandler runs RemoveSkill.
type RemoveSkillHandler struct {
	Catalog     AgentCatalog
	Deployer    Deployer
	Locks       LockRepository
	Manifests   ManifestRepository
	ProjectRoot string
}

// Handle removes the skills from their agents, the lock and skilus.yaml.
// Every name is checked before anything is touched. Content in the store
// is kept: other projects may use it.
func (h RemoveSkillHandler) Handle(ctx context.Context, cmd RemoveSkill) ([]lock.Entry, error) {
	if len(cmd.Names) == 0 {
		return nil, fmt.Errorf("name at least one skill: %w", domain.ErrInvalid)
	}
	lf, err := h.Locks.Load(ctx, cmd.Scope)
	if err != nil {
		return nil, fmt.Errorf("load lock: %w", err)
	}
	var entries []lock.Entry
	seen := map[string]bool{}
	for _, raw := range cmd.Names {
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
			return nil, fmt.Errorf("skill %s is not installed in the %s scope: %w", n, cmd.Scope, domain.ErrNotFound)
		}
		entries = append(entries, e)
	}

	agents, err := h.Catalog.Agents(ctx)
	if err != nil {
		return nil, fmt.Errorf("load agent catalog: %w", err)
	}
	byID := make(map[agent.ID]agent.Agent, len(agents))
	for _, a := range agents {
		byID[a.ID()] = a
	}

	for _, e := range entries {
		for _, t := range e.Targets {
			a, ok := byID[t.Agent]
			if !ok {
				return nil, fmt.Errorf("skill %s targets agent %s, which is no longer in the catalog: %w", e.Skill, t.Agent, domain.ErrConflict)
			}
			dest := filepath.Join(skillsDir(a, t.Scope, h.ProjectRoot), e.Skill.String())
			if err := h.Deployer.Remove(ctx, dest); err != nil {
				return nil, fmt.Errorf("remove %s: %w", dest, err)
			}
		}
		if err := lf.Remove(e.Skill); err != nil {
			return nil, err
		}
	}
	if err := h.Locks.Save(ctx, cmd.Scope, lf); err != nil {
		return nil, fmt.Errorf("save lock: %w", err)
	}
	lf.PullEvents()
	for _, e := range entries {
		if err := h.Manifests.RemoveSkill(ctx, cmd.Scope, e.Skill); err != nil {
			return entries, fmt.Errorf("removed, but could not update skilus.yaml: %w", err)
		}
	}
	return entries, nil
}

// skillsDir is where an agent keeps its skills for a scope; project
// directories are resolved against the project root.
func skillsDir(a agent.Agent, scope agent.Scope, projectRoot string) string {
	dir := a.Dir(scope)
	if scope == agent.ScopeProject {
		dir = filepath.Join(projectRoot, filepath.FromSlash(dir))
	}
	return dir
}

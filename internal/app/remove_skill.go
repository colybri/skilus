package app

import (
	"context"
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

// Handle removes the skills from their agents, the lock and skilus.yaml,
// together with the dependencies nothing else needs any more. It refuses
// to remove a skill another installed skill requires, unless that one goes
// too. Every name is checked before anything is touched. Content in the
// store is kept: other projects may use it.
func (h RemoveSkillHandler) Handle(ctx context.Context, cmd RemoveSkill) ([]lock.Entry, error) {
	if len(cmd.Names) == 0 {
		return nil, domain.Errorf("indica al menos una skill: %w", domain.ErrInvalid)
	}
	lf, err := h.Locks.Load(ctx, cmd.Scope)
	if err != nil {
		return nil, domain.Errorf("cargar el lock: %w", err)
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
			return nil, domain.Errorf("la skill %s no está instalada en el ámbito %s: %w", n, cmd.Scope, domain.ErrNotFound)
		}
		entries = append(entries, e)
	}
	gone := map[skill.Name]bool{}
	for _, e := range entries {
		gone[e.Skill] = true
	}
	var blocked domain.Errors
	for _, e := range entries {
		if deps := lf.Dependents(e.Skill, gone); len(deps) > 0 {
			blocked = append(blocked, domain.Errorf("%s es necesaria para %s", e.Skill, joinNames(deps)))
		}
	}
	if len(blocked) > 0 {
		return nil, domain.Errorf("%w; quítalas también: %w", blocked, domain.ErrConflict)
	}
	entries = append(entries, lf.Unneeded(gone)...)

	agents, err := h.Catalog.Agents(ctx)
	if err != nil {
		return nil, domain.Errorf("cargar el catálogo de agentes: %w", err)
	}
	byID := make(map[agent.ID]agent.Agent, len(agents))
	for _, a := range agents {
		byID[a.ID()] = a
	}

	for _, e := range entries {
		for _, t := range e.Targets {
			a, ok := byID[t.Agent]
			if !ok {
				return nil, domain.Errorf("la skill %s apunta al agente %s, que ya no está en el catálogo: %w", e.Skill, t.Agent, domain.ErrConflict)
			}
			dest := filepath.Join(skillsDir(a, t.Scope, h.ProjectRoot), e.Skill.String())
			if err := h.Deployer.Remove(ctx, dest); err != nil {
				return nil, domain.Errorf("borrar %s: %w", dest, err)
			}
		}
		if err := lf.Remove(e.Skill); err != nil {
			return nil, err
		}
	}
	if err := h.Locks.Save(ctx, cmd.Scope, lf); err != nil {
		return nil, domain.Errorf("guardar el lock: %w", err)
	}
	lf.PullEvents()
	for _, e := range entries {
		if err := h.Manifests.RemoveSkill(ctx, cmd.Scope, e.Skill); err != nil {
			return entries, domain.Errorf("quitado, pero no se pudo actualizar skilus.yaml: %w", err)
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

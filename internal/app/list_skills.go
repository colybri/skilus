package app

import (
	"context"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
)

// ListSkills is the query behind `skilus list`.
type ListSkills struct {
	Locks LockRepository
}

// InstalledSkill is one lock entry and the scope whose lock holds it.
type InstalledSkill struct {
	Scope agent.Scope
	Entry lock.Entry
}

// Handle returns the skills of the given scopes, in that order and sorted
// by name within each scope.
func (q ListSkills) Handle(ctx context.Context, scopes ...agent.Scope) ([]InstalledSkill, error) {
	var out []InstalledSkill
	for _, s := range scopes {
		lf, err := q.Locks.Load(ctx, s)
		if err != nil {
			return nil, domain.Errorf("cargar el lock del ámbito %s: %w", s, err)
		}
		for _, e := range lf.Entries() {
			out = append(out, InstalledSkill{Scope: s, Entry: e})
		}
	}
	return out, nil
}

package app

import (
	"context"
	"path/filepath"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

// declaredSources returns the source skills: of the scope's skilus.yaml
// gives each skill. A nil reader declares nothing.
func declaredSources(ctx context.Context, r ManifestReader, scope agent.Scope) (map[skill.Name]string, error) {
	if r == nil {
		return nil, nil
	}
	m, err := r.Manifest(ctx, scope)
	if err != nil {
		return nil, domain.Errorf("leer skilus.yaml: %w", err)
	}
	out := make(map[skill.Name]string, len(m.Skills))
	for _, s := range m.Skills {
		out[s.Name] = s.Source
	}
	return out, nil
}

// changedSource returns the source skilus.yaml declares for e when it is
// not the one in the lock: another origin or, for Git, another ref. Local
// paths are compared once made absolute against root, as the lock keeps
// them. Dependencies are not in skilus.yaml and never change.
func changedSource(declared map[skill.Name]string, e lock.Entry, root string) (source.Source, bool, error) {
	raw, ok := declared[e.Skill]
	if !ok || e.Dependency {
		return source.Source{}, false, nil
	}
	src, err := source.Parse(raw)
	if err != nil {
		return source.Source{}, false, domain.Errorf("skilus.yaml, skill %s: %w", e.Skill, err)
	}
	id, locked := src.ID, e.Source
	if src.Kind == source.KindLocal {
		id, locked = localPath(id, root), localPath(locked, root)
	}
	changed := id != locked || (src.Kind == source.KindGit && src.Ref != e.Requested)
	return src, changed, nil
}

// localPath makes a local source absolute against root.
func localPath(p, root string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	return filepath.Clean(p)
}

// lockedSource renders where the lock says e comes from, as skilus.yaml
// would write it.
func lockedSource(e lock.Entry) string {
	if e.Requested == "" {
		return e.Source
	}
	return e.Source + "@" + e.Requested
}

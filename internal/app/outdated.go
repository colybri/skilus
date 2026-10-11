package app

import (
	"context"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

// Outdated is the query behind `skilus update --check`.
type Outdated struct {
	Scopes []agent.Scope
	Names  []string // skills to check; empty means all of them
}

// Freshness says whether a skill's requested ref has moved.
type Freshness string

// Freshness values.
const (
	FreshCurrent  Freshness = "current"  // the ref still points to the locked commit
	FreshOutdated Freshness = "outdated" // the ref points to a newer commit
	FreshPinned   Freshness = "pinned"   // the ref is a commit, which never moves
	FreshLocal    Freshness = "local"    // local directories have no commits
	FreshUnknown  Freshness = "unknown"  // the ref could not be resolved; see Err
	// FreshChanged: skilus.yaml declares another source or ref, which
	// skilus update will move to.
	FreshChanged Freshness = "changed"
)

// SkillFreshness is one skill and where its ref points now.
type SkillFreshness struct {
	Scope    agent.Scope
	Entry    lock.Entry
	Latest   string // commit the requested ref points to now
	State    Freshness
	Declared string // for FreshChanged, the source skilus.yaml declares
	Err      error
}

// OutdatedHandler runs Outdated. It reads refs, never content.
type OutdatedHandler struct {
	Resolver RefResolver
	Locks    LockRepository
	// Manifest reveals skills whose source changed in skilus.yaml; nil
	// skips that check.
	Manifest    ManifestReader
	ProjectRoot string
}

// Handle checks every Git skill in the scopes. Skills of one repository and
// ref share a single lookup. A ref that cannot be resolved is reported, not
// returned as an error, so one gone repository does not hide the rest.
func (h OutdatedHandler) Handle(ctx context.Context, q Outdated) ([]SkillFreshness, error) {
	wanted := map[skill.Name]bool{}
	for _, raw := range q.Names {
		n, err := skill.NewName(raw)
		if err != nil {
			return nil, err
		}
		wanted[n] = false
	}
	var out []SkillFreshness
	resolved := map[string]SkillFreshness{}
	for _, scope := range q.Scopes {
		lf, err := h.Locks.Load(ctx, scope)
		if err != nil {
			return nil, domain.Errorf("cargar el lock del ámbito %s: %w", scope, err)
		}
		declared, err := declaredSources(ctx, h.Manifest, scope)
		if err != nil {
			return nil, err
		}
		for _, e := range lf.Entries() {
			if len(q.Names) > 0 {
				if _, ok := wanted[e.Skill]; !ok {
					continue
				}
				wanted[e.Skill] = true
			}
			f := SkillFreshness{Scope: scope, Entry: e}
			src, err := source.Parse(e.Source)
			_, changed, cerr := changedSource(declared, e, h.ProjectRoot)
			switch {
			case err != nil:
				f.State, f.Err = FreshUnknown, err
			case cerr != nil:
				f.State, f.Err = FreshUnknown, cerr
			case changed:
				f.State, f.Declared = FreshChanged, declared[e.Skill]
			case src.Kind == source.KindLocal || e.Commit == "":
				f.State = FreshLocal
			case e.Requested == e.Commit:
				f.State, f.Latest = FreshPinned, e.Commit
			default:
				src.Ref = e.Requested
				key := src.String()
				r, ok := resolved[key]
				if !ok {
					r.Latest, r.Err = h.Resolver.Resolve(ctx, src)
					resolved[key] = r
				}
				f.Latest, f.Err = r.Latest, r.Err
				switch {
				case f.Err != nil:
					f.State = FreshUnknown
				case f.Latest == e.Commit:
					f.State = FreshCurrent
				default:
					f.State = FreshOutdated
				}
			}
			out = append(out, f)
		}
	}
	for _, raw := range q.Names {
		if n, _ := skill.NewName(raw); !wanted[n] {
			return nil, domain.Errorf("la skill %s no está instalada: %w", n, domain.ErrNotFound)
		}
	}
	return out, nil
}

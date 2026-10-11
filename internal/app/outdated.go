package app

import (
	"context"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/source"
)

// Outdated is the query behind `skilus outdated`.
type Outdated struct {
	Scopes []agent.Scope
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
)

// SkillFreshness is one skill and where its ref points now.
type SkillFreshness struct {
	Scope  agent.Scope
	Entry  lock.Entry
	Latest string // commit the requested ref points to now
	State  Freshness
	Err    error
}

// OutdatedHandler runs Outdated. It reads refs, never content.
type OutdatedHandler struct {
	Resolver RefResolver
	Locks    LockRepository
}

// Handle checks every Git skill in the scopes. Skills of one repository and
// ref share a single lookup. A ref that cannot be resolved is reported, not
// returned as an error, so one gone repository does not hide the rest.
func (h OutdatedHandler) Handle(ctx context.Context, q Outdated) ([]SkillFreshness, error) {
	var out []SkillFreshness
	resolved := map[string]SkillFreshness{}
	for _, scope := range q.Scopes {
		lf, err := h.Locks.Load(ctx, scope)
		if err != nil {
			return nil, domain.Errorf("cargar el lock del ámbito %s: %w", scope, err)
		}
		for _, e := range lf.Entries() {
			f := SkillFreshness{Scope: scope, Entry: e}
			src, err := source.Parse(e.Source)
			switch {
			case err != nil:
				f.State, f.Err = FreshUnknown, err
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
	return out, nil
}

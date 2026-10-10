package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/source"
)

// Search is the query behind `skilus search`.
type Search struct {
	Query string
	Owner string // only skills of this GitHub owner; empty means any
	Limit int    // per origin; 0 means DefaultSearchLimit
	Scope agent.Scope
	// NoRegistry skips the public registry and reads only the indexes.
	NoRegistry bool
}

// DefaultSearchLimit caps the results of each origin.
const DefaultSearchLimit = 20

// Found is one search result.
type Found struct {
	Listing
	Origin string // the registry name or the index URL
	// Trusted is nil when no trust: list applies, so nothing is checked.
	Trusted *bool
}

// SearchResult holds what every origin returned and the origins that
// failed, so one unreachable index does not hide the rest.
type SearchResult struct {
	Found  []Found
	Failed []OriginError
}

// OriginError is an origin that could not be searched.
type OriginError struct {
	Origin string
	Err    error
}

// SearchHandler runs Search.
type SearchHandler struct {
	Registry Registry // nil disables the registry
	Indexes  IndexReader
	Manifest ManifestReader
	Trust    TrustList
}

// Handle searches the registry and the indexes declared in skilus.yaml (a
// project's and the global one). It fails only when every origin failed.
func (h SearchHandler) Handle(ctx context.Context, q Search) (SearchResult, error) {
	terms := strings.Fields(strings.ToLower(q.Query))
	if len(terms) == 0 {
		return SearchResult{}, fmt.Errorf("search needs at least one word: %w", domain.ErrInvalid)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultSearchLimit
	}

	var urls []string
	seen := map[string]bool{}
	var trust policy.Trust
	for _, s := range trustScopes(q.Scope) {
		m, err := h.Manifest.Manifest(ctx, s)
		if err != nil {
			return SearchResult{}, fmt.Errorf("read skilus.yaml of the %s scope: %w", s, err)
		}
		for _, u := range m.Indexes {
			if !seen[u] {
				seen[u] = true
				urls = append(urls, u)
			}
		}
		if h.Trust != nil {
			t, err := h.Trust.Trust(ctx, s)
			if err != nil {
				return SearchResult{}, fmt.Errorf("read trust: of the %s scope: %w", s, err)
			}
			trust = append(trust, t...)
		}
	}

	var res SearchResult
	origins := 0
	if h.Registry != nil && !q.NoRegistry {
		origins++
		found, err := h.Registry.Search(ctx, q.Query, q.Owner, limit)
		if err != nil {
			res.Failed = append(res.Failed, OriginError{Origin: h.Registry.Name(), Err: err})
		}
		for _, l := range found {
			res.Found = append(res.Found, Found{Listing: l, Origin: h.Registry.Name()})
		}
	}
	for _, u := range urls {
		origins++
		listings, err := h.Indexes.Index(ctx, u)
		if err != nil {
			res.Failed = append(res.Failed, OriginError{Origin: u, Err: err})
			continue
		}
		var matched []Found
		for _, l := range listings {
			if matches(l, terms, q.Owner) {
				matched = append(matched, Found{Listing: l, Origin: u})
			}
		}
		if len(matched) > limit {
			matched = matched[:limit]
		}
		res.Found = append(res.Found, matched...)
	}
	if origins == 0 {
		return res, fmt.Errorf("nothing to search: the registry is off and skilus.yaml declares no indexes: %w", domain.ErrInvalid)
	}
	if len(res.Failed) == origins {
		return res, fmt.Errorf("no origin could be searched: %s: %w", res.Failed[0].Origin, res.Failed[0].Err)
	}

	for i := range res.Found {
		res.Found[i].Trusted = trusted(res.Found[i].Source, trust)
	}
	// Curated indexes first, as they were chosen by the team; then by installs.
	sort.SliceStable(res.Found, func(i, j int) bool {
		a, b := res.Found[i], res.Found[j]
		ai, bi := h.Registry != nil && a.Origin == h.Registry.Name(), h.Registry != nil && b.Origin == h.Registry.Name()
		if ai != bi {
			return !ai
		}
		return a.Installs > b.Installs
	})
	return res, nil
}

// matches requires every term in the name, the description or the source.
func matches(l Listing, terms []string, owner string) bool {
	if owner != "" {
		src, err := source.Parse(l.Source)
		if err != nil || !strings.HasPrefix(strings.ToLower(src.ID), "github.com/"+strings.ToLower(owner)+"/") {
			return false
		}
	}
	text := strings.ToLower(l.Name + " " + l.Description + " " + l.Source)
	for _, t := range terms {
		if !strings.Contains(text, t) {
			return false
		}
	}
	return true
}

func trusted(raw string, t policy.Trust) *bool {
	if len(t) == 0 {
		return nil
	}
	ok := false
	if src, err := source.Parse(raw); err == nil {
		ok = src.Kind == source.KindLocal || t.Covers(src.ID)
	}
	return &ok
}

package app

import (
	"context"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/source"
)

// trustScopes are the skilus.yaml files whose trust: lists apply to a
// scope: a project trusts its own list and the global one.
func trustScopes(scope agent.Scope) []agent.Scope {
	if scope == agent.ScopeGlobal {
		return []agent.Scope{agent.ScopeGlobal}
	}
	return []agent.Scope{agent.ScopeProject, agent.ScopeGlobal}
}

// trustFindings checks a source against the trust: lists of the scopes.
// Local directories are the user's own and are never checked, and with no
// list at all nothing is.
func trustFindings(ctx context.Context, tl TrustList, raw string, scopes ...agent.Scope) ([]policy.Finding, error) {
	if tl == nil {
		return nil, nil
	}
	src, err := source.Parse(raw)
	if err != nil || src.Kind == source.KindLocal {
		return nil, nil //nolint:nilerr // the caller reports parse errors
	}
	var trust policy.Trust
	for _, s := range scopes {
		t, err := tl.Trust(ctx, s)
		if err != nil {
			return nil, domain.Errorf("leer trust: del ámbito %s: %w", s, err)
		}
		trust = append(trust, t...)
	}
	return policy.CheckSource(src.ID, trust), nil
}

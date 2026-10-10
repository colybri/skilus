package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/policy"
)

type fakeRegistry struct {
	listings []app.Listing
	err      error
	queries  []string
}

func (f *fakeRegistry) Name() string { return "skills.sh" }

func (f *fakeRegistry) Search(_ context.Context, q, _ string, _ int) ([]app.Listing, error) {
	f.queries = append(f.queries, q)
	return f.listings, f.err
}

type fakeIndexes map[string][]app.Listing

func (f fakeIndexes) Index(_ context.Context, u string) ([]app.Listing, error) {
	l, ok := f[u]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return l, nil
}

type scopedManifest map[agent.Scope]app.Manifest

func (f scopedManifest) Manifest(_ context.Context, s agent.Scope) (app.Manifest, error) {
	return f[s], nil
}

type fakeTrust map[agent.Scope]policy.Trust

func (f fakeTrust) Trust(_ context.Context, s agent.Scope) (policy.Trust, error) { return f[s], nil }

func TestSearchMergesRegistryAndIndexes(t *testing.T) {
	reg := &fakeRegistry{listings: []app.Listing{
		{Name: "pdf", Source: "someone/pdf-tools", Installs: 5},
		{Name: "pdf-pro", Source: "anthropics/skills", Installs: 900},
	}}
	h := app.SearchHandler{
		Registry: reg,
		Indexes: fakeIndexes{
			"https://team/index.yaml": {
				{Name: "pdf", Source: "anthropics/skills@v1", Description: "Lee PDF"},
				{Name: "docx", Source: "anthropics/skills", Description: "Word"},
			},
		},
		Manifest: scopedManifest{
			agent.ScopeProject: {Indexes: []string{"https://team/index.yaml"}},
			agent.ScopeGlobal:  {Indexes: []string{"https://team/index.yaml", "https://gone/index.yaml"}},
		},
		Trust: fakeTrust{agent.ScopeGlobal: {"github.com/anthropics"}},
	}
	res, err := h.Handle(context.Background(), app.Search{Query: "PDF", Scope: agent.ScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 1 || res.Failed[0].Origin != "https://gone/index.yaml" {
		t.Fatalf("failed = %+v", res.Failed)
	}
	var got []string
	for _, f := range res.Found {
		trust := "-"
		if f.Trusted != nil && *f.Trusted {
			trust = "yes"
		} else if f.Trusted != nil {
			trust = "no"
		}
		got = append(got, f.Name+"@"+f.Origin+":"+trust)
	}
	want := []string{"pdf@https://team/index.yaml:yes", "pdf-pro@skills.sh:yes", "pdf@skills.sh:no"}
	if len(got) != len(want) {
		t.Fatalf("found %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("found %v, want %v", got, want)
		}
	}
}

func TestSearchFailures(t *testing.T) {
	ctx := context.Background()
	h := app.SearchHandler{Registry: &fakeRegistry{err: errors.New("offline")}, Indexes: fakeIndexes{}, Manifest: scopedManifest{}}
	if _, err := h.Handle(ctx, app.Search{Query: "  "}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("empty query: %v", err)
	}
	if _, err := h.Handle(ctx, app.Search{Query: "x"}); err == nil {
		t.Error("every origin failed but no error")
	}
	if _, err := h.Handle(ctx, app.Search{Query: "x", NoRegistry: true}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("no origins: %v", err)
	}
}

func TestSearchOwnerFiltersIndexes(t *testing.T) {
	h := app.SearchHandler{
		Indexes:  fakeIndexes{"i": {{Name: "a", Source: "anthropics/skills"}, {Name: "a", Source: "other/skills"}}},
		Manifest: scopedManifest{agent.ScopeProject: {Indexes: []string{"i"}}},
	}
	res, err := h.Handle(context.Background(), app.Search{Query: "a", Owner: "Anthropics", NoRegistry: true, Scope: agent.ScopeProject})
	if err != nil || len(res.Found) != 1 || res.Found[0].Source != "anthropics/skills" || res.Found[0].Trusted != nil {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

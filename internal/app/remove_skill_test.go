package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
)

func TestListAndRemove(t *testing.T) {
	f := newFixture(t,
		app.FetchedSkill{Path: "a", Package: pkg(t, "alpha")},
		app.FetchedSkill{Path: "b", Package: pkg(t, "beta")},
	)
	ctx := context.Background()
	if _, err := f.handler.Handle(ctx, app.AddSkill{Source: "./skills", Scope: agent.ScopeProject, Agents: []string{"codex", "claude-code"}, Yes: true}); err != nil {
		t.Fatal(err)
	}

	list := app.ListSkills{Locks: f.locks}
	got, err := list.Handle(ctx, agent.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Entry.Skill.String() != "alpha" || got[0].Scope != agent.ScopeProject {
		t.Fatalf("list = %+v", got)
	}

	rm := app.RemoveSkillHandler{
		Catalog:     f.handler.Catalog,
		Deployer:    f.deployer,
		Locks:       f.locks,
		Manifests:   f.manifests,
		ProjectRoot: "/repo",
	}
	for label, tt := range map[string]struct {
		names []string
		want  error
	}{
		"none":          {nil, domain.ErrInvalid},
		"bad name":      {[]string{"Alpha!"}, domain.ErrInvalid},
		"not installed": {[]string{"alpha", "gamma"}, domain.ErrNotFound},
	} {
		if _, err := rm.Handle(ctx, app.RemoveSkill{Names: tt.names, Scope: agent.ScopeProject}); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", label, err, tt.want)
		}
	}
	if len(f.deployer.removed) != 0 {
		t.Fatalf("a failed remove touched the disk: %v", f.deployer.removed)
	}

	removed, err := rm.Handle(ctx, app.RemoveSkill{Names: []string{"alpha", "alpha"}, Scope: agent.ScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	// newAgent gives every test agent the same project dir, so codex and
	// claude-code share one target.
	want := filepath.Join("/repo", ".agents", "skills", "alpha")
	if len(removed) != 1 || len(f.deployer.removed) != 1 || f.deployer.removed[0] != want {
		t.Fatalf("removed = %+v, paths = %v", removed, f.deployer.removed)
	}
	if len(f.manifests.removed) != 1 || f.manifests.removed[0] != "alpha" {
		t.Fatalf("manifest removed = %v", f.manifests.removed)
	}
	got, err = list.Handle(ctx, agent.ScopeProject)
	if err != nil || len(got) != 1 || got[0].Entry.Skill.String() != "beta" {
		t.Fatalf("list after remove = %+v, %v", got, err)
	}
}

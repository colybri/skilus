package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/skill"
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

func TestAddInstallsRequirementsAndRemoveTakesThem(t *testing.T) {
	needs := func(n, reqs string) skill.Package {
		r, err := skill.ParseRequires(reqs)
		if err != nil {
			t.Fatal(err)
		}
		p, err := pkg(t, n).WithRequires(r)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	f := newFixture(t,
		app.FetchedSkill{Path: "report", Package: needs("report", "pdf")},
		app.FetchedSkill{Path: "pdf", Package: needs("pdf", "fonts")},
		app.FetchedSkill{Path: "fonts", Package: pkg(t, "fonts")},
	)
	ctx := context.Background()
	res, err := f.handler.Handle(ctx, app.AddSkill{Source: "./skills", Scope: agent.ScopeProject, Skills: []string{"report"}, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Installed) != 3 {
		t.Fatalf("installed = %+v", res.Installed)
	}
	if len(f.manifests.entries) != 1 || f.manifests.entries[0].Name.String() != "report" {
		t.Fatalf("manifest = %+v; dependencies stay out of it", f.manifests.entries)
	}
	pdf, _ := f.locks.lf.Entry(pkg(t, "pdf").Name())
	if !pdf.Dependency || len(pdf.Requires) != 1 || pdf.Requires[0].String() != "fonts" {
		t.Fatalf("pdf = %+v", pdf)
	}

	rm := app.RemoveSkillHandler{Catalog: f.handler.Catalog, Deployer: f.deployer, Locks: f.locks, Manifests: f.manifests, ProjectRoot: "/repo"}
	if _, err := rm.Handle(ctx, app.RemoveSkill{Names: []string{"fonts"}, Scope: agent.ScopeProject}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("removing a requirement: err = %v, want ErrConflict", err)
	}
	removed, err := rm.Handle(ctx, app.RemoveSkill{Names: []string{"report"}, Scope: agent.ScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 3 || len(f.locks.lf.Entries()) != 0 {
		t.Fatalf("removed = %+v, left = %+v", removed, f.locks.lf.Entries())
	}
}

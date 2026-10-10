package app_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/profile"
	"github.com/colybri/skilus/internal/domain/skill"
)

// memProfiles is a manifest whose profiles the handler can edit.
type memProfiles struct{ m app.Manifest }

func (f *memProfiles) Manifest(context.Context, agent.Scope) (app.Manifest, error) { return f.m, nil }

func (f *memProfiles) SaveProfile(_ context.Context, _ agent.Scope, p profile.Profile) error {
	i := slices.IndexFunc(f.m.Profiles, func(q profile.Profile) bool { return q.Name() == p.Name() })
	if i < 0 {
		f.m.Profiles = append(f.m.Profiles, p)
	} else {
		f.m.Profiles[i] = p
	}
	return nil
}

func (f *memProfiles) DeleteProfile(_ context.Context, _ agent.Scope, name string) error {
	f.m.Profiles = slices.DeleteFunc(f.m.Profiles, func(q profile.Profile) bool { return q.Name() == name })
	return nil
}

func editFixture(t *testing.T) (app.EditProfileHandler, *memProfiles) {
	t.Helper()
	store := &memProfiles{}
	for _, s := range []string{"a", "b", "c"} {
		n, _ := skill.NewName(s)
		store.m.Skills = append(store.m.Skills, app.ManifestEntry{Name: n, Source: "./skills"})
	}
	store.m.Profiles = []profile.Profile{mustProfile(t, "web", []string{"a"}, "codex")}
	cat := fakeCatalog{agents: []agent.Agent{agentAt(t, "claude-code", ".claude/skills"), agentAt(t, "codex", ".agents/skills")}}
	return app.EditProfileHandler{Manifest: store, Profiles: store, Catalog: cat}, store
}

func profileNames(p profile.Profile) (skills, agents []string) {
	for _, n := range p.Skills() {
		skills = append(skills, n.String())
	}
	for _, id := range p.Agents() {
		agents = append(agents, id.String())
	}
	return skills, agents
}

func TestCreateProfile(t *testing.T) {
	ctx := context.Background()
	h, store := editFixture(t)

	p, err := h.Create(ctx, app.CreateProfile{Name: "backend", Skills: []string{"a", "b"}, Agents: []string{"claude-code"}})
	if err != nil {
		t.Fatal(err)
	}
	if s, a := profileNames(p); !slices.Equal(s, []string{"a", "b"}) || !slices.Equal(a, []string{"claude-code"}) {
		t.Errorf("created %v %v", s, a)
	}
	if len(store.m.Profiles) != 2 {
		t.Errorf("profiles = %d, want 2", len(store.m.Profiles))
	}

	cases := []struct {
		cmd  app.CreateProfile
		want error
	}{
		{app.CreateProfile{Name: "web"}, domain.ErrAlreadyExists},
		{app.CreateProfile{Name: "ops", Skills: []string{"nope"}}, domain.ErrInvalid},
		{app.CreateProfile{Name: "ops", Agents: []string{"emacs"}}, domain.ErrInvalid},
		{app.CreateProfile{Name: "Ops"}, domain.ErrInvalid},
		{app.CreateProfile{Name: "ops", Skills: []string{"a", "a"}}, domain.ErrInvalid},
	}
	for _, c := range cases {
		if _, err := h.Create(ctx, c.cmd); !errors.Is(err, c.want) {
			t.Errorf("Create(%+v) = %v, want %v", c.cmd, err, c.want)
		}
	}
	if len(store.m.Profiles) != 2 {
		t.Errorf("failed creates changed the manifest: %d profiles", len(store.m.Profiles))
	}
}

func TestChangeProfile(t *testing.T) {
	ctx := context.Background()
	h, store := editFixture(t)

	p, err := h.Change(ctx, app.ChangeProfile{Name: "web", AddSkills: []string{"a", "c"}, AddAgents: []string{"claude-code"}, RemoveAgents: []string{"codex"}})
	if err != nil {
		t.Fatal(err)
	}
	if s, a := profileNames(p); !slices.Equal(s, []string{"a", "c"}) || !slices.Equal(a, []string{"claude-code"}) {
		t.Errorf("changed %v %v", s, a)
	}
	p, err = h.Change(ctx, app.ChangeProfile{Name: "web", RemoveSkills: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := profileNames(store.m.Profiles[0]); !slices.Equal(s, []string{"c"}) || p.Name() != "web" {
		t.Errorf("after remove: %v", s)
	}

	cases := []struct {
		cmd  app.ChangeProfile
		want error
	}{
		{app.ChangeProfile{Name: "mobile", AddSkills: []string{"a"}}, domain.ErrNotFound},
		{app.ChangeProfile{Name: "web", RemoveSkills: []string{"b"}}, domain.ErrNotFound},
		{app.ChangeProfile{Name: "web", RemoveAgents: []string{"codex"}}, domain.ErrNotFound},
		{app.ChangeProfile{Name: "web", AddSkills: []string{"nope"}}, domain.ErrInvalid},
		{app.ChangeProfile{Name: "web", AddAgents: []string{"emacs"}}, domain.ErrInvalid},
	}
	for _, c := range cases {
		if _, err := h.Change(ctx, c.cmd); !errors.Is(err, c.want) {
			t.Errorf("Change(%+v) = %v, want %v", c.cmd, err, c.want)
		}
	}
}

func TestDeleteProfile(t *testing.T) {
	ctx := context.Background()
	h, store := editFixture(t)
	if err := h.Delete(ctx, app.DeleteProfile{Name: "mobile"}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("delete missing = %v, want ErrNotFound", err)
	}
	if err := h.Delete(ctx, app.DeleteProfile{Name: "web"}); err != nil {
		t.Fatal(err)
	}
	if len(store.m.Profiles) != 0 {
		t.Errorf("profiles left: %d", len(store.m.Profiles))
	}
}

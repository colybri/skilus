package yamlrepo_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/colybri/skilus/internal/adapter/yamlrepo"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/profile"
	"github.com/colybri/skilus/internal/domain/skill"
)

func mustProfile(t *testing.T, name string, skills []string, agents ...string) profile.Profile {
	t.Helper()
	var ns []skill.Name
	for _, s := range skills {
		n, err := skill.NewName(s)
		if err != nil {
			t.Fatal(err)
		}
		ns = append(ns, n)
	}
	var ids []agent.ID
	for _, a := range agents {
		id, err := agent.NewID(a)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	p, err := profile.New(name, ns, ids)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSaveAndDeleteProfileKeepUserContent(t *testing.T) {
	ctx := context.Background()
	repo := yamlrepo.Repo{ProjectRoot: t.TempDir()}
	path := filepath.Join(repo.ProjectRoot, yamlrepo.ManifestFile)
	original := `# Mis skills
version: 1
skills:
  - name: a
    source: ./skills
  - name: b
    source: ./skills
profiles:
  # El del equipo de API
  backend:
    skills:
      - a # la primera
    agents: [claude-code]
  web:
    skills: [b]
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		do   func() error
		want string
	}{
		{
			do: func() error {
				return repo.SaveProfile(ctx, agent.ScopeProject, mustProfile(t, "backend", []string{"a", "b"}))
			},
			want: `# Mis skills
version: 1
skills:
  - name: a
    source: ./skills
  - name: b
    source: ./skills
profiles:
  # El del equipo de API
  backend:
    skills:
      - a # la primera
      - b
  web:
    skills: [b]
`,
		},
		{
			do: func() error {
				return repo.SaveProfile(ctx, agent.ScopeProject, mustProfile(t, "ops", nil, "codex", "cursor"))
			},
			want: `# Mis skills
version: 1
skills:
  - name: a
    source: ./skills
  - name: b
    source: ./skills
profiles:
  # El del equipo de API
  backend:
    skills:
      - a # la primera
      - b
  web:
    skills: [b]
  ops:
    skills: []
    agents: [codex, cursor]
`,
		},
		{
			do: func() error { return repo.DeleteProfile(ctx, agent.ScopeProject, "web") },
			want: `# Mis skills
version: 1
skills:
  - name: a
    source: ./skills
  - name: b
    source: ./skills
profiles:
  # El del equipo de API
  backend:
    skills:
      - a # la primera
      - b
  ops:
    skills: []
    agents: [codex, cursor]
`,
		},
	}
	for i, s := range steps {
		if err := s.do(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != s.want {
			t.Fatalf("step %d:\n%s\nwant:\n%s", i, data, s.want)
		}
	}

	m, err := repo.Manifest(ctx, agent.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Profiles) != 2 || m.Profiles[1].Name() != "ops" || len(m.Profiles[1].Agents()) != 2 {
		t.Errorf("profiles read back = %+v", m.Profiles)
	}
}

func TestSaveProfileCreatesTheSection(t *testing.T) {
	ctx := context.Background()
	repo := yamlrepo.Repo{ProjectRoot: t.TempDir(), GlobalDir: t.TempDir()}
	path := filepath.Join(repo.ProjectRoot, yamlrepo.ManifestFile)
	if err := os.WriteFile(path, []byte("version: 1\nprofiles:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProfile(ctx, agent.ScopeProject, mustProfile(t, "web", []string{"ui"}, "codex")); err != nil {
		t.Fatal(err)
	}
	// Dropping the agents removes the key: the profile goes to the detected agents.
	if err := repo.SaveProfile(ctx, agent.ScopeProject, mustProfile(t, "web", []string{"ui"})); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "version: 1\nprofiles:\n  web:\n    skills: [ui]\n"; string(data) != want {
		t.Errorf("got:\n%s\nwant:\n%s", data, want)
	}
	if err := repo.DeleteProfile(ctx, agent.ScopeGlobal, "web"); err != nil {
		t.Errorf("delete in a scope without skilus.yaml: %v", err)
	}
}

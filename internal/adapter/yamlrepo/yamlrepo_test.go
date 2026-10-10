package yamlrepo_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colybri/skilus/internal/adapter/yamlrepo"
	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/skill"
)

func TestLockRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := yamlrepo.Repo{ProjectRoot: t.TempDir(), GlobalDir: t.TempDir()}

	empty, err := repo.Load(ctx, agent.ScopeProject)
	if err != nil || len(empty.Entries()) != 0 {
		t.Fatalf("missing lock = %v, %v", empty, err)
	}

	n, _ := skill.NewName("demo")
	h, _ := skill.NewTreeHash(strings.Repeat("ab", 32))
	id, _ := agent.NewID("claude-code")
	lf := lock.New()
	entry := lock.Entry{Skill: n, Source: "./skills", Path: "skills/demo", TreeHash: h, Executables: []string{"run.sh"},
		Targets: []agent.Target{{Agent: id, Scope: agent.ScopeGlobal, Mode: agent.ModeSymlink}}}
	if err := lf.Install(entry); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(ctx, agent.ScopeGlobal, lf); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo.GlobalDir, yamlrepo.LockFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"version: 1", "  demo:", "tree_sha256: " + h.String(), "agent: claude-code", "executables:\n      - run.sh"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("lock lacks %q:\n%s", want, data)
		}
	}

	got, err := repo.Load(ctx, agent.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := got.Entry(n)
	if !ok || e.TreeHash != h || e.Path != "skills/demo" || len(e.Targets) != 1 || e.Targets[0] != entry.Targets[0] || len(e.Executables) != 1 {
		t.Fatalf("loaded = %+v", e)
	}
}

func TestLockRejectsUnknownVersion(t *testing.T) {
	repo := yamlrepo.Repo{ProjectRoot: t.TempDir()}
	if err := os.WriteFile(filepath.Join(repo.ProjectRoot, yamlrepo.LockFile), []byte("version: 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Load(context.Background(), agent.ScopeProject); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestManifestAddSkillKeepsUserContent(t *testing.T) {
	ctx := context.Background()
	repo := yamlrepo.Repo{ProjectRoot: t.TempDir()}
	path := filepath.Join(repo.ProjectRoot, yamlrepo.ManifestFile)
	original := "# Mis skills\nversion: 1\nsources:\n  locales: ./skills # local\nskills: []\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	n, _ := skill.NewName("demo")
	e := app.ManifestEntry{Name: n, Source: "./skills", Allow: []string{"scripts"}}
	for range 2 { // adding twice keeps one entry
		if err := repo.AddSkill(ctx, agent.ScopeProject, e); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"# Mis skills", "locales: ./skills # local", "  - name: demo\n    source: ./skills\n    allow: [scripts]\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("manifest lacks %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "name: demo") != 1 {
		t.Errorf("duplicate entry:\n%s", got)
	}
}

func TestManifestCreatedWhenMissing(t *testing.T) {
	repo := yamlrepo.Repo{ProjectRoot: t.TempDir()}
	n, _ := skill.NewName("demo")
	if err := repo.AddSkill(context.Background(), agent.ScopeProject, app.ManifestEntry{Name: n, Source: "."}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo.ProjectRoot, yamlrepo.ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 1\nskills:\n  - name: demo\n    source: .\n"
	if string(data) != want {
		t.Fatalf("manifest =\n%s\nwant\n%s", data, want)
	}
}

func TestManifestRemoveSkill(t *testing.T) {
	ctx := context.Background()
	repo := yamlrepo.Repo{ProjectRoot: t.TempDir()}
	n, _ := skill.NewName("demo")
	if err := repo.RemoveSkill(ctx, agent.ScopeProject, n); err != nil {
		t.Fatalf("missing manifest: %v", err)
	}
	path := filepath.Join(repo.ProjectRoot, yamlrepo.ManifestFile)
	original := "version: 1\nskills:\n  # la que uso\n  - name: keep\n    source: ./a\n  - name: demo\n    source: ./b\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveSkill(ctx, agent.ScopeProject, n); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "version: 1\nskills:\n  # la que uso\n  - name: keep\n    source: ./a\n"; string(data) != want {
		t.Fatalf("manifest =\n%s\nwant\n%s", data, want)
	}
}

func TestTrust(t *testing.T) {
	ctx := context.Background()
	repo := yamlrepo.Repo{ProjectRoot: t.TempDir(), GlobalDir: t.TempDir()}
	if got, err := repo.Trust(ctx, agent.ScopeProject); err != nil || got != nil {
		t.Fatalf("missing file = %v, %v", got, err)
	}
	p := filepath.Join(repo.ProjectRoot, yamlrepo.ManifestFile)
	if err := os.WriteFile(p, []byte("version: 1\ntrust:\n  - github.com/anthropics\n  - github.com/obra/superpowers\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Trust(ctx, agent.ScopeProject)
	if err != nil || len(got) != 2 || got[1] != "github.com/obra/superpowers" {
		t.Fatalf("trust = %v, %v", got, err)
	}
	if err := os.WriteFile(p, []byte("version: 1\ntrust: github.com/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Trust(ctx, agent.ScopeProject); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("scalar trust err = %v, want ErrInvalid", err)
	}
}

func TestManifestProfiles(t *testing.T) {
	ctx := context.Background()
	repo := yamlrepo.Repo{ProjectRoot: t.TempDir(), GlobalDir: t.TempDir()}
	p := filepath.Join(repo.ProjectRoot, yamlrepo.ManifestFile)

	m, err := repo.Manifest(ctx, agent.ScopeProject)
	if err != nil || len(m.Skills) != 0 || len(m.Profiles) != 0 {
		t.Fatalf("missing file = %+v, %v", m, err)
	}

	data := `version: 1
skills:
  - name: a
    source: owner/repo@v1
    allow: [scripts]
  - name: b
    source: ./skills
indexes:
  - https://example.com/skills.yaml
profiles:
  web:
    skills: [b]
  backend:
    skills: [a, b]
    agents: [claude-code]
  empty:
`
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err = repo.Manifest(ctx, agent.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Skills) != 2 || m.Skills[0].Source != "owner/repo@v1" || m.Skills[0].Allow[0] != "scripts" {
		t.Fatalf("skills = %+v", m.Skills)
	}
	if len(m.Indexes) != 1 || m.Indexes[0] != "https://example.com/skills.yaml" {
		t.Fatalf("indexes = %v", m.Indexes)
	}
	var got []string
	for _, pr := range m.Profiles {
		got = append(got, pr.Name())
	}
	if strings.Join(got, ",") != "web,backend,empty" {
		t.Fatalf("profiles = %v, want file order", got)
	}
	if len(m.Profiles[1].Skills()) != 2 || m.Profiles[1].Agents()[0].String() != "claude-code" || len(m.Profiles[2].Skills()) != 0 {
		t.Fatalf("backend = %+v", m.Profiles[1])
	}

	for _, bad := range []string{
		"profiles: [web]\n",
		"profiles:\n  web:\n    skills: [Bad]\n",
		"profiles:\n  web:\n    agents: [x_y]\n",
		"profiles:\n  Web:\n    skills: []\n",
		"skills:\n  - name: a\n",
		"indexes: https://x\n",
		"indexes: [\"\"]\n",
	} {
		if err := os.WriteFile(p, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Manifest(ctx, agent.ScopeProject); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%q: err = %v, want ErrInvalid", bad, err)
		}
	}
}

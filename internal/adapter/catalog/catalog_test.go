package catalog_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/colybri/skilus/internal/adapter/catalog"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func byID(t *testing.T, agents []agent.Agent, id string) agent.Agent {
	t.Helper()
	for _, a := range agents {
		if a.ID().String() == id {
			return a
		}
	}
	t.Fatalf("agent %s not in catalog", id)
	return agent.Agent{}
}

func TestPhaseOneAgents(t *testing.T) {
	home := filepath.FromSlash("/home/ana")
	agents, err := catalog.New(home, env(nil)).Agents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		"claude-code":    {".claude/skills", "/home/ana/.claude/skills"},
		"codex":          {".agents/skills", "/home/ana/.agents/skills"},
		"cursor":         {".agents/skills", "/home/ana/.cursor/skills"},
		"github-copilot": {".agents/skills", "/home/ana/.copilot/skills"},
		"universal":      {".agents/skills", "/home/ana/.config/agents/skills"},
	}
	if len(agents) != len(want) {
		t.Fatalf("catalog has %d agents, want %d", len(agents), len(want))
	}
	for id, dirs := range want {
		a := byID(t, agents, id)
		if a.ProjectDir() != dirs[0] {
			t.Errorf("%s project dir = %q, want %q", id, a.ProjectDir(), dirs[0])
		}
		if a.GlobalDir() != filepath.FromSlash(dirs[1]) {
			t.Errorf("%s global dir = %q, want %q", id, a.GlobalDir(), filepath.FromSlash(dirs[1]))
		}
	}
	if byID(t, agents, "universal").DetectPath() != "" {
		t.Error("universal should have no detect path")
	}
}

func TestEnvironmentOverrides(t *testing.T) {
	agents, err := catalog.New("/home/ana", env(map[string]string{
		"CLAUDE_CONFIG_DIR": "/opt/claude",
		"CODEX_HOME":        "/opt/codex",
		"XDG_CONFIG_HOME":   "/xdg",
	})).Agents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	claude := byID(t, agents, "claude-code")
	if claude.GlobalDir() != filepath.FromSlash("/opt/claude/skills") || claude.DetectPath() != filepath.FromSlash("/opt/claude") {
		t.Errorf("claude-code = %q, %q", claude.GlobalDir(), claude.DetectPath())
	}
	if got := byID(t, agents, "codex").DetectPath(); got != filepath.FromSlash("/opt/codex") {
		t.Errorf("codex detect = %q", got)
	}
	if got := byID(t, agents, "universal").GlobalDir(); got != filepath.FromSlash("/xdg/agents/skills") {
		t.Errorf("universal global = %q", got)
	}
}

func TestUserFileExtendsAndOverrides(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "agents.yaml")
	c := catalog.New("/home/ana", env(nil)).WithUserFile(user)

	base, err := c.Agents(context.Background())
	if err != nil {
		t.Fatalf("missing user file: %v", err)
	}

	data := `version: 1
agents:
  - id: claude-code
    name: Claude Code (mi ruta)
    project_dir: .claude/skills
    global_dir: ~/dotfiles/claude/skills
  - id: windsurf
    name: Windsurf
    project_dir: .windsurf/skills
    global_dir: ~/.codeium/windsurf/skills
    detect: ~/.codeium/windsurf
`
	if err := os.WriteFile(user, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	agents, err := c.Agents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != len(base)+1 || agents[len(agents)-1].ID().String() != "windsurf" {
		t.Fatalf("agents = %v", agents)
	}
	if got := byID(t, agents, "claude-code").GlobalDir(); got != filepath.FromSlash("/home/ana/dotfiles/claude/skills") {
		t.Errorf("claude-code global dir = %q", got)
	}
	if agents[0].ID().String() != "claude-code" {
		t.Error("an override should keep the agent's place")
	}

	for _, bad := range []string{
		"version: 2\nagents: []\n",
		"version: 1\nagents:\n  - id: x\n    name: X\n    project_dir: ../out\n    global_dir: /x\n",
		"version: 1\nagents:\n  - id: x\n    name: X\n    project_dir: x\n    global_dir: /x\n  - id: x\n    name: X\n    project_dir: x\n    global_dir: /x\n",
	} {
		if err := os.WriteFile(user, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Agents(context.Background()); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%q: err = %v, want ErrInvalid", bad, err)
		}
	}
}

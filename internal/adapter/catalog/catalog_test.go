package catalog_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

func TestBundledAgents(t *testing.T) {
	home := filepath.FromSlash("/home/ana")
	agents, err := catalog.New(home, env(nil)).Agents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][3]string{
		"claude-code":    {".claude/skills", "/home/ana/.claude/skills", "/home/ana/.claude"},
		"codex":          {".agents/skills", "/home/ana/.agents/skills", "/home/ana/.codex"},
		"cursor":         {".agents/skills", "/home/ana/.cursor/skills", "/home/ana/.cursor"},
		"github-copilot": {".agents/skills", "/home/ana/.copilot/skills", "/home/ana/.copilot"},
		"gemini-cli":     {".agents/skills", "/home/ana/.gemini/skills", "/home/ana/.gemini"},
		"windsurf":       {".windsurf/skills", "/home/ana/.codeium/windsurf/skills", "/home/ana/.codeium/windsurf"},
		"opencode":       {".agents/skills", "/home/ana/.config/opencode/skills", "/home/ana/.config/opencode"},
	}
	for id, dirs := range want {
		a := byID(t, agents, id)
		if a.ProjectDir() != dirs[0] {
			t.Errorf("%s project dir = %q, want %q", id, a.ProjectDir(), dirs[0])
		}
		if a.GlobalDir() != filepath.FromSlash(dirs[1]) {
			t.Errorf("%s global dir = %q, want %q", id, a.GlobalDir(), filepath.FromSlash(dirs[1]))
		}
		if a.DetectPath() != filepath.FromSlash(dirs[2]) {
			t.Errorf("%s detect = %q, want %q", id, a.DetectPath(), filepath.FromSlash(dirs[2]))
		}
	}
	// The first four keep their place: the first agent of a shared
	// directory is the one the lock records.
	for i, id := range []string{"claude-code", "codex", "cursor", "github-copilot"} {
		if got := agents[i].ID().String(); got != id {
			t.Errorf("agent %d = %s, want %s", i, got, id)
		}
	}
	if len(agents) < 70 {
		t.Errorf("catalog has %d agents, want at least 70", len(agents))
	}
	for _, a := range agents {
		if a.ID().String() == "universal" {
			t.Error("universal is not an agent and should not be in the catalog")
		}
		// Every bundled agent can be detected, so none is assumed installed.
		if a.DetectPath() == "" {
			t.Errorf("%s has no detect path", a.ID())
		}
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
	if got := byID(t, agents, "devin").GlobalDir(); got != filepath.FromSlash("/xdg/devin/skills") {
		t.Errorf("devin global = %q", got)
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
  - id: acme
    name: Acme
    project_dir: .acme/skills
    global_dir: ~/.acme/skills
    detect: ~/.acme
`
	if err := os.WriteFile(user, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	agents, err := c.Agents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != len(base)+1 || agents[len(agents)-1].ID().String() != "acme" {
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

// TestDocsListEveryAgent keeps the table of the website's agents page in
// step with the catalog.
func TestDocsListEveryAgent(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "i18n", "shared.js"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)export const AGENT_ROWS = \[(.*?)\n\];`).FindSubmatch(data)
	if block == nil {
		t.Fatal("docs/i18n/shared.js has no AGENT_ROWS")
	}
	var docs []string
	for _, m := range regexp.MustCompile("\\['`([^`]+)`', '[^']*', '`([^`]+)`'").FindAllSubmatch(block[1], -1) {
		docs = append(docs, string(m[1])+" "+string(m[2]))
	}
	agents, err := catalog.New("/home/ana", env(nil)).Agents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, a := range agents {
		want = append(want, a.ID().String()+" "+a.ProjectDir())
	}
	if strings.Join(docs, "\n") != strings.Join(want, "\n") {
		t.Errorf("AGENT_ROWS (id and project dir) differ from the catalog:\n%s\nwant\n%s", strings.Join(docs, "\n"), strings.Join(want, "\n"))
	}
}

package agent_test

import (
	"errors"
	"testing"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
)

func mustID(t *testing.T, s string) agent.ID {
	t.Helper()
	id, err := agent.NewID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestNewRejectsProjectDirOutsideProject(t *testing.T) {
	id := mustID(t, "evil")
	for _, dir := range []string{"", "/etc/skills", "../skills", `..\skills`, "a/../../b"} {
		if _, err := agent.New(id, "Evil", dir, "/home/u/.evil", ""); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("New(projectDir=%q) error = %v, want ErrInvalid", dir, err)
		}
	}
}

func TestNewCleansProjectDir(t *testing.T) {
	a, err := agent.New(mustID(t, "claude-code"), "Claude Code", "./.claude/skills/", "/home/u/.claude/skills", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := a.Dir(agent.ScopeProject); got != ".claude/skills" {
		t.Fatalf("Dir(project) = %q", got)
	}
	if got := a.Dir(agent.ScopeGlobal); got != "/home/u/.claude/skills" {
		t.Fatalf("Dir(global) = %q", got)
	}
}

func TestDedupeSharedProjectDir(t *testing.T) {
	codex, _ := agent.New(mustID(t, "codex"), "Codex", ".agents/skills", "/h/.agents/skills", "")
	cursor, _ := agent.New(mustID(t, "cursor"), "Cursor", ".agents/skills", "/h/.cursor/skills", "")
	agents := map[agent.ID]agent.Agent{codex.ID(): codex, cursor.ID(): cursor}

	project := agent.Dedupe([]agent.Target{
		{Agent: codex.ID(), Scope: agent.ScopeProject, Mode: agent.ModeSymlink},
		{Agent: cursor.ID(), Scope: agent.ScopeProject, Mode: agent.ModeSymlink},
	}, agents)
	if len(project) != 1 || project[0].Agent != codex.ID() {
		t.Fatalf("project targets = %+v, want only codex", project)
	}

	global := agent.Dedupe([]agent.Target{
		{Agent: codex.ID(), Scope: agent.ScopeGlobal, Mode: agent.ModeSymlink},
		{Agent: cursor.ID(), Scope: agent.ScopeGlobal, Mode: agent.ModeSymlink},
	}, agents)
	if len(global) != 2 {
		t.Fatalf("global targets = %+v, want both", global)
	}
}

func TestParseScopeAndMode(t *testing.T) {
	if _, err := agent.ParseScope("system"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("ParseScope(system) error = %v", err)
	}
	if _, err := agent.ParseMode("hardlink"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("ParseMode(hardlink) error = %v", err)
	}
	if s, err := agent.ParseScope("global"); err != nil || s != agent.ScopeGlobal {
		t.Errorf("ParseScope(global) = %v, %v", s, err)
	}
}

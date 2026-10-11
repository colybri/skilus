package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/skill"
)

// fakeChooser answers with fixed names and target directories and records
// what it was offered.
type fakeChooser struct {
	skills  []string
	dirs    []string
	offered []skill.Package
	targets []app.PlannedTarget
	total   int
	// profile and declared record what ChooseProfileSkills offered.
	profile  string
	declared []app.ProfileSkill
}

func (f *fakeChooser) ChooseProfileSkills(_ context.Context, profile string, skills []app.ProfileSkill) ([]skill.Name, error) {
	f.profile, f.declared = profile, skills
	return f.ChooseSkills(context.Background(), "", "", nil)
}

func (f *fakeChooser) ChooseSkills(_ context.Context, _, _ string, skills []skill.Package) ([]skill.Name, error) {
	f.offered = skills
	var out []skill.Name
	for _, n := range f.skills {
		name, err := skill.NewName(n)
		if err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, nil
}

func (f *fakeChooser) ChooseTargets(_ context.Context, targets []app.PlannedTarget, total int) ([]app.PlannedTarget, error) {
	f.targets, f.total = targets, total
	var out []app.PlannedTarget
	for _, t := range targets {
		for _, d := range f.dirs {
			if t.Dir == d {
				out = append(out, t)
			}
		}
	}
	return out, nil
}

// newChooseFixture has two skills and two directories: codex and cursor
// share .agents/skills, claude-code has .claude/skills.
func newChooseFixture(t *testing.T, c *fakeChooser) *fixture {
	t.Helper()
	f := newFixture(t,
		app.FetchedSkill{Path: "a", Package: pkg(t, "alpha")},
		app.FetchedSkill{Path: "b", Package: pkg(t, "beta")},
	)
	claude, err := agent.New(mustID(t, "claude-code"), "Claude Code", ".claude/skills", "/home/u/.claude/skills", "/home/u/.claude")
	if err != nil {
		t.Fatal(err)
	}
	f.handler.Catalog = fakeCatalog{agents: []agent.Agent{newAgent(t, "codex"), newAgent(t, "cursor"), claude, newAgent(t, "zed")}}
	f.handler.Detector = fakeDetector{"codex": true, "cursor": true, "claude-code": true}
	f.handler.Chooser = c
	return f
}

func agentsDir() string { return filepath.Join("/repo", ".agents", "skills") }

func claudeDir() string { return filepath.Join("/repo", ".claude", "skills") }

func TestAddInstallsWhatTheUserChooses(t *testing.T) {
	c := &fakeChooser{skills: []string{"beta"}, dirs: []string{claudeDir()}}
	f := newChooseFixture(t, c)

	res, err := f.handler.Handle(context.Background(), app.AddSkill{Source: "./skills", Scope: agent.ScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.offered) != 2 {
		t.Fatalf("offered %d skills, want 2", len(c.offered))
	}
	// Only installed agents are offered, grouped by directory, and the
	// chooser learns how many agents skilus knows.
	if len(c.targets) != 2 || c.total != 4 {
		t.Fatalf("offered targets = %+v, total = %d", c.targets, c.total)
	}
	shared := c.targets[0]
	if shared.Dir != agentsDir() || len(shared.Agents) != 2 || shared.Agents[1].ID().String() != "cursor" || shared.Target.Agent.String() != "codex" {
		t.Fatalf("shared target = %+v", shared)
	}
	if len(res.Installed) != 1 || res.Installed[0].Skill.String() != "beta" {
		t.Fatalf("installed = %+v", res.Installed)
	}
	if _, ok := f.deployer.deployed[filepath.Join(claudeDir(), "beta")]; !ok || len(f.deployer.deployed) != 1 {
		t.Fatalf("deployed = %v", f.deployer.deployed)
	}
	// Choosing a subset does not report the source's unreadable skills.
	if res.Plan.Skipped != nil {
		t.Fatalf("skipped = %+v", res.Plan.Skipped)
	}
}

func TestAddChoosingNothingCancels(t *testing.T) {
	for name, c := range map[string]*fakeChooser{
		"no skills": {dirs: []string{agentsDir()}},
		"no agents": {skills: []string{"alpha"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newChooseFixture(t, c)
			_, err := f.handler.Handle(context.Background(), app.AddSkill{Source: "./skills", Scope: agent.ScopeProject})
			if !errors.Is(err, app.ErrCancelled) {
				t.Fatalf("err = %v, want ErrCancelled", err)
			}
			if f.prompter.asked || len(f.deployer.deployed) != 0 {
				t.Fatal("an empty choice went on")
			}
		})
	}
}

func TestAddFlagsSkipTheChooser(t *testing.T) {
	c := &fakeChooser{}
	f := newChooseFixture(t, c)

	cmd := app.AddSkill{Source: "./skills", Scope: agent.ScopeProject, Skills: []string{"alpha"}, Agents: []string{"codex", "cursor", "codex"}}
	res, err := f.handler.Handle(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if c.offered != nil || c.targets != nil {
		t.Fatal("the chooser was asked although the flags said what to install")
	}
	if got := res.Plan.Targets; len(got) != 1 || len(got[0].Agents) != 2 {
		t.Fatalf("targets = %+v, want one directory for codex and cursor", got)
	}
}

func TestAddDoesNotAskWhenThereIsNothingToChoose(t *testing.T) {
	c := &fakeChooser{}
	f := newFixture(t, app.FetchedSkill{Path: ".", Package: pkg(t, "demo")})
	f.handler.Chooser = c

	if _, err := f.handler.Handle(context.Background(), app.AddSkill{Source: "./skills", Scope: agent.ScopeProject}); err != nil {
		t.Fatal(err)
	}
	// One skill and one directory (codex and cursor share it).
	if c.offered != nil || c.targets != nil {
		t.Fatal("the chooser was asked with a single option")
	}
}

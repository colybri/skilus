package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/profile"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

type fakeManifest struct{ m app.Manifest }

func (f fakeManifest) Manifest(context.Context, agent.Scope) (app.Manifest, error) { return f.m, nil }

func agentAt(t *testing.T, id, dir string) agent.Agent {
	t.Helper()
	a, err := agent.New(mustID(t, id), id, dir, "/home/u/."+id+"/skills", "")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

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
		ids = append(ids, mustID(t, a))
	}
	p, err := profile.New(name, ns, ids)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type profileFixture struct {
	disk     *disk
	locks    *fakeLocks
	prompter *fakePrompter
	manifest *fakeManifest
	use      app.UseProfileHandler
	list     app.ListProfilesHandler
}

func newProfileFixture(t *testing.T, extra ...app.FetchedSkill) *profileFixture {
	t.Helper()
	skills := append([]app.FetchedSkill{
		{Path: "skills/a", Package: pkg(t, "a")},
		{Path: "skills/b", Package: pkg(t, "b")},
		{Path: "skills/c", Package: pkg(t, "c")},
	}, extra...)
	var declared []app.ManifestEntry
	for _, s := range skills {
		declared = append(declared, app.ManifestEntry{Name: s.Package.Name(), Source: "./skills"})
	}
	f := &profileFixture{
		disk:     newDisk(),
		locks:    &fakeLocks{},
		prompter: &fakePrompter{answer: true},
		manifest: &fakeManifest{m: app.Manifest{
			Skills: declared,
			Profiles: []profile.Profile{
				mustProfile(t, "backend", []string{"a", "b"}, "claude-code"),
				mustProfile(t, "web", []string{"b", "c"}, "codex"),
			},
		}},
	}
	catalog := fakeCatalog{agents: []agent.Agent{agentAt(t, "claude-code", ".claude/skills"), agentAt(t, "codex", ".agents/skills")}}
	fetchers := map[source.Kind]app.Fetcher{source.KindLocal: fakeFetcher{fetched: app.Fetched{Source: "./skills", Skills: skills}}}
	add := app.AddSkillHandler{
		Catalog:      catalog,
		Detector:     fakeDetector{"codex": true},
		Fetchers:     fetchers,
		Store:        f.disk,
		Deployer:     f.disk,
		Locks:        f.locks,
		Manifests:    &fakeManifests{},
		ProjectRoot:  "/repo",
		DefaultModes: map[agent.Scope]agent.Mode{agent.ScopeProject: agent.ModeCopy},
		Limits:       policy.DefaultLimits,
	}
	sync := app.SyncHandler{Catalog: catalog, Fetchers: fetchers, Store: f.disk, Deployer: f.disk, Trees: f.disk, Locks: f.locks, ProjectRoot: "/repo"}
	f.use = app.UseProfileHandler{Add: add, Sync: sync, Manifest: f.manifest, Prompter: f.prompter}
	f.list = app.ListProfilesHandler{Manifest: f.manifest, Locks: f.locks}
	return f
}

func (f *profileFixture) has(dir string) bool {
	_, ok := f.disk.dirs[filepath.Join("/repo", filepath.FromSlash(dir))]
	return ok
}

func TestUseProfileSwitchesSkillsAndAgents(t *testing.T) {
	f := newProfileFixture(t)
	ctx := context.Background()

	if _, err := f.use.Handle(ctx, app.UseProfile{Name: "backend", Scope: agent.ScopeProject}); err != nil {
		t.Fatal(err)
	}
	if !f.prompter.asked || !f.has(".claude/skills/a") || !f.has(".claude/skills/b") || f.has(".agents/skills/b") {
		t.Fatalf("after backend: asked=%v dirs=%v", f.prompter.asked, f.disk.dirs)
	}

	res, err := f.use.Handle(ctx, app.UseProfile{Name: "web", Scope: agent.ScopeProject, Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Installed) != 1 || len(res.Retargeted) != 1 || len(res.Removed) != 1 {
		t.Fatalf("result = %+v", res)
	}
	for dir, want := range map[string]bool{
		".claude/skills/a": false, ".claude/skills/b": false,
		".agents/skills/b": true, ".agents/skills/c": true,
	} {
		if f.has(dir) != want {
			t.Errorf("%s present = %v, want %v", dir, !want, want)
		}
	}
	b, ok := f.locks.lf.Entry(pkg(t, "b").Name())
	if !ok || len(b.Targets) != 1 || b.Targets[0].Agent.String() != "codex" {
		t.Fatalf("b = %+v", b)
	}
	if _, ok := f.locks.lf.Entry(pkg(t, "a").Name()); ok {
		t.Fatal("a still in the lock")
	}

	statuses, err := f.list.Handle(ctx, app.ListProfiles{Scope: agent.ScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if statuses[0].Active || !statuses[1].Active {
		t.Fatalf("statuses = %+v", statuses)
	}

	// Using it again changes nothing and asks nothing.
	f.prompter.asked = false
	res, err = f.use.Handle(ctx, app.UseProfile{Name: "web", Scope: agent.ScopeProject})
	if err != nil || !res.Plan.Empty() || f.prompter.asked {
		t.Fatalf("second use: err=%v plan=%+v asked=%v", err, res.Plan, f.prompter.asked)
	}
}

func TestUseProfileDryRunAndCancel(t *testing.T) {
	f := newProfileFixture(t)
	ctx := context.Background()
	res, err := f.use.Handle(ctx, app.UseProfile{Name: "web", Scope: agent.ScopeProject, DryRun: true})
	if err != nil || len(res.Plan.Install) != 1 || len(f.disk.dirs) != 0 || f.prompter.asked {
		t.Fatalf("dry run: err=%v plan=%+v dirs=%v", err, res.Plan, f.disk.dirs)
	}
	f.prompter.answer = false
	if _, err := f.use.Handle(ctx, app.UseProfile{Name: "web", Scope: agent.ScopeProject}); !errors.Is(err, app.ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
	if len(f.disk.dirs) != 0 {
		t.Fatalf("cancelled but wrote %v", f.disk.dirs)
	}
}

func TestUseProfileErrors(t *testing.T) {
	ctx := context.Background()
	f := newProfileFixture(t)
	if _, err := f.use.Handle(ctx, app.UseProfile{Name: "data", Scope: agent.ScopeProject}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown profile: %v", err)
	}

	f.manifest.m.Profiles = append(f.manifest.m.Profiles, mustProfile(t, "broken", []string{"a", "zzz"}))
	if _, err := f.use.Handle(ctx, app.UseProfile{Name: "broken", Scope: agent.ScopeProject}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("undeclared skill: %v", err)
	}
	statuses, err := f.list.Handle(ctx, app.ListProfiles{Scope: agent.ScopeProject})
	if err != nil || len(statuses[2].Undeclared) != 1 {
		t.Errorf("list: err=%v statuses=%+v", err, statuses)
	}
}

func TestUseProfileScriptsAllowedByManifest(t *testing.T) {
	script := skill.File{Path: "run.sh", Kind: skill.KindExecutable, Data: []byte("echo hi\n")}
	f := newProfileFixture(t, app.FetchedSkill{Path: "skills/s", Package: pkg(t, "s", script)})
	f.manifest.m.Profiles = append(f.manifest.m.Profiles, mustProfile(t, "ops", []string{"s"}))
	ctx := context.Background()

	if _, err := f.use.Handle(ctx, app.UseProfile{Name: "ops", Scope: agent.ScopeProject, Yes: true}); !errors.Is(err, app.ErrRejected) {
		t.Fatalf("err = %v, want ErrRejected", err)
	}
	for i, e := range f.manifest.m.Skills {
		if e.Name.String() == "s" {
			f.manifest.m.Skills[i].Allow = []string{app.AllowScripts}
		}
	}
	if _, err := f.use.Handle(ctx, app.UseProfile{Name: "ops", Scope: agent.ScopeProject, Yes: true}); err != nil {
		t.Fatal(err)
	}
	if !f.has(".agents/skills/s") {
		t.Fatalf("dirs = %v", f.disk.dirs)
	}
}

package app_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/profile"
	"github.com/colybri/skilus/internal/domain/skill"
)

// newReconcileFixture serves demo unchanged and extra next to it, so update
// has nothing to move and only skills: decides what changes.
func newReconcileFixture(t *testing.T, declared ...string) *updateFixture {
	t.Helper()
	f := newUpdateFixture(t)
	f.next, f.commit = f.pkg, f.entry.Commit
	f.extra = []app.FetchedSkill{{Path: "skills/extra", Package: pkg(t, "extra")}}
	f.handler.Detector = fakeDetector{"codex": true}
	f.handler.DefaultModes = map[agent.Scope]agent.Mode{agent.ScopeProject: agent.ModeCopy}
	m := app.Manifest{}
	for _, n := range declared {
		m.Skills = append(m.Skills, app.ManifestEntry{Name: name(t, n), Source: "github.com/o/r@v1"})
	}
	f.handler.Manifest = &fakeManifest{m}
	return f
}

func name(t *testing.T, raw string) skill.Name {
	t.Helper()
	n, err := skill.NewName(raw)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUpdateInstallsSkillsAddedToSkilusYaml(t *testing.T) {
	f := newReconcileFixture(t, "demo", "extra")
	ctx := context.Background()

	res, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if !f.prompter.asked || len(res.Plan.Install) != 1 || len(res.Plan.Skills) != 0 {
		t.Fatalf("plan = %+v, asked = %v", res.Plan, f.prompter.asked)
	}
	if len(res.Added) != 1 || res.Added[0].Skill.String() != "extra" || res.Added[0].Dependency {
		t.Fatalf("added = %+v", res.Added)
	}
	e, ok := f.locks.lf.Entry(name(t, "extra"))
	if !ok || e.Requested != "v1" || len(e.Targets) != 1 || e.Targets[0].Agent.String() != "codex" {
		t.Fatalf("lock entry = %+v, %v", e, ok)
	}
	if _, ok := f.disk.dirs[filepath.Join("/repo", ".agents", "skills", "extra")]; !ok {
		t.Fatal("extra is not deployed in the detected agent")
	}

	again, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject})
	if err != nil || !again.Plan.Empty() {
		t.Fatalf("second update = %+v, %v; want nothing to do", again.Plan, err)
	}
}

func TestUpdateNeedsADetectedAgentForAddedSkills(t *testing.T) {
	f := newReconcileFixture(t, "demo", "extra")
	f.handler.Detector = fakeDetector{}
	_, err := f.handler.Handle(context.Background(), app.Update{Scope: agent.ScopeProject})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if _, ok := f.locks.lf.Entry(name(t, "extra")); ok {
		t.Fatal("extra installed without agents")
	}
}

func TestUpdateRemovesSkillsGoneFromSkilusYaml(t *testing.T) {
	f := newReconcileFixture(t)
	ctx := context.Background()
	helper := f.entry
	helper.Skill, helper.Path, helper.Dependency = name(t, "helper"), "skills/helper", true
	demo := f.entry
	demo.Requires = []skill.Name{helper.Skill}
	lf, err := lock.Restore([]lock.Entry{demo, helper})
	if err != nil {
		t.Fatal(err)
	}
	f.locks.lf = lf

	res, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Plan.Remove) != 2 || len(res.Removed) != 2 || len(res.Plan.Skills) != 0 {
		t.Fatalf("plan = %+v, removed = %+v", res.Plan, res.Removed)
	}
	if len(f.locks.lf.Entries()) != 0 {
		t.Fatalf("lock = %+v, want empty", f.locks.lf.Entries())
	}
	if _, ok := f.disk.dirs[f.dest]; ok {
		t.Fatal("demo is still deployed")
	}
}

func TestUpdateWithoutSkilusYamlRemovesNothing(t *testing.T) {
	f := newReconcileFixture(t)
	f.handler.Manifest = &fakeManifest{app.Manifest{Missing: true}}
	res, err := f.handler.Handle(context.Background(), app.Update{Scope: agent.ScopeProject})
	if err != nil || !res.Plan.Empty() {
		t.Fatalf("plan = %+v, %v; a missing skilus.yaml declares nothing to apply", res.Plan, err)
	}
	if _, ok := f.locks.lf.Entry(f.entry.Skill); !ok {
		t.Fatal("demo removed")
	}
}

func TestUpdateKeepsUndeclaredSkillsOthersRequire(t *testing.T) {
	f := newReconcileFixture(t, "demo")
	lib := f.entry
	lib.Skill, lib.Path = name(t, "lib"), "skills/lib"
	demo := f.entry
	demo.Requires = []skill.Name{lib.Skill}
	lf, err := lock.Restore([]lock.Entry{demo, lib})
	if err != nil {
		t.Fatal(err)
	}
	f.locks.lf = lf

	_, err = f.handler.Handle(context.Background(), app.Update{Scope: agent.ScopeProject})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if f.prompter.asked || len(f.locks.lf.Entries()) != 2 {
		t.Fatalf("asked = %v, lock = %+v; nothing should change", f.prompter.asked, f.locks.lf.Entries())
	}
}

func TestUpdateLeavesOutSkillsTheActiveProfileLeavesOut(t *testing.T) {
	f := newReconcileFixture(t, "demo", "extra")
	p, err := profile.New("min", []skill.Name{f.entry.Skill}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := f.handler.Manifest.(*fakeManifest)
	m.m.Profiles = []profile.Profile{p}
	ctx := context.Background()

	res, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject})
	if err != nil || !res.Plan.Empty() {
		t.Fatalf("plan = %+v, %v; the profile leaves extra out", res.Plan, err)
	}

	// Naming it installs it anyway.
	res, err = f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject, Names: []string{"extra"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != 1 || len(res.Plan.Current) != 0 {
		t.Fatalf("added = %+v, current = %+v; only extra was named", res.Added, res.Plan.Current)
	}
}

func TestUpdateWithNamesTouchesOnlyThose(t *testing.T) {
	f := newReconcileFixture(t, "extra")
	ctx := context.Background()

	if _, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject, Names: []string{"nope"}}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	res, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject, Names: []string{"extra"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != 1 || len(res.Removed) != 0 {
		t.Fatalf("added = %+v, removed = %+v; demo was not named", res.Added, res.Removed)
	}
	if _, ok := f.locks.lf.Entry(f.entry.Skill); !ok {
		t.Fatal("demo removed without being named")
	}
}

func TestOutdatedReportsSkillsAddedAndRemovedInSkilusYaml(t *testing.T) {
	f := newReconcileFixture(t, "extra")
	q := app.OutdatedHandler{Resolver: &fakeResolver{}, Locks: f.locks, Manifest: f.handler.Manifest}

	got, err := q.Handle(context.Background(), app.Outdated{Scopes: project})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Entry.Skill != f.entry.Skill || got[0].State != app.FreshRemoved {
		t.Fatalf("demo = %+v, want removed", got[0])
	}
	if got[1].Entry.Skill.String() != "extra" || got[1].State != app.FreshAdded || got[1].Declared != "github.com/o/r@v1" {
		t.Fatalf("extra = %+v, want added", got[1])
	}

	got, err = q.Handle(context.Background(), app.Outdated{Scopes: project, Names: []string{"extra"}})
	if err != nil || len(got) != 1 || got[0].State != app.FreshAdded {
		t.Fatalf("named check = %+v, %v", got, err)
	}
}

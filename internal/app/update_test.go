package app_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

const newCommit = "fedcba9876543210fedcba9876543210fedcba98"

type fakeResolver struct {
	commits map[string]string // source string -> commit
	calls   int
}

func (f *fakeResolver) Resolve(_ context.Context, src source.Source) (string, error) {
	f.calls++
	c, ok := f.commits[src.String()]
	if !ok {
		return "", fmt.Errorf("%s: %w", src, domain.ErrNotFound)
	}
	return c, nil
}

func TestOutdatedComparesRequestedRefWithLock(t *testing.T) {
	f := newSyncFixture(t)
	other := f.entry
	other.Skill, _ = skill.NewName("other")
	pinned := f.entry
	pinned.Skill, _ = skill.NewName("pinned")
	pinned.Requested = pinned.Commit
	local := f.entry
	local.Skill, _ = skill.NewName("local")
	local.Source, local.Requested, local.Commit = "./skills", "", ""
	lf, err := lock.Restore([]lock.Entry{f.entry, other, pinned, local})
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeResolver{commits: map[string]string{"github.com/o/r@v1": newCommit}}
	q := app.OutdatedHandler{Resolver: r, Locks: &fakeLocks{lf: lf}}

	got, err := q.Handle(context.Background(), app.Outdated{Scopes: project})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]app.Freshness{}
	for _, s := range got {
		states[s.Entry.Skill.String()] = s.State
	}
	want := map[string]app.Freshness{"demo": app.FreshOutdated, "other": app.FreshOutdated, "pinned": app.FreshPinned, "local": app.FreshLocal}
	if fmt.Sprint(states) != fmt.Sprint(want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	if r.calls != 1 {
		t.Fatalf("resolver calls = %d; one repository and ref is one lookup", r.calls)
	}
}

type updateFixture struct {
	*syncFixture
	handler  app.UpdateHandler
	prompter *fakePrompter
	next     skill.Package
	extra    []app.FetchedSkill // served next to demo
}

// newUpdateFixture installs demo with sync and serves a v2 in which run.sh
// changed and a new script appeared.
func newUpdateFixture(t *testing.T) *updateFixture {
	t.Helper()
	f := &updateFixture{syncFixture: newSyncFixture(t), prompter: &fakePrompter{answer: true}}
	if _, err := f.sync.Handle(context.Background(), app.Sync{Scopes: project}); err != nil {
		t.Fatal(err)
	}
	f.next = pkg(t, "demo",
		skill.File{Path: "run.sh", Kind: skill.KindExecutable, Data: []byte("echo v2\n")},
		skill.File{Path: "new.sh", Kind: skill.KindExecutable, Data: []byte("echo new\n")})
	f.handler = app.UpdateHandler{
		Catalog:     fakeCatalog{agents: []agent.Agent{newAgent(t, "codex")}},
		Fetchers:    map[source.Kind]app.Fetcher{source.KindGit: f},
		Store:       f.disk,
		Deployer:    f.disk,
		Trees:       f.disk,
		Locks:       f.locks,
		Prompter:    f.prompter,
		ProjectRoot: "/repo",
		Limits:      policy.DefaultLimits,
	}
	return f
}

// Fetch serves the next version at the requested ref.
func (f *updateFixture) Fetch(_ context.Context, src source.Source) (app.Fetched, error) {
	if src.Ref != "v1" {
		return app.Fetched{}, fmt.Errorf("fetched ref %q, want the requested one", src.Ref)
	}
	return app.Fetched{Source: src.ID, Requested: src.Ref, Commit: newCommit, Skills: append([]app.FetchedSkill{{Path: "skills/demo", Package: f.next}}, f.extra...)}, nil
}

func TestUpdateInstallsNewRequirements(t *testing.T) {
	f := newUpdateFixture(t)
	ctx := context.Background()
	helper := pkg(t, "helper")
	f.extra = []app.FetchedSkill{{Path: "skills/helper", Package: helper}}
	reqs, err := skill.ParseRequires("helper")
	if err != nil {
		t.Fatal(err)
	}
	if f.next, err = f.next.WithRequires(reqs); err != nil {
		t.Fatal(err)
	}

	res, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject, AllowScripts: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Installed) != 1 || res.Installed[0].Skill != helper.Name() || !res.Installed[0].Dependency {
		t.Fatalf("installed = %+v", res.Installed)
	}
	e, _ := f.locks.lf.Entry(f.entry.Skill)
	if len(e.Requires) != 1 || e.Requires[0] != helper.Name() {
		t.Fatalf("demo requires = %v", e.Requires)
	}
	if len(res.Installed[0].Targets) != len(e.Targets) {
		t.Fatalf("helper targets = %+v, want those of demo %+v", res.Installed[0].Targets, e.Targets)
	}
	if _, err := f.verify.Handle(ctx, app.Verify{Scopes: project}); err != nil {
		t.Fatalf("verify after update: %v", err)
	}
}

func TestUpdateShowsChangesAndReplacesInstalledCopy(t *testing.T) {
	f := newUpdateFixture(t)
	ctx := context.Background()

	res, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if !f.prompter.asked || len(res.Updated) != 1 {
		t.Fatalf("asked=%v result=%+v", f.prompter.asked, res)
	}
	u := res.Plan.Skills[0]
	want := []skill.Change{{Path: "new.sh", Kind: skill.Added}, {Path: "run.sh", Kind: skill.Modified}}
	if fmt.Sprint(u.Changes) != fmt.Sprint(want) {
		t.Fatalf("changes = %v, want %v", u.Changes, want)
	}
	// run.sh was accepted at install time; only new.sh is reported.
	if len(u.Report.Findings) != 1 || u.Report.Findings[0].Path != "new.sh" {
		t.Fatalf("findings = %+v", u.Report.Findings)
	}
	e, _ := f.locks.lf.Entry(f.entry.Skill)
	if e.Commit != newCommit || e.TreeHash != f.next.TreeHash() || e.Requested != "v1" || !f.locks.saved {
		t.Fatalf("lock entry = %+v", e)
	}
	if _, err := f.verify.Handle(ctx, app.Verify{Scopes: project}); err != nil {
		t.Fatalf("verify after update: %v", err)
	}

	// Nothing moved since: nothing to do.
	f.prompter.asked = false
	res, err = f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject})
	if err != nil || len(res.Plan.Skills) != 0 || len(res.Plan.Current) != 1 || f.prompter.asked {
		t.Fatalf("second update = %+v, %v", res, err)
	}
}

func TestUpdateGuards(t *testing.T) {
	ctx := context.Background()

	f := newUpdateFixture(t)
	_, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject, Yes: true})
	if !errors.Is(err, app.ErrRejected) {
		t.Fatalf("--yes with a new script: err = %v, want ErrRejected", err)
	}
	if _, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject, Yes: true, AllowScripts: true}); err != nil {
		t.Fatal(err)
	}

	f = newUpdateFixture(t)
	f.prompter.answer = false
	if _, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject}); !errors.Is(err, app.ErrCancelled) {
		t.Fatalf("answered no: err = %v", err)
	}
	if e, _ := f.locks.lf.Entry(f.entry.Skill); e.Commit != f.entry.Commit {
		t.Fatal("a cancelled update changed the lock")
	}

	f = newUpdateFixture(t)
	f.disk.dirs[f.dest] = []skill.File{{Path: skill.ManifestFile, Kind: skill.KindRegular, Data: []byte("mine")}}
	if _, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject}); !errors.Is(err, domain.ErrConflict) || f.prompter.asked {
		t.Fatalf("hand-modified copy: err = %v, asked = %v", err, f.prompter.asked)
	}
	if _, err := f.handler.Handle(ctx, app.Update{Scope: agent.ScopeProject, Force: true}); err != nil {
		t.Fatal(err)
	}

	if _, err := f.handler.Handle(ctx, app.Update{Names: []string{"nope"}, Scope: agent.ScopeProject}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown skill: err = %v", err)
	}
}

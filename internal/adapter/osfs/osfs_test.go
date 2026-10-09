package osfs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/colybri/skilus/internal/adapter/osfs"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/skill"
)

func write(t *testing.T, path, data string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), perm); err != nil {
		t.Fatal(err)
	}
}

func manifest(name string) string {
	return "---\nname: " + name + "\ndescription: \"Skill " + name + "\"\n---\n# " + name + "\n"
}

func TestLocalFetcherDiscovery(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()

	// A single skill at the root.
	write(t, filepath.Join(work, "single", "SKILL.md"), manifest("single"), 0o644)
	got, err := osfs.LocalFetcher{Dir: work}.Fetch(ctx, "single")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != filepath.Join(work, "single") || len(got.Skills) != 1 || got.Skills[0].Path != "." {
		t.Fatalf("single = %+v", got)
	}

	// A collection under skills/, with .git ignored.
	write(t, filepath.Join(work, "repo", "skills", "beta", "SKILL.md"), manifest("beta"), 0o644)
	write(t, filepath.Join(work, "repo", "skills", "alpha", "SKILL.md"), manifest("alpha"), 0o644)
	write(t, filepath.Join(work, "repo", "skills", "alpha", "scripts", "run.sh"), "#!/bin/sh\n", 0o755)
	write(t, filepath.Join(work, "repo", "skills", "alpha", ".git", "HEAD"), "x", 0o644)
	got, err = osfs.LocalFetcher{Dir: work}.Fetch(ctx, filepath.Join(work, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 2 || got.Skills[0].Path != "skills/alpha" || got.Skills[1].Package.Name().String() != "beta" {
		t.Fatalf("collection = %+v", got.Skills)
	}
	files := got.Skills[0].Package.Files()
	if len(files) != 2 || files[1].Path != "scripts/run.sh" {
		t.Fatalf("alpha files = %+v", files)
	}
	wantKind := skill.KindExecutable
	if runtime.GOOS == "windows" {
		wantKind = skill.KindRegular
	}
	if files[1].Kind != wantKind {
		t.Fatalf("run.sh kind = %s, want %s", files[1].Kind, wantKind)
	}
}

func TestLocalFetcherErrors(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	write(t, filepath.Join(work, "nofm", "SKILL.md"), "# no frontmatter\n", 0o644)
	write(t, filepath.Join(work, "badname", "SKILL.md"), manifest("Bad_Name"), 0o644)
	write(t, filepath.Join(work, "file"), "x", 0o644)
	write(t, filepath.Join(work, "empty", "README.md"), "x", 0o644)

	tests := map[string]error{
		"missing": domain.ErrNotFound,
		"file":    domain.ErrInvalid,
		"nofm":    domain.ErrInvalid,
		"badname": domain.ErrInvalid,
	}
	for src, want := range tests {
		if _, err := (osfs.LocalFetcher{Dir: work}).Fetch(ctx, src); !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", src, err, want)
		}
	}
	got, err := osfs.LocalFetcher{Dir: work}.Fetch(ctx, "empty")
	if err != nil || len(got.Skills) != 0 {
		t.Errorf("empty: %+v, %v", got, err)
	}
}

func TestStoreAndDeploy(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	n, _ := skill.NewName("demo")
	p, err := skill.NewPackage(n, "Demo", []skill.File{
		{Path: "SKILL.md", Kind: skill.KindRegular, Data: []byte(manifest("demo"))},
		{Path: "scripts/run.sh", Kind: skill.KindExecutable, Data: []byte("#!/bin/sh\n")},
	})
	if err != nil {
		t.Fatal(err)
	}

	store := osfs.Store{Root: filepath.Join(base, "store")}
	dir, err := store.Put(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := store.Put(ctx, p); err != nil || again != dir {
		t.Fatalf("second Put = %s, %v", again, err)
	}
	if filepath.Base(dir) != p.TreeHash().String() {
		t.Fatalf("store dir = %s", dir)
	}

	d := osfs.Deployer{}
	dest := filepath.Join(base, "agent", "skills", "demo")
	if err := d.Deploy(ctx, dir, dest, agent.ModeCopy); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "SKILL.md"))
	if err != nil || string(data) != manifest("demo") {
		t.Fatalf("copied SKILL.md = %q, %v", data, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dest, "scripts", "run.sh"))
		if err != nil || info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("run.sh lost its exec bit: %v", err)
		}
	}
	if err := d.Deploy(ctx, dir, dest, agent.ModeCopy); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second Deploy err = %v, want ErrConflict", err)
	}
	if err := d.Remove(ctx, dest); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS == "windows" {
		return // symlinks need extra privileges on Windows
	}
	if err := d.Deploy(ctx, dir, dest, agent.ModeSymlink); err != nil {
		t.Fatal(err)
	}
	if target, err := os.Readlink(dest); err != nil || target != dir {
		t.Fatalf("symlink -> %s, %v", target, err)
	}
	if err := d.Remove(ctx, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatalf("removing the symlink touched the store: %v", err)
	}
}

package gitsrc_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colybri/skilus/internal/adapter/gitsrc"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

// repo is a throwaway Git repository built with the git binary.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "--quiet", "--initial-branch=main")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(path, data string) {
	r.t.Helper()
	full := filepath.Join(r.dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
		r.t.Fatal(err)
	}
	r.git("add", "--", path)
}

// special adds an index entry with an explicit mode (symlink, submodule),
// which works the same on every OS.
func (r *repo) special(mode, path, content string) {
	r.t.Helper()
	obj := "1111111111111111111111111111111111111111"
	if mode != "160000" {
		cmd := exec.Command("git", "hash-object", "-w", "--stdin")
		cmd.Dir, cmd.Stdin = r.dir, strings.NewReader(content)
		out, err := cmd.Output()
		if err != nil {
			r.t.Fatal(err)
		}
		obj = strings.TrimSpace(string(out))
	}
	r.git("update-index", "--add", "--cacheinfo", mode+","+obj+","+path)
}

func (r *repo) commit(msg string) string {
	r.git("commit", "--quiet", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

func (r *repo) source(t *testing.T, ref string) source.Source {
	t.Helper()
	p := filepath.ToSlash(r.dir)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // file:///C:/...
	}
	raw := "file://" + p
	if ref != "" {
		raw += "@" + ref
	}
	s, err := source.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func manifest(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\n"
}

func TestFetchPinsCommitAndReadsModes(t *testing.T) {
	r := newRepo(t)
	r.write("README.md", "not a skill")
	r.write("skills/review/SKILL.md", manifest("review", "v1"))
	v1 := r.commit("v1")
	r.git("tag", "v1.0.0")

	r.write("skills/review/SKILL.md", manifest("review", "v2"))
	r.write("skills/review/scripts/lint.sh", "#!/bin/sh\n")
	r.git("add", "--chmod=+x", "--", "skills/review/scripts/lint.sh")
	r.special("120000", "skills/review/guide.md", "../../README.md")
	r.write("skills/other/SKILL.md", manifest("other", "Other"))
	head := r.commit("v2")

	ctx := context.Background()
	got, err := gitsrc.Fetcher{}.Fetch(ctx, r.source(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got.Commit != head || got.Requested != "" || !strings.HasPrefix(got.Source, "file://") {
		t.Fatalf("fetched = %+v", got)
	}
	if len(got.Skills) != 2 || got.Skills[0].Path != "skills/other" || got.Skills[1].Path != "skills/review" {
		t.Fatalf("skills = %+v", got.Skills)
	}
	files := map[string]skill.File{}
	for _, f := range got.Skills[1].Package.Files() {
		files[f.Path] = f
	}
	if files["scripts/lint.sh"].Kind != skill.KindExecutable || string(files["scripts/lint.sh"].Data) != "#!/bin/sh\n" {
		t.Errorf("lint.sh = %+v", files["scripts/lint.sh"])
	}
	if files["guide.md"].Kind != skill.KindSymlink || files["guide.md"].LinkTarget != "../../README.md" {
		t.Errorf("guide.md = %+v", files["guide.md"])
	}

	old, err := gitsrc.Fetcher{}.Fetch(ctx, r.source(t, "v1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	if old.Commit != v1 || old.Requested != "v1.0.0" || old.Skills[0].Package.Description() != "v1" {
		t.Fatalf("v1.0.0 = %+v", old)
	}
	if old.Skills[0].Package.TreeHash() == got.Skills[1].Package.TreeHash() {
		t.Fatal("different content, same hash")
	}
}

func TestFetchErrors(t *testing.T) {
	r := newRepo(t)
	r.write("SKILL.md", manifest("demo", "Demo"))
	r.write("big.txt", strings.Repeat("x", 100))
	r.commit("init")
	ctx := context.Background()

	if _, err := (gitsrc.Fetcher{}).Fetch(ctx, r.source(t, "nope")); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing ref err = %v, want ErrNotFound", err)
	}
	invalid := func(f gitsrc.Fetcher, label string) {
		t.Helper()
		got, err := f.Fetch(ctx, r.source(t, ""))
		if err != nil || len(got.Skills) != 0 || len(got.Invalid) != 1 || !errors.Is(got.Invalid[0].Err, domain.ErrInvalid) {
			t.Errorf("%s: %+v, %v", label, got, err)
		}
	}
	invalid(gitsrc.Fetcher{MaxSkillBytes: 50}, "too large")

	r.special("160000", "vendor", "")
	r.commit("submodule")
	invalid(gitsrc.Fetcher{}, "submodule")

	if _, err := (gitsrc.Fetcher{Git: filepath.Join(t.TempDir(), "no-git")}).Fetch(ctx, r.source(t, "")); err == nil || !strings.Contains(err.Error(), "git") {
		t.Errorf("missing binary err = %v", err)
	}
}

func TestResolveFollowsRefsLikeFetch(t *testing.T) {
	r := newRepo(t)
	r.write("SKILL.md", manifest("demo", "v1"))
	v1 := r.commit("v1")
	r.git("tag", "-a", "-m", "annotated", "v1.0.0")
	r.git("branch", "stable")
	r.write("SKILL.md", manifest("demo", "v2"))
	head := r.commit("v2")

	ctx := context.Background()
	f := gitsrc.Fetcher{}
	for ref, want := range map[string]string{"": head, "main": head, "v1.0.0": v1, "stable": v1, v1: v1} {
		got, err := f.Resolve(ctx, r.source(t, ref))
		if err != nil || got != want {
			t.Errorf("Resolve(%q) = %s, %v; want %s", ref, got, err, want)
		}
	}
	if _, err := f.Resolve(ctx, r.source(t, "nope")); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing ref err = %v, want ErrNotFound", err)
	}
}

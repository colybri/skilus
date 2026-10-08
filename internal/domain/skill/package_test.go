package skill_test

import (
	"errors"
	"testing"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
)

func name(t *testing.T, s string) skill.Name {
	t.Helper()
	n, err := skill.NewName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func manifest() skill.File {
	return skill.File{Path: "SKILL.md", Kind: skill.KindRegular, Data: []byte("---\nname: demo\n---\n")}
}

func TestNewPackageSortsAndHashesDeterministically(t *testing.T) {
	a := []skill.File{{Path: "scripts/run.sh", Kind: skill.KindExecutable, Data: []byte("echo")}, manifest()}
	b := []skill.File{manifest(), {Path: "scripts/run.sh", Kind: skill.KindExecutable, Data: []byte("echo")}}

	pa, err := skill.NewPackage(name(t, "demo"), "A demo skill", a)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := skill.NewPackage(name(t, "demo"), "A demo skill", b)
	if err != nil {
		t.Fatal(err)
	}
	if pa.TreeHash() != pb.TreeHash() {
		t.Fatal("file order changed the hash")
	}
	if pa.Files()[0].Path != "SKILL.md" {
		t.Fatalf("files not sorted: %v", pa.Files()[0].Path)
	}
	if pa.Size() != int64(len(manifest().Data)+4) {
		t.Fatalf("Size() = %d", pa.Size())
	}
}

func TestTreeHashSeesEveryField(t *testing.T) {
	base := []skill.File{manifest(), {Path: "a", Kind: skill.KindRegular, Data: []byte("bc")}}
	variants := map[string][]skill.File{
		"content":  {manifest(), {Path: "a", Kind: skill.KindRegular, Data: []byte("bd")}},
		"kind":     {manifest(), {Path: "a", Kind: skill.KindExecutable, Data: []byte("bc")}},
		"rename":   {manifest(), {Path: "b", Kind: skill.KindRegular, Data: []byte("bc")}},
		"boundary": {manifest(), {Path: "ab", Kind: skill.KindRegular, Data: []byte("c")}},
	}
	p0, err := skill.NewPackage(name(t, "demo"), "d", base)
	if err != nil {
		t.Fatal(err)
	}
	for label, files := range variants {
		p, err := skill.NewPackage(name(t, "demo"), "d", files)
		if err != nil {
			t.Fatal(err)
		}
		if p.TreeHash() == p0.TreeHash() {
			t.Errorf("%s change kept the same hash", label)
		}
	}
}

func TestNewPackageRejects(t *testing.T) {
	tests := map[string][]skill.File{
		"no manifest":      {{Path: "README.md", Kind: skill.KindRegular}},
		"manifest symlink": {{Path: "SKILL.md", Kind: skill.KindSymlink, LinkTarget: "x"}},
		"parent path":      {manifest(), {Path: "../evil", Kind: skill.KindRegular}},
		"absolute path":    {manifest(), {Path: "/etc/passwd", Kind: skill.KindRegular}},
		"unclean path":     {manifest(), {Path: "a/./b", Kind: skill.KindRegular}},
		"backslash":        {manifest(), {Path: `a\b`, Kind: skill.KindRegular}},
		"duplicate":        {manifest(), manifest()},
		"unknown kind":     {manifest(), {Path: "dev", Kind: "device"}},
	}
	for label, files := range tests {
		t.Run(label, func(t *testing.T) {
			if _, err := skill.NewPackage(name(t, "demo"), "d", files); !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
	if _, err := skill.NewPackage(name(t, "demo"), "  ", []skill.File{manifest()}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty description error = %v", err)
	}
}

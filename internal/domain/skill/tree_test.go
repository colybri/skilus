package skill_test

import (
	"reflect"
	"testing"

	"github.com/colybri/skilus/internal/domain/skill"
)

func TestHashFilesMatchesPackage(t *testing.T) {
	files := []skill.File{{Path: "run.sh", Kind: skill.KindExecutable, Data: []byte("x")}, manifest()}
	p, err := skill.NewPackage(name(t, "demo"), "d", files)
	if err != nil {
		t.Fatal(err)
	}
	h, err := skill.HashFiles(files)
	if err != nil || h != p.TreeHash() {
		t.Fatalf("HashFiles = %v, %v; package hash %v", h, err, p.TreeHash())
	}
	// No SKILL.md is fine: a damaged install must still hash.
	if _, err := skill.HashFiles([]skill.File{{Path: "a", Kind: skill.KindRegular}}); err != nil {
		t.Fatal(err)
	}
	if _, err := skill.HashFiles([]skill.File{{Path: "../a", Kind: skill.KindRegular}}); err == nil {
		t.Fatal("unclean path accepted")
	}
}

func TestExecutablesAndMark(t *testing.T) {
	files := []skill.File{manifest(), {Path: "b.sh", Kind: skill.KindRegular}, {Path: "a.sh", Kind: skill.KindExecutable}}
	marked := skill.MarkExecutable(files, []string{"b.sh", "SKILL.md-missing"})
	if got := skill.Executables(marked); !reflect.DeepEqual(got, []string{"a.sh", "b.sh"}) {
		t.Fatalf("Executables = %v", got)
	}
	if files[1].Kind != skill.KindRegular {
		t.Fatal("MarkExecutable changed its input")
	}
}

func TestDiff(t *testing.T) {
	want := []skill.File{manifest(), {Path: "a", Kind: skill.KindRegular, Data: []byte("1")}, {Path: "gone", Kind: skill.KindRegular}}
	got := []skill.File{manifest(), {Path: "a", Kind: skill.KindRegular, Data: []byte("2")}, {Path: "new", Kind: skill.KindRegular}}
	changes := skill.Diff(want, got)
	expect := []skill.Change{{Path: "a", Kind: skill.Modified}, {Path: "gone", Kind: skill.Removed}, {Path: "new", Kind: skill.Added}}
	if !reflect.DeepEqual(changes, expect) {
		t.Fatalf("Diff = %+v", changes)
	}
	if len(skill.Diff(want, want)) != 0 {
		t.Fatal("identical trees differ")
	}
}

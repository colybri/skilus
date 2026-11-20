package osfs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
)

// TestWriteFileNeverFollowsSymlinks writes, in package order, a symlink to
// a directory outside the root and then entries below it, which
// skill.NewPackage refuses but Store.Put must not trust blindly.
func TestWriteFileNeverFollowsSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs privileges on Windows")
	}
	root, outside := t.TempDir(), t.TempDir()
	if err := writeFile(root, skill.File{Path: "x", Kind: skill.KindSymlink, LinkTarget: outside}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []skill.File{
		{Path: "x/marker", Kind: skill.KindRegular, Data: []byte("pwned")},
		{Path: "x/sub/marker", Kind: skill.KindRegular, Data: []byte("pwned")},
	} {
		if err := writeFile(root, f); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: error = %v, want ErrInvalid", f.Path, err)
		}
	}

	// A file whose own name is a symlink is not written through it.
	target := filepath.Join(outside, "target")
	if err := os.Symlink(target, filepath.Join(root, "y")); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(root, skill.File{Path: "y", Kind: skill.KindRegular, Data: []byte("pwned")}); err == nil {
		t.Error("wrote through the symlink y")
	}

	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("wrote %d entries outside the root", len(entries))
	}
}

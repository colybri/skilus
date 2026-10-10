package skill

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/colybri/skilus/internal/domain"
)

// sortedFiles validates a file list and returns a sorted copy.
func sortedFiles(files []File) ([]File, error) {
	sorted := make([]File, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	for i, f := range sorted {
		if err := validPath(f.Path); err != nil {
			return nil, err
		}
		if i > 0 && sorted[i-1].Path == f.Path {
			return nil, fmt.Errorf("%s is listed twice: %w", f.Path, domain.ErrInvalid)
		}
		switch f.Kind {
		case KindRegular, KindExecutable, KindSymlink:
		default:
			return nil, fmt.Errorf("file %s has unknown kind %q: %w", f.Path, f.Kind, domain.ErrInvalid)
		}
	}
	return sorted, nil
}

// HashFiles computes the tree hash of any file list, such as a skill read
// back from an agent's directory. Unlike NewPackage it does not require a
// valid SKILL.md, so a damaged installation can still be hashed.
func HashFiles(files []File) (TreeHash, error) {
	sorted, err := sortedFiles(files)
	if err != nil {
		return TreeHash{}, err
	}
	return hashTree(sorted), nil
}

// Executables returns the paths of the executable files.
func Executables(files []File) []string {
	var out []string
	for _, f := range files {
		if f.Kind == KindExecutable {
			out = append(out, f.Path)
		}
	}
	sort.Strings(out)
	return out
}

// MarkExecutable returns files with the regular files at the given paths
// turned executable. It restores what a file system without an executable
// bit (Windows) cannot keep.
func MarkExecutable(files []File, paths []string) []File {
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[p] = true
	}
	out := make([]File, len(files))
	for i, f := range files {
		if f.Kind == KindRegular && set[f.Path] {
			f.Kind = KindExecutable
		}
		out[i] = f
	}
	return out
}

// ChangeKind says how a file differs from the expected tree.
type ChangeKind string

// Change kinds.
const (
	Added    ChangeKind = "added"
	Removed  ChangeKind = "removed"
	Modified ChangeKind = "modified"
)

// Change is one difference between two trees.
type Change struct {
	Path string
	Kind ChangeKind
}

// Diff lists how actual differs from expected, sorted by path. A change of
// kind (regular, executable, symlink) counts as a modification.
func Diff(expected, actual []File) []Change {
	want := make(map[string]File, len(expected))
	for _, f := range expected {
		want[f.Path] = f
	}
	var out []Change
	seen := make(map[string]bool, len(actual))
	for _, f := range actual {
		seen[f.Path] = true
		w, ok := want[f.Path]
		switch {
		case !ok:
			out = append(out, Change{Path: f.Path, Kind: Added})
		case w.Kind != f.Kind || w.LinkTarget != f.LinkTarget || !bytes.Equal(w.Data, f.Data):
			out = append(out, Change{Path: f.Path, Kind: Modified})
		}
	}
	for _, f := range expected {
		if !seen[f.Path] {
			out = append(out, Change{Path: f.Path, Kind: Removed})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

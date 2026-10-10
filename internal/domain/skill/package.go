package skill

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/colybri/skilus/internal/domain"
)

// ManifestFile is the file every skill package must contain at its root.
const ManifestFile = "SKILL.md"

// maxDescriptionLen bounds what skilus shows and stores. The Agent Skills
// spec asks for 1024 characters, but published skills exceed it and agents
// load them anyway, so only absurd lengths are rejected.
const maxDescriptionLen = 4096

// Kind classifies a file for hashing and inspection. Only what is portable
// across operating systems is kept: the executable bit and symlinks.
type Kind string

// File kinds.
const (
	KindRegular    Kind = "file"
	KindExecutable Kind = "exec"
	KindSymlink    Kind = "symlink"
)

// File is one entry of a skill package. Path uses forward slashes and is
// relative to the skill root. Data holds the content of regular and
// executable files; LinkTarget holds the target of a symlink as written.
type File struct {
	Path       string
	Kind       Kind
	Data       []byte
	LinkTarget string
}

// Size returns the number of content bytes.
func (f File) Size() int64 { return int64(len(f.Data)) }

// Package is a skill read from a source: its metadata, files and the hash
// that identifies that exact content.
type Package struct {
	name        Name
	description string
	files       []File
	hash        TreeHash
}

// NewPackage validates the metadata and file list and computes the tree
// hash. Files are sorted by path.
func NewPackage(name Name, description string, files []File) (Package, error) {
	if name.IsZero() {
		return Package{}, fmt.Errorf("skill package has no name: %w", domain.ErrInvalid)
	}
	description = strings.TrimSpace(description)
	switch {
	case description == "":
		return Package{}, fmt.Errorf("skill %s has no description: %w", name, domain.ErrInvalid)
	case utf8.RuneCountInString(description) > maxDescriptionLen:
		return Package{}, fmt.Errorf("skill %s description is longer than %d characters: %w", name, maxDescriptionLen, domain.ErrInvalid)
	}

	sorted, err := sortedFiles(files)
	if err != nil {
		return Package{}, fmt.Errorf("skill %s: %w", name, err)
	}
	hasManifest := false
	for _, f := range sorted {
		if f.Path == ManifestFile && f.Kind == KindRegular {
			hasManifest = true
		}
	}
	if !hasManifest {
		return Package{}, fmt.Errorf("skill %s has no regular %s at its root: %w", name, ManifestFile, domain.ErrInvalid)
	}

	return Package{name: name, description: description, files: sorted, hash: hashTree(sorted)}, nil
}

// Name returns the skill name.
func (p Package) Name() Name { return p.name }

// Description returns the skill description.
func (p Package) Description() string { return p.description }

// Files returns the files sorted by path. The slice must not be modified.
func (p Package) Files() []File { return p.files }

// TreeHash returns the hash of the package content.
func (p Package) TreeHash() TreeHash { return p.hash }

// Size returns the total content size in bytes.
func (p Package) Size() int64 {
	var n int64
	for _, f := range p.files {
		n += f.Size()
	}
	return n
}

func validPath(p string) error {
	clean := path.Clean(p)
	switch {
	case p == "" || p == ".":
		return fmt.Errorf("empty file path: %w", domain.ErrInvalid)
	case strings.Contains(p, `\`):
		return fmt.Errorf("file path %q uses backslashes: %w", p, domain.ErrInvalid)
	case clean != p || path.IsAbs(p) || clean == ".." || strings.HasPrefix(clean, "../"):
		return fmt.Errorf("file path %q is not a clean relative path inside the skill: %w", p, domain.ErrInvalid)
	}
	return nil
}

// hashTree hashes the sorted files unambiguously: every variable-length
// field is prefixed with its length, so two different trees never produce
// the same byte stream.
func hashTree(files []File) TreeHash {
	h := sha256.New()
	field := func(b []byte) {
		var n [binary.MaxVarintLen64]byte
		h.Write(n[:binary.PutUvarint(n[:], uint64(len(b)))])
		h.Write(b)
	}
	field([]byte("skilus-tree-v1"))
	for _, f := range files {
		field([]byte(f.Path))
		field([]byte(f.Kind))
		if f.Kind == KindSymlink {
			field([]byte(f.LinkTarget))
		} else {
			field(f.Data)
		}
	}
	return TreeHash{value: hex.EncodeToString(h.Sum(nil))}
}

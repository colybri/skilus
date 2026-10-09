package osfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
)

var _ app.Fetcher = LocalFetcher{}

// LocalFetcher reads skills from a directory on disk. A source is either a
// skill (it has SKILL.md at its root) or a collection holding skills in
// skills/<name>/ or <name>/.
type LocalFetcher struct {
	// Dir resolves relative sources, usually the working directory.
	Dir string
}

// Fetch implements app.Fetcher. The returned Source is the absolute path.
func (f LocalFetcher) Fetch(_ context.Context, source string) (app.Fetched, error) {
	root := source
	if !filepath.IsAbs(root) {
		root = filepath.Join(f.Dir, root)
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return app.Fetched{}, fmt.Errorf("directory %s does not exist: %w", source, domain.ErrNotFound)
	case err != nil:
		return app.Fetched{}, err
	case !info.IsDir():
		return app.Fetched{}, fmt.Errorf("%s is not a directory: %w", source, domain.ErrInvalid)
	}

	dirs, err := discover(root)
	if err != nil {
		return app.Fetched{}, err
	}
	out := app.Fetched{Source: root}
	for _, rel := range dirs {
		p, err := readPackage(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return app.Fetched{}, fmt.Errorf("skill in %s: %w", rel, err)
		}
		out.Skills = append(out.Skills, app.FetchedSkill{Path: rel, Package: p})
	}
	return out, nil
}

// discover returns the skill directories of a source, relative to it and
// with forward slashes.
func discover(root string) ([]string, error) {
	if isFile(filepath.Join(root, skill.ManifestFile)) {
		return []string{"."}, nil
	}
	for _, base := range []string{"skills", "."} {
		entries, err := os.ReadDir(filepath.Join(root, base))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var found []string
		for _, e := range entries {
			if !e.IsDir() || e.Name()[0] == '.' {
				continue
			}
			if isFile(filepath.Join(root, base, e.Name(), skill.ManifestFile)) {
				found = append(found, filepath.ToSlash(filepath.Join(base, e.Name())))
			}
		}
		if len(found) > 0 {
			sort.Strings(found)
			return found, nil
		}
	}
	return nil, nil
}

func isFile(p string) bool {
	info, err := os.Lstat(p)
	return err == nil && info.Mode().IsRegular()
}

// readPackage reads a skill directory without following symlinks.
func readPackage(dir string) (skill.Package, error) {
	var files []skill.File
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			return nil
		case mode&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			files = append(files, skill.File{Path: rel, Kind: skill.KindSymlink, LinkTarget: filepath.ToSlash(target)})
		case mode.IsRegular():
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			kind := skill.KindRegular
			if mode.Perm()&0o111 != 0 {
				kind = skill.KindExecutable
			}
			files = append(files, skill.File{Path: rel, Kind: kind, Data: data})
		default:
			return fmt.Errorf("%s is not a regular file, directory or symlink: %w", rel, domain.ErrInvalid)
		}
		return nil
	})
	if err != nil {
		return skill.Package{}, err
	}

	data, err := os.ReadFile(filepath.Join(dir, skill.ManifestFile))
	if err != nil {
		return skill.Package{}, err
	}
	meta, err := parseFrontmatter(data)
	if err != nil {
		return skill.Package{}, err
	}
	name, err := skill.NewName(meta.Name)
	if err != nil {
		return skill.Package{}, err
	}
	return skill.NewPackage(name, meta.Description, files)
}

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// parseFrontmatter reads the YAML block between the leading "---" lines.
func parseFrontmatter(data []byte) (frontmatter, error) {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(data, []byte("---\n"))
	if !ok {
		return frontmatter{}, fmt.Errorf("%s does not start with YAML frontmatter (---): %w", skill.ManifestFile, domain.ErrInvalid)
	}
	block, _, ok := bytes.Cut(rest, []byte("\n---"))
	if !ok {
		return frontmatter{}, fmt.Errorf("%s frontmatter is not closed with ---: %w", skill.ManifestFile, domain.ErrInvalid)
	}
	var fm frontmatter
	if err := yaml.Unmarshal(block, &fm); err != nil {
		return frontmatter{}, fmt.Errorf("%s frontmatter: %w: %w", skill.ManifestFile, err, domain.ErrInvalid)
	}
	return fm, nil
}

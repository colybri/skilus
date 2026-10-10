package osfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/colybri/skilus/internal/adapter/skillsrc"
	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
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
func (f LocalFetcher) Fetch(_ context.Context, src source.Source) (app.Fetched, error) {
	root := src.Raw
	if !filepath.IsAbs(root) {
		root = filepath.Join(f.Dir, root)
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return app.Fetched{}, fmt.Errorf("directory %s does not exist: %w", src.Raw, domain.ErrNotFound)
	case err != nil:
		return app.Fetched{}, err
	case !info.IsDir():
		return app.Fetched{}, fmt.Errorf("%s is not a directory: %w", src.Raw, domain.ErrInvalid)
	}

	candidates, err := manifests(root)
	if err != nil {
		return app.Fetched{}, err
	}
	out := app.Fetched{Source: root}
	for _, rel := range skillsrc.Discover(candidates) {
		files, err := readTree(filepath.Join(root, filepath.FromSlash(rel)), true)
		if err != nil {
			return app.Fetched{}, fmt.Errorf("skill in %s: %w", rel, err)
		}
		p, err := skillsrc.Build(files)
		if err != nil {
			out.Invalid = append(out.Invalid, app.InvalidSkill{Path: rel, Err: err})
			continue
		}
		out.Skills = append(out.Skills, app.FetchedSkill{Path: rel, Package: p})
	}
	return out, nil
}

// manifests lists the regular SKILL.md files where skillsrc.Discover looks:
// the root, skills/*/ and */.
func manifests(root string) ([]string, error) {
	var out []string
	if isFile(filepath.Join(root, skill.ManifestFile)) {
		out = append(out, skill.ManifestFile)
	}
	for _, base := range []string{"skills", "."} {
		entries, err := os.ReadDir(filepath.Join(root, base))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() && isFile(filepath.Join(root, base, e.Name(), skill.ManifestFile)) {
				out = append(out, filepath.ToSlash(filepath.Join(base, e.Name(), skill.ManifestFile)))
			}
		}
	}
	return out, nil
}

func isFile(p string) bool {
	info, err := os.Lstat(p)
	return err == nil && info.Mode().IsRegular()
}

// readTree reads a skill directory without following symlinks. skipGit
// leaves out .git directories, which a local source may have.
func readTree(dir string, skipGit bool) ([]skill.File, error) {
	var files []skill.File
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		if skipGit && d.IsDir() && d.Name() == ".git" {
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
	return files, err
}

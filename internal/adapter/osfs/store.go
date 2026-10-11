package osfs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/skill"
)

var (
	_ app.Store    = Store{}
	_ app.Deployer = Deployer{}
)

// Store keeps every package under Root/<tree hash>. A directory there is
// only ever created complete: it is written to a temporary directory and
// renamed into place.
type Store struct {
	Root string
}

// Put implements app.Store.
func (s Store) Put(_ context.Context, p skill.Package) (string, error) {
	dir := filepath.Join(s.Root, p.TreeHash().String())
	if _, err := os.Lstat(dir); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(s.Root, ".tmp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // best effort; gone after a successful rename

	for _, f := range p.Files() {
		if err := writeFile(filepath.Join(tmp, filepath.FromSlash(f.Path)), f); err != nil {
			return "", err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		if _, statErr := os.Lstat(dir); statErr == nil {
			return dir, nil // another process stored the same content first
		}
		return "", err
	}
	return dir, nil
}

func writeFile(dest string, f skill.File) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	switch f.Kind {
	case skill.KindSymlink:
		return os.Symlink(filepath.FromSlash(f.LinkTarget), dest)
	case skill.KindExecutable:
		return os.WriteFile(dest, f.Data, 0o755)
	default:
		return os.WriteFile(dest, f.Data, 0o644)
	}
}

// Deployer places stored skills into agent directories, as a symlink to
// the store or as a copy.
type Deployer struct{}

// Deploy implements app.Deployer.
func (Deployer) Deploy(_ context.Context, storeDir, dest string, mode agent.Mode) error {
	if _, err := os.Lstat(dest); err == nil {
		return domain.Errorf("%s ya existe: %w", dest, domain.ErrConflict)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	if mode == agent.ModeSymlink {
		return os.Symlink(storeDir, dest)
	}

	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(dest)+".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // best effort; gone after a successful rename
	if err := copyTree(storeDir, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// Remove implements app.Deployer. A symlink is removed, not its target.
func (Deployer) Remove(_ context.Context, dest string) error {
	return os.RemoveAll(dest)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case info.IsDir():
			return os.MkdirAll(target, 0o755)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			return copyFile(p, target, info.Mode().Perm())
		}
	})
}

func copyFile(src, dst string, perm fs.FileMode) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck // read-only
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}

// Lookup implements app.Store.
func (s Store) Lookup(_ context.Context, h skill.TreeHash) (string, bool, error) {
	dir := filepath.Join(s.Root, h.String())
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", false, nil
	case err != nil:
		return "", false, err
	case !info.IsDir():
		return "", false, domain.Errorf("%s no es un directorio en el almacén: %w", h.Short(), domain.ErrInvalid)
	}
	return dir, true, nil
}

// Discard implements app.Store.
func (s Store) Discard(_ context.Context, h skill.TreeHash) error {
	return os.RemoveAll(filepath.Join(s.Root, h.String()))
}

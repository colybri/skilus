package osfs

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
)

var _ app.TreeReader = TreeReader{}

// TreeReader reads installed skills back from disk.
type TreeReader struct{}

// ReadTree implements app.TreeReader. Unlike a local source, nothing is
// skipped: a .git directory inside an installed skill is a change too.
func (TreeReader) ReadTree(_ context.Context, dir string) ([]skill.File, error) {
	info, err := os.Stat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, domain.Errorf("%s no existe: %w", dir, domain.ErrNotFound)
	case err != nil:
		return nil, err
	case !info.IsDir():
		return nil, domain.Errorf("%s no es un directorio: %w", dir, domain.ErrInvalid)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	return readTree(resolved, false)
}

// ExecutableBits implements app.TreeReader.
func (TreeReader) ExecutableBits() bool {
	return runtime.GOOS != "windows"
}

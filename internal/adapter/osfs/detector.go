// Package osfs holds adapters that read the real file system.
package osfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain/agent"
)

var _ app.AgentDetector = Detector{}

// Detector reports an agent as installed when its detect path exists.
type Detector struct{}

// Installed implements app.AgentDetector. An agent without a detect path is
// always available.
func (Detector) Installed(_ context.Context, a agent.Agent) (bool, error) {
	if a.DetectPath() == "" {
		return true, nil
	}
	_, err := os.Stat(a.DetectPath())
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("stat %s: %w", a.DetectPath(), err)
	}
}

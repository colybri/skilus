// Package catalog implements app.AgentCatalog from a YAML file embedded in
// the binary.
package catalog

import (
	"context"
	_ "embed"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain/agent"
)

//go:embed agents.yaml
var embedded []byte

var _ app.AgentCatalog = (*Catalog)(nil)

// Catalog resolves the embedded agent list against the current environment.
type Catalog struct {
	home   string
	getenv func(string) string
	data   []byte
}

// New returns a catalog for the given home directory. getenv is usually
// os.Getenv; tests pass their own.
func New(home string, getenv func(string) string) *Catalog {
	return &Catalog{home: home, getenv: getenv, data: embedded}
}

type file struct {
	Version int `yaml:"version"`
	Agents  []struct {
		ID         string `yaml:"id"`
		Name       string `yaml:"name"`
		ProjectDir string `yaml:"project_dir"`
		GlobalDir  string `yaml:"global_dir"`
		Detect     string `yaml:"detect"`
	} `yaml:"agents"`
}

// Agents parses the catalog and resolves its paths.
func (c *Catalog) Agents(_ context.Context) ([]agent.Agent, error) {
	var f file
	if err := yaml.Unmarshal(c.data, &f); err != nil {
		return nil, fmt.Errorf("parse agent catalog: %w", err)
	}
	if f.Version != 1 {
		return nil, fmt.Errorf("agent catalog version %d is not supported", f.Version)
	}
	out := make([]agent.Agent, 0, len(f.Agents))
	for _, raw := range f.Agents {
		id, err := agent.NewID(raw.ID)
		if err != nil {
			return nil, err
		}
		detect := ""
		if raw.Detect != "" {
			detect = c.expand(raw.Detect)
		}
		a, err := agent.New(id, raw.Name, raw.ProjectDir, c.expand(raw.GlobalDir), detect)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

var varRe = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*):-([^}]*)\}`)

// expand replaces ${VAR:-default} (default used when VAR is empty) and a
// leading ~, then converts the result to the platform's separators.
func (c *Catalog) expand(s string) string {
	s = varRe.ReplaceAllStringFunc(s, func(m string) string {
		parts := varRe.FindStringSubmatch(m)
		if v := strings.TrimSpace(c.getenv(parts[1])); v != "" {
			return filepath.ToSlash(v)
		}
		return parts[2]
	})
	if s == "~" || strings.HasPrefix(s, "~/") {
		s = filepath.ToSlash(c.home) + s[1:]
	}
	return filepath.FromSlash(s)
}

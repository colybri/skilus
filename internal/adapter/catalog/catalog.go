// Package catalog implements app.AgentCatalog from a YAML file embedded in
// the binary, extended by the user's own file (usually ~/.skilus/agents.yaml).
package catalog

import (
	"context"
	_ "embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
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
	user   string
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

// WithUserFile makes the catalog read path too, in the same format: an
// agent with an id already in the catalog replaces it, a new id is added
// at the end. A missing file is ignored.
func (c *Catalog) WithUserFile(path string) *Catalog {
	out := *c
	out.user = path
	return &out
}

// Agents parses the catalog and resolves its paths.
func (c *Catalog) Agents(_ context.Context) ([]agent.Agent, error) {
	out, err := c.parse(c.data, "agent catalog")
	if err != nil {
		return nil, err
	}
	if c.user == "" {
		return out, nil
	}
	data, err := os.ReadFile(c.user)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	extra, err := c.parse(data, c.user)
	if err != nil {
		return nil, err
	}
	index := make(map[agent.ID]int, len(out))
	for i, a := range out {
		index[a.ID()] = i
	}
	for _, a := range extra {
		if i, ok := index[a.ID()]; ok {
			out[i] = a
			continue
		}
		index[a.ID()] = len(out)
		out = append(out, a)
	}
	return out, nil
}

func (c *Catalog) parse(data []byte, name string) ([]agent.Agent, error) {
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, domain.Errorf("analizar %s: %w: %w", name, err, domain.ErrInvalid)
	}
	if f.Version != 1 {
		return nil, domain.Errorf("%s: la versión %d no está soportada: %w", name, f.Version, domain.ErrInvalid)
	}
	out := make([]agent.Agent, 0, len(f.Agents))
	seen := map[string]bool{}
	for _, raw := range f.Agents {
		id, err := agent.NewID(raw.ID)
		if err != nil {
			return nil, domain.Errorf("%s: %w", name, err)
		}
		if seen[raw.ID] {
			return nil, domain.Errorf("%s: el agente %s aparece dos veces: %w", name, id, domain.ErrInvalid)
		}
		seen[raw.ID] = true
		detect := ""
		if raw.Detect != "" {
			detect = c.expand(raw.Detect)
		}
		a, err := agent.New(id, raw.Name, raw.ProjectDir, c.expand(raw.GlobalDir), detect)
		if err != nil {
			return nil, domain.Errorf("%s: %w", name, err)
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

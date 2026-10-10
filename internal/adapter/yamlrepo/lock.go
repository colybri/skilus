// Package yamlrepo persists skilus.lock and skilus.yaml as YAML files: in
// the project root for the project scope and in the skilus home directory
// for the global scope.
package yamlrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/skill"
)

// File names.
const (
	LockFile     = "skilus.lock"
	ManifestFile = "skilus.yaml"
)

var (
	_ app.LockRepository     = Repo{}
	_ app.ManifestRepository = Repo{}
	_ app.TrustList          = Repo{}
	_ app.ManifestReader     = Repo{}
)

// Repo stores the files of both scopes.
type Repo struct {
	ProjectRoot string
	GlobalDir   string // usually ~/.skilus
}

func (r Repo) path(scope agent.Scope, name string) string {
	if scope == agent.ScopeGlobal {
		return filepath.Join(r.GlobalDir, name)
	}
	return filepath.Join(r.ProjectRoot, name)
}

type lockDoc struct {
	Version int                  `yaml:"version"`
	Skills  map[string]lockEntry `yaml:"skills"`
}

type lockEntry struct {
	Source      string       `yaml:"source"`
	Requested   string       `yaml:"requested,omitempty"`
	Commit      string       `yaml:"commit,omitempty"`
	Path        string       `yaml:"path"`
	TreeSHA256  string       `yaml:"tree_sha256"`
	Executables []string     `yaml:"executables,omitempty"`
	Targets     []lockTarget `yaml:"targets"`
}

type lockTarget struct {
	Agent string `yaml:"agent"`
	Scope string `yaml:"scope"`
	Mode  string `yaml:"mode"`
}

// Load implements app.LockRepository. A missing file is an empty lock.
func (r Repo) Load(_ context.Context, scope agent.Scope) (*lock.Lockfile, error) {
	p := r.path(scope, LockFile)
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return lock.New(), nil
	}
	if err != nil {
		return nil, err
	}
	var doc lockDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w: %w", p, err, domain.ErrInvalid)
	}
	if doc.Version != 1 {
		return nil, fmt.Errorf("%s: version %d is not supported: %w", p, doc.Version, domain.ErrInvalid)
	}
	entries := make([]lock.Entry, 0, len(doc.Skills))
	for name, raw := range doc.Skills {
		e, err := raw.toDomain(name)
		if err != nil {
			return nil, fmt.Errorf("%s: skill %s: %w", p, name, err)
		}
		entries = append(entries, e)
	}
	return lock.Restore(entries)
}

func (raw lockEntry) toDomain(name string) (lock.Entry, error) {
	n, err := skill.NewName(name)
	if err != nil {
		return lock.Entry{}, err
	}
	h, err := skill.NewTreeHash(raw.TreeSHA256)
	if err != nil {
		return lock.Entry{}, err
	}
	e := lock.Entry{Skill: n, Source: raw.Source, Requested: raw.Requested, Commit: raw.Commit, Path: raw.Path, TreeHash: h, Executables: raw.Executables}
	for _, t := range raw.Targets {
		id, err := agent.NewID(t.Agent)
		if err != nil {
			return lock.Entry{}, err
		}
		scope, err := agent.ParseScope(t.Scope)
		if err != nil {
			return lock.Entry{}, err
		}
		mode, err := agent.ParseMode(t.Mode)
		if err != nil {
			return lock.Entry{}, err
		}
		e.Targets = append(e.Targets, agent.Target{Agent: id, Scope: scope, Mode: mode})
	}
	return e, nil
}

// Save implements app.LockRepository.
func (r Repo) Save(_ context.Context, scope agent.Scope, l *lock.Lockfile) error {
	doc := lockDoc{Version: 1, Skills: map[string]lockEntry{}}
	for _, e := range l.Entries() {
		raw := lockEntry{Source: e.Source, Requested: e.Requested, Commit: e.Commit, Path: e.Path, TreeSHA256: e.TreeHash.String(), Executables: e.Executables}
		for _, t := range e.Targets {
			raw.Targets = append(raw.Targets, lockTarget{Agent: t.Agent.String(), Scope: string(t.Scope), Mode: string(t.Mode)})
		}
		doc.Skills[e.Skill.String()] = raw
	}
	data, err := marshal(&doc)
	if err != nil {
		return err
	}
	header := []byte("# Generado por skilus. No lo edites a mano.\n")
	return writeAtomic(r.path(scope, LockFile), append(header, data...))
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeAtomic replaces path with data so readers never see a partial file.
func writeAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(tmp.Name()) //nolint:errcheck // best effort cleanup
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck // the write error wins
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

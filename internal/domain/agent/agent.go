// Package agent models the AI agents skilus installs skills into and the
// targets (agent, scope, mode) of an installation.
package agent

import (
	"path"
	"regexp"
	"strings"

	"github.com/colybri/skilus/internal/domain"
)

var idRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ID identifies an agent in the catalog, e.g. "claude-code".
type ID struct {
	value string
}

// NewID validates s as an agent identifier.
func NewID(s string) (ID, error) {
	if !idRe.MatchString(s) {
		return ID{}, domain.Errorf("el id de agente %q solo puede usar minúsculas, dígitos y guiones: %w", s, domain.ErrInvalid)
	}
	return ID{value: s}, nil
}

func (id ID) String() string { return id.value }

// Agent describes where an agent reads skills from. Paths are already
// resolved for the current machine by the catalog adapter.
type Agent struct {
	id         ID
	name       string
	projectDir string
	globalDir  string
	detectPath string
}

// New validates and builds an Agent. projectDir must be relative and stay
// inside the project; globalDir must be absolute. detectPath may be empty,
// meaning the agent is always considered present.
func New(id ID, name, projectDir, globalDir, detectPath string) (Agent, error) {
	if id.value == "" {
		return Agent{}, domain.Errorf("el id de agente está vacío: %w", domain.ErrInvalid)
	}
	if strings.TrimSpace(name) == "" {
		return Agent{}, domain.Errorf("el agente %s no tiene nombre visible: %w", id, domain.ErrInvalid)
	}
	clean := path.Clean(strings.ReplaceAll(projectDir, `\`, "/"))
	if projectDir == "" || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return Agent{}, domain.Errorf("el directorio de proyecto del agente %s, %q, debe ser una ruta relativa dentro del proyecto: %w", id, projectDir, domain.ErrInvalid)
	}
	if globalDir == "" {
		return Agent{}, domain.Errorf("el agente %s no tiene directorio global: %w", id, domain.ErrInvalid)
	}
	return Agent{id: id, name: name, projectDir: clean, globalDir: globalDir, detectPath: detectPath}, nil
}

// ID returns the agent identifier.
func (a Agent) ID() ID { return a.id }

// Name returns the display name.
func (a Agent) Name() string { return a.name }

// ProjectDir returns the skills directory relative to a project root.
func (a Agent) ProjectDir() string { return a.projectDir }

// GlobalDir returns the absolute skills directory for the user.
func (a Agent) GlobalDir() string { return a.globalDir }

// DetectPath returns the path whose existence means the agent is installed,
// or "" when the agent is always available.
func (a Agent) DetectPath() string { return a.detectPath }

// Dir returns the skills directory for the given scope. For ScopeProject it
// is relative to the project root.
func (a Agent) Dir(s Scope) string {
	if s == ScopeGlobal {
		return a.globalDir
	}
	return a.projectDir
}

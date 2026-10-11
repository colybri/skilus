package agent

import (
	"github.com/colybri/skilus/internal/domain"
)

// Scope says whether a skill is installed for one project or for the user.
type Scope string

// Scopes.
const (
	ScopeProject Scope = "project"
	ScopeGlobal  Scope = "global"
)

// ParseScope validates s as a Scope.
func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case ScopeProject, ScopeGlobal:
		return Scope(s), nil
	}
	return "", domain.Errorf("el ámbito %q debe ser project o global: %w", s, domain.ErrInvalid)
}

// Mode says how files reach the agent's directory.
type Mode string

// Modes.
const (
	ModeSymlink Mode = "symlink"
	ModeCopy    Mode = "copy"
)

// ParseMode validates s as a Mode.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeSymlink, ModeCopy:
		return Mode(s), nil
	}
	return "", domain.Errorf("el modo %q debe ser symlink o copy: %w", s, domain.ErrInvalid)
}

// Target is one place a skill is deployed to.
type Target struct {
	Agent ID
	Scope Scope
	Mode  Mode
}

// Dedupe removes targets that resolve to the same directory and scope as an
// earlier one (several agents share .agents/skills), keeping the first.
func Dedupe(targets []Target, agents map[ID]Agent) []Target {
	seen := make(map[string]bool, len(targets))
	out := make([]Target, 0, len(targets))
	for _, t := range targets {
		key := string(t.Scope) + "|" + agents[t.Agent].Dir(t.Scope)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	return out
}

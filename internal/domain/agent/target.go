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

// Group is the agents that read one skills directory.
type Group struct {
	Dir    string
	Agents []Agent
}

// GroupByDir groups the agents that read the same skills directory in
// scope (several share .agents/skills in a project), keeping their order.
func GroupByDir(agents []Agent, scope Scope) []Group {
	var out []Group
	index := make(map[string]int, len(agents))
	for _, a := range agents {
		dir := a.Dir(scope)
		if i, ok := index[dir]; ok {
			out[i].Agents = append(out[i].Agents, a)
			continue
		}
		index[dir] = len(out)
		out = append(out, Group{Dir: dir, Agents: []Agent{a}})
	}
	return out
}

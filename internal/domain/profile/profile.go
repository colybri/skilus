// Package profile models the named sets of skills a team switches between,
// declared under profiles: in skilus.yaml.
package profile

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/skill"
)

var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9_-]*[a-z0-9])?$`)

// Profile is a set of skills and, optionally, the agents they go to.
type Profile struct {
	name   string
	skills []skill.Name
	agents []agent.ID
}

// New validates a profile. skills may be empty: such a profile removes
// every skill. agents empty means the detected agents.
func New(name string, skills []skill.Name, agents []agent.ID) (Profile, error) {
	if !nameRe.MatchString(name) {
		return Profile{}, fmt.Errorf("profile name %q must use lowercase letters, digits, hyphens and underscores: %w", name, domain.ErrInvalid)
	}
	seen := map[string]bool{}
	for _, s := range skills {
		if seen[s.String()] {
			return Profile{}, fmt.Errorf("profile %s lists skill %s twice: %w", name, s, domain.ErrInvalid)
		}
		seen[s.String()] = true
	}
	seenAgents := map[string]bool{}
	for _, a := range agents {
		if seenAgents[a.String()] {
			return Profile{}, fmt.Errorf("profile %s lists agent %s twice: %w", name, a, domain.ErrInvalid)
		}
		seenAgents[a.String()] = true
	}
	return Profile{name: name, skills: slices.Clone(skills), agents: slices.Clone(agents)}, nil
}

// Name returns the profile name.
func (p Profile) Name() string { return p.name }

// Skills returns the skills in declaration order.
func (p Profile) Skills() []skill.Name { return slices.Clone(p.skills) }

// Agents returns the agents the profile deploys to; nil means the detected
// ones.
func (p Profile) Agents() []agent.ID { return slices.Clone(p.agents) }

// Has reports whether the profile includes the skill.
func (p Profile) Has(n skill.Name) bool { return slices.Contains(p.skills, n) }

// Matches reports whether installed is exactly the profile's set of skills.
func (p Profile) Matches(installed []skill.Name) bool {
	if len(installed) != len(p.skills) {
		return false
	}
	for _, n := range installed {
		if !p.Has(n) {
			return false
		}
	}
	return true
}

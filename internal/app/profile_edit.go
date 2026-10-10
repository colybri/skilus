package app

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/profile"
	"github.com/colybri/skilus/internal/domain/skill"
)

// CreateProfile is the command behind `skilus profile create`.
type CreateProfile struct {
	Name   string
	Scope  agent.Scope
	Skills []string
	Agents []string // empty means the detected agents
}

// ChangeProfile is the command behind `skilus profile add` and
// `skilus profile remove`.
type ChangeProfile struct {
	Name         string
	Scope        agent.Scope
	AddSkills    []string
	RemoveSkills []string
	AddAgents    []string
	RemoveAgents []string
}

// DeleteProfile is the command behind `skilus profile delete`.
type DeleteProfile struct {
	Name  string
	Scope agent.Scope
}

// EditProfileHandler changes the profiles declared in skilus.yaml. It
// never installs or removes skills: skilus profile use does that.
type EditProfileHandler struct {
	Manifest ManifestReader
	Profiles ProfileRepository
	Catalog  AgentCatalog
}

// Create declares a new profile. Its skills must already be in skills:
// and its agents in the catalog.
func (h EditProfileHandler) Create(ctx context.Context, cmd CreateProfile) (profile.Profile, error) {
	m, err := h.Manifest.Manifest(ctx, cmd.Scope)
	if err != nil {
		return profile.Profile{}, err
	}
	if slices.ContainsFunc(m.Profiles, func(p profile.Profile) bool { return p.Name() == cmd.Name }) {
		return profile.Profile{}, fmt.Errorf("profile %q is already in skilus.yaml: %w", cmd.Name, domain.ErrAlreadyExists)
	}
	skills, err := declaredSkills(m, cmd.Skills)
	if err != nil {
		return profile.Profile{}, err
	}
	agents, err := h.knownAgents(ctx, cmd.Agents)
	if err != nil {
		return profile.Profile{}, err
	}
	p, err := profile.New(cmd.Name, skills, agents)
	if err != nil {
		return profile.Profile{}, err
	}
	return p, h.Profiles.SaveProfile(ctx, cmd.Scope, p)
}

// Change adds and removes skills and agents of an existing profile. Adding
// what the profile already has is a no-op; removing what it does not have
// is an error, so a typo does not pass unnoticed.
func (h EditProfileHandler) Change(ctx context.Context, cmd ChangeProfile) (profile.Profile, error) {
	m, err := h.Manifest.Manifest(ctx, cmd.Scope)
	if err != nil {
		return profile.Profile{}, err
	}
	p, err := findProfile(m, cmd.Name)
	if err != nil {
		return profile.Profile{}, err
	}

	addSkills, err := declaredSkills(m, cmd.AddSkills)
	if err != nil {
		return profile.Profile{}, err
	}
	skills := p.Skills()
	for _, n := range addSkills {
		if !slices.Contains(skills, n) {
			skills = append(skills, n)
		}
	}
	for _, s := range cmd.RemoveSkills {
		i := slices.IndexFunc(skills, func(n skill.Name) bool { return n.String() == s })
		if i < 0 {
			return profile.Profile{}, fmt.Errorf("profile %s does not include skill %s: %w", p.Name(), s, domain.ErrNotFound)
		}
		skills = slices.Delete(skills, i, i+1)
	}

	addAgents, err := h.knownAgents(ctx, cmd.AddAgents)
	if err != nil {
		return profile.Profile{}, err
	}
	agents := p.Agents()
	for _, id := range addAgents {
		if !slices.Contains(agents, id) {
			agents = append(agents, id)
		}
	}
	for _, a := range cmd.RemoveAgents {
		i := slices.IndexFunc(agents, func(id agent.ID) bool { return id.String() == a })
		if i < 0 {
			return profile.Profile{}, fmt.Errorf("profile %s does not include agent %s: %w", p.Name(), a, domain.ErrNotFound)
		}
		agents = slices.Delete(agents, i, i+1)
	}

	changed, err := profile.New(p.Name(), skills, agents)
	if err != nil {
		return profile.Profile{}, err
	}
	return changed, h.Profiles.SaveProfile(ctx, cmd.Scope, changed)
}

// Delete drops a profile from skilus.yaml. Installed skills stay.
func (h EditProfileHandler) Delete(ctx context.Context, cmd DeleteProfile) error {
	m, err := h.Manifest.Manifest(ctx, cmd.Scope)
	if err != nil {
		return err
	}
	if _, err := findProfile(m, cmd.Name); err != nil {
		return err
	}
	return h.Profiles.DeleteProfile(ctx, cmd.Scope, cmd.Name)
}

// declaredSkills parses names and checks skills: declares each one, since
// that is where skilus profile use finds their sources.
func declaredSkills(m Manifest, names []string) ([]skill.Name, error) {
	out := make([]skill.Name, 0, len(names))
	for _, s := range names {
		n, err := skill.NewName(s)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(m.Skills, func(e ManifestEntry) bool { return e.Name == n }) {
			return nil, fmt.Errorf("skill %s is not in skills: of skilus.yaml; add it first with skilus add: %w", n, domain.ErrInvalid)
		}
		out = append(out, n)
	}
	return out, nil
}

func (h EditProfileHandler) knownAgents(ctx context.Context, ids []string) ([]agent.ID, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	known, err := h.Catalog.Agents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]agent.ID, 0, len(ids))
	for _, s := range ids {
		id, err := agent.NewID(s)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(known, func(a agent.Agent) bool { return a.ID() == id }) {
			names := make([]string, len(known))
			for i, a := range known {
				names[i] = a.ID().String()
			}
			return nil, fmt.Errorf("unknown agent %s; agents: %s: %w", id, strings.Join(names, ", "), domain.ErrInvalid)
		}
		out = append(out, id)
	}
	return out, nil
}

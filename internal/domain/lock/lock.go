// Package lock models skilus.lock: the record of exactly which skill content
// is installed, pinned to a commit and a content hash.
package lock

import (
	"fmt"
	"sort"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/skill"
)

// Lockfile is the aggregate root: the set of installed skills, one entry
// per name. Use New for an empty lock and Restore when loading one.
type Lockfile struct {
	entries map[skill.Name]Entry
	events  []Event
}

// New returns an empty lockfile.
func New() *Lockfile {
	return &Lockfile{entries: map[skill.Name]Entry{}}
}

// Restore rebuilds a lockfile from persisted entries without recording
// events. Only repository adapters should call it.
func Restore(entries []Entry) (*Lockfile, error) {
	l := New()
	for _, e := range entries {
		if _, ok := l.entries[e.Skill]; ok {
			return nil, fmt.Errorf("lock has skill %s twice: %w", e.Skill, domain.ErrConflict)
		}
		l.entries[e.Skill] = e
	}
	return l, nil
}

// Install records a newly installed skill.
func (l *Lockfile) Install(e Entry) error {
	if _, ok := l.entries[e.Skill]; ok {
		return fmt.Errorf("skill %s is already installed: %w", e.Skill, domain.ErrAlreadyExists)
	}
	l.entries[e.Skill] = e
	l.events = append(l.events, SkillInstalled{Entry: e})
	return nil
}

// Update replaces the entry of an installed skill, e.g. after moving to a
// newer commit.
func (l *Lockfile) Update(e Entry) error {
	prev, ok := l.entries[e.Skill]
	if !ok {
		return fmt.Errorf("skill %s is not installed: %w", e.Skill, domain.ErrNotFound)
	}
	l.entries[e.Skill] = e
	l.events = append(l.events, SkillUpdated{Previous: prev, Current: e})
	return nil
}

// Remove drops an installed skill.
func (l *Lockfile) Remove(name skill.Name) error {
	e, ok := l.entries[name]
	if !ok {
		return fmt.Errorf("skill %s is not installed: %w", name, domain.ErrNotFound)
	}
	delete(l.entries, name)
	l.events = append(l.events, SkillRemoved{Entry: e})
	return nil
}

// Entry returns the entry for name.
func (l *Lockfile) Entry(name skill.Name) (Entry, bool) {
	e, ok := l.entries[name]
	return e, ok
}

// Entries returns all entries sorted by skill name, for stable output.
func (l *Lockfile) Entries() []Entry {
	out := make([]Entry, 0, len(l.entries))
	for _, e := range l.entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Skill.String() < out[j].Skill.String() })
	return out
}

// PullEvents returns the events recorded since the last call and clears them.
func (l *Lockfile) PullEvents() []Event {
	ev := l.events
	l.events = nil
	return ev
}

// Entry is one installed skill.
type Entry struct {
	Skill     skill.Name
	Source    string // normalized source, e.g. github.com/owner/repo
	Requested string // ref the user asked for: tag, branch or SHA
	Commit    string // resolved commit SHA; empty for local sources
	Path      string // directory of the skill inside the source
	TreeHash  skill.TreeHash
	// Executables lists the executable files, so the tree hash can be
	// checked on file systems that do not keep the executable bit.
	Executables []string
	Targets     []agent.Target
}

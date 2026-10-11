// Package lock models skilus.lock: the record of exactly which skill content
// is installed, pinned to a commit and a content hash.
package lock

import (
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
			return nil, domain.Errorf("el lock tiene la skill %s dos veces: %w", e.Skill, domain.ErrConflict)
		}
		l.entries[e.Skill] = e
	}
	return l, nil
}

// Install records a newly installed skill.
func (l *Lockfile) Install(e Entry) error {
	if _, ok := l.entries[e.Skill]; ok {
		return domain.Errorf("la skill %s ya está instalada: %w", e.Skill, domain.ErrAlreadyExists)
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
		return domain.Errorf("la skill %s no está instalada: %w", e.Skill, domain.ErrNotFound)
	}
	l.entries[e.Skill] = e
	l.events = append(l.events, SkillUpdated{Previous: prev, Current: e})
	return nil
}

// Remove drops an installed skill.
func (l *Lockfile) Remove(name skill.Name) error {
	e, ok := l.entries[name]
	if !ok {
		return domain.Errorf("la skill %s no está instalada: %w", name, domain.ErrNotFound)
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
	// Requires lists the installed skills this one needs.
	Requires []skill.Name
	// Dependency marks a skill installed only because another needs it;
	// it goes away when nothing needs it any more.
	Dependency bool
}

// Needs reports whether e requires n.
func (e Entry) Needs(n skill.Name) bool {
	for _, r := range e.Requires {
		if r == n {
			return true
		}
	}
	return false
}

// Dependents returns the installed skills, other than those in except,
// that require name.
func (l *Lockfile) Dependents(name skill.Name, except map[skill.Name]bool) []skill.Name {
	var out []skill.Name
	for _, e := range l.Entries() {
		if !except[e.Skill] && e.Needs(name) {
			out = append(out, e.Skill)
		}
	}
	return out
}

// Unneeded returns the dependency entries nothing would need once the
// skills in gone are removed, following chains: a dependency only needed
// by another unneeded dependency is unneeded too. gone is not changed.
func (l *Lockfile) Unneeded(gone map[skill.Name]bool) []Entry {
	removed := make(map[skill.Name]bool, len(gone))
	for n := range gone {
		removed[n] = true
	}
	var out []Entry
	for changed := true; changed; {
		changed = false
		for _, e := range l.Entries() {
			if removed[e.Skill] || !e.Dependency {
				continue
			}
			if len(l.Dependents(e.Skill, removed)) == 0 {
				removed[e.Skill] = true
				out = append(out, e)
				changed = true
			}
		}
	}
	return out
}

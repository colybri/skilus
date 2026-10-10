package skill

import (
	"fmt"
	"strings"

	"github.com/colybri/skilus/internal/domain"
)

// RequiresKey is the key under metadata: in SKILL.md that lists the skills
// a skill needs. The Agent Skills specification keeps metadata values as
// strings, so the list is a string: entries separated by spaces or commas,
// each a skill name of the same source or source#name for another one,
// where source is anything skilus add accepts.
//
//	metadata:
//	  requires: "pdf anthropics/skills@v1#docx"
const RequiresKey = "requires"

// Requirement is one skill another skill needs.
type Requirement struct {
	// Source is where to find it, as skilus add takes it; empty means the
	// source of the skill that requires it.
	Source string
	Name   Name
}

func (r Requirement) String() string {
	if r.Source == "" {
		return r.Name.String()
	}
	return r.Source + "#" + r.Name.String()
}

// ParseRequires reads the value of metadata.requires.
func ParseRequires(s string) ([]Requirement, error) {
	var out []Requirement
	seen := map[Name]bool{}
	for _, field := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		src, raw, ok := strings.Cut(field, "#")
		if !ok {
			src, raw = "", field
		}
		if ok && src == "" {
			return nil, fmt.Errorf("requirement %q has an empty source: %w", field, domain.ErrInvalid)
		}
		n, err := NewName(raw)
		if err != nil {
			return nil, fmt.Errorf("requirement %q: %w", field, err)
		}
		if seen[n] {
			return nil, fmt.Errorf("skill %s is required twice: %w", n, domain.ErrInvalid)
		}
		seen[n] = true
		out = append(out, Requirement{Source: src, Name: n})
	}
	return out, nil
}

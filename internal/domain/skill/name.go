// Package skill models a skill package as defined by the Agent Skills
// specification: a directory with a SKILL.md file.
package skill

import (
	"fmt"
	"regexp"

	"github.com/colybri/skilus/internal/domain"
)

const maxNameLen = 64

var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// Name is a skill name: 1 to 64 lowercase letters, digits and hyphens, not
// starting or ending with a hyphen and without consecutive hyphens.
type Name struct {
	value string
}

// NewName validates s and returns it as a Name.
func NewName(s string) (Name, error) {
	switch {
	case s == "":
		return Name{}, fmt.Errorf("skill name is empty: %w", domain.ErrInvalid)
	case len(s) > maxNameLen:
		return Name{}, fmt.Errorf("skill name %q is longer than %d characters: %w", s, maxNameLen, domain.ErrInvalid)
	case !nameRe.MatchString(s) || containsDoubleHyphen(s):
		return Name{}, fmt.Errorf("skill name %q must use lowercase letters, digits and single hyphens: %w", s, domain.ErrInvalid)
	}
	return Name{value: s}, nil
}

func (n Name) String() string { return n.value }

// IsZero reports whether n was never set.
func (n Name) IsZero() bool { return n.value == "" }

func containsDoubleHyphen(s string) bool {
	for i := 1; i < len(s); i++ {
		if s[i] == '-' && s[i-1] == '-' {
			return true
		}
	}
	return false
}

package skill_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
)

func TestNewName(t *testing.T) {
	tests := []struct {
		in    string
		valid bool
	}{
		{"code-review", true},
		{"pdf", true},
		{"a1", true},
		{strings.Repeat("a", 64), true},
		{"", false},
		{strings.Repeat("a", 65), false},
		{"Code-Review", false},
		{"-review", false},
		{"review-", false},
		{"code--review", false},
		{"code_review", false},
		{"../evil", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			n, err := skill.NewName(tt.in)
			if tt.valid {
				if err != nil {
					t.Fatalf("NewName(%q) error = %v", tt.in, err)
				}
				if n.String() != tt.in {
					t.Fatalf("String() = %q, want %q", n.String(), tt.in)
				}
				return
			}
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("NewName(%q) error = %v, want ErrInvalid", tt.in, err)
			}
		})
	}
}

func TestNewTreeHash(t *testing.T) {
	good := strings.Repeat("ab", 32)
	h, err := skill.NewTreeHash(good)
	if err != nil {
		t.Fatalf("NewTreeHash(valid) error = %v", err)
	}
	if h.Short() != good[:12] {
		t.Fatalf("Short() = %q", h.Short())
	}
	for _, bad := range []string{"", "abc", strings.Repeat("AB", 32), strings.Repeat("g", 64)} {
		if _, err := skill.NewTreeHash(bad); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("NewTreeHash(%q) error = %v, want ErrInvalid", bad, err)
		}
	}
}

package profile_test

import (
	"errors"
	"testing"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/profile"
	"github.com/colybri/skilus/internal/domain/skill"
)

func names(t *testing.T, ss ...string) []skill.Name {
	t.Helper()
	out := make([]skill.Name, 0, len(ss))
	for _, s := range ss {
		n, err := skill.NewName(s)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func TestNewValidates(t *testing.T) {
	codex, _ := agent.NewID("codex")
	cases := []struct {
		name   string
		skills []skill.Name
		agents []agent.ID
	}{
		{"Backend", nil, nil},
		{"-x", nil, nil},
		{"web", names(t, "a", "a"), nil},
		{"web", nil, []agent.ID{codex, codex}},
	}
	for _, c := range cases {
		if _, err := profile.New(c.name, c.skills, c.agents); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("New(%q) = %v, want ErrInvalid", c.name, err)
		}
	}
	if _, err := profile.New("data_science", nil, nil); err != nil {
		t.Errorf("empty profile: %v", err)
	}
}

func TestMatches(t *testing.T) {
	p, err := profile.New("web", names(t, "a", "b"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Matches(names(t, "b", "a")) {
		t.Error("same set should match")
	}
	if p.Matches(names(t, "a")) || p.Matches(names(t, "a", "c")) || p.Matches(names(t, "a", "b", "c")) {
		t.Error("different sets should not match")
	}
}

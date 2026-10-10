package policy_test

import (
	"testing"

	"github.com/colybri/skilus/internal/domain/policy"
)

func TestTrustCovers(t *testing.T) {
	trust := policy.Trust{"github.com/anthropics", "github.com/obra/superpowers/", "https://example.com/dl"}
	for id, want := range map[string]bool{
		"github.com/anthropics/skills":       true,
		"GitHub.com/Anthropics/skills":       true,
		"github.com/anthropics":              true,
		"github.com/anthropics-evil/skills":  false,
		"github.com/obra/superpowers":        true,
		"github.com/obra/other":              false,
		"https://example.com/dl/skills.zip":  true,
		"https://example.com/dlx/skills.zip": false,
	} {
		if got := trust.Covers(id); got != want {
			t.Errorf("Covers(%q) = %v, want %v", id, got, want)
		}
	}
	if !(policy.Trust{}).Covers("anything") {
		t.Error("an empty list must trust everything")
	}
	if f := policy.CheckSource("github.com/x/y", trust); len(f) != 1 || f[0].Severity != policy.Warn || f[0].Code != policy.CodeUntrusted {
		t.Errorf("CheckSource = %+v", f)
	}
}

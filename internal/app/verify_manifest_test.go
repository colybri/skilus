package app_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/profile"
	"github.com/colybri/skilus/internal/domain/skill"
)

type brokenManifest struct{}

func (brokenManifest) Manifest(context.Context, agent.Scope) (app.Manifest, error) {
	return app.Manifest{}, fmt.Errorf("skilus.yaml: yaml: line 3: did not find expected key: %w", domain.ErrInvalid)
}

// verifyManifest runs verify on a lock without targets, so only the
// skilus.yaml checks can fail.
func verifyManifest(t *testing.T, m app.ManifestReader, locked map[string]bool) ([]string, error) {
	t.Helper()
	var entries []lock.Entry
	for _, s := range []string{"a", "b", "c", "d"} {
		dep, ok := locked[s]
		if !ok {
			continue
		}
		n, _ := skill.NewName(s)
		h, _ := skill.NewTreeHash("sha256:" + fmt.Sprintf("%064d", 0))
		entries = append(entries, lock.Entry{Skill: n, Source: "./skills", Path: s, TreeHash: h, Dependency: dep})
	}
	lf, err := lock.Restore(entries)
	if err != nil {
		t.Fatal(err)
	}
	h := app.VerifyHandler{Catalog: fakeCatalog{}, Locks: &fakeLocks{lf: lf}, Manifest: m}
	res, err := h.Handle(context.Background(), app.Verify{Scopes: []agent.Scope{agent.ScopeProject}})
	var got []string
	for _, c := range res.Manifest {
		got = append(got, fmt.Sprintf("%s %s %s", c.Problem, c.Skill, c.Profile))
	}
	return got, err
}

func declares(t *testing.T, profiles []profile.Profile, names ...string) *fakeManifest {
	t.Helper()
	m := &fakeManifest{app.Manifest{Profiles: profiles}}
	for _, s := range names {
		n, _ := skill.NewName(s)
		m.m.Skills = append(m.m.Skills, app.ManifestEntry{Name: n, Source: "./skills"})
	}
	return m
}

func TestVerifyManifestAgreesWithLock(t *testing.T) {
	got, err := verifyManifest(t, declares(t, nil, "a", "b"), map[string]bool{"a": false, "b": false, "c": true})
	if err != nil || len(got) != 0 {
		t.Fatalf("matching manifest: %v %v", got, err)
	}
}

func TestVerifyManifestDrift(t *testing.T) {
	got, err := verifyManifest(t, declares(t, nil, "a", "b"), map[string]bool{"a": false, "d": false})
	if !errors.Is(err, app.ErrDrift) {
		t.Errorf("err = %v, want ErrDrift", err)
	}
	want := []string{"not-installed b ", "not-declared d "}
	if !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestVerifyManifestActiveProfile(t *testing.T) {
	// With web active the lock holds only a; b belongs to another profile.
	web := mustProfile(t, "web", []string{"a"})
	api := mustProfile(t, "api", []string{"b"})
	got, err := verifyManifest(t, declares(t, []profile.Profile{web, api}, "a", "b"), map[string]bool{"a": false})
	if err != nil || len(got) != 0 {
		t.Fatalf("active profile: %v %v", got, err)
	}
}

func TestVerifyManifestInvalid(t *testing.T) {
	broken := mustProfile(t, "broken", []string{"a", "nope"})
	got, err := verifyManifest(t, declares(t, []profile.Profile{broken}, "a"), map[string]bool{"a": false})
	if !errors.Is(err, app.ErrInvalidManifest) || !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalidManifest", err)
	}
	if want := []string{"profile-undeclared nope broken"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}

	got, err = verifyManifest(t, brokenManifest{}, map[string]bool{"a": false})
	if !errors.Is(err, app.ErrInvalidManifest) {
		t.Errorf("err = %v, want ErrInvalidManifest", err)
	}
	if want := []string{"manifest-invalid  "}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

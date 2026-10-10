package lock_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/lock"
	"github.com/colybri/skilus/internal/domain/skill"
)

func entry(t *testing.T, name, hashByte string) lock.Entry {
	t.Helper()
	n, err := skill.NewName(name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := skill.NewTreeHash(strings.Repeat(hashByte, 64))
	if err != nil {
		t.Fatal(err)
	}
	return lock.Entry{Skill: n, Source: "github.com/foo/bar", Commit: strings.Repeat("a", 40), TreeHash: h}
}

func TestInstallRecordsEventAndRejectsDuplicates(t *testing.T) {
	l := lock.New()
	e := entry(t, "code-review", "1")

	if err := l.Install(e); err != nil {
		t.Fatal(err)
	}
	if err := l.Install(e); !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("second Install error = %v, want ErrAlreadyExists", err)
	}

	events := l.PullEvents()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if _, ok := events[0].(lock.SkillInstalled); !ok {
		t.Fatalf("event = %T, want SkillInstalled", events[0])
	}
	if len(l.PullEvents()) != 0 {
		t.Fatal("PullEvents did not clear events")
	}
}

func TestUpdateAndRemoveRequireInstalledSkill(t *testing.T) {
	l := lock.New()
	e := entry(t, "pdf", "2")
	if err := l.Update(e); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update error = %v, want ErrNotFound", err)
	}
	if err := l.Remove(e.Skill); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Remove error = %v, want ErrNotFound", err)
	}

	_ = l.Install(e)
	newer := entry(t, "pdf", "3")
	if err := l.Update(newer); err != nil {
		t.Fatal(err)
	}
	got, _ := l.Entry(e.Skill)
	if got.TreeHash != newer.TreeHash {
		t.Fatalf("TreeHash = %s, want %s", got.TreeHash, newer.TreeHash)
	}
	if err := l.Remove(e.Skill); err != nil {
		t.Fatal(err)
	}
	if len(l.Entries()) != 0 {
		t.Fatal("entry still present after Remove")
	}
}

func TestRestoreDoesNotRecordEventsAndSorts(t *testing.T) {
	l, err := lock.Restore([]lock.Entry{entry(t, "zeta", "4"), entry(t, "alpha", "5")})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.PullEvents()) != 0 {
		t.Fatal("Restore recorded events")
	}
	es := l.Entries()
	if es[0].Skill.String() != "alpha" || es[1].Skill.String() != "zeta" {
		t.Fatalf("Entries not sorted: %v, %v", es[0].Skill, es[1].Skill)
	}
	if _, err := lock.Restore([]lock.Entry{entry(t, "a", "6"), entry(t, "a", "7")}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Restore duplicate error = %v, want ErrConflict", err)
	}
}

func TestUnneededFollowsChains(t *testing.T) {
	n := func(s string) skill.Name {
		v, err := skill.NewName(s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	mk := func(name string, dep bool, requires ...string) lock.Entry {
		e := lock.Entry{Skill: n(name), Dependency: dep}
		for _, r := range requires {
			e.Requires = append(e.Requires, n(r))
		}
		return e
	}
	// report -> pdf -> fonts; other -> fonts.
	lf, err := lock.Restore([]lock.Entry{
		mk("report", false, "pdf"),
		mk("pdf", true, "fonts"),
		mk("fonts", true),
		mk("other", false, "fonts"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if deps := lf.Dependents(n("pdf"), nil); len(deps) != 1 || deps[0] != n("report") {
		t.Fatalf("dependents of pdf = %v", deps)
	}
	got := lf.Unneeded(map[skill.Name]bool{n("report"): true})
	if len(got) != 1 || got[0].Skill != n("pdf") {
		t.Fatalf("unneeded after report = %v", got)
	}
	got = lf.Unneeded(map[skill.Name]bool{n("report"): true, n("other"): true})
	if len(got) != 2 {
		t.Fatalf("unneeded after report and other = %v", got)
	}
}

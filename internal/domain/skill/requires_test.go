package skill_test

import (
	"errors"
	"testing"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
)

func TestParseRequires(t *testing.T) {
	reqs, err := skill.ParseRequires(" pdf,anthropics/skills@v1#docx\n\tgit@host:x/y.git#z ")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, r := range reqs {
		got = append(got, r.String())
	}
	want := []string{"pdf", "anthropics/skills@v1#docx", "git@host:x/y.git#z"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if reqs, err := skill.ParseRequires(""); err != nil || len(reqs) != 0 {
		t.Errorf("empty: %v, %v", reqs, err)
	}
	for _, bad := range []string{"#pdf", "PDF", "pdf a/b#pdf", "a/b#"} {
		if _, err := skill.ParseRequires(bad); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

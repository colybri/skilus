package source_test

import (
	"errors"
	"testing"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/source"
)

func TestParse(t *testing.T) {
	tests := []struct {
		raw, url, id, ref string
		kind              source.Kind
	}{
		{"./skills", "", "./skills", "", source.KindLocal},
		{"../x@y", "", "../x@y", "", source.KindLocal},
		{"/abs/dir", "", "/abs/dir", "", source.KindLocal},
		{`C:\skills`, "", `C:\skills`, "", source.KindLocal},
		{"anthropics/skills", "https://github.com/anthropics/skills", "github.com/anthropics/skills", "", source.KindGit},
		{"anthropics/skills@v1.2.0", "https://github.com/anthropics/skills", "github.com/anthropics/skills", "v1.2.0", source.KindGit},
		{"GitHub.com/o/r.git@feature/x", "https://GitHub.com/o/r.git", "github.com/o/r", "feature/x", source.KindGit},
		{"https://gitlab.com/g/sub/r.git", "https://gitlab.com/g/sub/r.git", "gitlab.com/g/sub/r", "", source.KindGit},
		{"https://me@github.com/o/r@main", "https://me@github.com/o/r", "github.com/o/r", "main", source.KindGit},
		{"git@github.com:o/r.git@v1", "git@github.com:o/r.git", "git@github.com:o/r.git", "v1", source.KindGit},
		{"ssh://git@host/o/r", "ssh://git@host/o/r", "ssh://git@host/o/r", "", source.KindGit},
		{"file:///tmp/repo@0123abc", "file:///tmp/repo", "file:///tmp/repo", "0123abc", source.KindGit},
		{"https://example.com/dl/skills.zip", "https://example.com/dl/skills.zip", "https://example.com/dl/skills.zip", "", source.KindArchive},
		{"https://github.com/o/r/archive/v1@x.tar.gz", "https://github.com/o/r/archive/v1@x.tar.gz", "https://github.com/o/r/archive/v1@x.tar.gz", "", source.KindArchive},
		{"file:///tmp/s.TGZ", "file:///tmp/s.TGZ", "file:///tmp/s.TGZ", "", source.KindArchive},
	}
	for _, tt := range tests {
		got, err := source.Parse(tt.raw)
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.raw, err)
			continue
		}
		if got.Kind != tt.kind || got.URL != tt.url || got.ID != tt.id || got.Ref != tt.ref {
			t.Errorf("Parse(%q) = %+v", tt.raw, got)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, raw := range []string{
		"",
		"skills",                         // a bare word is neither
		"a/b/c",                          // three segments without a host
		"o/r@",                           // empty ref
		"o/r@-upload-pack=x",             // option injection
		"o/r@a..b",                       // invalid ref
		"https://u:token@github.com/o/r", // credentials
		"http://github.com/o/r",          // plain HTTP
		"git://github.com/o/r",           // unauthenticated protocol
		"https://x.com/s.zip?token=abc",  // a token would end up in the lock
		"https://me@x.com/s.zip",         // credentials
		"http://x.com/s.zip",             // plain HTTP
	} {
		if _, err := source.Parse(raw); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("Parse(%q) err = %v, want ErrInvalid", raw, err)
		}
	}
}

func TestString(t *testing.T) {
	s, err := source.Parse("o/r@v1")
	if err != nil {
		t.Fatal(err)
	}
	if s.String() != "o/r@v1" {
		t.Fatalf("String() = %q", s.String())
	}
}

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/skill"
)

func TestWrap(t *testing.T) {
	tests := []struct {
		text     string
		width    int
		maxLines int
		want     []string
	}{
		{"one two three", 0, 0, []string{"one two three"}},
		{"", 30, 0, []string{""}},
		{"aaaa bbbb cccc dddd eeee", 20, 0, []string{"aaaa bbbb cccc dddd", "eeee"}},
		{"aaaa bbbb cccc dddd eeee ffff gggg hhhh iiii", 20, 2, []string{"aaaa bbbb cccc dddd", "eeee ffff gggg hhhh…"}},
		{strings.Repeat("x", 25), 20, 0, []string{strings.Repeat("x", 20), "xxxxx"}},
		// CJK has no spaces and takes two columns per character.
		{strings.Repeat("技", 12), 20, 0, []string{strings.Repeat("技", 10), "技技"}},
	}
	for _, tt := range tests {
		got := wrap(tt.text, tt.width, tt.maxLines)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("wrap(%q, %d, %d) = %q, want %q", tt.text, tt.width, tt.maxLines, got, tt.want)
		}
		for _, l := range got {
			if tt.width > 0 && textWidth(l) > tt.width {
				t.Errorf("wrap(%q): %q is wider than %d", tt.text, l, tt.width)
			}
		}
	}
}

func TestTextWidth(t *testing.T) {
	tests := map[string]int{"abc": 3, "ñandú": 5, "日本語": 6, "한글": 4, "e\u0301": 1, "": 0}
	for s, want := range tests {
		if got := textWidth(s); got != want {
			t.Errorf("textWidth(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestSize(t *testing.T) {
	es, en := load(t, "es"), load(t, "en")
	tests := []struct {
		n    int64
		t    *i18n.Catalog
		want string
	}{
		{512, es, "512 B"},
		{9011, es, "8,8 KiB"},
		{9011, en, "8.8 KiB"},
		{21504, es, "21 KiB"},
		{3 << 20, es, "3,0 MiB"},
		{25 << 20, en, "25 MiB"},
	}
	for _, tt := range tests {
		if got := size(tt.n, tt.t); got != tt.want {
			t.Errorf("size(%d, %s) = %q, want %q", tt.n, tt.t.Lang(), got, tt.want)
		}
	}
}

func TestCount(t *testing.T) {
	if got := count(1, "%d fichero", "%d ficheros"); got != "1 fichero" {
		t.Errorf("count(1) = %q", got)
	}
	if got := count(0, "%d fichero", "%d ficheros"); got != "0 ficheros" {
		t.Errorf("count(0) = %q", got)
	}
}

func TestRenderPlan(t *testing.T) {
	name, _ := skill.NewName("review")
	desc := "Reviews the diff of the current branch against the main branch and suggests changes, one by one, with reasons."
	pkg, err := skill.NewPackage(name, desc, []skill.File{
		{Path: "SKILL.md", Kind: skill.KindRegular, Data: []byte("x")},
		{Path: "run.sh", Kind: skill.KindExecutable, Data: []byte("y")},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := policy.Report{
		Executables: []string{"run.sh"},
		Findings: []policy.Finding{
			{Code: policy.CodeExecutable, Severity: policy.Warn, Path: "run.sh", Detail: domain.Msg("fichero ejecutable; acéptalo con allow: [scripts]")},
			{Code: policy.CodeSymlinkEscape, Severity: policy.Block, Path: "leak", Detail: domain.Msg("apunta a %s, fuera de la skill", "../x")},
		},
	}
	plan := app.InstallPlan{Source: "owner/repo", Commit: strings.Repeat("a", 40), Skills: []app.PlannedSkill{{Package: pkg, Report: report}}}

	var b bytes.Buffer
	renderPlan(&b, plan, style{width: 40}, load(t, "es"))
	want := `owner/repo · commit aaaaaaaaaaaa · 1 skill

● review   2 ficheros · 2 B · sha256 ` + pkg.TreeHash().Short() + `
  Reviews the diff of the current branch
  against the main branch and suggests…
  ! run.sh: fichero ejecutable; acéptalo
    con allow: [scripts] [executable]
  ✗ leak: apunta a ../x, fuera de la
    skill [symlink-escape]

1 skill · 1 bloqueo · 1 aviso
`
	if b.String() != want {
		t.Errorf("renderPlan() =\n%s\nwant\n%s", b.String(), want)
	}

	// Colors wrap the marks and the tag, and --full shows the whole description.
	b.Reset()
	renderPlan(&b, plan, style{width: 40, color: true, full: true}, load(t, "es"))
	for _, s := range []string{"\x1b[1mreview\x1b[0m", "\x1b[33m!\x1b[0m", "\x1b[31m✗\x1b[0m", "\x1b[2m[executable]\x1b[0m", "one by one, with reasons."} {
		if !strings.Contains(b.String(), s) {
			t.Errorf("renderPlan() lacks %q:\n%s", s, b.String())
		}
	}
}

func TestRenderReportAcceptedExecutables(t *testing.T) {
	var b bytes.Buffer
	renderReport(&b, policy.Report{Executables: []string{"run.sh"}}, style{}, load(t, "es"))
	if got := b.String(); got != "  ✓ ejecutable aceptado: run.sh\n" {
		t.Errorf("renderReport() = %q", got)
	}
	b.Reset()
	renderReport(&b, policy.Report{}, style{}, load(t, "es"))
	if got := b.String(); got != "  ✓ sin hallazgos\n" {
		t.Errorf("renderReport() = %q", got)
	}
}

func load(t *testing.T, lang string) *i18n.Catalog {
	t.Helper()
	c, err := i18n.Load(lang)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

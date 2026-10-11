package yamlrepo_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colybri/skilus/internal/adapter/yamlrepo"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
)

func TestManifestFileRoundTrip(t *testing.T) {
	ctx := context.Background()
	r := yamlrepo.Repo{ProjectRoot: t.TempDir(), GlobalDir: filepath.Join(t.TempDir(), ".skilus")}
	if got := r.ManifestPath(agent.ScopeGlobal); got != filepath.Join(r.GlobalDir, "skilus.yaml") {
		t.Errorf("global path = %s", got)
	}
	if _, exists, err := r.ReadManifest(ctx, agent.ScopeGlobal); exists || err != nil {
		t.Fatalf("ReadManifest before writing = %v, %v", exists, err)
	}
	// The text is kept byte for byte: comments and CRLF included.
	text := "# mine\r\nversion: 1\r\nskills: []\r\n"
	if err := r.WriteManifest(ctx, agent.ScopeGlobal, []byte(text)); err != nil {
		t.Fatal(err)
	}
	data, exists, err := r.ReadManifest(ctx, agent.ScopeGlobal)
	if err != nil || !exists || string(data) != text {
		t.Fatalf("ReadManifest = %q, %v, %v", data, exists, err)
	}
	if raw, _ := os.ReadFile(r.ManifestPath(agent.ScopeGlobal)); string(raw) != text {
		t.Errorf("on disk = %q", raw)
	}
}

func TestParseManifest(t *testing.T) {
	ctx := context.Background()
	r := yamlrepo.Repo{ProjectRoot: t.TempDir()}
	m, err := r.ParseManifest(ctx, agent.ScopeProject, []byte(`# comment
version: 1
skills:
  - name: pdf
    source: anthropics/skills
profiles:
  web:
    skills: [pdf]
trust: [github.com/anthropics]
indexes: [https://example.com/index.yaml]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Skills) != 1 || len(m.Profiles) != 1 || len(m.Indexes) != 1 {
		t.Errorf("manifest = %+v", m)
	}
	for _, empty := range []string{"", "# only comments\n"} {
		if _, err := r.ParseManifest(ctx, agent.ScopeProject, []byte(empty)); err != nil {
			t.Errorf("ParseManifest(%q) = %v", empty, err)
		}
	}

	for text, want := range map[string]string{
		"skills: [\n":                          "skilus.yaml",
		"- pdf\n":                              "mapa",
		"version: 1\nskils: []\n":              `"skils" en la línea 2`,
		"version: 2\n":                         "version debe ser 1",
		"trust: github.com/anthropics\n":       "trust debe ser una lista",
		"skills:\n  - name: pdf\n":             "no tiene origen",
		"profiles:\n  web:\n    skills: [X]\n": "perfil web",
	} {
		_, err := r.ParseManifest(ctx, agent.ScopeProject, []byte(text))
		if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseManifest(%q) = %v; want ErrInvalid mentioning %q", text, err, want)
		}
	}
}

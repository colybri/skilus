package yamlrepo_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colybri/skilus/internal/adapter/yamlrepo"
)

func TestLanguageSetting(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	r := yamlrepo.Repo{GlobalDir: dir}
	if got, err := r.Language(ctx); err != nil || got != "" {
		t.Fatalf("no file: %q, %v", got, err)
	}
	path := filepath.Join(dir, yamlrepo.SettingsFile)
	if err := os.WriteFile(path, []byte("other: kept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.SetLanguage(ctx, "fr"); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Language(ctx); got != "fr" {
		t.Fatalf("language = %q", got)
	}
	if err := r.SetLanguage(ctx, ""); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "language") || !strings.Contains(string(raw), "other: kept") {
		t.Fatalf("config.yaml = %q", raw)
	}
}

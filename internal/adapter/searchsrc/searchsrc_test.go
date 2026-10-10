package searchsrc_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colybri/skilus/internal/adapter/searchsrc"
	"github.com/colybri/skilus/internal/domain"
)

// fileURL turns a path into a file URL; on Windows C:\x becomes file:///C:/x.
func fileURL(p string) string {
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

func TestSkillsSHSearch(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" || r.URL.Query().Get("q") != "pdf tools" || r.URL.Query().Get("owner") != "anthropics" || r.URL.Query().Get("limit") != "5" {
			t.Errorf("request = %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"skills":[
			{"id":"anthropics/skills/pdf","name":"pdf","installs":120,"source":"anthropics/skills"},
			{"id":"x/y/Bad","name":"Bad Name","installs":3,"source":"x/y"},
			{"id":"x/y/z","name":"z","installs":3,"source":"../etc"}]}`))
	}))
	defer srv.Close()

	r := searchsrc.SkillsSH{BaseURL: srv.URL, Client: srv.Client()}
	got, err := r.Search(context.Background(), "pdf tools", "anthropics", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "pdf" || got[0].Source != "anthropics/skills" || got[0].Installs != 120 {
		t.Fatalf("got %+v", got)
	}

	if _, err := (searchsrc.SkillsSH{BaseURL: "http://example.com"}).Search(context.Background(), "x", "", 1); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("http base: err = %v, want ErrInvalid", err)
	}
}

func TestIndexes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		return fileURL(p)
	}
	ok := write("ok.yaml", "version: 1\nskills:\n  - name: pdf\n    source: anthropics/skills@v1\n    description: PDF\n")
	got, err := searchsrc.Indexes{}.Index(context.Background(), ok)
	if err != nil || len(got) != 1 || got[0].Source != "anthropics/skills@v1" || got[0].Description != "PDF" {
		t.Fatalf("got %+v, %v", got, err)
	}

	for name, data := range map[string]string{
		"version.yaml": "version: 2\nskills: []\n",
		"name.yaml":    "version: 1\nskills:\n  - name: Bad\n    source: a/b\n",
		"local.yaml":   "version: 1\nskills:\n  - name: x\n    source: ../secret\n",
	} {
		if _, err := (searchsrc.Indexes{}).Index(context.Background(), write(name, data)); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := (searchsrc.Indexes{}).Index(context.Background(), fileURL(filepath.Join(dir, "none.yaml"))); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing: err = %v", err)
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("version: 1\nskills:\n  - name: pdf\n    source: anthropics/skills\n"))
	}))
	defer srv.Close()
	got, err = searchsrc.Indexes{Client: srv.Client()}.Index(context.Background(), srv.URL+"/index.yaml")
	if err != nil || len(got) != 1 {
		t.Fatalf("https: %+v, %v", got, err)
	}
}

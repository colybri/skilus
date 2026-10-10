package archivesrc_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/colybri/skilus/internal/adapter/archivesrc"
	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

type file struct {
	name, body, link string
	mode             int64
}

func manifest(name string) string {
	return "---\nname: " + name + "\ndescription: Skill " + name + "\n---\n"
}

func makeZip(t *testing.T, files []file) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		mode := os.FileMode(f.mode)
		if mode == 0 {
			mode = 0o644
		}
		body := f.body
		if f.link != "" {
			mode, body = os.ModeSymlink|0o777, f.link
		}
		h.SetMode(mode)
		fw, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeTarGz(t *testing.T, files []file) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		h := &tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.body)), Typeflag: tar.TypeReg}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if f.link != "" {
			h.Typeflag, h.Linkname, h.Size = tar.TypeSymlink, f.link, 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if f.link == "" {
			if _, err := tw.Write([]byte(f.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fileSource writes data to a temporary file and returns its file:// source.
func fileSource(t *testing.T, name string, data []byte) source.Source {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	u := filepath.ToSlash(p)
	if !strings.HasPrefix(u, "/") {
		u = "/" + u
	}
	src, err := source.Parse("file://" + u)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// repo is a GitHub-style archive: everything under one top directory.
var repo = []file{
	{name: "repo-main/README.md", body: "readme"},
	{name: "repo-main/skills/alpha/SKILL.md", body: manifest("alpha")},
	{name: "repo-main/skills/alpha/run.sh", body: "#!/bin/sh\n", mode: 0o755},
	{name: "repo-main/skills/alpha/guide.md", link: "../../README.md"},
	{name: "repo-main/skills/beta/SKILL.md", body: manifest("beta")},
}

func TestFetchZipAndTarGzGiveTheSameSkills(t *testing.T) {
	ctx := context.Background()
	fromZip, err := archivesrc.Fetcher{}.Fetch(ctx, fileSource(t, "s.zip", makeZip(t, repo)))
	if err != nil {
		t.Fatal(err)
	}
	fromTar, err := archivesrc.Fetcher{}.Fetch(ctx, fileSource(t, "s.tar.gz", makeTarGz(t, repo)))
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range [][]string{paths(fromZip.Skills), paths(fromTar.Skills)} {
		if strings.Join(got, ",") != "skills/alpha,skills/beta" {
			t.Fatalf("skills = %v", got)
		}
	}
	if fromZip.Commit != "" || fromZip.Source == "" {
		t.Fatalf("fetched = %+v", fromZip)
	}
	alpha := fromZip.Skills[0].Package
	if alpha.TreeHash() != fromTar.Skills[0].Package.TreeHash() {
		t.Fatal("same files, different hash")
	}
	kinds := map[string]skill.Kind{}
	for _, f := range alpha.Files() {
		kinds[f.Path] = f.Kind
	}
	if kinds["run.sh"] != skill.KindExecutable || kinds["guide.md"] != skill.KindSymlink {
		t.Fatalf("kinds = %v", kinds)
	}
}

func paths(skills []app.FetchedSkill) []string {
	var out []string
	for _, s := range skills {
		out = append(out, s.Path)
	}
	return out
}

func TestFetchRejectsUnsafeArchives(t *testing.T) {
	ctx := context.Background()
	tests := map[string]struct {
		files []file
		f     archivesrc.Fetcher
	}{
		"traversal": {files: []file{{name: "../evil/SKILL.md", body: manifest("evil")}}},
		"absolute":  {files: []file{{name: "/etc/SKILL.md", body: manifest("evil")}}},
		"backslash": {files: []file{{name: `a\..\SKILL.md`, body: manifest("evil")}}},
		"bomb": {
			files: []file{{name: "SKILL.md", body: manifest("big") + strings.Repeat("x", 4096)}},
			f:     archivesrc.Fetcher{MaxUnpacked: 1024},
		},
		"entries": {
			files: []file{{name: "SKILL.md", body: manifest("a")}, {name: "b", body: "b"}, {name: "c", body: "c"}},
			f:     archivesrc.Fetcher{MaxEntries: 2},
		},
		"download": {
			files: []file{{name: "SKILL.md", body: manifest("a")}},
			f:     archivesrc.Fetcher{MaxDownload: 10},
		},
	}
	for name, tt := range tests {
		for _, ext := range []string{".zip", ".tar.gz"} {
			data := makeZip(t, tt.files)
			if ext == ".tar.gz" {
				data = makeTarGz(t, tt.files)
			}
			_, err := tt.f.Fetch(ctx, fileSource(t, "a"+ext, data))
			if !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("%s%s: err = %v, want ErrInvalid", name, ext, err)
			}
		}
	}
}

func TestFetchOverHTTPS(t *testing.T) {
	data := makeZip(t, []file{{name: "SKILL.md", body: manifest("solo")}})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/solo.zip" {
			http.NotFound(w, r)
			return
		}
		w.Write(data) //nolint:errcheck // test server
	}))
	defer srv.Close()
	f := archivesrc.Fetcher{Client: srv.Client()}
	ctx := context.Background()

	src, err := source.Parse(srv.URL + "/solo.zip")
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.Fetch(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Skills) != 1 || got.Skills[0].Path != "." || got.Source != srv.URL+"/solo.zip" {
		t.Fatalf("fetched = %+v", got)
	}

	missing, _ := source.Parse(srv.URL + "/nope.zip")
	if _, err := f.Fetch(ctx, missing); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("404 err = %v, want ErrNotFound", err)
	}
}

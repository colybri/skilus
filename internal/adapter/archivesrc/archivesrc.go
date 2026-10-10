// Package archivesrc reads skills from ZIP and tar.gz archives downloaded
// over HTTPS (or read from file:// URLs). Archives are unpacked in memory:
// nothing is written to disk, every path is validated, and the download,
// the number of entries and the unpacked size are all bounded.
package archivesrc

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/colybri/skilus/internal/adapter/skillsrc"
	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

var _ app.Fetcher = Fetcher{}

// Default limits.
const (
	DefaultMaxDownload = 50 << 20
	DefaultMaxUnpacked = 100 << 20
	DefaultMaxEntries  = 10000
)

// Fetcher implements app.Fetcher for archive sources.
type Fetcher struct {
	Client      *http.Client // nil means http.DefaultClient
	MaxDownload int64        // bytes of the archive itself
	MaxUnpacked int64        // bytes of all files once unpacked
	MaxEntries  int          // files, directories and links
}

// entry is one unpacked file, with its path inside the archive.
type entry struct {
	path string
	file skill.File
}

// Fetch implements app.Fetcher. Archives have no commit: the lock pins the
// content by its tree hash.
func (f Fetcher) Fetch(ctx context.Context, src source.Source) (app.Fetched, error) {
	data, err := f.download(ctx, src.URL)
	if err != nil {
		return app.Fetched{}, err
	}
	var entries []entry
	switch name := strings.ToLower(src.URL); {
	case strings.HasSuffix(name, ".zip"):
		entries, err = f.unzip(data)
	default:
		entries, err = f.untar(data)
	}
	if err != nil {
		return app.Fetched{}, fmt.Errorf("unpack %s: %w", src.URL, err)
	}
	entries = stripTopDir(entries)

	out := app.Fetched{Source: src.ID}
	var manifests []string
	for _, e := range entries {
		if e.file.Kind == skill.KindRegular && path.Base(e.path) == skill.ManifestFile {
			manifests = append(manifests, e.path)
		}
	}
	for _, dir := range skillsrc.Discover(manifests) {
		var files []skill.File
		for _, e := range entries {
			if rel, ok := skillsrc.Under(dir, e.path); ok {
				f := e.file
				f.Path = rel
				files = append(files, f)
			}
		}
		p, err := skillsrc.Build(files)
		if err != nil {
			out.Invalid = append(out.Invalid, app.InvalidSkill{Path: dir, Err: err})
			continue
		}
		out.Skills = append(out.Skills, app.FetchedSkill{Path: dir, Package: p})
	}
	return out, nil
}

func (f Fetcher) download(ctx context.Context, raw string) ([]byte, error) {
	limit := f.MaxDownload
	if limit <= 0 {
		limit = DefaultMaxDownload
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", raw, domain.ErrInvalid)
	}
	var body io.ReadCloser
	switch u.Scheme {
	case "file":
		file, err := os.Open(filePath(u))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s does not exist: %w", raw, domain.ErrNotFound)
		}
		if err != nil {
			return nil, err
		}
		body = file
	case "https":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		client := f.Client
		if client == nil {
			client = http.DefaultClient
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("download %s: %w", raw, err)
		}
		switch {
		case resp.StatusCode == http.StatusNotFound:
			resp.Body.Close() //nolint:errcheck // nothing to read
			return nil, fmt.Errorf("download %s: %s: %w", raw, resp.Status, domain.ErrNotFound)
		case resp.StatusCode != http.StatusOK:
			resp.Body.Close() //nolint:errcheck // nothing to read
			return nil, fmt.Errorf("download %s: %s", raw, resp.Status)
		case resp.Request.URL.Scheme != "https":
			resp.Body.Close() //nolint:errcheck // nothing to read
			return nil, fmt.Errorf("download %s: redirected to %s, which is not HTTPS: %w", raw, resp.Request.URL.Scheme, domain.ErrInvalid)
		}
		body = resp.Body
	default:
		return nil, fmt.Errorf("scheme %s is not supported for archives: %w", u.Scheme, domain.ErrInvalid)
	}
	defer body.Close() //nolint:errcheck // read-only
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", raw, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes: %w", raw, limit, domain.ErrInvalid)
	}
	return data, nil
}

// filePath turns a file URL into a local path; on Windows the path of
// file:///C:/x is C:/x.
func filePath(u *url.URL) string {
	p := u.Path
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

// budget enforces the unpacking limits across all entries.
type budget struct {
	bytes   int64
	entries int
}

func (f Fetcher) newBudget() *budget {
	b := &budget{bytes: f.MaxUnpacked, entries: f.MaxEntries}
	if b.bytes <= 0 {
		b.bytes = DefaultMaxUnpacked
	}
	if b.entries <= 0 {
		b.entries = DefaultMaxEntries
	}
	return b
}

func (b *budget) entry() error {
	b.entries--
	if b.entries < 0 {
		return fmt.Errorf("too many entries: %w", domain.ErrInvalid)
	}
	return nil
}

// read reads r without letting the archive as a whole unpack to more than
// the budget, whatever sizes its headers claim.
func (b *budget) read(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, b.bytes+1))
	if err != nil {
		return nil, err
	}
	b.bytes -= int64(len(data))
	if b.bytes < 0 {
		return nil, fmt.Errorf("unpacks to too many bytes: %w", domain.ErrInvalid)
	}
	return data, nil
}

func (f Fetcher) unzip(data []byte) ([]entry, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", err, domain.ErrInvalid)
	}
	b := f.newBudget()
	var out []entry
	for _, zf := range zr.File {
		if err := b.entry(); err != nil {
			return nil, err
		}
		name, err := cleanName(zf.Name)
		if err != nil {
			return nil, err
		}
		mode := zf.Mode()
		if mode.IsDir() || name == "" {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, fmt.Errorf("%s: %w: %w", name, err, domain.ErrInvalid)
		}
		content, err := b.read(rc)
		rc.Close() //nolint:errcheck // read-only
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		file := skill.File{Kind: skill.KindRegular, Data: content}
		switch {
		case mode&fs.ModeSymlink != 0:
			file = skill.File{Kind: skill.KindSymlink, LinkTarget: string(content)}
		case mode&fs.ModeType != 0:
			return nil, fmt.Errorf("%s is not a regular file, directory or symlink: %w", name, domain.ErrInvalid)
		case mode.Perm()&0o111 != 0:
			file.Kind = skill.KindExecutable
		}
		out = append(out, entry{path: name, file: file})
	}
	return out, nil
}

func (f Fetcher) untar(data []byte) ([]entry, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", err, domain.ErrInvalid)
	}
	b := f.newBudget()
	tr := tar.NewReader(zr)
	var out []entry
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", err, domain.ErrInvalid)
		}
		if err := b.entry(); err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue // e.g. the commit id git archive records
		}
		name, err := cleanName(h.Name)
		if err != nil {
			return nil, err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
			content, err := b.read(tr)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			file := skill.File{Kind: skill.KindRegular, Data: content}
			if h.Mode&0o111 != 0 {
				file.Kind = skill.KindExecutable
			}
			out = append(out, entry{path: name, file: file})
		case tar.TypeSymlink:
			out = append(out, entry{path: name, file: skill.File{Kind: skill.KindSymlink, LinkTarget: h.Linkname}})
		default:
			return nil, fmt.Errorf("%s is not a regular file, directory or symlink: %w", name, domain.ErrInvalid)
		}
	}
}

// cleanName validates an entry name: relative, forward slashes, no "..".
// Directories come back with no trailing slash; the root as "".
func cleanName(name string) (string, error) {
	n := strings.TrimSuffix(strings.TrimPrefix(name, "./"), "/")
	bad := strings.Contains(n, `\`) || strings.HasPrefix(n, "/") || strings.Contains(n, ":") || strings.ContainsRune(n, 0)
	for _, part := range strings.Split(n, "/") {
		bad = bad || part == ".." || (part == "" && n != "") || part == "."
	}
	if bad {
		return "", fmt.Errorf("unsafe path %q in the archive: %w", name, domain.ErrInvalid)
	}
	return n, nil
}

// stripTopDir removes a directory that wraps every entry, as in the
// archives GitHub and GitLab generate (repo-ref/...).
func stripTopDir(entries []entry) []entry {
	if len(entries) == 0 {
		return entries
	}
	top, _, ok := strings.Cut(entries[0].path, "/")
	if !ok {
		return entries
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.path, top+"/") {
			return entries
		}
	}
	out := make([]entry, len(entries))
	for i, e := range entries {
		e.path = strings.TrimPrefix(e.path, top+"/")
		out[i] = e
	}
	return out
}

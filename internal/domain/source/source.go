// Package source parses what the user types as the origin of a skill:
// a local directory, a Git repository with an optional ref, or a ZIP or
// tar.gz archive.
package source

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/colybri/skilus/internal/domain"
)

// Kind says how a source is fetched.
type Kind string

// Kinds.
const (
	KindLocal   Kind = "local"
	KindGit     Kind = "git"
	KindArchive Kind = "archive"
)

// archiveExts are the file endings of the archives skilus can read.
var archiveExts = []string{".zip", ".tar.gz", ".tgz"}

// Source is a parsed origin.
type Source struct {
	Kind Kind
	// Raw is the text the user typed, without the ref.
	Raw string
	// URL is what git or the archive reader fetches; empty for local
	// sources.
	URL string
	// ID is the normalized name recorded in the lock: the path for local
	// sources, host/path for HTTPS remotes (github.com/owner/repo) and the
	// URL without the ref for the rest.
	ID string
	// Ref is the branch, tag or commit after "@"; empty means the default
	// branch. Local sources have none.
	Ref string
}

var (
	shorthandRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)
	scpRe       = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:`)
	driveRe     = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	refRe       = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._/-]*$`)
)

// Parse classifies raw. Local directories must start with ".", "/", "~"
// or a drive letter, so "owner/repo" is never mistaken for a folder.
//
//	./skills, ../x, /abs/path, C:\x      local directory
//	owner/repo[@ref]                     github.com/owner/repo
//	host.tld/owner/repo[@ref]            https://host.tld/owner/repo
//	https://…, ssh://…, file://…[@ref]   that URL
//	https://….zip, file://….tar.gz      an archive (also .tgz); no ref
//	user@host:owner/repo[@ref]           SSH, scp syntax
func Parse(raw string) (Source, error) {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return Source{}, fmt.Errorf("empty source: %w", domain.ErrInvalid)
	case isLocal(raw):
		return Source{Kind: KindLocal, Raw: raw, ID: raw}, nil
	}
	if src, ok, err := parseArchive(raw); ok || err != nil {
		return src, err
	}

	// Split "<prefix><path>[@ref]" so the "@" of "git@host:" or of URL
	// credentials is not taken for the ref separator.
	prefix, rest := splitPrefix(raw)
	path, ref, hasRef := strings.Cut(rest, "@")
	if hasRef && (ref == "" || !refRe.MatchString(ref) || strings.Contains(ref, "..")) {
		return Source{}, fmt.Errorf("invalid ref %q in %s: %w", ref, raw, domain.ErrInvalid)
	}
	base := prefix + path
	src := Source{Kind: KindGit, Raw: base, Ref: ref}

	switch {
	case strings.Contains(base, "://"):
		u, err := url.Parse(base)
		if err != nil {
			return Source{}, fmt.Errorf("invalid URL %s: %w", base, domain.ErrInvalid)
		}
		if _, hasPassword := u.User.Password(); hasPassword {
			return Source{}, fmt.Errorf("the URL contains a password; skilus uses Git's own credentials and never stores tokens: %w", domain.ErrInvalid)
		}
		switch u.Scheme {
		case "https":
			src.URL, src.ID = base, strings.ToLower(u.Host)+strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), ".git")
		case "ssh", "file":
			src.URL, src.ID = base, base
		default:
			return Source{}, fmt.Errorf("scheme %s is not supported; use https, ssh or file: %w", u.Scheme, domain.ErrInvalid)
		}
	case scpRe.MatchString(base):
		src.URL, src.ID = base, base
	case shorthandRe.MatchString(base):
		src.URL, src.ID = "https://github.com/"+base, "github.com/"+base
	case strings.Contains(strings.SplitN(base, "/", 2)[0], ".") && strings.Count(base, "/") >= 2:
		host, p, _ := strings.Cut(base, "/")
		src.URL = "https://" + base
		src.ID = strings.ToLower(host) + "/" + strings.TrimSuffix(p, ".git")
	default:
		return Source{}, fmt.Errorf("source %q is neither a directory (start it with ./) nor a Git repository: %w", raw, domain.ErrInvalid)
	}
	return src, nil
}

// parseArchive recognizes https and file URLs of archives. Their content
// is pinned by its hash in the lock, so they take no ref; a query string
// is refused because it often carries a token, which would end up there.
func parseArchive(raw string) (Source, bool, error) {
	if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "file://") {
		return Source{}, false, nil
	}
	u, err := url.Parse(raw)
	if err != nil || !isArchive(u.Path) {
		return Source{}, false, nil
	}
	switch {
	case u.User != nil:
		return Source{}, true, fmt.Errorf("the URL contains credentials; skilus never stores tokens: %w", domain.ErrInvalid)
	case u.RawQuery != "" || u.Fragment != "":
		return Source{}, true, fmt.Errorf("archive URLs cannot have a query or fragment, which would be recorded in the lock: %w", domain.ErrInvalid)
	}
	return Source{Kind: KindArchive, Raw: raw, URL: raw, ID: raw}, true, nil
}

func isArchive(p string) bool {
	p = strings.ToLower(p)
	for _, ext := range archiveExts {
		if strings.HasSuffix(p, ext) {
			return true
		}
	}
	return false
}

// String renders the source as the user would type it again.
func (s Source) String() string {
	if s.Ref == "" {
		return s.Raw
	}
	return s.Raw + "@" + s.Ref
}

func isLocal(s string) bool {
	return s == "." || s == ".." ||
		strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, `.\`) || strings.HasPrefix(s, `..\`) ||
		strings.HasPrefix(s, "/") || strings.HasPrefix(s, `\`) ||
		strings.HasPrefix(s, "~") || driveRe.MatchString(s)
}

// splitPrefix separates the part of a remote that may contain "@" (scheme,
// credentials and host, or the user@host: of scp syntax) from the path.
func splitPrefix(s string) (prefix, rest string) {
	if i := strings.Index(s, "://"); i >= 0 {
		after := s[i+3:]
		if j := strings.Index(after, "/"); j >= 0 {
			return s[:i+3+j], after[j:]
		}
		return s, ""
	}
	if loc := scpRe.FindStringIndex(s); loc != nil {
		return s[:loc[1]], s[loc[1]:]
	}
	return "", s
}

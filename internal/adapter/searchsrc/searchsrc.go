// Package searchsrc implements the origins of skilus search: the skills.sh
// registry and curated index files.
package searchsrc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

var (
	_ app.Registry    = SkillsSH{}
	_ app.IndexReader = Indexes{}
)

// maxBody bounds every response and index file.
const maxBody = 5 << 20

// SkillsSH searches https://skills.sh, the directory behind `npx skills`.
type SkillsSH struct {
	BaseURL string       // empty means https://skills.sh
	Client  *http.Client // nil means a client with a 15 s timeout
}

// Name implements app.Registry.
func (SkillsSH) Name() string { return "skills.sh" }

// Search implements app.Registry.
func (r SkillsSH) Search(ctx context.Context, query, owner string, limit int) ([]app.Listing, error) {
	base := r.BaseURL
	if base == "" {
		base = "https://skills.sh"
	}
	params := url.Values{"q": {query}, "limit": {strconv.Itoa(limit)}}
	if owner != "" {
		params.Set("owner", owner)
	}
	data, err := get(ctx, r.Client, base+"/api/search?"+params.Encode(), true)
	if err != nil {
		return nil, err
	}
	var body struct {
		Skills []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Installs int    `json:"installs"`
			Source   string `json:"source"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, domain.Errorf("skills.sh ha respondido algo que no es un resultado de búsqueda: %w", err)
	}
	var out []app.Listing
	for _, s := range body.Skills {
		// Results skilus could not install are left out.
		if _, err := skill.NewName(s.Name); err != nil {
			continue
		}
		if src, err := source.Parse(s.Source); err != nil || src.Kind == source.KindLocal {
			continue
		}
		out = append(out, app.Listing{Name: s.Name, Source: s.Source, Installs: s.Installs})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// Indexes reads curated index files over https or from file:// URLs:
//
//	version: 1
//	skills:
//	  - name: pdf
//	    source: anthropics/skills
//	    description: Lee y crea PDF
type Indexes struct {
	Client *http.Client // nil means a client with a 15 s timeout
}

// Index implements app.IndexReader.
func (x Indexes) Index(ctx context.Context, raw string) ([]app.Listing, error) {
	data, err := get(ctx, x.Client, raw, false)
	if err != nil {
		return nil, err
	}
	var f struct {
		Version int `yaml:"version"`
		Skills  []struct {
			Name        string `yaml:"name"`
			Source      string `yaml:"source"`
			Description string `yaml:"description"`
		} `yaml:"skills"`
	}
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, domain.Errorf("%s: %w: %w", raw, err, domain.ErrInvalid)
	}
	if f.Version != 1 {
		return nil, domain.Errorf("%s: la versión %d del índice no está soportada: %w", raw, f.Version, domain.ErrInvalid)
	}
	out := make([]app.Listing, 0, len(f.Skills))
	for i, s := range f.Skills {
		if _, err := skill.NewName(s.Name); err != nil {
			return nil, domain.Errorf("%s: entrada %d: %w", raw, i+1, err)
		}
		src, err := source.Parse(s.Source)
		if err != nil {
			return nil, domain.Errorf("%s: skill %s: %w", raw, s.Name, err)
		}
		if src.Kind == source.KindLocal {
			// A path in a remote file would point into the reader's disk.
			return nil, domain.Errorf("%s: la skill %s tiene un origen local: %w", raw, s.Name, domain.ErrInvalid)
		}
		out = append(out, app.Listing{Name: s.Name, Source: s.Source, Description: s.Description})
	}
	return out, nil
}

// get downloads raw over https, or reads it from a file:// URL unless
// httpsOnly, with the response bounded to maxBody.
func get(ctx context.Context, client *http.Client, raw string, httpsOnly bool) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, domain.Errorf("%s: %w", raw, domain.ErrInvalid)
	}
	var body io.ReadCloser
	switch {
	case u.Scheme == "file" && !httpsOnly:
		file, err := os.Open(filePath(u))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, domain.Errorf("%s no existe: %w", raw, domain.ErrNotFound)
		}
		if err != nil {
			return nil, err
		}
		body = file
	case u.Scheme == "https":
		if u.User != nil {
			return nil, domain.Errorf("%s: no se aceptan URLs con credenciales: %w", u.Redacted(), domain.ErrInvalid)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "skilus")
		if client == nil {
			client = &http.Client{Timeout: 15 * time.Second}
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.Request.URL.Scheme != "https" {
			resp.Body.Close() //nolint:errcheck // nothing to read
			return nil, domain.Errorf("%s redirige a una URL que no es https: %w", raw, domain.ErrInvalid)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close() //nolint:errcheck // nothing to read
			if resp.StatusCode == http.StatusNotFound {
				return nil, domain.Errorf("%s: %s: %w", raw, resp.Status, domain.ErrNotFound)
			}
			return nil, domain.Errorf("%s: %s", raw, resp.Status)
		}
		body = resp.Body
	default:
		return nil, domain.Errorf("%s: solo se aceptan URLs https: %w", raw, domain.ErrInvalid)
	}
	defer body.Close() //nolint:errcheck // read-only
	data, err := io.ReadAll(io.LimitReader(body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBody {
		return nil, domain.Errorf("%s ocupa más de %d bytes: %w", raw, maxBody, domain.ErrInvalid)
	}
	return data, nil
}

func filePath(u *url.URL) string {
	p := u.Path
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

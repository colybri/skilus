package yamlrepo

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
)

var _ app.ManifestFile = Repo{}

// manifestKeys are the top-level keys skilus.yaml may have. Any other is
// most likely a typo, which would otherwise be ignored without a word.
var manifestKeys = map[string]bool{"version": true, "skills": true, "profiles": true, "trust": true, "indexes": true}

// ManifestPath implements app.ManifestFile.
func (r Repo) ManifestPath(scope agent.Scope) string { return r.path(scope, ManifestFile) }

// ReadManifest implements app.ManifestFile.
func (r Repo) ReadManifest(_ context.Context, scope agent.Scope) ([]byte, bool, error) {
	data, err := os.ReadFile(r.path(scope, ManifestFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// WriteManifest implements app.ManifestFile. The text is written as the
// user saved it, comments and line endings included.
func (r Repo) WriteManifest(_ context.Context, scope agent.Scope, data []byte) error {
	return writeAtomic(r.path(scope, ManifestFile), data)
}

// ParseManifest implements app.ManifestFile. It is stricter than reading
// the file for a command: the root must be a mapping, every key must be
// known and version must be 1.
func (r Repo) ParseManifest(_ context.Context, scope agent.Scope, data []byte) (app.Manifest, error) {
	p := r.path(scope, ManifestFile)
	doc, err := decodeNode(p, data)
	if err != nil {
		return app.Manifest{}, err
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return app.Manifest{}, domain.Errorf("%s: el fichero debe ser un mapa de claves como skills: y profiles: (línea %d): %w", p, root.Line, domain.ErrInvalid)
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		if !manifestKeys[key.Value] {
			return app.Manifest{}, domain.Errorf("%s: clave desconocida %q en la línea %d; las válidas son version, skills, profiles, trust e indexes: %w", p, key.Value, key.Line, domain.ErrInvalid)
		}
	}
	if v := mappingValue(root, "version"); v != nil && v.Value != "1" {
		return app.Manifest{}, domain.Errorf("%s: version debe ser 1, no %q: %w", p, v.Value, domain.ErrInvalid)
	}
	if _, err := parseTrust(p, doc); err != nil {
		return app.Manifest{}, err
	}
	return parseManifest(p, doc)
}

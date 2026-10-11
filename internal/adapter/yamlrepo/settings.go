package yamlrepo

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/colybri/skilus/internal/domain"
)

// SettingsFile holds the user's preferences in GlobalDir.
const SettingsFile = "config.yaml"

// Language implements app.SettingsRepository.
func (r Repo) Language(_ context.Context) (string, error) {
	doc, err := r.settings()
	if err != nil {
		return "", err
	}
	s, _ := doc["language"].(string)
	return s, nil
}

// SetLanguage implements app.SettingsRepository. Other keys are kept.
func (r Repo) SetLanguage(_ context.Context, code string) error {
	doc, err := r.settings()
	if err != nil {
		return err
	}
	if code == "" {
		delete(doc, "language")
	} else {
		doc["language"] = code
	}
	data, err := marshal(doc)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(r.GlobalDir, SettingsFile), data)
}

func (r Repo) settings() (map[string]any, error) {
	path := filepath.Join(r.GlobalDir, SettingsFile)
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	doc := map[string]any{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, domain.Errorf("%s: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

package app

import (
	"bytes"
	"context"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
)

// ManifestFile reads and writes a scope's skilus.yaml as text, so the user
// can edit it by hand.
type ManifestFile interface {
	// ManifestPath returns where the scope's skilus.yaml lives.
	ManifestPath(scope agent.Scope) string
	// ReadManifest returns the file's contents; exists is false when there
	// is no file yet.
	ReadManifest(ctx context.Context, scope agent.Scope) (data []byte, exists bool, err error)
	WriteManifest(ctx context.Context, scope agent.Scope, data []byte) error
	// ParseManifest validates data as the scope's skilus.yaml.
	ParseManifest(ctx context.Context, scope agent.Scope, data []byte) (Manifest, error)
}

// Editor opens a text in the user's editor and returns it as saved.
type Editor interface {
	// Edit shows content under the file name name, such as skilus.yaml, so
	// the editor can highlight it, and waits until the editor is closed.
	Edit(ctx context.Context, name string, content []byte) ([]byte, error)
}

// EditManifest is the command behind `skilus edit`.
type EditManifest struct {
	Scope agent.Scope
	// Template is what a new skilus.yaml starts with.
	Template []byte
	// Retry is asked, with the problems found, whether to reopen the
	// editor; false discards the changes.
	Retry func(ctx context.Context, problems error) (bool, error)
}

// EditOutcome says what happened to skilus.yaml.
type EditOutcome string

// Edit outcomes.
const (
	EditSaved     EditOutcome = "saved"
	EditUnchanged EditOutcome = "unchanged"
)

// EditResult is what `skilus edit` did.
type EditResult struct {
	Path    string
	Outcome EditOutcome
	Created bool
}

// ErrEditDiscarded means the user gave up on a skilus.yaml with errors:
// the file was left as it was.
var ErrEditDiscarded = domain.Errorf("cambios descartados; skilus.yaml no se ha modificado: %w", domain.ErrInvalid)

// EditManifestHandler lets the user edit skilus.yaml in their editor and
// only saves it when it is valid, so a typo never leaves a broken file.
type EditManifestHandler struct {
	Files  ManifestFile
	Editor Editor
}

// Edit opens the scope's skilus.yaml, or the template when there is none,
// and saves what the user wrote once it validates.
func (h EditManifestHandler) Edit(ctx context.Context, c EditManifest) (EditResult, error) {
	res := EditResult{Path: h.Files.ManifestPath(c.Scope), Outcome: EditUnchanged}
	original, exists, err := h.Files.ReadManifest(ctx, c.Scope)
	if err != nil {
		return res, err
	}
	content := original
	if !exists {
		content = c.Template
	}
	for {
		edited, err := h.Editor.Edit(ctx, "skilus.yaml", content)
		if err != nil {
			return res, err
		}
		// Saving the original or the untouched template changes nothing.
		if bytes.Equal(edited, original) || (!exists && bytes.Equal(edited, c.Template)) {
			return res, nil
		}
		problems := h.validate(ctx, c.Scope, edited)
		if problems == nil {
			if err := h.Files.WriteManifest(ctx, c.Scope, edited); err != nil {
				return res, err
			}
			res.Outcome, res.Created = EditSaved, !exists
			return res, nil
		}
		again, err := c.Retry(ctx, problems)
		if err != nil {
			return res, err
		}
		if !again {
			return res, ErrEditDiscarded
		}
		content = edited
	}
}

// validate returns every problem in data: a file that does not parse, or
// profiles that use skills skills: does not declare.
func (h EditManifestHandler) validate(ctx context.Context, scope agent.Scope, data []byte) error {
	m, err := h.Files.ParseManifest(ctx, scope, data)
	if err != nil {
		return err
	}
	var problems domain.Errors
	for _, p := range m.Profiles {
		for _, n := range undeclared(m, p) {
			problems = append(problems, domain.Errorf("el perfil %s usa %s, que no está en skills: %w", p.Name(), n, domain.ErrInvalid))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return problems
}

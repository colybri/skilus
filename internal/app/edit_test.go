package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/profile"
	"github.com/colybri/skilus/internal/domain/skill"
)

// memManifestFile holds skilus.yaml as text. Its parser knows a few fixed
// contents: "broken" does not parse and "orphan" has a profile using a
// skill skills: does not declare; anything else is an empty manifest.
type memManifestFile struct {
	data   []byte
	exists bool
	writes int
}

func (f *memManifestFile) ManifestPath(scope agent.Scope) string {
	return string(scope) + "/skilus.yaml"
}

func (f *memManifestFile) ReadManifest(context.Context, agent.Scope) ([]byte, bool, error) {
	return f.data, f.exists, nil
}

func (f *memManifestFile) WriteManifest(_ context.Context, _ agent.Scope, data []byte) error {
	f.data, f.exists = data, true
	f.writes++
	return nil
}

func (f *memManifestFile) ParseManifest(_ context.Context, _ agent.Scope, data []byte) (app.Manifest, error) {
	switch string(data) {
	case "broken":
		return app.Manifest{}, domain.Errorf("línea 1: %w", domain.ErrInvalid)
	case "orphan":
		n, _ := skill.NewName("pdf")
		p, _ := profile.New("web", []skill.Name{n}, nil)
		return app.Manifest{Profiles: []profile.Profile{p}}, nil
	}
	return app.Manifest{}, nil
}

// scriptedEditor saves each of its outputs in turn and records what it was
// shown.
type scriptedEditor struct {
	outputs []string
	shown   []string
}

func (e *scriptedEditor) Edit(_ context.Context, name string, content []byte) ([]byte, error) {
	if name != "skilus.yaml" {
		return nil, errors.New("unexpected file name " + name)
	}
	e.shown = append(e.shown, string(content))
	out := e.outputs[0]
	e.outputs = e.outputs[1:]
	return []byte(out), nil
}

func retry(answers ...bool) (func(context.Context, error) (bool, error), *[]error) {
	var seen []error
	return func(_ context.Context, problems error) (bool, error) {
		seen = append(seen, problems)
		a := answers[0]
		answers = answers[1:]
		return a, nil
	}, &seen
}

func TestEditCreatesFromTemplate(t *testing.T) {
	files := &memManifestFile{}
	ed := &scriptedEditor{outputs: []string{"version: 1\n"}}
	h := app.EditManifestHandler{Files: files, Editor: ed}
	res, err := h.Edit(context.Background(), app.EditManifest{Scope: agent.ScopeGlobal, Template: []byte("# template\n")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != app.EditSaved || !res.Created || res.Path != "global/skilus.yaml" {
		t.Errorf("result = %+v", res)
	}
	if ed.shown[0] != "# template\n" || string(files.data) != "version: 1\n" {
		t.Errorf("shown %q, saved %q", ed.shown[0], files.data)
	}
}

func TestEditLeavesUntouchedFilesAlone(t *testing.T) {
	for name, files := range map[string]*memManifestFile{
		"existing": {data: []byte("version: 1\n"), exists: true},
		"template": {},
	} {
		saved := "version: 1\n"
		if !files.exists {
			saved = "# template\n"
		}
		h := app.EditManifestHandler{Files: files, Editor: &scriptedEditor{outputs: []string{saved}}}
		res, err := h.Edit(context.Background(), app.EditManifest{Scope: agent.ScopeProject, Template: []byte("# template\n")})
		if err != nil || res.Outcome != app.EditUnchanged || files.writes != 0 {
			t.Errorf("%s: result = %+v, %v; writes = %d", name, res, err, files.writes)
		}
	}
}

func TestEditReopensUntilValid(t *testing.T) {
	files := &memManifestFile{data: []byte("version: 1\n"), exists: true}
	ed := &scriptedEditor{outputs: []string{"broken", "orphan", "fixed"}}
	again, seen := retry(true, true)
	h := app.EditManifestHandler{Files: files, Editor: ed}
	res, err := h.Edit(context.Background(), app.EditManifest{Scope: agent.ScopeProject, Retry: again})
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != app.EditSaved || res.Created || string(files.data) != "fixed" {
		t.Errorf("result = %+v, saved %q", res, files.data)
	}
	// The editor reopens with what the user wrote, not the original.
	if len(ed.shown) != 3 || ed.shown[1] != "broken" || ed.shown[2] != "orphan" {
		t.Errorf("shown = %q", ed.shown)
	}
	if len(*seen) != 2 || !errors.Is((*seen)[0], domain.ErrInvalid) || !errors.Is((*seen)[1], domain.ErrInvalid) {
		t.Errorf("problems = %v", *seen)
	}
	var list domain.Errors
	if !errors.As((*seen)[1], &list) || len(list) != 1 {
		t.Errorf("an undeclared skill should be listed: %v", (*seen)[1])
	}
}

func TestEditDiscardsWhenTheUserGivesUp(t *testing.T) {
	files := &memManifestFile{data: []byte("version: 1\n"), exists: true}
	again, _ := retry(false)
	h := app.EditManifestHandler{Files: files, Editor: &scriptedEditor{outputs: []string{"broken"}}}
	_, err := h.Edit(context.Background(), app.EditManifest{Scope: agent.ScopeProject, Retry: again})
	if !errors.Is(err, app.ErrEditDiscarded) || !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("err = %v, want ErrEditDiscarded", err)
	}
	if files.writes != 0 || string(files.data) != "version: 1\n" {
		t.Errorf("the file changed: %q", files.data)
	}
}

package editor_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/colybri/skilus/internal/adapter/editor"
	"github.com/colybri/skilus/internal/domain"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func found(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		if slices.Contains(names, name) {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
}

func TestCommandOrder(t *testing.T) {
	ctx := context.Background()
	git := func(context.Context) string { return "emacs\n" }
	tests := []struct {
		name string
		e    editor.Editor
		want string
	}{
		{"SKILUS_EDITOR first", editor.Editor{Getenv: env(map[string]string{"SKILUS_EDITOR": "micro", "VISUAL": "code --wait", "EDITOR": "vim"}), GitEditor: git}, "micro"},
		{"VISUAL before EDITOR", editor.Editor{Getenv: env(map[string]string{"VISUAL": "code --wait", "EDITOR": "vim"}), GitEditor: git}, "code --wait"},
		{"EDITOR", editor.Editor{Getenv: env(map[string]string{"EDITOR": " vim "}), GitEditor: git}, "vim"},
		{"git core.editor", editor.Editor{Getenv: env(nil), GitEditor: git}, "emacs"},
		{"nano", editor.Editor{Getenv: env(nil), LookPath: found("nano", "vi")}, "nano"},
		{"vi", editor.Editor{Getenv: env(nil), LookPath: found("vi")}, "vi"},
		{"notepad", editor.Editor{Getenv: env(nil), LookPath: found("notepad", "nano"), Windows: true}, "notepad"},
	}
	for _, tt := range tests {
		if got, err := tt.e.Command(ctx); err != nil || got != tt.want {
			t.Errorf("%s: Command = %q, %v; want %q", tt.name, got, err, tt.want)
		}
	}
	none := editor.Editor{Getenv: env(nil), LookPath: found(), GitEditor: func(context.Context) string { return "" }}
	if _, err := none.Command(ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("no editor: err = %v, want ErrNotFound", err)
	}
}

func TestSplit(t *testing.T) {
	for in, want := range map[string][]string{
		`notepad`:        {"notepad"},
		` code  --wait `: {"code", "--wait"},
		`"C:\Program Files\Notepad++\notepad++.exe" -multiInst -nosession`: {`C:\Program Files\Notepad++\notepad++.exe`, "-multiInst", "-nosession"},
		`""`: {""},
		``:   nil,
	} {
		if got := editor.Split(in); !slices.Equal(got, want) {
			t.Errorf("Split(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEditRunsTheEditor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the e2e scripts cover Windows with a fake editor")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "my editor.sh")
	// The editor gets the file name as its last argument after its own.
	if err := os.WriteFile(script, []byte("#!/bin/sh\n[ \"$1\" = --flag ] || exit 3\ncase \"$2\" in */skilus.yaml) ;; *) exit 4;; esac\nprintf 'version: 1\\n' >> \"$2\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	e := editor.Editor{Getenv: env(map[string]string{"EDITOR": `"` + script + `" --flag`}), Stderr: &stderr, Stdout: &stderr}
	got, err := e.Edit(context.Background(), "skilus.yaml", []byte("# hi\n"))
	if err != nil {
		t.Fatalf("Edit: %v (%s)", err, stderr.String())
	}
	if string(got) != "# hi\nversion: 1\n" {
		t.Errorf("Edit = %q", got)
	}

	failing := editor.Editor{Getenv: env(map[string]string{"EDITOR": "false"})}
	if _, err := failing.Edit(context.Background(), "skilus.yaml", nil); err == nil {
		t.Error("a failing editor should be an error")
	}
}

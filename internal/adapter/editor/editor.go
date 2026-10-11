// Package editor opens texts in the user's own text editor.
package editor

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
)

var _ app.Editor = Editor{}

// Editor runs the editor the user chose, in the terminal skilus runs in.
type Editor struct {
	Getenv   func(string) string
	LookPath func(string) (string, error)
	// GitEditor returns git's core.editor, or "" when it is not set.
	GitEditor func(ctx context.Context) string
	Windows   bool
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

// New returns an Editor for this machine.
func New(windows bool) Editor {
	return Editor{
		Getenv:    os.Getenv,
		LookPath:  exec.LookPath,
		GitEditor: gitEditor,
		Windows:   windows,
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
	}
}

// Command returns the editor to run: SKILUS_EDITOR, VISUAL, EDITOR, git's
// core.editor, and otherwise notepad on Windows and nano or vi elsewhere.
func (e Editor) Command(ctx context.Context) (string, error) {
	for _, name := range []string{"SKILUS_EDITOR", "VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(e.Getenv(name)); v != "" {
			return v, nil
		}
	}
	if e.GitEditor != nil {
		if v := strings.TrimSpace(e.GitEditor(ctx)); v != "" {
			return v, nil
		}
	}
	fallbacks := []string{"nano", "vi"}
	if e.Windows {
		fallbacks = []string{"notepad"}
	}
	for _, name := range fallbacks {
		if _, err := e.LookPath(name); err == nil {
			return name, nil
		}
	}
	return "", domain.Errorf("no se encuentra ningún editor; define la variable EDITOR, por ejemplo EDITOR=nano o EDITOR=\"code --wait\": %w", domain.ErrNotFound)
}

// Edit implements app.Editor. The text goes in a temporary file named
// name, which is removed once the editor closes.
func (e Editor) Edit(ctx context.Context, name string, content []byte) ([]byte, error) {
	command, err := e.Command(ctx)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "skilus-edit-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir) //nolint:errcheck // a leftover temporary directory is harmless
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return nil, err
	}

	cmd, err := e.command(ctx, command, path)
	if err != nil {
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = e.Stdin, e.Stdout, e.Stderr
	if err := cmd.Run(); err != nil {
		return nil, domain.Errorf("el editor %s falló: %w", command, err)
	}
	return os.ReadFile(path)
}

// command builds the process for the editor command line. Like git, it
// runs it through sh outside Windows, so EDITOR may carry arguments and
// quotes; on Windows the line is split on spaces outside double quotes.
func (e Editor) command(ctx context.Context, command, path string) (*exec.Cmd, error) {
	if !e.Windows {
		return exec.CommandContext(ctx, "sh", "-c", command+` "$@"`, command, path), nil
	}
	args := Split(command)
	if len(args) == 0 {
		return nil, domain.Errorf("el editor está vacío: %w", domain.ErrInvalid)
	}
	return exec.CommandContext(ctx, args[0], append(args[1:], path)...), nil
}

// Split breaks a Windows command line into arguments: spaces separate them
// unless inside double quotes, which are removed; backslashes are kept, as
// they are part of Windows paths.
func Split(s string) []string {
	var args []string
	var cur strings.Builder
	inQuotes, started := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuotes, started = !inQuotes, true
		case (r == ' ' || r == '\t') && !inQuotes:
			if started {
				args = append(args, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if started {
		args = append(args, cur.String())
	}
	return args
}

// gitEditor reads core.editor from git's configuration.
func gitEditor(ctx context.Context) string {
	out, err := exec.CommandContext(ctx, "git", "config", "--get", "core.editor").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

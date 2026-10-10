package cli

import (
	"fmt"
	"testing"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
)

func TestExitCode(t *testing.T) {
	tests := map[error]int{
		domain.ErrInvalid:       ExitInvalid,
		errUsage:                ExitInvalid,
		domain.ErrNotFound:      ExitNotFound,
		domain.ErrAlreadyExists: ExitConflict,
		domain.ErrConflict:      ExitConflict,
		app.ErrRejected:         ExitRejected,
		app.ErrCancelled:        ExitError,
		app.ErrDrift:            ExitDrift,
	}
	for err, want := range tests {
		if got := exitCode(fmt.Errorf("wrapped: %w", err)); got != want {
			t.Errorf("exitCode(%v) = %d, want %d", err, got, want)
		}
	}
}

func TestCleanEscapesControlCharacters(t *testing.T) {
	if got := clean("ok\x1b]0;pwned\x07 ñ"); got != `ok'\x1b']0;pwned'\a' ñ` {
		t.Fatalf("clean() = %q", got)
	}
}

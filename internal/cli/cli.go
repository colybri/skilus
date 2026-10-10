// Package cli is the driving adapter: Cobra commands that parse flags, call
// use cases from app and render their results. It is the only place that
// turns domain errors into messages and exit codes.
package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
)

// Exit codes.
const (
	ExitOK       = 0
	ExitError    = 1
	ExitInvalid  = 2
	ExitNotFound = 3
	ExitConflict = 4
	ExitRejected = 5
	ExitDrift    = 6
)

// Deps are the use cases the commands call, wired in cmd/skilus.
type Deps struct {
	Version    string
	ListAgents app.ListAgents
	// AddSkill is completed with the CLI's own Prompter.
	AddSkill    app.AddSkillHandler
	ListSkills  app.ListSkills
	RemoveSkill app.RemoveSkillHandler
	Verify      app.VerifyHandler
	Sync        app.SyncHandler
	Inspect     app.InspectHandler
	Outdated    app.OutdatedHandler
	Update      app.UpdateHandler
	// UseProfile is completed with the CLI's own Prompter.
	ListProfiles app.ListProfilesHandler
	UseProfile   app.UseProfileHandler
	Search       app.SearchHandler
	Audit        app.AuditHandler
}

// Run executes the CLI with args and returns the process exit code.
func Run(deps Deps, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRoot(deps)
	root.SetArgs(args)
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.Execute()
	if err == nil {
		return ExitOK
	}
	fmt.Fprintln(stderr, "skilus:", err)
	return exitCode(err)
}

func exitCode(err error) int {
	switch {
	case errors.Is(err, domain.ErrInvalid), errors.Is(err, errUsage):
		return ExitInvalid
	case errors.Is(err, domain.ErrNotFound):
		return ExitNotFound
	case errors.Is(err, domain.ErrAlreadyExists), errors.Is(err, domain.ErrConflict):
		return ExitConflict
	case errors.Is(err, app.ErrRejected):
		return ExitRejected
	case errors.Is(err, app.ErrDrift):
		return ExitDrift
	default:
		return ExitError
	}
}

var errUsage = errors.New("usage error")

func newRoot(deps Deps) *cobra.Command {
	root := &cobra.Command{
		Use:           "skilus",
		Short:         "Gestor de skills para agentes de IA, reproducible y verificable",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       deps.Version,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w: %w", errUsage, err)
	})
	root.AddCommand(newAgentsCommand(deps.ListAgents), newAddCommand(deps.AddSkill), newListCommand(deps.ListSkills), newRemoveCommand(deps.RemoveSkill), newVerifyCommand(deps.Verify), newSyncCommand(deps.Sync), newInspectCommand(deps.Inspect), newOutdatedCommand(deps.Outdated), newUpdateCommand(deps.Update), newProfileCommand(deps.ListProfiles, deps.UseProfile), newSearchCommand(deps.Search), newAuditCommand(deps.Audit))
	return root
}

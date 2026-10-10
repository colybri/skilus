// Package cli is the driving adapter: Cobra commands that parse flags, call
// use cases from app and render their results. It is the only place that
// turns domain errors into messages and exit codes.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
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
	Language     app.LanguageSetting
	// Getenv reads the locale variables; SystemLocale is the operating
	// system's own locale where the variables are not the norm (Windows).
	Getenv       func(string) string
	SystemLocale string
}

// Run executes the CLI with args and returns the process exit code.
func Run(deps Deps, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	choice, t := language(deps, stderr)
	root := newRoot(deps, choice, t)
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

// language picks the catalog to print with. A broken setting must not stop
// skilus, so it is reported and the system decides.
func language(deps Deps, stderr io.Writer) (i18n.Choice, *i18n.Catalog) {
	getenv := deps.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	setting, err := deps.Language.Get(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "skilus:", err)
	}
	choice := i18n.Resolve(setting, getenv, deps.SystemLocale)
	t, err := i18n.Load(choice.Code)
	if err != nil {
		fmt.Fprintln(stderr, "skilus:", err)
		choice = i18n.Choice{Code: i18n.Source, Origin: i18n.FromFallback}
		t, _ = i18n.Load(i18n.Source)
	}
	return choice, t
}

func newRoot(deps Deps, choice i18n.Choice, t *i18n.Catalog) *cobra.Command {
	root := &cobra.Command{
		Use:           "skilus",
		Short:         t.T("Gestor de skills para agentes de IA, reproducible y verificable"),
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       deps.Version,
	}
	// Cobra's completion command only speaks English; it still works.
	root.CompletionOptions.HiddenDefaultCmd = true
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return fmt.Errorf("%w: %w", errUsage, err)
	})
	root.AddCommand(newAgentsCommand(deps.ListAgents, t), newAddCommand(deps.AddSkill, t), newListCommand(deps.ListSkills, t), newRemoveCommand(deps.RemoveSkill, t), newVerifyCommand(deps.Verify, t), newSyncCommand(deps.Sync, t), newInspectCommand(deps.Inspect, t), newOutdatedCommand(deps.Outdated, t), newUpdateCommand(deps.Update, t), newProfileCommand(deps.ListProfiles, deps.UseProfile, t), newSearchCommand(deps.Search, t), newAuditCommand(deps.Audit, t), newLangCommand(deps.Language, choice, t))
	localizeCobra(root, t)
	return root
}

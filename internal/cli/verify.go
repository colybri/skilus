package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain/agent"
)

// parseScopes turns --scope into scopes; "all" means project then global.
func parseScopes(raw string) ([]agent.Scope, error) {
	if raw == "all" {
		return []agent.Scope{agent.ScopeProject, agent.ScopeGlobal}, nil
	}
	s, err := agent.ParseScope(raw)
	if err != nil {
		return nil, err
	}
	return []agent.Scope{s}, nil
}

type checkJSON struct {
	Skill   string       `json:"skill"`
	Scope   string       `json:"scope"`
	Agent   string       `json:"agent"`
	Dir     string       `json:"dir,omitempty"`
	Status  string       `json:"status"`
	Detail  string       `json:"detail,omitempty"`
	Changes []changeJSON `json:"changes,omitempty"`
}

type changeJSON struct {
	Path   string `json:"path"`
	Change string `json:"change"`
}

func newVerifyCommand(q app.VerifyHandler, t *i18n.Catalog) *cobra.Command {
	var (
		asJSON bool
		scope  string
	)
	cmd := &cobra.Command{
		Use:   "verify",
		Short: t.T("Comprueba que las skills instaladas coinciden con el lock"),
		Long: t.T(`Recalcula el hash de cada skill en cada agente y lo compara con
skilus.lock. Sale con código 6 si falta alguna o si algún fichero ha
cambiado, y dice cuál.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scopes, err := parseScopes(scope)
			if err != nil {
				return err
			}
			res, err := q.Handle(cmd.Context(), app.Verify{Scopes: scopes})
			if err != nil && !errors.Is(err, app.ErrDrift) {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				if jerr := renderChecksJSON(out, res.Checks); jerr != nil {
					return jerr
				}
				return err
			}
			renderChecks(out, res, t)
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, t.T("salida en JSON"))
	cmd.Flags().StringVar(&scope, "scope", "all", t.T("project, global o all"))
	return cmd
}

func renderChecks(w io.Writer, res app.VerifyResult, t *i18n.Catalog) {
	problems := res.Problems()
	if len(problems) == 0 {
		skills := map[string]bool{}
		for _, c := range res.Checks {
			skills[string(c.Scope)+"/"+c.Skill.String()] = true
		}
		fmt.Fprint(w, t.T("Todo en orden: %d skill(s) en %d destino(s) coinciden con el lock.\n", len(skills), len(res.Checks)))
		return
	}
	labels := map[app.Problem]string{
		app.ProblemMissing:    t.T("FALTA"),
		app.ProblemModified:   t.T("CAMBIADA"),
		app.ProblemUnreadable: t.T("ILEGIBLE"),
	}
	changes := map[string]string{"added": t.T("añadido"), "removed": t.T("borrado"), "modified": t.T("modificado")}
	for _, c := range problems {
		fmt.Fprint(w, t.T("%s %s (%s, %s) en %s\n", labels[c.Problem], c.Skill, c.Scope, c.Target.Agent, clean(c.Dir)))
		if c.Detail != "" {
			fmt.Fprintf(w, "  %s\n", clean(c.Detail))
		}
		for _, ch := range c.Changes {
			fmt.Fprintf(w, "  %s: %s\n", changes[string(ch.Kind)], clean(ch.Path))
		}
	}
	fmt.Fprintln(w, t.T("Ejecuta skilus sync para restaurarlas (con --force si las has cambiado a mano)."))
}

func renderChecksJSON(w io.Writer, checks []app.TargetCheck) error {
	rows := make([]checkJSON, 0, len(checks))
	for _, c := range checks {
		row := checkJSON{Skill: c.Skill.String(), Scope: string(c.Scope), Agent: c.Target.Agent.String(), Dir: c.Dir, Status: "ok", Detail: c.Detail}
		if c.Problem != "" {
			row.Status = string(c.Problem)
		}
		for _, ch := range c.Changes {
			row.Changes = append(row.Changes, changeJSON{Path: ch.Path, Change: string(ch.Kind)})
		}
		rows = append(rows, row)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

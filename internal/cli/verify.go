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
	Skill   string       `json:"skill,omitempty"`
	Scope   string       `json:"scope"`
	Agent   string       `json:"agent,omitempty"`
	Profile string       `json:"profile,omitempty"`
	Source  string       `json:"source,omitempty"`
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
skilus.lock, y comprueba que skilus.yaml se puede leer y coincide con el
lock: cada skill de skills: está instalada, salvo las que no pertenecen al
perfil activo; cada skill instalada, salvo las dependencias, está en
skills:; y los perfiles solo usan skills declaradas.

Sale con código 2 si skilus.yaml tiene errores y con código 6 si falta
alguna skill, si algún fichero ha cambiado o si skilus.yaml no coincide
con el lock, y dice qué.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scopes, err := parseScopes(scope)
			if err != nil {
				return err
			}
			res, err := q.Handle(cmd.Context(), app.Verify{Scopes: scopes})
			if err != nil && !errors.Is(err, app.ErrDrift) && !errors.Is(err, app.ErrInvalidManifest) {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				if jerr := renderChecksJSON(out, res); jerr != nil {
					return jerr
				}
			} else {
				renderChecks(out, res, t)
			}
			if err != nil {
				return reported{err}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, t.T("salida en JSON"))
	cmd.Flags().StringVar(&scope, "scope", "all", t.T("project, global o all"))
	return cmd
}

func renderChecks(w io.Writer, res app.VerifyResult, t *i18n.Catalog) {
	problems := res.Problems()
	if len(problems) == 0 && len(res.Manifest) == 0 {
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
	if len(problems) > 0 {
		fmt.Fprintln(w, t.T("Ejecuta skilus sync para restaurarlas (con --force si las has cambiado a mano)."))
	}
	for _, c := range res.Manifest {
		renderManifestCheck(w, c, t)
	}
}

func renderManifestCheck(w io.Writer, c app.ManifestCheck, t *i18n.Catalog) {
	switch c.Problem {
	case app.ManifestInvalid:
		fmt.Fprint(w, t.T("ERROR skilus.yaml (%s) no se puede leer:\n", c.Scope))
		fmt.Fprintf(w, "  %s\n", clean(c.Detail))
	case app.ProfileUndeclared:
		fmt.Fprint(w, t.T("ERROR el perfil %s (%s) usa %s, que no está en skills: de skilus.yaml\n", c.Profile, c.Scope, c.Skill))
		fmt.Fprint(w, t.T("  Añádela con skilus add o quítala con skilus profile remove %s %s.\n", c.Profile, c.Skill))
	case app.NotInstalled:
		fmt.Fprint(w, t.T("NO INSTALADA %s (%s) está en skills: de skilus.yaml pero no en skilus.lock\n", c.Skill, c.Scope))
		fmt.Fprint(w, t.T("  Instálala con skilus add %s --skill %s o bórrala de skills:.\n", clean(c.Source), c.Skill))
	case app.NotDeclared:
		fmt.Fprint(w, t.T("NO DECLARADA %s (%s) está en skilus.lock pero no en skills: de skilus.yaml\n", c.Skill, c.Scope))
		fmt.Fprint(w, t.T("  Retírala con skilus remove %s o añádela a skills:.\n", c.Skill))
	}
}

func renderChecksJSON(w io.Writer, res app.VerifyResult) error {
	rows := make([]checkJSON, 0, len(res.Checks)+len(res.Manifest))
	for _, c := range res.Checks {
		row := checkJSON{Skill: c.Skill.String(), Scope: string(c.Scope), Agent: c.Target.Agent.String(), Dir: c.Dir, Status: "ok", Detail: c.Detail}
		if c.Problem != "" {
			row.Status = string(c.Problem)
		}
		for _, ch := range c.Changes {
			row.Changes = append(row.Changes, changeJSON{Path: ch.Path, Change: string(ch.Kind)})
		}
		rows = append(rows, row)
	}
	for _, c := range res.Manifest {
		row := checkJSON{Scope: string(c.Scope), Profile: c.Profile, Source: c.Source, Status: string(c.Problem), Detail: c.Detail}
		if c.Problem != app.ManifestInvalid {
			row.Skill = c.Skill.String()
		}
		rows = append(rows, row)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

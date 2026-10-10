package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain/agent"
)

type outdatedJSON struct {
	Name      string `json:"name"`
	Scope     string `json:"scope"`
	Source    string `json:"source"`
	Requested string `json:"requested,omitempty"`
	Commit    string `json:"commit,omitempty"`
	Latest    string `json:"latest,omitempty"`
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
}

func newOutdatedCommand(q app.OutdatedHandler) *cobra.Command {
	var (
		asJSON bool
		scope  string
	)
	cmd := &cobra.Command{
		Use:   "outdated",
		Short: "Dice qué skills tienen un commit más nuevo en su rama o tag",
		Long: `Consulta, sin descargar contenido, a qué commit apunta ahora la rama o el
tag de cada skill y lo compara con el del lock. Las skills fijadas a un
commit y las locales no se comprueban.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scopes, err := parseScopes(scope)
			if err != nil {
				return err
			}
			res, err := q.Handle(cmd.Context(), app.Outdated{Scopes: scopes})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				rows := make([]outdatedJSON, 0, len(res))
				for _, f := range res {
					e := f.Entry
					row := outdatedJSON{Name: e.Skill.String(), Scope: string(f.Scope), Source: e.Source, Requested: e.Requested, Commit: e.Commit, Latest: f.Latest, State: string(f.State)}
					if f.Err != nil {
						row.Error = f.Err.Error()
					}
					rows = append(rows, row)
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			if len(res) == 0 {
				fmt.Fprintln(out, "No hay skills instaladas.")
				return nil
			}
			states := map[app.Freshness]string{
				app.FreshCurrent:  "al día",
				app.FreshOutdated: "hay versión nueva",
				app.FreshPinned:   "fijada a un commit",
				app.FreshLocal:    "local o archivo, sin ref",
				app.FreshUnknown:  "no se pudo consultar",
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "SKILL\tÁMBITO\tREF\tINSTALADO\tÚLTIMO\tESTADO")
			for _, f := range res {
				e := f.Entry
				ref := e.Requested
				if ref == "" {
					ref = "(rama por defecto)"
				}
				state := states[f.State]
				if f.Err != nil {
					state += ": " + f.Err.Error()
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", e.Skill, f.Scope, clean(ref), short(e.Commit), short(f.Latest), clean(state))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "salida en JSON")
	cmd.Flags().StringVar(&scope, "scope", "all", "project, global o all")
	return cmd
}

func short(commit string) string {
	if len(commit) < 12 {
		return "-"
	}
	return commit[:12]
}

func newUpdateCommand(h app.UpdateHandler) *cobra.Command {
	var (
		c     app.Update
		scope string
	)
	cmd := &cobra.Command{
		Use:   "update [skill]...",
		Short: "Actualiza skills al commit al que apunta ahora su rama o tag",
		Long: `Descarga la versión a la que apunta ahora la rama o el tag de cada skill
(o de las indicadas), muestra qué ficheros cambian y la inspección de la
versión nueva y, tras tu confirmación, reemplaza las copias instaladas y
actualiza skilus.lock. skilus.yaml no cambia.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := agent.ParseScope(scope)
			if err != nil {
				return err
			}
			c.Names, c.Scope = args, s
			out := cmd.OutOrStdout()
			h.Prompter = prompter{in: cmd.InOrStdin(), out: out}
			res, err := h.Handle(cmd.Context(), c)
			if errors.Is(err, app.ErrRejected) || (err == nil && c.Yes) {
				renderUpdatePlan(cmd.ErrOrStderr(), res.Plan)
			}
			if err != nil {
				return err
			}
			if len(res.Plan.Skills) == 0 {
				fmt.Fprintln(out, "Todo al día.")
				return nil
			}
			for _, e := range res.Installed {
				fmt.Fprintf(out, "Instalada %s (%s), que ahora se necesita.\n", e.Skill, e.TreeHash.Short())
			}
			for _, e := range res.Updated {
				fmt.Fprintf(out, "Actualizada %s a %s (%s).\n", e.Skill, short(e.Commit), e.TreeHash.Short())
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&scope, "scope", string(agent.ScopeProject), "project o global")
	f.BoolVarP(&c.Yes, "yes", "y", false, "no preguntes; los ejecutables nuevos siguen necesitando --allow-scripts")
	f.BoolVar(&c.Strict, "strict", false, "trata los avisos de la inspección como bloqueos")
	f.BoolVar(&c.AllowScripts, "allow-scripts", false, "acepta ficheros ejecutables nuevos")
	f.BoolVar(&c.Force, "force", false, "sobrescribe las skills modificadas a mano")
	return cmd
}

func renderUpdatePlan(w io.Writer, plan app.UpdatePlan) {
	changes := map[string]string{"added": "+", "removed": "-", "modified": "~"}
	for _, u := range plan.Skills {
		e := u.Previous
		from, to := short(e.Commit), short(u.Commit)
		fmt.Fprintf(w, "%s  %s -> %s  sha256 %s -> %s\n", e.Skill, from, to, e.TreeHash.Short(), u.Package.TreeHash().Short())
		switch {
		case !u.ContentChanged():
			fmt.Fprintln(w, "    sin cambios en los ficheros")
		case u.Changes == nil:
			fmt.Fprintln(w, "    la versión instalada ya no está en el almacén; no se puede mostrar qué cambia")
		default:
			for _, ch := range u.Changes {
				fmt.Fprintf(w, "    %s %s\n", changes[string(ch.Kind)], clean(ch.Path))
			}
		}
		renderReport(w, u.Report)
	}
	for _, ip := range plan.Dependencies {
		fmt.Fprintln(w, "Requeridas por las skills actualizadas:")
		renderPlan(w, ip)
	}
}

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain/agent"
)

type outdatedJSON struct {
	Name      string `json:"name"`
	Scope     string `json:"scope"`
	Source    string `json:"source"`
	Requested string `json:"requested,omitempty"`
	Commit    string `json:"commit,omitempty"`
	Latest    string `json:"latest,omitempty"`
	Declared  string `json:"declared,omitempty"`
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
}

func newOutdatedCommand(q app.OutdatedHandler, t *i18n.Catalog) *cobra.Command {
	var (
		asJSON bool
		scope  string
	)
	cmd := &cobra.Command{
		Use:   "outdated",
		Short: t.T("Dice qué skills tienen un commit más nuevo en su rama o tag"),
		Long: t.T(`Consulta, sin descargar contenido, a qué commit apunta ahora la rama o el
tag de cada skill y lo compara con el del lock. Las skills fijadas a un
commit y las locales no se comprueban.

Las skills cuyo origen o ref has cambiado a mano en skilus.yaml salen
como cambiadas; skilus update aplica el cambio.`),
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
					row := outdatedJSON{Name: e.Skill.String(), Scope: string(f.Scope), Source: e.Source, Requested: e.Requested, Commit: e.Commit, Latest: f.Latest, Declared: f.Declared, State: string(f.State)}
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
				fmt.Fprintln(out, t.T("No hay skills instaladas."))
				return nil
			}
			states := map[app.Freshness]string{
				app.FreshCurrent:  t.T("al día"),
				app.FreshOutdated: t.T("hay versión nueva"),
				app.FreshPinned:   t.T("fijada a un commit"),
				app.FreshLocal:    t.T("local o archivo, sin ref"),
				app.FreshUnknown:  t.T("no se pudo consultar"),
				app.FreshChanged:  t.T("cambiada en skilus.yaml"),
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, t.T("SKILL\tÁMBITO\tREF\tINSTALADO\tÚLTIMO\tESTADO"))
			for _, f := range res {
				e := f.Entry
				ref := e.Requested
				if ref == "" {
					ref = t.T("(rama por defecto)")
				}
				state := states[f.State]
				if f.State == app.FreshChanged {
					state += ": " + f.Declared
				}
				if f.Err != nil {
					state += ": " + f.Err.Error()
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", e.Skill, f.Scope, clean(ref), short(e.Commit), short(f.Latest), clean(state))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, t.T("salida en JSON"))
	cmd.Flags().StringVar(&scope, "scope", "all", t.T("project, global o all"))
	return cmd
}

func short(commit string) string {
	if len(commit) < 12 {
		return "-"
	}
	return commit[:12]
}

func newUpdateCommand(h app.UpdateHandler, t *i18n.Catalog) *cobra.Command {
	var (
		c     app.Update
		scope string
	)
	cmd := &cobra.Command{
		Use:   "update [skill]...",
		Short: t.T("Actualiza skills al commit al que apunta ahora su rama o tag"),
		Long: t.T(`Descarga la versión a la que apunta ahora la rama o el tag de cada skill
(o de las indicadas), muestra qué ficheros cambian y la inspección de la
versión nueva y, tras tu confirmación, reemplaza las copias instaladas y
actualiza skilus.lock. skilus.yaml no cambia.

Si has editado a mano el origen o la ref de una skill en skilus.yaml,
update la descarga de ahí y registra el cambio en skilus.lock.`),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := agent.ParseScope(scope)
			if err != nil {
				return err
			}
			c.Names, c.Scope = args, s
			out := cmd.OutOrStdout()
			h.Prompter = prompter{in: cmd.InOrStdin(), out: out, t: t, st: styleFor(cmd, out)}
			res, err := h.Handle(cmd.Context(), c)
			if errors.Is(err, app.ErrRejected) || (err == nil && c.Yes) {
				renderUpdatePlan(cmd.ErrOrStderr(), res.Plan, styleFor(cmd, cmd.ErrOrStderr()), t)
			}
			if err != nil {
				return err
			}
			if len(res.Plan.Skills) == 0 {
				fmt.Fprintln(out, t.T("Todo al día."))
				return nil
			}
			for _, e := range res.Installed {
				fmt.Fprint(out, t.T("Instalada %s (%s), que ahora se necesita.\n", e.Skill, e.TreeHash.Short()))
			}
			for _, e := range res.Updated {
				fmt.Fprint(out, t.T("Actualizada %s a %s (%s).\n", e.Skill, short(e.Commit), e.TreeHash.Short()))
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&scope, "scope", string(agent.ScopeProject), t.T("project o global"))
	f.BoolVarP(&c.Yes, "yes", "y", false, t.T("no preguntes; los ejecutables nuevos siguen necesitando --allow-scripts"))
	f.BoolVar(&c.Strict, "strict", false, t.T("trata los avisos de la inspección como bloqueos"))
	f.BoolVar(&c.AllowScripts, "allow-scripts", false, t.T("acepta ficheros ejecutables nuevos"))
	f.BoolVar(&c.Force, "force", false, t.T("sobrescribe las skills modificadas a mano"))
	fullFlag(cmd, t)
	return cmd
}

// sourceLabel writes a locked source as skilus.yaml would, with a short
// path for local directories.
func sourceLabel(source, requested string) string {
	if requested == "" {
		return shortPath(source)
	}
	return source + "@" + requested
}

func renderUpdatePlan(w io.Writer, plan app.UpdatePlan, st style, t *i18n.Catalog) {
	changes := map[string]string{"added": st.green("+"), "removed": st.red("-"), "modified": st.yellow("~")}
	var n tally
	for _, u := range plan.Skills {
		e := u.Previous
		info := short(e.Commit) + " → " + short(u.Commit) + sep + "sha256 " + e.TreeHash.Short() + " → " + u.Package.TreeHash().Short()
		fmt.Fprintln(w, markSkill+" "+st.bold(e.Skill.String())+"   "+st.dim(info))
		if u.Moved() {
			from, to := sourceLabel(e.Source, e.Requested), sourceLabel(u.Source, u.Requested)
			st.line(w, 2, t.T("origen cambiado en skilus.yaml: %s → %s", clean(from), clean(to)), 0)
		}
		switch {
		case !u.ContentChanged():
			st.line(w, 2, t.T("sin cambios en los ficheros"), 0)
		case u.Changes == nil:
			st.line(w, 2, t.T("la versión instalada ya no está en el almacén; no se puede mostrar qué cambia"), 0)
		default:
			for _, ch := range u.Changes {
				st.item(w, 2, changes[string(ch.Kind)], clean(ch.Path))
			}
		}
		renderReport(w, u.Report, st, t)
		n.add(u.Report)
		fmt.Fprintln(w)
	}
	for _, ip := range plan.Dependencies {
		fmt.Fprintln(w, t.T("Requeridas por las skills actualizadas:"))
		renderSources(w, ip, st, t, &n)
	}
	renderTargets(w, st, t, plan.Dependencies...)
	n.render(w, st, t)
}

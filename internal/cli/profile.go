package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain/agent"
)

func newProfileCommand(list app.ListProfilesHandler, use app.UseProfileHandler, t *i18n.Catalog) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: t.T("Cambia entre los perfiles de skills declarados en skilus.yaml"),
		Long: t.T(`Un perfil es un conjunto de skills de skilus.yaml y, opcionalmente, los
agentes donde se despliegan:

  profiles:
    backend:
      skills: [systematic-debugging, mcp-builder]
      agents: [claude-code]
    web:
      skills: [systematic-debugging, web-design-guidelines]

skilus profile use despliega uno y retira lo que no le pertenece.`),
	}
	cmd.AddCommand(newProfileListCommand(list, t), newProfileUseCommand(use, t))
	return cmd
}

func newProfileListCommand(h app.ListProfilesHandler, t *i18n.Catalog) *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:   "list",
		Short: t.T("Lista los perfiles y marca el activo"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s, err := agent.ParseScope(scope)
			if err != nil {
				return err
			}
			profiles, err := h.Handle(cmd.Context(), app.ListProfiles{Scope: s})
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(profiles) == 0 {
				fmt.Fprintln(out, t.T("skilus.yaml no declara perfiles."))
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, t.T("\tPERFIL\tSKILLS\tAGENTES"))
			for _, st := range profiles {
				p := st.Profile
				mark := ""
				if st.Active {
					mark = "*"
				}
				skills := make([]string, 0, len(p.Skills()))
				for _, n := range p.Skills() {
					skills = append(skills, n.String())
				}
				agents := t.T("detectados")
				if ids := p.Agents(); len(ids) > 0 {
					names := make([]string, len(ids))
					for i, id := range ids {
						names[i] = id.String()
					}
					agents = strings.Join(names, ",")
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", mark, p.Name(), orDash(strings.Join(skills, ",")), agents)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			for _, st := range profiles {
				if len(st.Undeclared) > 0 {
					names := make([]string, len(st.Undeclared))
					for i, n := range st.Undeclared {
						names[i] = n.String()
					}
					fmt.Fprint(cmd.ErrOrStderr(), t.T("El perfil %s usa skills que no están en skills: %s\n", st.Profile.Name(), strings.Join(names, ", ")))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&scope, "scope", string(agent.ScopeProject), t.T("project o global"))
	return cmd
}

func newProfileUseCommand(h app.UseProfileHandler, t *i18n.Catalog) *cobra.Command {
	var (
		c           app.UseProfile
		scope, mode string
	)
	cmd := &cobra.Command{
		Use:   t.T("use <perfil>"),
		Short: t.T("Despliega un perfil en sus agentes y retira lo que no le pertenece"),
		Long: t.T(`Instala las skills del perfil que faltan, desde el origen que registra
skilus.yaml y con la misma inspección que skilus add; mueve las que ya
están a los agentes del perfil, si declara alguno; y retira de los agentes
y de skilus.lock las que el perfil no incluye. skilus.yaml no cambia, así
que volver al perfil anterior reinstala lo mismo.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := agent.ParseScope(scope)
			if err != nil {
				return err
			}
			c.Name, c.Scope, c.Mode = args[0], s, agent.Mode(mode)
			out := cmd.OutOrStdout()
			h.Prompter = prompter{in: cmd.InOrStdin(), out: out, t: t}
			res, err := h.Handle(cmd.Context(), c)
			if errors.Is(err, app.ErrRejected) || (err == nil && (c.Yes || c.DryRun)) {
				w := cmd.ErrOrStderr()
				if c.DryRun {
					w = out
				}
				renderProfilePlan(w, res.Plan, t)
			}
			if err != nil {
				return err
			}
			if res.Plan.Empty() {
				fmt.Fprint(out, t.T("El perfil %s ya está desplegado.\n", res.Plan.Profile))
				return nil
			}
			if c.DryRun {
				return nil
			}
			for _, e := range res.Installed {
				fmt.Fprint(out, t.T("Instalada %s (%s) en %d destino(s).\n", e.Skill, e.TreeHash.Short(), len(e.Targets)))
			}
			for _, e := range res.Retargeted {
				fmt.Fprint(out, t.T("Movida %s a %d destino(s).\n", e.Skill, len(e.Targets)))
			}
			for _, e := range res.Removed {
				fmt.Fprint(out, t.T("Retirada %s.\n", e.Skill))
			}
			fmt.Fprint(out, t.T("Perfil %s desplegado.\n", res.Plan.Profile))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&scope, "scope", string(agent.ScopeProject), t.T("project o global"))
	f.StringVar(&mode, "mode", "", t.T("symlink o copy para los destinos nuevos (por defecto: copy en proyecto, symlink en global)"))
	f.BoolVarP(&c.Yes, "yes", "y", false, t.T("no preguntes; los ejecutables siguen necesitando --allow-scripts o allow: [scripts]"))
	f.BoolVar(&c.Strict, "strict", false, t.T("trata los avisos de la inspección como bloqueos"))
	f.BoolVar(&c.AllowScripts, "allow-scripts", false, t.T("acepta ficheros ejecutables en todas las skills"))
	f.BoolVar(&c.DryRun, "dry-run", false, t.T("muestra el plan sin cambiar nada"))
	return cmd
}

func (p prompter) ConfirmProfile(_ context.Context, plan app.ProfilePlan) (bool, error) {
	renderProfilePlan(p.out, plan, p.t)
	return p.ask(p.t.T("¿Cambiar al perfil %s? [s/N] ", plan.Profile))
}

func renderProfilePlan(w io.Writer, plan app.ProfilePlan, t *i18n.Catalog) {
	fmt.Fprint(w, t.T("Perfil: %s\n", plan.Profile))
	if len(plan.Install) > 0 {
		fmt.Fprintln(w, t.T("Instalar:"))
	}
	for _, ip := range plan.Install {
		renderPlan(w, ip, t)
	}
	if len(plan.Retarget) > 0 {
		fmt.Fprintln(w, t.T("Mover a los agentes del perfil:"))
		for _, r := range plan.Retarget {
			fmt.Fprintf(w, "  %s\n", r.Entry.Skill)
			for _, tg := range r.Add {
				fmt.Fprintf(w, "    + %s (%s)\n", clean(filepath.Join(tg.Dir, r.Entry.Skill.String())), tg.Target.Agent)
			}
			for _, d := range r.Drop {
				fmt.Fprintf(w, "    - %s\n", clean(d))
			}
		}
	}
	if len(plan.Remove) > 0 {
		fmt.Fprintln(w, t.T("Retirar:"))
		for _, e := range plan.Remove {
			fmt.Fprintf(w, "  %s\n", e.Skill)
		}
	}
	if len(plan.Keep) > 0 {
		fmt.Fprintln(w, t.T("Sin cambios:"))
		for _, e := range plan.Keep {
			fmt.Fprintf(w, "  %s\n", e.Skill)
		}
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

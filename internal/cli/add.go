package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/policy"
)

func newAddCommand(h app.AddSkillHandler, t *i18n.Catalog) *cobra.Command {
	var (
		c            app.AddSkill
		scope, mode  string
		allowScripts bool
	)
	cmd := &cobra.Command{
		Use:   t.T("add <origen>"),
		Short: t.T("Inspecciona e instala skills desde un directorio, un repositorio Git o un ZIP o tar.gz"),
		Long: t.T(`Lee las skills del origen, las inspecciona y, tras tu confirmación,
las instala en los agentes detectados (o en los de --agent). Registra el
resultado en skilus.lock y la intención en skilus.yaml.

El origen puede ser un directorio (./skills), un repositorio Git
(owner/repo[@ref] o una URL) o un archivo ZIP o tar.gz por https. Dentro,
una skill (con SKILL.md en su raíz) o skills en skills/<nombre>/ o
<nombre>/.`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := agent.ParseScope(scope)
			if err != nil {
				return err
			}
			c.Source, c.Scope, c.Mode, c.AllowScripts = args[0], s, agent.Mode(mode), allowScripts

			out := cmd.OutOrStdout()
			h.Prompter = prompter{in: cmd.InOrStdin(), out: out, t: t}
			res, err := h.Handle(cmd.Context(), c)
			if errors.Is(err, app.ErrRejected) {
				renderPlan(cmd.ErrOrStderr(), res.Plan, t)
			}
			if err != nil {
				return err
			}
			for _, e := range res.Installed {
				fmt.Fprint(out, t.T("Instalada %s (%s) en %d destino(s).\n", e.Skill, e.TreeHash.Short(), len(e.Targets)))
			}
			if c.Yes {
				renderSkipped(cmd.ErrOrStderr(), res.Plan.Skipped, t)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&c.Skills, "skill", nil, t.T("instala solo estas skills del origen (repetible)"))
	f.StringSliceVar(&c.Agents, "agent", nil, t.T("instala en estos agentes en lugar de los detectados (repetible)"))
	f.StringVar(&scope, "scope", string(agent.ScopeProject), t.T("project o global"))
	f.StringVar(&mode, "mode", "", t.T("symlink o copy (por defecto: copy en proyecto, symlink en global)"))
	f.BoolVarP(&c.Yes, "yes", "y", false, t.T("no preguntes; los ejecutables siguen necesitando --allow-scripts"))
	f.BoolVar(&c.Strict, "strict", false, t.T("trata los avisos de la inspección como bloqueos"))
	f.BoolVar(&allowScripts, "allow-scripts", false, t.T("acepta ficheros ejecutables en las skills"))
	return cmd
}

// prompter implements app.Prompter on the terminal.
type prompter struct {
	in  io.Reader
	out io.Writer
	t   *i18n.Catalog
}

func (p prompter) ConfirmInstall(_ context.Context, plan app.InstallPlan) (bool, error) {
	renderPlan(p.out, plan, p.t)
	return p.ask(p.t.T("¿Instalar? [s/N] "))
}

func (p prompter) ConfirmUpdate(_ context.Context, plan app.UpdatePlan) (bool, error) {
	renderUpdatePlan(p.out, plan, p.t)
	return p.ask(p.t.T("¿Actualizar? [s/N] "))
}

func (p prompter) ask(question string) (bool, error) {
	fmt.Fprint(p.out, question)
	line, err := bufio.NewReader(p.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	fmt.Fprintln(p.out)
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "" {
		return false, nil
	}
	// "y" and "s" always work; each language adds its own words for yes.
	yes := "s,si,sí,y,yes," + p.t.T("s,si,sí")
	for _, w := range strings.Split(yes, ",") {
		if answer == strings.ToLower(strings.TrimSpace(w)) {
			return true, nil
		}
	}
	return false, nil
}

func renderPlan(w io.Writer, plan app.InstallPlan, t *i18n.Catalog) {
	renderSource(w, plan, t)
	for _, more := range plan.More {
		fmt.Fprintln(w)
		renderSource(w, more, t)
	}
}

func renderSource(w io.Writer, plan app.InstallPlan, t *i18n.Catalog) {
	fmt.Fprint(w, t.T("Origen: %s\n", clean(plan.Source)))
	if plan.Commit != "" {
		fmt.Fprint(w, t.T("Commit: %s\n", plan.Commit))
	}
	if len(plan.Targets) > 0 {
		fmt.Fprintln(w, t.T("Destinos:"))
		for _, tg := range plan.Targets {
			fmt.Fprintf(w, "  %s (%s, %s)\n", clean(tg.Dir), tg.Target.Agent, tg.Target.Mode)
		}
	}
	if len(plan.Skipped) > 0 {
		fmt.Fprintln(w, t.T("No se pueden leer y no se instalarán:"))
		for _, s := range plan.Skipped {
			fmt.Fprintf(w, "  %s: %s\n", clean(s.Path), clean(s.Err.Error()))
		}
	}
	fmt.Fprintln(w, t.T("Skills:"))
	for _, s := range plan.Skills {
		p := s.Package
		fmt.Fprint(w, t.T("  %s  %d ficheros, %d bytes, sha256 %s\n", p.Name(), len(p.Files()), p.Size(), p.TreeHash().Short()))
		fmt.Fprintf(w, "    %s\n", clean(p.Description()))
		if reqs := p.Requires(); len(reqs) > 0 {
			names := make([]string, len(reqs))
			for i, r := range reqs {
				names[i] = clean(r.String())
			}
			fmt.Fprint(w, t.T("    necesita %s\n", strings.Join(names, ", ")))
		}
		if s.Dependency {
			names := make([]string, len(s.RequiredBy))
			for i, n := range s.RequiredBy {
				names[i] = n.String()
			}
			fmt.Fprint(w, t.T("    dependencia de %s\n", strings.Join(names, ", ")))
		}
		renderReport(w, s.Report, t)
	}
}

func renderReport(w io.Writer, r policy.Report, t *i18n.Catalog) {
	for _, e := range r.Executables {
		fmt.Fprint(w, t.T("    ejecutable: %s\n", clean(e)))
	}
	for _, f := range r.Findings {
		label := t.T("aviso")
		if f.Severity == policy.Block {
			label = t.T("BLOQUEO")
		}
		where := ""
		if f.Path != "" {
			where = " " + clean(f.Path) + ":"
		}
		fmt.Fprintf(w, "    %s [%s]%s %s\n", label, f.Code, where, clean(text(f.Detail, t)))
	}
}

func renderSkipped(w io.Writer, skipped []app.InvalidSkill, t *i18n.Catalog) {
	for _, s := range skipped {
		fmt.Fprint(w, t.T("Omitida %s: %s\n", clean(s.Path), clean(s.Err.Error())))
	}
}

// clean makes untrusted text safe to print: control characters, which could
// drive the terminal, are shown as their Go escape.
func clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			fmt.Fprintf(&b, "%q", r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

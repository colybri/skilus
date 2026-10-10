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
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/policy"
)

func newAddCommand(h app.AddSkillHandler) *cobra.Command {
	var (
		c            app.AddSkill
		scope, mode  string
		allowScripts bool
	)
	cmd := &cobra.Command{
		Use:   "add <origen>",
		Short: "Inspecciona e instala skills desde un directorio local",
		Long: `Lee las skills del origen, las inspecciona y, tras tu confirmación,
las instala en los agentes detectados (o en los de --agent). Registra el
resultado en skilus.lock y la intención en skilus.yaml.

El origen puede ser una skill (con SKILL.md en su raíz) o un directorio con
skills en skills/<nombre>/ o <nombre>/.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := agent.ParseScope(scope)
			if err != nil {
				return err
			}
			c.Source, c.Scope, c.Mode, c.AllowScripts = args[0], s, agent.Mode(mode), allowScripts

			out := cmd.OutOrStdout()
			h.Prompter = prompter{in: cmd.InOrStdin(), out: out}
			res, err := h.Handle(cmd.Context(), c)
			if errors.Is(err, app.ErrRejected) {
				renderPlan(cmd.ErrOrStderr(), res.Plan)
			}
			if err != nil {
				return err
			}
			for _, e := range res.Installed {
				fmt.Fprintf(out, "Instalada %s (%s) en %d destino(s).\n", e.Skill, e.TreeHash.Short(), len(e.Targets))
			}
			if c.Yes {
				renderSkipped(cmd.ErrOrStderr(), res.Plan.Skipped)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&c.Skills, "skill", nil, "instala solo estas skills del origen (repetible)")
	f.StringSliceVar(&c.Agents, "agent", nil, "instala en estos agentes en lugar de los detectados (repetible)")
	f.StringVar(&scope, "scope", string(agent.ScopeProject), "project o global")
	f.StringVar(&mode, "mode", "", "symlink o copy (por defecto: copy en proyecto, symlink en global)")
	f.BoolVarP(&c.Yes, "yes", "y", false, "no preguntes; los ejecutables siguen necesitando --allow-scripts")
	f.BoolVar(&c.Strict, "strict", false, "trata los avisos de la inspección como bloqueos")
	f.BoolVar(&allowScripts, "allow-scripts", false, "acepta ficheros ejecutables en las skills")
	return cmd
}

// prompter implements app.Prompter on the terminal.
type prompter struct {
	in  io.Reader
	out io.Writer
}

func (p prompter) ConfirmInstall(_ context.Context, plan app.InstallPlan) (bool, error) {
	renderPlan(p.out, plan)
	return p.ask("¿Instalar? [s/N] ")
}

func (p prompter) ConfirmUpdate(_ context.Context, plan app.UpdatePlan) (bool, error) {
	renderUpdatePlan(p.out, plan)
	return p.ask("¿Actualizar? [s/N] ")
}

func (p prompter) ask(question string) (bool, error) {
	fmt.Fprint(p.out, question)
	line, err := bufio.NewReader(p.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	fmt.Fprintln(p.out)
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "s", "si", "sí", "y", "yes":
		return true, nil
	}
	return false, nil
}

func renderPlan(w io.Writer, plan app.InstallPlan) {
	fmt.Fprintf(w, "Origen: %s\n", clean(plan.Source))
	if plan.Commit != "" {
		fmt.Fprintf(w, "Commit: %s\n", plan.Commit)
	}
	if len(plan.Targets) > 0 {
		fmt.Fprintln(w, "Destinos:")
		for _, t := range plan.Targets {
			fmt.Fprintf(w, "  %s (%s, %s)\n", clean(t.Dir), t.Target.Agent, t.Target.Mode)
		}
	}
	if len(plan.Skipped) > 0 {
		fmt.Fprintln(w, "No se pueden leer y no se instalarán:")
		for _, s := range plan.Skipped {
			fmt.Fprintf(w, "  %s: %s\n", clean(s.Path), clean(s.Err.Error()))
		}
	}
	fmt.Fprintln(w, "Skills:")
	for _, s := range plan.Skills {
		p := s.Package
		fmt.Fprintf(w, "  %s  %d ficheros, %d bytes, sha256 %s\n", p.Name(), len(p.Files()), p.Size(), p.TreeHash().Short())
		fmt.Fprintf(w, "    %s\n", clean(p.Description()))
		renderReport(w, s.Report)
	}
}

func renderReport(w io.Writer, r policy.Report) {
	for _, e := range r.Executables {
		fmt.Fprintf(w, "    ejecutable: %s\n", clean(e))
	}
	for _, f := range r.Findings {
		label := "aviso"
		if f.Severity == policy.Block {
			label = "BLOQUEO"
		}
		where := ""
		if f.Path != "" {
			where = " " + clean(f.Path) + ":"
		}
		fmt.Fprintf(w, "    %s [%s]%s %s\n", label, f.Code, where, clean(f.Detail))
	}
}

func renderSkipped(w io.Writer, skipped []app.InvalidSkill) {
	for _, s := range skipped {
		fmt.Fprintf(w, "Omitida %s: %s\n", clean(s.Path), clean(s.Err.Error()))
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

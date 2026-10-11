package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/lock"
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
			st := styleFor(cmd, out)
			h.Prompter = prompter{in: cmd.InOrStdin(), out: out, t: t, st: st}
			if !c.Yes {
				h.Chooser = newChooser(cmd.InOrStdin(), out, st, t)
			}
			res, err := h.Handle(cmd.Context(), c)
			if errors.Is(err, app.ErrRejected) {
				renderPlan(cmd.ErrOrStderr(), res.Plan, styleFor(cmd, cmd.ErrOrStderr()), t)
			}
			if err != nil {
				return err
			}
			for _, e := range res.Installed {
				fmt.Fprint(out, installed(e, t))
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
	fullFlag(cmd, t)
	return cmd
}

// prompter implements app.Prompter on the terminal.
type prompter struct {
	in  io.Reader
	out io.Writer
	t   *i18n.Catalog
	st  style
}

func (p prompter) ConfirmInstall(_ context.Context, plan app.InstallPlan) (bool, error) {
	renderPlan(p.out, plan, p.st, p.t)
	return p.ask(p.t.T("¿Instalar? [s/N] "))
}

func (p prompter) ConfirmUpdate(_ context.Context, plan app.UpdatePlan) (bool, error) {
	renderUpdatePlan(p.out, plan, p.st, p.t)
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

// tally counts what a plan shows, for the line before the question.
type tally struct{ skills, blocks, warns int }

func (n *tally) add(r policy.Report) {
	n.skills++
	n.warns += r.Warnings()
	n.blocks += len(r.Findings) - r.Warnings()
}

func (n tally) render(w io.Writer, st style, t *i18n.Catalog) {
	blocks := count(n.blocks, t.T("%d bloqueo"), t.T("%d bloqueos"))
	if n.blocks > 0 {
		blocks = st.red(blocks)
	}
	warns := count(n.warns, t.T("%d aviso"), t.T("%d avisos"))
	if n.warns > 0 {
		warns = st.yellow(warns)
	}
	fmt.Fprintln(w, count(n.skills, t.T("%d skill"), t.T("%d skills"))+sep+blocks+sep+warns)
}

// renderPlan shows what add would install: each source with its skills,
// where they go and a summary.
func renderPlan(w io.Writer, plan app.InstallPlan, st style, t *i18n.Catalog) {
	var n tally
	renderSources(w, plan, st, t, &n)
	renderTargets(w, st, t, plan)
	n.render(w, st, t)
}

func renderSources(w io.Writer, plan app.InstallPlan, st style, t *i18n.Catalog, n *tally) {
	renderSource(w, plan, st, t, n)
	for _, more := range plan.More {
		renderSource(w, more, st, t, n)
	}
}

func renderSource(w io.Writer, plan app.InstallPlan, st style, t *i18n.Catalog, n *tally) {
	head := st.bold(clean(shortPath(plan.Source)))
	if plan.Commit != "" {
		head += sep + "commit " + short(plan.Commit)
	}
	fmt.Fprintln(w, head+sep+count(len(plan.Skills), t.T("%d skill"), t.T("%d skills")))
	fmt.Fprintln(w)
	if len(plan.Skipped) > 0 {
		fmt.Fprintln(w, t.T("No se pueden leer y no se instalarán:"))
		for _, s := range plan.Skipped {
			st.item(w, 2, st.yellow(markWarn), clean(s.Path)+": "+clean(s.Err.Error()))
		}
		fmt.Fprintln(w)
	}
	for _, s := range plan.Skills {
		p := s.Package
		info := []string{
			count(len(p.Files()), t.T("%d fichero"), t.T("%d ficheros")),
			size(p.Size(), t),
			"sha256 " + p.TreeHash().Short(),
		}
		fmt.Fprintln(w, markSkill+" "+st.bold(p.Name().String())+"   "+st.dim(strings.Join(info, sep)))
		if d := strings.Join(strings.Fields(p.Description()), " "); d != "" {
			lines := 2
			if st.full {
				lines = 0
			}
			st.line(w, 2, clean(d), lines)
		}
		if reqs := p.Requires(); len(reqs) > 0 {
			names := make([]string, len(reqs))
			for i, r := range reqs {
				names[i] = clean(r.String())
			}
			st.line(w, 2, t.T("necesita %s", strings.Join(names, ", ")), 0)
		}
		if s.Dependency {
			names := make([]string, len(s.RequiredBy))
			for i, r := range s.RequiredBy {
				names[i] = r.String()
			}
			st.line(w, 2, t.T("dependencia de %s", strings.Join(names, ", ")), 0)
		}
		renderReport(w, s.Report, st, t)
		n.add(s.Report)
		fmt.Fprintln(w)
	}
}

// renderTargets lists, once, where the skills of plans go, on one line
// when it fits.
func renderTargets(w io.Writer, st style, t *i18n.Catalog, plans ...app.InstallPlan) {
	var all []app.InstallPlan
	for _, plan := range plans {
		all = append(all, plan.All()...)
	}
	var dirs []string
	seen := map[string]bool{}
	for _, p := range all {
		for _, tg := range p.Targets {
			ids := []string{tg.Target.Agent.String()}
			if len(tg.Agents) > 0 {
				ids = ids[:0]
				for _, a := range tg.Agents {
					ids = append(ids, a.ID().String())
				}
			}
			d := fmt.Sprintf("%s (%s, %s)", clean(shortPath(tg.Dir)), strings.Join(ids, ", "), tg.Target.Mode)
			if !seen[d] {
				seen[d] = true
				dirs = append(dirs, d)
			}
		}
	}
	if len(dirs) == 0 {
		return
	}
	label := t.T("Destinos:")
	if one := label + " " + strings.Join(dirs, sep); st.width == 0 || textWidth(one) <= st.width {
		fmt.Fprintln(w, one)
		return
	}
	fmt.Fprintln(w, label)
	for _, d := range dirs {
		fmt.Fprintln(w, "  "+d)
	}
}

// shortPath shows an absolute dir relative to the working directory when it
// is inside it, or to the home directory as ~. Anything else, such as a
// repository or a URL, is returned as is.
func shortPath(dir string) string {
	if !filepath.IsAbs(dir) {
		return dir
	}
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(wd, dir); err == nil && filepath.IsLocal(rel) {
			return rel
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, dir); err == nil && filepath.IsLocal(rel) {
			return filepath.Join("~", rel)
		}
	}
	return dir
}

// renderReport shows the inspection of one skill: its findings, the
// executables already accepted, or that there is nothing to report.
func renderReport(w io.Writer, r policy.Report, st style, t *i18n.Catalog) {
	if !renderFindings(w, r, st, t) {
		st.item(w, 2, st.green(markOK), t.T("sin hallazgos"))
	}
}

// renderFindings shows the findings and accepted executables of r and
// reports whether there was any.
func renderFindings(w io.Writer, r policy.Report, st style, t *i18n.Catalog) bool {
	flagged := map[string]bool{}
	for _, f := range r.Findings {
		mark := st.yellow(markWarn)
		if f.Severity == policy.Block {
			mark = st.red(markBlock)
		}
		where := ""
		if f.Path != "" {
			where = clean(f.Path) + ": "
			if f.Code == policy.CodeExecutable {
				flagged[f.Path] = true
			}
		}
		st.tagged(w, 2, mark, where+clean(text(f.Detail, t)), "["+string(f.Code)+"]")
	}
	accepted := 0
	for _, e := range r.Executables {
		if !flagged[e] {
			accepted++
			st.item(w, 2, st.dim(markOK), t.T("ejecutable aceptado: %s", clean(e)))
		}
	}
	return len(r.Findings) > 0 || accepted > 0
}

// installed reports that an entry was installed.
func installed(e lock.Entry, t *i18n.Catalog) string {
	if len(e.Targets) == 1 {
		return t.T("Instalada %s (%s) en 1 destino.\n", e.Skill, e.TreeHash.Short())
	}
	return t.T("Instalada %s (%s) en %d destinos.\n", e.Skill, e.TreeHash.Short(), len(e.Targets))
}

// fullFlag adds --full to a command that shows install plans.
func fullFlag(cmd *cobra.Command, t *i18n.Catalog) {
	cmd.Flags().Bool("full", false, t.T("muestra las descripciones completas"))
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

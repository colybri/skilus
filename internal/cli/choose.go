package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain/skill"
)

// chooser implements app.Chooser with selectors on the terminal.
type chooser struct {
	in, out *os.File
	st      style
	t       *i18n.Catalog
}

// newChooser returns a chooser when both ends are a terminal that can draw
// the selectors, or nil.
func newChooser(in, out any, st style, t *i18n.Catalog) app.Chooser {
	fin, ok := in.(*os.File)
	if !ok || !isTerminal(fin) {
		return nil
	}
	fout, ok := out.(*os.File)
	if !ok || !isTerminal(fout) || !enableColor(fout) {
		return nil
	}
	return chooser{in: fin, out: fout, st: st, t: t}
}

func (c chooser) ChooseSkills(_ context.Context, src, commit string, skills []skill.Package) ([]skill.Name, error) {
	t, st := c.t, c.st
	head := st.bold(clean(shortPath(src)))
	if commit != "" {
		head += sep + "commit " + short(commit)
	}
	fmt.Fprintln(c.out, head+sep+count(len(skills), t.T("%d skill"), t.T("%d skills")))
	fmt.Fprintln(c.out)

	sel := selector{
		title: t.T("¿Qué skills instalo?"),
		hint:  t.T("↑↓ mover · espacio marcar · → todas · ← ninguna · escribe para filtrar · enter aceptar"),
		footer: func(shown, checked int) string {
			if shown < len(skills) {
				return t.T("%d de %d coinciden · %d marcadas", shown, len(skills), checked)
			}
			return t.T("%d de %d marcadas", checked, len(skills))
		},
	}
	for _, p := range skills {
		desc := clean(strings.Join(strings.Fields(p.Description()), " "))
		sel.options = append(sel.options, option{
			label: p.Name().String(), detail: desc, checked: true,
			match: p.Name().String(),
		})
	}
	if err := c.run(&sel); err != nil {
		return nil, err
	}
	var out []skill.Name
	for i, o := range sel.options {
		if o.checked {
			out = append(out, skills[i].Name())
		}
	}
	return out, nil
}

func (c chooser) ChooseTargets(_ context.Context, targets []app.PlannedTarget, total int) ([]app.PlannedTarget, error) {
	t := c.t
	installed := 0
	sel := selector{
		title: t.T("¿En qué agentes?"),
		hint:  t.T("↑↓ mover · espacio marcar · → todos · ← ninguno · escribe para filtrar · enter aceptar"),
	}
	for _, tg := range targets {
		names := make([]string, len(tg.Agents))
		ids := make([]string, len(tg.Agents))
		for i, a := range tg.Agents {
			names[i], ids[i] = clean(a.Name()), a.ID().String()
		}
		installed += len(tg.Agents)
		sel.options = append(sel.options, option{
			label: strings.Join(names, ", "), detail: clean(shortPath(tg.Dir)), checked: true,
			match: strings.ToLower(strings.Join(names, " ") + " " + strings.Join(ids, " ")),
		})
	}
	sel.footer = func(int, int) string {
		return t.T("%d de %d agentes instalados · los demás, con --agent <id> (lista en skilus agents)", installed, total)
	}
	if err := c.run(&sel); err != nil {
		return nil, err
	}
	var out []app.PlannedTarget
	for i, o := range sel.options {
		if o.checked {
			out = append(out, targets[i])
		}
	}
	return out, nil
}

// run shows sel; Esc and Ctrl+C cancel the installation.
func (c chooser) run(sel *selector) error {
	ok, err := sel.run(c.in, c.out, c.st, c.t)
	if err != nil {
		return err
	}
	if !ok {
		return app.ErrCancelled
	}
	return nil
}

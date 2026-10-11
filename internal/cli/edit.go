package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
)

func newEditCommand(h app.EditManifestHandler, t *i18n.Catalog) *cobra.Command {
	var scope string
	var printPath bool
	cmd := &cobra.Command{
		Use:   "edit",
		Short: t.T("Abre skilus.yaml en tu editor y lo guarda solo si es válido"),
		Long: t.T(`Abre el skilus.yaml del proyecto, o el global con --scope global, en tu
editor. Si no existe, empieza con una plantilla comentada. El editor es el
de SKILUS_EDITOR, VISUAL o EDITOR, el core.editor de git o, si no hay
ninguno, notepad en Windows y nano o vi en el resto. Un editor gráfico
tiene que esperar a que cierres el fichero, como code --wait.

Al cerrar el editor, skilus comprueba el fichero como skilus verify. Si
tiene errores, los muestra y pregunta si quieres volver a editarlo; si no,
descarta los cambios y skilus.yaml queda como estaba. --path solo imprime
la ruta del fichero.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			sc, err := agent.ParseScope(scope)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if printPath {
				fmt.Fprintln(out, h.Files.ManifestPath(sc))
				return nil
			}
			in := bufio.NewReader(cmd.InOrStdin())
			res, err := h.Edit(cmd.Context(), app.EditManifest{
				Scope:    sc,
				Template: []byte(manifestTemplate(t)),
				Retry: func(_ context.Context, problems error) (bool, error) {
					renderEditProblems(cmd.ErrOrStderr(), problems, t)
					return prompter{in: in, out: out, t: t}.ask(t.T("¿Volver a editarlo? [s/N] "))
				},
			})
			if err != nil {
				return err
			}
			switch {
			case res.Outcome == app.EditUnchanged:
				fmt.Fprint(out, t.T("Sin cambios en %s\n", res.Path))
				return nil
			case res.Created:
				fmt.Fprint(out, t.T("Creado %s\n", res.Path))
			default:
				fmt.Fprint(out, t.T("Guardado %s\n", res.Path))
			}
			fmt.Fprintln(out, t.T("Editar skilus.yaml no instala nada; skilus verify dice qué falta por aplicar."))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&scope, "scope", string(agent.ScopeProject), t.T("project o global"))
	f.BoolVar(&printPath, "path", false, t.T("imprime la ruta de skilus.yaml sin abrir el editor"))
	return cmd
}

func renderEditProblems(w io.Writer, problems error, t *i18n.Catalog) {
	fmt.Fprintln(w, t.T("skilus.yaml tiene errores:"))
	list, ok := problems.(domain.Errors) //nolint:errorlint // the handler returns the list itself
	if !ok {
		list = domain.Errors{problems}
	}
	for _, p := range list {
		fmt.Fprintln(w, "  -", localize(p, t))
	}
}

// manifestTemplate is what a new skilus.yaml starts with: the keys it
// takes, commented out, in the user's language.
func manifestTemplate(t *i18n.Catalog) string {
	var b strings.Builder
	for _, line := range strings.Split(t.T(`skilus.yaml: las skills que quieres y de dónde salen. skilus add y
skilus remove lo mantienen al día; también puedes editarlo a mano.
Quita el # de las secciones que quieras usar.`), "\n") {
		b.WriteString("# " + line + "\n")
	}
	b.WriteString(`version: 1
skills: []
#  - name: pdf
#    source: anthropics/skills@v1.0.0

`)
	b.WriteString("# " + t.T("Perfiles: conjuntos de skills de skills: que activas con skilus profile use.") + "\n")
	b.WriteString(`# profiles:
#   web:
#     skills: [pdf]
#     agents: [claude-code]

`)
	b.WriteString("# " + t.T("Orígenes de confianza; con la lista, skilus avisa de los demás.") + "\n")
	b.WriteString(`# trust:
#   - github.com/anthropics

`)
	b.WriteString("# " + t.T("Índices curados que consulta skilus search.") + "\n")
	b.WriteString(`# indexes:
#   - https://example.com/skills-index.yaml
`)
	return b.String()
}

package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
)

type agentJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ProjectDir string `json:"project_dir"`
	GlobalDir  string `json:"global_dir"`
	Installed  bool   `json:"installed"`
}

func newAgentsCommand(q app.ListAgents, t *i18n.Catalog) *cobra.Command {
	var asJSON, all bool
	cmd := &cobra.Command{
		Use:   "agents",
		Short: t.T("Lista los agentes instalados en este equipo y sus rutas"),
		Long: t.T(`Lista los agentes que skilus detecta en este equipo, con la carpeta de
skills de cada uno en un proyecto y en el ámbito global. Con --all lista
todos los que soporta, detectados o no.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			statuses, err := q.Handle(cmd.Context())
			if err != nil {
				return err
			}
			total := len(statuses)
			if !all {
				detected := statuses[:0]
				for _, s := range statuses {
					if s.Installed {
						detected = append(detected, s)
					}
				}
				statuses = detected
			}
			out := cmd.OutOrStdout()
			if asJSON {
				rows := make([]agentJSON, 0, len(statuses))
				for _, s := range statuses {
					rows = append(rows, agentJSON{
						ID:         s.Agent.ID().String(),
						Name:       s.Agent.Name(),
						ProjectDir: s.Agent.ProjectDir(),
						GlobalDir:  s.Agent.GlobalDir(),
						Installed:  s.Installed,
					})
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			// The hint goes to stderr so the table stays easy to pipe.
			hidden := total - len(statuses)
			if len(statuses) == 0 {
				fmt.Fprint(cmd.ErrOrStderr(), t.T("No se ha detectado ningún agente; skilus agents --all lista los %d que soporta.\n", total))
				return nil
			}
			defer func() {
				if hidden > 0 {
					fmt.Fprint(cmd.ErrOrStderr(), count(hidden,
						t.T("%d agente soportado más sin detectar; skilus agents --all lo lista.\n"),
						t.T("%d agentes soportados más sin detectar; skilus agents --all los lista.\n")))
				}
			}()
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, t.T("AGENTE\tPROYECTO\tGLOBAL\tDETECTADO"))
			for _, s := range statuses {
				detected := t.T("no")
				if s.Installed {
					detected = t.T("sí")
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Agent.ID(), s.Agent.ProjectDir(), s.Agent.GlobalDir(), detected)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, t.T("salida en JSON"))
	cmd.Flags().BoolVarP(&all, "all", "a", false, t.T("lista también los agentes no detectados"))
	return cmd
}

package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
)

type agentJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ProjectDir string `json:"project_dir"`
	GlobalDir  string `json:"global_dir"`
	Installed  bool   `json:"installed"`
}

func newAgentsCommand(q app.ListAgents) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Lista los agentes soportados y cuáles están instalados",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			statuses, err := q.Handle(cmd.Context())
			if err != nil {
				return err
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
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "AGENTE\tPROYECTO\tGLOBAL\tDETECTADO")
			for _, s := range statuses {
				detected := "no"
				if s.Installed {
					detected = "sí"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Agent.ID(), s.Agent.ProjectDir(), s.Agent.GlobalDir(), detected)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "salida en JSON")
	return cmd
}

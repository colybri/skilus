package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain/agent"
)

type foundJSON struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Description string `json:"description,omitempty"`
	Installs    int    `json:"installs,omitempty"`
	Origin      string `json:"origin"`
	Trusted     *bool  `json:"trusted,omitempty"`
}

func newSearchCommand(h app.SearchHandler) *cobra.Command {
	var (
		q      app.Search
		scope  string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "search <consulta>...",
		Short: "Busca skills en skills.sh y en los índices de skilus.yaml",
		Long: `Busca en skills.sh y en los índices curados que declara indexes: en
skilus.yaml (del proyecto y el global):

  indexes:
    - https://example.com/skills.yaml

Un índice es un fichero YAML con version: 1 y una lista skills: de name,
source y description. Sus resultados salen primero. Si hay lista trust:,
cada resultado dice si su origen es de confianza. search no instala nada:
usa skilus inspect y skilus add con el origen que muestra.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := agent.ParseScope(scope)
			if err != nil {
				return err
			}
			q.Query, q.Scope = strings.Join(args, " "), s
			res, err := h.Handle(cmd.Context(), q)
			for _, f := range res.Failed {
				fmt.Fprintf(cmd.ErrOrStderr(), "No se pudo consultar %s: %s\n", clean(f.Origin), clean(f.Err.Error()))
			}
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				rows := make([]foundJSON, 0, len(res.Found))
				for _, f := range res.Found {
					rows = append(rows, foundJSON{Name: f.Name, Source: f.Source, Description: f.Description, Installs: f.Installs, Origin: f.Origin, Trusted: f.Trusted})
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			if len(res.Found) == 0 {
				fmt.Fprintln(out, "Sin resultados.")
				return nil
			}
			withTrust := false
			for _, f := range res.Found {
				withTrust = withTrust || f.Trusted != nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			header := "SKILL\tORIGEN\tINSTALACIONES\tENCONTRADA EN"
			if withTrust {
				header += "\tCONFIANZA"
			}
			fmt.Fprintln(tw, header)
			for _, f := range res.Found {
				installs := "-"
				if f.Installs > 0 {
					installs = strconv.Itoa(f.Installs)
				}
				line := fmt.Sprintf("%s\t%s\t%s\t%s", clean(f.Name), clean(f.Source), installs, clean(f.Origin))
				if withTrust {
					trust := "no"
					if f.Trusted != nil && *f.Trusted {
						trust = "sí"
					}
					line += "\t" + trust
				}
				fmt.Fprintln(tw, line)
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			first := res.Found[0]
			fmt.Fprintf(out, "\nRevisa una con: skilus inspect %s --skill %s\n", clean(first.Source), clean(first.Name))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&q.Owner, "owner", "", "solo skills de este propietario de GitHub")
	f.IntVar(&q.Limit, "limit", app.DefaultSearchLimit, "máximo de resultados por origen")
	f.BoolVar(&q.NoRegistry, "no-registry", false, "no consultes skills.sh, solo los índices")
	f.StringVar(&scope, "scope", string(agent.ScopeProject), "project (incluye el global) o global")
	f.BoolVar(&asJSON, "json", false, "salida en JSON")
	return cmd
}

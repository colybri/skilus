package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain/agent"
)

type skillJSON struct {
	Name       string       `json:"name"`
	Scope      string       `json:"scope"`
	Source     string       `json:"source"`
	Requested  string       `json:"requested,omitempty"`
	Commit     string       `json:"commit,omitempty"`
	Path       string       `json:"path"`
	TreeSHA256 string       `json:"tree_sha256"`
	Targets    []targetJSON `json:"targets"`
	Requires   []string     `json:"requires,omitempty"`
	Dependency bool         `json:"dependency,omitempty"`
}

type targetJSON struct {
	Agent string `json:"agent"`
	Mode  string `json:"mode"`
}

func newListCommand(q app.ListSkills, t *i18n.Catalog) *cobra.Command {
	var (
		asJSON bool
		scope  string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: t.T("Lista las skills instaladas con su commit, hash y agentes"),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scopes := []agent.Scope{agent.ScopeProject, agent.ScopeGlobal}
			if scope != "all" {
				s, err := agent.ParseScope(scope)
				if err != nil {
					return err
				}
				scopes = []agent.Scope{s}
			}
			skills, err := q.Handle(cmd.Context(), scopes...)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				rows := make([]skillJSON, 0, len(skills))
				for _, s := range skills {
					e := s.Entry
					row := skillJSON{Name: e.Skill.String(), Scope: string(s.Scope), Source: e.Source, Requested: e.Requested, Commit: e.Commit, Path: e.Path, TreeSHA256: e.TreeHash.String(), Dependency: e.Dependency}
					for _, r := range e.Requires {
						row.Requires = append(row.Requires, r.String())
					}
					for _, tg := range e.Targets {
						row.Targets = append(row.Targets, targetJSON{Agent: tg.Agent.String(), Mode: string(tg.Mode)})
					}
					rows = append(rows, row)
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			if len(skills) == 0 {
				fmt.Fprintln(out, t.T("No hay skills instaladas."))
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, t.T("SKILL\tÁMBITO\tORIGEN\tCOMMIT\tHASH\tAGENTES"))
			for _, s := range skills {
				e := s.Entry
				source := e.Source
				if e.Requested != "" {
					source += "@" + e.Requested
				}
				commit := "-"
				if len(e.Commit) >= 12 {
					commit = e.Commit[:12]
				}
				agents := make([]string, 0, len(e.Targets))
				for _, tg := range e.Targets {
					agents = append(agents, tg.Agent.String())
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", e.Skill, s.Scope, clean(source), commit, e.TreeHash.Short(), strings.Join(agents, ","))
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			for _, s := range skills {
				if e := s.Entry; len(e.Requires) > 0 {
					names := make([]string, len(e.Requires))
					for i, r := range e.Requires {
						names[i] = r.String()
					}
					fmt.Fprint(out, t.T("%s necesita %s.\n", e.Skill, strings.Join(names, ", ")))
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, t.T("salida en JSON"))
	cmd.Flags().StringVar(&scope, "scope", "all", t.T("project, global o all"))
	return cmd
}

func newRemoveCommand(h app.RemoveSkillHandler, t *i18n.Catalog) *cobra.Command {
	var scope string
	cmd := &cobra.Command{
		Use:     t.T("remove <skill>..."),
		Aliases: []string{"rm"},
		Short:   t.T("Quita skills de sus agentes, de skilus.yaml y del lock"),
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := agent.ParseScope(scope)
			if err != nil {
				return err
			}
			removed, err := h.Handle(cmd.Context(), app.RemoveSkill{Names: args, Scope: s})
			for _, e := range removed {
				note := ""
				if e.Dependency {
					note = t.T(" (era una dependencia)")
				}
				fmt.Fprint(cmd.OutOrStdout(), t.T("Eliminada %s%s de %d destino(s).\n", e.Skill, note, len(e.Targets)))
			}
			return err
		},
	}
	cmd.Flags().StringVar(&scope, "scope", string(agent.ScopeProject), t.T("project o global"))
	return cmd
}

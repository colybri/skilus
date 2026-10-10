package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain/agent"
)

type syncJSON struct {
	Skill  string `json:"skill"`
	Scope  string `json:"scope"`
	Agent  string `json:"agent"`
	Dir    string `json:"dir"`
	Action string `json:"action"`
}

func newSyncCommand(h app.SyncHandler) *cobra.Command {
	var (
		c      app.Sync
		asJSON bool
		scope  string
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Deja los agentes exactamente como dice el lock",
		Long: `Instala cada skill de skilus.lock en sus agentes, descargándola por su
commit si el almacén no la tiene, y falla si su hash no coincide con el del
lock. No cambia skilus.lock ni skilus.yaml.

Si una skill instalada se ha modificado a mano, no toca nada y avisa; con
--force la sobrescribe.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scopes, err := parseScopes(scope)
			if err != nil {
				return err
			}
			c.Scopes = scopes
			res, err := h.Handle(cmd.Context(), c)
			out := cmd.OutOrStdout()
			if asJSON {
				rows := make([]syncJSON, 0, len(res.Targets))
				for _, t := range res.Targets {
					rows = append(rows, syncJSON{Skill: t.Entry.Skill.String(), Scope: string(t.Scope), Agent: t.Target.Agent.String(), Dir: t.Dir, Action: string(t.Action)})
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if jerr := enc.Encode(rows); jerr != nil {
					return jerr
				}
				return err
			}
			for _, t := range res.Targets {
				if t.Action == app.SyncBlocked {
					fmt.Fprintf(cmd.ErrOrStderr(), "Modificada a mano: %s (%s)\n", clean(t.Dir), t.Entry.Skill)
				}
			}
			if err != nil {
				return err
			}
			for _, e := range res.Downloaded {
				fmt.Fprintf(out, "Descargada %s (%s)\n", e.Skill, e.TreeHash.Short())
			}
			count := map[app.SyncAction]int{}
			for _, t := range res.Targets {
				switch t.Action {
				case app.SyncCreated:
					fmt.Fprintf(out, "Instalada %s en %s\n", t.Entry.Skill, clean(t.Dir))
				case app.SyncReplaced:
					fmt.Fprintf(out, "Reemplazada %s en %s\n", t.Entry.Skill, clean(t.Dir))
				}
				count[t.Action]++
			}
			fmt.Fprintf(out, "Sincronizado: %d instalada(s), %d reemplazada(s), %d sin cambios.\n", count[app.SyncCreated], count[app.SyncReplaced], count[app.SyncUnchanged])
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&c.Force, "force", false, "sobrescribe las skills modificadas a mano")
	f.BoolVar(&asJSON, "json", false, "salida en JSON")
	f.StringVar(&scope, "scope", string(agent.ScopeProject), "project, global o all")
	return cmd
}

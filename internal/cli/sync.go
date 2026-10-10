package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain/agent"
)

type syncJSON struct {
	Skill  string `json:"skill"`
	Scope  string `json:"scope"`
	Agent  string `json:"agent"`
	Dir    string `json:"dir"`
	Action string `json:"action"`
}

func newSyncCommand(h app.SyncHandler, t *i18n.Catalog) *cobra.Command {
	var (
		c      app.Sync
		asJSON bool
		scope  string
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Short: t.T("Deja los agentes exactamente como dice el lock"),
		Long: t.T(`Instala cada skill de skilus.lock en sus agentes, descargándola por su
commit si el almacén no la tiene, y falla si su hash no coincide con el del
lock. No cambia skilus.lock ni skilus.yaml.

Si una skill instalada se ha modificado a mano, no toca nada y avisa; con
--force la sobrescribe.`),
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
				for _, tg := range res.Targets {
					rows = append(rows, syncJSON{Skill: tg.Entry.Skill.String(), Scope: string(tg.Scope), Agent: tg.Target.Agent.String(), Dir: tg.Dir, Action: string(tg.Action)})
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if jerr := enc.Encode(rows); jerr != nil {
					return jerr
				}
				return err
			}
			for _, tg := range res.Targets {
				if tg.Action == app.SyncBlocked {
					fmt.Fprint(cmd.ErrOrStderr(), t.T("Modificada a mano: %s (%s)\n", clean(tg.Dir), tg.Entry.Skill))
				}
			}
			if err != nil {
				return err
			}
			for _, e := range res.Downloaded {
				fmt.Fprint(out, t.T("Descargada %s (%s)\n", e.Skill, e.TreeHash.Short()))
			}
			count := map[app.SyncAction]int{}
			for _, tg := range res.Targets {
				switch tg.Action {
				case app.SyncCreated:
					fmt.Fprint(out, t.T("Instalada %s en %s\n", tg.Entry.Skill, clean(tg.Dir)))
				case app.SyncReplaced:
					fmt.Fprint(out, t.T("Reemplazada %s en %s\n", tg.Entry.Skill, clean(tg.Dir)))
				}
				count[tg.Action]++
			}
			fmt.Fprint(out, t.T("Sincronizado: %d instalada(s), %d reemplazada(s), %d sin cambios.\n", count[app.SyncCreated], count[app.SyncReplaced], count[app.SyncUnchanged]))
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&c.Force, "force", false, t.T("sobrescribe las skills modificadas a mano"))
	f.BoolVar(&asJSON, "json", false, t.T("salida en JSON"))
	f.StringVar(&scope, "scope", string(agent.ScopeProject), t.T("project, global o all"))
	return cmd
}

package cli

import (
	"encoding/json"
	"errors"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
)

type inspectJSON struct {
	Source  string             `json:"source"`
	Commit  string             `json:"commit,omitempty"`
	Skills  []inspectSkillJSON `json:"skills"`
	Skipped []skippedJSON      `json:"skipped,omitempty"`
}

type inspectSkillJSON struct {
	Name        string        `json:"name"`
	Path        string        `json:"path"`
	Description string        `json:"description"`
	Files       int           `json:"files"`
	Bytes       int64         `json:"bytes"`
	TreeSHA256  string        `json:"tree_sha256"`
	Executables []string      `json:"executables,omitempty"`
	Requires    []string      `json:"requires,omitempty"`
	Findings    []findingJSON `json:"findings,omitempty"`
}

type findingJSON struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Detail   string `json:"detail"`
}

type skippedJSON struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

func newInspectCommand(q app.InspectHandler, t *i18n.Catalog) *cobra.Command {
	var (
		c      app.Inspect
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   t.T("inspect <origen>"),
		Short: t.T("Inspecciona las skills de un origen sin instalar nada"),
		Long: t.T(`Lee las skills del origen y muestra lo mismo que skilus add antes de
preguntar: ficheros, tamaño, hash, ejecutables y hallazgos. Sale con
código 5 si add rechazaría instalarlas (bloqueos, o avisos con --strict).`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c.Source = args[0]
			plan, err := q.Handle(cmd.Context(), c)
			if err != nil && !errors.Is(err, app.ErrRejected) {
				return err
			}
			out := cmd.OutOrStdout()
			if !asJSON {
				renderPlan(out, plan, t)
				return err
			}
			doc := inspectJSON{Source: plan.Source, Commit: plan.Commit, Skills: []inspectSkillJSON{}}
			for _, s := range plan.Skills {
				p := s.Package
				row := inspectSkillJSON{
					Name: p.Name().String(), Path: s.Path, Description: p.Description(),
					Files: len(p.Files()), Bytes: p.Size(), TreeSHA256: p.TreeHash().String(),
					Executables: s.Report.Executables,
				}
				for _, r := range p.Requires() {
					row.Requires = append(row.Requires, r.String())
				}
				for _, f := range s.Report.Findings {
					row.Findings = append(row.Findings, findingJSON{Code: string(f.Code), Severity: string(f.Severity), Path: f.Path, Detail: f.Detail})
				}
				doc.Skills = append(doc.Skills, row)
			}
			for _, s := range plan.Skipped {
				doc.Skipped = append(doc.Skipped, skippedJSON{Path: s.Path, Error: s.Err.Error()})
			}
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			if jerr := enc.Encode(doc); jerr != nil {
				return jerr
			}
			return err
		},
	}
	f := cmd.Flags()
	f.StringSliceVar(&c.Skills, "skill", nil, t.T("inspecciona solo estas skills del origen (repetible)"))
	f.BoolVar(&c.Strict, "strict", false, t.T("trata los avisos como bloqueos"))
	f.BoolVar(&c.AllowScripts, "allow-scripts", false, t.T("no avises de los ficheros ejecutables"))
	f.BoolVar(&asJSON, "json", false, t.T("salida en JSON"))
	return cmd
}

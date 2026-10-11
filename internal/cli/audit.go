package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli/i18n"
)

type auditJSON struct {
	Skill          string         `json:"skill"`
	Scope          string         `json:"scope"`
	Source         string         `json:"source"`
	Commit         string         `json:"commit,omitempty"`
	Signature      *signatureJSON `json:"signature,omitempty"`
	ContentChecked bool           `json:"content_checked"`
	Findings       []findingJSON  `json:"findings,omitempty"`
}

type signatureJSON struct {
	State      string `json:"state"`
	Format     string `json:"format,omitempty"`
	VerifiedBy string `json:"verified_by,omitempty"`
	Signer     string `json:"signer,omitempty"`
}

func newAuditCommand(h app.AuditHandler, t *i18n.Catalog) *cobra.Command {
	var (
		q      app.Audit
		scope  string
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "audit",
		Short: t.T("Revisa lo instalado con las reglas de hoy: confianza, firmas e inspección"),
		Long: t.T(`Para cada skill de skilus.lock comprueba si su origen está en trust:, si
el commit fijado está firmado y la firma se puede verificar (con tus claves
de git o, en GitHub, con las que GitHub conoce) y vuelve a inspeccionar el
contenido con las heurísticas actuales. Los ejecutables aceptados al
instalar no se repiten.

Sale con código 5 si hay bloqueos, o avisos con --strict, para usarlo en CI.`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			scopes, err := parseScopes(scope)
			if err != nil {
				return err
			}
			q.Scopes = scopes
			res, err := h.Handle(cmd.Context(), q)
			if err != nil && !errors.Is(err, app.ErrRejected) {
				return err
			}
			out := cmd.OutOrStdout()
			if asJSON {
				rows := make([]auditJSON, 0, len(res.Skills))
				for _, a := range res.Skills {
					row := auditJSON{Skill: a.Entry.Skill.String(), Scope: string(a.Scope), Source: a.Entry.Source, Commit: a.Entry.Commit, ContentChecked: a.ContentChecked}
					if s := a.Signature; s != nil {
						row.Signature = &signatureJSON{State: string(s.State), Format: s.Format, VerifiedBy: s.VerifiedBy, Signer: s.Signer}
					}
					for _, f := range a.Findings {
						row.Findings = append(row.Findings, findingJSON{Code: string(f.Code), Severity: string(f.Severity), Path: f.Path, Detail: text(f.Detail, t)})
					}
					rows = append(rows, row)
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				if encErr := enc.Encode(rows); encErr != nil {
					return encErr
				}
				return err
			}
			renderAudit(out, res, t)
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&scope, "scope", "all", t.T("project, global o all"))
	f.BoolVar(&q.Strict, "strict", false, t.T("los avisos también hacen fallar la auditoría"))
	f.BoolVar(&asJSON, "json", false, t.T("salida en JSON"))
	return cmd
}

func renderAudit(w io.Writer, res app.AuditResult, t *i18n.Catalog) {
	if len(res.Skills) == 0 {
		fmt.Fprintln(w, t.T("No hay skills instaladas."))
		return
	}
	blocks, warns := 0, 0
	for _, a := range res.Skills {
		e := a.Entry
		where := clean(e.Source)
		if e.Commit != "" {
			where += " @ " + short(e.Commit)
		}
		fmt.Fprintf(w, "%s (%s)  %s\n", e.Skill, a.Scope, where)
		if line := signatureLine(a, t); line != "" {
			fmt.Fprint(w, t.T("    firma: %s\n", line))
		}
		if !a.ContentChecked {
			fmt.Fprintln(w, t.T("    contenido: no está en el almacén, no se ha revisado; ejecuta skilus sync"))
		}
		renderReport(w, a.Report(), t)
		r := a.Report()
		blocks += len(r.Findings) - r.Warnings()
		warns += r.Warnings()
	}
	fmt.Fprint(w, t.T("Auditadas %d skill(s): %d bloqueo(s), %d aviso(s).\n", len(res.Skills), blocks, warns))
}

func signatureLine(a app.AuditedSkill, t *i18n.Catalog) string {
	s := a.Signature
	if s == nil {
		return ""
	}
	switch s.State {
	case app.Verified:
		line := t.T("verificada por %s (%s)", s.VerifiedBy, s.Format)
		if s.Signer != "" {
			line += ", " + clean(s.Signer)
		}
		return line
	case app.Signed:
		return t.T("firmada (%s), pero ni tus claves ni el servidor la verifican", s.Format)
	default:
		return t.T("sin firmar")
	}
}

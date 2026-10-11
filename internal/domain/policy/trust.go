package policy

import (
	"strings"

	"github.com/colybri/skilus/internal/domain"
)

// Trust is the list of sources the user trusts, from trust: in
// skilus.yaml. An entry covers the source with that normalized name and
// every source under it: github.com/anthropics covers
// github.com/anthropics/skills. An empty list trusts every source, so the
// check is opt-in.
type Trust []string

// Covers reports whether id, a source's normalized name, is trusted.
func (t Trust) Covers(id string) bool {
	if len(t) == 0 {
		return true
	}
	id = strings.TrimSuffix(id, "/")
	for _, entry := range t {
		entry = strings.TrimSuffix(strings.TrimSpace(entry), "/")
		if entry == "" {
			continue
		}
		if strings.EqualFold(id, entry) || strings.HasPrefix(strings.ToLower(id), strings.ToLower(entry)+"/") {
			return true
		}
	}
	return false
}

// CheckSource returns the finding for a source the list does not cover.
// It is a warning: --strict turns it into a rejection.
func CheckSource(id string, t Trust) []Finding {
	if t.Covers(id) {
		return nil
	}
	return []Finding{{Code: CodeUntrusted, Severity: Warn, Detail: domain.Msg("%s no está en trust: de skilus.yaml", id)}}
}

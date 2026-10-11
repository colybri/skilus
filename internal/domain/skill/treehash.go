package skill

import (
	"regexp"

	"github.com/colybri/skilus/internal/domain"
)

var treeHashRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TreeHash is the SHA-256 of a skill's canonical file tree, hex encoded.
type TreeHash struct {
	value string
}

// NewTreeHash validates s as 64 lowercase hex characters.
func NewTreeHash(s string) (TreeHash, error) {
	if !treeHashRe.MatchString(s) {
		return TreeHash{}, domain.Errorf("el hash de árbol %q no tiene 64 caracteres hexadecimales en minúscula: %w", s, domain.ErrInvalid)
	}
	return TreeHash{value: s}, nil
}

func (h TreeHash) String() string { return h.value }

// Short returns the first 12 characters, for display.
func (h TreeHash) Short() string {
	if len(h.value) < 12 {
		return h.value
	}
	return h.value[:12]
}

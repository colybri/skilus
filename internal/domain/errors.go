// Package domain holds the error kinds shared by every aggregate. The
// subpackages (skill, agent, lock, ...) are the model itself and import only
// the standard library and this package.
package domain

import "errors"

// Error kinds. Domain code wraps them with context using fmt.Errorf("...: %w").
// Only the cli package translates them into messages and exit codes.
var (
	ErrInvalid       = errors.New("invalid value")
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrConflict      = errors.New("conflict")
)

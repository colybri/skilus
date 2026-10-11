// Package domain holds the error kinds shared by every aggregate. The
// subpackages (skill, agent, lock, ...) are the model itself and import only
// the standard library and this package.
package domain

// Error kinds. Code wraps them with context using Errorf("...: %w").
// Only the cli package translates them into messages and exit codes.
var (
	ErrInvalid       = NewError("valor no válido")
	ErrNotFound      = NewError("no encontrado")
	ErrAlreadyExists = NewError("ya existe")
	ErrConflict      = NewError("conflicto")
)

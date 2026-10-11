package domain_test

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/colybri/skilus/internal/domain"
)

func TestTextString(t *testing.T) {
	if got := domain.Msg("sin argumentos %d").String(); got != "sin argumentos %d" {
		t.Errorf("String() without args = %q, want the format untouched", got)
	}
	if got := domain.Msg("la skill %s falta en %v", "pdf", domain.Msg("el perfil %s", "dev")).String(); got != "la skill pdf falta en el perfil dev" {
		t.Errorf("String() = %q", got)
	}
}

func TestErrorfWrapsLikeFmt(t *testing.T) {
	err := domain.Errorf("no se pudo leer %s: %w", "skilus.yaml", fs.ErrNotExist)
	if err.Error() != "no se pudo leer skilus.yaml: file does not exist" {
		t.Errorf("Error() = %q", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Error("errors.Is should see the wrapped error")
	}
	var m *domain.Message
	if !errors.As(err, &m) || m.Format != "no se pudo leer %s: %w" || len(m.Args) != 2 {
		t.Errorf("Message = %+v", m)
	}
}

func TestErrorfWrapsSeveralErrors(t *testing.T) {
	err := domain.Errorf("%w y %w", domain.ErrInvalid, domain.ErrConflict)
	if !errors.Is(err, domain.ErrInvalid) || !errors.Is(err, domain.ErrConflict) {
		t.Errorf("errors.Is should see both wrapped errors: %v", err)
	}
	var m *domain.Message
	errors.As(err, &m)
	if got := m.Unwrap(); len(got) != 2 {
		t.Errorf("Unwrap() = %v, want two errors", got)
	}
}

func TestMessageWithoutWrapUnwrapsNothing(t *testing.T) {
	var m *domain.Message
	errors.As(domain.Errorf("valor %d", 3), &m)
	if got := m.Unwrap(); got != nil {
		t.Errorf("Unwrap() = %v, want nil", got)
	}
	errors.As(domain.NewError("sin causa"), &m)
	if m.Error() != "sin causa" || m.Unwrap() != nil {
		t.Errorf("NewError = %q, %v", m.Error(), m.Unwrap())
	}
}

func TestSentinelsAreDistinct(t *testing.T) {
	sentinels := []error{domain.ErrInvalid, domain.ErrNotFound, domain.ErrAlreadyExists, domain.ErrConflict}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if (i == j) != errors.Is(a, b) {
				t.Errorf("errors.Is(%v, %v) = %v", a, b, i != j)
			}
		}
	}
}

func TestErrorsJoinsAndUnwraps(t *testing.T) {
	errs := domain.Errors{domain.ErrInvalid, domain.Errorf("la skill %s no está firmada", "pdf")}
	if errs.Error() != "valor no válido; la skill pdf no está firmada" {
		t.Errorf("Error() = %q", errs.Error())
	}
	if !errors.Is(errs, domain.ErrInvalid) {
		t.Error("errors.Is should see a listed error")
	}
	if domain.Errors(nil).Error() != "" {
		t.Error("an empty list should print nothing")
	}
}

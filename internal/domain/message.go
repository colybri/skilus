package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Text is a message the CLI translates before printing it. Format is the
// message id, written in Spanish like the CLI's own texts, and Args fill it
// as in fmt.Sprintf. Args that are errors or Texts are translated too.
type Text struct {
	Format string
	Args   []any
}

// Msg returns a Text.
func Msg(format string, args ...any) Text { return Text{Format: format, Args: args} }

// String formats the text in Spanish.
func (t Text) String() string {
	if len(t.Args) == 0 {
		return t.Format
	}
	return fmt.Errorf(t.Format, t.Args...).Error()
}

// Message is an error whose text the CLI translates. It wraps the errors
// its format consumes with %w, like fmt.Errorf.
type Message struct {
	Text
	err error
}

// Errorf is fmt.Errorf for errors the user may read: every error built in
// domain, app and the adapters goes through it or NewError so the CLI can
// print it in the user's language.
func Errorf(format string, args ...any) error {
	return &Message{Text: Msg(format, args...), err: fmt.Errorf(format, args...)}
}

// NewError is errors.New for sentinel errors the user may read.
func NewError(text string) error {
	return &Message{Text: Msg(text), err: errors.New(text)}
}

func (m *Message) Error() string { return m.err.Error() }

// Unwrap returns the errors wrapped with %w.
func (m *Message) Unwrap() []error {
	switch u := m.err.(type) { //nolint:errorlint // asks fmt.Errorf's result what it wraps
	case interface{ Unwrap() []error }:
		return u.Unwrap()
	case interface{ Unwrap() error }:
		if w := u.Unwrap(); w != nil {
			return []error{w}
		}
	}
	return nil
}

// Errors lists several errors in one, separated by "; ", such as the
// reasons an install was rejected.
type Errors []error

func (e Errors) Error() string {
	parts := make([]string, len(e))
	for i, err := range e {
		parts[i] = err.Error()
	}
	return strings.Join(parts, "; ")
}

// Unwrap returns the listed errors.
func (e Errors) Unwrap() []error { return e }

package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain"
)

// localize renders err in the catalog's language. Messages are translated,
// and so are the errors and texts they carry, as are Cobra's and pflag's
// errors; other errors from the system or a library keep their own text.
func localize(err error, t *i18n.Catalog) string {
	switch e := err.(type) { //nolint:errorlint // each level of the chain is rendered on its own
	case *domain.Message:
		return text(e.Text, t)
	case domain.Errors:
		parts := make([]string, len(e))
		for i, err := range e {
			parts[i] = localize(err, t)
		}
		return strings.Join(parts, "; ")
	case reported:
		return localize(e.error, t)
	}
	return libraryMessage(err.Error(), t)
}

// wrapVerb matches %w, also with an argument index such as %[2]w: once
// the wrapped errors are rendered as strings it formats like %v.
var wrapVerb = regexp.MustCompile(`%(\[\d+\])?w`)

// text renders a domain text in the catalog's language.
func text(x domain.Text, t *i18n.Catalog) string {
	if len(x.Args) == 0 {
		return t.Translate(x.Format)
	}
	args := make([]any, len(x.Args))
	for i, a := range x.Args {
		switch v := a.(type) {
		case error:
			args[i] = localize(v, t)
		case domain.Text:
			args[i] = text(v, t)
		default:
			args[i] = a
		}
	}
	return fmt.Sprintf(wrapVerb.ReplaceAllString(t.Translate(x.Format), "%${1}v"), args...)
}

// libraryMessages translates the errors Cobra and pflag return for a bad
// command line, which only speak English.
var libraryMessages = []struct {
	re *regexp.Regexp
	tr func(t *i18n.Catalog, m []string) string
}{
	{regexp.MustCompile(`^unknown command (".*") for (".*?")(?s:\n\nDid you mean this\?\n(.*))?$`), func(t *i18n.Catalog, m []string) string {
		s := t.T("comando desconocido %s para %s", m[1], m[2])
		if m[3] != "" {
			s += "\n\n" + t.T("¿Querías decir esto?") + "\n" + m[3]
		}
		return s
	}},
	{regexp.MustCompile(`^requires at least (\d+) arg\(s\), only received (\d+)$`), func(t *i18n.Catalog, m []string) string {
		return t.T("necesita al menos %s argumento(s) y ha recibido %s", m[1], m[2])
	}},
	{regexp.MustCompile(`^accepts at most (\d+) arg\(s\), received (\d+)$`), func(t *i18n.Catalog, m []string) string {
		return t.T("acepta como mucho %s argumento(s) y ha recibido %s", m[1], m[2])
	}},
	{regexp.MustCompile(`^accepts (\d+) arg\(s\), received (\d+)$`), func(t *i18n.Catalog, m []string) string {
		return t.T("acepta %s argumento(s) y ha recibido %s", m[1], m[2])
	}},
	{regexp.MustCompile(`^unknown flag: (--.*)$`), func(t *i18n.Catalog, m []string) string {
		return t.T("opción desconocida: %s", m[1])
	}},
	{regexp.MustCompile(`^unknown shorthand flag: ('.*') in (-.*)$`), func(t *i18n.Catalog, m []string) string {
		return t.T("opción corta desconocida: %s en %s", m[1], m[2])
	}},
	{regexp.MustCompile(`^flag needs an argument: (.*)$`), func(t *i18n.Catalog, m []string) string {
		return t.T("la opción necesita un valor: %s", m[1])
	}},
	{regexp.MustCompile(`^invalid argument (".*") for (".*") flag: (.*)$`), func(t *i18n.Catalog, m []string) string {
		return t.T("valor %s no válido para la opción %s: %s", m[1], m[2], m[3])
	}},
	{regexp.MustCompile(`^bad flag syntax: (.*)$`), func(t *i18n.Catalog, m []string) string {
		return t.T("sintaxis de opción incorrecta: %s", m[1])
	}},
}

func libraryMessage(msg string, t *i18n.Catalog) string {
	for _, lm := range libraryMessages {
		if m := lm.re.FindStringSubmatch(msg); m != nil {
			return lm.tr(t, m)
		}
	}
	return msg
}

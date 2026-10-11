// Package i18n translates the texts the CLI prints. The Spanish text is the
// message id: it is printed as is in Spanish and looked up in the embedded
// catalogs for every other language.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Source is the language the message ids are written in.
const Source = "es"

// Fallback is used when no setting or environment variable names a
// supported language.
const Fallback = "en"

// Language is one supported language.
type Language struct {
	Code string // ISO 639-1
	Name string // in the language itself
}

// Languages lists the supported languages: the same as the docs site.
var Languages = []Language{
	{"es", "Español"},
	{"en", "English"},
	{"fr", "Français"},
	{"de", "Deutsch"},
	{"pt", "Português"},
	{"zh", "中文"},
	{"ja", "日本語"},
	{"id", "Bahasa Indonesia"},
	{"ar", "العربية"},
	{"ru", "Русский"},
	{"pl", "Polski"},
	{"ur", "اردو"},
	{"hi", "हिन्दी"},
}

//go:embed locales/*.json
var locales embed.FS

// Catalog translates message ids into one language.
type Catalog struct {
	lang     string
	messages map[string]string
}

// Load returns the catalog for a supported language code.
func Load(code string) (*Catalog, error) {
	c := &Catalog{lang: code}
	if code == Source {
		return c, nil
	}
	if _, ok := Lookup(code); !ok {
		return nil, fmt.Errorf("unsupported language %q", code)
	}
	raw, err := locales.ReadFile("locales/" + code + ".json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &c.messages); err != nil {
		return nil, fmt.Errorf("locales/%s.json: %w", code, err)
	}
	return c, nil
}

// Lang returns the catalog's language code.
func (c *Catalog) Lang() string { return c.lang }

// T translates msgid and, with args, formats it like fmt.Sprintf. An id the
// catalog lacks is printed in Spanish rather than not at all.
func (c *Catalog) T(msgid string, args ...any) string {
	s := msgid
	if c != nil {
		if tr, ok := c.messages[msgid]; ok && tr != "" {
			s = tr
		}
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Translate returns the translation of a message id built outside the CLI,
// such as an error's format, without formatting it.
func (c *Catalog) Translate(msgid string) string { return c.T(msgid) }

// Lookup returns the supported language for code.
func Lookup(code string) (Language, bool) {
	for _, l := range Languages {
		if l.Code == code {
			return l, true
		}
	}
	return Language{}, false
}

// Match returns the supported language a locale names, such as es_ES.UTF-8,
// pt-BR or zh_CN, ignoring the region, the encoding and the modifier.
func Match(locale string) (string, bool) {
	s := strings.TrimSpace(locale)
	if i := strings.IndexAny(s, ".@"); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexAny(s, "_-"); i >= 0 {
		s = s[:i]
	}
	s = strings.ToLower(s)
	if _, ok := Lookup(s); ok {
		return s, true
	}
	return "", false
}

// Origin says where the language in use came from.
type Origin string

// Origins of the language in use.
const (
	FromFlag     Origin = "SKILUS_LANG"
	FromSetting  Origin = "setting"
	FromEnv      Origin = "env"
	FromSystem   Origin = "system"
	FromFallback Origin = "fallback"
)

// Choice is the language in use and where it came from.
type Choice struct {
	Code   string
	Origin Origin
	Detail string // the variable or value that decided it
}

// Resolve picks the language: SKILUS_LANG, then the saved setting, then the
// usual locale variables in gettext's order, then the system locale (set
// on Windows, where the variables are rarely set), then Fallback.
func Resolve(setting string, getenv func(string) string, system string) Choice {
	if code, ok := Match(getenv("SKILUS_LANG")); ok {
		return Choice{Code: code, Origin: FromFlag, Detail: "SKILUS_LANG"}
	}
	if code, ok := Match(setting); ok {
		return Choice{Code: code, Origin: FromSetting}
	}
	for _, name := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := getenv(name)
		if v == "" {
			continue
		}
		for _, part := range strings.Split(v, ":") {
			if code, ok := Match(part); ok {
				return Choice{Code: code, Origin: FromEnv, Detail: name + "=" + v}
			}
		}
		if name != "LANGUAGE" {
			// A set LC_ALL, LC_MESSAGES or LANG is the user's choice, even
			// when skilus does not speak it.
			break
		}
	}
	if code, ok := Match(system); ok {
		return Choice{Code: code, Origin: FromSystem, Detail: system}
	}
	return Choice{Code: Fallback, Origin: FromFallback}
}

// Has reports whether the catalog translates msgid.
func (c *Catalog) Has(msgid string) bool {
	if c.lang == Source {
		return true
	}
	_, ok := c.messages[msgid]
	return ok
}

// Unused returns the catalog's ids missing from ids, sorted.
func (c *Catalog) Unused(ids []string) []string {
	known := map[string]bool{}
	for _, id := range ids {
		known[id] = true
	}
	var out []string
	for id := range c.messages {
		if !known[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

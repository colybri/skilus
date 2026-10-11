package i18n

import "testing"

func TestMatch(t *testing.T) {
	for in, want := range map[string]string{
		"es_ES.UTF-8": "es", "pt-BR": "pt", "zh_CN.utf8": "zh", "EN": "en",
		"de_DE@euro": "de", "ja": "ja", "C": "", "POSIX": "", "it_IT.UTF-8": "", "": "",
	} {
		got, ok := Match(in)
		if got != want || ok != (want != "") {
			t.Errorf("Match(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestResolveOrder(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	tests := []struct {
		name    string
		setting string
		vars    map[string]string
		system  string
		want    Choice
	}{
		{"nothing", "", nil, "", Choice{Code: "en", Origin: FromFallback}},
		{"lang", "", map[string]string{"LANG": "fr_FR.UTF-8"}, "", Choice{Code: "fr", Origin: FromEnv, Detail: "LANG=fr_FR.UTF-8"}},
		{"lc_all beats lang", "", map[string]string{"LC_ALL": "de_DE.UTF-8", "LANG": "fr_FR.UTF-8"}, "", Choice{Code: "de", Origin: FromEnv, Detail: "LC_ALL=de_DE.UTF-8"}},
		{"language list", "", map[string]string{"LANGUAGE": "it:pt_BR", "LANG": "fr_FR"}, "", Choice{Code: "pt", Origin: FromEnv, Detail: "LANGUAGE=it:pt_BR"}},
		{"unsupported lc_all wins over lang", "", map[string]string{"LC_ALL": "C", "LANG": "fr_FR"}, "", Choice{Code: "en", Origin: FromFallback}},
		{"setting beats env", "ja", map[string]string{"LANG": "fr_FR"}, "", Choice{Code: "ja", Origin: FromSetting}},
		{"SKILUS_LANG beats setting", "ja", map[string]string{"SKILUS_LANG": "ru"}, "", Choice{Code: "ru", Origin: FromFlag, Detail: "SKILUS_LANG"}},
		{"windows locale", "", nil, "pl-PL", Choice{Code: "pl", Origin: FromSystem, Detail: "pl-PL"}},
	}
	for _, tt := range tests {
		if got := Resolve(tt.setting, env(tt.vars), tt.system); got != tt.want {
			t.Errorf("%s: Resolve = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestSpanishIsTheSource(t *testing.T) {
	c, err := Load("es")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.T("Instalada %s en %s\n", "pdf", "/x"); got != "Instalada pdf en /x\n" {
		t.Fatalf("T = %q", got)
	}
	if _, err := Load("it"); err == nil {
		t.Fatal("Load(it) should fail")
	}
}

func TestEveryLanguageLoads(t *testing.T) {
	for _, l := range Languages {
		c, err := Load(l.Code)
		if err != nil {
			t.Errorf("Load(%s): %v", l.Code, err)
			continue
		}
		if c.Lang() != l.Code {
			t.Errorf("Lang() = %q, want %q", c.Lang(), l.Code)
		}
	}
}

func TestTranslate(t *testing.T) {
	c := &Catalog{lang: "en", messages: map[string]string{
		"Instalada %s en %s\n": "Installed %s in %s\n",
		"vacía":                "",
	}}
	if got := c.T("Instalada %s en %s\n", "pdf", "/x"); got != "Installed pdf in /x\n" {
		t.Errorf("T = %q", got)
	}
	if got := c.Translate("Instalada %s en %s\n"); got != "Installed %s in %s\n" {
		t.Errorf("Translate should not format: %q", got)
	}
	if got := c.T("sin traducir"); got != "sin traducir" {
		t.Errorf("a missing id should print in Spanish: %q", got)
	}
	if got := c.T("vacía"); got != "vacía" {
		t.Errorf("an empty translation should print in Spanish: %q", got)
	}
	var none *Catalog
	if got := none.T("hola %s", "Mario"); got != "hola Mario" {
		t.Errorf("a nil catalog should print in Spanish: %q", got)
	}
}

func TestHasAndUnused(t *testing.T) {
	c := &Catalog{lang: "en", messages: map[string]string{"b": "B", "a": "A", "c": "C"}}
	if !c.Has("a") || c.Has("z") {
		t.Errorf("Has(a), Has(z) = %v, %v", c.Has("a"), c.Has("z"))
	}
	es, _ := Load(Source)
	if !es.Has("cualquier texto") {
		t.Error("Spanish has every id")
	}
	got := c.Unused([]string{"b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("Unused = %v, want [a c]", got)
	}
}

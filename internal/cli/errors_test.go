package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/colybri/skilus/internal/cli/i18n"
	"github.com/colybri/skilus/internal/domain"
)

func TestLocalizeTranslatesTheWholeChain(t *testing.T) {
	en, err := i18n.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	const agentGone = "el agente %s ya no está en el catálogo"
	inner := domain.Errorf(agentGone, "codex")
	e := domain.Errorf("%s: %w", "skilus.yaml", domain.Errorf("%w: %w", domain.Errors{inner, inner}, domain.ErrNotFound))

	want := fmt.Sprintf("skilus.yaml: %[1]s; %[1]s: %[2]s", fmt.Sprintf(en.Translate(agentGone), "codex"), en.Translate("no encontrado"))
	if got := localize(e, en); got != want {
		t.Errorf("localize = %q, want %q", got, want)
	}
	if got := localize(e, en); got == e.Error() {
		t.Errorf("not translated: %q", got)
	}
	if !errors.Is(e, domain.ErrNotFound) {
		t.Error("the chain lost its kind")
	}
	es, _ := i18n.Load(i18n.Source)
	if got := localize(e, es); got != e.Error() {
		t.Errorf("Spanish = %q, want the message ids: %q", got, e.Error())
	}
}

func TestLocalizeTranslatesCobraErrors(t *testing.T) {
	es, _ := i18n.Load(i18n.Source)
	cases := map[string]string{
		"accepts 1 arg(s), received 0":                                            "acepta 1 argumento(s) y ha recibido 0",
		"requires at least 1 arg(s), only received 0":                             "necesita al menos 1 argumento(s) y ha recibido 0",
		"unknown flag: --bogus":                                                   "opción desconocida: --bogus",
		"unknown command \"lsit\" for \"skilus\"\n\nDid you mean this?\n\tlist\n": "comando desconocido \"lsit\" para \"skilus\"\n\n¿Querías decir esto?\n\tlist\n",
		"open x: no such file or directory":                                       "open x: no such file or directory",
	}
	for in, want := range cases {
		if got := localize(errors.New(in), es); got != want {
			t.Errorf("localize(%q) = %q, want %q", in, got, want)
		}
	}
}

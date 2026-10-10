package main

import (
	"os"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain lets testscript run the real binary entry point, so the scripts
// exercise the same wiring users get.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"skilus": func() { os.Exit(run()) },
	})
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		// The scripts check the Spanish texts; lang.txtar unsets it.
		Setup: func(env *testscript.Env) error {
			env.Setenv("SKILUS_LANG", "es")
			return nil
		},
	})
}

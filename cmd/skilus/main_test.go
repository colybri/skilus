package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain lets testscript run the real binary entry point, so the scripts
// exercise the same wiring users get.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"skilus":   func() { os.Exit(run()) },
		"fakeedit": fakeEdit,
	})
}

// fakeEdit stands in for the user's editor: fakeedit SRC... FILE. The n-th
// time it runs it copies the n-th SRC (or the last one) over FILE, after
// saving what FILE held to $WORK/shown-n. A SRC named KEEP leaves FILE as
// it is and one named FAIL exits with 1.
func fakeEdit() {
	args := os.Args[1:]
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: fakeedit SRC... FILE")
		os.Exit(2)
	}
	srcs, file := args[:len(args)-1], args[len(args)-1]
	counter := filepath.Join(os.Getenv("WORK"), ".fakeedit")
	raw, _ := os.ReadFile(counter)
	n, _ := strconv.Atoi(string(raw))
	n++
	if err := os.WriteFile(counter, []byte(strconv.Itoa(n)), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	shown, err := os.ReadFile(file)
	if err == nil {
		err = os.WriteFile(filepath.Join(os.Getenv("WORK"), "shown-"+strconv.Itoa(n)), shown, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	src := srcs[min(n, len(srcs))-1]
	switch filepath.Base(src) {
	case "KEEP":
		return
	case "FAIL":
		os.Exit(1)
	}
	data, err := os.ReadFile(src)
	if err == nil {
		err = os.WriteFile(file, data, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
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

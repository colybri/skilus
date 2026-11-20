package main

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain lets testscript run the real binary entry point, so the scripts
// exercise the same wiring users get.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"skilus":   func() { os.Exit(run()) },
		"fakeedit": fakeEdit,
		"mktargz":  mkTarGz,
	})
}

// fakeEdit stands in for the user's editor: fakeedit SRC... FILE. The n-th
// time it runs it copies the n-th SRC (or the last one) over FILE, after
// saving what FILE held to $WORK/shown-n. A SRC named KEEP leaves FILE as
// it is and one named FAIL exits with 1. $WORK and $WORKURL are expanded
// in SRC's content too, so a test can write the paths of its work directory.
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
	// testscript does not expand variables inside quotes and Windows runs
	// the editor without a shell, so $WORK is expanded here.
	src := os.ExpandEnv(srcs[min(n, len(srcs))-1])
	switch filepath.Base(src) {
	case "KEEP":
		return
	case "FAIL":
		os.Exit(1)
	}
	data, err := os.ReadFile(src)
	if err == nil {
		r := strings.NewReplacer("$WORKURL", os.Getenv("WORKURL"), "$WORK", os.Getenv("WORK"))
		data = []byte(r.Replace(string(data)))
		err = os.WriteFile(file, data, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// mkTarGz writes a tar.gz with the entries given, in order, so scripts can
// build archives no file system would hold, on every system:
// mktargz OUT ENTRY..., where ENTRY is PATH=CONTENT for a file or
// PATH->TARGET for a symlink.
func mkTarGz() {
	args := os.Args[1:]
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mktargz OUT ENTRY...")
		os.Exit(2)
	}
	out, err := os.Create(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	for _, e := range args[1:] {
		var h tar.Header
		var data string
		if name, target, ok := strings.Cut(e, "->"); ok {
			h = tar.Header{Typeflag: tar.TypeSymlink, Name: name, Linkname: target, Mode: 0o777}
		} else {
			name, content, _ := strings.Cut(e, "=")
			data = strings.ReplaceAll(content, `\n`, "\n")
			h = tar.Header{Typeflag: tar.TypeReg, Name: name, Size: int64(len(data)), Mode: 0o644}
		}
		err := tw.WriteHeader(&h)
		if err == nil {
			_, err = tw.Write([]byte(data))
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	for _, c := range []interface{ Close() error }{tw, gz, out} {
		if err := c.Close(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

// workURL is the file URL of dir.
func workURL(dir string) string {
	p := filepath.ToSlash(dir)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		// The scripts check the Spanish texts; lang.txtar unsets it.
		Setup: func(env *testscript.Env) error {
			env.Setenv("SKILUS_LANG", "es")
			// file://$WORK is no URL on Windows (file://C:\...), so the
			// scripts use $WORKURL: file:///tmp/x or file:///C:/x.
			env.Setenv("WORKURL", workURL(env.WorkDir))
			return nil
		},
	})
}

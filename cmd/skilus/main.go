// Command skilus is the composition root: it builds the adapters, injects
// them into the use cases and hands those to the CLI.
package main

import (
	"fmt"
	"os"

	"github.com/colybri/skilus/internal/adapter/catalog"
	"github.com/colybri/skilus/internal/adapter/osfs"
	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "skilus: cannot find the home directory:", err)
		return cli.ExitError
	}
	deps := cli.Deps{
		Version: version,
		ListAgents: app.ListAgents{
			Catalog:  catalog.New(home, os.Getenv),
			Detector: osfs.Detector{},
		},
	}
	return cli.Run(deps, os.Args[1:], os.Stdout, os.Stderr)
}

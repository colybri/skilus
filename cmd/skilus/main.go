// Command skilus is the composition root: it builds the adapters, injects
// them into the use cases and hands those to the CLI.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/colybri/skilus/internal/adapter/archivesrc"
	"github.com/colybri/skilus/internal/adapter/catalog"
	"github.com/colybri/skilus/internal/adapter/gitsrc"
	"github.com/colybri/skilus/internal/adapter/osfs"
	"github.com/colybri/skilus/internal/adapter/yamlrepo"
	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/cli"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/source"
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
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "skilus: cannot read the working directory:", err)
		return cli.ExitError
	}
	skilusHome := filepath.Join(home, ".skilus")

	agents := catalog.New(home, os.Getenv)
	detector := osfs.Detector{}
	repo := yamlrepo.Repo{ProjectRoot: cwd, GlobalDir: skilusHome}

	// Project installs are copies so they can be committed; global ones
	// link to the store. Windows needs privileges for symlinks.
	globalMode := agent.ModeSymlink
	if runtime.GOOS == "windows" {
		globalMode = agent.ModeCopy
	}

	git := gitsrc.Fetcher{MaxSkillBytes: policy.DefaultLimits.MaxTotalBytes}
	fetchers := map[source.Kind]app.Fetcher{
		source.KindLocal:   osfs.LocalFetcher{Dir: cwd},
		source.KindGit:     git,
		source.KindArchive: archivesrc.Fetcher{MaxUnpacked: policy.DefaultLimits.MaxTotalBytes * 4},
	}
	store := osfs.Store{Root: filepath.Join(skilusHome, "store")}

	deps := cli.Deps{
		Version:    version,
		ListAgents: app.ListAgents{Catalog: agents, Detector: detector},
		AddSkill: app.AddSkillHandler{
			Catalog:      agents,
			Detector:     detector,
			Fetchers:     fetchers,
			Store:        store,
			Deployer:     osfs.Deployer{},
			Locks:        repo,
			Manifests:    repo,
			Trust:        repo,
			ProjectRoot:  cwd,
			DefaultModes: map[agent.Scope]agent.Mode{agent.ScopeProject: agent.ModeCopy, agent.ScopeGlobal: globalMode},
			Limits:       policy.DefaultLimits,
		},
		ListSkills: app.ListSkills{Locks: repo},
		RemoveSkill: app.RemoveSkillHandler{
			Catalog:     agents,
			Deployer:    osfs.Deployer{},
			Locks:       repo,
			Manifests:   repo,
			ProjectRoot: cwd,
		},
		Inspect:  app.InspectHandler{Fetchers: fetchers, Trust: repo, Limits: policy.DefaultLimits},
		Outdated: app.OutdatedHandler{Resolver: git, Locks: repo},
		Update: app.UpdateHandler{
			Catalog:     agents,
			Fetchers:    fetchers,
			Store:       store,
			Deployer:    osfs.Deployer{},
			Trees:       osfs.TreeReader{},
			Locks:       repo,
			Trust:       repo,
			ProjectRoot: cwd,
			Limits:      policy.DefaultLimits,
		},
		Verify: app.VerifyHandler{Catalog: agents, Store: store, Trees: osfs.TreeReader{}, Locks: repo, ProjectRoot: cwd},
		Sync: app.SyncHandler{
			Catalog:     agents,
			Fetchers:    fetchers,
			Store:       store,
			Deployer:    osfs.Deployer{},
			Trees:       osfs.TreeReader{},
			Locks:       repo,
			ProjectRoot: cwd,
		},
	}
	return cli.Run(deps, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}

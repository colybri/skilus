# skilus

A skill manager (`SKILL.md`) for AI agents, shipped as a single Go binary. It installs skills from Git, a URL or a local folder into 74 agents (Claude Code, Codex, Cursor, GitHub Copilot, Gemini CLI, Windsurf, OpenCode, Cline…) and makes sure that what gets installed is exactly what was reviewed: every skill is pinned to a commit, content-hashed and inspected before install.

This README is also available in [Spanish](README.es.md).

> Status: pre-release (0.x); the `skilus.yaml` and `skilus.lock` formats may still change. Available commands: `skilus agents`, `skilus add` (from a local folder, a Git repository or a ZIP or tar.gz archive), `skilus list`, `skilus remove`, `skilus edit`, `skilus inspect`, `skilus verify`, `skilus sync`, `skilus update`, `skilus profile`, `skilus search`, `skilus audit` and `skilus lang`.

## Why skilus

- **Secure.** A skill is instructions and scripts your agent runs with your permissions. skilus inspects every skill before installing it, pins it to a commit and a content hash, and later checks that nobody has changed it.
- **Built for teams.** Everyone can use the agent they prefer; skilus installs the same skills in all of them. Commit `skilus.yaml` and `skilus.lock` and `skilus sync` rebuilds the same setup on any machine, like `npm ci`.
- **Profiles.** Group skills by task or project and switch from one set to another with a single command.
- **In your language.** Messages, prompts, help and errors are available in 13 languages.
- **Easy to use.** Interactive selectors to pick skills and agents, a plan shown before every change, and no tokens stored: Git sources use your system `git` and its credentials.

## Installation

Download the archive for your system from [Releases](https://github.com/colybri/skilus/releases) (Linux, macOS and Windows, amd64 and arm64) and put `skilus` on your `PATH`. You need `git` installed for Git sources.

Every release publishes a `checksums.txt` signed keylessly with [cosign](https://github.com/sigstore/cosign) (the workflow's OIDC identity) and an SBOM per archive. To verify a download:

```sh
cosign verify-blob checksums.txt \
  --certificate checksums.txt.pem --signature checksums.txt.sig \
  --certificate-identity-regexp '^https://github.com/colybri/skilus/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum --check --ignore-missing checksums.txt
```

## Quick start

```sh
skilus inspect anthropics/skills --skill pdf   # review it without installing anything
skilus add anthropics/skills --skill pdf       # show the plan, ask and install
skilus list                                    # what is installed, with commit, hash and agents
skilus verify                                  # check that nothing changed since
```

## Usage

```sh
skilus agents          # agents detected on your machine and their paths
skilus agents --all    # every supported agent, with a column saying which are installed
skilus agents --json

skilus add anthropics/skills --skill pdf     # GitHub, default branch
skilus add github.com/o/r@v1.2.0            # a tag, branch or commit
skilus add git@gitlab.com:g/r.git           # any Git URL (https, ssh, file)
skilus add https://example.com/skills.zip   # a ZIP or tar.gz over https
skilus add ./my-skills                      # a local folder: starts with ./, ../ or /
                                            # in a terminal, you pick the skills and the agents
skilus add ./my-skills --skill review -y    # without asking
skilus add ./my-skills --agent claude-code --scope global

skilus list                                 # installed skills with commit, hash and agents
skilus list --scope global --json
skilus remove review                        # removes it from the agents, the lock and skilus.yaml,
                                            # with the dependencies nothing needs any more
skilus edit                                 # opens skilus.yaml in your editor and only saves it if valid
skilus edit --scope global                  # the global one, in ~/.skilus/skilus.yaml
skilus edit --path                          # only prints where it is

skilus inspect anthropics/skills --skill pdf # what add would check, without installing anything
skilus inspect ./my-skills --strict --json  # for CI: exit code 5 if add would reject it
skilus inspect ./my-skills --full           # with the full descriptions

skilus verify                               # checks that the installed skills and skilus.yaml match the lock
skilus verify --json
skilus sync                                 # makes the agents match the lock (like npm ci)
skilus sync --force                         # also overwrites files changed by hand

skilus update --check                       # which branches or tags point to a new commit, changing nothing
skilus update                               # applies skilus.yaml, shows what changes, asks and updates
skilus update review --yes --allow-scripts

skilus profile create web --skill debug,ui --agent codex  # declares a profile in skilus.yaml
skilus profile add web review               # adds skills (or --agent) to a profile
skilus profile remove web ui --agent codex  # removes them
skilus profile delete web                   # deletes the profile; installed skills do not change
skilus profile list                         # profiles in skilus.yaml; * marks the active one
skilus profile use web                      # deploys the profile and removes what does not belong to it
skilus profile use web --dry-run            # only shows the plan

skilus search pdf                           # searches skills.sh and your indexes
skilus search pdf --owner anthropics --json

skilus audit                                # trust, signatures and inspection of what is installed
skilus audit --strict --json                # for CI: exit code 5 on warnings

skilus lang                                 # language in use, where it comes from and the available ones
skilus lang set en                          # changes skilus's language for your user
skilus lang set auto                        # follows the system again
```

## How it works

### Files and scopes

skilus works in two scopes, each with its own `skilus.yaml` (the intent: which skills you asked for, their source and ref, profiles, `trust:` and `indexes:`) and `skilus.lock` (the result: the exact content installed, its commit, hash and agents).

- **Project** (the default): `skilus.yaml` and `skilus.lock` live in the folder where you run skilus, usually the root of your repository. Skills are installed in each agent's project folder, such as `.claude/skills`, as copies you can commit and share with your team.
- **Global** (`--scope global`): both files live in `~/.skilus/` (`C:\Users\<user>\.skilus` on Windows). Skills are installed in each agent's user folder, such as `~/.claude/skills`, as symlinks to the store (copies on Windows), and are available in all your projects.

The content is stored in `~/.skilus/store/<sha256>`, shared by every project. By default `sync` works on the project and `verify` on both scopes; both accept `--scope project|global|all`.

`skilus edit` opens `skilus.yaml` in the editor from `SKILUS_EDITOR`, `VISUAL`, `EDITOR` or git's `core.editor` (otherwise `notepad` on Windows and `nano` or `vi` elsewhere; a graphical editor has to wait until you close the file, like `code --wait`). If the file does not exist, it starts from a commented template. When the editor closes, skilus checks the file like `verify` does and only saves it if it is valid; an unknown top-level key such as `skils:` is an error too. If there are errors it lists them and asks whether to edit again; otherwise it discards the changes and exits with code 2. Editing installs nothing: afterwards, `skilus verify` tells you what is left to apply.

### Sources

Git sources are fetched with the system `git`, so your credentials, SSH keys and helpers work; skilus stores no tokens and rejects URLs with passwords. The repository is read without a checkout (no filters or hooks run) and the skill is pinned to the exact commit in `skilus.lock`. Without `@ref`, skilus uses the default branch.

ZIP and tar.gz archives (also `.tgz`) are downloaded over https only and unpacked in memory, without writing to disk. Absolute paths, paths with `..` or `\`, and entries that are not files, folders or symlinks are rejected, and there are limits on the download size (50 MB), the unpacked size and the number of entries. If everything sits under a single top folder, as in GitHub's archives, it is stripped. Archives take no `@ref` or commit: the lock pins their content by its hash, and a URL with `?` is rejected because it often carries a token.

`skilus add` reads a single skill (with `SKILL.md` at the root) or a folder with skills in `skills/<name>/`, `skills/.curated/<name>/` (like openai/skills) or `<name>/`.

In a terminal, `skilus add` without `--skill` lets you mark which skills of the source to install and, without `--agent`, which of the agents installed on your machine get them. Agents that share a folder (in a project, Codex, Cursor, Gemini CLI and others use `.agents/skills`) appear as a single option and get a single copy. Arrows move, the space bar marks, → marks everything, ← nothing, typing filters and Esc cancels. With `-y`, or when input or output is not a terminal, it does not ask and uses every skill and every detected agent.

### Inspection and trust

Before installing, skilus inspects every skill. It blocks symlinks that point outside the skill, files or packages that are too large and control characters in the description. It warns about executable files, `curl … | sh` and credential access (`~/.ssh`, `.aws/credentials`…) in any text file, and about invisible text and escape sequences in Markdown. With `--strict` warnings block, and with `--yes` executables also need `--allow-scripts`.

In a terminal, the plan fits its width, trims each description to two lines (`--full` shows them whole) and uses colors: a green ✓ when there are no findings, a yellow ! for warnings and a red ✗ for blocks, with a summary before asking. `--no-color` or the `NO_COLOR` variable turns colors off, and they are never used in a pipe or with `--json`.

To limit where skills can come from, add `trust:` to `skilus.yaml` (the project's or the global one in `~/.skilus/`):

```yaml
trust:
  - github.com/anthropics          # the whole organization
  - github.com/obra/superpowers    # one repository
```

With the list, any other source gets an `untrusted-source` warning, which `--strict` turns into a rejection. Without a list nothing is checked, and local folders never are.

### Verify, sync and update

`skilus verify` recomputes the hash of every skill in every agent and compares it with the lock; if one is missing or a file changed, it names it and exits with code 6. It also checks `skilus.yaml`: that it can be read, that every skill under `skills:` is installed (except those the active profile leaves out), that every installed skill other than dependencies is under `skills:`, and that profiles only use declared skills. If the file has errors it exits with code 2, and if it does not match the lock, with code 6, suggesting the command that fixes it.

`skilus sync` installs what is missing from the store or, if it is not there, by downloading each skill at its pinned commit, and fails if the hash does not match the lock. It does not change `skilus.lock` or `skilus.yaml` and leaves skills changed by hand alone unless you pass `--force`.

`skilus update --check` asks `git ls-remote`, without downloading content, which commit each skill's branch or tag points to now; skills pinned to a commit are not checked. `skilus update` downloads that version, shows the added, deleted and modified files and the inspection of the new version (executables you already accepted are not asked about again) and, once you confirm, replaces the installed copies and updates the lock. `skilus.yaml` does not change, because it stores the branch or tag, not the commit.

`skilus update` also applies what you edited by hand in `skilus.yaml`: it installs in the detected agents the skills you add under `skills:`, removes the ones you take out (with the dependencies nothing needs any more) and downloads from the new source or ref the ones you change. With an active profile, it does not install the skills the profile leaves out. Until then, `skilus verify` warns about the difference and `skilus update --check` shows it.

### Profiles

A profile groups skills already declared under `skills:` in `skilus.yaml` and the agents they go to:

```yaml
profiles:
  backend:
    skills: [systematic-debugging, mcp-builder]
    agents: [claude-code]
  web:
    skills: [systematic-debugging, web-design-guidelines]
    agents: [claude-code, cursor]
```

You do not need to edit the YAML by hand: `skilus profile create web --skill systematic-debugging,web-design-guidelines` declares the profile (`--agent` sets its agents), `skilus profile add` and `skilus profile remove` add or remove skills and, with `--agent`, agents, and `skilus profile delete` deletes it. These commands only edit `profiles:`, keep the file's comments and order, and install or remove nothing; the skills must already be under `skills:` (add them first with `skilus add`) and the agents must be in the catalog. In a terminal, `skilus profile create` without `--skill` lets you mark which skills from `skills:` it includes and, without `--agent`, which detected agents it deploys to, with the same selector as `skilus add`; with `-y` or outside a terminal it takes every skill and every detected agent. The chosen agents are written into the profile: a profile without agents cannot be deployed. Deleting the last profile removes `profiles:` as well.

`skilus profile use web` installs the profile's missing skills from the source recorded under `skills:`, with the same inspection as `add` (`allow: [scripts]` in that entry counts as `--allow-scripts`). It moves the skills already installed to the profile's agents, reusing the content pinned in the lock, and removes from the agents and the lock the skills the profile does not include. It installs first and only then removes, and `skilus.yaml` does not change, so switching back to `backend` reinstalls exactly the same. The active profile is not stored: it is the one whose skills match the lock.

### Agents

`skilus agents` lists the agents detected on your machine with their paths, and `skilus agents --all` lists the 74 it knows. To add an agent skilus does not ship, or change the paths of one, create `~/.skilus/agents.yaml` in the format of the bundled catalog (`internal/adapter/catalog/agents.yaml`): an existing id replaces that agent and a new one is added at the end.

```yaml
version: 1
agents:
  - id: acme
    name: Acme Agent
    project_dir: .acme/skills
    global_dir: ~/.acme/skills
    detect: ~/.acme
```

Only the file in your home folder is read: a cloned repository cannot decide where skilus writes outside the project.

### Dependencies between skills

A skill can need others. It declares them in the frontmatter of its `SKILL.md`, under `metadata`, which the Agent Skills format reserves for tool-specific data and agents ignore:

```yaml
---
name: report
description: Builds PDF and Word reports
metadata:
  requires: pdf anthropics/skills#docx   # or a YAML list
---
```

Each entry is a name, looked up in the same source as the skill, or `source#name` for another source (not a local folder). `skilus add` installs the missing ones in the same agents, with the same inspection and `trust:` check, and shows them in the plan. In `skilus.lock` every skill records what it needs, and skills installed this way are marked as dependencies; `skilus.yaml` only records what you asked for. A requirement is met if a skill with that name is already installed, wherever it came from. `skilus remove` does not remove a skill another one needs unless you remove both, and removing the last skill that needed a dependency removes the dependency too. `skilus update` installs what the new version requires and `skilus profile use` deploys the dependencies of the profile's skills.

### Search

`skilus search` queries [skills.sh](https://skills.sh) and the indexes of recommended skills declared under `indexes:` in `skilus.yaml` (the project's and the global one). An index is a YAML file, over https or `file://`, that a team keeps with the skills it recommends:

```yaml
version: 1
skills:
  - name: pdf
    source: anthropics/skills@v1.0.0
    description: Reads and creates PDFs
```

Index results come first, and with a `trust:` list each one says whether its source is trusted. If a source does not answer, skilus warns and shows the rest; `--no-registry` keeps only the indexes and `SKILUS_REGISTRY_URL` points to another compatible server (https only). `search` installs nothing: it shows the `skilus inspect` command to review a result before running `add`.

### Audit

`skilus audit` checks every skill in `skilus.lock` with today's rules:

- Whether its source is in `trust:`.
- Whether the pinned commit is signed (GPG, SSH or x509, which includes Sigstore's gitsign) and whether the signature verifies. It first tries `git verify-commit`, that is, your keys, your `allowedSignersFile` and your `gpg.x509.program`. If that is not enough and the repository is on GitHub, it asks GitHub, which knows the keys its users register. Only the commit object is downloaded, and `GITHUB_TOKEN`, if set, is used only to raise the API rate limit.
- It also inspects the content in the store again, in case the heuristics changed since install. Executables you already accepted are not reported again.

An unsigned commit, or a ZIP or tar.gz archive, which never carries a signature, gets an `unsigned-commit` warning. With `--strict` warnings make the audit fail with exit code 5.

### Language

skilus writes in your system's language if it is one of the 13 it supports (Spanish, English, French, German, Portuguese, Chinese, Japanese, Indonesian, Arabic, Russian, Polish, Urdu and Hindi). It reads `LANGUAGE`, `LC_ALL`, `LC_MESSAGES` and `LANG`, in that order, and on Windows the user's language; if none of them is supported, it uses English. `skilus lang set <language>` saves it for your user in `~/.skilus/config.yaml` and `skilus lang set auto` clears it. The `SKILUS_LANG` variable overrides everything else, which is handy in CI. Messages, prompts, help and errors are translated, including command-line errors and finding details; the keys and status values of `--json`, the finding codes and text coming from git, the operating system or a library do not change.

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Error or cancelled |
| 2 | Invalid usage |
| 3 | Not found |
| 4 | Already installed or conflict |
| 5 | Rejected by the inspection |
| 6 | What is installed does not match the lock |

## Contributing

Contributions are welcome: bug reports, ideas, translations, documentation and code.

### Before you start

- **Report a bug or suggest a feature** by opening an [issue](https://github.com/colybri/skilus/issues). For bugs, include the command you ran, its output, your operating system and `skilus --version`.
- **Discuss larger changes first.** Open an issue before working on a new command, a change to the `skilus.yaml` or `skilus.lock` format or anything that touches several layers, so we can agree on the approach.
- **Contributor License Agreement.** skilus is also offered under a commercial license (see [COMMERCIAL.md](COMMERCIAL.md)), so every external contribution needs a signed CLA before it can be merged.

### Development setup

Requirements: Go 1.25 or later, `git` and [golangci-lint](https://golangci-lint.run/) v2.

```sh
git clone https://github.com/colybri/skilus.git
cd skilus
make test    # go test -race ./...
make lint    # golangci-lint, including the layer rules (depguard)
make build   # bin/skilus
go test ./cmd/skilus -run 'TestScripts/edit$'   # a single end-to-end script
```

### Architecture

The code follows a hexagonal (DDD) architecture adapted to idiomatic Go. Dependencies point inwards and `.golangci.yml` (depguard) enforces it:

```text
cmd/skilus/        composition root: wires the adapters
internal/domain/   model and value objects: standard library only, no I/O
internal/app/      use cases (one handler per command) and ports (ports.go)
internal/adapter/  port implementations (git, archives, file system, YAML, catalog…)
internal/cli/      Cobra commands: only talks to app, never imports adapters
```

There is no message bus, dependency-injection container or global state. `context.Context` is always the first argument, and `app` tests use in-memory fakes rather than generated mocks.

### Conventions

- **Errors.** Every error a user may read is created with `domain.Errorf`, `domain.NewError` or `domain.Msg` and wraps one of the sentinels in `internal/domain/errors.go`. Only `internal/cli/cli.go` turns them into exit codes. The linter rejects `fmt.Errorf` and `errors.New` outside the CLI, `cmd` and tests.
- **Translations.** User-facing text goes through `t.T("…")` with a Spanish literal, which is the message id. Every new id must be added to the 12 catalogs in `internal/cli/i18n/locales/*.json` (all languages except Spanish); `TestCatalogsTranslateEveryMessage` fails if one is missing or extra, or if the format verbs, tabs or trailing newline differ.
- **Code style.** Code comments and identifiers are in English; user-facing text is written in Spanish and translated. Format with `gofmt` and `goimports -local github.com/colybri/skilus`.
- **Tests.** Unit tests live next to the code (`*_test.go`). End-to-end tests are [testscript](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript) files in `cmd/skilus/testdata/script/*.txtar`; they must pass on Linux, macOS and Windows (set `HOME` and `USERPROFILE`, and match paths with `[/\\]`). Never disable or skip a test to make CI pass.
- **Documentation.** The website in `docs/` is a static page in 13 languages. To preview it, run `python3 -m http.server` in `docs/` and open `#/<language>/<page>`. If you change user-facing behavior, update the docs and both READMEs.

### Pull requests

1. Fork the repository and create a branch from `main`.
2. Make your change with tests.
3. Before opening the pull request, run `gofmt -l .`, `make lint`, `make test` and `GOOS=windows go build ./...`.
4. Open the pull request describing what changes for the user and why.

CI runs the tests and the linter on Linux, macOS and Windows. It also runs `test/gate/run.sh`, which installs real skills from public repositories, so it needs network access and usually does not work locally. If the lock format changes, regenerate its reference with `GATE_UPDATE=1 test/gate/run.sh bin/skilus`.

## License

[PolyForm Noncommercial 1.0.0](LICENSE). Commercial use requires a commercial license: see [COMMERCIAL.md](COMMERCIAL.md).

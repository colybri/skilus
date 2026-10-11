// Package gitsrc implements app.Fetcher for Git repositories with the git
// binary of the system, so the user's own credentials, SSH keys and
// credential helpers apply and skilus never handles a token.
//
// The repository is fetched into a temporary bare repository and read with
// ls-tree and cat-file: nothing is checked out, so no filter, hook or
// attribute runs, and file modes come from Git itself on every OS.
package gitsrc

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/colybri/skilus/internal/adapter/skillsrc"
	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
	"github.com/colybri/skilus/internal/domain/source"
)

var _ app.Fetcher = Fetcher{}

// Fetcher reads skills from a Git repository.
type Fetcher struct {
	// Git is the git binary; empty means "git" from PATH.
	Git string
	// MaxSkillBytes stops reading a skill whose blobs add up to more, so
	// a huge repository cannot exhaust memory before inspection runs.
	MaxSkillBytes int64
}

type entry struct {
	mode, kind, object string
	size               int64
	path               string
}

// Fetch implements app.Fetcher.
func (f Fetcher) Fetch(ctx context.Context, src source.Source) (app.Fetched, error) {
	dir, err := os.MkdirTemp("", "skilus-git-")
	if err != nil {
		return app.Fetched{}, err
	}
	defer os.RemoveAll(dir) //nolint:errcheck // temporary clone

	if _, err := f.git(ctx, dir, nil, "init", "--quiet", "--bare", dir); err != nil {
		return app.Fetched{}, err
	}
	ref := src.Ref
	if ref == "" {
		ref = "HEAD"
	}
	if _, err := f.git(ctx, dir, nil, "fetch", "--quiet", "--depth=1", "--no-tags", "--end-of-options", src.URL, ref); err != nil {
		if notFound(err) {
			return app.Fetched{}, domain.Errorf("descargar %s: %w: %w", src, err, domain.ErrNotFound)
		}
		return app.Fetched{}, domain.Errorf("descargar %s: %w", src, err)
	}
	commit, err := f.git(ctx, dir, nil, "rev-parse", "--verify", "--end-of-options", "FETCH_HEAD^{commit}")
	if err != nil {
		return app.Fetched{}, err
	}
	out := app.Fetched{Source: src.ID, Requested: src.Ref, Commit: strings.TrimSpace(string(commit))}

	entries, err := f.lsTree(ctx, dir, out.Commit)
	if err != nil {
		return app.Fetched{}, err
	}
	var candidates []string
	for _, e := range entries {
		if e.mode == "100644" && (e.path == skill.ManifestFile || strings.HasSuffix(e.path, "/"+skill.ManifestFile)) {
			candidates = append(candidates, e.path)
		}
	}
	for _, dirPath := range skillsrc.Discover(candidates) {
		files, err := f.readSkill(ctx, dir, dirPath, entries)
		if errors.Is(err, domain.ErrInvalid) {
			out.Invalid = append(out.Invalid, app.InvalidSkill{Path: dirPath, Err: err})
			continue
		}
		if err != nil {
			return app.Fetched{}, domain.Errorf("skill en %s: %w", dirPath, err)
		}
		p, err := skillsrc.Build(files)
		if err != nil {
			out.Invalid = append(out.Invalid, app.InvalidSkill{Path: dirPath, Err: err})
			continue
		}
		out.Skills = append(out.Skills, app.FetchedSkill{Path: dirPath, Package: p})
	}
	return out, nil
}

func (f Fetcher) lsTree(ctx context.Context, dir, commit string) ([]entry, error) {
	raw, err := f.git(ctx, dir, nil, "ls-tree", "-r", "-l", "-z", "--full-tree", "--end-of-options", commit)
	if err != nil {
		return nil, err
	}
	var out []entry
	for _, rec := range bytes.Split(raw, []byte{0}) {
		if len(rec) == 0 {
			continue
		}
		meta, path, ok := bytes.Cut(rec, []byte{'\t'})
		fields := strings.Fields(string(meta))
		if !ok || len(fields) != 4 {
			return nil, domain.Errorf("salida inesperada de ls-tree: %q", rec)
		}
		var size int64
		if fields[3] != "-" {
			if size, err = strconv.ParseInt(fields[3], 10, 64); err != nil {
				return nil, domain.Errorf("tamaño inesperado en ls-tree: %q", fields[3])
			}
		}
		out = append(out, entry{mode: fields[0], kind: fields[1], object: fields[2], size: size, path: string(path)})
	}
	return out, nil
}

// readSkill reads every blob under dirPath with one cat-file process.
func (f Fetcher) readSkill(ctx context.Context, dir, dirPath string, entries []entry) ([]skill.File, error) {
	var (
		picked []entry
		files  []skill.File
		total  int64
	)
	for _, e := range entries {
		rel, ok := skillsrc.Under(dirPath, e.path)
		if !ok {
			continue
		}
		var kind skill.Kind
		switch e.mode {
		case "100644":
			kind = skill.KindRegular
		case "100755":
			kind = skill.KindExecutable
		case "120000":
			kind = skill.KindSymlink
		case "160000":
			return nil, domain.Errorf("%s es un submódulo de Git, y las skills no pueden contenerlos: %w", rel, domain.ErrInvalid)
		default:
			return nil, domain.Errorf("%s tiene un modo no soportado %s: %w", rel, e.mode, domain.ErrInvalid)
		}
		total += e.size
		if f.MaxSkillBytes > 0 && total > f.MaxSkillBytes {
			return nil, domain.Errorf("ocupa más de %d bytes; no se descarga: %w", f.MaxSkillBytes, domain.ErrInvalid)
		}
		picked = append(picked, e)
		files = append(files, skill.File{Path: rel, Kind: kind})
	}

	var objects bytes.Buffer
	for _, e := range picked {
		objects.WriteString(e.object + "\n")
	}
	raw, err := f.git(ctx, dir, &objects, "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(bytes.NewReader(raw))
	for i, e := range picked {
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, domain.Errorf("leer %s: %w", e.path, err)
		}
		var obj, typ string
		var size int64
		if _, err := fmt.Sscanf(header, "%s %s %d", &obj, &typ, &size); err != nil || obj != e.object || typ != "blob" {
			return nil, domain.Errorf("cabecera inesperada de cat-file %q para %s", strings.TrimSpace(header), e.path)
		}
		data := make([]byte, size+1) // content plus the trailing newline
		if _, err := io.ReadFull(r, data); err != nil {
			return nil, domain.Errorf("leer %s: %w", e.path, err)
		}
		data = data[:size]
		if files[i].Kind == skill.KindSymlink {
			files[i].LinkTarget = string(data)
		} else {
			files[i].Data = data
		}
	}
	return files, nil
}

// notFound recognizes git's messages for a missing repository or ref.
func notFound(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"couldn't find remote ref", "repository not found", "does not appear to be a git repository", "not our ref"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// git runs git with prompts and external helpers that could run code from
// the fetched repository turned off.
func (f Fetcher) git(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error) {
	bin := f.Git
	if bin == "" {
		bin = "git"
	}
	base := []string{
		"-c", "core.hooksPath=" + os.DevNull,
		"-c", "core.fsmonitor=false",
		"-c", "protocol.ext.allow=never",
		"-C", dir,
	}
	cmd := exec.CommandContext(ctx, bin, append(base, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"GIT_LFS_SKIP_SMUDGE=1",
	)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return nil, domain.Errorf("skilus necesita tener git instalado: %w", err)
		}
		if errors.As(err, &exitErr) {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = exitErr.Error()
			}
			return nil, domain.Errorf("git %s: %s", args[0], msg)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

var _ app.RefResolver = Fetcher{}

// Resolve implements app.RefResolver with git ls-remote, which reads the
// remote's refs without downloading any content. Refs resolve as git fetch
// resolves them: tags before branches, annotated tags to their commit.
func (f Fetcher) Resolve(ctx context.Context, src source.Source) (string, error) {
	if isCommit(src.Ref) {
		return src.Ref, nil
	}
	want := []string{"HEAD"}
	patterns := []string{"HEAD"}
	if src.Ref != "" {
		// The peeled line of an annotated tag only shows when asked for.
		patterns = []string{src.Ref, src.Ref + "^{}"}
		want = []string{"refs/tags/" + src.Ref + "^{}", "refs/tags/" + src.Ref, "refs/heads/" + src.Ref}
	}
	args := append([]string{"ls-remote", "--end-of-options", src.URL}, patterns...)
	raw, err := f.git(ctx, os.TempDir(), nil, args...)
	if err != nil {
		if notFound(err) {
			return "", domain.Errorf("resolver %s: %w: %w", src, err, domain.ErrNotFound)
		}
		return "", domain.Errorf("resolver %s: %w", src, err)
	}
	refs := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if sha, name, ok := strings.Cut(strings.TrimSpace(line), "\t"); ok {
			refs[name] = sha
		}
	}
	for _, name := range want {
		if sha, ok := refs[name]; ok {
			return sha, nil
		}
	}
	return "", domain.Errorf("%s no tiene la referencia %s: %w", src.URL, patterns[0], domain.ErrNotFound)
}

// isCommit reports whether ref is a full commit id, which never moves.
func isCommit(ref string) bool {
	if len(ref) != 40 && len(ref) != 64 {
		return false
	}
	for _, c := range ref {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

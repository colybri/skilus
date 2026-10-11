// Package policy inspects a skill package before it is installed. It is a
// pure domain service: it reads the package in memory and never touches
// the disk or the network.
package policy

import (
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
)

// Severity of a finding.
type Severity string

// Severities. A blocking finding stops the installation; a warning needs
// the user's confirmation (or an explicit allow) and blocks under --strict.
const (
	Block Severity = "block"
	Warn  Severity = "warn"
)

// Code identifies the rule that produced a finding.
type Code string

// Rule codes.
const (
	CodeSymlinkEscape Code = "symlink-escape"
	CodeExecutable    Code = "executable"
	CodeFileTooLarge  Code = "file-too-large"
	CodeTooLarge      Code = "package-too-large"
	CodeTooManyFiles  Code = "too-many-files"
	CodePipeToShell   Code = "pipe-to-shell"
	CodeInvisibleText Code = "invisible-text"
	CodeControlChars  Code = "control-characters"
	CodeUntrusted     Code = "untrusted-source"
	CodeCredentials   Code = "credential-access"
	CodeUnsigned      Code = "unsigned-commit"
	CodeUnchecked     Code = "signature-unchecked"
	CodeInvalidSkill  Code = "invalid-skill"
)

// Finding is one result of the inspection.
type Finding struct {
	Code     Code
	Severity Severity
	Path     string
	Detail   domain.Text
}

// Limits bounds what a package may contain.
type Limits struct {
	MaxFileBytes  int64
	MaxTotalBytes int64
	MaxFiles      int
}

// DefaultLimits mirror the download limits of vercel-labs/skills.
var DefaultLimits = Limits{MaxFileBytes: 10 << 20, MaxTotalBytes: 25 << 20, MaxFiles: 1000}

// Allow lists what the user explicitly accepts for one skill.
type Allow struct {
	Scripts bool
}

// Report is the result of inspecting a package.
type Report struct {
	Findings []Finding
	// Executables lists every executable file, allowed or not, for display.
	Executables []string
}

// Blocking reports whether any finding stops the installation.
func (r Report) Blocking() bool { return r.count(Block) > 0 }

// Warnings returns the number of warnings.
func (r Report) Warnings() int { return r.count(Warn) }

func (r Report) count(s Severity) int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == s {
			n++
		}
	}
	return n
}

var (
	credentialsRe = regexp.MustCompile(`(?i)~/\.ssh\b|\bid_(rsa|ed25519|ecdsa)\b|\.aws/credentials|\.netrc\b|\.git-credentials|\.docker/config\.json|/etc/shadow|\bsecurity\s+find-(generic|internet)-password|\.config/gh/hosts\.yml`)
	pipeToShellRe = regexp.MustCompile(`(?i)\b(curl|wget|iwr|invoke-webrequest)\b[^\n|]*\|\s*(sudo\s+)?(ba|z|da)?sh\b|\b(curl|wget)\b[^\n|]*\|\s*(sudo\s+)?(python3?|node|perl|ruby)\b|\biex\s*\(`)
)

// Inspect checks a package against the rules and limits.
func Inspect(p skill.Package, limits Limits, allow Allow) Report {
	var r Report
	add := func(code Code, sev Severity, file string, detail domain.Text) {
		r.Findings = append(r.Findings, Finding{Code: code, Severity: sev, Path: file, Detail: detail})
	}

	files := p.Files()
	if limits.MaxFiles > 0 && len(files) > limits.MaxFiles {
		add(CodeTooManyFiles, Block, "", domain.Msg("%d ficheros, límite %d", len(files), limits.MaxFiles))
	}
	if limits.MaxTotalBytes > 0 && p.Size() > limits.MaxTotalBytes {
		add(CodeTooLarge, Block, "", domain.Msg("%d bytes, límite %d", p.Size(), limits.MaxTotalBytes))
	}

	for _, f := range files {
		switch f.Kind {
		case skill.KindSymlink:
			if escapes(f.Path, f.LinkTarget) {
				add(CodeSymlinkEscape, Block, f.Path, domain.Msg("apunta a %s, fuera de la skill", f.LinkTarget))
			}
		case skill.KindExecutable:
			r.Executables = append(r.Executables, f.Path)
			if !allow.Scripts {
				add(CodeExecutable, Warn, f.Path, domain.Msg("fichero ejecutable; acéptalo con allow: [scripts]"))
			}
		}
		if limits.MaxFileBytes > 0 && f.Size() > limits.MaxFileBytes {
			add(CodeFileTooLarge, Block, f.Path, domain.Msg("%d bytes, límite %d", f.Size(), limits.MaxFileBytes))
		}
		if f.Kind != skill.KindSymlink && utf8.Valid(f.Data) {
			inspectCode(f, add)
		}
		if f.Path == skill.ManifestFile || strings.HasSuffix(f.Path, ".md") {
			inspectText(f, add)
		}
	}
	inspectMetadata(p, add)
	return r
}

func escapes(file, target string) bool {
	if target == "" || path.IsAbs(target) || strings.Contains(target, `\`) || strings.Contains(target, ":") {
		return true
	}
	resolved := path.Join(path.Dir(file), target)
	return resolved == ".." || strings.HasPrefix(resolved, "../")
}

// inspectCode looks in every text file, scripts included, for code that
// downloads and runs more code or reads the user's credentials.
func inspectCode(f skill.File, add func(Code, Severity, string, domain.Text)) {
	text := string(f.Data)
	if m := pipeToShellRe.FindString(text); m != "" {
		add(CodePipeToShell, Warn, f.Path, domain.Msg("descarga y ejecuta código: %s", truncate(m, 80)))
	}
	if m := credentialsRe.FindString(text); m != "" {
		add(CodeCredentials, Warn, f.Path, domain.Msg("menciona credenciales: %s", truncate(m, 80)))
	}
}

// inspectText looks in Markdown for text the user would not see.
func inspectText(f skill.File, add func(Code, Severity, string, domain.Text)) {
	text := string(f.Data)
	if r, ok := firstInvisible(text); ok {
		add(CodeInvisibleText, Warn, f.Path, domain.Msg("contiene el carácter invisible U+%04X", r))
	}
	if strings.ContainsRune(text, 0x1b) {
		add(CodeControlChars, Warn, f.Path, domain.Msg("contiene secuencias de escape de terminal"))
	}
}

func inspectMetadata(p skill.Package, add func(Code, Severity, string, domain.Text)) {
	if strings.ContainsFunc(p.Description(), isControl) {
		add(CodeControlChars, Block, skill.ManifestFile, domain.Msg("la descripción contiene caracteres de control"))
	}
}

func isControl(r rune) bool {
	return (r < 0x20 && r != '\t' && r != '\n') || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// firstInvisible finds zero-width, bidirectional-override and Unicode tag
// characters, which can hide instructions from a human reviewer.
func firstInvisible(s string) (rune, bool) {
	for _, r := range s {
		switch {
		case r >= 0x200B && r <= 0x200F,
			r >= 0x202A && r <= 0x202E,
			r >= 0x2060 && r <= 0x2064,
			r >= 0x2066 && r <= 0x2069,
			r == 0xFEFF,
			r >= 0xE0000 && r <= 0xE007F:
			return r, true
		}
	}
	return 0, false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

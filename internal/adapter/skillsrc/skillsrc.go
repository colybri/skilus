// Package skillsrc holds what every source adapter shares: where skills
// live inside a source and how a SKILL.md becomes a skill.Package.
package skillsrc

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
)

// SkillDirs are the directories, relative to the source root and in order
// of preference, whose subdirectories Discover takes for skills.
var SkillDirs = []string{"skills/", "skills/.curated/", ""}

// Discover returns the skill directories of a source, given the paths of
// its regular SKILL.md files (forward slashes, relative to the source root).
// A SKILL.md at the root makes the source a single skill; otherwise skills
// are looked for in skills/<name>/, then in skills/.curated/<name>/ (the
// layout of openai/skills) and then in <name>/. Other hidden directories
// are ignored.
func Discover(manifests []string) []string {
	set := make(map[string]bool, len(manifests))
	for _, m := range manifests {
		set[m] = true
	}
	if set[skill.ManifestFile] {
		return []string{"."}
	}
	for _, base := range SkillDirs {
		var found []string
		for m := range set {
			rest, ok := strings.CutPrefix(m, base)
			if !ok {
				continue
			}
			dir, file, ok := strings.Cut(rest, "/")
			if !ok || file != skill.ManifestFile || strings.HasPrefix(dir, ".") {
				continue
			}
			found = append(found, base+dir)
		}
		if len(found) > 0 {
			sort.Strings(found)
			return found
		}
	}
	return nil
}

// Build turns the files of one skill directory (paths relative to it) into
// a package, reading the name and description from SKILL.md.
func Build(files []skill.File) (skill.Package, error) {
	var manifest []byte
	for _, f := range files {
		if f.Path == skill.ManifestFile && f.Kind == skill.KindRegular {
			manifest = f.Data
		}
	}
	if manifest == nil {
		return skill.Package{}, fmt.Errorf("no regular %s: %w", skill.ManifestFile, domain.ErrInvalid)
	}
	meta, err := parseFrontmatter(manifest)
	if err != nil {
		return skill.Package{}, err
	}
	name, err := skill.NewName(meta.Name)
	if err != nil {
		return skill.Package{}, err
	}
	p, err := skill.NewPackage(name, meta.Description, files)
	if err != nil {
		return skill.Package{}, err
	}
	raw, err := meta.requires()
	if err != nil {
		return skill.Package{}, err
	}
	reqs, err := skill.ParseRequires(raw)
	if err != nil {
		return skill.Package{}, fmt.Errorf("%s metadata.%s: %w", skill.ManifestFile, skill.RequiresKey, err)
	}
	return p.WithRequires(reqs)
}

// Under reports whether p is inside dir ("." is the root) and returns its
// path relative to dir.
func Under(dir, p string) (string, bool) {
	if dir == "." {
		return p, true
	}
	return strings.CutPrefix(p, path.Clean(dir)+"/")
}

type frontmatter struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Metadata    yaml.Node `yaml:"metadata"`
}

// requires returns metadata.requires. The specification wants a string;
// a list of strings is accepted too.
func (fm frontmatter) requires() (string, error) {
	// Other tools put anything under metadata: only requires is read.
	var node *yaml.Node
	if fm.Metadata.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(fm.Metadata.Content); i += 2 {
			if fm.Metadata.Content[i].Value == skill.RequiresKey {
				node = fm.Metadata.Content[i+1]
			}
		}
	}
	if node == nil {
		return "", nil
	}
	var s string
	if err := node.Decode(&s); err == nil && node.Kind == yaml.ScalarNode {
		return s, nil
	}
	var list []string
	if err := node.Decode(&list); err != nil {
		return "", fmt.Errorf("%s metadata.%s must be a string: %w", skill.ManifestFile, skill.RequiresKey, domain.ErrInvalid)
	}
	return strings.Join(list, " "), nil
}

// parseFrontmatter reads the YAML block between the leading "---" lines.
func parseFrontmatter(data []byte) (frontmatter, error) {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	rest, ok := bytes.CutPrefix(data, []byte("---\n"))
	if !ok {
		return frontmatter{}, fmt.Errorf("%s does not start with YAML frontmatter (---): %w", skill.ManifestFile, domain.ErrInvalid)
	}
	block, _, ok := bytes.Cut(rest, []byte("\n---"))
	if !ok {
		return frontmatter{}, fmt.Errorf("%s frontmatter is not closed with ---: %w", skill.ManifestFile, domain.ErrInvalid)
	}
	var fm frontmatter
	if err := yaml.Unmarshal(block, &fm); err != nil {
		return frontmatter{}, fmt.Errorf("%s frontmatter: %w: %w", skill.ManifestFile, err, domain.ErrInvalid)
	}
	return fm, nil
}

// Parser implements app.PackageParser with Build.
type Parser struct{}

// Package implements app.PackageParser.
func (Parser) Package(files []skill.File) (skill.Package, error) { return Build(files) }

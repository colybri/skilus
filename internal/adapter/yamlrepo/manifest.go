package yamlrepo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/colybri/skilus/internal/app"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/agent"
	"github.com/colybri/skilus/internal/domain/policy"
	"github.com/colybri/skilus/internal/domain/profile"
	"github.com/colybri/skilus/internal/domain/skill"
)

// AddSkill implements app.ManifestRepository. It edits the YAML tree so the
// user's comments and order survive, and leaves an existing entry for the
// same skill untouched.
func (r Repo) AddSkill(_ context.Context, scope agent.Scope, e app.ManifestEntry) error {
	p := r.path(scope, ManifestFile)
	doc, err := readNode(p)
	if err != nil {
		return err
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: the document must be a mapping: %w", p, domain.ErrInvalid)
	}

	skills := mappingValue(root, "skills")
	if skills == nil {
		skills = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		root.Content = append(root.Content, scalar("skills"), skills)
	}
	if skills.Kind != yaml.SequenceNode {
		// "skills: " with no value parses as null; treat it as empty.
		if skills.Tag != "!!null" {
			return fmt.Errorf("%s: skills must be a list: %w", p, domain.ErrInvalid)
		}
		*skills = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	}
	skills.Style = 0 // a flow "skills: []" becomes a block list
	for _, item := range skills.Content {
		if n := mappingValue(item, "name"); n != nil && n.Value == e.Name.String() {
			return nil
		}
	}

	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	entry.Content = append(entry.Content, scalar("name"), scalar(e.Name.String()), scalar("source"), scalar(e.Source))
	if len(e.Allow) > 0 {
		allow := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
		for _, a := range e.Allow {
			allow.Content = append(allow.Content, scalar(a))
		}
		entry.Content = append(entry.Content, scalar("allow"), allow)
	}
	skills.Content = append(skills.Content, entry)

	data, err := marshal(doc)
	if err != nil {
		return err
	}
	return writeAtomic(p, data)
}

// RemoveSkill implements app.ManifestRepository.
func (r Repo) RemoveSkill(_ context.Context, scope agent.Scope, name skill.Name) error {
	p := r.path(scope, ManifestFile)
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	doc, err := readNode(p)
	if err != nil {
		return err
	}
	skills := mappingValue(doc.Content[0], "skills")
	if skills == nil || skills.Kind != yaml.SequenceNode {
		return nil
	}
	kept := skills.Content[:0]
	for _, item := range skills.Content {
		if n := mappingValue(item, "name"); n != nil && n.Value == name.String() {
			continue
		}
		kept = append(kept, item)
	}
	if len(kept) == len(skills.Content) {
		return nil
	}
	skills.Content = kept
	data, err := marshal(doc)
	if err != nil {
		return err
	}
	return writeAtomic(p, data)
}

// Trust implements app.TrustList: the trust: list of the scope's
// skilus.yaml, empty when the file or the key is missing.
func (r Repo) Trust(_ context.Context, scope agent.Scope) (policy.Trust, error) {
	p := r.path(scope, ManifestFile)
	doc, err := readNode(p)
	if err != nil {
		return nil, err
	}
	node := mappingValue(doc.Content[0], "trust")
	if node == nil || node.Tag == "!!null" {
		return nil, nil
	}
	var trust policy.Trust
	if err := node.Decode(&trust); err != nil {
		return nil, fmt.Errorf("%s: trust must be a list of sources: %w", p, domain.ErrInvalid)
	}
	return trust, nil
}

// Manifest implements app.ManifestReader. Profiles keep their file order.
func (r Repo) Manifest(_ context.Context, scope agent.Scope) (app.Manifest, error) {
	p := r.path(scope, ManifestFile)
	doc, err := readNode(p)
	if err != nil {
		return app.Manifest{}, err
	}
	root := doc.Content[0]
	var m app.Manifest

	if node := mappingValue(root, "skills"); node != nil && node.Tag != "!!null" {
		var raw []struct {
			Name   string   `yaml:"name"`
			Source string   `yaml:"source"`
			Allow  []string `yaml:"allow"`
		}
		if err := node.Decode(&raw); err != nil {
			return app.Manifest{}, fmt.Errorf("%s: skills must be a list of name and source: %w", p, domain.ErrInvalid)
		}
		for _, s := range raw {
			n, err := skill.NewName(s.Name)
			if err != nil {
				return app.Manifest{}, fmt.Errorf("%s: %w", p, err)
			}
			if s.Source == "" {
				return app.Manifest{}, fmt.Errorf("%s: skill %s has no source: %w", p, n, domain.ErrInvalid)
			}
			m.Skills = append(m.Skills, app.ManifestEntry{Name: n, Source: s.Source, Allow: s.Allow})
		}
	}

	node := mappingValue(root, "profiles")
	if node == nil || node.Tag == "!!null" {
		return m, nil
	}
	if node.Kind != yaml.MappingNode {
		return app.Manifest{}, fmt.Errorf("%s: profiles must map names to skills and agents: %w", p, domain.ErrInvalid)
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := node.Content[i].Value
		var raw struct {
			Skills []string `yaml:"skills"`
			Agents []string `yaml:"agents"`
		}
		if err := node.Content[i+1].Decode(&raw); err != nil {
			return app.Manifest{}, fmt.Errorf("%s: profile %s must have skills and, optionally, agents: %w", p, name, domain.ErrInvalid)
		}
		var skills []skill.Name
		for _, s := range raw.Skills {
			n, err := skill.NewName(s)
			if err != nil {
				return app.Manifest{}, fmt.Errorf("%s: profile %s: %w", p, name, err)
			}
			skills = append(skills, n)
		}
		var agents []agent.ID
		for _, a := range raw.Agents {
			id, err := agent.NewID(a)
			if err != nil {
				return app.Manifest{}, fmt.Errorf("%s: profile %s: %w", p, name, err)
			}
			agents = append(agents, id)
		}
		prof, err := profile.New(name, skills, agents)
		if err != nil {
			return app.Manifest{}, fmt.Errorf("%s: %w", p, err)
		}
		m.Profiles = append(m.Profiles, prof)
	}
	return m, nil
}

func readNode(p string) (*yaml.Node, error) {
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return newDoc(), nil
	}
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w: %w", p, err, domain.ErrInvalid)
	}
	if len(doc.Content) == 0 {
		return newDoc(), nil // empty file
	}
	return &doc, nil
}

func newDoc() *yaml.Node {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	root.Content = append(root.Content, scalar("version"), &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "1"})
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func scalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

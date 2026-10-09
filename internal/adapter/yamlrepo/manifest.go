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

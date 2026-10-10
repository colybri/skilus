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
	"github.com/colybri/skilus/internal/domain/profile"
)

var _ app.ProfileRepository = Repo{}

// SaveProfile implements app.ProfileRepository. It edits the YAML tree:
// an existing profile keeps its place, its comments and the style of its
// lists, and a new one goes at the end of profiles:.
func (r Repo) SaveProfile(_ context.Context, scope agent.Scope, prof profile.Profile) error {
	p := r.path(scope, ManifestFile)
	doc, err := readNode(p)
	if err != nil {
		return err
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: the document must be a mapping: %w", p, domain.ErrInvalid)
	}

	profiles := mappingValue(root, "profiles")
	if profiles == nil {
		profiles = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, scalar("profiles"), profiles)
	}
	if profiles.Kind != yaml.MappingNode {
		// "profiles: " with no value parses as null; treat it as empty.
		if profiles.Tag != "!!null" {
			return fmt.Errorf("%s: profiles must map names to skills and agents: %w", p, domain.ErrInvalid)
		}
		*profiles = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	profiles.Style = 0 // a flow "profiles: {}" becomes a block mapping

	body := mappingValue(profiles, prof.Name())
	switch {
	case body == nil:
		body = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		profiles.Content = append(profiles.Content, scalar(prof.Name()), body)
	case body.Kind != yaml.MappingNode:
		*body = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", HeadComment: body.HeadComment, LineComment: body.LineComment}
	}

	skills := make([]string, 0, len(prof.Skills()))
	for _, n := range prof.Skills() {
		skills = append(skills, n.String())
	}
	agents := make([]string, 0, len(prof.Agents()))
	for _, id := range prof.Agents() {
		agents = append(agents, id.String())
	}
	setList(body, "skills", skills, true)
	setList(body, "agents", agents, false)

	return writeDoc(p, doc)
}

// DeleteProfile implements app.ProfileRepository.
func (r Repo) DeleteProfile(_ context.Context, scope agent.Scope, name string) error {
	p := r.path(scope, ManifestFile)
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	doc, err := readNode(p)
	if err != nil {
		return err
	}
	profiles := mappingValue(doc.Content[0], "profiles")
	if profiles == nil || profiles.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(profiles.Content); i += 2 {
		if profiles.Content[i].Value != name {
			continue
		}
		profiles.Content = append(profiles.Content[:i], profiles.Content[i+2:]...)
		return writeDoc(p, doc)
	}
	return nil
}

// setList sets m[key] to values. An existing list keeps its style and the
// comments of the items that stay; a new one is written inline, like
// "skills: [a, b]". With keepEmpty false an empty list drops the key.
func setList(m *yaml.Node, key string, values []string, keepEmpty bool) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != key {
			continue
		}
		if len(values) == 0 && !keepEmpty {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
		old := m.Content[i+1]
		items := map[string]*yaml.Node{}
		style := yaml.FlowStyle
		if old.Kind == yaml.SequenceNode {
			style = old.Style
			for _, it := range old.Content {
				items[it.Value] = it
			}
		}
		next := list(values, style)
		for j, v := range values {
			if it, ok := items[v]; ok {
				next.Content[j] = it
			}
		}
		next.HeadComment, next.LineComment, next.FootComment = old.HeadComment, old.LineComment, old.FootComment
		*old = *next
		return
	}
	if len(values) == 0 && !keepEmpty {
		return
	}
	m.Content = append(m.Content, scalar(key), list(values, yaml.FlowStyle))
}

func list(values []string, style yaml.Style) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: style}
	for _, v := range values {
		n.Content = append(n.Content, scalar(v))
	}
	return n
}

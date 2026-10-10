package skillsrc_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/colybri/skilus/internal/adapter/skillsrc"
	"github.com/colybri/skilus/internal/domain"
	"github.com/colybri/skilus/internal/domain/skill"
)

func TestDiscover(t *testing.T) {
	tests := []struct {
		in   []string
		want []string
	}{
		{[]string{"SKILL.md", "skills/a/SKILL.md"}, []string{"."}},
		{[]string{"skills/b/SKILL.md", "skills/a/SKILL.md", "c/SKILL.md"}, []string{"skills/a", "skills/b"}},
		{[]string{"c/SKILL.md", ".hidden/SKILL.md", "deep/x/SKILL.md"}, []string{"c"}},
		{[]string{"README.md"}, nil},
		// openai/skills: curated skills, plus system ones that are not offered.
		{[]string{"skills/.curated/b/SKILL.md", "skills/.curated/a/SKILL.md", "skills/.system/s/SKILL.md"}, []string{"skills/.curated/a", "skills/.curated/b"}},
		{[]string{"skills/a/SKILL.md", "skills/.curated/b/SKILL.md"}, []string{"skills/a"}},
	}
	for _, tt := range tests {
		if got := skillsrc.Discover(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Discover(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestBuild(t *testing.T) {
	ok := []skill.File{{Path: "SKILL.md", Kind: skill.KindRegular, Data: []byte("---\r\nname: demo\r\ndescription: \"A demo\"\r\n---\r\nBody")}}
	p, err := skillsrc.Build(ok)
	if err != nil || p.Name().String() != "demo" || p.Description() != "A demo" {
		t.Fatalf("Build() = %v, %v", p.Name(), err)
	}
	for label, data := range map[string]string{
		"no frontmatter": "# demo",
		"unclosed":       "---\nname: demo\n",
		"bad yaml":       "---\nname: [\n---\n",
		"bad name":       "---\nname: Demo Skill\ndescription: d\n---\n",
	} {
		files := []skill.File{{Path: "SKILL.md", Kind: skill.KindRegular, Data: []byte(data)}}
		if _, err := skillsrc.Build(files); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", label, err)
		}
	}
}

func TestUnder(t *testing.T) {
	if rel, ok := skillsrc.Under("skills/a", "skills/a/x/y.md"); !ok || rel != "x/y.md" {
		t.Errorf("Under = %q, %v", rel, ok)
	}
	if _, ok := skillsrc.Under("skills/a", "skills/ab/SKILL.md"); ok {
		t.Error("skills/ab is not under skills/a")
	}
	if rel, ok := skillsrc.Under(".", "SKILL.md"); !ok || rel != "SKILL.md" {
		t.Errorf("root Under = %q, %v", rel, ok)
	}
}

func TestBuildReadsRequires(t *testing.T) {
	build := func(fm string) (skill.Package, error) {
		return skillsrc.Build([]skill.File{{Path: skill.ManifestFile, Kind: skill.KindRegular, Data: []byte("---\nname: report\ndescription: Informes\n" + fm + "---\nbody\n")}})
	}
	p, err := build("metadata:\n  author: x\n  requires: \"pdf, anthropics/skills@v1#docx\"\n")
	if err != nil {
		t.Fatal(err)
	}
	reqs := p.Requires()
	if len(reqs) != 2 || reqs[0].String() != "pdf" || reqs[1].Source != "anthropics/skills@v1" || reqs[1].Name.String() != "docx" {
		t.Fatalf("requires = %+v", reqs)
	}
	if p, err := build("metadata:\n  requires: [pdf, docx]\n"); err != nil || len(p.Requires()) != 2 {
		t.Fatalf("list: %v, %v", p.Requires(), err)
	}
	if p, err := build("metadata: free text\n"); err != nil || len(p.Requires()) != 0 {
		t.Fatalf("scalar metadata: %v, %v", p.Requires(), err)
	}
	for _, bad := range []string{
		"metadata:\n  requires: \"Bad\"\n",
		"metadata:\n  requires: \"#pdf\"\n",
		"metadata:\n  requires: \"report\"\n",
		"metadata:\n  requires: {a: b}\n",
	} {
		if _, err := build(bad); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%q: err = %v, want ErrInvalid", bad, err)
		}
	}
}

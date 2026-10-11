package cli

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/colybri/skilus/internal/cli/i18n"
)

// textFuncs are the functions outside this package whose first argument is
// a message id: the domain's translatable errors and texts.
var textFuncs = map[string]bool{"Errorf": true, "NewError": true, "Msg": true}

// messageIDs returns every literal message id the catalogs must translate:
// the first argument of T calls in this package and of domain.Errorf,
// domain.NewError and domain.Msg anywhere under internal/. Ids without
// letters, such as "%s: %w", need no translation.
func messageIDs(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		inCLI := filepath.Dir(name) == filepath.Join("..", "cli")
		// message.go defines Errorf, NewError and Msg in terms of each other.
		inDomain := f.Name.Name == "domain" && filepath.Base(name) != "message.go"
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			var fn string
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				if x, ok := fun.X.(*ast.Ident); ok && x.Name == "domain" && textFuncs[fun.Sel.Name] {
					fn = fun.Sel.Name
				} else if inCLI && fun.Sel.Name == "T" {
					fn = "T"
				}
			case *ast.Ident:
				if inDomain && textFuncs[fun.Name] {
					fn = fun.Name
				}
			}
			if fn == "" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s: %s needs a literal message id", fset.Position(call.Pos()), fn)
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			if strings.IndexFunc(verb.ReplaceAllString(s, ""), unicode.IsLetter) >= 0 {
				seen[s] = true
			}
			return true
		})
	}
	ids := make([]string, 0, len(seen))
	for s := range seen {
		ids = append(ids, s)
	}
	sort.Strings(ids)
	return ids
}

// useLine matches Cobra Use strings such as "add <origen>".
var useLine = regexp.MustCompile(`^[a-z]+ [<\[]`)

var verb = regexp.MustCompile(`%(\[\d+\])?[-+# 0]*\d*(\.\d+)?[a-zA-Z%]`)

// sample returns arguments that fit the verbs of msgid, in order.
func sample(msgid string) []any {
	var args []any
	for _, v := range verb.FindAllString(msgid, -1) {
		switch v[len(v)-1] {
		case '%':
		case 'd':
			args = append(args, 7)
		default:
			args = append(args, "x")
		}
	}
	return args
}

func TestCatalogsTranslateEveryMessage(t *testing.T) {
	ids := messageIDs(t)
	if len(ids) < 100 {
		t.Fatalf("found only %d message ids; is the extraction broken?", len(ids))
	}
	for _, l := range i18n.Languages {
		if l.Code == i18n.Source {
			continue
		}
		c, err := i18n.Load(l.Code)
		if err != nil {
			t.Fatalf("%s: %v", l.Code, err)
		}
		for _, id := range ids {
			if !c.Has(id) {
				t.Errorf("%s: missing %q", l.Code, id)
				continue
			}
			args := sample(id)
			// Errors format %w like %v once translated.
			got := fmt.Sprintf(wrapVerb.ReplaceAllString(c.T(id), "%${1}v"), args...)
			if len(args) > 0 && strings.Contains(got, "%!") {
				t.Errorf("%s: %q -> %q does not take the same arguments", l.Code, id, got)
			}
			if strings.Count(c.T(id), "\t") != strings.Count(id, "\t") || strings.HasSuffix(c.T(id), "\n") != strings.HasSuffix(id, "\n") {
				t.Errorf("%s: %q must keep the tabs and the final newline of %q", l.Code, c.T(id), id)
			}
			if useLine.MatchString(id) {
				// A Use line: the command name cannot change.
				if strings.Fields(c.T(id))[0] != strings.Fields(id)[0] {
					t.Errorf("%s: %q must start with the command name", l.Code, c.T(id))
				}
			}
		}
		for _, extra := range c.Unused(ids) {
			t.Errorf("%s: %q is no longer used", l.Code, extra)
		}
	}
}

func TestLanguagesMatchTheDocs(t *testing.T) {
	var codes []string
	for _, l := range i18n.Languages {
		codes = append(codes, l.Code)
	}
	if got := strings.Join(codes, ","); got != "es,en,fr,de,pt,zh,ja,id,ar,ru,pl,ur,hi" {
		t.Fatalf("languages = %s", got)
	}
}

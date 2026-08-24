package intent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strconv"
	"strings"
	"testing"
)

// TestAllLabelsListsEveryDeclaredLabel keeps AllLabels honest against the
// constants it claims to enumerate.
//
// AllLabels is the source of truth for IsValid, definition coverage, and the
// classifier prompt, and every one of those is checked by iterating AllLabels.
// A label that is declared but missing from the list therefore passes all of
// those checks while silently dropping out of the taxonomy: IsValid rejects it,
// no definition has to cover it, and the classifier is never told about it.
func TestAllLabelsListsEveryDeclaredLabel(t *testing.T) {
	declared := declaredIntentLabelConsts(t)
	if len(declared) == 0 {
		t.Fatal("found no IntentLabel constants; the parser stopped seeing the taxonomy")
	}

	listed := make(map[IntentLabel]bool, len(AllLabels()))
	for _, label := range AllLabels() {
		listed[label] = true
	}

	for name, label := range declared {
		if !listed[label] {
			t.Errorf("const %s (%q) is declared but AllLabels() omits it", name, label)
		}
	}

	declaredValues := make(map[IntentLabel]bool, len(declared))
	for _, label := range declared {
		declaredValues[label] = true
	}
	for label := range listed {
		if !declaredValues[label] {
			t.Errorf("AllLabels() returns %q, which no IntentLabel constant declares", label)
		}
	}
}

// declaredIntentLabelConsts returns every `Name IntentLabel = "value"` constant
// in the package, keyed by constant name.
//
// Only specs that spell the type out are collected, which is how the taxonomy
// is written. A constant that inherits its type from a preceding spec would be
// missed; if the taxonomy ever adopts that style, this helper must grow with it
// rather than silently under-reporting.
func declaredIntentLabelConsts(t *testing.T) map[string]IntentLabel {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse intent package: %v", err)
	}
	out := make(map[string]IntentLabel)
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					value, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					ident, ok := value.Type.(*ast.Ident)
					if !ok || ident.Name != "IntentLabel" {
						continue
					}
					for i, name := range value.Names {
						if i >= len(value.Values) {
							continue
						}
						lit, ok := value.Values[i].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						text, err := strconv.Unquote(lit.Value)
						if err != nil {
							t.Fatalf("const %s has an unreadable value %s: %v", name.Name, lit.Value, err)
						}
						out[name.Name] = IntentLabel(text)
					}
				}
			}
		}
	}
	return out
}

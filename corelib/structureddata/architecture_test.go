package structureddata

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestCorelibStructuredDataDoesNotDependOnDataSrvImplementation(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	forbiddenImports := map[string]string{
		"database/sql":                "SQL storage belongs in datasrv/structureddata",
		"github.com/mattn/go-sqlite3": "SQLite storage belongs in datasrv/structureddata",
		"modernc.org/sqlite":          "SQLite storage belongs in datasrv/structureddata",
		"net":                         "network serving belongs in datasrv/structureddata or command packages",
		"net/http":                    "HTTP serving belongs in datasrv/structureddata",
		"os":                          "process and filesystem access belongs in datasrv/structureddata or command packages",
		"github.com/RapidAI/CodeClaw/datasrv/structureddata": "the concrete datasrv implementation must not flow back into corelib contracts",
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		datasrvImport := regexp.MustCompile(`(?m)^\s*(?:\w+\s+)?"github\.com/RapidAI/CodeClaw/datasrv/structureddata"`)
		if datasrvImport.MatchString(text) {
			t.Fatalf("%s imports the datasrv implementation package; corelib/structureddata must stay contract-only", file)
		}
		for path, reason := range forbiddenImports {
			importPattern := regexp.MustCompile(`(?m)^\s*(?:\w+\s+)?"` + regexp.QuoteMeta(path) + `"`)
			if importPattern.MatchString(text) {
				t.Fatalf("%s imports %s; %s", file, path, reason)
			}
		}
		for _, symbol := range []string{"NewService", "NewSQLiteStore", "NewHTTPServer", "NewHTTPServerWithAPIKeys"} {
			if strings.Contains(text, symbol) {
				t.Fatalf("%s exposes %s; service/store/http implementation constructors belong in datasrv/structureddata", file, symbol)
			}
		}
		implementationType := regexp.MustCompile(`(?m)^type (HTTPServer|Service|SQLiteStore|Store)\b`)
		if match := implementationType.FindStringSubmatch(text); len(match) > 1 {
			t.Fatalf("%s declares implementation type %s; corelib/structureddata must stay contract-only", file, match[1])
		}
		exportedBehavior := regexp.MustCompile(`(?m)^(func|var|const) ([A-Z][A-Za-z0-9_]*)\b|^func \([^)]+\) ([A-Z][A-Za-z0-9_]*)\b`)
		if match := exportedBehavior.FindStringSubmatch(text); len(match) > 0 {
			name := firstNonEmptyTestMatch(match[2], match[3])
			t.Fatalf("%s declares exported behavior %s; corelib/structureddata should only expose access contracts and DTO types", file, name)
		}
	}
}

func firstNonEmptyTestMatch(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "<unknown>"
}

func TestCorelibArchitectureAllowsDocumentingDataSrvPackage(t *testing.T) {
	data, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "github.com/RapidAI/CodeClaw/datasrv/structureddata") {
		t.Fatal("doc.go should document where the concrete datasrv implementation lives")
	}
}

func TestCorelibStructuredDataExportedStructFieldsHaveJSONTags(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	allowedHiddenFields := map[string]bool{
		"Principal.Policy":     true,
		"RecordRevision.RowID": true,
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range parsed.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok.String() != "type" {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || !typeSpec.Name.IsExported() {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range structType.Fields.List {
					for _, name := range field.Names {
						if !name.IsExported() {
							continue
						}
						if field.Tag == nil || !strings.Contains(field.Tag.Value, `json:"`) {
							pos := fset.Position(field.Pos())
							t.Fatalf("%s exported field %s.%s must define a json tag for the shared access contract", pos, typeSpec.Name.Name, name.Name)
						}
						tagValue, err := strconv.Unquote(field.Tag.Value)
						if err != nil {
							pos := fset.Position(field.Pos())
							t.Fatalf("%s exported field %s.%s has invalid struct tag: %v", pos, typeSpec.Name.Name, name.Name, err)
						}
						jsonName := strings.Split(reflect.StructTag(tagValue).Get("json"), ",")[0]
						if jsonName == "" || jsonName == "-" {
							if allowedHiddenFields[typeSpec.Name.Name+"."+name.Name] {
								continue
							}
							pos := fset.Position(field.Pos())
							t.Fatalf("%s exported field %s.%s must have a concrete json contract name", pos, typeSpec.Name.Name, name.Name)
						}
					}
				}
			}
		}
	}
}

func TestCorelibStructuredDataExportedTypesAreStructDTOs(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range parsed.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok.String() != "type" {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || !typeSpec.Name.IsExported() {
					continue
				}
				if _, ok := typeSpec.Type.(*ast.StructType); !ok {
					pos := fset.Position(typeSpec.Pos())
					t.Fatalf("%s exported type %s must be a struct DTO; behavioral contracts and implementation types do not belong in corelib/structureddata", pos, typeSpec.Name.Name)
				}
			}
		}
	}
}

func TestCorelibStructuredDataKeepsMinimalContractImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	allowedImports := map[string]bool{
		"time": true,
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse imports for %s: %v", file, err)
		}
		for _, importSpec := range parsed.Imports {
			path := strings.Trim(importSpec.Path.Value, `"`)
			if !allowedImports[path] {
				t.Fatalf("%s imports %s; corelib/structureddata should keep DTO contracts lightweight and implementation-free", file, path)
			}
		}
	}
}

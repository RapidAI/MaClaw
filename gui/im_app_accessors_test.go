package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIMMessageHandler_NoSelfRecursiveMethods scans ALL Go source files in
// the gui package for methods on *IMMessageHandler. For each method, it
// verifies that the method body does NOT contain a call expression that
// resolves back to itself (h.foo() calling h.foo()), which would cause
// infinite recursion and a stack-overflow crash with no panic trace.
//
// This is a structural AST check that catches the exact class of bug that
// caused the weather-query crash: getGateIntentClassifier() called itself
// instead of delegating to h.app.GetGateIntentClassifier().
//
// Coverage: all methods, not just get* accessors — ensure/tool/handle
// methods are equally susceptible to copy-paste self-recursion.
func TestIMMessageHandler_NoSelfRecursiveMethods(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("failed to read gui directory: %v", err)
	}

	var violations []string

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		src, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			continue
		}

		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			continue
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			if !isIMMessageHandlerReceiver(fn.Recv) {
				continue
			}

			methodName := fn.Name.Name
			recvName := receiverVarName(fn.Recv)
			if recvName == "" {
				continue
			}

			// Walk the function body looking for self-calls.
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok {
					return true
				}
				if ident.Name == recvName && sel.Sel.Name == methodName {
					pos := fset.Position(call.Pos())
					violations = append(violations,
						pos.String()+": "+methodName+" calls itself (infinite recursion)")
				}
				return true
			})
		}
	}

	if len(violations) > 0 {
		t.Errorf("found %d self-recursive method(s) on IMMessageHandler:\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
}

func isIMMessageHandlerReceiver(recv *ast.FieldList) bool {
	if recv == nil || len(recv.List) == 0 {
		return false
	}
	star, ok := recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	return ok && ident.Name == "IMMessageHandler"
}

func receiverVarName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 || len(recv.List[0].Names) == 0 {
		return ""
	}
	return recv.List[0].Names[0].Name
}

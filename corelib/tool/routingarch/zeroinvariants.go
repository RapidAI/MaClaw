package routingarch

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
)

// Design section 10.1 names four rates that must stay at zero. Unlike the other
// metrics they are not measured in production dashboards here; they are held by
// tests spread across three packages. That spread is the problem: a rename or a
// deletion removes the only enforcement of a zero-invariant and nothing reports
// it. ScanTestFunctions gives the evidence register a way to prove that every
// test it names still exists.

// ZeroInvariant identifies one of the four rates.
type ZeroInvariant string

const (
	// An external effect whose outcome is unknown must never be resent by the
	// system on its own; it converges to unknown and waits for reconciliation.
	ZeroInvariantUnknownEffectReplay ZeroInvariant = "unknown_external_effect_auto_replay"
	// One idempotency key may produce at most one external effect, under
	// concurrency, retry and crash recovery.
	ZeroInvariantDuplicateEffect ZeroInvariant = "duplicate_idempotency_key_effect"
	// After recovery, a superseded plan revision must not execute.
	ZeroInvariantStaleRevisionExecution ZeroInvariant = "stale_revision_execution_after_recovery"
	// A model must not reach a control-plane operation, or widen its authority
	// beyond the scope it was granted.
	ZeroInvariantControlPlaneOverreach ZeroInvariant = "control_plane_call_beyond_scope"
)

// ZeroInvariants is the closed set, in the order section 10.1 lists them.
var ZeroInvariants = []ZeroInvariant{
	ZeroInvariantControlPlaneOverreach,
	ZeroInvariantUnknownEffectReplay,
	ZeroInvariantDuplicateEffect,
	ZeroInvariantStaleRevisionExecution,
}

// TestFunction is one test declaration found in the repository.
type TestFunction struct {
	Package string // slash-separated directory relative to the repository root
	Name    string
}

func (f TestFunction) String() string { return f.Package + ":" + f.Name }

// ScanTestFunctions returns every Go test function declared under root, keyed
// by "package/dir:TestName". A parse failure is an error rather than a silent
// omission, because an unreadable file would quietly shrink the register.
func ScanTestFunctions(root string) (map[string]TestFunction, error) {
	found := make(map[string]TestFunction)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if strings.HasPrefix(entry.Name(), ".") || skippedDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		pkg := filepath.ToSlash(filepath.Dir(rel))
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", filepath.ToSlash(rel), parseErr)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || function.Name == nil {
				continue
			}
			if !strings.HasPrefix(function.Name.Name, "Test") {
				continue
			}
			entry := TestFunction{Package: pkg, Name: function.Name.Name}
			found[entry.String()] = entry
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

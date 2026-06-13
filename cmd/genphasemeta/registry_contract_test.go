package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// normalizeEOL strips carriage returns so the comparison is insensitive to the
// line-ending convention git or the editor may have applied to the committed
// file (e.g. CRLF on Windows checkouts).
func normalizeEOL(s string) string {
	return strings.ReplaceAll(s, "\r", "")
}

// findRepoRoot walks up from the test's working directory looking for the
// module's go.mod, returning the directory that contains it. This makes the
// contract test robust to the test binary's working directory and to changes
// in how deeply cmd/genphasemeta is nested under the repository root, rather
// than hardcoding a fixed number of "../" hops.
func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repository root (no go.mod found walking up from working directory)")
		}
		dir = parent
	}
}

// buildPopulatedV1Registry creates a V1 WorkflowRegistry populated from V2
// templates — the same logic used in main(). This ensures the contract test
// validates against the same data source as the actual code generator.
func buildPopulatedV1Registry() *workflow.WorkflowRegistry {
	v2Reg := v2.NewTemplateRegistry()
	v2.RegisterBuiltinTemplates(v2Reg)

	v1Reg := workflow.NewWorkflowRegistry()
	for _, typ := range knownV2Types {
		v2Tmpl := v2Reg.Get(typ)
		if v2Tmpl == nil {
			continue
		}
		v1Phases := make([]workflow.PhaseTemplate, 0, len(v2Tmpl.Phases))
		for _, p := range v2Tmpl.Phases {
			v1Phases = append(v1Phases, workflow.PhaseTemplate{
				ID:           p.ID,
				Name:         p.Name,
				NeedsConfirm: p.NeedsConfirm,
				ToolPolicy:   mapV2ToolPolicyToV1(p.ToolPolicy),
			})
		}
		v1Reg.MustRegister(&workflow.WorkflowTemplate{
			Type:        workflow.WorkflowType(v2Tmpl.Type),
			Name:        v2Tmpl.Name,
			Description: v2Tmpl.Description,
			Keywords:    v2Tmpl.Keywords,
			Phases:      v1Phases,
		})
	}
	return v1Reg
}

// TestGeneratedArtifactUpToDate is the Go contract test for Property 5:
// "Generated artifact is up to date".
//
// It regenerates the TypeScript fallback artifact in memory from the live
// workflow registry and asserts that it byte-equals the committed file (after
// line-ending normalization). A failure means a workflow template changed but
// the artifact was not regenerated — the fix is to run `go generate ./...`.
//
// The committed file lives at the repository-root-relative generatedFilePath;
// the on-disk path is resolved by locating the repository root (the directory
// containing go.mod) by walking up from the test's working directory, so the
// test does not depend on a fixed relative offset.
//
// Validates: Requirements 2.3
func TestGeneratedArtifactUpToDate(t *testing.T) {
	inMemory := renderGeneratedTS(buildPopulatedV1Registry())

	committedPath := filepath.Join(findRepoRoot(t), filepath.FromSlash(generatedFilePath))
	onDisk, err := os.ReadFile(committedPath)
	if err != nil {
		t.Fatalf("read committed artifact %s: %v", committedPath, err)
	}

	if normalizeEOL(inMemory) != normalizeEOL(string(onDisk)) {
		t.Fatalf("workflowPhaseMeta.generated.ts is stale; run `go generate ./...` to regenerate %s", generatedFilePath)
	}
}

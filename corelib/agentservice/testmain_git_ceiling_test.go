package agentservice

import (
	"os"
	"testing"
)

// TestMain keeps host git from walking above the temp dir: some development
// hosts version-control their user home, and local coding-runtime workspaces
// created under os.TempDir() would otherwise inherit that parent repository
// (slow untracked-file scans over the whole home tree, racing deletions)
// instead of failing closed or using their own fresh baseline.
func TestMain(m *testing.M) {
	ceiling := os.TempDir()
	if current := os.Getenv("GIT_CEILING_DIRECTORIES"); current != "" {
		ceiling = current + string(os.PathListSeparator) + ceiling
	}
	_ = os.Setenv("GIT_CEILING_DIRECTORIES", ceiling)
	os.Exit(m.Run())
}

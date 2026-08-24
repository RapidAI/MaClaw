package agentservice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The legacy tool surface answers only with a string, so these helpers spell
// their failures "Error: ...". Paired with a nil error by the managed file
// read, an unreadable path arrived at the model as file contents and the turn
// was recorded as a success.
func TestHostFileHelpersReportFailuresThroughTheErrorAndNotTheProse(t *testing.T) {
	dir := t.TempDir()
	cb := &coreAgentCallbacks{workspace: dir}

	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("read missing file", func(t *testing.T) {
		text, err := cb.readFileDetailed(map[string]interface{}{"path": "absent.txt"})
		if err == nil {
			t.Fatalf("a file that could not be read reported itself as contents: %q", text)
		}
	})
	t.Run("read a directory", func(t *testing.T) {
		text, err := cb.readFileDetailed(map[string]interface{}{"path": "sub"})
		if err == nil {
			t.Fatalf("a directory reported itself as file contents: %q", text)
		}
	})
	t.Run("read a real file", func(t *testing.T) {
		text, err := cb.readFileDetailed(map[string]interface{}{"path": "notes.txt"})
		if err != nil {
			t.Fatalf("a readable file reported a failure: %v", err)
		}
		if !strings.Contains(text, "hello") {
			t.Fatalf("contents = %q, want the file body", text)
		}
	})
	t.Run("list a file", func(t *testing.T) {
		text, err := cb.listDirectoryDetailed(map[string]interface{}{"path": "notes.txt"})
		if err == nil {
			t.Fatalf("a plain file reported itself as a directory listing: %q", text)
		}
	})
	t.Run("list a real directory", func(t *testing.T) {
		text, err := cb.listDirectoryDetailed(map[string]interface{}{"path": "sub"})
		if err != nil {
			t.Fatalf("a readable directory reported a failure: %v", err)
		}
		if !strings.Contains(text, "Directory:") {
			t.Fatalf("listing = %q, want the directory header", text)
		}
	})
}

// The legacy callers still take only the string, so splitting the verdict out
// must not have changed a single word of what they see.
func TestHostFileHelpersLeaveTheLegacyProseUntouched(t *testing.T) {
	dir := t.TempDir()
	cb := &coreAgentCallbacks{workspace: dir}

	for _, tc := range []struct {
		name string
		args map[string]interface{}
		text func(map[string]interface{}) string
		pair func(map[string]interface{}) (string, error)
	}{
		{"read", map[string]interface{}{"path": "absent.txt"}, cb.executeReadFile, cb.readFileDetailed},
		{"list", map[string]interface{}{"path": "absent"}, cb.executeListDirectory, cb.listDirectoryDetailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flat := tc.text(tc.args)
			carried, err := tc.pair(tc.args)
			if err == nil {
				t.Fatal("expected the detailed form to carry a failure")
			}
			if flat != carried {
				t.Fatalf("legacy text %q differs from %q", flat, carried)
			}
			if !strings.HasPrefix(flat, "Error: ") {
				t.Fatalf("legacy text = %q, want the wording callers already parse", flat)
			}
		})
	}
}

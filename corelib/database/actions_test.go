package database

import (
	"strings"
	"testing"
)

func TestIsWriteAction(t *testing.T) {
	if IsWriteAction("query") || IsWriteAction("inspect") || IsWriteAction("") {
		t.Fatal("read actions must not be writes")
	}
	for _, action := range []string{"execute", "batch_execute", "write_table", "export_excel", "propose_profile", "EXECUTE"} {
		if !IsWriteAction(action) {
			t.Fatalf("%s should be a write", action)
		}
	}
}

func TestIsReadAction(t *testing.T) {
	if !IsReadAction("query") || !IsReadAction("list_connections") || !IsReadAction("read_table") || !IsReadAction("job_status") || !IsReadAction("explain") || !IsReadAction("list_favorites") {
		t.Fatal("expected read actions")
	}
	if IsReadAction("execute") || IsReadAction("export_excel") {
		t.Fatal("writes are not reads")
	}
}

func TestRefuseWriteForReadOnlyTool(t *testing.T) {
	if _, refused := RefuseWriteForReadOnlyTool(map[string]interface{}{"action": "query"}); refused {
		t.Fatal("query must pass")
	}
	msg, refused := RefuseWriteForReadOnlyTool(map[string]interface{}{"action": "execute"})
	if !refused || !strings.Contains(msg, "read-only") {
		t.Fatalf("execute refuse = %q %v", msg, refused)
	}
}

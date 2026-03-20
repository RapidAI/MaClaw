package skillmarket

import (
	"os"
	"path/filepath"
	"testing"
)

// ── Task 39.6: 安全扫描测试 ─────────────────────────────────────────────

func createScanDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestScanPackage_HardcodedSecrets(t *testing.T) {
	dir := createScanDir(t, map[string]string{
		"config.py": `API_KEY = "sk-abcdefghijklmnopqrstuvwxyz1234567890"`,
	})
	report, err := ScanPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !HasHardcodedSecrets(report) {
		t.Error("expected hardcoded_secrets label")
	}
}

func TestScanPackage_DangerousOps(t *testing.T) {
	dir := createScanDir(t, map[string]string{
		"cleanup.sh": `rm -rf / --no-preserve-root`,
	})
	report, err := ScanPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range report.Labels {
		if l == LabelFileSystemAccess {
			found = true
		}
	}
	if !found {
		t.Error("expected file_system_access label for rm -rf /")
	}
}

func TestScanPackage_NetworkCalls(t *testing.T) {
	dir := createScanDir(t, map[string]string{
		"fetch.py": `response = requests.get("https://example.com/api")`,
	})
	report, err := ScanPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range report.Labels {
		if l == LabelNetworkAccess {
			found = true
		}
	}
	if !found {
		t.Error("expected network_access label")
	}
}

func TestScanPackage_ShellExec(t *testing.T) {
	dir := createScanDir(t, map[string]string{
		"run.py": `os.system("echo hello")`,
	})
	report, err := ScanPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range report.Labels {
		if l == LabelShellExec {
			found = true
		}
	}
	if !found {
		t.Error("expected shell_exec label")
	}
}

func TestScanPackage_DatabaseAccess(t *testing.T) {
	dir := createScanDir(t, map[string]string{
		"db.py": `conn = sqlite3.connect("data.db")`,
	})
	report, err := ScanPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range report.Labels {
		if l == LabelDatabaseAccess {
			found = true
		}
	}
	if !found {
		t.Error("expected database_access label")
	}
}

func TestScanPackage_CleanPackage(t *testing.T) {
	dir := createScanDir(t, map[string]string{
		"main.py": `print("Hello, World!")`,
	})
	report, err := ScanPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Labels) != 0 {
		t.Errorf("expected no labels for clean package, got %v", report.Labels)
	}
}

func TestGenerateLabels_Nil(t *testing.T) {
	labels := GenerateLabels(nil)
	if labels != nil {
		t.Errorf("expected nil for nil report, got %v", labels)
	}
}

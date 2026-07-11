package httpapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMobileSeedMarketJSONSkillCreatesPackage(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "demo.json")
	body := `{
  "id": "demo-skill",
  "name": "演示技能",
  "description": "用于单元测试",
  "version": "1.0.0",
  "author": "test",
  "steps": [
    {"action": "send_input", "params": {"text": "hello"}, "on_error": "stop"}
  ]
}`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := mobileSeedMarketJSONSkill(src, dstDir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("n=%d", n)
	}
	yamlPath := filepath.Join(dstDir, "demo-skill", "skill.yaml")
	if _, err := os.Stat(yamlPath); err != nil {
		t.Fatalf("skill.yaml missing: %v", err)
	}
	// Idempotent: second seed does nothing.
	n2, err := mobileSeedMarketJSONSkill(src, dstDir)
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Fatalf("second seed n=%d, want 0", n2)
	}
}

func TestMobileDirLooksLikeSkillPackage(t *testing.T) {
	dir := t.TempDir()
	if mobileDirLooksLikeSkillPackage(dir) {
		t.Fatal("empty dir should not look like skill package")
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: x\nstatus: active\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !mobileDirLooksLikeSkillPackage(dir) {
		t.Fatal("skill.yaml should make package")
	}
}

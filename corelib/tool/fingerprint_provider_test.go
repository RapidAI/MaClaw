package tool

import (
	"testing"
	"time"
)

func TestComputeFingerprint_Deterministic(t *testing.T) {
	fields1 := map[string]interface{}{
		"host":  "example.com",
		"port":  22,
		"user":  "root",
		"model": "gpt-4",
	}
	fields2 := map[string]interface{}{
		"model": "gpt-4",
		"user":  "root",
		"port":  22,
		"host":  "example.com",
	}

	fp1 := computeFingerprint(fields1)
	fp2 := computeFingerprint(fields2)

	if fp1 == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if fp1 != fp2 {
		t.Fatalf("fingerprints should be deterministic regardless of key order: %q != %q", fp1, fp2)
	}
	if len(fp1) != 16 {
		t.Fatalf("fingerprint should be 16 hex chars, got %d: %q", len(fp1), fp1)
	}
}

func TestComputeFingerprint_EmptyFields(t *testing.T) {
	fp := computeFingerprint(map[string]interface{}{})
	if fp != "" {
		t.Fatalf("expected empty fingerprint for empty fields, got %q", fp)
	}

	fp = computeFingerprint(nil)
	if fp != "" {
		t.Fatalf("expected empty fingerprint for nil fields, got %q", fp)
	}
}

func TestComputeFingerprint_DifferentFields(t *testing.T) {
	fp1 := computeFingerprint(map[string]interface{}{
		"host": "a.com",
		"port": 22,
	})
	fp2 := computeFingerprint(map[string]interface{}{
		"host": "b.com",
		"port": 22,
	})

	if fp1 == fp2 {
		t.Fatal("different config should produce different fingerprints")
	}
}

func TestConfigFingerprintProvider(t *testing.T) {
	provider := &ConfigFingerprintProvider{
		ConfigFieldsFunc: func(toolName string) map[string]interface{} {
			if toolName == "craft_tool" {
				return map[string]interface{}{
					"endpoint": "https://api.openai.com",
					"model":    "gpt-4",
					"api_key":  "sk-xxx",
				}
			}
			return nil
		},
	}

	fp := provider.ComputeFingerprint("craft_tool")
	if fp == "" {
		t.Fatal("expected non-empty fingerprint for configured tool")
	}
	if len(fp) != 16 {
		t.Fatalf("expected 16 hex chars, got %d", len(fp))
	}

	// Unknown tool returns empty
	fp2 := provider.ComputeFingerprint("unknown_tool")
	if fp2 != "" {
		t.Fatalf("expected empty fingerprint for unknown tool, got %q", fp2)
	}

	// Nil func returns empty
	nilProvider := &ConfigFingerprintProvider{ConfigFieldsFunc: nil}
	fp3 := nilProvider.ComputeFingerprint("craft_tool")
	if fp3 != "" {
		t.Fatalf("expected empty fingerprint with nil func, got %q", fp3)
	}
}

func TestSSHFingerprintProvider(t *testing.T) {
	cfg := &StaticSSHHostConfig{
		Host:    "api.example.com",
		Port:    22,
		User:    "root",
		KeyPath: "/home/user/.ssh/id_rsa",
	}

	provider := &SSHFingerprintProvider{
		SSHConfigFunc: func(toolName string) map[string]interface{} {
			if toolName == "ssh" {
				return cfg.SSHFields()
			}
			return nil
		},
	}

	fp := provider.ComputeFingerprint("ssh")
	if fp == "" {
		t.Fatal("expected non-empty fingerprint for SSH tool")
	}
	if len(fp) != 16 {
		t.Fatalf("expected 16 hex chars, got %d", len(fp))
	}

	// Non-ssh tool returns empty
	fp2 := provider.ComputeFingerprint("bash")
	if fp2 != "" {
		t.Fatalf("expected empty fingerprint for non-ssh tool, got %q", fp2)
	}

	// Empty host returns empty via nil fields
	emptyProvider := &SSHFingerprintProvider{
		SSHConfigFunc: func(toolName string) map[string]interface{} {
			return (&StaticSSHHostConfig{}).SSHFields()
		},
	}
	fp3 := emptyProvider.ComputeFingerprint("ssh")
	if fp3 != "" {
		t.Fatalf("expected empty fingerprint for empty host, got %q", fp3)
	}
}

func TestSSHFingerprintProvider_DifferentConfig(t *testing.T) {
	p1 := NewSSHFingerprintProviderFromStatic(func(toolName string) *StaticSSHHostConfig {
		return &StaticSSHHostConfig{Host: "a.com", Port: 22, User: "root"}
	})
	p2 := NewSSHFingerprintProviderFromStatic(func(toolName string) *StaticSSHHostConfig {
		return &StaticSSHHostConfig{Host: "b.com", Port: 22, User: "root"}
	})

	fp1 := p1.ComputeFingerprint("ssh")
	fp2 := p2.ComputeFingerprint("ssh")

	if fp1 == fp2 {
		t.Fatal("different SSH configs should produce different fingerprints")
	}
}

func TestSkillFingerprintProvider(t *testing.T) {
	provider := &SkillFingerprintProvider{
		SkillInfoFunc: func(toolName string) map[string]interface{} {
			if toolName == "manage_skill" {
				return MakeSkillInfoFields("1.2.3", time.Unix(1700000000, 0))
			}
			return nil
		},
	}

	fp := provider.ComputeFingerprint("manage_skill")
	if fp == "" {
		t.Fatal("expected non-empty fingerprint for manage_skill")
	}

	// Non-manage_skill tool returns empty
	fp2 := provider.ComputeFingerprint("bash")
	if fp2 != "" {
		t.Fatalf("expected empty for non-manage_skill tool, got %q", fp2)
	}

	// Nil func returns empty
	nilProvider := &SkillFingerprintProvider{SkillInfoFunc: nil}
	fp3 := nilProvider.ComputeFingerprint("manage_skill")
	if fp3 != "" {
		t.Fatalf("expected empty with nil func, got %q", fp3)
	}

	// Empty version + zero mtime returns empty
	emptyProvider := &SkillFingerprintProvider{
		SkillInfoFunc: func(toolName string) map[string]interface{} {
			return MakeSkillInfoFields("", time.Time{})
		},
	}
	fp4 := emptyProvider.ComputeFingerprint("manage_skill")
	if fp4 != "" {
		t.Fatalf("expected empty for no version/mtime, got %q", fp4)
	}
}

func TestSkillFingerprintProvider_VersionChange(t *testing.T) {
	p1 := &SkillFingerprintProvider{
		SkillInfoFunc: func(toolName string) map[string]interface{} {
			return MakeSkillInfoFields("1.0.0", time.Time{})
		},
	}
	p2 := &SkillFingerprintProvider{
		SkillInfoFunc: func(toolName string) map[string]interface{} {
			return MakeSkillInfoFields("1.0.1", time.Time{})
		},
	}

	fp1 := p1.ComputeFingerprint("manage_skill")
	fp2 := p2.ComputeFingerprint("manage_skill")

	if fp1 == fp2 {
		t.Fatal("different skill versions should produce different fingerprints")
	}
}

func TestFormatSSHScopeToken(t *testing.T) {
	token := FormatSSHScopeToken("root", "api.example.com", 22)
	expected := "host:root@api.example.com:22"
	if token != expected {
		t.Fatalf("expected %q, got %q", expected, token)
	}
}

func TestStaticSSHHostConfig_NoKeyPath(t *testing.T) {
	config := &StaticSSHHostConfig{
		Host: "server.com",
		Port: 2222,
		User: "deploy",
	}

	fields := config.SSHFields()
	if fields == nil {
		t.Fatal("expected non-nil fields")
	}
	if _, ok := fields["key_path"]; ok {
		t.Fatal("key_path should not be present when empty")
	}
	if fields["host"] != "server.com" {
		t.Fatalf("expected host=server.com, got %v", fields["host"])
	}
	if fields["port"] != 2222 {
		t.Fatalf("expected port=2222, got %v", fields["port"])
	}
}

func TestStaticSkillInfo_NonexistentDir(t *testing.T) {
	info := &StaticSkillInfo{
		Version:  "1.0.0",
		SkillDir: "/nonexistent/path/that/does/not/exist",
	}

	fields := info.SkillFields()
	// Version is set but mtime is 0 (dir doesn't exist) — should still return fields
	if fields == nil {
		t.Fatal("expected non-nil fields when version is set")
	}
	if fields["version"] != "1.0.0" {
		t.Fatalf("expected version=1.0.0, got %v", fields["version"])
	}
	if _, ok := fields["mtime"]; ok {
		t.Fatal("mtime should not be present for nonexistent directory")
	}
}

func TestFingerprintProviderInterface(t *testing.T) {
	// Verify all providers implement the interface at compile time
	var _ FingerprintProvider = &ConfigFingerprintProvider{}
	var _ FingerprintProvider = &SkillFingerprintProvider{}
	var _ FingerprintProvider = &SSHFingerprintProvider{}
}

func TestComputeFingerprint_StableAcrossTime(t *testing.T) {
	// Same inputs must produce same output every time
	fields := map[string]interface{}{
		"endpoint": "https://api.openai.com/v1",
		"model":    "gpt-4-turbo",
		"timeout":  30,
	}

	fp1 := computeFingerprint(fields)
	time.Sleep(1 * time.Millisecond) // Ensure time passes
	fp2 := computeFingerprint(fields)

	if fp1 != fp2 {
		t.Fatalf("fingerprint should be stable across time: %q != %q", fp1, fp2)
	}
}

func TestNewSSHFingerprintProviderFromStatic(t *testing.T) {
	provider := NewSSHFingerprintProviderFromStatic(func(toolName string) *StaticSSHHostConfig {
		if toolName == "ssh" {
			return &StaticSSHHostConfig{Host: "test.com", Port: 22, User: "admin"}
		}
		return nil
	})

	fp := provider.ComputeFingerprint("ssh")
	if fp == "" {
		t.Fatal("expected non-empty fingerprint")
	}

	fp2 := provider.ComputeFingerprint("bash")
	if fp2 != "" {
		t.Fatalf("expected empty for non-ssh, got %q", fp2)
	}
}

func TestNewConfigFingerprintProviderFromFields(t *testing.T) {
	provider := NewConfigFingerprintProviderFromFields(func(toolName string) map[string]interface{} {
		if toolName == "craft_tool" {
			return map[string]interface{}{"model": "gpt-4"}
		}
		return nil
	})

	fp := provider.ComputeFingerprint("craft_tool")
	if fp == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if len(fp) != 16 {
		t.Fatalf("expected 16 hex chars, got %d", len(fp))
	}
}

func TestMakeSkillInfoFields(t *testing.T) {
	// Both empty → nil
	fields := MakeSkillInfoFields("", time.Time{})
	if fields != nil {
		t.Fatalf("expected nil for empty inputs, got %v", fields)
	}

	// Version only
	fields = MakeSkillInfoFields("2.0.0", time.Time{})
	if fields == nil {
		t.Fatal("expected non-nil with version")
	}
	if fields["version"] != "2.0.0" {
		t.Fatalf("expected version=2.0.0, got %v", fields["version"])
	}
	if _, ok := fields["mtime"]; ok {
		t.Fatal("mtime should not be present for zero time")
	}

	// Both set
	mt := time.Unix(1700000000, 0)
	fields = MakeSkillInfoFields("3.0.0", mt)
	if fields["version"] != "3.0.0" {
		t.Fatalf("expected version=3.0.0, got %v", fields["version"])
	}
	if fields["mtime"] != int64(1700000000) {
		t.Fatalf("expected mtime=1700000000, got %v", fields["mtime"])
	}
}

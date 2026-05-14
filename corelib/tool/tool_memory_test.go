package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolMemoryStore_LearnAndInject(t *testing.T) {
	s := NewToolMemoryStore("")

	s.LearnRule("ssh", "ssh:api.rapidai.tech:profile", "api.rapidai.tech",
		"连接后需要执行 source /etc/profile 才能使用 go 命令")

	got := s.InjectRules("ssh", []string{"api.rapidai.tech"})
	if !strings.Contains(got, "source /etc/profile") {
		t.Errorf("rule not injected, got: %s", got)
	}
	if !strings.Contains(got, "工具记忆") {
		t.Error("missing header")
	}
}

func TestToolMemoryStore_InjectNoMatch(t *testing.T) {
	s := NewToolMemoryStore("")

	s.LearnRule("ssh", "ssh:api.rapidai.tech:profile", "api.rapidai.tech",
		"连接后需要执行 source /etc/profile")

	// Different context — should not match
	got := s.InjectRules("ssh", []string{"other.server.com"})
	if got != "" {
		t.Errorf("should not inject for unrelated context, got: %s", got)
	}
}

func TestToolMemoryStore_InjectDifferentTool(t *testing.T) {
	s := NewToolMemoryStore("")

	s.LearnRule("ssh", "ssh:host:rule", "host", "some rule")

	// Different tool — should not match
	got := s.InjectRules("bash", []string{"host"})
	if got != "" {
		t.Errorf("should not inject for different tool, got: %s", got)
	}
}

func TestToolMemoryStore_ConfirmBoostsConfidence(t *testing.T) {
	s := NewToolMemoryStore("")

	s.LearnRule("ssh", "key1", "ctx", "rule content")
	rules := s.GetRules("ssh")
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	initial := rules[0].Confidence

	s.ConfirmRule("key1")
	rules = s.GetRules("ssh")
	if rules[0].Confidence <= initial {
		t.Errorf("confidence should increase after confirm: %.2f → %.2f", initial, rules[0].Confidence)
	}
}

func TestToolMemoryStore_FailureReducesConfidence(t *testing.T) {
	s := NewToolMemoryStore("")

	s.LearnRule("ssh", "key1", "ctx", "rule content")
	rules := s.GetRules("ssh")
	initial := rules[0].Confidence

	s.RecordFailure("key1")
	rules = s.GetRules("ssh")
	if rules[0].Confidence >= initial {
		t.Errorf("confidence should decrease after failure: %.2f → %.2f", initial, rules[0].Confidence)
	}
}

func TestToolMemoryStore_PruneRemovesLowConfidence(t *testing.T) {
	s := NewToolMemoryStore("")

	s.LearnRule("ssh", "key1", "ctx", "good rule")
	s.LearnRule("ssh", "key2", "ctx", "bad rule")

	// Drive key2 confidence below threshold
	for i := 0; i < 10; i++ {
		s.RecordFailure("key2")
	}

	pruned := s.Prune()
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}
	rules := s.GetRules("ssh")
	if len(rules) != 1 {
		t.Errorf("expected 1 rule remaining, got %d", len(rules))
	}
	if rules[0].Key != "key1" {
		t.Errorf("wrong rule survived: %s", rules[0].Key)
	}
}

func TestToolMemoryStore_DuplicateLearnBoosts(t *testing.T) {
	s := NewToolMemoryStore("")

	s.LearnRule("ssh", "key1", "ctx", "rule")
	rules := s.GetRules("ssh")
	c1 := rules[0].Confidence

	// Learn same key again — should boost, not duplicate
	s.LearnRule("ssh", "key1", "ctx", "rule updated")
	rules = s.GetRules("ssh")
	if len(rules) != 1 {
		t.Fatalf("duplicate rule created: %d rules", len(rules))
	}
	if rules[0].Confidence <= c1 {
		t.Error("confidence should increase on re-learn")
	}
	if rules[0].Content != "rule updated" {
		t.Error("content should be updated")
	}
}

func TestToolMemoryStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool_rules.json")

	// Create and populate
	s1 := NewToolMemoryStore(path)
	s1.LearnRule("ssh", "key1", "host1", "rule 1")
	s1.LearnRule("bash", "key2", "project", "rule 2")
	s1.Flush()

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}

	// Load from file
	s2 := NewToolMemoryStore(path)
	rules := s2.AllRules()
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules after reload, got %d", len(rules))
	}
}

func TestToolMemoryStore_MaxInjectRules(t *testing.T) {
	s := NewToolMemoryStore("")

	// Add 10 rules for same tool+context
	for i := 0; i < 10; i++ {
		s.LearnRule("ssh", "key"+string(rune('A'+i)), "host",
			"rule "+string(rune('A'+i)))
	}

	got := s.InjectRules("ssh", []string{"host"})
	// Should only inject maxInjectRules (3)
	lines := strings.Split(strings.TrimSpace(got), "\n")
	ruleLines := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "- ") {
			ruleLines++
		}
	}
	if ruleLines > maxInjectRules {
		t.Errorf("injected %d rules, max should be %d", ruleLines, maxInjectRules)
	}
}

func TestToolMemoryStore_NilSafe(t *testing.T) {
	var s *ToolMemoryStore
	// All methods should be nil-safe
	got := s.InjectRules("ssh", []string{"host"})
	if got != "" {
		t.Error("nil store should return empty")
	}
	s.LearnRule("ssh", "k", "c", "r")
	s.ConfirmRule("k")
	s.RecordFailure("k")
	s.Flush()
	if s.Prune() != 0 {
		t.Error("nil prune should return 0")
	}
}

func TestExtractContextKeys_SSH(t *testing.T) {
	keys := ExtractContextKeys("ssh", map[string]interface{}{
		"host":       "api.rapidai.tech",
		"session_id": "ssh_root@api:22_1",
	})
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
	}
	if keys[0] != "api.rapidai.tech" {
		t.Errorf("first key should be host, got %s", keys[0])
	}
}

func TestExtractContextKeys_WebFetch(t *testing.T) {
	keys := ExtractContextKeys("web_fetch", map[string]interface{}{
		"url": "https://www.example.com/path/to/page",
	})
	if len(keys) != 1 || keys[0] != "www.example.com" {
		t.Errorf("expected domain, got %v", keys)
	}
}

func TestExtractContextKeys_WriteFile(t *testing.T) {
	keys := ExtractContextKeys("write_file", map[string]interface{}{
		"path": "/home/user/project/src/main.go",
	})
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	expected := filepath.Dir("/home/user/project/src/main.go")
	if keys[0] != expected {
		t.Errorf("expected %s, got %s", expected, keys[0])
	}
}

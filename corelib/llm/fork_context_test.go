package llm

import (
	"testing"
)

func TestForkableContext_Fork(t *testing.T) {
	prefix := []interface{}{
		map[string]string{"role": "system", "content": "You are a coding assistant."},
	}
	fc := NewForkableContext(prefix)

	fork1 := fc.Fork("task-1")
	fork2 := fc.Fork("task-2")

	fork1.Append(map[string]string{"role": "user", "content": "Write hello world"})
	fork2.Append(map[string]string{"role": "user", "content": "Write fizzbuzz"})

	msgs1 := fork1.BuildMessages()
	msgs2 := fork2.BuildMessages()

	// Both should have prefix + own message
	if len(msgs1) != 2 {
		t.Errorf("fork1 should have 2 messages, got %d", len(msgs1))
	}
	if len(msgs2) != 2 {
		t.Errorf("fork2 should have 2 messages, got %d", len(msgs2))
	}

	// Forks are independent
	fork1.Append(map[string]string{"role": "assistant", "content": "Here's hello world..."})
	if fork1.OwnMessageCount() != 2 {
		t.Errorf("fork1 should have 2 own messages, got %d", fork1.OwnMessageCount())
	}
	if fork2.OwnMessageCount() != 1 {
		t.Errorf("fork2 should still have 1 own message, got %d", fork2.OwnMessageCount())
	}
}

func TestForkableContext_PrefixHash(t *testing.T) {
	prefix := []interface{}{
		map[string]string{"role": "system", "content": "prompt"},
	}
	fc := NewForkableContext(prefix)

	hash := fc.PrefixHash()
	if hash == "" {
		t.Error("hash should not be empty")
	}
	if len(hash) != 16 {
		t.Errorf("hash should be 16 hex chars, got %d: %s", len(hash), hash)
	}

	// Same prefix → same hash
	fc2 := NewForkableContext(prefix)
	if fc2.PrefixHash() != hash {
		t.Error("same prefix should produce same hash")
	}

	// Different prefix → different hash
	fc3 := NewForkableContext([]interface{}{
		map[string]string{"role": "system", "content": "different prompt"},
	})
	if fc3.PrefixHash() == hash {
		t.Error("different prefix should produce different hash")
	}
}

func TestForkableContext_CacheHint(t *testing.T) {
	prefix := []interface{}{
		map[string]string{"role": "system", "content": "prompt"},
		map[string]string{"role": "user", "content": "context"},
	}
	fc := NewForkableContext(prefix)
	fork := fc.Fork("t1")
	fork.Append(map[string]string{"role": "user", "content": "question"})

	hint := fork.CacheHint()
	if !hint.HasCacheablePrefix() {
		t.Error("should have cacheable prefix")
	}
	if hint.PrefixLen != 2 {
		t.Errorf("prefix len should be 2, got %d", hint.PrefixLen)
	}
	if hint.TotalLen != 3 {
		t.Errorf("total len should be 3, got %d", hint.TotalLen)
	}
}

func TestForkedContext_Clear(t *testing.T) {
	fc := NewForkableContext([]interface{}{
		map[string]string{"role": "system", "content": "prompt"},
	})
	fork := fc.Fork("t1")
	fork.Append(map[string]string{"role": "user", "content": "msg1"})
	fork.Append(map[string]string{"role": "user", "content": "msg2"})

	if fork.OwnMessageCount() != 2 {
		t.Fatal("should have 2 messages before clear")
	}

	fork.Clear()
	if fork.OwnMessageCount() != 0 {
		t.Error("should have 0 messages after clear")
	}

	// Prefix still accessible
	msgs := fork.BuildMessages()
	if len(msgs) != 1 {
		t.Errorf("after clear, should only have prefix (1), got %d", len(msgs))
	}
}

func TestForkableContext_UpdatePrefix(t *testing.T) {
	fc := NewForkableContext([]interface{}{
		map[string]string{"role": "system", "content": "old"},
	})
	oldHash := fc.PrefixHash()

	fc.UpdatePrefix([]interface{}{
		map[string]string{"role": "system", "content": "new"},
	})

	if fc.PrefixHash() == oldHash {
		t.Error("hash should change after prefix update")
	}

	// Existing forks see the new prefix
	fork := fc.Fork("t1")
	msgs := fork.BuildMessages()
	if len(msgs) != 1 {
		t.Fatal("should have 1 prefix message")
	}
}

func TestForkedContext_NilSafe(t *testing.T) {
	var f *ForkedContext
	f.Append(map[string]string{"role": "user", "content": "test"})
	if f.BuildMessages() != nil {
		t.Error("nil fork should return nil messages")
	}
	if f.OwnMessageCount() != 0 {
		t.Error("nil fork should have 0 messages")
	}
	f.Clear() // should not panic

	var fc *ForkableContext
	if fc.PrefixHash() != "" {
		t.Error("nil context should return empty hash")
	}
	fork := fc.Fork("x")
	if fork == nil {
		t.Error("fork from nil context should return non-nil ForkedContext")
	}
}

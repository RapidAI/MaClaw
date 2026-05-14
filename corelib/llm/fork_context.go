package llm

// ForkContext: shared prefix context for KV-cache reuse across sub-agents.
// Inspired by OpenHuman's agent/harness/fork_context.rs.
//
// When multiple sub-agents share the same system prompt + tool definitions +
// memory context (the "cacheable prefix"), the LLM provider can reuse the
// KV-cache from the first request, dramatically reducing prefill latency
// for subsequent requests.
//
// Architecture:
//   ForkableContext (shared prefix)
//     ├── Fork("task-1") → ForkedContext (own conversation)
//     ├── Fork("task-2") → ForkedContext (own conversation)
//     └── Fork("task-3") → ForkedContext (own conversation)
//
// Each ForkedContext prepends the shared prefix to its own messages.
// The prefix hash is sent as a cache hint to providers that support it
// (Anthropic cache_control, OpenAI cached_tokens).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
)

// ForkableContext holds a shared prefix that can be forked into
// independent conversation contexts.
type ForkableContext struct {
	mu     sync.RWMutex
	prefix []interface{} // system prompt + tool context + memory
	hash   string        // SHA-256 of serialized prefix (for cache matching)
}

// NewForkableContext creates a context with the given shared prefix messages.
func NewForkableContext(prefix []interface{}) *ForkableContext {
	fc := &ForkableContext{
		prefix: prefix,
	}
	fc.hash = fc.computeHash()
	return fc
}

// PrefixHash returns the hash of the shared prefix.
// Providers can use this to identify cache-eligible requests.
func (fc *ForkableContext) PrefixHash() string {
	if fc == nil {
		return ""
	}
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.hash
}

// PrefixLen returns the number of messages in the shared prefix.
func (fc *ForkableContext) PrefixLen() int {
	if fc == nil {
		return 0
	}
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return len(fc.prefix)
}

// Fork creates an independent conversation context that shares the prefix.
func (fc *ForkableContext) Fork(id string) *ForkedContext {
	if fc == nil {
		return &ForkedContext{id: id}
	}
	return &ForkedContext{
		id:     id,
		parent: fc,
	}
}

// UpdatePrefix replaces the shared prefix (invalidates cache).
func (fc *ForkableContext) UpdatePrefix(prefix []interface{}) {
	if fc == nil {
		return
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	fc.prefix = prefix
	fc.hash = fc.computeHashLocked()
}

func (fc *ForkableContext) computeHash() string {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return fc.computeHashLocked()
}

func (fc *ForkableContext) computeHashLocked() string {
	data, _ := json.Marshal(fc.prefix)
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8]) // 16-char hex prefix is enough for cache matching
}

// ForkedContext is an independent conversation that shares a parent prefix.
type ForkedContext struct {
	id       string
	parent   *ForkableContext
	mu       sync.Mutex
	messages []interface{} // this fork's own messages (after prefix)
}

// ID returns the fork identifier.
func (f *ForkedContext) ID() string {
	if f == nil {
		return ""
	}
	return f.id
}

// Append adds a message to this fork's conversation.
func (f *ForkedContext) Append(msg interface{}) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = append(f.messages, msg)
}

// BuildMessages returns the full message list (prefix + fork's own messages).
// This is what gets sent to the LLM.
func (f *ForkedContext) BuildMessages() []interface{} {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	ownMsgs := make([]interface{}, len(f.messages))
	copy(ownMsgs, f.messages)
	f.mu.Unlock()

	if f.parent == nil {
		return ownMsgs
	}

	f.parent.mu.RLock()
	prefix := make([]interface{}, len(f.parent.prefix))
	copy(prefix, f.parent.prefix)
	f.parent.mu.RUnlock()

	return append(prefix, ownMsgs...)
}

// OwnMessageCount returns the number of messages in this fork (excluding prefix).
func (f *ForkedContext) OwnMessageCount() int {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.messages)
}

// TotalMessageCount returns prefix + own messages count.
func (f *ForkedContext) TotalMessageCount() int {
	if f == nil {
		return 0
	}
	prefixLen := 0
	if f.parent != nil {
		prefixLen = f.parent.PrefixLen()
	}
	return prefixLen + f.OwnMessageCount()
}

// CacheHint returns metadata that providers can use for KV-cache matching.
func (f *ForkedContext) CacheHint() CacheHint {
	if f == nil || f.parent == nil {
		return CacheHint{}
	}
	return CacheHint{
		PrefixHash:   f.parent.PrefixHash(),
		PrefixLen:    f.parent.PrefixLen(),
		TotalLen:     f.TotalMessageCount(),
	}
}

// CacheHint provides information for LLM providers to match KV-cache.
type CacheHint struct {
	PrefixHash string // hash of the shared prefix
	PrefixLen  int    // number of messages in prefix
	TotalLen   int    // total messages (prefix + own)
}

// HasCacheablePrefix returns true if there's a meaningful shared prefix.
func (h CacheHint) HasCacheablePrefix() bool {
	return h.PrefixHash != "" && h.PrefixLen > 0
}

// Clear resets this fork's conversation (keeps prefix).
func (f *ForkedContext) Clear() {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.messages = nil
}

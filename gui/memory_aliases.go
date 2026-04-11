package main

// This file bridges GUI code to the unified corelib/memory package.
// Type aliases allow existing GUI code to compile with minimal changes.

import (
	"crypto/rand"
	"fmt"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// Type aliases — GUI code can keep using the old names.
type MemoryStore = memory.Store
type MemoryEntry = memory.Entry
type MemoryCategory = memory.Category
type MemoryHealthReport = memory.HealthReport

// Category constant aliases.
const (
	MemCategorySelfIdentity        = memory.CategorySelfIdentity
	MemCategoryUserFact            = memory.CategoryUserFact
	MemCategoryPreference          = memory.CategoryPreference
	MemCategoryProjectKnowledge    = memory.CategoryProjectKnowledge
	MemCategoryInstruction         = memory.CategoryInstruction
	MemCategoryConversationSummary = memory.CategoryConversationSummary
	MemCategorySessionCheckpoint   = memory.CategorySessionCheckpoint
)

// NewMemoryStore delegates to the corelib constructor.
var NewMemoryStore = memory.NewStore

// generateID produces a unique ID (used by scheduled_task.go and others).
func generateID() string {
	var buf [2]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("%d-%04x", time.Now().UnixNano(), int(buf[0])<<8|int(buf[1]))
}

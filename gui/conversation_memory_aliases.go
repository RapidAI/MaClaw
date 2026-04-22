package main

// conversation_memory_aliases.go provides type aliases so that existing gui/
// code continues to compile unchanged after the conversation memory types
// were migrated to corelib/agent/.
//
// This is Phase 1 Step 2 of the agent-unification plan.

import "github.com/RapidAI/CodeClaw/corelib/agent"

// Type aliases — gui code keeps using the old lowercase names.
type conversationEntry = agent.ConversationEntry
type unfinishedTaskSlot = agent.UnfinishedTaskSlot
type conversationMemory = agent.ConversationMemory

// Function aliases.
var (
	newConversationMemory           = agent.NewConversationMemory
	newPersistentConversationMemory = agent.NewPersistentConversationMemory
	cloneUnfinishedTaskSlot         = agent.CloneUnfinishedTaskSlot
)

// Constant aliases.
const (
	maxConversationTurns   = agent.MaxConversationTurns
	maxMemoryTokenEstimate = agent.MaxMemoryTokenEstimate
)

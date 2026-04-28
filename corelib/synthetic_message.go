package corelib

import "strings"

// SyntheticUserMessagePrefixes lists prefixes that identify user-role messages
// injected by the framework rather than typed by the actual user. These include
// SubAgent context, compaction recovery prompts, system notifications, and
// other framework-generated messages.
//
// This is the single source of truth — consumed by:
//   - gui/im_conversation_trim.go (fork-turn boundary classification)
//   - corelib/memory/knowledge_extractor.go (memory extraction filtering)
//
// When adding a new system-injected user message format, add its prefix here.
var SyntheticUserMessagePrefixes = []string{
	"__SUBAGENT_CONTEXT__",
	"[上下文恢复]",
	"[对话摘要]",
	"[系统通知]",
	"[收尾要求]",
	"[Recover 阶段]",
	"[Skill 优先要求]",
	"[用户补充]",
	"[SubAgent Task]",
}

// IsSyntheticUserContent returns true if the text starts with any known
// framework-injected prefix. Used to distinguish real user messages from
// system-generated ones in conversation history processing.
func IsSyntheticUserContent(text string) bool {
	for _, prefix := range SyntheticUserMessagePrefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

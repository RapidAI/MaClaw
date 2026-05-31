package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/steering"
)

// steeringFileKey builds a per-user key for the steeringContextFiles map.
func steeringFileKey(userID, path string) string {
	return userID + "\x00" + path
}

// appendSteeringSection injects resolved steering file content into the
// system prompt. Files are resolved based on the current context (user
// message, context files, manual references) and subject to token budget.
//
// Injection point: after core identity/principles, before memory section.
// This ensures steering rules have high priority in the LLM's attention
// but don't override core identity.
func (h *IMMessageHandler) appendSteeringSection(b *strings.Builder, userMessage string) {
	if h.getSteeringStore() == nil {
		return
	}

	cfg := h.getMaclawLLMConfig()
	userID := h.promptRuntimeUserID(nil)

	ctx := steering.ResolveContext{
		UserMessage:            userMessage,
		ContextFiles:           h.getSteeringContextFiles(userID),
		ManualRefs:             h.extractSteeringRefs(userMessage),
		EffectiveContextTokens: cfg.EffectiveContextTokens(),
	}

	resolved := h.getSteeringStore().Resolve(ctx)
	if len(resolved) == 0 {
		return
	}

	b.WriteString("\n\n## 用户规则（Steering）\n")
	for _, sf := range resolved {
		b.WriteString(fmt.Sprintf("\n### %s\n", strings.TrimSuffix(sf.Name, ".md")))
		b.WriteString(sf.Content)
		if !strings.HasSuffix(sf.Content, "\n") {
			b.WriteString("\n")
		}
	}

	log.Printf("[steering] injected %d files into system prompt", len(resolved))
}

// getSteeringContextFiles returns file paths for the given user,
// used for fileMatch steering resolution.
func (h *IMMessageHandler) getSteeringContextFiles(userID string) []string {
	prefix := userID + "\x00"
	var files []string
	h.steeringContextFiles.Range(func(key, _ interface{}) bool {
		if k, ok := key.(string); ok && strings.HasPrefix(k, prefix) {
			files = append(files, k[len(prefix):])
		}
		return true
	})
	return files
}

// trackSteeringFile records a file path for fileMatch steering resolution.
// Per-user via composite key. Thread-safe via sync.Map.
func (h *IMMessageHandler) trackSteeringFile(path string) {
	if path == "" {
		return
	}
	userID := h.promptRuntimeUserID(nil)
	if userID == "" {
		userID = "_default"
	}
	h.steeringContextFiles.Store(steeringFileKey(userID, path), true)
}

// clearSteeringContextFiles clears the accumulated file context for a user.
// Called by clearPerUserSessionState when starting a new conversation.
func (h *IMMessageHandler) clearSteeringContextFiles(userID string) {
	prefix := userID + "\x00"
	h.steeringContextFiles.Range(func(key, _ interface{}) bool {
		if k, ok := key.(string); ok && strings.HasPrefix(k, prefix) {
			h.steeringContextFiles.Delete(key)
		}
		return true
	})
}

// extractSteeringRefs extracts #name references from the user message
// for manual steering file inclusion.
// Returns the referenced names (without # prefix, without .md extension).
//
// Uses rune scanning instead of space-based tokenization to handle
// CJK text where words are not separated by spaces (e.g. "查看#ssh规则").
func (h *IMMessageHandler) extractSteeringRefs(text string) []string {
	if text == "" {
		return nil
	}
	var refs []string
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		if runes[i] != '#' {
			i++
			continue
		}
		j := i + 1
		for j < len(runes) {
			r := runes[j]
			if r >= 0x4e00 && r <= 0x9fff { // CJK Unified Ideographs
				j++
			} else if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				j++
			} else {
				break
			}
		}
		if j > i+1 {
			name := string(runes[i+1 : j])
			allDigits := true
			for _, r := range name {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if !allDigits {
				refs = append(refs, name)
			}
		}
		i = j
	}
	return refs
}

// trackSteeringFileFromArgs extracts file paths from tool call arguments
// and records them for fileMatch steering resolution.
//
// Mechanism: scans for common file path parameter names instead of
// hardcoding tool names. Automatically covers new tools and MCP tools.
func (h *IMMessageHandler) trackSteeringFileFromArgs(toolName string, args map[string]interface{}) {
	fileParamNames := []string{"path", "file_path", "file", "local_path", "source", "destination"}
	for _, key := range fileParamNames {
		if val, ok := args[key].(string); ok && val != "" && looksLikeFilePath(val) {
			h.trackSteeringFile(val)
		}
	}
}

// looksLikeFilePath returns true if the string looks like a filesystem path
// rather than a URL, hostname, or other string value.
func looksLikeFilePath(s string) bool {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "ftp://") {
		return false
	}
	hasSep := strings.ContainsAny(s, "/\\")
	hasDot := strings.Contains(s, ".")
	if hasDot && !hasSep {
		return false // likely a hostname like "api.example.com"
	}
	return hasSep || hasDot
}

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const maxTruncationRetries = 3

const maxEssentialTruncationHints = 1

const (
	// maxAgentLoopInlineWriteFileContentRunes is a soft threshold for logging only.
	// write_file calls exceeding this are auto-passed to the backend handler (actual limit is writeFileMaxSize=1MB).
	// The schema no longer declares maxLength for write_file to avoid LLM refusing to call the tool for long content.
	maxAgentLoopInlineWriteFileContentRunes = 1800
	maxAgentLoopInlineEditContentRunes      = 1800
	maxAgentLoopInlineBashCommandRunes      = 4000
	maxAgentLoopInlineSSHCommandRunes       = 4000
)

type agentLoopTruncationRecoveryResult struct {
	Conversation []interface{}
	Tools        []map[string]interface{}
	ContinueLoop bool
}

func logAgentLoopPartialTruncation(choice llm.Choice) {
	if len(choice.TruncatedToolNames) == 0 {
		return
	}
	log.Printf("[agent-loop] partial truncation: %d tool call(s) removed (%s), %d valid call(s) proceeding",
		len(choice.TruncatedToolNames), strings.Join(choice.TruncatedToolNames, ", "), len(choice.Message.ToolCalls))
}

func resetAgentLoopTruncationRecoveryAfterToolCalls(phase *agentLoopPhase, choice llm.Choice) {
	if phase == nil || len(choice.Message.ToolCalls) == 0 || len(choice.TruncatedToolNames) > 0 {
		return
	}
	if phase.TruncationRetries == 0 && phase.EssentialTruncationHints == 0 {
		return
	}
	// NOTE: This reset does NOT clear TruncationBlockedTools — once blocked, a tool
	// stays blocked for the remainder of the agent loop. The reset only affects retry
	// counters so that a future different tool truncation gets fresh retries.
	log.Printf("[agent-loop] reset truncation recovery counters after valid tool call branch (retries=%d essential_hints=%d blocked_tools=%v)",
		phase.TruncationRetries, phase.EssentialTruncationHints, blockedToolNames(phase))
	phase.TruncationRetries = 0
	phase.EssentialTruncationHints = 0
}

func blockedToolNames(phase *agentLoopPhase) []string {
	if phase == nil || len(phase.TruncationBlockedTools) == 0 {
		return nil
	}
	names := make([]string, 0, len(phase.TruncationBlockedTools))
	for name := range phase.TruncationBlockedTools {
		names = append(names, name)
	}
	return names
}

func (h *IMMessageHandler) handleAgentLoopTruncatedToolCalls(
	iteration int,
	choice llm.Choice,
	phase *agentLoopPhase,
	conversation []interface{},
	tools []map[string]interface{},
	fallbackCatalog []map[string]interface{},
	recordSystemMessages func(int, []interface{}),
) agentLoopTruncationRecoveryResult {
	result := agentLoopTruncationRecoveryResult{Conversation: conversation, Tools: tools}
	if len(choice.TruncatedToolNames) == 0 || phase == nil {
		return result
	}
	result.ContinueLoop = true

	// Best-effort partial write: if write_file was truncated and we have
	// the raw (incomplete) args, extract path+content and write to disk.
	// This converts a failed call into a partially successful one.
	if rawArgs, ok := truncatedToolArgsForName(choice.TruncatedToolArgs, "write_file"); ok && rawArgs != "" {
		workingDir := h.getEffectiveWorkingDir()
		if pw := attemptPartialWriteFile(rawArgs, workingDir); pw != nil {
			phase.ConsecutiveNoTool = 0
			phase.TruncationRetries = 0 // reset — we made progress
			hint := buildPartialWriteHint(pw)
			systemMessagesStart := len(conversation)
			conversation = append(conversation, map[string]string{
				"role":    "system",
				"content": hint,
			})
			recordSystemMessages(systemMessagesStart, conversation)
			result.Conversation = conversation
			log.Printf("[agent-loop] partial write success: %s (%d bytes), guiding LLM to append remaining content",
				pw.Path, pw.BytesWritten)
			return result
		}
		// Partial write was not possible. If the file already exists (from a
		// previous partial write), inject a targeted hint to use mode=append
		// instead of falling through to the generic essential-tool hint.
		extractedPath := extractJSONStringField(rawArgs, "path")
		if extractedPath != "" {
			resolvedPath, pathOK := resolveTruncationPartialWritePath(extractedPath, workingDir)
			if pathOK {
				if info, statErr := os.Stat(resolvedPath); statErr == nil && info.Size() > 0 {
					phase.ConsecutiveNoTool = 0
					hint := fmt.Sprintf(
						"[system] write_file was truncated again. The file %q already exists (%d bytes from a previous partial write). "+
							"Do NOT use mode=overwrite — use write_file(path=%q, mode=\"append\", content=\"...remaining...\") to continue from where you left off. "+
							"Keep each chunk under 3000 characters.",
						resolvedPath, info.Size(), resolvedPath)
					systemMessagesStart := len(conversation)
					conversation = append(conversation, map[string]string{
						"role":    "system",
						"content": hint,
					})
					recordSystemMessages(systemMessagesStart, conversation)
					result.Conversation = conversation
					log.Printf("[agent-loop] partial write refused (file exists %d bytes), injecting append hint for %s",
						info.Size(), resolvedPath)
					return result
				}
			}
		}
	}

	if allTruncatedToolsPreservedAfterTruncation(choice.TruncatedToolNames) {
		return h.handleAgentLoopEssentialTruncatedToolCalls(iteration, choice, phase, conversation, tools, recordSystemMessages)
	}
	if phase.TruncationRetries < maxTruncationRetries {
		phase.TruncationRetries++
		phase.ConsecutiveNoTool = 0
		truncatedList := strings.Join(choice.TruncatedToolNames, ", ")
		log.Printf("[agent-loop] truncated tool call recovery (retry %d/%d, iter=%d, tools=%s), injecting hint as system message",
			phase.TruncationRetries, maxTruncationRetries, iteration, truncatedList)
		hint := buildTruncationRetryHint(truncatedList, tools)
		systemMessagesStart := len(conversation)
		conversation = append(conversation, map[string]string{
			"role":    "system",
			"content": hint,
		})
		recordSystemMessages(systemMessagesStart, conversation)
		result.Conversation = conversation
		return result
	}

	if phase.TruncationBlockedTools == nil {
		phase.TruncationBlockedTools = make(map[string]bool)
	}
	var newlyBlocked []string
	for _, tn := range choice.TruncatedToolNames {
		if classifyAgentToolKind(tn).PreserveAfterTruncation() {
			log.Printf("[agent-loop] skipping truncation block for essential tool %q (iter=%d)", tn, iteration)
			continue
		}
		if !phase.TruncationBlockedTools[tn] {
			phase.TruncationBlockedTools[tn] = true
			newlyBlocked = append(newlyBlocked, tn)
		}
	}
	if len(newlyBlocked) == 0 {
		log.Printf("[agent-loop] truncated tool call: no new tools to block (tools=%v, already_blocked=%v, iter=%d)",
			choice.TruncatedToolNames, phase.TruncationBlockedTools, iteration)
		result.ContinueLoop = false
		return result
	}

	phase.ConsecutiveNoTool = 0
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, td := range tools {
		name := tool.ExtractToolName(td)
		if !phase.TruncationBlockedTools[name] {
			filtered = append(filtered, td)
		}
	}
	filtered = ensureTruncationFallbackTools(filtered, fallbackCatalog, newlyBlocked, phase.TruncationBlockedTools)
	result.Tools = filtered
	blockedList := strings.Join(newlyBlocked, ", ")
	log.Printf("[agent-loop] truncation retries exhausted: blocking tools [%s] from LLM tool list (iter=%d, remaining_tools=%d)",
		blockedList, iteration, len(filtered))

	hint := fmt.Sprintf("[system hint] %sBlocked tools are not available now; use an alternate tool path.", buildTruncationBlockAlternativeInstructions(newlyBlocked, filtered))
	systemMessagesStart := len(conversation)
	conversation = append(conversation, map[string]string{
		"role":    "system",
		"content": hint,
	})
	recordSystemMessages(systemMessagesStart, conversation)
	result.Conversation = conversation
	return result
}

func allTruncatedToolsPreservedAfterTruncation(names []string) bool {
	if len(names) == 0 {
		return false
	}
	for _, name := range names {
		if !classifyAgentToolKind(name).PreserveAfterTruncation() {
			return false
		}
	}
	return true
}

func (h *IMMessageHandler) handleAgentLoopEssentialTruncatedToolCalls(
	iteration int,
	choice llm.Choice,
	phase *agentLoopPhase,
	conversation []interface{},
	tools []map[string]interface{},
	recordSystemMessages func(int, []interface{}),
) agentLoopTruncationRecoveryResult {
	result := agentLoopTruncationRecoveryResult{Conversation: conversation, Tools: tools}
	if phase.EssentialTruncationHints >= maxEssentialTruncationHints {
		// Hints exhausted and the model still cannot complete the tool call JSON.
		// Override the "essential" protection: block the tool and force an alternative path.
		// This prevents the agent loop from finalizing with incomplete work.
		if phase.TruncationBlockedTools == nil {
			phase.TruncationBlockedTools = make(map[string]bool)
		}
		var newlyBlocked []string
		for _, tn := range choice.TruncatedToolNames {
			if !phase.TruncationBlockedTools[tn] {
				phase.TruncationBlockedTools[tn] = true
				newlyBlocked = append(newlyBlocked, tn)
			}
		}
		if len(newlyBlocked) == 0 {
			// Tools already blocked but LLM still produced a truncated call or
			// a promise-only text response. Inject a lightweight reminder so the
			// LLM knows to use bash + Python instead. Limit to 2 reminders to
			// prevent infinite loops; after that, fall through to no-tool finalize.
			const maxBlockedReminders = 2
			if phase.TruncationBlockedReminders < maxBlockedReminders {
				phase.TruncationBlockedReminders++
				blockedNames := make([]string, 0, len(phase.TruncationBlockedTools))
				for tn := range phase.TruncationBlockedTools {
					blockedNames = append(blockedNames, tn)
				}
				hint := fmt.Sprintf("[system] %s is currently disabled (output too long for JSON). Use bash with a Python script instead. Example: bash(command=\"python3 -c \\\"import pathlib; pathlib.Path('file.py').write_text(content, encoding='utf-8')\\\"\")",
					strings.Join(blockedNames, ", "))
				systemMessagesStart := len(conversation)
				conversation = append(conversation, map[string]string{
					"role":    "system",
					"content": hint,
				})
				recordSystemMessages(systemMessagesStart, conversation)
				result.Conversation = conversation
				result.ContinueLoop = true
				phase.ConsecutiveNoTool = 0
				log.Printf("[agent-loop] essential tool truncation: injecting blocked reminder %d/%d (iter=%d)", phase.TruncationBlockedReminders, maxBlockedReminders, iteration)
				return result
			}
			log.Printf("[agent-loop] essential tool truncation: reminders exhausted, allowing no-tool recovery path (iter=%d)", iteration)
			return result
		}
		// Remove blocked tools from the tool list
		var filtered []map[string]interface{}
		for _, td := range tools {
			name := tool.ExtractToolName(td)
			if !phase.TruncationBlockedTools[name] {
				filtered = append(filtered, td)
			}
		}
		result.Tools = filtered
		blockedList := strings.Join(newlyBlocked, ", ")
		log.Printf("[agent-loop] essential tool truncation override: blocking %s after %d failed hints (iter=%d, remaining_tools=%d)",
			blockedList, phase.EssentialTruncationHints, iteration, len(filtered))
		// Inject a strong system message forcing alternative approach
		hint := fmt.Sprintf(
			"[system] %s has been temporarily disabled because arguments were repeatedly truncated (model cannot finish the JSON). "+
				"Use bash with a Python/Node script to write files instead. Example: bash(command=\"python3 -c \\\"import pathlib; pathlib.Path('output.py').write_text(content, encoding='utf-8')\\\"\")",
			blockedList)
		systemMessagesStart := len(conversation)
		conversation = append(conversation, map[string]string{
			"role":    "system",
			"content": hint,
		})
		recordSystemMessages(systemMessagesStart, conversation)
		result.Conversation = conversation
		result.ContinueLoop = true
		phase.ConsecutiveNoTool = 0
		return result
	}
	phase.EssentialTruncationHints++
	phase.ConsecutiveNoTool = 0
	truncatedList := strings.Join(choice.TruncatedToolNames, ", ")
	log.Printf("[agent-loop] essential tool truncation recovery (hint %d/%d, iter=%d, tools=%s)", phase.EssentialTruncationHints, maxEssentialTruncationHints, iteration, truncatedList)
	hint := buildEssentialTruncationRecoveryHint(choice.TruncatedToolNames)
	systemMessagesStart := len(conversation)
	conversation = append(conversation, map[string]string{
		"role":    "system",
		"content": hint,
	})
	recordSystemMessages(systemMessagesStart, conversation)
	result.Conversation = conversation
	result.ContinueLoop = true
	return result
}

func buildEssentialTruncationRecoveryHint(toolNames []string) string {
	truncatedList := strings.Join(toolNames, ", ")
	parts := []string{
		fmt.Sprintf("[system hint] Tool call arguments were truncated for essential tool(s): %s.", truncatedList),
		"Keep those tools available, but do not send oversized inline payloads.",
		agentLoopInlinePayloadLimitInstruction(),
	}
	if containsToolName(toolNames, "delegate_task") {
		parts = append(parts, "For delegate_task, keep request concise: reference the approved workflow task IDs and existing context instead of embedding long documents or file contents.")
	}
	if containsToolName(toolNames, "bash") {
		parts = append(parts, "Use bash only for short commands.")
	}
	if containsToolName(toolNames, "write_file") {
		parts = append(parts, "For write_file, keep the tool available and regenerate a complete JSON object; if the content is very large, split it across overwrite/append calls so the model can finish each argument. Alternatively, use edit_lines for targeted insertions/replacements (no content size limit).")
	}
	return strings.Join(parts, " ")
}

func containsToolName(names []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, name := range names {
		if strings.TrimSpace(name) == want {
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) truncationFallbackToolCatalog(ctx *LoopContext, userID string, phase *agentLoopPhase, baseTools []map[string]interface{}) []map[string]interface{} {
	if h == nil || len(baseTools) == 0 {
		return nil
	}
	catalog := append([]map[string]interface{}(nil), baseTools...)
	phaseValue := derefAgentLoopPhase(phase)
	if phaseValue.ForceSkillPreference {
		if shouldRestrictToSkillSearch(phaseValue) {
			catalog = filterToolsForRemoteSkillSearch(catalog)
		} else {
			catalog = filterToolsForSkillPreference(catalog)
		}
	}
	if policyOwnerID, applyFilter := h.workflowToolFilterOwnerAndDecision(userID, ctx); applyFilter {
		catalog = h.applyWorkflowToolFilterWithCatalog(policyOwnerID, catalog, h.getTools())
	}
	return catalog
}

func ensureTruncationFallbackTools(tools []map[string]interface{}, catalog []map[string]interface{}, newlyBlocked []string, blockedSet map[string]bool) []map[string]interface{} {
	want := truncationFallbackToolNames(newlyBlocked)
	if len(want) == 0 || len(catalog) == 0 {
		return tools
	}

	present := make(map[string]bool, len(tools))
	for _, td := range tools {
		if name := tool.ExtractToolName(td); name != "" {
			present[name] = true
		}
	}

	byName := make(map[string]map[string]interface{}, len(catalog))
	for _, td := range catalog {
		if name := tool.ExtractToolName(td); name != "" {
			byName[name] = td
		}
	}

	out := tools
	for _, name := range want {
		if blockedSet[name] {
			continue
		}
		if present[name] {
			continue
		}
		if td, ok := byName[name]; ok {
			out = append(out, td)
			present[name] = true
		}
	}
	return out
}

func truncationFallbackToolNames(blocked []string) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	for _, tn := range blocked {
		switch classifyAgentToolKind(tn) {
		case agentToolKindWriteFile, agentToolKindEditFile, agentToolKindGeneratePDF, agentToolKindOffice:
			add("craft_tool")
		}
	}
	return names
}

func buildTruncationRetryHint(truncatedList string, tools []map[string]interface{}) string {
	alternatives := []string{agentLoopInlinePayloadLimitInstruction()}
	if toolListContainsName(tools, "write_file") {
		alternatives = append(alternatives, "For large file content, use write_file once with mode=overwrite for the first chunk, then mode=append for later chunks.")
	}
	if toolListContainsName(tools, "craft_tool") {
		alternatives = append(alternatives, "craft_tool is available, so you may use it to generate a local script.")
	}
	if toolListContainsName(tools, "bash") {
		alternatives = append(alternatives, "bash is available only for short commands; do not embed generated file bodies in bash arguments.")
	}
	if len(alternatives) == 1 {
		alternatives = append(alternatives, "Use only tools available in the current tool list.")
	}
	return fmt.Sprintf("[system hint] Tool call arguments were incomplete or truncated: %s. %s", truncatedList, strings.Join(alternatives, " "))
}

func agentLoopInlinePayloadLimitInstruction() string {
	return fmt.Sprintf("Respect inline payload limits: bash.command <= %d runes per call. write_file has no backend content limit; write_file/edit_file/edit_lines have no content size limit, but their JSON arguments must still be complete within the model's output token budget; for very large content, split across multiple calls.", maxAgentLoopInlineBashCommandRunes)
}

func buildTruncationBlockAlternativeInstructions(blocked []string, availableTools []map[string]interface{}) string {
	var instructions string
	for _, tn := range blocked {
		instructions += truncationBlockInstructionForAvailableTools(tn, availableTools)
	}
	return instructions
}

func truncationBlockInstructionForAvailableTools(toolName string, availableTools []map[string]interface{}) string {
	switch classifyAgentToolKind(toolName) {
	case agentToolKindWriteFile:
		return buildUnavailableToolInstruction(toolName, "file writes", availableTools)
	case agentToolKindEditFile:
		return buildUnavailableToolInstruction(toolName, "file edits", availableTools)
	default:
		return fmt.Sprintf("%s is temporarily blocked after repeated truncation. Use another currently available tool path.\n", toolName)
	}
}

func buildUnavailableToolInstruction(toolName, task string, availableTools []map[string]interface{}) string {
	var alternatives []string
	if toolListContainsName(availableTools, "craft_tool") {
		alternatives = append(alternatives, "prefer craft_tool to generate a local script")
	}
	if toolListContainsName(availableTools, "bash") {
		alternatives = append(alternatives, "use bash with an appropriate script")
	}
	if len(alternatives) == 0 {
		return fmt.Sprintf("%s is temporarily blocked after repeated truncation. Use another currently available tool path for %s.\n", toolName, task)
	}
	return fmt.Sprintf("%s is temporarily blocked after repeated truncation. For %s, %s.\n", toolName, task, strings.Join(alternatives, "; or "))
}

func toolListContainsName(tools []map[string]interface{}, want string) bool {
	for _, td := range tools {
		if tool.ExtractToolName(td) == want {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Best-effort partial write for truncated write_file calls
// ---------------------------------------------------------------------------

// getEffectiveWorkingDir returns the effective working directory for write_file
// tool resolution, reusing the same logic as resolveToolWorkDir("").
func (h *IMMessageHandler) getEffectiveWorkingDir() string {
	return h.resolveToolWorkDir("")
}

// truncationPartialWriteResult holds the result of a best-effort partial write.
type truncationPartialWriteResult struct {
	Path         string // file path that was written to
	BytesWritten int    // number of bytes successfully written
	RuneCount    int    // number of runes in the written content
	Tail         string // last N characters of written content (for LLM to know where to continue)
}

// attemptPartialWriteFile tries to extract path and partial content from a
// truncated write_file JSON argument string and writes whatever content was
// received to disk. This converts a failed tool call into a partially
// successful one, giving the LLM a concrete next step ("continue with
// mode=append from byte N") instead of a vague "please split" hint.
//
// Returns nil if the raw args don't contain enough information to perform
// a partial write (e.g. path is missing or content is empty).
func attemptPartialWriteFile(rawArgs string, workingDir string) *truncationPartialWriteResult {
	if rawArgs == "" {
		return nil
	}

	// The JSON is truncated (invalid), so we can't use json.Unmarshal.
	// Extract "path" and "content" fields using best-effort JSON string extraction.
	path := extractJSONStringField(rawArgs, "path")
	if path == "" {
		return nil
	}
	resolvedPath, ok := resolveTruncationPartialWritePath(path, workingDir)
	if !ok {
		log.Printf("[agent-loop] partial write: refusing path outside working directory: %q", path)
		return nil
	}
	path = resolvedPath

	content := extractJSONStringField(rawArgs, "content")
	if content == "" {
		return nil
	}
	// Require minimum content length to avoid writing useless tiny fragments
	// that are too short to be meaningful code (e.g. just a few chars from
	// a truncated JSON value boundary).
	const minPartialWriteBytes = 10
	if len(content) < minPartialWriteBytes {
		log.Printf("[agent-loop] partial write: content too short (%d bytes), skipping partial write for %q", len(content), path)
		return nil
	}

	// Determine write mode from args (default: overwrite).
	mode := extractJSONStringField(rawArgs, "mode")

	if mode != "append" {
		if _, statErr := os.Stat(path); statErr == nil {
			log.Printf("[agent-loop] partial write: refusing to overwrite existing file with truncated content: %q", path)
			return nil
		} else if !os.IsNotExist(statErr) {
			log.Printf("[agent-loop] partial write: failed to stat %q: %v", path, statErr)
			return nil
		}
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[agent-loop] partial write: failed to create directory %q: %v", dir, err)
		return nil
	}

	var err error
	contentBytes := []byte(content)
	if mode == "append" {
		f, openErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if openErr != nil {
			log.Printf("[agent-loop] partial write: failed to open %q for append: %v", path, openErr)
			return nil
		}
		_, err = f.Write(contentBytes)
		f.Close()
	} else {
		err = os.WriteFile(path, contentBytes, 0o644)
	}
	if err != nil {
		log.Printf("[agent-loop] partial write: failed to write %q: %v", path, err)
		return nil
	}

	runeCount := utf8.RuneCount(contentBytes)
	log.Printf("[agent-loop] partial write: wrote %d bytes (%d runes) to %q (mode=%s, truncated args=%d bytes)",
		len(contentBytes), runeCount, path, mode, len(rawArgs))

	// Capture tail of written content so LLM knows where to continue.
	tail := content
	runes := []rune(tail)
	const tailMaxRunes = 80
	if len(runes) > tailMaxRunes {
		tail = string(runes[len(runes)-tailMaxRunes:])
	}

	return &truncationPartialWriteResult{
		Path:         path,
		BytesWritten: len(contentBytes),
		RuneCount:    runeCount,
		Tail:         tail,
	}
}

func truncatedToolArgsForName(args map[string]string, name string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	if value, ok := args[name]; ok {
		return value, true
	}
	want := strings.TrimSpace(name)
	for key, value := range args {
		if strings.TrimSpace(key) == want {
			return value, true
		}
	}
	return "", false
}

func resolveTruncationPartialWritePath(path string, workingDir string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	if workingDir == "" {
		// No working directory constraint — only allow relative paths as a
		// safety measure. Absolute paths without a base dir could write anywhere.
		if filepath.IsAbs(path) {
			return "", false
		}
		return filepath.Clean(path), true
	}
	base, err := filepath.Abs(workingDir)
	if err != nil {
		base = filepath.Clean(workingDir)
	}
	target := path
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		target = filepath.Clean(target)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return target, true
}

// extractJSONStringField does best-effort extraction of a string field from
// potentially truncated JSON. It finds "fieldName": "..." and extracts the
// string value, handling JSON escape sequences. If the string value is
// truncated (no closing quote), it returns whatever content was found.
func extractJSONStringField(rawJSON string, fieldName string) string {
	fieldMarker := `"` + fieldName + `"`
	startIdx := strings.Index(rawJSON, fieldMarker)
	if startIdx < 0 {
		return ""
	}
	i := startIdx + len(fieldMarker)
	for i < len(rawJSON) && (rawJSON[i] == ' ' || rawJSON[i] == '\t' || rawJSON[i] == '\r' || rawJSON[i] == '\n') {
		i++
	}
	if i >= len(rawJSON) || rawJSON[i] != ':' {
		return ""
	}
	i++
	for i < len(rawJSON) && (rawJSON[i] == ' ' || rawJSON[i] == '\t' || rawJSON[i] == '\r' || rawJSON[i] == '\n') {
		i++
	}
	if i >= len(rawJSON) || rawJSON[i] != '"' {
		return ""
	}
	i++

	// Parse the JSON string value, handling escapes.
	var buf strings.Builder
	for i < len(rawJSON) {
		ch := rawJSON[i]
		if ch == '"' {
			break
		}
		if ch == '\\' {
			if i+1 >= len(rawJSON) {
				break // truncated escape at end of input
			}
			next := rawJSON[i+1]
			switch next {
			case '"', '\\', '/':
				buf.WriteByte(next)
				i += 2
			case 'n':
				buf.WriteByte('\n')
				i += 2
			case 'r':
				buf.WriteByte('\r')
				i += 2
			case 't':
				buf.WriteByte('\t')
				i += 2
			case 'b':
				buf.WriteByte('\b')
				i += 2
			case 'f':
				buf.WriteByte('\f')
				i += 2
			case 'u':
				// Unicode escape \uXXXX (with surrogate pair support)
				if i+5 >= len(rawJSON) {
					// Truncated unicode escape — return what we have.
					return buf.String()
				}
				hexStr := rawJSON[i+2 : i+6]
				var codepoint uint32
				if _, err := fmt.Sscanf(hexStr, "%04x", &codepoint); err == nil {
					// Check for UTF-16 surrogate pair (emoji etc.)
					if codepoint >= 0xD800 && codepoint <= 0xDBFF {
						// High surrogate — look for \uDCxx low surrogate
						if i+11 < len(rawJSON) && rawJSON[i+6] == '\\' && rawJSON[i+7] == 'u' {
							lowHex := rawJSON[i+8 : i+12]
							var low uint32
							if _, err2 := fmt.Sscanf(lowHex, "%04x", &low); err2 == nil && low >= 0xDC00 && low <= 0xDFFF {
								combined := 0x10000 + (codepoint-0xD800)*0x400 + (low - 0xDC00)
								buf.WriteRune(rune(combined))
								i += 12
							} else {
								// Orphan high surrogate — emit replacement character.
								buf.WriteRune('\uFFFD')
								i += 6
							}
						} else {
							// No following \u — emit replacement character.
							buf.WriteRune('\uFFFD')
							i += 6
						}
					} else if codepoint >= 0xDC00 && codepoint <= 0xDFFF {
						// Orphan low surrogate — emit replacement character.
						buf.WriteRune('\uFFFD')
						i += 6
					} else {
						buf.WriteRune(rune(codepoint))
						i += 6
					}
				} else {
					// Malformed \u escape — emit literal and advance.
					buf.WriteByte('\\')
					buf.WriteByte('u')
					i += 2
				}
			default:
				// Unknown escape — emit literal backslash + char.
				buf.WriteByte('\\')
				buf.WriteByte(next)
				i += 2
			}
		} else {
			buf.WriteByte(ch)
			i++
		}
	}

	return buf.String()
}

// buildPartialWriteHint generates the system message after a successful
// partial write, telling the LLM exactly what happened and what to do next.
func buildPartialWriteHint(result *truncationPartialWriteResult) string {
	hint := fmt.Sprintf(
		"[system] write_file arguments were truncated by the model output limit. "+
			"The system saved the received partial content to disk.\n"+
			"  File: %s\n"+
			"  Written: %d bytes (%d runes)\n",
		result.Path, result.BytesWritten, result.RuneCount)
	if result.Tail != "" {
		hint += fmt.Sprintf("  Last written chars: ...%s\n", result.Tail)
	}
	hint += fmt.Sprintf(
		"Continue from where you left off with write_file(path=%q, mode=\"append\", content=\"...remaining content starting after the last chars shown above...\"). "+
			"Keep each content chunk under about 3000 characters; multiple append calls are fine.",
		result.Path)
	return hint
}

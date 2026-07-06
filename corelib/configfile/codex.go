package configfile

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"

	_ "modernc.org/sqlite"
)

// CodexAuthPath returns ~/.codex/auth.json
func CodexAuthPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "auth.json")
}

// CodexConfigPath returns ~/.codex/config.toml
func CodexConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "config.toml")
}

// WriteCodexConfig writes both ~/.codex/auth.json and ~/.codex/config.toml
// atomically. If config.toml write fails, auth.json is rolled back.
//
// Key improvements over the old approach (learned from cc-switch):
// 1. Incremental TOML editing: preserves user's MCP servers, profiles, comments
// 2. Atomic dual-file write with rollback
// 3. base_url goes into [model_providers.xxx] section, not top-level
func WriteCodexConfig(apiKey, baseURL, modelID, providerName, wireApi string) error {
	return WriteCodexConfigWithClientName(apiKey, baseURL, modelID, providerName, wireApi, corelib.CodeGenClientName)
}

func WriteCodexConfigWithClientName(apiKey, baseURL, modelID, providerName, wireApi, clientName string) error {
	return WriteCodexConfigAtWithClientName(filepath.Dir(CodexAuthPath()), apiKey, baseURL, modelID, providerName, wireApi, clientName)
}

// WriteCodexConfigAt writes auth.json and config.toml under codexDir using the
// same conservative switching path as WriteCodexConfig. It exists for
// project-scoped Codex config directories.
func WriteCodexConfigAt(codexDir, apiKey, baseURL, modelID, providerName, wireApi string) error {
	return WriteCodexConfigAtWithClientName(codexDir, apiKey, baseURL, modelID, providerName, wireApi, corelib.CodeGenClientName)
}

func WriteCodexConfigAtWithClientName(codexDir, apiKey, baseURL, modelID, providerName, wireApi, clientName string) error {
	if err := ensureCodexProcessesStopped(); err != nil {
		return err
	}

	authPath := filepath.Join(codexDir, "auth.json")
	configPath := filepath.Join(codexDir, "config.toml")

	// Ensure directory exists
	dir := filepath.Dir(authPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create codex dir: %w", err)
	}

	// Save old files for rollback
	oldAuth, _ := os.ReadFile(authPath)
	oldConfig, _ := os.ReadFile(configPath)

	// Step 1: Write auth.json
	if apiKey != "" {
		auth := map[string]string{"OPENAI_API_KEY": apiKey}
		if err := AtomicWriteJSON(authPath, auth); err != nil {
			return fmt.Errorf("write codex auth: %w", err)
		}
	} else if err := os.Remove(authPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale codex auth: %w", err)
	}

	// Step 2: Build config.toml with incremental editing
	configToml, err := buildCodexConfigTomlWithClientName(configPath, baseURL, modelID, providerName, wireApi, clientName)
	if err != nil {
		// Rollback auth.json
		rollbackFile(authPath, oldAuth)
		return fmt.Errorf("build codex config: %w", err)
	}

	if err := AtomicWrite(configPath, []byte(configToml)); err != nil {
		rollbackFile(authPath, oldAuth)
		return fmt.Errorf("write codex config: %w", err)
	}

	if err := syncCodexSessionState(codexDir, CodexProviderKey(providerName), modelID); err != nil {
		rollbackFile(configPath, oldConfig)
		rollbackFile(authPath, oldAuth)
		return fmt.Errorf("sync codex session state: %w", err)
	}

	return nil
}

// buildCodexConfigToml reads existing config.toml and incrementally updates
// only the provider-specific fields, preserving MCP servers, profiles, etc.
func buildCodexConfigToml(configPath, baseURL, modelID, providerName, wireApi string) (string, error) {
	return buildCodexConfigTomlWithClientName(configPath, baseURL, modelID, providerName, wireApi, corelib.CodeGenClientName)
}

func buildCodexConfigTomlWithClientName(configPath, baseURL, modelID, providerName, wireApi, clientName string) (string, error) {
	providerName = CodexProviderKey(providerName)
	if providerName == "" {
		providerName = "custom"
	}
	if modelID == "" {
		modelID = "gpt-5.4"
	}
	if wireApi == "" {
		wireApi = "responses"
	}

	// Read existing config
	existing, _ := os.ReadFile(configPath)
	existingStr := string(existing)

	if strings.TrimSpace(existingStr) == "" {
		// No existing config, generate fresh
		return generateFreshCodexToml(providerName, modelID, baseURL, wireApi, codexProviderHTTPHeaders(baseURL, clientName)), nil
	}

	// Incremental edit: update only provider-related fields
	// We use line-based editing to preserve comments and formatting
	lines := strings.Split(existingStr, "\n")
	result := incrementalUpdateCodexToml(lines, providerName, modelID, baseURL, wireApi, codexProviderHTTPHeaders(baseURL, clientName))
	return result, nil
}

// BuildCodexConfigTomlContent returns a fresh Codex config.toml using the same
// provider normalization and defaults as WriteCodexConfigAt.
func BuildCodexConfigTomlContent(baseURL, modelID, providerName, wireApi string) string {
	return BuildCodexConfigTomlContentWithClientName(baseURL, modelID, providerName, wireApi, corelib.CodeGenClientName)
}

// BuildCodexConfigTomlContentWithClientName returns a fresh Codex config.toml
// and uses clientName for CodeGen's X-Codegen-Client-Name header.
func BuildCodexConfigTomlContentWithClientName(baseURL, modelID, providerName, wireApi, clientName string) string {
	providerName = CodexProviderKey(providerName)
	if providerName == "" {
		providerName = "custom"
	}
	if modelID == "" {
		modelID = "gpt-5.4"
	}
	if wireApi == "" {
		wireApi = "responses"
	}
	return generateFreshCodexToml(providerName, modelID, baseURL, wireApi, codexProviderHTTPHeaders(baseURL, clientName))
}

func codexProviderHTTPHeaders(baseURL, clientName string) map[string]string {
	if !corelib.IsCodeGenURL(baseURL) {
		return nil
	}
	return map[string]string{corelib.CodeGenClientNameHeader: corelib.NormalizeCodeGenClientName(clientName)}
}

// incrementalUpdateCodexToml updates provider fields in existing TOML while
// preserving MCP servers, profiles, comments, and other user config.
func incrementalUpdateCodexToml(lines []string, providerName, modelID, baseURL, wireApi string, httpHeaders map[string]string) string {
	var result []string
	updatedModelProvider := false
	updatedModel := false
	providerSectionKey := fmt.Sprintf("[model_providers.%s]", providerName)
	foundProviderSection := false
	currentSection := ""
	targetKeys := map[string]bool{}

	appendMissingTargetFields := func() {
		if !foundProviderSection {
			return
		}
		expected := map[string]string{
			"name":                fmt.Sprintf("name = %s", tomlString(providerName)),
			"wire_api":            fmt.Sprintf("wire_api = %s", tomlString(wireApi)),
			"supports_websockets": "supports_websockets = false",
		}
		if baseURL != "" {
			expected["base_url"] = fmt.Sprintf("base_url = %s", tomlString(baseURL))
		}
		if len(httpHeaders) > 0 {
			expected["http_headers"] = fmt.Sprintf("http_headers = %s", tomlInlineStringMap(httpHeaders))
		}
		order := []string{"name", "base_url", "wire_api", "http_headers", "supports_websockets"}
		for _, key := range order {
			line, ok := expected[key]
			if ok && !targetKeys[key] {
				result = append(result, line)
				targetKeys[key] = true
			}
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if section := codexTomlSectionName(trimmed); section != "" {
			if currentSection == fmt.Sprintf("model_providers.%s", providerName) {
				appendMissingTargetFields()
			}
			currentSection = section
			if trimmed == providerSectionKey {
				foundProviderSection = true
			}
			result = append(result, line)
			continue
		}

		// Update top-level model_provider
		if currentSection == "" && codexTomlKey(trimmed) == "model_provider" {
			result = append(result, fmt.Sprintf("model_provider = %s", tomlString(providerName)))
			updatedModelProvider = true
			continue
		}

		// Update top-level model
		if currentSection == "" && codexTomlKey(trimmed) == "model" {
			result = append(result, fmt.Sprintf("model = %s", tomlString(modelID)))
			updatedModel = true
			continue
		}

		if strings.HasPrefix(currentSection, "model_providers.") {
			switch codexTomlKey(trimmed) {
			case "requires_openai_auth":
				continue
			case "supports_websockets":
				if currentSection != fmt.Sprintf("model_providers.%s", providerName) {
					result = append(result, "supports_websockets = false")
					continue
				}
			}
		}

		// Update fields inside provider section
		if currentSection == fmt.Sprintf("model_providers.%s", providerName) {
			switch key := codexTomlKey(trimmed); key {
			case "name":
				result = append(result, fmt.Sprintf("name = %s", tomlString(providerName)))
				targetKeys[key] = true
				continue
			case "base_url":
				if baseURL != "" {
					result = append(result, fmt.Sprintf("base_url = %s", tomlString(baseURL)))
				}
				// If baseURL is empty, skip the line (remove it)
				targetKeys[key] = true
				continue
			case "wire_api":
				result = append(result, fmt.Sprintf("wire_api = %s", tomlString(wireApi)))
				targetKeys[key] = true
				continue
			case "http_headers":
				if len(httpHeaders) > 0 {
					result = append(result, mergeHTTPHeaderLine(line, httpHeaders))
				} else if line, keep := stripCodeGenHTTPHeaderLine(line); keep {
					result = append(result, line)
				}
				targetKeys[key] = true
				continue
			case "supports_websockets":
				result = append(result, "supports_websockets = false")
				targetKeys[key] = true
				continue
			case "requires_openai_auth":
				continue
			default:
				if key != "" {
					targetKeys[key] = true
				}
			}
		}

		if currentSection == "features" && codexTomlKey(trimmed) == "responses_websockets_v2" {
			continue
		}

		result = append(result, line)
	}

	// If we were still in provider section at EOF, inject missing fields
	if currentSection == fmt.Sprintf("model_providers.%s", providerName) {
		appendMissingTargetFields()
	}

	// If top-level fields weren't found, prepend them
	if !updatedModelProvider {
		result = append([]string{fmt.Sprintf("model_provider = %s", tomlString(providerName))}, result...)
	}
	if !updatedModel {
		// Insert after model_provider line
		for i, l := range result {
			if strings.HasPrefix(strings.TrimSpace(l), "model_provider") {
				modelLine := fmt.Sprintf("model = %s", tomlString(modelID))
				// Safe insert: copy tail to avoid mutating underlying array
				tail := make([]string, len(result[i+1:]))
				copy(tail, result[i+1:])
				result = append(result[:i+1], append([]string{modelLine}, tail...)...)
				break
			}
		}
	}

	// If provider section doesn't exist, append it
	if !foundProviderSection {
		result = append(result, "")
		result = append(result, providerSectionKey)
		result = append(result, fmt.Sprintf("name = %s", tomlString(providerName)))
		if baseURL != "" {
			result = append(result, fmt.Sprintf("base_url = %s", tomlString(baseURL)))
		}
		result = append(result, fmt.Sprintf("wire_api = %s", tomlString(wireApi)))
		if len(httpHeaders) > 0 {
			result = append(result, fmt.Sprintf("http_headers = %s", tomlInlineStringMap(httpHeaders)))
		}
		result = append(result, "supports_websockets = false")
	}

	return strings.Join(result, "\n")
}

func isInsideSection(lines []string) bool {
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "[") {
			return true
		}
		if trimmed == "" {
			continue
		}
	}
	return false
}

func generateFreshCodexToml(providerName, modelID, baseURL, wireApi string, httpHeaders map[string]string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "model_provider = %s\n", tomlString(providerName))
	fmt.Fprintf(&sb, "model = %s\n", tomlString(modelID))
	sb.WriteString("model_reasoning_effort = \"xhigh\"\n")
	sb.WriteString("disable_response_storage = true\n")
	fmt.Fprintf(&sb, "\n[model_providers.%s]\n", providerName)
	fmt.Fprintf(&sb, "name = %s\n", tomlString(providerName))
	if baseURL != "" {
		fmt.Fprintf(&sb, "base_url = %s\n", tomlString(baseURL))
	}
	fmt.Fprintf(&sb, "wire_api = %s\n", tomlString(wireApi))
	if len(httpHeaders) > 0 {
		fmt.Fprintf(&sb, "http_headers = %s\n", tomlInlineStringMap(httpHeaders))
	}
	sb.WriteString("supports_websockets = false\n")
	return sb.String()
}

func tomlInlineStringMap(values map[string]string) string {
	if len(values) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s = %s", tomlString(key), tomlString(values[key])))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func stripCodeGenHTTPHeaderLine(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.Contains(strings.ToLower(trimmed), strings.ToLower(corelib.CodeGenClientNameHeader)) {
		return line, true
	}
	_, rawValue, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "", false
	}
	mapValue, suffix := splitTomlValueComment(rawValue)
	headers, ok := parseTomlInlineStringMap(mapValue)
	if !ok {
		return "", false
	}
	for key := range headers {
		if strings.EqualFold(key, corelib.CodeGenClientNameHeader) {
			delete(headers, key)
		}
	}
	if len(headers) == 0 {
		return "", false
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	return indent + "http_headers = " + tomlInlineStringMap(headers) + suffix, true
}

func mergeHTTPHeaderLine(line string, managed map[string]string) string {
	trimmed := strings.TrimSpace(line)
	_, rawValue, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "http_headers = " + tomlInlineStringMap(managed)
	}
	mapValue, suffix := splitTomlValueComment(rawValue)
	headers, ok := parseTomlInlineStringMap(mapValue)
	if !ok {
		return "http_headers = " + tomlInlineStringMap(managed) + suffix
	}
	for key, value := range managed {
		for existing := range headers {
			if strings.EqualFold(existing, key) && existing != key {
				delete(headers, existing)
			}
		}
		headers[key] = value
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	return indent + "http_headers = " + tomlInlineStringMap(headers) + suffix
}

func splitTomlValueComment(raw string) (string, string) {
	inDoubleString := false
	inSingleString := false
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '"':
			if inSingleString {
				continue
			}
			backslashes := 0
			for j := i - 1; j >= 0 && raw[j] == '\\'; j-- {
				backslashes++
			}
			if backslashes%2 == 0 {
				inDoubleString = !inDoubleString
			}
		case '\'':
			if !inDoubleString {
				inSingleString = !inSingleString
			}
		case '#':
			if !inDoubleString && !inSingleString {
				value := strings.TrimSpace(raw[:i])
				comment := raw[i:]
				if value != "" {
					return value, " " + comment
				}
				return value, comment
			}
		}
	}
	return strings.TrimSpace(raw), ""
}

func parseTomlInlineStringMap(raw string) (map[string]string, bool) {
	text := strings.TrimSpace(raw)
	if !strings.HasPrefix(text, "{") {
		return nil, false
	}
	inner, ok := readTomlInlineTableBody(text)
	if !ok {
		return nil, false
	}
	text = strings.TrimSpace(inner)
	values := map[string]string{}
	if text == "" {
		return values, true
	}
	for text != "" {
		key, rest, ok := readTomlKey(text)
		if !ok {
			return nil, false
		}
		rest = strings.TrimSpace(rest)
		if !strings.HasPrefix(rest, "=") {
			return nil, false
		}
		value, rest, ok := readTomlString(strings.TrimSpace(strings.TrimPrefix(rest, "=")))
		if !ok {
			return nil, false
		}
		values[key] = value
		rest = strings.TrimSpace(rest)
		if rest == "" {
			break
		}
		if !strings.HasPrefix(rest, ",") {
			return nil, false
		}
		text = strings.TrimSpace(strings.TrimPrefix(rest, ","))
	}
	return values, true
}

func readTomlInlineTableBody(raw string) (string, bool) {
	text := strings.TrimSpace(raw)
	if !strings.HasPrefix(text, "{") {
		return "", false
	}
	inDoubleString := false
	inSingleString := false
	for i := 1; i < len(text); i++ {
		switch text[i] {
		case '"':
			if inSingleString {
				continue
			}
			backslashes := 0
			for j := i - 1; j >= 0 && text[j] == '\\'; j-- {
				backslashes++
			}
			if backslashes%2 == 0 {
				inDoubleString = !inDoubleString
			}
		case '\'':
			if !inDoubleString {
				inSingleString = !inSingleString
			}
		case '}':
			if !inDoubleString && !inSingleString {
				return text[1:i], strings.TrimSpace(text[i+1:]) == ""
			}
		}
	}
	return "", false
}

func readTomlKey(raw string) (string, string, bool) {
	text := strings.TrimSpace(raw)
	if strings.HasPrefix(text, "\"") || strings.HasPrefix(text, "'") {
		return readTomlString(text)
	}
	for i := 0; i < len(text); i++ {
		r := text[i]
		if r == '=' || r == ' ' || r == '\t' {
			key := strings.TrimSpace(text[:i])
			return key, text[i:], key != ""
		}
	}
	return "", "", false
}

func readTomlString(raw string) (string, string, bool) {
	text := strings.TrimSpace(raw)
	if strings.HasPrefix(text, "'") {
		for i := 1; i < len(text); i++ {
			if text[i] == '\'' {
				return text[1:i], text[i+1:], true
			}
		}
		return "", "", false
	}
	if !strings.HasPrefix(text, "\"") {
		return "", "", false
	}
	for i := 1; i < len(text); i++ {
		if text[i] != '"' {
			continue
		}
		backslashes := 0
		for j := i - 1; j >= 0 && text[j] == '\\'; j-- {
			backslashes++
		}
		if backslashes%2 != 0 {
			continue
		}
		value, err := strconv.Unquote(text[:i+1])
		if err != nil {
			return "", "", false
		}
		return value, text[i+1:], true
	}
	return "", "", false
}

func tomlString(value string) string {
	data, err := json.Marshal(value)
	if err != nil {
		return strconv.Quote(value)
	}
	return string(data)
}

func codexTomlSectionName(trimmed string) string {
	if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") || strings.HasPrefix(trimmed, "[[") {
		return ""
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"))
}

func codexTomlKey(trimmed string) string {
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
		return ""
	}
	before, _, ok := strings.Cut(trimmed, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(before)
}

func sanitizeTomlKey(s string) string {
	return SanitizeID(s)
}

func CodexProviderKey(providerName string) string {
	return normalizeCodexProviderKey(sanitizeTomlKey(providerName))
}

func normalizeCodexProviderKey(providerName string) string {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "openai":
		return "openai-compatible"
	default:
		return providerName
	}
}

// ReadCodexAuth reads ~/.codex/auth.json for backfill.
func ReadCodexAuth() (map[string]interface{}, error) {
	data, err := os.ReadFile(CodexAuthPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ReadCodexConfigToml reads ~/.codex/config.toml for backfill.
func ReadCodexConfigToml() (string, error) {
	data, err := os.ReadFile(CodexConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// ClearCodexThirdPartySettings removes third-party provider configuration
// from ~/.codex/auth.json and ~/.codex/config.toml when switching back to
// the builtin provider. auth.json is removed entirely (it only contains the
// API key). In config.toml, the model_provider and model top-level fields
// and the [model_providers.xxx] section are removed, preserving MCP servers,
// profiles, features, and other user settings.
// If files don't exist, this is a no-op.
func ClearCodexThirdPartySettings() error {
	return ClearCodexThirdPartySettingsAt(filepath.Dir(CodexAuthPath()))
}

// ClearCodexThirdPartySettingsAt removes third-party provider configuration
// from a specific Codex config directory.
func ClearCodexThirdPartySettingsAt(codexDir string) error {
	needsClear, err := hasCodexThirdPartySettingsAt(codexDir)
	if err != nil {
		return err
	}
	if !needsClear {
		return nil
	}

	if err := ensureCodexProcessesStopped(); err != nil {
		return err
	}

	var firstErr error
	if err := clearCodexAuthAt(codexDir); err != nil {
		firstErr = fmt.Errorf("clear codex auth: %w", err)
	}
	if err := clearCodexConfigProviderAt(codexDir); err != nil {
		if firstErr != nil {
			return firstErr
		}
		return fmt.Errorf("clear codex config: %w", err)
	}
	return firstErr
}

func hasCodexThirdPartySettingsAt(codexDir string) (bool, error) {
	authPath := filepath.Join(codexDir, "auth.json")
	if _, err := os.Stat(authPath); err == nil {
		return true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("stat codex auth: %w", err)
	}

	configPath := filepath.Join(codexDir, "config.toml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read config.toml: %w", err)
	}
	content := string(data)
	if strings.TrimSpace(content) == "" {
		return false, nil
	}
	cleared := strings.Join(stripCodexProviderLines(strings.Split(content, "\n")), "\n")
	return content != cleared, nil
}

// syncCodexSessionState mirrors the conservative parts of switch_provider.py:
// JSONL session metadata is the authoritative source, while state_*.sqlite is
// Codex Desktop's cache. Both must be consistent or existing conversations can
// keep routing through the previous provider after a switch.
func syncCodexSessionState(codexDir, providerName, modelID string) error {
	if strings.EqualFold(os.Getenv("AICODER_SKIP_CODEX_SESSION_SYNC"), "1") ||
		strings.EqualFold(os.Getenv("AICODER_SKIP_CODEX_SESSION_SYNC"), "true") {
		return nil
	}
	providerName = CodexProviderKey(providerName)
	if providerName == "" {
		providerName = "custom"
	}
	if strings.TrimSpace(modelID) == "" {
		modelID = "gpt-5.4"
	}

	if err := updateCodexSessionJSONL(codexDir, providerName, modelID); err != nil {
		return err
	}
	if err := updateCodexStateSQLite(codexDir, providerName, modelID); err != nil {
		return err
	}
	return nil
}

func updateCodexSessionJSONL(codexDir, providerName, modelID string) error {
	roots := []string{
		filepath.Join(codexDir, "sessions"),
		filepath.Join(codexDir, "archived_sessions"),
	}
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stat %s: %w", root, err)
		}
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasPrefix(filepath.Base(path), "rollout-") || filepath.Ext(path) != ".jsonl" {
				return nil
			}
			changed, err := updateCodexSessionJSONLFile(path, providerName, modelID)
			if err != nil {
				return err
			}
			if changed {
				log.Printf("[config] Codex session metadata updated: %s", path)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("walk %s: %w", root, err)
		}
	}
	return nil
}

func updateCodexSessionJSONLFile(path, providerName, modelID string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return false, nil
	}

	changed := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			out = append(out, line)
			continue
		}

		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			out = append(out, line)
			continue
		}

		lineChanged := false
		switch obj["type"] {
		case "session_meta":
			if payload, ok := obj["payload"].(map[string]interface{}); ok {
				if _, ok := payload["model_provider"]; ok && payload["model_provider"] != providerName {
					payload["model_provider"] = providerName
					lineChanged = true
				}
			}
		case "turn_context":
			if payload, ok := obj["payload"].(map[string]interface{}); ok {
				if _, ok := payload["model"]; ok && payload["model"] != modelID {
					payload["model"] = modelID
					lineChanged = true
				}
				if cm, ok := payload["collaboration_mode"].(map[string]interface{}); ok {
					if settings, ok := cm["settings"].(map[string]interface{}); ok {
						if _, ok := settings["model"]; ok && settings["model"] != modelID {
							settings["model"] = modelID
							lineChanged = true
						}
					}
				}
			}
		}

		if !lineChanged {
			out = append(out, line)
			continue
		}
		encoded, err := json.Marshal(obj)
		if err != nil {
			return false, fmt.Errorf("marshal %s: %w", path, err)
		}
		out = append(out, string(encoded)+"\n")
		changed = true
	}

	if !changed {
		return false, nil
	}
	if err := AtomicWrite(path, []byte(strings.Join(out, ""))); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

func updateCodexStateSQLite(codexDir, providerName, modelID string) error {
	dbs, err := discoverCodexStateDBs(codexDir)
	if err != nil {
		return err
	}
	if len(dbs) == 0 {
		return nil
	}
	for _, dbPath := range dbs {
		db, err := sql.Open("sqlite", dbPath)
		if err != nil {
			return fmt.Errorf("open %s: %w", dbPath, err)
		}
		res, execErr := db.Exec(
			"UPDATE threads SET model_provider = ?, model = ? WHERE model_provider IS NOT ? OR model IS NOT ?",
			providerName, modelID, providerName, modelID,
		)
		if execErr == nil {
			_, execErr = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
		}
		closeErr := db.Close()
		if execErr != nil {
			return fmt.Errorf("update %s: %w", dbPath, execErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close %s: %w", dbPath, closeErr)
		}
		if rows, err := res.RowsAffected(); err == nil && rows > 0 {
			log.Printf("[config] Codex state cache updated: rows=%d db=%s", rows, dbPath)
		}
	}
	return nil
}

func discoverCodexStateDBs(codexDir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(codexDir, "state_*.sqlite"))
	if err != nil {
		return nil, err
	}
	type candidate struct {
		path    string
		modTime time.Time
	}
	var candidates []candidate
	for _, path := range matches {
		base := filepath.Base(path)
		if strings.Contains(base, ".bak.") || strings.HasSuffix(base, "-wal") || strings.HasSuffix(base, "-shm") {
			continue
		}
		ok, err := codexStateDBHasThreads(path)
		if err != nil {
			log.Printf("[config] Codex state cache ignored: %s: %v", path, err)
			continue
		}
		if !ok {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", path, err)
		}
		candidates = append(candidates, candidate{path: path, modTime: info.ModTime()})
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	return []string{candidates[0].path}, nil
}

func codexStateDBHasThreads(path string) (bool, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return false, err
	}
	defer db.Close()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='threads'").Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// clearCodexAuth removes ~/.codex/auth.json. No-op if it doesn't exist.
func clearCodexAuth() error {
	return clearCodexAuthAt(filepath.Dir(CodexAuthPath()))
}

func clearCodexAuthAt(codexDir string) error {
	authPath := filepath.Join(codexDir, "auth.json")
	if err := os.Remove(authPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// clearCodexConfigProvider reads ~/.codex/config.toml, removes the
// model_provider and model top-level fields and any [model_providers.xxx]
// section, then writes back preserving all other content.
// No-op if the file doesn't exist.
func clearCodexConfigProvider() error {
	return clearCodexConfigProviderAt(filepath.Dir(CodexConfigPath()))
}

func clearCodexConfigProviderAt(codexDir string) error {
	configPath := filepath.Join(codexDir, "config.toml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read config.toml: %w", err)
	}

	content := string(data)
	if strings.TrimSpace(content) == "" {
		return nil // empty file, nothing to clear
	}

	lines := strings.Split(content, "\n")
	result := stripCodexProviderLines(lines)
	out := strings.Join(result, "\n")

	// Skip write if content hasn't changed.
	if content == out {
		return nil
	}

	return AtomicWrite(configPath, []byte(out))
}

// stripCodexProviderLines removes provider-related lines from config.toml:
// - top-level model_provider = "..." line
// - top-level model = "..." line (only when not inside a section)
// - entire [model_providers.xxx] sections (header + all fields until next section)
// Everything else (MCP servers, profiles, features, comments) is preserved.
func stripCodexProviderLines(lines []string) []string {
	var result []string
	inModelProvidersSection := false
	inFeaturesSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect [model_providers.xxx] section headers
		if strings.HasPrefix(trimmed, "[model_providers.") {
			inModelProvidersSection = true
			continue // skip the section header
		}

		// Detect any other section header — exits model_providers section
		if strings.HasPrefix(trimmed, "[") && !strings.HasPrefix(trimmed, "[model_providers.") {
			inModelProvidersSection = false
			inFeaturesSection = codexTomlSectionName(trimmed) == "features"
		}

		// Skip all lines inside a [model_providers.xxx] section
		if inModelProvidersSection {
			continue
		}

		if inFeaturesSection && codexTomlKey(trimmed) == "responses_websockets_v2" {
			continue
		}

		// Skip top-level model_provider = "..."
		if strings.HasPrefix(trimmed, "model_provider") && strings.Contains(trimmed, "=") && !isInsideSection(result) {
			continue
		}

		// Skip top-level model = "..." (but not inside a section)
		if (strings.HasPrefix(trimmed, "model =") || strings.HasPrefix(trimmed, "model=")) && !isInsideSection(result) {
			continue
		}

		result = append(result, line)
	}

	// Clean up leading/trailing blank lines that may result from removal
	result = trimConsecutiveBlankLines(result)

	return result
}

// trimConsecutiveBlankLines collapses runs of 3+ consecutive blank lines
// down to 2 (one visual separator), and trims trailing blank lines.
func trimConsecutiveBlankLines(lines []string) []string {
	var result []string
	consecutiveBlanks := 0

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			consecutiveBlanks++
			if consecutiveBlanks > 2 {
				continue // collapse excessive blank lines
			}
		} else {
			consecutiveBlanks = 0
		}
		result = append(result, line)
	}

	// Trim trailing blank lines
	for len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
		result = result[:len(result)-1]
	}

	// Ensure file ends with a newline
	if len(result) > 0 {
		result = append(result, "")
	}

	return result
}

func rollbackFile(path string, oldData []byte) {
	if oldData != nil {
		_ = AtomicWrite(path, oldData)
	} else {
		_ = os.Remove(path)
	}
}

type codexProcess struct {
	PID         int
	Name        string
	CommandLine string
}

var (
	findCodexProcessesFunc = findCodexProcesses
	killProcessTreeFunc    = killProcessTree
	codexProcessStopSleep  = time.Sleep
)

func ensureCodexProcessesStopped() error {
	if strings.EqualFold(os.Getenv("AICODER_SKIP_CODEX_PROCESS_KILL"), "1") ||
		strings.EqualFold(os.Getenv("AICODER_SKIP_CODEX_PROCESS_KILL"), "true") {
		return nil
	}

	processes, err := findCodexProcessesFunc()
	if err != nil {
		return fmt.Errorf("check codex processes: %w", err)
	}
	if len(processes) == 0 {
		return nil
	}

	for _, p := range processes {
		log.Printf("[config] Codex process running before provider switch: pid=%d name=%s", p.PID, p.Name)
		if err := killProcessTreeFunc(p.PID); err != nil {
			return fmt.Errorf("kill codex process pid=%d name=%s: %w", p.PID, p.Name, err)
		}
	}

	var remaining []codexProcess
	for i := 0; i < 10; i++ {
		remaining, err = findCodexProcessesFunc()
		if err != nil {
			return fmt.Errorf("verify codex processes stopped: %w", err)
		}
		if len(remaining) == 0 {
			return nil
		}
		codexProcessStopSleep(150 * time.Millisecond)
	}

	parts := make([]string, 0, len(remaining))
	for _, p := range remaining {
		parts = append(parts, fmt.Sprintf("%s(pid=%d)", p.Name, p.PID))
	}
	return fmt.Errorf("codex processes still running after kill: %s", strings.Join(parts, ", "))
}

func findCodexProcesses() ([]codexProcess, error) {
	if runtime.GOOS == "windows" {
		return findCodexProcessesWindows()
	}
	return findCodexProcessesUnix()
}

func findCodexProcessesWindows() ([]codexProcess, error) {
	script := `$ErrorActionPreference = 'Stop'
$selfPid = $PID
Get-CimInstance Win32_Process | ForEach-Object {
  $cmd = [string]$_.CommandLine
  $exe = [string]$_.ExecutablePath
  $name = [string]$_.Name
  if ($_.ProcessId -ne $selfPid -and ($name -match '(?i)codex' -or $exe -match '(?i)codex' -or $cmd -match '(?i)codex|@openai[\\/]+codex|openai\.codex_')) {
    "$($_.ProcessId)$([char]9)$($name)$([char]9)$($cmd -replace $([char]9), ' ')"
  }
}`
	out, err := exec.Command(windowsPowerShellPath(), "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script).Output()
	if err != nil {
		return nil, err
	}
	return parseCodexProcessLines(string(out)), nil
}

func windowsPowerShellPath() string {
	return windowsSystemExecutablePath("powershell.exe")
}

func windowsSystemExecutablePath(name string) string {
	candidates := []string{name}
	if strings.HasSuffix(strings.ToLower(name), ".exe") {
		candidates = append(candidates, strings.TrimSuffix(name, filepath.Ext(name)))
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	for _, root := range []string{os.Getenv("SystemRoot"), os.Getenv("WINDIR")} {
		if root == "" {
			continue
		}
		path := filepath.Join(root, "System32", name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
		if strings.EqualFold(name, "powershell.exe") {
			path := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	return name
}

func findCodexProcessesUnix() ([]codexProcess, error) {
	out, err := exec.Command("ps", "-eo", "pid=,comm=,args=").Output()
	if err != nil {
		return nil, err
	}
	var processes []codexProcess
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil || pid == os.Getpid() {
			continue
		}
		name := fields[1]
		cmd := strings.Join(fields[2:], " ")
		if isCodexProcessCandidate(name, cmd) {
			processes = append(processes, codexProcess{PID: pid, Name: name, CommandLine: cmd})
		}
	}
	return processes, nil
}

func parseCodexProcessLines(out string) []codexProcess {
	var processes []codexProcess
	seen := map[int]bool{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || pid == os.Getpid() || seen[pid] {
			continue
		}
		seen[pid] = true
		cmd := ""
		if len(parts) == 3 {
			cmd = strings.TrimSpace(parts[2])
		}
		name := strings.TrimSpace(parts[1])
		if !isCodexProcessCandidate(name, cmd) {
			continue
		}
		processes = append(processes, codexProcess{
			PID:         pid,
			Name:        name,
			CommandLine: cmd,
		})
	}
	return processes
}

func isCodexProcessCandidate(name, commandLine string) bool {
	cmd := strings.ToLower(commandLine)
	normalized := strings.NewReplacer("\\", "/", "\"", " ", "'", " ").Replace(cmd)
	if strings.Contains(normalized, "/windowsapps/openai.codex_") {
		return false
	}

	base := strings.ToLower(filepath.Base(strings.TrimSpace(name)))
	base = strings.TrimSuffix(base, ".exe")
	base = strings.TrimSuffix(base, ".cmd")
	if base == "codex" {
		return true
	}

	if strings.Contains(normalized, "/@openai/codex/bin/codex.js") {
		return true
	}
	fields := strings.Fields(normalized)
	for i, field := range fields {
		fieldBase := strings.TrimSuffix(filepath.Base(field), ".exe")
		fieldBase = strings.TrimSuffix(fieldBase, ".cmd")
		if fieldBase == "codex.js" {
			return true
		}
		if fieldBase == "codex" && isCodexCommandToken(fields, i) {
			return true
		}
	}
	return false
}

func isCodexCommandToken(fields []string, index int) bool {
	if index < 0 || index >= len(fields) {
		return false
	}
	field := fields[index]
	if strings.Contains(field, "/") {
		return true
	}
	if index == 0 {
		return true
	}
	prev := strings.ToLower(strings.TrimSpace(fields[index-1]))
	switch prev {
	case "-command", "-c", "/c", "/k":
		return true
	default:
		return false
	}
}

func killProcessTree(pid int) error {
	if runtime.GOOS == "windows" {
		return exec.Command(windowsSystemExecutablePath("taskkill.exe"), "/PID", strconv.Itoa(pid), "/T", "/F").Run()
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

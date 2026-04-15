package configfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	if apiKey == "" {
		return nil
	}

	authPath := CodexAuthPath()
	configPath := CodexConfigPath()

	// Ensure directory exists
	dir := filepath.Dir(authPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create codex dir: %w", err)
	}

	// Save old auth.json for rollback
	oldAuth, _ := os.ReadFile(authPath)

	// Step 1: Write auth.json
	auth := map[string]string{"OPENAI_API_KEY": apiKey}
	if err := AtomicWriteJSON(authPath, auth); err != nil {
		return fmt.Errorf("write codex auth: %w", err)
	}

	// Step 2: Build config.toml with incremental editing
	configToml, err := buildCodexConfigToml(configPath, baseURL, modelID, providerName, wireApi)
	if err != nil {
		// Rollback auth.json
		rollbackFile(authPath, oldAuth)
		return fmt.Errorf("build codex config: %w", err)
	}

	if err := AtomicWrite(configPath, []byte(configToml)); err != nil {
		rollbackFile(authPath, oldAuth)
		return fmt.Errorf("write codex config: %w", err)
	}

	return nil
}

// buildCodexConfigToml reads existing config.toml and incrementally updates
// only the provider-specific fields, preserving MCP servers, profiles, etc.
func buildCodexConfigToml(configPath, baseURL, modelID, providerName, wireApi string) (string, error) {
	providerName = sanitizeTomlKey(providerName)
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
		return generateFreshCodexToml(providerName, modelID, baseURL, wireApi), nil
	}

	// Incremental edit: update only provider-related fields
	// We use line-based editing to preserve comments and formatting
	lines := strings.Split(existingStr, "\n")
	result := incrementalUpdateCodexToml(lines, providerName, modelID, baseURL, wireApi)
	return result, nil
}

// incrementalUpdateCodexToml updates provider fields in existing TOML while
// preserving MCP servers, profiles, comments, and other user config.
func incrementalUpdateCodexToml(lines []string, providerName, modelID, baseURL, wireApi string) string {
	var result []string
	updatedModelProvider := false
	updatedModel := false
	inProviderSection := false
	providerSectionKey := fmt.Sprintf("[model_providers.%s]", providerName)
	updatedBaseURL := false
	updatedWireApi := false
	foundProviderSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Update top-level model_provider
		if strings.HasPrefix(trimmed, "model_provider") && strings.Contains(trimmed, "=") {
			result = append(result, fmt.Sprintf("model_provider = %q", providerName))
			updatedModelProvider = true
			continue
		}

		// Update top-level model
		if strings.HasPrefix(trimmed, "model =") || strings.HasPrefix(trimmed, "model=") {
			if !inProviderSection && !isInsideSection(result) {
				result = append(result, fmt.Sprintf("model = %q", modelID))
				updatedModel = true
				continue
			}
		}

		// Detect provider section
		if trimmed == providerSectionKey {
			inProviderSection = true
			foundProviderSection = true
			result = append(result, line)
			continue
		}

		// Detect other sections (exit provider section)
		if inProviderSection && strings.HasPrefix(trimmed, "[") && trimmed != providerSectionKey {
			// Before leaving, inject missing fields
			if !updatedBaseURL && baseURL != "" {
				result = append(result, fmt.Sprintf("base_url = %q", baseURL))
			}
			if !updatedWireApi {
				result = append(result, fmt.Sprintf("wire_api = %q", wireApi))
			}
			inProviderSection = false
		}

		// Update fields inside provider section
		if inProviderSection {
			if strings.HasPrefix(trimmed, "base_url") && strings.Contains(trimmed, "=") {
				if baseURL != "" {
					result = append(result, fmt.Sprintf("base_url = %q", baseURL))
				}
				// If baseURL is empty, skip the line (remove it)
				updatedBaseURL = true
				continue
			}
			if strings.HasPrefix(trimmed, "wire_api") && strings.Contains(trimmed, "=") {
				result = append(result, fmt.Sprintf("wire_api = %q", wireApi))
				updatedWireApi = true
				continue
			}
		}

		result = append(result, line)
	}

	// If we were still in provider section at EOF, inject missing fields
	if inProviderSection {
		if !updatedBaseURL && baseURL != "" {
			result = append(result, fmt.Sprintf("base_url = %q", baseURL))
		}
		if !updatedWireApi {
			result = append(result, fmt.Sprintf("wire_api = %q", wireApi))
		}
	}

	// If top-level fields weren't found, prepend them
	if !updatedModelProvider {
		result = append([]string{fmt.Sprintf("model_provider = %q", providerName)}, result...)
	}
	if !updatedModel {
		// Insert after model_provider line
		for i, l := range result {
			if strings.HasPrefix(strings.TrimSpace(l), "model_provider") {
				modelLine := fmt.Sprintf("model = %q", modelID)
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
		result = append(result, fmt.Sprintf("name = %q", providerName))
		if baseURL != "" {
			result = append(result, fmt.Sprintf("base_url = %q", baseURL))
		}
		result = append(result, fmt.Sprintf("wire_api = %q", wireApi))
		result = append(result, "supports_websockets = true")
		result = append(result, "requires_openai_auth = true")
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

func generateFreshCodexToml(providerName, modelID, baseURL, wireApi string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "model_provider = %q\n", providerName)
	fmt.Fprintf(&sb, "model = %q\n", modelID)
	sb.WriteString("model_reasoning_effort = \"xhigh\"\n")
	sb.WriteString("disable_response_storage = true\n")
	fmt.Fprintf(&sb, "\n[model_providers.%s]\n", providerName)
	fmt.Fprintf(&sb, "name = %q\n", providerName)
	if baseURL != "" {
		fmt.Fprintf(&sb, "base_url = %q\n", baseURL)
	}
	fmt.Fprintf(&sb, "wire_api = %q\n", wireApi)
	sb.WriteString("supports_websockets = true\n")
	sb.WriteString("requires_openai_auth = true\n")
	sb.WriteString("\n[features]\n")
	sb.WriteString("responses_websockets_v2 = true\n")
	return sb.String()
}

func sanitizeTomlKey(s string) string {
	return SanitizeID(s)
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
	var firstErr error
	if err := clearCodexAuth(); err != nil {
		firstErr = fmt.Errorf("clear codex auth: %w", err)
	}
	if err := clearCodexConfigProvider(); err != nil {
		if firstErr != nil {
			return firstErr
		}
		return fmt.Errorf("clear codex config: %w", err)
	}
	return firstErr
}

// clearCodexAuth removes ~/.codex/auth.json. No-op if it doesn't exist.
func clearCodexAuth() error {
	authPath := CodexAuthPath()
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
	configPath := CodexConfigPath()

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
		}

		// Skip all lines inside a [model_providers.xxx] section
		if inModelProvidersSection {
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

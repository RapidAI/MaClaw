package configfile

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	ExternalAgentSourceCodex      = "codex"
	ExternalAgentSourceClaudeCode = "claude_code"
	ExternalAgentSourceOpenCode   = "opencode"

	ExternalAgentProviderCodex      = "Codex"
	ExternalAgentProviderClaudeCode = "Claude Code"
	ExternalAgentProviderOpenCode   = "OpenCode"

	OpenCodeZenBaseURL      = "https://opencode.ai/zen/v1"
	OpenCodeZenAuthURL      = "https://opencode.ai/auth"
	OpenCodeZenDefaultModel = "big-pickle"

	ExternalAgentTypeCodex      = "Codex"
	ExternalAgentTypeClaudeCode = "Claude Code"
	ExternalAgentTypeOpenCode   = "OpenCode"
)

// ExternalAgentType returns the User-Agent identity for an imported local agent.
func ExternalAgentType(source string) string {
	switch strings.TrimSpace(source) {
	case ExternalAgentSourceCodex:
		return ExternalAgentTypeCodex
	case ExternalAgentSourceClaudeCode:
		return ExternalAgentTypeClaudeCode
	case ExternalAgentSourceOpenCode:
		return ExternalAgentTypeOpenCode
	default:
		return ""
	}
}

// ExternalAgentCandidate is a local-agent connection that has not been
// live-tested yet. Import only happens after a successful auth/connectivity test.
type ExternalAgentCandidate struct {
	Source           string
	Name             string
	URL              string
	Key              string
	Model            string
	Protocol         string
	WireAPI          string
	AgentType        string
	AuthType         string
	OAuthAccessToken string
	ContextLength    int
	// PreferredModel is the already-imported provider's current model, if any.
	// Auth tests this first so a later scan does not reset the user's choice.
	PreferredModel string
}

// ScanExternalAgents reads Codex / Claude Code / OpenCode configs from disk.
// Missing or incomplete configs are omitted. Callers must still live-test.
func ScanExternalAgents() []ExternalAgentCandidate {
	var out []ExternalAgentCandidate
	if c, ok := scanCodexCandidate(); ok {
		out = append(out, c)
	}
	if c, ok := scanClaudeCodeCandidate(); ok {
		out = append(out, c)
	}
	if c, ok := scanOpenCodeZenCandidate(); ok {
		out = append(out, c)
	}
	return out
}

func scanCodexCandidate() (ExternalAgentCandidate, bool) {
	auth, err := ReadCodexAuth()
	if err != nil || auth == nil {
		return ExternalAgentCandidate{}, false
	}
	apiKey, oauthToken := extractCodexCredentials(auth)
	if apiKey == "" && oauthToken == "" {
		return ExternalAgentCandidate{}, false
	}

	tomlText, _ := ReadCodexConfigToml()
	model, providerID, providers := parseCodexToml(tomlText)
	baseURL, wireAPI := "", ""
	if providerID != "" {
		if section := providers[providerID]; section != nil {
			baseURL = strings.TrimRight(strings.TrimSpace(firstNonEmpty(section["base_url"], section["baseURL"])), "/")
			wireAPI = strings.TrimSpace(firstNonEmpty(section["wire_api"], section["wireApi"]))
		}
	}

	c := ExternalAgentCandidate{
		Source:           ExternalAgentSourceCodex,
		Name:             ExternalAgentProviderCodex,
		OAuthAccessToken: oauthToken,
		Model:            model,
		AgentType:        ExternalAgentTypeCodex,
		ContextLength:    128000,
	}
	if baseURL != "" {
		c.URL = baseURL
		c.Key = firstNonEmpty(apiKey, oauthToken)
		c.WireAPI = wireAPI
		if c.Model == "" {
			c.Model = "gpt-5.4"
		}
		return c, true
	}
	// A leftover ChatGPT token must not win over an explicit third-party API key
	// when no model_provider section is present.
	if oauthToken != "" && apiKey == "" && looksLikeCodexSubscription(auth, providerID) {
		c.URL = "https://chatgpt.com/backend-api/codex"
		c.Key = oauthToken
		c.AuthType = "oauth"
		c.WireAPI = "responses-ws"
		if c.Model == "" {
			c.Model = "gpt-5.4"
		}
		return c, true
	}
	if apiKey == "" {
		return ExternalAgentCandidate{}, false
	}
	c.URL = "https://api.openai.com/v1"
	c.Key = apiKey
	if c.Model == "" {
		c.Model = "gpt-5.4"
	}
	return c, true
}

func extractCodexCredentials(auth map[string]interface{}) (apiKey, oauthToken string) {
	if key, _ := auth["OPENAI_API_KEY"].(string); strings.TrimSpace(key) != "" {
		apiKey = strings.TrimSpace(key)
	}
	tokens, _ := auth["tokens"].(map[string]interface{})
	if tokens == nil {
		return apiKey, ""
	}
	if access, _ := tokens["access_token"].(string); strings.TrimSpace(access) != "" {
		oauthToken = strings.TrimSpace(access)
	}
	return apiKey, oauthToken
}

func looksLikeCodexSubscription(auth map[string]interface{}, providerID string) bool {
	if providerID != "" && !strings.EqualFold(providerID, "openai") && !strings.EqualFold(providerID, "openai-compatible") {
		return false
	}
	tokens, _ := auth["tokens"].(map[string]interface{})
	if tokens == nil {
		return false
	}
	access, _ := tokens["access_token"].(string)
	return strings.TrimSpace(access) != ""
}

func scanClaudeCodeCandidate() (ExternalAgentCandidate, bool) {
	settings, err := ReadClaudeSettings()
	if err != nil || settings == nil {
		return ExternalAgentCandidate{}, false
	}
	env, _ := settings["env"].(map[string]interface{})
	if env == nil {
		return ExternalAgentCandidate{}, false
	}
	key := strings.TrimSpace(firstNonEmpty(
		stringValue(env["ANTHROPIC_AUTH_TOKEN"]),
		stringValue(env["ANTHROPIC_API_KEY"]),
	))
	if key == "" {
		return ExternalAgentCandidate{}, false
	}
	baseURL := strings.TrimRight(strings.TrimSpace(stringValue(env["ANTHROPIC_BASE_URL"])), "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	model := strings.TrimSpace(firstNonEmpty(
		stringValue(env["ANTHROPIC_MODEL"]),
		stringValue(env["ANTHROPIC_DEFAULT_SONNET_MODEL"]),
		stringValue(env["ANTHROPIC_DEFAULT_OPUS_MODEL"]),
	))
	if model == "" {
		model = "claude-sonnet-4-5-20250514"
	}
	return ExternalAgentCandidate{
		Source:        ExternalAgentSourceClaudeCode,
		Name:          ExternalAgentProviderClaudeCode,
		URL:           baseURL,
		Key:           key,
		Model:         model,
		Protocol:      "anthropic",
		AgentType:     ExternalAgentTypeClaudeCode,
		ContextLength: 200000,
	}, true
}

func scanOpenCodeZenCandidate() (ExternalAgentCandidate, bool) {
	key := ReadOpenCodeZenKey()
	if key == "" {
		return ExternalAgentCandidate{}, false
	}
	model := readOpenCodePreferredModel()
	if model == "" {
		model = OpenCodeZenDefaultModel
	}
	return ExternalAgentCandidate{
		Source:        ExternalAgentSourceOpenCode,
		Name:          ExternalAgentProviderOpenCode,
		URL:           OpenCodeZenBaseURL,
		Key:           key,
		Model:         model,
		Protocol:      "openai",
		AgentType:     ExternalAgentTypeOpenCode,
		ContextLength: 128000,
	}, true
}

// ReadOpenCodeZenKey returns a Zen API key from OPENCODE_API_KEY or local auth.json.
func ReadOpenCodeZenKey() string {
	if envKey := strings.TrimSpace(os.Getenv("OPENCODE_API_KEY")); envKey != "" {
		return envKey
	}
	for _, path := range OpencodeAuthCandidates() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var auth map[string]interface{}
		if json.Unmarshal(data, &auth) != nil || auth == nil {
			continue
		}
		if key := extractOpenCodeZenKey(auth); key != "" {
			return key
		}
	}
	return ""
}

func extractOpenCodeZenKey(auth map[string]interface{}) string {
	if p, ok := auth["opencode"].(map[string]interface{}); ok {
		if key := strings.TrimSpace(firstNonEmpty(
			stringValue(p["key"]),
			stringValue(p["apiKey"]),
			stringValue(p["access"]),
			stringValue(p["access_token"]),
		)); key != "" {
			return key
		}
	}
	return strings.TrimSpace(stringValue(auth["OPENCODE_API_KEY"]))
}

func readOpenCodePreferredModel() string {
	for _, path := range []string{OpencodeConfigPath(), OpencodeConfigJSONCPath()} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg map[string]interface{}
		if json.Unmarshal(stripJSONC(data), &cfg) != nil || cfg == nil {
			continue
		}
		raw := strings.TrimSpace(stringValue(cfg["model"]))
		if raw == "" {
			continue
		}
		if i := strings.LastIndex(raw, "/"); i >= 0 {
			raw = raw[i+1:]
		}
		if raw != "" {
			return raw
		}
	}
	return ""
}

func (c ExternalAgentCandidate) ToProvider(models []string) corelib.MaclawLLMProvider {
	if len(models) == 0 && strings.TrimSpace(c.Model) != "" {
		models = []string{c.Model}
	}
	agentType := strings.TrimSpace(c.AgentType)
	if agentType == "" {
		agentType = ExternalAgentType(c.Source)
	}
	contextLength := c.ContextLength
	if contextLength <= 0 {
		contextLength = 128000
	}
	return corelib.MaclawLLMProvider{
		Name:             c.Name,
		URL:              c.URL,
		Key:              c.Key,
		Model:            c.Model,
		Protocol:         c.Protocol,
		WireAPI:          c.WireAPI,
		AgentType:        agentType,
		AuthType:         c.AuthType,
		OAuthAccessToken: c.OAuthAccessToken,
		ContextLength:    contextLength,
		TimeoutSec:       corelib.DefaultLLMTimeoutSec,
		Models:           models,
		ImportSource:     c.Source,
	}
}

func parseCodexToml(content string) (model, providerID string, providers map[string]map[string]string) {
	providers = map[string]map[string]string{}
	section := ""
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			if id, ok := codexProviderSectionID(section); ok {
				if providers[id] == nil {
					providers[id] = map[string]string{}
				}
			}
			continue
		}
		key, value, ok := splitTOMLAssignment(line)
		if !ok {
			continue
		}
		if section == "" {
			switch key {
			case "model":
				model = value
			case "model_provider":
				providerID = value
			}
			continue
		}
		if id, isProv := codexProviderSectionID(section); isProv {
			providers[id][key] = value
		}
	}
	return model, providerID, providers
}

func codexProviderSectionID(section string) (string, bool) {
	const prefix = "model_providers."
	if !strings.HasPrefix(section, prefix) {
		return "", false
	}
	id := strings.TrimSpace(section[len(prefix):])
	id = strings.Trim(id, `"'`)
	if id == "" {
		return "", false
	}
	return id, true
}

func splitTOMLAssignment(line string) (key, value string, ok bool) {
	eq := strings.Index(line, "=")
	if eq <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:eq])
	value = unquoteTOML(stripTOMLInlineComment(strings.TrimSpace(line[eq+1:])))
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

func stripTOMLInlineComment(v string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimSpace(v[:i])
			}
		}
	}
	return v
}

func unquoteTOML(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func stringValue(v interface{}) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func stripJSONC(data []byte) []byte {
	s := string(data)
	var b strings.Builder
	b.Grow(len(s))
	inStr := false
	esc := false
	i := 0
	for i < len(s) {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			i++
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			i++
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			}
			continue
		}
		b.WriteByte(c)
		i++
	}
	return []byte(b.String())
}

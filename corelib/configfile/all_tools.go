package configfile

// ToolConfigParams contains the parameters needed to write all tool configs.
type ToolConfigParams struct {
	Token             string // API token / key
	BaseURL           string // CodeGen API base URL (openai 协议)
	AnthropicBaseURL  string // anthropic 协议兼容端点（Claude/TigerClaw Code 使用）；为空时使用 BaseURL
	ModelID           string // 默认模型 ID
	ProviderName      string // 服务商名称（用于 OpenCode/Codex 的 provider ID）
}

// effectiveAnthropicBaseURL returns AnthropicBaseURL if set, otherwise BaseURL.
func (p ToolConfigParams) effectiveAnthropicBaseURL() string {
	if p.AnthropicBaseURL != "" {
		return p.AnthropicBaseURL
	}
	return p.BaseURL
}

// ToolConfigError records a single tool config write failure.
type ToolConfigError struct {
	Tool  string
	Error error
}

// ToolConfigResult records the outcome of writing configs for all tools.
type ToolConfigResult struct {
	Succeeded []string          // 成功写入的工具名称
	Failed    []ToolConfigError // 失败的工具及原因
}

// ToolWriter pairs a tool name with a write function.
type ToolWriter struct {
	Name string
	Fn   func() error
}

// DefaultToolWriters returns the standard set of tool writers for the given params.
func DefaultToolWriters(params ToolConfigParams) []ToolWriter {
	anthropicURL := params.effectiveAnthropicBaseURL()
	return []ToolWriter{
		{
			Name: "Claude",
			Fn:   func() error { return WriteClaudeSettings(params.Token, anthropicURL, params.ModelID) },
		},
		{
			Name: "CodeGen",
			Fn:   func() error { return WriteCodeGenSettings(params.Token, anthropicURL, params.ModelID) },
		},
		{
			Name: "OpenCode",
			Fn: func() error {
				return WriteOpencodeConfig(params.Token, params.BaseURL, params.ModelID, params.ProviderName)
			},
		},
		{
			Name: "Codex",
			Fn: func() error {
				return WriteCodexConfig(params.Token, params.BaseURL, params.ModelID, params.ProviderName, "responses")
			},
		},
		{
			Name: "Gemini",
			Fn:   func() error { return WriteGeminiConfig(params.Token, params.BaseURL, params.ModelID) },
		},
	}
}

// WriteAllToolConfigsWithWriters executes the given writers and records
// successes/failures. A failure in one writer does not block the others.
func WriteAllToolConfigsWithWriters(writers []ToolWriter) ToolConfigResult {
	var result ToolConfigResult
	for _, w := range writers {
		if err := w.Fn(); err != nil {
			result.Failed = append(result.Failed, ToolConfigError{Tool: w.Name, Error: err})
		} else {
			result.Succeeded = append(result.Succeeded, w.Name)
		}
	}
	return result
}

// WriteAllToolConfigs writes authentication info to all supported coding tool
// config files. A failure in one tool does not block the others.
func WriteAllToolConfigs(params ToolConfigParams) ToolConfigResult {
	return WriteAllToolConfigsWithWriters(DefaultToolWriters(params))
}

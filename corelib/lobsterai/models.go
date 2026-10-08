package lobsterai

// UpstreamModel is one LobsterAI catalog entry.
type UpstreamModel struct {
	ID   string
	Name string
}

// allowlist 2026-09 真机 GET /api/models/available 实测节选（含 1M 上下文窗口
// 的主力模型）。远端目录优先；这份兜底只在目录请求失败时提供记忆默认值。
var allowlist = []UpstreamModel{
	{ID: "glm-5.3", Name: "GLM-5.3"},
	{ID: "glm-5.3-flash", Name: "GLM-5.3-Flash"},
	{ID: "glm-5.3-flashx", Name: "GLM-5.3-FlashX"},
	{ID: "glm-5.2", Name: "GLM-5.2"},
	{ID: "deepseek-flash", Name: "DeepSeek-V4.1-Flash"},
	{ID: "deepseek-v4-pro", Name: "DeepSeek-V4-Pro"},
	{ID: "deepseek-v4-flash", Name: "DeepSeek-V4-Flash"},
}

// ModelList returns the built-in fallback catalog.
func ModelList() []UpstreamModel {
	return append([]UpstreamModel(nil), allowlist...)
}

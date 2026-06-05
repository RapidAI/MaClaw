package views

// config_fields.go is the SINGLE SOURCE OF TRUTH for all TUI-editable config fields.
//
// Each ConfigFieldDef declares:
//   - Key:      string key used in UI and ConfigSaveMsg
//   - Tab:      which config tab this field belongs to
//   - Section:  visual grouping within a tab
//   - DescKey:  i18n message key for the description
//   - Options:  nil = free text, non-nil = inline selector
//   - ReadOnly: display only
//   - Default:  default value shown before LoadFromAppConfig
//   - Get:      reads the value from AppConfig -> string
//   - Set:      writes a string value into AppConfig
//
// Adding a new config field = adding ONE entry to this slice.
// No other file needs to change.

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

// ConfigFieldDef is the single definition for a TUI-editable config field.
type ConfigFieldDef struct {
	Key      string
	Tab      int
	Section  string
	DescKey  string   // i18n message key
	Options  []string // nil = free text; non-nil = inline selector
	ReadOnly bool
	Default  string
	Get      func(cfg *corelib.AppConfig) string
	Set      func(cfg *corelib.AppConfig, val string)
}

// boolGet / boolSet are helpers for boolean fields.
func boolGet(ptr func(cfg *corelib.AppConfig) bool) func(cfg *corelib.AppConfig) string {
	return func(cfg *corelib.AppConfig) string {
		if ptr(cfg) {
			return "true"
		}
		return "false"
	}
}

func boolSet(ptr func(cfg *corelib.AppConfig, v bool)) func(cfg *corelib.AppConfig, val string) {
	return func(cfg *corelib.AppConfig, val string) {
		ptr(cfg, val == "true")
	}
}

// intGet / intSet are helpers for integer fields (empty string for zero).
func intGet(ptr func(cfg *corelib.AppConfig) int) func(cfg *corelib.AppConfig) string {
	return func(cfg *corelib.AppConfig) string {
		v := ptr(cfg)
		if v == 0 {
			return ""
		}
		return fmt.Sprintf("%d", v)
	}
}

func intSet(ptr func(cfg *corelib.AppConfig, v int)) func(cfg *corelib.AppConfig, val string) {
	return func(cfg *corelib.AppConfig, val string) {
		var v int
		fmt.Sscanf(val, "%d", &v)
		ptr(cfg, v)
	}
}

// floatGet / floatSet are helpers for optional normalized numeric fields.
func floatGet(ptr func(cfg *corelib.AppConfig) float64) func(cfg *corelib.AppConfig) string {
	return func(cfg *corelib.AppConfig) string {
		v := ptr(cfg)
		if v == 0 {
			return ""
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
}

func floatSet(ptr func(cfg *corelib.AppConfig, v float64)) func(cfg *corelib.AppConfig, val string) {
	return func(cfg *corelib.AppConfig, val string) {
		v, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil {
			ptr(cfg, 0)
			return
		}
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		ptr(cfg, v)
	}
}

// boolOpts is the standard boolean option list.
var boolOpts = []string{"true", "false"}

var (
	setupStatusOpts      = []string{"needs_setup", "needs_llm_key", "needs_redeem", "mcp_optional", "official_ready", "llm_ready"}
	workDirProfileOpts   = []string{"default_workspace", "current_directory", "home_directory", "custom"}
	maxIterationOpts     = []string{"30", "60", "100", "150", "300"}
	contextLengthOpts    = []string{"32000", "64000", "110000", "200000"}
	llmModelChoiceOpts   = []string{"auto", "glm-5-turbo", "glm-5.1", "kimi-for-coding", "MiniMax-M2.7", "astron-code-latest", "qwen2.5-coder:32b", "deepseek-coder-v2", "llama3.1"}
	auxLLMProfileOpts    = []string{"off", "same_as_primary", "custom"}
	imChannelProfileOpts = []string{"off", "weixin", "telegram", "qq", "lansenger"}
	proxyPortOpts        = []string{"7890", "8080", "1080", "3128"}
	proxyProfileOpts     = []string{"off", "local_http_7890", "local_http_8080", "local_socks5_1080", "custom"}
	securityProfileOpts  = []string{"relaxed", "standard", "strict", "offline", "developer", "custom"}
)

type llmProviderPreset struct {
	Name          string
	URL           string
	Model         string
	Protocol      string
	ContextLength int
	TimeoutSec    int
	AgentType     string
	AuthType      string
	IsCustom      bool
}

var llmProviderPresets = []llmProviderPreset{
	{Name: "Zhipu GLM Lobster", URL: "https://open.bigmodel.cn/api/coding/paas/v4", Model: "glm-5-turbo", Protocol: "openai", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec, AuthType: "apikey"},
	{Name: "Zhipu GLM Coding", URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.1", Protocol: "anthropic", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec, AgentType: "claude-code/2.0.0", AuthType: "apikey"},
	{Name: "MiniMax", URL: "https://api.minimaxi.com/v1", Model: "MiniMax-M2.7", Protocol: "openai", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec, AuthType: "apikey"},
	{Name: "Kimi", URL: "https://api.kimi.com/coding/v1", Model: "kimi-for-coding", Protocol: "openai", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec, AgentType: "claude-code/2.0.0", AuthType: "apikey"},
	{Name: "Xfyun Astron", URL: "https://maas-coding-api.cn-huabei-1.xf-yun.com/v2", Model: "astron-code-latest", Protocol: "openai", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec, AuthType: "apikey"},
	{Name: "OpenAI API Key", URL: "https://api.openai.com/v1", Model: "gpt-4o", Protocol: "openai", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec, AuthType: "apikey"},
	{Name: "Anthropic", URL: "https://api.anthropic.com", Model: "claude-sonnet-4-20250514", Protocol: "anthropic", ContextLength: 110000, TimeoutSec: corelib.DefaultLLMTimeoutSec, AgentType: "claude-code/2.0.0", AuthType: "apikey"},
	{Name: "Ollama Local", URL: "http://localhost:11434/v1", Model: "qwen2.5-coder:32b", Protocol: "openai", ContextLength: 32000, TimeoutSec: corelib.DefaultLLMTimeoutSec, AuthType: "none"},
	{Name: "LM Studio Local", URL: "http://localhost:1234/v1", Model: "auto", Protocol: "openai", ContextLength: 32000, TimeoutSec: corelib.DefaultLLMTimeoutSec, AuthType: "none"},
	{Name: "Custom", Protocol: "openai", AuthType: "apikey", TimeoutSec: corelib.DefaultLLMTimeoutSec, IsCustom: true},
}

func llmProviderPresetOptions() []string {
	opts := make([]string, 0, len(llmProviderPresets))
	for _, p := range llmProviderPresets {
		opts = append(opts, p.Name)
	}
	return opts
}

func appendUniqueOption(opts []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return opts
	}
	for _, opt := range opts {
		if opt == value {
			return opts
		}
	}
	return append(opts, value)
}

func llmProviderOptionsFromConfig(c *corelib.AppConfig) []string {
	opts := llmProviderPresetOptions()
	if c == nil {
		return opts
	}
	if current := strings.TrimSpace(c.MaclawLLMCurrentProvider); current != "" {
		opts = appendUniqueOption(opts, current)
	}
	for _, p := range c.MaclawLLMProviders {
		opts = appendUniqueOption(opts, p.Name)
	}
	return opts
}

func currentLLMPresetName(c *corelib.AppConfig) string {
	if strings.TrimSpace(c.MaclawLLMCurrentProvider) != "" {
		return c.MaclawLLMCurrentProvider
	}
	if strings.TrimSpace(c.MaclawLLMUrl) == "" && strings.TrimSpace(c.MaclawLLMModel) == "" {
		for _, p := range llmProviderPresets {
			if !p.IsCustom {
				return p.Name
			}
		}
	}
	for _, p := range llmProviderPresets {
		if p.IsCustom {
			continue
		}
		if strings.TrimRight(c.MaclawLLMUrl, "/") == strings.TrimRight(p.URL, "/") && c.MaclawLLMModel == p.Model {
			return p.Name
		}
	}
	return "Custom"
}

func applyLLMProviderPreset(c *corelib.AppConfig, name string) {
	var preset llmProviderPreset
	for _, p := range llmProviderPresets {
		if p.Name == name {
			preset = p
			break
		}
	}
	if preset.Name == "" {
		for _, provider := range c.MaclawLLMProviders {
			if provider.Name != name {
				continue
			}
			c.MaclawLLMCurrentProvider = provider.Name
			c.MaclawLLMUrl = strings.TrimRight(provider.URL, "/")
			c.MaclawLLMKey = provider.Key
			c.MaclawLLMModel = provider.Model
			c.MaclawLLMProtocol = provider.Protocol
			if c.MaclawLLMProtocol == "" {
				c.MaclawLLMProtocol = "openai"
			}
			c.MaclawLLMContextLength = provider.ContextLength
			c.MaclawLLMTimeoutSec = provider.TimeoutSec
			return
		}
		return
	}

	providerName := preset.Name
	c.MaclawLLMCurrentProvider = providerName
	c.MaclawLLMProtocol = preset.Protocol
	if c.MaclawLLMProtocol == "" {
		c.MaclawLLMProtocol = "openai"
	}
	if preset.TimeoutSec > 0 {
		c.MaclawLLMTimeoutSec = preset.TimeoutSec
	}
	if preset.ContextLength > 0 {
		c.MaclawLLMContextLength = preset.ContextLength
	}
	if !preset.IsCustom {
		c.MaclawLLMUrl = strings.TrimRight(preset.URL, "/")
		c.MaclawLLMModel = preset.Model
	}
	if preset.AuthType == "none" {
		c.MaclawLLMKey = ""
	}

	found := false
	for i := range c.MaclawLLMProviders {
		if c.MaclawLLMProviders[i].Name == providerName {
			if !preset.IsCustom {
				c.MaclawLLMProviders[i].URL = strings.TrimRight(preset.URL, "/")
				c.MaclawLLMProviders[i].Model = preset.Model
			}
			c.MaclawLLMProviders[i].Protocol = c.MaclawLLMProtocol
			c.MaclawLLMProviders[i].ContextLength = c.MaclawLLMContextLength
			c.MaclawLLMProviders[i].TimeoutSec = c.MaclawLLMTimeoutSec
			c.MaclawLLMProviders[i].AgentType = preset.AgentType
			c.MaclawLLMProviders[i].AuthType = preset.AuthType
			c.MaclawLLMProviders[i].IsCustom = preset.IsCustom
			found = true
			break
		}
	}
	if !found {
		c.MaclawLLMProviders = append(c.MaclawLLMProviders, corelib.MaclawLLMProvider{
			Name:          providerName,
			URL:           c.MaclawLLMUrl,
			Key:           c.MaclawLLMKey,
			Model:         c.MaclawLLMModel,
			Protocol:      c.MaclawLLMProtocol,
			ContextLength: c.MaclawLLMContextLength,
			TimeoutSec:    c.MaclawLLMTimeoutSec,
			AgentType:     preset.AgentType,
			AuthType:      preset.AuthType,
			IsCustom:      preset.IsCustom,
		})
	}
}

func llmModelOptionsFromConfig(c *corelib.AppConfig) []string {
	values := cloneConfigValues(llmModelChoiceOpts...)
	if c == nil {
		return values
	}
	values = appendUniqueOption(values, c.MaclawLLMModel)
	for _, p := range llmProviderPresets {
		values = appendUniqueOption(values, p.Model)
	}
	for _, p := range c.MaclawLLMProviders {
		values = appendUniqueOption(values, p.Model)
	}
	return values
}

func applyLLMModelChoice(c *corelib.AppConfig, model string) {
	c.MaclawLLMModel = strings.TrimSpace(model)
	syncCurrentLLMProvider(c)
}

func syncCurrentLLMProvider(c *corelib.AppConfig) {
	name := strings.TrimSpace(c.MaclawLLMCurrentProvider)
	if name == "" {
		name = currentLLMPresetName(c)
		c.MaclawLLMCurrentProvider = name
	}
	if name == "" {
		return
	}
	if strings.TrimSpace(c.MaclawLLMKey) == "" {
		for _, provider := range c.MaclawLLMProviders {
			if strings.TrimSpace(provider.Name) == name {
				c.MaclawLLMKey = strings.TrimSpace(provider.Key)
				break
			}
		}
	}
	applyMissingLLMPresetDefaults(c, name)
	for i := range c.MaclawLLMProviders {
		if c.MaclawLLMProviders[i].Name == name {
			c.MaclawLLMProviders[i].URL = c.MaclawLLMUrl
			c.MaclawLLMProviders[i].Key = c.MaclawLLMKey
			c.MaclawLLMProviders[i].Model = c.MaclawLLMModel
			c.MaclawLLMProviders[i].Protocol = c.MaclawLLMProtocol
			c.MaclawLLMProviders[i].ContextLength = c.MaclawLLMContextLength
			c.MaclawLLMProviders[i].TimeoutSec = c.MaclawLLMTimeoutSec
			return
		}
	}
	c.MaclawLLMProviders = append(c.MaclawLLMProviders, corelib.MaclawLLMProvider{
		Name:          name,
		URL:           c.MaclawLLMUrl,
		Key:           c.MaclawLLMKey,
		Model:         c.MaclawLLMModel,
		Protocol:      c.MaclawLLMProtocol,
		ContextLength: c.MaclawLLMContextLength,
		TimeoutSec:    c.MaclawLLMTimeoutSec,
		IsCustom:      name == "Custom",
	})
}

func applyMissingLLMPresetDefaults(c *corelib.AppConfig, name string) {
	for _, preset := range llmProviderPresets {
		if preset.Name != name || preset.IsCustom {
			continue
		}
		if strings.TrimSpace(c.MaclawLLMUrl) == "" {
			c.MaclawLLMUrl = strings.TrimRight(preset.URL, "/")
		}
		if strings.TrimSpace(c.MaclawLLMModel) == "" {
			c.MaclawLLMModel = preset.Model
		}
		if strings.TrimSpace(c.MaclawLLMProtocol) == "" {
			c.MaclawLLMProtocol = preset.Protocol
		}
		if c.MaclawLLMContextLength == 0 {
			c.MaclawLLMContextLength = preset.ContextLength
		}
		if c.MaclawLLMTimeoutSec == 0 {
			c.MaclawLLMTimeoutSec = preset.TimeoutSec
		}
		if preset.AuthType == "none" {
			c.MaclawLLMKey = ""
		}
		return
	}
}

func currentWorkingDirectoryProfile(c *corelib.AppConfig) string {
	if c == nil {
		return "default_workspace"
	}
	dir := strings.TrimSpace(c.WorkingDirectory)
	if dir == "" {
		return "default_workspace"
	}
	if sameFilePath(dir, currentWorkingDirectory()) {
		return "current_directory"
	}
	if sameFilePath(dir, userHomeDirectory()) {
		return "home_directory"
	}
	return "custom"
}

func applyWorkingDirectoryProfile(c *corelib.AppConfig, profile string) {
	switch profile {
	case "default_workspace":
		c.WorkingDirectory = ""
	case "current_directory":
		c.WorkingDirectory = currentWorkingDirectory()
	case "home_directory":
		c.WorkingDirectory = userHomeDirectory()
	case "custom":
		if strings.TrimSpace(c.WorkingDirectory) == "" {
			c.WorkingDirectory = corelib.WorkspaceDir()
		}
	}
}

func currentWorkingDirectory() string {
	cwd, _ := os.Getwd()
	return strings.TrimSpace(cwd)
}

func userHomeDirectory() string {
	home, _ := os.UserHomeDir()
	return strings.TrimSpace(home)
}

func sameFilePath(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if aa, err := filepath.Abs(a); err == nil {
		a = aa
	}
	if bb, err := filepath.Abs(b); err == nil {
		b = bb
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

const (
	ConfigSetupNeedsSetup    = "needs_setup"
	ConfigSetupNeedsLLMKey   = "needs_llm_key"
	ConfigSetupNeedsRedeem   = "needs_redeem"
	ConfigSetupMCPOptional   = "mcp_optional"
	ConfigSetupOfficialReady = "official_ready"
	ConfigSetupLLMReady      = "llm_ready"
)

func currentSetupStatus(c *corelib.AppConfig) string {
	if c == nil {
		return ConfigSetupNeedsSetup
	}
	llmReady := configLLMReady(c)
	mcpReady := len(c.MCPServers)+len(c.LocalMCPServers) > 0
	if serviceRedeemUsesOfficialLLM(*c) && llmReady {
		if !mcpReady {
			return ConfigSetupMCPOptional
		}
		return ConfigSetupOfficialReady
	}
	if llmReady {
		if !mcpReady {
			return ConfigSetupMCPOptional
		}
		return ConfigSetupLLMReady
	}
	if configLLMNeedsKey(c) {
		return ConfigSetupNeedsLLMKey
	}
	hubReady := strings.TrimSpace(c.RemoteHubURL) != "" && strings.TrimSpace(c.RemoteViewerToken) != ""
	if hubReady {
		return ConfigSetupNeedsRedeem
	}
	return ConfigSetupNeedsSetup
}

// ConfigSetupStatus returns the setup readiness status used by TUI entrypoints.
func ConfigSetupStatus(cfg corelib.AppConfig) string {
	return currentSetupStatus(&cfg)
}

// ConfigLLMReady reports whether the TUI has enough local configuration to
// call the selected default LLM without asking for another required secret.
func ConfigLLMReady(cfg corelib.AppConfig) bool {
	return configLLMReady(&cfg)
}

// ConfigLLMNeedsKey reports whether a provider and model are selected, but the
// selected provider still needs an API key before chat can run.
func ConfigLLMNeedsKey(cfg corelib.AppConfig) bool {
	return configLLMNeedsKey(&cfg)
}

func configLLMReady(c *corelib.AppConfig) bool {
	if c == nil {
		return false
	}
	if strings.TrimSpace(c.MaclawLLMUrl) == "" || strings.TrimSpace(c.MaclawLLMModel) == "" {
		return false
	}
	if serviceRedeemUsesOfficialLLM(*c) {
		return currentLLMProviderKey(c) != "" || strings.TrimSpace(c.RemoteViewerToken) != ""
	}
	if llmProviderNeedsKey(c) {
		return currentLLMProviderKey(c) != ""
	}
	return true
}

func configLLMNeedsKey(c *corelib.AppConfig) bool {
	if c == nil {
		return false
	}
	if strings.TrimSpace(c.MaclawLLMUrl) == "" || strings.TrimSpace(c.MaclawLLMModel) == "" {
		return false
	}
	if serviceRedeemUsesOfficialLLM(*c) {
		return false
	}
	return llmProviderNeedsKey(c) && currentLLMProviderKey(c) == ""
}

func currentLLMProviderKey(c *corelib.AppConfig) string {
	if c == nil {
		return ""
	}
	if key := strings.TrimSpace(c.MaclawLLMKey); key != "" {
		return key
	}
	current := strings.TrimSpace(c.MaclawLLMCurrentProvider)
	if current == "" {
		current = currentLLMPresetName(c)
	}
	for _, provider := range c.MaclawLLMProviders {
		if strings.TrimSpace(provider.Name) == current {
			return strings.TrimSpace(provider.Key)
		}
	}
	return ""
}

func currentAuxLLMProfile(c *corelib.AppConfig) string {
	if c == nil {
		return "off"
	}
	aux := c.AuxiliaryLLM
	if strings.TrimSpace(aux.URL) == "" && strings.TrimSpace(aux.Key) == "" && strings.TrimSpace(aux.Model) == "" && strings.TrimSpace(aux.Protocol) == "" {
		return "off"
	}
	primaryKey := currentLLMProviderKey(c)
	primaryConfigured := strings.TrimSpace(c.MaclawLLMUrl) != "" || primaryKey != "" || strings.TrimSpace(c.MaclawLLMModel) != "" || strings.TrimSpace(c.MaclawLLMProtocol) != ""
	if primaryConfigured && sameConfigEndpoint(aux.URL, c.MaclawLLMUrl) && strings.TrimSpace(aux.Key) == primaryKey && strings.TrimSpace(aux.Model) == strings.TrimSpace(c.MaclawLLMModel) && normalizedLLMProtocol(aux.Protocol) == normalizedLLMProtocol(c.MaclawLLMProtocol) {
		return "same_as_primary"
	}
	return "custom"
}

func applyAuxLLMProfile(c *corelib.AppConfig, profile string) {
	switch profile {
	case "off":
		c.AuxiliaryLLM = corelib.AuxiliaryLLMConfig{}
	case "same_as_primary":
		c.AuxiliaryLLM = corelib.AuxiliaryLLMConfig{
			URL:      strings.TrimRight(strings.TrimSpace(c.MaclawLLMUrl), "/"),
			Key:      currentLLMProviderKey(c),
			Model:    strings.TrimSpace(c.MaclawLLMModel),
			Protocol: normalizedLLMProtocol(c.MaclawLLMProtocol),
		}
	case "custom":
		if strings.TrimSpace(c.AuxiliaryLLM.URL) == "" {
			c.AuxiliaryLLM.URL = strings.TrimRight(strings.TrimSpace(c.MaclawLLMUrl), "/")
		}
		if strings.TrimSpace(c.AuxiliaryLLM.Key) == "" {
			c.AuxiliaryLLM.Key = currentLLMProviderKey(c)
		}
		if strings.TrimSpace(c.AuxiliaryLLM.Model) == "" {
			c.AuxiliaryLLM.Model = strings.TrimSpace(c.MaclawLLMModel)
		}
		if strings.TrimSpace(c.AuxiliaryLLM.Protocol) == "" {
			c.AuxiliaryLLM.Protocol = normalizedLLMProtocol(c.MaclawLLMProtocol)
		}
	}
}

func sameConfigEndpoint(a, b string) bool {
	return strings.TrimRight(strings.TrimSpace(a), "/") == strings.TrimRight(strings.TrimSpace(b), "/")
}

func normalizedLLMProtocol(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "openai"
	}
	return v
}

func currentIMChannelProfile(c *corelib.AppConfig) string {
	if c == nil {
		return "off"
	}
	enabled := make([]string, 0, 4)
	if c.WeixinEnabled {
		enabled = append(enabled, "weixin")
	}
	if c.TelegramBotEnabled {
		enabled = append(enabled, "telegram")
	}
	if c.QQBotEnabled {
		enabled = append(enabled, "qq")
	}
	if c.LansengerEnabled {
		enabled = append(enabled, "lansenger")
	}
	if len(enabled) == 0 {
		return "off"
	}
	if len(enabled) == 1 {
		return enabled[0]
	}
	return "custom"
}

func applyIMChannelProfile(c *corelib.AppConfig, profile string) {
	if profile == "custom" {
		return
	}
	c.WeixinEnabled = false
	c.TelegramBotEnabled = false
	c.QQBotEnabled = false
	c.LansengerEnabled = false
	switch profile {
	case "weixin":
		c.WeixinEnabled = true
	case "telegram":
		c.TelegramBotEnabled = true
	case "qq":
		c.QQBotEnabled = true
	case "lansenger":
		c.LansengerEnabled = true
	}
}

func currentProxyProfile(c *corelib.AppConfig) string {
	if c == nil || !c.DefaultProxyEnabled {
		return "off"
	}
	host := strings.ToLower(strings.TrimSpace(c.DefaultProxyHost))
	if host == "localhost" {
		host = "127.0.0.1"
	}
	protocol := strings.ToLower(strings.TrimSpace(c.DefaultProxyProtocol))
	port := strings.TrimSpace(c.DefaultProxyPort)
	if host == "127.0.0.1" && protocol == "http" && port == "7890" {
		return "local_http_7890"
	}
	if host == "127.0.0.1" && protocol == "http" && port == "8080" {
		return "local_http_8080"
	}
	if host == "127.0.0.1" && protocol == "socks5" && port == "1080" {
		return "local_socks5_1080"
	}
	return "custom"
}

func applyProxyProfile(c *corelib.AppConfig, profile string) {
	switch profile {
	case "off":
		c.DefaultProxyEnabled = false
	case "local_http_7890":
		applyLocalProxy(c, "http", "7890")
	case "local_http_8080":
		applyLocalProxy(c, "http", "8080")
	case "local_socks5_1080":
		applyLocalProxy(c, "socks5", "1080")
	case "custom":
		c.DefaultProxyEnabled = true
		if strings.TrimSpace(c.DefaultProxyProtocol) == "" {
			c.DefaultProxyProtocol = "http"
		}
		if strings.TrimSpace(c.DefaultProxyHost) == "" {
			c.DefaultProxyHost = "127.0.0.1"
		}
	}
}

func applyLocalProxy(c *corelib.AppConfig, protocol, port string) {
	c.DefaultProxyEnabled = true
	c.DefaultProxyProtocol = protocol
	c.DefaultProxyHost = "127.0.0.1"
	c.DefaultProxyPort = port
	c.DefaultProxyScopeMaclaw = true
	c.DefaultProxyScopeAgent = true
}

type securityProfilePreset struct {
	Name                 string
	PolicyMode           string
	SandboxMode          string
	NetworkLevel         string
	YoloModeAllowed      bool
	SmartRouteEnabled    bool
	FileOutboundEnabled  bool
	ImageOutboundEnabled bool
}

var securityProfilePresets = []securityProfilePreset{
	{Name: "relaxed", PolicyMode: "relaxed", SandboxMode: "none", NetworkLevel: "full", YoloModeAllowed: true, SmartRouteEnabled: true, FileOutboundEnabled: true, ImageOutboundEnabled: true},
	{Name: "standard", PolicyMode: "standard", SandboxMode: "none", NetworkLevel: "full", YoloModeAllowed: true, SmartRouteEnabled: true, FileOutboundEnabled: true, ImageOutboundEnabled: true},
	{Name: "strict", PolicyMode: "strict", SandboxMode: "os", NetworkLevel: "intranet", YoloModeAllowed: false, SmartRouteEnabled: false, FileOutboundEnabled: false, ImageOutboundEnabled: false},
	{Name: "offline", PolicyMode: "strict", SandboxMode: "os", NetworkLevel: "none", YoloModeAllowed: false, SmartRouteEnabled: false, FileOutboundEnabled: false, ImageOutboundEnabled: false},
	{Name: "developer", PolicyMode: "developer", SandboxMode: "none", NetworkLevel: "full", YoloModeAllowed: true, SmartRouteEnabled: true, FileOutboundEnabled: true, ImageOutboundEnabled: true},
}

func currentSecurityProfile(c *corelib.AppConfig) string {
	if c == nil || securityFieldsAreBlank(c) {
		return "relaxed"
	}
	for _, preset := range securityProfilePresets {
		if securityProfileMatches(c, preset) {
			return preset.Name
		}
	}
	return "custom"
}

func applySecurityProfile(c *corelib.AppConfig, profile string) {
	if profile == "custom" {
		if currentSecurityProfile(c) != "custom" {
			applySecurityProfilePreset(c, securityProfilePresets[0])
			c.SandboxMode = "docker"
		}
		return
	}
	for _, preset := range securityProfilePresets {
		if preset.Name == profile {
			applySecurityProfilePreset(c, preset)
			return
		}
	}
}

func securityFieldsAreBlank(c *corelib.AppConfig) bool {
	return strings.TrimSpace(c.SecurityPolicyMode) == "" && strings.TrimSpace(c.SandboxMode) == "" && strings.TrimSpace(c.NetworkLevel) == "" && !c.YoloModeAllowed && !c.SmartRouteEnabled && !c.FileOutboundEnabled && !c.ImageOutboundEnabled
}

func securityProfileMatches(c *corelib.AppConfig, preset securityProfilePreset) bool {
	return strings.TrimSpace(c.SecurityPolicyMode) == preset.PolicyMode && strings.TrimSpace(c.SandboxMode) == preset.SandboxMode && strings.TrimSpace(c.NetworkLevel) == preset.NetworkLevel && c.YoloModeAllowed == preset.YoloModeAllowed && c.SmartRouteEnabled == preset.SmartRouteEnabled && c.FileOutboundEnabled == preset.FileOutboundEnabled && c.ImageOutboundEnabled == preset.ImageOutboundEnabled
}

func applySecurityProfilePreset(c *corelib.AppConfig, preset securityProfilePreset) {
	c.SecurityPolicyMode = preset.PolicyMode
	c.SandboxMode = preset.SandboxMode
	c.NetworkLevel = preset.NetworkLevel
	c.YoloModeAllowed = preset.YoloModeAllowed
	c.SmartRouteEnabled = preset.SmartRouteEnabled
	c.FileOutboundEnabled = preset.FileOutboundEnabled
	c.ImageOutboundEnabled = preset.ImageOutboundEnabled
}

// allConfigFields is the single source of truth.
// Order within each tab determines display order.
//
// String fields use raw closures for Get/Set (no type conversion needed).
// Boolean fields use boolGet/boolSet (string<->bool conversion).
// Integer fields use intGet/intSet (string<->int conversion).
func configDataDir() string {
	if dir := strings.TrimSpace(os.Getenv("MACLAW_DATA_DIR")); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".maclaw")
}

var allConfigFields = []ConfigFieldDef{
	// ---- Tab 0: General ----
	{
		Key: "setup_status", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescSetupStatus, Options: setupStatusOpts, ReadOnly: true,
		Get: func(c *corelib.AppConfig) string { return currentSetupStatus(c) },
		Set: func(_ *corelib.AppConfig, _ string) {},
	},
	{
		Key: "hubcenter_url", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescHubCenterURL,
		Get:     func(c *corelib.AppConfig) string { return c.RemoteHubCenterURL },
		Set: func(c *corelib.AppConfig, v string) {
			c.RemoteHubCenterURL = strings.TrimRight(strings.TrimSpace(v), "/")
		},
	},
	{
		Key: "hub_url", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescHubURL, ReadOnly: true,
		Get: func(c *corelib.AppConfig) string { return c.RemoteHubURL },
		Set: func(c *corelib.AppConfig, v string) { c.RemoteHubURL = v },
	},
	{
		Key: "token", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescToken, ReadOnly: true,
		Get: func(c *corelib.AppConfig) string { return c.RemoteMachineToken },
		Set: func(c *corelib.AppConfig, v string) { c.RemoteMachineToken = v },
	},
	{
		Key: "data_dir", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescDataDir, ReadOnly: true,
		Get: func(_ *corelib.AppConfig) string { return configDataDir() },
		Set: func(_ *corelib.AppConfig, _ string) {},
	},
	{
		Key: "working_directory_profile", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescWorkDirProfile, Options: workDirProfileOpts, Default: "default_workspace",
		Get: func(c *corelib.AppConfig) string { return currentWorkingDirectoryProfile(c) },
		Set: func(c *corelib.AppConfig, v string) { applyWorkingDirectoryProfile(c, v) },
	},
	{
		Key: "working_directory", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescWorkDir,
		Get:     func(c *corelib.AppConfig) string { return c.WorkingDirectory },
		Set:     func(c *corelib.AppConfig, v string) { c.WorkingDirectory = strings.TrimSpace(v) },
	},
	{
		Key: "language", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescLanguage, Options: []string{"zh", "en"},
		Get: func(c *corelib.AppConfig) string { return c.Language },
		Set: func(c *corelib.AppConfig, v string) { c.Language = v },
	},
	{
		Key: "max_iterations", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescMaxIterations, Options: maxIterationOpts, Default: fmt.Sprintf("%d", config.MaxAgentIterationsCap),
		Get: intGet(func(c *corelib.AppConfig) int { return c.MaclawAgentMaxIterations }),
		Set: intSet(func(c *corelib.AppConfig, v int) { c.MaclawAgentMaxIterations = v }),
	},
	{
		Key: "check_update_on_startup", Tab: CfgTabGeneral, Section: "general",
		DescKey: i18n.MsgTUIConfigDescCheckUpdate, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.CheckUpdateOnStartup }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.CheckUpdateOnStartup = v }),
	},

	// ---- Tab 1: LLM ----
	{
		Key: "maclaw_llm_provider_preset", Tab: CfgTabLLM, Section: "maclaw_llm",
		DescKey: i18n.MsgTUIConfigDescLLMProviderPreset, Options: llmProviderPresetOptions(), Default: "Custom",
		Get: func(c *corelib.AppConfig) string { return currentLLMPresetName(c) },
		Set: func(c *corelib.AppConfig, v string) { applyLLMProviderPreset(c, v) },
	},
	{
		Key: "maclaw_llm_model_choice", Tab: CfgTabLLM, Section: "maclaw_llm",
		DescKey: i18n.MsgTUIConfigDescLLMModelChoice, Options: llmModelChoiceOpts, Default: "auto",
		Get: func(c *corelib.AppConfig) string { return c.MaclawLLMModel },
		Set: func(c *corelib.AppConfig, v string) { applyLLMModelChoice(c, v) },
	},
	{
		Key: "maclaw_llm_url", Tab: CfgTabLLM, Section: "maclaw_llm",
		DescKey: i18n.MsgTUIConfigDescLLMURL,
		Get:     func(c *corelib.AppConfig) string { return c.MaclawLLMUrl },
		Set: func(c *corelib.AppConfig, v string) {
			c.MaclawLLMUrl = strings.TrimRight(v, "/")
			syncCurrentLLMProvider(c)
		},
	},
	{
		Key: "maclaw_llm_key", Tab: CfgTabLLM, Section: "maclaw_llm",
		DescKey: i18n.MsgTUIConfigDescLLMKey,
		Get:     func(c *corelib.AppConfig) string { return currentLLMProviderKey(c) },
		Set:     func(c *corelib.AppConfig, v string) { c.MaclawLLMKey = v; syncCurrentLLMProvider(c) },
	},
	{
		Key: "maclaw_llm_model", Tab: CfgTabLLM, Section: "maclaw_llm",
		DescKey: i18n.MsgTUIConfigDescLLMModel,
		Get:     func(c *corelib.AppConfig) string { return c.MaclawLLMModel },
		Set:     func(c *corelib.AppConfig, v string) { c.MaclawLLMModel = v; syncCurrentLLMProvider(c) },
	},
	{
		Key: "maclaw_llm_protocol", Tab: CfgTabLLM, Section: "maclaw_llm",
		DescKey: i18n.MsgTUIConfigDescLLMProtocol, Options: []string{"openai", "anthropic"}, Default: "openai",
		Get: func(c *corelib.AppConfig) string { return c.MaclawLLMProtocol },
		Set: func(c *corelib.AppConfig, v string) { c.MaclawLLMProtocol = v; syncCurrentLLMProvider(c) },
	},
	{
		Key: "maclaw_llm_context_length", Tab: CfgTabLLM, Section: "maclaw_llm",
		DescKey: i18n.MsgTUIConfigDescLLMContextLength, Options: contextLengthOpts,
		Get: intGet(func(c *corelib.AppConfig) int { return c.MaclawLLMContextLength }),
		Set: intSet(func(c *corelib.AppConfig, v int) { c.MaclawLLMContextLength = v; syncCurrentLLMProvider(c) }),
	},
	// Auxiliary LLM
	{
		Key: "aux_llm_profile", Tab: CfgTabLLM, Section: "aux_llm",
		DescKey: i18n.MsgTUIConfigDescAuxLLMProfile, Options: auxLLMProfileOpts, Default: "off",
		Get: func(c *corelib.AppConfig) string { return currentAuxLLMProfile(c) },
		Set: func(c *corelib.AppConfig, v string) { applyAuxLLMProfile(c, v) },
	},
	{
		Key: "aux_llm_url", Tab: CfgTabLLM, Section: "aux_llm",
		DescKey: i18n.MsgTUIConfigDescAuxLLMURL,
		Get:     func(c *corelib.AppConfig) string { return c.AuxiliaryLLM.URL },
		Set:     func(c *corelib.AppConfig, v string) { c.AuxiliaryLLM.URL = v },
	},
	{
		Key: "aux_llm_key", Tab: CfgTabLLM, Section: "aux_llm",
		DescKey: i18n.MsgTUIConfigDescAuxLLMKey,
		Get:     func(c *corelib.AppConfig) string { return c.AuxiliaryLLM.Key },
		Set:     func(c *corelib.AppConfig, v string) { c.AuxiliaryLLM.Key = v },
	},
	{
		Key: "aux_llm_model", Tab: CfgTabLLM, Section: "aux_llm",
		DescKey: i18n.MsgTUIConfigDescAuxLLMModel,
		Get:     func(c *corelib.AppConfig) string { return c.AuxiliaryLLM.Model },
		Set:     func(c *corelib.AppConfig, v string) { c.AuxiliaryLLM.Model = v },
	},
	{
		Key: "aux_llm_protocol", Tab: CfgTabLLM, Section: "aux_llm",
		DescKey: i18n.MsgTUIConfigDescAuxLLMProtocol, Options: []string{"openai", "anthropic"}, Default: "openai",
		Get: func(c *corelib.AppConfig) string { return c.AuxiliaryLLM.Protocol },
		Set: func(c *corelib.AppConfig, v string) { c.AuxiliaryLLM.Protocol = v },
	},

	// ---- Tab 2: IM ----
	{
		Key: "im_channel_profile", Tab: CfgTabIM, Section: "im_overview",
		DescKey: i18n.MsgTUIConfigDescIMChannelProfile, Options: imChannelProfileOpts, Default: "off",
		Get: func(c *corelib.AppConfig) string { return currentIMChannelProfile(c) },
		Set: func(c *corelib.AppConfig, v string) { applyIMChannelProfile(c, v) },
	},
	{
		Key: "qqbot_enabled", Tab: CfgTabIM, Section: "qqbot",
		DescKey: i18n.MsgTUIConfigDescQQBotEnabled, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.QQBotEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.QQBotEnabled = v }),
	},
	{
		Key: "qqbot_app_id", Tab: CfgTabIM, Section: "qqbot",
		DescKey: i18n.MsgTUIConfigDescQQBotAppID,
		Get:     func(c *corelib.AppConfig) string { return c.QQBotAppID },
		Set:     func(c *corelib.AppConfig, v string) { c.QQBotAppID = v },
	},
	{
		Key: "qqbot_app_secret", Tab: CfgTabIM, Section: "qqbot",
		DescKey: i18n.MsgTUIConfigDescQQBotAppSecret,
		Get:     func(c *corelib.AppConfig) string { return c.QQBotAppSecret },
		Set:     func(c *corelib.AppConfig, v string) { c.QQBotAppSecret = v },
	},
	{
		Key: "telegram_bot_enabled", Tab: CfgTabIM, Section: "telegram",
		DescKey: i18n.MsgTUIConfigDescTelegramEnabled, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.TelegramBotEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.TelegramBotEnabled = v }),
	},
	{
		Key: "telegram_bot_token", Tab: CfgTabIM, Section: "telegram",
		DescKey: i18n.MsgTUIConfigDescTelegramToken,
		Get:     func(c *corelib.AppConfig) string { return c.TelegramBotToken },
		Set:     func(c *corelib.AppConfig, v string) { c.TelegramBotToken = v },
	},
	{
		Key: "weixin_enabled", Tab: CfgTabIM, Section: "weixin",
		DescKey: i18n.MsgTUIConfigDescWeixinEnabled, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.WeixinEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.WeixinEnabled = v }),
	},
	{
		Key: "weixin_token", Tab: CfgTabIM, Section: "weixin",
		DescKey: i18n.MsgTUIConfigDescWeixinToken, ReadOnly: true,
		Get: func(c *corelib.AppConfig) string { return c.WeixinToken },
		Set: func(c *corelib.AppConfig, v string) { c.WeixinToken = v },
	},
	{
		Key: "weixin_base_url", Tab: CfgTabIM, Section: "weixin",
		DescKey: i18n.MsgTUIConfigDescWeixinBaseURL,
		Get:     func(c *corelib.AppConfig) string { return c.WeixinBaseURL },
		Set:     func(c *corelib.AppConfig, v string) { c.WeixinBaseURL = v },
	},
	{
		Key: "lansenger_enabled", Tab: CfgTabIM, Section: "lansenger",
		DescKey: i18n.MsgTUIConfigDescLansengerEnabled, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.LansengerEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.LansengerEnabled = v }),
	},
	{
		Key: "lansenger_app_id", Tab: CfgTabIM, Section: "lansenger",
		DescKey: i18n.MsgTUIConfigDescLansengerAppID,
		Get:     func(c *corelib.AppConfig) string { return c.LansengerAppID },
		Set:     func(c *corelib.AppConfig, v string) { c.LansengerAppID = v },
	},
	{
		Key: "lansenger_app_secret", Tab: CfgTabIM, Section: "lansenger",
		DescKey: i18n.MsgTUIConfigDescLansengerAppSecret,
		Get:     func(c *corelib.AppConfig) string { return c.LansengerAppSecret },
		Set:     func(c *corelib.AppConfig, v string) { c.LansengerAppSecret = v },
	},
	{
		Key: "lansenger_gateway_url", Tab: CfgTabIM, Section: "lansenger",
		DescKey: i18n.MsgTUIConfigDescLansengerGateway,
		Get:     func(c *corelib.AppConfig) string { return c.LansengerGatewayURL },
		Set:     func(c *corelib.AppConfig, v string) { c.LansengerGatewayURL = v },
	},

	// ---- Tab 3: Proxy ----
	{
		Key: "default_proxy_profile", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyProfile, Options: proxyProfileOpts, Default: "off",
		Get: func(c *corelib.AppConfig) string { return currentProxyProfile(c) },
		Set: func(c *corelib.AppConfig, v string) { applyProxyProfile(c, v) },
	},
	{
		Key: "default_proxy_enabled", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyEnabled, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.DefaultProxyEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.DefaultProxyEnabled = v }),
	},
	{
		Key: "default_proxy_protocol", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyProtocol, Options: []string{"http", "https", "socks5"}, Default: "http",
		Get: func(c *corelib.AppConfig) string { return c.DefaultProxyProtocol },
		Set: func(c *corelib.AppConfig, v string) { c.DefaultProxyProtocol = v },
	},
	{
		Key: "default_proxy_host", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyHost,
		Get:     func(c *corelib.AppConfig) string { return c.DefaultProxyHost },
		Set:     func(c *corelib.AppConfig, v string) { c.DefaultProxyHost = v },
	},
	{
		Key: "default_proxy_port", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyPort, Options: proxyPortOpts,
		Get: func(c *corelib.AppConfig) string { return c.DefaultProxyPort },
		Set: func(c *corelib.AppConfig, v string) { c.DefaultProxyPort = v },
	},
	{
		Key: "default_proxy_username", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyUser,
		Get:     func(c *corelib.AppConfig) string { return c.DefaultProxyUsername },
		Set:     func(c *corelib.AppConfig, v string) { c.DefaultProxyUsername = v },
	},
	{
		Key: "default_proxy_password", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyPass,
		Get:     func(c *corelib.AppConfig) string { return c.DefaultProxyPassword },
		Set:     func(c *corelib.AppConfig, v string) { c.DefaultProxyPassword = v },
	},
	{
		Key: "default_proxy_scope_maclaw", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyScopeLLM, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.DefaultProxyScopeMaclaw }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.DefaultProxyScopeMaclaw = v }),
	},
	{
		Key: "default_proxy_scope_agent", Tab: CfgTabProxy, Section: "proxy",
		DescKey: i18n.MsgTUIConfigDescProxyScopeAgent, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.DefaultProxyScopeAgent }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.DefaultProxyScopeAgent = v }),
	},

	// ---- Tab 4: Security ----
	{
		Key: "security_profile", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescSecurityProfile, Options: securityProfileOpts, Default: "relaxed",
		Get: func(c *corelib.AppConfig) string { return currentSecurityProfile(c) },
		Set: func(c *corelib.AppConfig, v string) { applySecurityProfile(c, v) },
	},
	{
		Key: "security_policy_mode", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescSecurityMode, Options: []string{"none", "relaxed", "standard", "strict", "developer"}, Default: "relaxed",
		Get: func(c *corelib.AppConfig) string { return c.SecurityPolicyMode },
		Set: func(c *corelib.AppConfig, v string) {
			if v == "permissive" {
				v = "relaxed"
			}
			c.SecurityPolicyMode = v
		},
	},
	{
		Key: "sandbox_mode", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescSandbox, Options: []string{"none", "os", "docker"}, Default: "none",
		Get: func(c *corelib.AppConfig) string { return c.SandboxMode },
		Set: func(c *corelib.AppConfig, v string) { c.SandboxMode = v },
	},
	{
		Key: "network_level", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescNetworkLevel, Options: []string{"none", "intranet", "allowlist", "full"}, Default: "full",
		Get: func(c *corelib.AppConfig) string { return c.NetworkLevel },
		Set: func(c *corelib.AppConfig, v string) { c.NetworkLevel = v },
	},
	{
		Key: "yolo_mode_allowed", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescYoloMode, Options: boolOpts, Default: "true",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.YoloModeAllowed }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.YoloModeAllowed = v }),
	},
	{
		Key: "smart_route_enabled", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescSmartRoute, Options: boolOpts, Default: "true",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.SmartRouteEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.SmartRouteEnabled = v }),
	},
	{
		Key: "file_outbound_enabled", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescFileOutbound, Options: boolOpts, Default: "true",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.FileOutboundEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.FileOutboundEnabled = v }),
	},
	{
		Key: "image_outbound_enabled", Tab: CfgTabSecurity, Section: "security",
		DescKey: i18n.MsgTUIConfigDescImageOutbound, Options: boolOpts, Default: "true",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.ImageOutboundEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.ImageOutboundEnabled = v }),
	},

	// ---- Tab 5: Advanced ----
	{
		Key: "skill_purchase_mode", Tab: CfgTabAdvanced, Section: "skillmarket",
		DescKey: i18n.MsgTUIConfigDescSkillPurchaseMode, Options: []string{"auto", "free_only"}, Default: "auto",
		Get: func(c *corelib.AppConfig) string { return c.SkillPurchaseMode },
		Set: func(c *corelib.AppConfig, v string) { c.SkillPurchaseMode = v },
	},
	{
		Key: "ui_mode", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescUIMode, Options: []string{"pro", "lite"}, Default: "lite",
		Get: func(c *corelib.AppConfig) string { return c.UIMode },
		Set: func(c *corelib.AppConfig, v string) { c.UIMode = v },
	},
	{
		Key: "memory_auto_compress", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescMemoryCompress, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.MemoryAutoCompress }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.MemoryAutoCompress = v }),
	},
	{
		Key: "log_detail_enabled", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescLogDetail, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.LogDetailEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.LogDetailEnabled = v }),
	},
	{
		Key: "llm_trajectory_logging", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescTrajectory, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.LLMTrajectoryLogging }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.LLMTrajectoryLogging = v }),
	},
	{
		Key: "maclaw_debug_tool_calls", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescDebugTools, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.MaclawDebugToolCalls }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.MaclawDebugToolCalls = v }),
	},
	{
		Key: "gossip_enabled", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescGossip, Options: boolOpts, Default: "true",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.GossipEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.GossipEnabled = v }),
	},
	{
		Key: "trial_reflect_enabled", Tab: CfgTabAdvanced, Section: "advanced",
		DescKey: i18n.MsgTUIConfigDescTrialReflect, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.TrialReflectEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.TrialReflectEnabled = v }),
	},
	{
		Key: "local_needle_enabled", Tab: CfgTabAdvanced, Section: "needle",
		DescKey: i18n.MsgTUIConfigDescLocalNeedleEnabled, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.LocalNeedleEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.LocalNeedleEnabled = v }),
	},
	{
		Key: "local_needle_log_enabled", Tab: CfgTabAdvanced, Section: "needle",
		DescKey: i18n.MsgTUIConfigDescLocalNeedleLog, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.LocalNeedleLogEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.LocalNeedleLogEnabled = v }),
	},
	{
		Key: "local_needle_training_export_enabled", Tab: CfgTabAdvanced, Section: "needle",
		DescKey: i18n.MsgTUIConfigDescLocalNeedleExport, Options: boolOpts, Default: "false",
		Get: boolGet(func(c *corelib.AppConfig) bool { return c.LocalNeedleTrainingExportEnabled }),
		Set: boolSet(func(c *corelib.AppConfig, v bool) { c.LocalNeedleTrainingExportEnabled = v }),
	},
	{
		Key: "local_needle_model_path", Tab: CfgTabAdvanced, Section: "needle",
		DescKey: i18n.MsgTUIConfigDescLocalNeedleModel,
		Get:     func(c *corelib.AppConfig) string { return c.LocalNeedleModelPath },
		Set:     func(c *corelib.AppConfig, v string) { c.LocalNeedleModelPath = strings.TrimSpace(v) },
	},
	{
		Key: "local_needle_min_confidence", Tab: CfgTabAdvanced, Section: "needle",
		DescKey: i18n.MsgTUIConfigDescLocalNeedleMinConfidence, Options: []string{"0.78", "0.85", "0.9", "0.95"}, Default: "0.78",
		Get: floatGet(func(c *corelib.AppConfig) float64 { return c.LocalNeedleMinConfidence }),
		Set: floatSet(func(c *corelib.AppConfig, v float64) { c.LocalNeedleMinConfidence = v }),
	},
}

// configFieldIndex builds a key->*ConfigFieldDef lookup map (built once, reused).
var configFieldIndex map[string]*ConfigFieldDef

func init() {
	configFieldIndex = make(map[string]*ConfigFieldDef, len(allConfigFields))
	for i := range allConfigFields {
		configFieldIndex[allConfigFields[i].Key] = &allConfigFields[i]
	}
}

func ConfigFieldKeys() []string {
	keys := make([]string, 0, len(allConfigFields))
	for _, def := range allConfigFields {
		keys = append(keys, def.Key)
	}
	return keys
}

// ApplyConfigValue applies a TUI config key+value to an AppConfig.
// This is the ONLY write path -- called by app.go's saveConfig.
func ApplyConfigValue(cfg *corelib.AppConfig, key, value string) {
	if def, ok := configFieldIndex[key]; ok && def.Set != nil {
		def.Set(cfg, value)
	}
}

// LoadConfigValue reads a TUI config key from an AppConfig.
// Returns ("", false) if the key is unknown.
func LoadConfigValue(cfg *corelib.AppConfig, key string) (string, bool) {
	if def, ok := configFieldIndex[key]; ok && def.Get != nil {
		return def.Get(cfg), true
	}
	return "", false
}

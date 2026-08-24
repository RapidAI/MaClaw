package main

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	"github.com/pkg/browser"
)

// ExternalAgentImportSkip explains why one local agent was not imported.
type ExternalAgentImportSkip struct {
	Source string `json:"source"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// ExternalAgentImportResult is returned by ImportExternalAgents.
type ExternalAgentImportResult struct {
	Imported []string                   `json:"imported"`
	Skipped  []ExternalAgentImportSkip  `json:"skipped"`
	Current  string                     `json:"current"`
}

var (
	externalAgentImportMu sync.Mutex
	// test hooks
	scanExternalAgentsForTest func() []configfile.ExternalAgentCandidate
	testImportedAgentForTest  func(corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error)
	listImportedModelsForTest func(corelib.MaclawLLMConfig) []string
	openURLMu                 sync.Mutex
	openURLForTest            func(rawURL string) error
)

// OpenCodeZenLoginResult is returned by StartOpenCodeZenLogin.
type OpenCodeZenLoginResult struct {
	Message string `json:"message"`
	Key     string `json:"key,omitempty"`
}

// StartOpenCodeZenLogin opens OpenCode in the system browser so the user
// can copy an API key. If a local OpenCode CLI key is already present, it
// is returned so the settings form can fill it; the current provider is
// not switched.
func (a *App) StartOpenCodeZenLogin() (OpenCodeZenLoginResult, error) {
	if err := openExternalURL(configfile.OpenCodeZenAuthURL); err != nil {
		return OpenCodeZenLoginResult{}, fmt.Errorf("打开 OpenCode Zen 登录页失败: %w", err)
	}
	return OpenCodeZenLoginResult{
		Message: "已打开 OpenCode。登录后请到 API Keys 页复制密钥，粘贴到下方再检测并保存。",
		Key:     strings.TrimSpace(configfile.ReadOpenCodeZenKey()),
	}, nil
}

func openExternalURL(rawURL string) error {
	openURLMu.Lock()
	fn := openURLForTest
	openURLMu.Unlock()
	if fn != nil {
		return fn(rawURL)
	}
	return browser.OpenURL(rawURL)
}

func setOpenURLForTest(fn func(rawURL string) error) {
	openURLMu.Lock()
	openURLForTest = fn
	openURLMu.Unlock()
}

// ImportExternalAgents scans local Codex / Claude Code / OpenCode configs,
// live-tests each candidate, and appends only authenticated providers.
// The current provider is never changed.
func (a *App) ImportExternalAgents() (ExternalAgentImportResult, error) {
	return a.importExternalAgents(false)
}

func (a *App) maybeImportExternalAgentsOnce() {
	if a == nil {
		return
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		log.Printf("[external-agent-import] skip first-start scan: load config: %v", err)
		return
	}
	if cfg.ExternalAgentImportAttempted {
		return
	}
	result, err := a.importExternalAgents(true)
	if markErr := a.markExternalAgentImportAttempted(); markErr != nil {
		log.Printf("[external-agent-import] mark attempted failed: %v", markErr)
	}
	if err != nil {
		log.Printf("[external-agent-import] first-start scan failed: %v", err)
		return
	}
	log.Printf("[external-agent-import] first-start imported=%v skipped=%d current=%s", result.Imported, len(result.Skipped), result.Current)
}

func (a *App) markExternalAgentImportAttempted() error {
	_, err := a.PatchConfigIfChanged(func(current *corelib.AppConfig) bool {
		if current.ExternalAgentImportAttempted {
			return false
		}
		current.ExternalAgentImportAttempted = true
		return true
	})
	return err
}

func (a *App) importExternalAgents(auto bool) (ExternalAgentImportResult, error) {
	externalAgentImportMu.Lock()
	defer externalAgentImportMu.Unlock()

	state := a.GetMaclawLLMProviders()
	result := ExternalAgentImportResult{Current: state.Current}

	candidates := scanExternalAgentCandidates()
	for i := range candidates {
		if prev, _, ok := findImportedProviderBySource(state.Providers, candidates[i].Source); ok {
			candidates[i].PreferredModel = prev.Model
			continue
		}
		if prev, ok := findImportedProviderByName(state.Providers, candidates[i].Name); ok {
			candidates[i].PreferredModel = prev.Model
		}
	}
	if len(candidates) == 0 {
		if !auto {
			result.Skipped = append(result.Skipped, ExternalAgentImportSkip{
				Source: "",
				Name:   "",
				Reason: "未发现 Codex / Claude Code / OpenCode 的可用本地配置",
			})
		}
		return result, nil
	}

	providers := append([]corelib.MaclawLLMProvider(nil), state.Providers...)
	type oneResult struct {
		provider     corelib.MaclawLLMProvider
		skip         ExternalAgentImportSkip
		visionStatus string
		err          error
	}
	results := make([]oneResult, len(candidates))
	var wg sync.WaitGroup
	for i, candidate := range candidates {
		wg.Add(1)
		go func(i int, candidate configfile.ExternalAgentCandidate) {
			defer wg.Done()
			imported, skip, visionStatus, err := a.importOneExternalAgent(candidate)
			results[i] = oneResult{provider: imported, skip: skip, visionStatus: visionStatus, err: err}
		}(i, candidate)
	}
	wg.Wait()

	visionByName := map[string]string{}
	changed := false
	for i, candidate := range candidates {
		item := results[i]
		if item.err != nil {
			result.Skipped = append(result.Skipped, ExternalAgentImportSkip{
				Source: candidate.Source,
				Name:   candidate.Name,
				Reason: item.err.Error(),
			})
			continue
		}
		if item.skip.Reason != "" {
			result.Skipped = append(result.Skipped, item.skip)
			continue
		}
		previous, had := findExistingImportedProvider(providers, item.provider)
		item.provider = mergeImportedVision(previous, item.provider, item.visionStatus)
		next, replaced := upsertImportedProvider(providers, item.provider)
		if !replaced {
			result.Skipped = append(result.Skipped, ExternalAgentImportSkip{
				Source: candidate.Source,
				Name:   candidate.Name,
				Reason: "已存在同名服务商，未覆盖",
			})
			continue
		}
		providers = next
		result.Imported = append(result.Imported, item.provider.Name)
		visionByName[item.provider.Name] = item.visionStatus
		if !had || importedProviderNeedsWrite(previous, item.provider) {
			changed = true
		}
	}
	if !changed {
		return result, nil
	}

	current := strings.TrimSpace(state.Current)
	if current == "" {
		current = firstNonImportedProviderName(providers, state.Current)
	}
	if err := a.SaveMaclawLLMProviders(providers, current); err != nil {
		return result, fmt.Errorf("save imported providers: %w", err)
	}
	// Re-read so durable IDs exist, then persist the live-test proof without
	// switching the current assistant assignment.
	saved := a.GetMaclawLLMProviders()
	for _, name := range result.Imported {
		provider, ok := findImportedProviderByName(saved.Providers, name)
		if !ok {
			continue
		}
		tested := a.materializeMaclawLLMProvider(provider)
		if err := a.markMaclawLLMProviderConnectionTestPassed(name, tested); err != nil {
			log.Printf("[external-agent-import] mark tested %s: %v", name, err)
		}
		if status := visionByName[name]; status != "" && status != string(visionProbeInconclusive) {
			if err := a.saveVisionProbeResultForProvider(name, provider.Model, status == string(visionProbeSupported)); err != nil {
				log.Printf("[external-agent-import] save vision %s: %v", name, err)
			}
		}
	}
	result.Current = saved.Current
	return result, nil
}

func (a *App) importOneExternalAgent(candidate configfile.ExternalAgentCandidate) (corelib.MaclawLLMProvider, ExternalAgentImportSkip, string, error) {
	skip := ExternalAgentImportSkip{Source: candidate.Source, Name: candidate.Name}
	if strings.TrimSpace(candidate.URL) == "" || strings.TrimSpace(candidate.Key) == "" {
		skip.Reason = "配置不完整"
		return corelib.MaclawLLMProvider{}, skip, "", nil
	}
	if strings.TrimSpace(candidate.Model) == "" {
		skip.Reason = "未找到模型"
		return corelib.MaclawLLMProvider{}, skip, "", nil
	}

	agentType := strings.TrimSpace(candidate.AgentType)
	if agentType == "" {
		agentType = configfile.ExternalAgentType(candidate.Source)
	}
	candidate.AgentType = agentType
	scannedModel := strings.TrimSpace(candidate.Model)
	if prefer := strings.TrimSpace(candidate.PreferredModel); prefer != "" {
		candidate.Model = prefer
	}
	llmCfg := importedCandidateConfig(candidate)
	testResult, err := authenticateImportedCandidate(a, &candidate, llmCfg, false)
	if err != nil && scannedModel != "" && !strings.EqualFold(scannedModel, candidate.Model) {
		candidate.Model = scannedModel
		llmCfg = importedCandidateConfig(candidate)
		testResult, err = authenticateImportedCandidate(a, &candidate, llmCfg, false)
	}
	if err != nil && candidate.Source == configfile.ExternalAgentSourceOpenCode && !isImportedAuthFailure(err) {
		testResult, err = authenticateImportedCandidate(a, &candidate, llmCfg, true)
	}
	if err != nil {
		skip.Reason = "认证未通过: " + err.Error()
		return corelib.MaclawLLMProvider{}, skip, "", nil
	}
	llmCfg = importedCandidateConfig(candidate)

	models := listImportedAgentModels(a, llmCfg, candidate)
	if len(models) == 0 {
		models = []string{candidate.Model}
	} else if !containsStringFold(models, candidate.Model) {
		models = append([]string{candidate.Model}, models...)
	}

	provider := candidate.ToProvider(models)
	if isOpenCodeIncoming(provider) {
		provider = normalizeOpenCodeProvider(provider, defaultOpenCodeProvider())
	}
	if testResult.VisionProbeStatus == string(visionProbeSupported) && candidate.Model != "" {
		provider.SupportsVision = true
		provider.VisionModels = []string{candidate.Model}
	}
	return provider, ExternalAgentImportSkip{}, testResult.VisionProbeStatus, nil
}

func scanExternalAgentCandidates() []configfile.ExternalAgentCandidate {
	if scanExternalAgentsForTest != nil {
		return scanExternalAgentsForTest()
	}
	return configfile.ScanExternalAgents()
}

func importedCandidateConfig(candidate configfile.ExternalAgentCandidate) corelib.MaclawLLMConfig {
	return corelib.MaclawLLMConfig{
		URL:           candidate.URL,
		Key:           candidate.Key,
		Model:         candidate.Model,
		Protocol:      candidate.Protocol,
		WireAPI:       candidate.WireAPI,
		AgentType:     candidate.AgentType,
		AuthType:      candidate.AuthType,
		ProviderName:  candidate.Name,
		ContextLength: candidate.ContextLength,
		TimeoutSec:    corelib.DefaultLLMTimeoutSec,
	}
}

func authenticateImportedCandidate(a *App, candidate *configfile.ExternalAgentCandidate, llmCfg corelib.MaclawLLMConfig, allowAlternateModels bool) (corelib.MaclawLLMTestResult, error) {
	testResult, err := testImportedAgentConnection(a, llmCfg)
	if err != nil && shouldRetryImportedAgentAsChat(*candidate, err) {
		log.Printf("[external-agent-import] %s %s failed (%v); retrying as chat", candidate.Name, candidate.WireAPI, err)
		candidate.WireAPI = ""
		llmCfg.WireAPI = ""
		testResult, err = testImportedAgentConnection(a, llmCfg)
	}
	if err == nil || !allowAlternateModels || candidate.Source != configfile.ExternalAgentSourceOpenCode {
		return testResult, err
	}
	if isImportedAuthFailure(err) {
		return testResult, err
	}
	models := listImportedAgentModels(a, llmCfg, *candidate)
	tried := 0
	for _, model := range models {
		if strings.EqualFold(model, candidate.Model) {
			continue
		}
		if tried >= 3 {
			break
		}
		tried++
		llmCfg.Model = model
		alt, altErr := testImportedAgentConnection(a, llmCfg)
		if altErr == nil {
			candidate.Model = model
			return alt, nil
		}
	}
	return testResult, err
}

func testImportedAgentConnection(a *App, cfg corelib.MaclawLLMConfig) (corelib.MaclawLLMTestResult, error) {
	if testImportedAgentForTest != nil {
		return testImportedAgentForTest(cfg)
	}
	return a.TestMaclawLLM(cfg)
}

func shouldRetryImportedAgentAsChat(candidate configfile.ExternalAgentCandidate, err error) bool {
	if err == nil {
		return false
	}
	if candidate.Source != configfile.ExternalAgentSourceCodex {
		return false
	}
	wire := strings.ToLower(strings.TrimSpace(candidate.WireAPI))
	if wire != "responses" && wire != "responses-ws" {
		return false
	}
	if strings.Contains(strings.ToLower(candidate.URL), "chatgpt.com") {
		return false
	}
	msg := strings.ToLower(err.Error())
	return containsHTTPStatus(msg, "400", "404", "405") ||
		strings.Contains(msg, "method not allowed") ||
		strings.Contains(msg, "接口或模型不存在") ||
		looksLikeMissingEndpoint(msg)
}

func looksLikeMissingEndpoint(msg string) bool {
	if !strings.Contains(msg, "not found") {
		return false
	}
	return strings.Contains(msg, "http") ||
		strings.Contains(msg, "endpoint") ||
		strings.Contains(msg, "route") ||
		strings.Contains(msg, "接口")
}

func isImportedAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return containsHTTPStatus(msg, "401", "403") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "invalid api key") ||
		strings.Contains(msg, "api key is empty")
}

func containsHTTPStatus(msg string, codes ...string) bool {
	for _, code := range codes {
		if code == "" {
			continue
		}
		for _, pat := range []string{
			"http " + code,
			"status " + code,
			"(" + code + ")",
			"（" + code + "）",
			" " + code + ":",
			" " + code + " ",
		} {
			if strings.Contains(msg, pat) {
				return true
			}
		}
		if strings.HasPrefix(msg, code+":") || strings.HasPrefix(msg, code+" ") {
			return true
		}
	}
	return false
}

func listImportedAgentModels(a *App, cfg corelib.MaclawLLMConfig, candidate configfile.ExternalAgentCandidate) []string {
	if listImportedModelsForTest != nil {
		return listImportedModelsForTest(cfg)
	}
	items, err := a.fetchProviderModels(cfg.URL, cfg.Key, cfg.Protocol, cfg.UserAgent(), true)
	if err != nil {
		log.Printf("[external-agent-import] list models %s: %v", candidate.Name, err)
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func mergeImportedVision(existing, incoming corelib.MaclawLLMProvider, visionStatus string) corelib.MaclawLLMProvider {
	if strings.TrimSpace(existing.ImportSource) == "" && existing.Name == "" {
		return incoming
	}
	merged := existing.VisionModels
	switch visionStatus {
	case string(visionProbeSupported):
		incoming.VisionModels = visionModelsWithResult(merged, incoming.Model, true)
		incoming.SupportsVision = true
	case string(visionProbeUnsupported):
		incoming.VisionModels = visionModelsWithResult(merged, incoming.Model, false)
		incoming.SupportsVision = false
	default:
		incoming.VisionModels = normalizeVisionModelIDs(merged)
		incoming.SupportsVision = false
		for _, model := range incoming.VisionModels {
			if strings.EqualFold(strings.TrimSpace(model), strings.TrimSpace(incoming.Model)) {
				incoming.SupportsVision = true
				break
			}
		}
	}
	return incoming
}

func importedProviderNeedsWrite(existing, incoming corelib.MaclawLLMProvider) bool {
	if !existing.ConnectionTestPassed {
		return true
	}
	return !sameImportedConnection(existing, incoming)
}

func sameImportedConnection(a, b corelib.MaclawLLMProvider) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(a.URL), "/"), strings.TrimRight(strings.TrimSpace(b.URL), "/")) &&
		strings.TrimSpace(a.Key) == strings.TrimSpace(b.Key) &&
		strings.EqualFold(strings.TrimSpace(a.Model), strings.TrimSpace(b.Model)) &&
		strings.EqualFold(strings.TrimSpace(a.Protocol), strings.TrimSpace(b.Protocol)) &&
		strings.EqualFold(strings.TrimSpace(a.WireAPI), strings.TrimSpace(b.WireAPI)) &&
		strings.EqualFold(strings.TrimSpace(a.AgentType), strings.TrimSpace(b.AgentType)) &&
		strings.EqualFold(strings.TrimSpace(a.AuthType), strings.TrimSpace(b.AuthType)) &&
		stringSlicesEqualFold(a.Models, b.Models) &&
		stringSlicesEqualFold(a.VisionModels, b.VisionModels)
}

func stringSlicesEqualFold(a, b []string) bool {
	left := normalizeVisionModelIDs(a)
	right := normalizeVisionModelIDs(b)
	if len(left) != len(right) {
		return false
	}
	used := make([]bool, len(right))
	for _, item := range left {
		found := false
		for i, other := range right {
			if used[i] {
				continue
			}
			if strings.EqualFold(item, other) {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func findImportedProviderBySource(providers []corelib.MaclawLLMProvider, source string) (corelib.MaclawLLMProvider, int, bool) {
	source = strings.TrimSpace(source)
	if source == "" {
		return corelib.MaclawLLMProvider{}, -1, false
	}
	for i, existing := range providers {
		if strings.EqualFold(strings.TrimSpace(existing.ImportSource), source) {
			return existing, i, true
		}
	}
	return corelib.MaclawLLMProvider{}, -1, false
}

func findExistingImportedProvider(providers []corelib.MaclawLLMProvider, incoming corelib.MaclawLLMProvider) (corelib.MaclawLLMProvider, bool) {
	if prev, _, ok := findImportedProviderBySource(providers, incoming.ImportSource); ok {
		return prev, true
	}
	if isOpenCodeIncoming(incoming) {
		if prev, ok := findImportedProviderByName(providers, configfile.ExternalAgentProviderOpenCode); ok {
			return prev, true
		}
	}
	return corelib.MaclawLLMProvider{}, false
}

func upsertImportedProvider(providers []corelib.MaclawLLMProvider, incoming corelib.MaclawLLMProvider) ([]corelib.MaclawLLMProvider, bool) {
	src := strings.TrimSpace(incoming.ImportSource)
	for i, existing := range providers {
		if src != "" && strings.EqualFold(strings.TrimSpace(existing.ImportSource), src) {
			incoming = overlayImportedProvider(existing, incoming)
			providers[i] = incoming
			return providers, true
		}
	}
	for i, existing := range providers {
		if !corelib.MaclawLLMProviderNameEqual(existing.Name, incoming.Name) {
			continue
		}
		if isOpenCodeIncoming(incoming) && isOpenCodeZenProvider(existing) {
			incoming = overlayImportedProvider(existing, incoming)
			providers[i] = incoming
			return providers, true
		}
		if strings.TrimSpace(existing.ImportSource) != "" {
			continue
		}
		return providers, false
	}
	if isOpenCodeIncoming(incoming) {
		incoming.ImportSource = ""
		incoming.Name = configfile.ExternalAgentProviderOpenCode
		incoming.IsCustom = false
	}
	out := make([]corelib.MaclawLLMProvider, 0, len(providers)+1)
	out = append(out, incoming)
	out = append(out, providers...)
	return out, true
}

func isOpenCodeIncoming(incoming corelib.MaclawLLMProvider) bool {
	return isOpenCodePresetSlot(incoming)
}

func overlayImportedProvider(existing, incoming corelib.MaclawLLMProvider) corelib.MaclawLLMProvider {
	incoming.ID = existing.ID
	if isOpenCodeIncoming(incoming) && isOpenCodeZenProvider(existing) {
		if name := strings.TrimSpace(existing.Name); name != "" {
			incoming.Name = name
		} else {
			incoming.Name = configfile.ExternalAgentProviderOpenCode
		}
		incoming.ImportSource = ""
		incoming.IsCustom = false
	}
	return incoming
}

func firstNonImportedProviderName(providers []corelib.MaclawLLMProvider, fallback string) string {
	for _, p := range providers {
		if strings.TrimSpace(p.ImportSource) == "" && strings.TrimSpace(p.Name) != "" {
			return p.Name
		}
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	if len(providers) > 0 {
		return providers[0].Name
	}
	return ""
}

func findImportedProviderByName(providers []corelib.MaclawLLMProvider, name string) (corelib.MaclawLLMProvider, bool) {
	for _, p := range providers {
		if corelib.MaclawLLMProviderNameEqual(p.Name, name) {
			return p, true
		}
	}
	return corelib.MaclawLLMProvider{}, false
}

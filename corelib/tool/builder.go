package tool

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// DynamicToolBuilder builds legacy LLM tool definitions dynamically from the
// Registry. Host-controlled dynamic gateways (for example manage_skill and
// call_mcp_tool) are never emitted here: their provider/resource identity must
// be bound by a managed surface before a model can invoke them. When the total
// available tools exceed maxDirectTools, context-aware filtering keeps builtin
// tools and fills the remaining slots with relevant static tools.
type DynamicToolBuilder struct {
	// mu serializes configuration changes with Build. Build mutates its cached
	// BM25 index, so allowing activation to swap the registry or hybrid retriever
	// concurrently can otherwise race with an in-flight IM request.
	mu             sync.Mutex
	registry       *Registry
	maxDirectTools int              // threshold before filtering kicks in (default 20)
	maxDynamic     int              // max non-builtin tools when filtering (default 15)
	bm25Index      *bm25.Index      // cached BM25 index, reused across Build calls
	hybrid         *HybridRetriever // nil when no embedder set
	enrichStore    *EnrichmentStore
	tracker        *UsageTracker
	reranker       Reranker // nil when reranking is disabled
	skillProvider  SkillProvider
	skillBM25      *bm25.Index // separate index for skill trigger matching
}

// NewDynamicToolBuilder creates a builder backed by the given registry.
func NewDynamicToolBuilder(registry *Registry) *DynamicToolBuilder {
	return &DynamicToolBuilder{
		registry:       registry,
		maxDirectTools: 20,
		maxDynamic:     15,
		bm25Index:      bm25.New(),
		skillBM25:      bm25.New(),
	}
}

// SetSkillProvider sets the SkillProvider used for skill-aware routing.
func (b *DynamicToolBuilder) SetSkillProvider(provider SkillProvider) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.skillProvider = provider
	b.refreshSkillIndex()
}

// refreshSkillIndex rebuilds the skill BM25 index from the current SkillProvider.
func (b *DynamicToolBuilder) refreshSkillIndex() {
	if b.skillProvider == nil {
		return
	}
	skills := b.skillProvider.ListActiveSkills()
	docs := make([]bm25.Doc, len(skills))
	for i, s := range skills {
		text := s.Name + " " + s.Description + " " + strings.Join(s.Triggers, " ")
		docs[i] = bm25.Doc{ID: s.Name, Text: text}
	}
	b.skillBM25.Rebuild(docs)
}

// RefreshSkillIndex forces a rebuild of the skill BM25 index.
func (b *DynamicToolBuilder) RefreshSkillIndex() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refreshSkillIndex()
}

// builderSkillMatchScore computes the best skill match score for the given user message.
func (b *DynamicToolBuilder) builderSkillMatchScore(userMessage string) (float64, []string) {
	if b.skillProvider == nil {
		return 0, nil
	}
	// Index is built on SetSkillProvider / RefreshSkillIndex — no rebuild here.
	scores := b.skillBM25.Score(userMessage)
	if len(scores) == 0 {
		return 0, nil
	}
	type entry struct {
		name  string
		score float64
	}
	var sorted []entry
	for name, sc := range scores {
		if sc > 0 {
			sorted = append(sorted, entry{name, sc})
		}
	}
	if len(sorted) == 0 {
		return 0, nil
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].score > sorted[j].score })
	bestRaw := sorted[0].score
	normBest := clampFloat(bestRaw/3.0, 0, 1)
	n := 3
	if len(sorted) < n {
		n = len(sorted)
	}
	names := make([]string, n)
	for i := 0; i < n; i++ {
		names[i] = sorted[i].name
	}
	return normBest, names
}

// SetRegistry replaces the registry without discarding the cached BM25 index.
func (b *DynamicToolBuilder) SetRegistry(registry *Registry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.registry = registry
}

// SetEmbedder configures the embedder for hybrid retrieval.
// If emb is a NoopEmbedder, hybrid is disabled (set to nil).
func (b *DynamicToolBuilder) SetEmbedder(emb embedding.Embedder) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if embedding.IsNoop(emb) {
		b.hybrid = nil
		return
	}
	b.hybrid = NewHybridRetriever(emb)
}

// SetEnrichmentStore configures the enrichment store for enhanced tool descriptions.
func (b *DynamicToolBuilder) SetEnrichmentStore(store *EnrichmentStore) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.enrichStore = store
}

// SetUsageTracker configures the usage tracker for experience-aware scoring.
func (b *DynamicToolBuilder) SetUsageTracker(tracker *UsageTracker) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tracker = tracker
}

// SetReranker configures the LLM listwise reranker. Pass nil to disable.
func (b *DynamicToolBuilder) SetReranker(rr Reranker) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reranker = rr
}

// buildEmbeddingText returns the text used for embedding vector computation.
// It is name + description only: BodySummary is long, templated parameter
// documentation whose shared boilerplate collapses the embedding space
// (measured median pairwise cosine ~0.8 across tool vectors), turning cosine
// ranking into noise. BodySummary is still fed to the LLM reranker via
// CandidateSummary, where a judge can use it.
func (b *DynamicToolBuilder) buildEmbeddingText(name, description string) string {
	return name + " " + description
}

// BuildAll returns tool definitions for every available tool (no filtering).
func (b *DynamicToolBuilder) BuildAll() []map[string]interface{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	tools := b.registry.ListAvailable()
	out := make([]map[string]interface{}, 0, len(tools))
	for _, t := range tools {
		if IsDisabledExternalCodingSessionTool(t.Name) || isInternalBrowserDispatchToolName(t.Name) || IsLegacyModelDynamicGateway(t.Name) {
			continue
		}
		// Skip backward-compat aliases that have handler only, no definition.
		// These tools have empty descriptions and are not meant to be exposed
		// to the LLM as tool definitions (e.g. legacy "run_skill" replaced by
		// "manage_skill"). Including them causes the LLM to call them with
		// empty parameters since it has no schema to follow.
		if t.Description == "" {
			continue
		}
		out = append(out, RegisteredToolToDef(t))
	}
	return out
}

// Build returns tool definitions, applying context-aware filtering when
// the number of available tools exceeds maxDirectTools.
// userMessage is used for relevance scoring when filtering is active.
func (b *DynamicToolBuilder) Build(userMessage string) []map[string]interface{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	tools := b.registry.ListAvailable()
	if len(tools) <= b.maxDirectTools {
		out := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			if IsDisabledExternalCodingSessionTool(t.Name) || isInternalBrowserDispatchToolName(t.Name) || IsLegacyModelDynamicGateway(t.Name) {
				continue
			}
			if t.Description == "" {
				continue // skip backward-compat aliases
			}
			out = append(out, RegisteredToolToDef(t))
		}
		return out
	}

	// Detect group activation keywords in user message.
	groupTags := DetectGroupTags(userMessage)

	// Split into builtin (always included), group-activated, and dynamic (scored).
	var builtins, groupActivated, dynamic []RegisteredTool
	for _, t := range tools {
		if IsDisabledExternalCodingSessionTool(t.Name) || isInternalBrowserDispatchToolName(t.Name) || IsLegacyModelDynamicGateway(t.Name) {
			continue
		}
		// Skip backward-compat aliases (handler only, no definition).
		if t.Description == "" {
			continue
		}
		if t.Category == CategoryBuiltin {
			builtins = append(builtins, t)
			continue
		}
		if len(groupTags) > 0 {
			matched := false
			for _, tag := range t.Tags {
				if groupTags[strings.ToLower(tag)] {
					matched = true
					break
				}
			}
			if matched {
				groupActivated = append(groupActivated, t)
				continue
			}
		}
		dynamic = append(dynamic, t)
	}

	// Score remaining dynamic tools using BM25 (reuses cached index).
	docs := make([]bm25.Doc, len(dynamic))
	dynamicTexts := make(map[string]string, len(dynamic))
	embeddingTexts := make(map[string]string, len(dynamic))
	for i, t := range dynamic {
		var text string
		if b.enrichStore != nil {
			text = b.enrichStore.GetSearchText(t)
		} else {
			text = t.Name + " " + t.Description
			for _, tag := range t.Tags {
				text += " " + tag
			}
		}
		docs[i] = bm25.Doc{ID: t.Name, Text: text}
		dynamicTexts[t.Name] = text
		embeddingTexts[t.Name] = b.buildEmbeddingText(t.Name, t.Description)
	}
	b.bm25Index.RebuildIfChanged(docs)
	bm25Scores := b.bm25Index.Score(userMessage)

	// Fuse with vector scores when hybrid retrieval is active.
	if b.hybrid != nil {
		bm25Scores = b.hybrid.FuseScores(userMessage, bm25Scores, embeddingTexts)
	}

	// Multi-signal scoring: retrieval + contextual experience + outcome + priority + skill_match.
	queryTokens := bm25.Tokenize(userMessage)
	normScores := normalizeRetrievalScores(bm25Scores)

	type scored struct {
		tool  RegisteredTool
		score float64
	}
	scoredList := make([]scored, 0, len(dynamic))
	for _, t := range dynamic {
		retrievalScore := normScores[t.Name]
		var expScore float64
		if b.tracker != nil {
			expScore = b.tracker.ExperienceScore(t.Name, queryTokens)
		}
		var outcomeScore float64
		if b.tracker != nil {
			outcomeScore = b.tracker.ContextOutcomeScore(t.Name, queryTokens)
		}
		var routingHintAdjustment float64
		if b.tracker != nil {
			routingHintAdjustment = b.tracker.RoutingHintAdjustment(t.Name, queryTokens)
		}
		priorityBonus := clampFloat(float64(t.Priority)*0.1, 0, 1)

		var s float64
		if b.skillProvider != nil && b.tracker != nil {
			s = 0.50*retrievalScore + 0.25*expScore + 0.15*outcomeScore + 0.10*priorityBonus
		} else if b.tracker != nil {
			s = 0.50*retrievalScore + 0.25*expScore + 0.15*outcomeScore + 0.10*priorityBonus
		} else {
			s = 0.9*retrievalScore + 0.1*priorityBonus
		}
		s = clampFloat(s+routingHintAdjustment, 0, 1)
		scoredList = append(scoredList, scored{tool: t, score: s})
	}
	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].score > scoredList[j].score
	})

	// Rerank top candidates when reranker is configured and candidates exceed threshold.
	if b.reranker != nil && len(scoredList) > b.maxDirectTools {
		rerankerCount := 20
		if rerankerCount > len(scoredList) {
			rerankerCount = len(scoredList)
		}
		summaries := make([]CandidateSummary, rerankerCount)
		for i := 0; i < rerankerCount; i++ {
			summaries[i] = CandidateSummary{
				Name:        scoredList[i].tool.Name,
				Description: scoredList[i].tool.Description,
				BodySummary: scoredList[i].tool.BodySummary,
			}
		}

		reranked, err := b.reranker.Rerank(userMessage, summaries, 5)
		if err != nil || len(reranked) == 0 {
			if err != nil {
				log.Printf("[Builder] WARN: reranker failed: %v, falling back to fused scores", err)
			}
			// Fall back to fused score ordering — no change to scoredList
		} else {
			// Promote reranked results to front of scored list.
			rerankedSet := make(map[string]bool, len(reranked))
			for _, name := range reranked {
				rerankedSet[name] = true
			}

			newScored := make([]scored, 0, len(scoredList))
			// Add reranked items first, in reranker order.
			for _, name := range reranked {
				for _, s := range scoredList {
					if s.tool.Name == name {
						newScored = append(newScored, s)
						break
					}
				}
			}
			// Supplement with remaining items from fused score list.
			for _, s := range scoredList {
				if !rerankedSet[s.tool.Name] {
					newScored = append(newScored, s)
				}
			}
			scoredList = newScored
		}
	}

	limit := b.maxDynamic - len(groupActivated)
	if limit < 0 {
		limit = 0
	}
	if limit > len(scoredList) {
		limit = len(scoredList)
	}

	out := make([]map[string]interface{}, 0, len(builtins)+len(groupActivated)+limit)
	for _, t := range builtins {
		out = append(out, RegisteredToolToDef(t))
	}
	for _, t := range groupActivated {
		out = append(out, RegisteredToolToDef(t))
	}
	for i := 0; i < limit; i++ {
		out = append(out, RegisteredToolToDef(scoredList[i].tool))
	}
	return out
}

func (b *DynamicToolBuilder) matchedSkillCapabilities(matchedSkills []string) []string {
	return matchedSkillCapabilitiesFromProvider(b.skillProvider, matchedSkills)
}

func prioritizeMatchedSkillTool(defs []map[string]interface{}, matchedSkill bool) []map[string]interface{} {
	if !matchedSkill || len(defs) < 2 {
		return defs
	}
	idx := -1
	for i, def := range defs {
		if ExtractToolName(def) == "manage_skill" {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return defs
	}
	out := make([]map[string]interface{}, 0, len(defs))
	out = append(out, defs[idx])
	out = append(out, defs[:idx]...)
	out = append(out, defs[idx+1:]...)
	return out
}

// RegisteredToolToDef converts a RegisteredTool to an OpenAI function calling definition.
func RegisteredToolToDef(t RegisteredTool) map[string]interface{} {
	params, err := CanonicalRegisteredToolInvocationSchema(t.InputSchema, t.Required)
	if err != nil {
		// Registry definitions are trusted host configuration. Preserve the
		// historic rendering fallback for a malformed legacy entry; governed
		// routing calls the same canonicalizer and fails closed before issuing a
		// grant, so this fallback cannot authorize semantic execution.
		params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  params,
		},
	}
}

// CanonicalRegisteredToolInvocationSchema is the one projection from a
// RegisteredTool's historical flat InputSchema to a model-visible and
// executable parameter schema. Both the normal tool renderer and the semantic
// invocation authorizer must use it: otherwise equivalent Go map types such as
// map[string]string and map[string]interface{} produce different contracts.
//
// The registry is allowed to carry either the legacy flat field map or a full
// root object schema. The result is always a closed root object, with Required
// supplied by the registration metadata when present.
func CanonicalRegisteredToolInvocationSchema(input map[string]interface{}, required []string) (map[string]interface{}, error) {
	canonical, err := canonicalJSONSchemaMap(input)
	if err != nil {
		return nil, err
	}
	if canonical == nil {
		canonical = map[string]interface{}{}
	}

	root := make(map[string]interface{}, len(canonical)+3)
	properties := map[string]interface{}{}
	if rawProperties, hasProperties := canonical["properties"]; hasProperties {
		var ok bool
		properties, ok = rawProperties.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("registered tool schema properties must be an object")
		}
		if rawType, exists := canonical["type"]; exists && fmt.Sprint(rawType) != "object" {
			return nil, fmt.Errorf("registered tool schema root must be an object")
		}
		for key, value := range canonical {
			if key != "type" && key != "properties" && key != "required" && key != "additionalProperties" {
				root[key] = value
			}
		}
	} else {
		for key, value := range canonical {
			switch key {
			case "type", "required", "additionalProperties":
				continue
			}
			property, ok := value.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("registered tool schema property %q must be an object", key)
			}
			properties[key] = property
		}
	}

	for key, value := range properties {
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("registered tool schema property name is empty")
		}
		if _, ok := value.(map[string]interface{}); !ok {
			return nil, fmt.Errorf("registered tool schema property %q must be an object", key)
		}
	}
	root["type"] = "object"
	root["properties"] = properties
	root["additionalProperties"] = false
	if len(required) > 0 {
		root["required"] = append([]string(nil), required...)
	} else if rawRequired, ok := canonical["required"]; ok {
		root["required"] = rawRequired
	}
	return root, nil
}

// canonicalJSONSchemaMap normalizes Go container types through JSON. Tool
// registrations commonly use map[string]string for concise property specs,
// while JSON decoding produces map[string]interface{}. JSON Schema is a JSON
// document, so this conversion is lossless for valid schema data and removes
// that implementation-detail distinction before either rendering or checking.
func canonicalJSONSchemaMap(input map[string]interface{}) (map[string]interface{}, error) {
	if len(input) == 0 {
		return map[string]interface{}{}, nil
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("encode registered tool schema: %w", err)
	}
	var normalized map[string]interface{}
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, fmt.Errorf("decode registered tool schema: %w", err)
	}
	return normalized, nil
}

// GroupKeywords maps user-facing group names (Chinese and English) to tag sets.
var GroupKeywords = map[string][]string{
	"数据库":         {"database", "sql", "query", "db"},
	"database":    {"database", "sql", "query", "db"},
	"git":         {"git", "vcs", "version"},
	"版本控制":        {"git", "vcs", "version"},
	"文件":          {"file", "read", "write", "directory"},
	"file":        {"file", "read", "write", "directory"},
	"mcp":         {"mcp"},
	"skill":       {"skill"},
	"技能":          {"skill"},
	"直通任务":        {"passthrough", "run", "emergency", "recovery", "script"},
	"直通命令":        {"passthrough", "run", "emergency", "recovery", "script"},
	"应急命令":        {"passthrough", "run", "emergency", "recovery", "script"},
	"应急脚本":        {"passthrough", "run", "emergency", "recovery", "script"},
	"恢复脚本":        {"passthrough", "run", "emergency", "recovery", "script"},
	"passthrough": {"passthrough", "run", "emergency", "recovery", "script"},
	"emergency":   {"passthrough", "run", "emergency", "recovery", "script"},
	"recovery":    {"passthrough", "run", "emergency", "recovery", "script"},
	"会话":          {"session"},
	"session":     {"session"},
	"配置":          {"config", "settings"},
	"config":      {"config", "settings"},
	"记忆":          {"memory"},
	"memory":      {"memory"},
	"定时":          {"schedule", "task", "cron", "timer"},
	"schedule":    {"schedule", "task", "cron", "timer"},
	"网络":          {"network", "p2p", "web", "search", "fetch"},
	"network":     {"network", "p2p", "web", "search", "fetch"},
	"搜索":          {"web", "search", "internet", "fetch"},
	"search":      {"web", "search", "internet", "fetch"},
	"网页":          {"web", "fetch", "browse", "url"},
	"web":         {"web", "fetch", "browse", "url", "search"},
	"浏览器":         {"browser", "web", "automation", "test"},
	"browser":     {"browser", "web", "automation", "test"},
	"自动化":         {"browser", "automation", "test"},
	"automation":  {"browser", "automation", "test"},
	"测试":          {"browser", "automation", "test", "web"},
	"test":        {"browser", "automation", "test", "web"},
	"登录":          {"browser", "web", "automation"},
	"下单":          {"browser", "web", "automation"},
	"gui":         {"gui", "test", "automation", "desktop"},
	"桌面":          {"gui", "test", "automation", "desktop"},
	"录制":          {"gui", "test", "automation", "desktop", "录制"},
	"desktop":     {"gui", "test", "automation", "desktop"},
}

// DetectGroupTags checks if the user message contains any group activation
// keywords and returns the union of matching tag sets.
func DetectGroupTags(userMessage string) map[string]bool {
	msg := strings.ToLower(userMessage)
	tags := make(map[string]bool)
	for keyword, tagList := range GroupKeywords {
		if strings.Contains(msg, keyword) {
			for _, t := range tagList {
				tags[t] = true
			}
		}
	}
	return tags
}

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/configfile"
	corellm "github.com/RapidAI/CodeClaw/corelib/llm"
)

const (
	answerCacheFileName       = "answers.json"
	answerCacheMaxEntries     = 2000
	answerCacheMaxAnswerRunes = 24000
	// Kept below ordinary interactive prompts. The cache must not become an
	// unbounded on-disk transcript merely because a client sends a huge FAQ.
	answerCacheMaxQuestionRunes = 1000
)

type answerCacheEntry struct {
	Answer string `json:"answer"`
	// Question and ScopeFingerprint are intentionally stored only for the
	// conservative local near-match index. They contain neither a user ID nor
	// credentials; ScopeFingerprint is an opaque hash of the bot, conversation
	// scope, binding and runtime policy.
	Question         string    `json:"question,omitempty"`
	ScopeFingerprint string    `json:"scope_fingerprint,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type answerCacheFile struct {
	Entries map[string]answerCacheEntry `json:"entries"`
}

// answerCacheStore is deliberately a process-local, App-owned store. Every
// Lansenger profile handler reaches the same store. The key is scoped to a bot
// and its answer-generation policy, so identical safe questions can be reused
// by different users of that bot.
type answerCacheStore struct {
	mu      sync.Mutex
	path    string
	loaded  bool
	entries map[string]answerCacheEntry
	flights map[string]chan struct{}
}

func newAnswerCacheStore(path string) *answerCacheStore {
	return &answerCacheStore{path: path, entries: make(map[string]answerCacheEntry), flights: make(map[string]chan struct{})}
}

func (s *answerCacheStore) loadLocked() {
	if s == nil || s.loaded {
		return
	}
	s.loaded = true
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[answer-cache] load failed path=%q err=%v", s.path, err)
		}
		return
	}
	var state answerCacheFile
	if err := json.Unmarshal(data, &state); err != nil {
		log.Printf("[answer-cache] ignored invalid cache file path=%q err=%v", s.path, err)
		return
	}
	if state.Entries != nil {
		s.entries = state.Entries
	}
}

func (s *answerCacheStore) get(key string, now time.Time) (string, bool) {
	return s.getWithMaxAge(key, now, 0)
}

// getWithMaxAge enforces the currently configured cache lifetime as well as
// the lifetime recorded when the answer was written. This makes a TTL reduction
// take effect immediately for existing entries instead of leaving them reusable
// until the old, longer expiry time.
func (s *answerCacheStore) getWithMaxAge(key string, now time.Time, maxAge time.Duration) (string, bool) {
	if s == nil || key == "" {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	entry, ok := s.entries[key]
	if !ok {
		return "", false
	}
	policyExpired := maxAge > 0 && !now.Before(entry.CreatedAt.Add(maxAge))
	if !now.Before(entry.ExpiresAt) || policyExpired || strings.TrimSpace(entry.Answer) == "" {
		delete(s.entries, key)
		// Do not put a disk write on the reply path just to remove one stale
		// entry. The next successful put prunes all expired entries and persists
		// the compacted file; correctness comes from ExpiresAt, not deletion.
		return "", false
	}
	return entry.Answer, true
}

func (s *answerCacheStore) put(key, answer string, ttl time.Duration, now time.Time) {
	s.putWithMetadata(key, "", "", answer, ttl, now)
}

func (s *answerCacheStore) putWithMetadata(key, scopeFingerprint, question, answer string, ttl time.Duration, now time.Time) {
	if s == nil || key == "" || ttl <= 0 || strings.TrimSpace(answer) == "" {
		return
	}
	question = normalizeAnswerCacheQuestion(question)
	if len([]rune(answer)) > answerCacheMaxAnswerRunes {
		return
	}
	// Exact lookup is a hash and can safely support a longer independent FAQ.
	// Omit only the persisted near-match metadata for it, so an oversized input
	// cannot turn the cache file into an unbounded transcript.
	if len([]rune(question)) > answerCacheMaxQuestionRunes {
		question = ""
		scopeFingerprint = ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	for k, entry := range s.entries {
		if !now.Before(entry.ExpiresAt) || strings.TrimSpace(entry.Answer) == "" {
			delete(s.entries, k)
		}
	}
	if len(s.entries) >= answerCacheMaxEntries {
		var oldestKey string
		var oldest time.Time
		for k, entry := range s.entries {
			if oldestKey == "" || entry.CreatedAt.Before(oldest) {
				oldestKey, oldest = k, entry.CreatedAt
			}
		}
		if oldestKey != "" {
			delete(s.entries, oldestKey)
		}
	}
	s.entries[key] = answerCacheEntry{
		Answer:           answer,
		Question:         question,
		ScopeFingerprint: strings.TrimSpace(scopeFingerprint),
		CreatedAt:        now,
		ExpiresAt:        now.Add(ttl),
	}
	if err := s.persistLocked(); err != nil {
		log.Printf("[answer-cache] persist failed: %v", err)
	}
}

// getSemanticWithMaxAge performs a deliberately strict, local near-match
// lookup within one opaque answer-generation scope. It never calls an LLM or
// embedding service on the reply path. Exact keys are always checked first by
// the caller; this fallback accepts only a unique high-confidence candidate.
func (s *answerCacheStore) getSemanticWithMaxAge(scopeFingerprint, question string, now time.Time, maxAge time.Duration) (string, bool) {
	if s == nil || strings.TrimSpace(scopeFingerprint) == "" || strings.TrimSpace(question) == "" {
		return "", false
	}
	// Oversized questions may still use the hashed exact-key lookup performed
	// before this fallback, but they are deliberately omitted from persisted
	// semantic metadata on write. Do not turn a single untrusted long input into
	// an O(entries × question length) scan on the reply path.
	if len([]rune(question)) > answerCacheMaxQuestionRunes {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()

	bestScore := 0.0
	bestAnswer := ""
	ambiguous := false
	for key, entry := range s.entries {
		policyExpired := maxAge > 0 && !now.Before(entry.CreatedAt.Add(maxAge))
		if !now.Before(entry.ExpiresAt) || policyExpired || strings.TrimSpace(entry.Answer) == "" {
			delete(s.entries, key)
			continue
		}
		if entry.ScopeFingerprint != scopeFingerprint || strings.TrimSpace(entry.Question) == "" {
			continue
		}
		score, ok := answerCacheSemanticMatchScore(question, entry.Question)
		if !ok {
			continue
		}
		if score > bestScore+0.0001 {
			// A later candidate can only replace the leader when it is clearly
			// better. Similar scores must remain ambiguous regardless of map
			// iteration order.
			if bestScore > 0 && score-bestScore <= 0.10 {
				ambiguous = true
			} else {
				bestScore, bestAnswer = score, entry.Answer
			}
		} else if score >= bestScore-0.10 {
			// Two nearly-equivalent but independently generated answers are not a
			// safe choice. Fall back to a fresh response rather than guessing.
			ambiguous = true
		}
	}
	if ambiguous || bestScore < answerCacheSemanticMatchThreshold {
		return "", false
	}
	return bestAnswer, true
}

const answerCacheSemanticMatchThreshold = 0.90

// beginFlight coalesces concurrent cache misses for the same safe answer. The
// leader generates the answer; waiters re-check the persisted cache once that
// leader finishes. They never receive the in-flight response directly, so an
// unsafe/non-cacheable response cannot cross a user boundary.
func (s *answerCacheStore) beginFlight(key string) (leader bool, done <-chan struct{}, finish func()) {
	if s == nil || key == "" {
		return false, nil, func() {}
	}
	s.mu.Lock()
	if existing := s.flights[key]; existing != nil {
		s.mu.Unlock()
		return false, existing, func() {}
	}
	flight := make(chan struct{})
	s.flights[key] = flight
	s.mu.Unlock()
	var once sync.Once
	return true, nil, func() {
		once.Do(func() {
			s.mu.Lock()
			if s.flights[key] == flight {
				delete(s.flights, key)
				close(flight)
			}
			s.mu.Unlock()
		})
	}
}

// waitAnswerCacheFlight waits for the current answer producer without taking
// ownership of its flight. A canceled waiter must not start a duplicate model
// call or release the producer on behalf of other users.
func waitAnswerCacheFlight(cancelCtx <-chan struct{}, done <-chan struct{}) bool {
	if done == nil {
		return false
	}
	if cancelCtx == nil {
		<-done
		return true
	}
	select {
	case <-done:
		return true
	case <-cancelCtx:
		return false
	}
}

func (s *answerCacheStore) persistLocked() error {
	if s == nil {
		return nil
	}
	return configfile.AtomicWriteJSON(s.path, answerCacheFile{Entries: s.entries})
}

func (a *App) ensureAnswerCacheStore() *answerCacheStore {
	if a == nil {
		return nil
	}
	path := filepath.Join(a.GetDataDir(), "answer_cache", answerCacheFileName)
	a.answerCacheMu.Lock()
	defer a.answerCacheMu.Unlock()
	if a.answerCache == nil || a.answerCache.path != path {
		a.answerCache = newAnswerCacheStore(path)
	}
	return a.answerCache
}

func normalizeAnswerCacheQuestion(text string) string {
	text = strings.TrimSpace(strings.ToLower(text))
	return strings.Join(strings.FieldsFunc(text, unicode.IsSpace), " ")
}

func normalizeAnswerCacheLanguage(lang string) string {
	return strings.ToLower(strings.TrimSpace(lang))
}

func answerCacheQuestionText(msg IMUserMessage) string {
	if strings.TrimSpace(msg.CacheQuestion) != "" {
		return msg.CacheQuestion
	}
	return msg.Text
}

// canonicalAnswerCacheStrings makes semantically unordered configuration lists
// stable before they participate in an answer-cache key. Configuration forms
// can reorder rows while saving; that should not discard a still-valid answer.
func canonicalAnswerCacheStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

type answerCacheExpertPolicy struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	SystemPrompt string   `json:"system_prompt"`
	Tools        []string `json:"tools,omitempty"`
	Skills       []string `json:"skills,omitempty"`
}

type answerCacheBindingScope struct {
	ProfileID           string                  `json:"profile_id"`
	Mode                string                  `json:"mode"`
	ExpertID            string                  `json:"expert_id"`
	InitialPrompt       string                  `json:"initial_prompt"`
	WorkingDirectory    string                  `json:"working_directory"`
	DocumentDirectories []string                `json:"document_directories,omitempty"`
	AllowedDirectories  []string                `json:"allowed_directories,omitempty"`
	AllowAllDirectories bool                    `json:"allow_all_directories"`
	ExpertPolicy        answerCacheExpertPolicy `json:"expert_policy"`
}

// answerCacheRuntimePolicy contains non-secret settings that affect a model's
// behavior but are not part of the inbound robot binding. Keeping it separate
// from the binding scope makes the key contract explicit and avoids ever
// hashing credentials into the persistent cache index.
type answerCacheRuntimePolicy struct {
	RoleName        string                        `json:"role_name,omitempty"`
	RoleDescription string                        `json:"role_description,omitempty"`
	ProviderID      string                        `json:"provider_id,omitempty"`
	Model           string                        `json:"model,omitempty"`
	Protocol        string                        `json:"protocol,omitempty"`
	ReasoningEffort string                        `json:"reasoning_effort,omitempty"`
	ThinkingMode    string                        `json:"thinking_mode,omitempty"`
	AuxiliaryLLM    answerCacheAuxiliaryLLMPolicy `json:"auxiliary_llm"`
	ModelRoutes     []answerCacheModelRoutePolicy `json:"model_routes,omitempty"`
	CostRouteMode   string                        `json:"cost_route_mode,omitempty"`
}

type answerCacheAuxiliaryLLMPolicy struct {
	URL      string `json:"url,omitempty"`
	Model    string `json:"model,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// answerCacheModelRoutePolicy deliberately excludes the route API key. The
// remaining fields determine which model endpoint and protocol can answer a
// cache-eligible turn.
type answerCacheModelRoutePolicy struct {
	Task     string `json:"task"`
	Model    string `json:"model,omitempty"`
	URL      string `json:"url,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Provider string `json:"provider,omitempty"`
}

func answerCacheKey(msg IMUserMessage, profileID string, expertDef *ExpertDefinition) string {
	question := normalizeAnswerCacheQuestion(answerCacheQuestionText(msg))
	scopeRaw := answerCacheScopeRaw(msg, profileID, expertDef)
	if question == "" || scopeRaw == "" {
		return ""
	}
	// Keep the historical exact-key serialization unchanged. Entries written
	// before the near-match index was added can therefore remain exact hits.
	sum := sha256.Sum256([]byte(scopeRaw + "\x00" + question))
	return hex.EncodeToString(sum[:])
}

// answerCacheScopeFingerprint hashes every answer-affecting constraint except
// the question itself. It is persisted as an opaque value so near matches can
// never cross a bot, private/group conversation, language, expert binding or
// response-policy boundary.
func answerCacheScopeFingerprint(msg IMUserMessage, profileID string, expertDef *ExpertDefinition) string {
	raw := answerCacheScopeRaw(msg, profileID, expertDef)
	if raw == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func answerCacheScopeRaw(msg IMUserMessage, profileID string, expertDef *ExpertDefinition) string {
	if strings.TrimSpace(profileID) == "" {
		return ""
	}
	binding := "general"
	if msg.AssistantBinding != nil {
		// Keep only fields that can influence the generated answer. The expert
		// name is used by both light and full system prompts, while card-only
		// metadata such as icon, description and timestamps is intentionally
		// excluded.
		expertPolicy := answerCacheExpertPolicy{}
		if expertDef != nil {
			expertPolicy = answerCacheExpertPolicy{
				ID:           strings.TrimSpace(expertDef.ID),
				Name:         strings.TrimSpace(expertDef.Name),
				SystemPrompt: expertDef.SystemPrompt,
				Tools:        canonicalAnswerCacheStrings(expertDef.Tools),
				Skills:       canonicalAnswerCacheStrings(expertDef.Skills),
			}
		}
		encoded, _ := json.Marshal(answerCacheBindingScope{
			ProfileID:           strings.TrimSpace(msg.AssistantBinding.BotProfileID),
			Mode:                strings.ToLower(strings.TrimSpace(msg.AssistantBinding.Mode)),
			ExpertID:            strings.TrimSpace(msg.AssistantBinding.ExpertID),
			InitialPrompt:       msg.AssistantBinding.InitialPrompt,
			WorkingDirectory:    msg.AssistantBinding.WorkingDirectory,
			DocumentDirectories: canonicalAnswerCacheStrings(msg.AssistantBinding.DocumentDirectories),
			AllowedDirectories:  canonicalAnswerCacheStrings(msg.AssistantBinding.AllowedDirectories),
			AllowAllDirectories: msg.AssistantBinding.AllowAllDirectories,
			ExpertPolicy:        expertPolicy,
		})
		sum := sha256.Sum256(encoded)
		binding = hex.EncodeToString(sum[:])
	}
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(msg.Platform)),
		strings.TrimSpace(profileID),
		strings.TrimSpace(msg.CacheScope),
		strings.TrimSpace(msg.CachePolicyScope),
		// A bot can receive the same question with different requested response
		// languages. Never replay an answer in the wrong language.
		normalizeAnswerCacheLanguage(msg.Lang),
		binding,
	}, "\x00")
}

// answerCacheKeyWithRuntimePolicy extends the transport/binding key with
// behavior-affecting application settings. It deliberately builds on
// answerCacheKey so callers that only need identity-scope comparisons retain a
// small, deterministic helper.
func answerCacheKeyWithRuntimePolicy(msg IMUserMessage, profileID string, expertDef *ExpertDefinition, policy answerCacheRuntimePolicy) string {
	baseKey := answerCacheKey(msg, profileID, expertDef)
	if baseKey == "" {
		return ""
	}
	encoded, _ := json.Marshal(policy)
	sum := sha256.Sum256(append(append([]byte(baseKey), 0), encoded...))
	return hex.EncodeToString(sum[:])
}

func answerCacheScopeFingerprintWithRuntimePolicy(msg IMUserMessage, profileID string, expertDef *ExpertDefinition, policy answerCacheRuntimePolicy) string {
	baseScope := answerCacheScopeFingerprint(msg, profileID, expertDef)
	if baseScope == "" {
		return ""
	}
	encoded, _ := json.Marshal(policy)
	sum := sha256.Sum256(append(append([]byte(baseScope), 0), encoded...))
	return hex.EncodeToString(sum[:])
}

// answerCacheSemanticMatchScore supplies a low-cost, intentionally limited
// semantic approximation. It recognizes reordering and filler-word variants,
// but does not try to infer a broad relationship between different questions.
// Returning false for small/ambiguous feature sets is a safety property: a
// miss costs one generation while a false hit can return a wrong answer.
func answerCacheSemanticMatchScore(question, cachedQuestion string) (float64, bool) {
	left := answerCacheQuestionFeatures(question)
	right := answerCacheQuestionFeatures(cachedQuestion)
	if len(left) < 2 || len(right) < 2 {
		return 0, false
	}
	intersection := 0
	for feature := range left {
		if _, exists := right[feature]; exists {
			intersection++
		}
	}
	union := len(left) + len(right) - intersection
	if intersection == 0 || union == 0 {
		return 0, false
	}
	return float64(intersection) / float64(union), true
}

func answerCacheQuestionFeatures(question string) map[string]struct{} {
	features := make(map[string]struct{})
	question = answerCacheSemanticQuestionNormalization(question)
	for _, word := range strings.FieldsFunc(question, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		word = answerCacheCanonicalWord(word)
		if !answerCacheFillerWord(word) && len([]rune(word)) >= 2 {
			features["w:"+word] = struct{}{}
		}
	}
	// CJK text does not reliably contain spaces. Bigrams preserve key terms
	// without depending on an external tokenizer or model on the reply path.
	var runes []rune
	for _, r := range []rune(question) {
		if unicode.Is(unicode.Han, r) && !answerCacheCJKFillerRune(r) {
			runes = append(runes, r)
		}
	}
	for i := 0; i+1 < len(runes); i++ {
		features["c:"+string(runes[i:i+2])] = struct{}{}
	}
	// Keep individual Han characters as a conservative fallback for surface
	// phrasing changes (for example “是什么” versus “如何规定”). Bigrams still
	// carry most of the discriminating weight; the high threshold protects
	// against loosely related questions.
	for _, r := range runes {
		features["h:"+string(r)] = struct{}{}
	}
	return features
}

func answerCacheSemanticQuestionNormalization(question string) string {
	question = strings.ToLower(question)
	// These Chinese phrases only frame a question; removing them lets a narrow
	// lexical match recognize “退款政策是什么” and “退款政策如何规定” as the
	// same FAQ without equating distinct subjects such as 退款政策 and 退款流程.
	question = strings.NewReplacer(
		"是什么", "", "如何", "", "怎么", "", "怎样", "", "哪些", "", "规定", "",
	).Replace(question)
	return question
}

// answerCacheCanonicalWord handles only presentation-level English variants.
// It is intentionally not a synonym dictionary: broad semantic expansion is
// unsafe for a shared answer cache and belongs to a future opt-in embedding
// index with evaluation data.
func answerCacheCanonicalWord(word string) string {
	word = strings.TrimSuffix(word, "'s")
	if len(word) > 4 && strings.HasSuffix(word, "ies") {
		return strings.TrimSuffix(word, "ies") + "y"
	}
	if len(word) > 4 && strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") && !strings.HasSuffix(word, "us") {
		return strings.TrimSuffix(word, "s")
	}
	return word
}

func answerCacheFillerWord(word string) bool {
	switch word {
	case "a", "an", "the", "is", "are", "was", "were", "do", "does", "did", "what", "which", "who", "how", "please", "can", "could", "would", "you", "your", "tell", "about":
		return true
	default:
		return false
	}
}

func answerCacheCJKFillerRune(r rune) bool {
	switch r {
	case '\u7684', '\u4e86', '\u5417', '\u5462', '\u554a', '\u5440', '\u8bf7', '\u95ee', '\u662f', '\u6709', '\u5728', '\u548c', '\u4e0e', '\u53ca', '\u4e48':
		return true
	default:
		return false
	}
}

func hasAnswerCacheRefreshOrFollowUpIntent(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return true
	}
	refreshMarkers := []string{
		"更新", "最新", "重新查", "重新生成", "刷新", "查一下现在", "实时", "现状",
		"latest", "update", "refresh", "current", "recheck", "regenerate",
	}
	for _, marker := range refreshMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	followUpMarkers := []string{
		"继续", "为什么", "详细", "展开", "那呢", "然后", "第二个", "这个", "那个", "它", "他们",
		"what about", "why", "continue", "more detail", "elaborate", "that one", "the second",
	}
	for _, marker := range followUpMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	// An explicit dissatisfaction request must always generate a new answer.
	// This keeps a superficially similar question from replaying the answer the
	// user has just rejected.
	for _, marker := range []string{
		"不满意", "不准确", "不对", "不正确", "不行", "答非所问", "没解决", "没有解决", "没帮助", "没有帮助", "重新回答", "重新答", "重答", "再回答", "换一种说法", "换个说法", "更好", "更准确", "更详细",
		"not helpful", "not useful", "incorrect", "inaccurate", "doesn't answer", "does not answer", "answer again", "better answer", "rephrase",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// hasAnswerCachePersonalContextIntent rejects questions whose answer can
// reasonably depend on the asker's identity. This is intentionally
// conservative because cache entries may be reused by another user of the
// same bot conversation scope.
func hasAnswerCachePersonalContextIntent(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return true
	}
	for _, marker := range []string{"我", "我的", "本人", "账号", "帳號", "账户", "帳戶"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	for _, word := range strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) }) {
		switch word {
		case "i", "me", "my", "mine", "myself":
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) answerCacheConfig() (corelib.AnswerCacheConfig, bool) {
	if h == nil || h.app == nil || strings.TrimSpace(h.lansengerBotProfileID) == "" {
		return corelib.AnswerCacheConfig{}, false
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return corelib.AnswerCacheConfig{}, false
	}
	profile, ok := lansengerBotProfileFromConfig(cfg, h.lansengerBotProfileID)
	if !ok {
		return corelib.AnswerCacheConfig{}, false
	}
	cache := profile.EffectiveAnswerCache()
	return cache, cache.CanReuseAnswers()
}

func (h *IMMessageHandler) answerCacheLookup(msg IMUserMessage, entry imEntryContextResult, bindingScope *assistantBindingTurnScope) (*IMAgentResponse, string, func()) {
	if !answerCacheRequestEligible(msg, entry) {
		return nil, "", nil
	}
	store := h.app.ensureAnswerCacheStore()
	if store == nil {
		return nil, "", nil
	}
	for {
		// Config can change while a same-key request is in flight. Rebuild the
		// key before every cache read/leadership decision so a response generated
		// with a new model or policy is never written under an older key.
		cache, enabled := h.answerCacheConfig()
		if !enabled {
			return nil, "", nil
		}
		cacheKeyMessage, expertDef := answerCacheKeyTurnInput(msg, bindingScope)
		cacheKeyMessage.Lang = normalizeAnswerCacheLanguage(h.imCommandResponseLang(msg.Lang))
		runtimePolicy := h.answerCacheRuntimePolicy(expertDef)
		key := answerCacheKeyWithRuntimePolicy(cacheKeyMessage, h.lansengerBotProfileID, expertDef, runtimePolicy)
		if key == "" {
			return nil, "", nil
		}
		answer, ok := store.getWithMaxAge(key, time.Now(), time.Duration(cache.TTLDays)*24*time.Hour)
		responseSource := "answer_cache"
		if !ok {
			scope := answerCacheScopeFingerprintWithRuntimePolicy(cacheKeyMessage, h.lansengerBotProfileID, expertDef, runtimePolicy)
			answer, ok = store.getSemanticWithMaxAge(scope, answerCacheQuestionText(cacheKeyMessage), time.Now(), time.Duration(cache.TTLDays)*24*time.Hour)
			responseSource = "answer_cache_semantic"
		}
		if ok {
			resp := &IMAgentResponse{
				Text:           answer,
				RequestID:      imRequestID(msg),
				SessionKey:     msg.UserID,
				ResponseSource: responseSource,
			}
			if h.memory != nil {
				history := append([]agent.ConversationEntry(nil), entry.EntriesBeforeClear...)
				history = append(history,
					agent.ConversationEntry{Role: "user", Content: msg.Text},
					agent.ConversationEntry{Role: "assistant", Content: answer},
				)
				h.saveConversationHistoryTimed(msg.UserID, history, resp)
			}
			return resp, key, nil
		}
		leader, done, finish := store.beginFlight(key)
		if leader {
			return nil, key, finish
		}
		// A waiter must not remain blocked forever if its inbound transport has
		// already timed out or disconnected. It never owns the leader's flight,
		// so cancellation only abandons this request; the leader still releases
		// other waiters through its deferred finish callback.
		var cancelDone <-chan struct{}
		if msg.CancelCtx != nil {
			cancelDone = msg.CancelCtx.Done()
		}
		if waitAnswerCacheFlight(cancelDone, done) {
			continue
		}
		return &IMAgentResponse{
			Error:      "request canceled while waiting for a shared answer",
			RequestID:  imRequestID(msg),
			SessionKey: msg.UserID,
		}, "", nil
	}
}

func (h *IMMessageHandler) answerCacheRuntimePolicy(expertDef *ExpertDefinition) answerCacheRuntimePolicy {
	policy := answerCacheRuntimePolicy{}
	var cfg corelib.AppConfig
	if loadedCfg, err := h.loadConfig(); err == nil {
		cfg = loadedCfg
	}
	// Expert prompts replace the global role identity. For general assistants,
	// a role change must invalidate answers generated under the old persona.
	if expertDef == nil {
		policy.RoleName = strings.TrimSpace(cfg.MaclawRoleName)
		policy.RoleDescription = strings.TrimSpace(cfg.MaclawRoleDescription)
	}
	policy.AuxiliaryLLM = answerCacheAuxiliaryLLMPolicy{
		URL:      strings.TrimSpace(cfg.AuxiliaryLLM.URL),
		Model:    strings.TrimSpace(cfg.AuxiliaryLLM.Model),
		Protocol: strings.TrimSpace(cfg.AuxiliaryLLM.Protocol),
		Enabled:  cfg.AuxiliaryLLM.IsConfigured(),
	}
	if len(cfg.ModelRoutes) > 0 {
		policy.ModelRoutes = make([]answerCacheModelRoutePolicy, 0, len(cfg.ModelRoutes))
		for task, route := range cfg.ModelRoutes {
			policy.ModelRoutes = append(policy.ModelRoutes, answerCacheModelRoutePolicy{
				Task:     strings.ToLower(strings.TrimSpace(task)),
				Model:    strings.TrimSpace(route.Model),
				URL:      strings.TrimSpace(route.URL),
				Protocol: strings.TrimSpace(route.Protocol),
				Provider: strings.TrimSpace(route.Provider),
			})
		}
		sort.Slice(policy.ModelRoutes, func(i, j int) bool {
			return policy.ModelRoutes[i].Task < policy.ModelRoutes[j].Task
		})
	}
	// Cost routing is environment-controlled and can select a different model
	// or thinking policy without changing config.json.
	policy.CostRouteMode = string(corellm.ResolveCostRouteMode())
	llmConfig := h.getMaclawLLMConfig()
	policy.ProviderID = strings.TrimSpace(llmConfig.ProviderID)
	policy.Model = strings.TrimSpace(llmConfig.Model)
	policy.Protocol = strings.TrimSpace(llmConfig.Protocol)
	policy.ReasoningEffort = strings.TrimSpace(llmConfig.ReasoningEffort)
	policy.ThinkingMode = strings.TrimSpace(llmConfig.ThinkingMode)
	return policy
}

// answerCacheKeyTurnInput returns the exact policy snapshot used by a queued
// turn. Cache lookup must not observe later mutations to gateway-owned message
// data after the turn has already passed binding validation.
func answerCacheKeyTurnInput(msg IMUserMessage, bindingScope *assistantBindingTurnScope) (IMUserMessage, *ExpertDefinition) {
	// The agent falls back to the app language when the gateway did not send a
	// language. The caller puts that effective language in the returned message.
	cacheKeyMessage := msg
	var expertDef *ExpertDefinition
	if bindingScope != nil {
		// The turn policy was validated and copied before it entered the
		// per-session queue. Use that same immutable snapshot for the cache key:
		// callers may reuse or mutate the transport message while this turn waits.
		binding := bindingScope.binding
		cacheKeyMessage.AssistantBinding = &binding
		expertDef = bindingScope.expertDef
	}
	return cacheKeyMessage, expertDef
}

func answerCacheRequestEligible(msg IMUserMessage, entry imEntryContextResult) bool {
	if msg.IsBackground || msg.StartNewTask || msg.UIAction || len(msg.Attachments) > 0 ||
		strings.TrimSpace(msg.SlashCommand) != "" || strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		return false
	}
	// CacheQuestion is set by the Lansenger gateway only after it has removed
	// transport-only text and verified that no quoted/staff context affected
	// the turn. Refuse any Lansenger message without that proof rather than
	// accidentally keying a decorated prompt by its full text.
	if strings.EqualFold(strings.TrimSpace(msg.Platform), "lansenger_local") && strings.TrimSpace(msg.CacheQuestion) == "" {
		return false
	}
	// CacheScope is supplied by Lansenger only for a clean, independently
	// answerable transport question. Its absence on a Lansenger turn means the
	// prompt carries extra platform context (for example a quoted message).
	if strings.EqualFold(strings.TrimSpace(msg.Platform), "lansenger_local") && strings.TrimSpace(msg.CacheScope) == "" {
		return false
	}
	// Answer entries are shared by users of the same bot. Only cache a turn
	// that starts without prior conversation context, otherwise the generated
	// answer may depend on (or disclose) the current user's earlier exchange.
	if len(entry.EntriesBeforeClear) != 0 {
		return false
	}
	if entry.WorkflowActive || entry.WorkflowChoicePending || entry.TemplateSubAgentPending || entry.WorkflowAgentLoop || entry.WorkflowReviewPending || entry.WorkflowDocPhase || entry.ConfirmedResume || entry.SkipWorkflowRouting ||
		entry.HasPendingAskUser || entry.HasPendingUserReply || entry.FreshTask {
		return false
	}
	question := answerCacheQuestionText(msg)
	return !hasAnswerCacheRefreshOrFollowUpIntent(question) && !hasAnswerCachePersonalContextIntent(question)
}

func (h *IMMessageHandler) storeAnswerCacheResult(msg IMUserMessage, key string, bindingScope *assistantBindingTurnScope, resp *IMAgentResponse) {
	if key == "" || !answerCacheResultWriteEligible(msg, resp) {
		return
	}
	cache, enabled := h.answerCacheConfig()
	if !enabled {
		return
	}
	// Validate the complete policy fingerprint again immediately before writing.
	// The agent loop may outlive a settings change; persisting with its stale
	// lookup key would otherwise allow a new policy to read an old-policy reply.
	cacheKeyMessage, expertDef := answerCacheKeyTurnInput(msg, bindingScope)
	cacheKeyMessage.Lang = normalizeAnswerCacheLanguage(h.imCommandResponseLang(msg.Lang))
	runtimePolicy := h.answerCacheRuntimePolicy(expertDef)
	if currentKey := answerCacheKeyWithRuntimePolicy(cacheKeyMessage, h.lansengerBotProfileID, expertDef, runtimePolicy); currentKey != key {
		return
	}
	store := h.app.ensureAnswerCacheStore()
	if store == nil {
		return
	}
	store.putWithMetadata(
		key,
		answerCacheScopeFingerprintWithRuntimePolicy(cacheKeyMessage, h.lansengerBotProfileID, expertDef, runtimePolicy),
		answerCacheQuestionText(cacheKeyMessage),
		strings.TrimSpace(resp.Text),
		time.Duration(cache.TTLDays)*24*time.Hour,
		time.Now(),
	)
}

func answerCacheResultWriteEligible(msg IMUserMessage, resp *IMAgentResponse) bool {
	// A canceled producer may return a partial-looking text response. Never let
	// it become the answer another user receives.
	if msg.CancelCtx != nil && msg.CancelCtx.Err() != nil {
		return false
	}
	return answerCacheResponseEligible(resp)
}

func answerCacheResponseEligible(resp *IMAgentResponse) bool {
	if resp == nil || resp.ToolCallsInTurn != 0 || resp.Error != "" || resp.Deferred || resp.HardExit ||
		resp.ClearUI || resp.KeepPanel || resp.ConfirmedResume || resp.PendingVoiceParts != 0 ||
		strings.TrimSpace(resp.Text) == "" || strings.TrimSpace(resp.Reasoning) != "" || len(resp.Fields) != 0 || len(resp.Actions) != 0 || resp.Confirmation != nil ||
		resp.UnfinishedTask != nil || resp.UnfinishedSlot != nil || resp.RecoverableSession != nil ||
		strings.TrimSpace(resp.ImageKey) != "" || strings.TrimSpace(resp.FileData) != "" ||
		strings.TrimSpace(resp.FileName) != "" || strings.TrimSpace(resp.FileMimeType) != "" || strings.TrimSpace(resp.VoiceData) != "" ||
		strings.TrimSpace(resp.VoiceFileName) != "" || strings.TrimSpace(resp.VoiceMimeType) != "" ||
		len(resp.VoiceParts) != 0 || strings.TrimSpace(resp.LocalFilePath) != "" || len(resp.LocalFilePaths) != 0 ||
		strings.TrimSpace(resp.ThumbnailBase64) != "" {
		return false
	}
	return resp.ResponseSource == "shared_agent_loop" || resp.ResponseSource == "legacy_agent_loop"
}

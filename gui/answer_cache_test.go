package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestAnswerCacheStoreExpiresAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "answer_cache", "answers.json")
	store := newAnswerCacheStore(path)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	store.put("one", "cached answer", time.Hour, now)
	if got, ok := store.get("one", now.Add(30*time.Minute)); !ok || got != "cached answer" {
		t.Fatalf("cache get = %q, %v; want cached answer, true", got, ok)
	}
	if _, ok := store.get("one", now.Add(time.Hour)); ok {
		t.Fatal("expired answer cache entry was returned")
	}
	store.put("two", "persisted", time.Hour, now)
	reloaded := newAnswerCacheStore(path)
	if got, ok := reloaded.get("two", now.Add(time.Minute)); !ok || got != "persisted" {
		t.Fatalf("reloaded cache get = %q, %v; want persisted, true", got, ok)
	}
}

func TestAnswerCacheConfigUsesTheCurrentLansengerBotProfile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	supportCache := corelib.AnswerCacheConfig{Enabled: true, TTLDays: 7}
	if _, err := app.SaveLansengerBot(corelib.LansengerBotProfile{ID: "support", AnswerCache: &supportCache}); err != nil {
		t.Fatal(err)
	}
	salesCache := corelib.AnswerCacheConfig{Enabled: true, TTLDays: 0}
	if _, err := app.SaveLansengerBot(corelib.LansengerBotProfile{ID: "sales", AnswerCache: &salesCache}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		profile string
		wantTTL int
		enabled bool
	}{
		{name: "enabled bot", profile: "support", wantTTL: 7, enabled: true},
		{name: "disabled bot", profile: "sales", wantTTL: 0, enabled: false},
		{name: "missing profile", profile: "unknown", wantTTL: 0, enabled: false},
		{name: "non lansenger handler", profile: "", wantTTL: 0, enabled: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &IMMessageHandler{app: app, lansengerBotProfileID: tc.profile}
			got, enabled := h.answerCacheConfig()
			if got.TTLDays != tc.wantTTL || enabled != tc.enabled {
				t.Fatalf("answerCacheConfig() = %#v, %v; want ttl=%d enabled=%v", got, enabled, tc.wantTTL, tc.enabled)
			}
		})
	}
}

func TestAnswerCacheStoreBoundsPersistedQuestionMetadata(t *testing.T) {
	store := newAnswerCacheStore(filepath.Join(t.TempDir(), "answer_cache", "answers.json"))
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store.putWithMetadata("large-question", "scope-a", strings.Repeat("q", answerCacheMaxQuestionRunes+1), "answer", time.Hour, now)
	if got, ok := store.get("large-question", now); !ok || got != "answer" {
		t.Fatalf("oversized question lost its exact cache entry: %q, %v", got, ok)
	}
	if _, ok := store.getSemanticWithMaxAge("scope-a", strings.Repeat("q", answerCacheMaxQuestionRunes+1), now, time.Hour); ok {
		t.Fatal("oversized question entered the semantic cache index")
	}
	// Existing cache files can contain question metadata written by an older
	// build. A long incoming question must still bypass their semantic scan.
	store.putWithMetadata("normal-question", "scope-a", "What is the refund policy?", "answer", time.Hour, now)
	if _, ok := store.getSemanticWithMaxAge("scope-a", strings.Repeat("x", answerCacheMaxQuestionRunes+1), now, time.Hour); ok {
		t.Fatal("oversized incoming question performed semantic lookup")
	}
}

func TestAnswerCacheStoreAppliesReducedConfiguredTTLToExistingEntries(t *testing.T) {
	store := newAnswerCacheStore(filepath.Join(t.TempDir(), "answer_cache", "answers.json"))
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store.put("answer", "cached answer", 7*24*time.Hour, now)

	if got, ok := store.getWithMaxAge("answer", now.Add(23*time.Hour), 24*time.Hour); !ok || got != "cached answer" {
		t.Fatalf("entry before reduced TTL = %q, %v; want cached answer, true", got, ok)
	}
	if _, ok := store.getWithMaxAge("answer", now.Add(24*time.Hour), 24*time.Hour); ok {
		t.Fatal("entry outlived reduced configured TTL")
	}
}

func TestAnswerCacheStoreFindsOnlyStrictUniqueSemanticMatches(t *testing.T) {
	store := newAnswerCacheStore(filepath.Join(t.TempDir(), "answer_cache", "answers.json"))
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store.putWithMetadata("refund-policy", "scope-a", "What is the refund policy?", "Refunds are available within 30 days.", time.Hour, now)

	if got, ok := store.getSemanticWithMaxAge("scope-a", "What is the refund policy please?", now.Add(time.Minute), time.Hour); !ok || got != "Refunds are available within 30 days." {
		t.Fatalf("strict semantic hit = %q, %v", got, ok)
	}
	if _, ok := store.getSemanticWithMaxAge("scope-a", "How do I request a refund?", now.Add(time.Minute), time.Hour); ok {
		t.Fatal("different refund question incorrectly reused a cached answer")
	}
	if _, ok := store.getSemanticWithMaxAge("scope-b", "What is the refund policy please?", now.Add(time.Minute), time.Hour); ok {
		t.Fatal("semantic lookup crossed its scope fingerprint")
	}
}

func TestAnswerCacheSemanticLookupRejectsAmbiguousCandidates(t *testing.T) {
	store := newAnswerCacheStore(filepath.Join(t.TempDir(), "answer_cache", "answers.json"))
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store.putWithMetadata("one", "scope-a", "What is the refund policy?", "answer one", time.Hour, now)
	store.putWithMetadata("two", "scope-a", "Refund policy?", "answer two", time.Hour, now)
	if _, ok := store.getSemanticWithMaxAge("scope-a", "Please refund policy?", now.Add(time.Minute), time.Hour); ok {
		t.Fatal("ambiguous semantic candidates must fall back to generation")
	}
}

func TestAnswerCacheSemanticFeaturesHandleConservativeSurfaceVariants(t *testing.T) {
	store := newAnswerCacheStore(filepath.Join(t.TempDir(), "answer_cache", "answers.json"))
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store.putWithMetadata("plural", "scope-a", "What are the refund policies?", "refund policy answer", time.Hour, now)
	if got, ok := store.getSemanticWithMaxAge("scope-a", "What is the refund policy?", now.Add(time.Minute), time.Hour); !ok || got != "refund policy answer" {
		t.Fatalf("English surface variant = %q, %v", got, ok)
	}

	store.putWithMetadata("cn", "scope-b", "退款政策是什么？", "退款政策答案", time.Hour, now)
	if got, ok := store.getSemanticWithMaxAge("scope-b", "退款政策如何规定？", now.Add(time.Minute), time.Hour); !ok || got != "退款政策答案" {
		t.Fatalf("Chinese surface variant = %q, %v", got, ok)
	}
	if _, ok := store.getSemanticWithMaxAge("scope-b", "退款流程是什么？", now.Add(time.Minute), time.Hour); ok {
		t.Fatal("distinct Chinese FAQ incorrectly reused a cached answer")
	}
}

func TestAnswerCacheStoreCoalescesMissesUntilLeaderWrites(t *testing.T) {
	store := newAnswerCacheStore(filepath.Join(t.TempDir(), "answer_cache", "answers.json"))
	leader, done, finish := store.beginFlight("shared-question")
	if !leader || done != nil {
		t.Fatalf("first flight = leader:%v done:%v, want leader", leader, done)
	}
	leader, done, ignoredFinish := store.beginFlight("shared-question")
	if leader || done == nil {
		t.Fatalf("second flight = leader:%v done:%v, want waiter", leader, done)
	}
	ignoredFinish()
	select {
	case <-done:
		t.Fatal("waiter released before leader finished")
	default:
	}
	now := time.Now()
	store.put("shared-question", "safe shared answer", time.Hour, now)
	finish()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("waiter did not release after leader finished")
	}
	if got, ok := store.get("shared-question", now); !ok || got != "safe shared answer" {
		t.Fatalf("leader cache write = %q, %v", got, ok)
	}
}

func TestWaitAnswerCacheFlightCancellationDoesNotReleaseLeader(t *testing.T) {
	store := newAnswerCacheStore(filepath.Join(t.TempDir(), "answer_cache", "answers.json"))
	leader, _, finish := store.beginFlight("shared-question")
	if !leader {
		t.Fatal("initial request did not become the flight leader")
	}
	defer finish()
	_, done, ignoredFinish := store.beginFlight("shared-question")
	ignoredFinish()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitAnswerCacheFlight(ctx.Done(), done) {
		t.Fatal("canceled waiter was reported as released by the leader")
	}
	select {
	case <-done:
		t.Fatal("canceled waiter released the leader flight")
	default:
	}
}

func TestAnswerCacheKeyIsSharedWithinBotButScopedByBotAndBinding(t *testing.T) {
	base := IMUserMessage{Platform: "lansenger", UserID: "profile-a:user-1", Text: "What is the refund policy?"}
	key := answerCacheKey(base, "profile-a", nil)
	if key == "" {
		t.Fatal("base cache key is empty")
	}
	otherUser := base
	otherUser.UserID = "profile-a:user-2"
	if answerCacheKey(otherUser, "profile-a", nil) != key {
		t.Fatal("identical questions from users of the same bot did not share an answer-cache key")
	}
	groupScoped := base
	groupScoped.CacheQuestion = base.Text
	groupScoped.CacheScope = "lansenger:profile-a:group:one"
	groupScoped.Text = "[group context for Alice]\n\nUser message:\n" + base.Text
	secondGroupMember := groupScoped
	secondGroupMember.UserID = "profile-a:user-2"
	secondGroupMember.Text = "[group context for Bob]\n\nUser message:\n" + base.Text
	if answerCacheKey(secondGroupMember, "profile-a", nil) != answerCacheKey(groupScoped, "profile-a", nil) {
		t.Fatal("dynamic group context prevented same-group users from sharing a cache key")
	}
	otherGroup := groupScoped
	otherGroup.CacheScope = "lansenger:profile-a:group:two"
	if answerCacheKey(otherGroup, "profile-a", nil) == answerCacheKey(groupScoped, "profile-a", nil) {
		t.Fatal("different group conversations shared an answer-cache key")
	}
	changedPolicy := groupScoped
	changedPolicy.CachePolicyScope = "different-policy"
	if answerCacheKey(changedPolicy, "profile-a", nil) == answerCacheKey(groupScoped, "profile-a", nil) {
		t.Fatal("different response policies shared an answer-cache key")
	}
	if answerCacheKey(base, "profile-b", nil) == key {
		t.Fatal("different bot profiles shared an answer-cache key")
	}
	differentLanguage := base
	differentLanguage.Lang = "zh-Hans"
	if answerCacheKey(differentLanguage, "profile-a", nil) == key {
		t.Fatal("different response languages shared an answer-cache key")
	}
	languageCaseVariant := base
	languageCaseVariant.Lang = " EN "
	base.Lang = "en"
	if answerCacheKey(languageCaseVariant, "profile-a", nil) != answerCacheKey(base, "profile-a", nil) {
		t.Fatal("equivalent response language variants did not share an answer-cache key")
	}
	expert := base
	expert.AssistantBinding = &agent.AssistantBinding{BotProfileID: "profile-a", Mode: "expert", ExpertID: "support-expert", WorkingDirectory: "D:/support"}
	if answerCacheKey(expert, "profile-a", nil) == key {
		t.Fatal("general and expert bindings shared an answer-cache key")
	}
	expertDef := &ExpertDefinition{ID: "support-expert", SystemPrompt: "Answer as first-line support.", Tools: []string{"search"}}
	expertKey := answerCacheKey(expert, "profile-a", expertDef)
	changedExpert := *expertDef
	changedExpert.SystemPrompt = "Answer as escalation support."
	if answerCacheKey(expert, "profile-a", &changedExpert) == expertKey {
		t.Fatal("expert definition changes shared an answer-cache key")
	}
	// Directory/tool ordering is not policy; saving a reordered form should not
	// invalidate a correct answer. Icon and description are presentation-only,
	// but the expert name is part of the generated system prompt.
	expert.AssistantBinding.DocumentDirectories = []string{"docs/b", "docs/a", "docs/a"}
	expert.AssistantBinding.AllowedDirectories = []string{"work/b", "work/a"}
	expertDef.Tools = []string{"search", "read_file"}
	expertDef.Skills = []string{"faq", "lookup"}
	stableKey := answerCacheKey(expert, "profile-a", expertDef)
	reordered := *expert.AssistantBinding
	reordered.DocumentDirectories = []string{"docs/a", "docs/b"}
	reordered.AllowedDirectories = []string{"work/a", "work/b"}
	reorderedMsg := expert
	reorderedMsg.AssistantBinding = &reordered
	reorderedDef := *expertDef
	reorderedDef.Icon = "support-icon"
	reorderedDef.Description = "Presentation-only expert card text"
	reorderedDef.Tools = []string{"read_file", "search"}
	reorderedDef.Skills = []string{"lookup", "faq"}
	if got := answerCacheKey(reorderedMsg, "profile-a", &reorderedDef); got != stableKey {
		t.Fatal("non-policy list/card reordering unexpectedly changed cache key")
	}
	changedName := reorderedDef
	changedName.Name = "Escalation support"
	if answerCacheKey(reorderedMsg, "profile-a", &changedName) == stableKey {
		t.Fatal("expert name changes shared an answer-cache key")
	}
}

func TestAnswerCacheKeyUsesPreparedBindingSnapshot(t *testing.T) {
	msg := IMUserMessage{
		Platform: "lansenger",
		UserID:   "profile-a:user-1",
		Text:     "What is the refund policy?",
		AssistantBinding: &agent.AssistantBinding{
			BotProfileID:        "profile-a",
			Mode:                "general",
			WorkingDirectory:    "D:/support",
			DocumentDirectories: []string{"D:/support/docs"},
			AllowedDirectories:  []string{"D:/support"},
			AllowAllDirectories: false,
			InitialPrompt:       "Answer as support.",
		},
	}
	scope, errText := prepareAssistantBindingTurn(msg)
	if errText != "" || scope == nil {
		t.Fatalf("prepareAssistantBindingTurn() = %v, %q; want snapshot, empty error", scope, errText)
	}
	snapshotMsg, _ := answerCacheKeyTurnInput(msg, scope)
	snapshotKey := answerCacheKey(snapshotMsg, "profile-a", nil)

	// Simulate a caller reusing and changing its message while this turn waits
	// behind another request for the same user.
	msg.AssistantBinding.WorkingDirectory = "D:/other"
	msg.AssistantBinding.DocumentDirectories[0] = "D:/other/docs"
	msg.AssistantBinding.InitialPrompt = "Answer as another role."
	cacheKeyMsgAfterMutation, _ := answerCacheKeyTurnInput(msg, scope)
	if got := answerCacheKey(cacheKeyMsgAfterMutation, "profile-a", nil); got != snapshotKey {
		t.Fatal("cache key changed after a queued turn's original binding was mutated")
	}
}

func TestAnswerCacheKeyIncludesRuntimeBehaviorPolicy(t *testing.T) {
	msg := IMUserMessage{Platform: "lansenger", UserID: "profile-a:user-1", Text: "What is the refund policy?"}
	policy := answerCacheRuntimePolicy{RoleName: "Support", RoleDescription: "Answer as first-line support.", ProviderID: "primary", Model: "model-a", Protocol: "responses"}
	key := answerCacheKeyWithRuntimePolicy(msg, "profile-a", nil, policy)
	if key == "" {
		t.Fatal("runtime-scoped cache key is empty")
	}
	for name, changed := range map[string]answerCacheRuntimePolicy{
		"role name":        {RoleName: "Escalation", RoleDescription: policy.RoleDescription, ProviderID: policy.ProviderID, Model: policy.Model, Protocol: policy.Protocol},
		"role description": {RoleName: policy.RoleName, RoleDescription: "Answer as escalation support.", ProviderID: policy.ProviderID, Model: policy.Model, Protocol: policy.Protocol},
		"model":            {RoleName: policy.RoleName, RoleDescription: policy.RoleDescription, ProviderID: policy.ProviderID, Model: "model-b", Protocol: policy.Protocol},
		"thinking mode":    {RoleName: policy.RoleName, RoleDescription: policy.RoleDescription, ProviderID: policy.ProviderID, Model: policy.Model, Protocol: policy.Protocol, ThinkingMode: "enabled"},
		"auxiliary model":  {RoleName: policy.RoleName, RoleDescription: policy.RoleDescription, ProviderID: policy.ProviderID, Model: policy.Model, Protocol: policy.Protocol, AuxiliaryLLM: answerCacheAuxiliaryLLMPolicy{URL: "https://aux.example/v1", Model: "fast-model", Protocol: "responses", Enabled: true}},
		"model route":      {RoleName: policy.RoleName, RoleDescription: policy.RoleDescription, ProviderID: policy.ProviderID, Model: policy.Model, Protocol: policy.Protocol, ModelRoutes: []answerCacheModelRoutePolicy{{Task: "fast", Model: "fast-model", URL: "https://fast.example/v1"}}},
		"cost route":       {RoleName: policy.RoleName, RoleDescription: policy.RoleDescription, ProviderID: policy.ProviderID, Model: policy.Model, Protocol: policy.Protocol, CostRouteMode: "on"},
	} {
		if got := answerCacheKeyWithRuntimePolicy(msg, "profile-a", nil, changed); got == key {
			t.Fatalf("%s changes shared an answer-cache key", name)
		}
	}
}

func TestAnswerCacheRequestEligibilityRejectsStatefulTurns(t *testing.T) {
	msg := IMUserMessage{Platform: "lansenger", UserID: "bot:user", Text: "What is the refund policy?"}
	if !answerCacheRequestEligible(msg, imEntryContextResult{}) {
		t.Fatal("plain independent question should be cache eligible")
	}
	if answerCacheRequestEligible(msg, imEntryContextResult{EntriesBeforeClear: []agent.ConversationEntry{{Role: "user", Content: "My order is 123"}}}) {
		t.Fatal("contextual turn must not use a cross-user answer cache")
	}
	for name, entry := range map[string]imEntryContextResult{
		"active workflow":       {WorkflowActive: true},
		"workflow choice":       {WorkflowChoicePending: true},
		"template sub-agent":    {TemplateSubAgentPending: true},
		"workflow agent":        {WorkflowAgentLoop: true},
		"workflow review":       {WorkflowReviewPending: true},
		"workflow document":     {WorkflowDocPhase: true},
		"approved confirmation": {ConfirmedResume: true},
		"confirmation reroute":  {SkipWorkflowRouting: true},
		"ask user":              {HasPendingAskUser: true},
		"pending reply":         {HasPendingUserReply: true},
		"fresh task":            {FreshTask: true},
	} {
		if answerCacheRequestEligible(msg, entry) {
			t.Fatalf("%s turn must bypass answer cache", name)
		}
	}
}

func TestAnswerCacheBypassesRefreshAndFollowUpRequests(t *testing.T) {
	for _, text := range []string{"请更新一下答案", "最新退款政策是什么？", "继续", "Why is that?"} {
		if !hasAnswerCacheRefreshOrFollowUpIntent(text) {
			t.Fatalf("%q should bypass answer cache", text)
		}
	}
	if hasAnswerCacheRefreshOrFollowUpIntent("退款政策是什么？") {
		t.Fatal("independent question unexpectedly bypassed answer cache")
	}
}

func TestAnswerCacheBypassesDissatisfiedAnswerRequests(t *testing.T) {
	for _, text := range []string{
		"这个回答不准确，请重新回答", "我不满意，换一种更详细的回答", "答非所问，请再回答一次", "这个不行，重答", "This answer is not helpful; give me a better answer.",
	} {
		if !hasAnswerCacheRefreshOrFollowUpIntent(text) {
			t.Fatalf("%q should bypass answer cache", text)
		}
	}
}

func TestAnswerCacheBypassesPersonalizedQuestions(t *testing.T) {
	for _, question := range []string{"What is my account status?", "Who am I?", "我的订单状态是什么？", "本人账号还能使用吗？"} {
		if !hasAnswerCachePersonalContextIntent(question) {
			t.Fatalf("%q should bypass shared answer cache", question)
		}
	}
	if hasAnswerCachePersonalContextIntent("What is the refund policy?") {
		t.Fatal("general policy question unexpectedly bypassed answer cache")
	}
	msg := IMUserMessage{Platform: "lansenger", UserID: "bot:user", Text: "[group context for Alice]\n\nUser message:\nWhat is my account status?", CacheQuestion: "What is my account status?"}
	if answerCacheRequestEligible(msg, imEntryContextResult{}) {
		t.Fatal("personalized cache question was eligible")
	}
}

func TestAnswerCacheBypassesLansengerTurnsWithUnscopedPromptContext(t *testing.T) {
	msg := IMUserMessage{
		Platform:      "lansenger_local",
		UserID:        "bot:group:one:user:alice",
		Text:          "[group context]\n\nUser message:\nWhat is the policy?\n\n[quoted message]\nWhat did Alice say?",
		CacheQuestion: "What is the policy?",
	}
	if answerCacheRequestEligible(msg, imEntryContextResult{}) {
		t.Fatal("Lansenger turn with unscoped prompt context was cache eligible")
	}
}

func TestAnswerCacheRequiresGatewayCleanQuestionForLansenger(t *testing.T) {
	msg := IMUserMessage{
		Platform:   "lansenger_local",
		UserID:     "bot:group:one:user:alice",
		Text:       "[group context for Alice]\n\nUser message:\nWhat is the policy?",
		CacheScope: "lansenger:bot:group:one",
	}
	if answerCacheRequestEligible(msg, imEntryContextResult{}) {
		t.Fatal("Lansenger turn without the gateway clean-question proof was cache eligible")
	}
	msg.CacheQuestion = "What is the policy?"
	if !answerCacheRequestEligible(msg, imEntryContextResult{}) {
		t.Fatal("clean independent Lansenger question was not cache eligible")
	}
}

func TestAnswerCacheResponseEligibilityRejectsToolAndInteractiveResults(t *testing.T) {
	if !answerCacheResponseEligible(&IMAgentResponse{Text: "plain answer", ResponseSource: "shared_agent_loop"}) {
		t.Fatal("plain agent response should be cacheable")
	}
	for _, response := range []*IMAgentResponse{
		{Text: "tool answer", ResponseSource: "shared_agent_loop", ToolCallsInTurn: 1},
		{Text: "reasoning", ResponseSource: "shared_agent_loop", Reasoning: "internal trace"},
		{Text: "ask", ResponseSource: "ask_user"},
		{Text: "file", ResponseSource: "shared_agent_loop", LocalFilePath: "D:/report.pdf"},
		{Text: "file metadata", ResponseSource: "shared_agent_loop", FileMimeType: "application/pdf"},
		{Text: "pending voice", ResponseSource: "shared_agent_loop", PendingVoiceParts: 1},
		{Text: "voice metadata", ResponseSource: "shared_agent_loop", VoiceMimeType: "audio/ogg"},
		{Text: "preview", ResponseSource: "shared_agent_loop", ThumbnailBase64: "data"},
		{Text: "clear UI", ResponseSource: "shared_agent_loop", ClearUI: true},
		{Text: "error", ResponseSource: "shared_agent_loop", Error: "failed"},
	} {
		if answerCacheResponseEligible(response) {
			t.Fatalf("unsafe response was cacheable: %#v", response)
		}
	}
}

func TestAnswerCacheStoreResultRejectsCanceledProducer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	msg := IMUserMessage{CancelCtx: ctx}
	response := &IMAgentResponse{Text: "partial answer", ResponseSource: "shared_agent_loop"}
	if answerCacheResultWriteEligible(msg, response) {
		t.Fatal("canceled producer response was eligible for a shared cache write")
	}
	if !answerCacheResultWriteEligible(IMUserMessage{}, response) {
		t.Fatal("completed plain-text response was not eligible for a cache write")
	}
}

func TestAnswerCacheHistorySaveSkipsFollowUpClassification(t *testing.T) {
	h := &IMMessageHandler{memory: agent.NewConversationMemory()}
	called := false
	h.pendingReplyPromptClassifier = func(string) (bool, error) {
		called = true
		return true, nil
	}
	h.saveConversationHistoryTimed("cache-user", []agent.ConversationEntry{
		{Role: "user", Content: "What is the policy?"},
		{Role: "assistant", Content: "Which plan do you use?"},
	}, &IMAgentResponse{ResponseSource: "answer_cache"})
	if called {
		t.Fatal("cache-hit history save invoked pending-reply classification")
	}
	if got := h.memory.Load("cache-user"); len(got) != 2 {
		t.Fatalf("cached exchange history len = %d, want 2", len(got))
	}
	h.saveConversationHistoryTimed("semantic-cache-user", []agent.ConversationEntry{
		{Role: "user", Content: "What is the policy?"},
		{Role: "assistant", Content: "Which plan do you use?"},
	}, &IMAgentResponse{ResponseSource: "answer_cache_semantic"})
	if called {
		t.Fatal("semantic cache-hit history save invoked pending-reply classification")
	}
}

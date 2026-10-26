package guiapp

import (
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

func TestSuggestNewTaskExpertEmptyDoesNotClassify(t *testing.T) {
	if got := (*App)(nil).SuggestNewTaskExpert("润色这篇论文"); got.Confident || got.ExpertID != "" {
		t.Fatalf("nil app = %#v", got)
	}
	if got := (&App{}).SuggestNewTaskExpert("   /goal pause  "); got.Confident || got.ExpertID != "" {
		t.Fatalf("session verb = %#v", got)
	}
	if got := (&App{}).SuggestNewTaskExpert("继续"); got.Confident || got.ExpertID != "" {
		t.Fatalf("thin command = %#v", got)
	}
}

func TestNewTaskExpertCardsKeepsBuiltinsAndSkipsInactiveManaged(t *testing.T) {
	local := []ExpertDefinition{
		{ID: "expert-notes", Name: "笔记专家", Description: "整理研究笔记"},
		{ID: "industry-off", Name: "停用专家", Description: "不该出现"},
		{ID: "industry-on", Name: "在岗专家", Description: "行业问答"},
		{ID: "builtin-pptx-maker", Name: "幻灯片", Description: "自定义演示"},
		{ID: "", Name: "空"},
		{ID: "expert-blank", Name: " ", Description: " "},
	}
	cards := newTaskExpertCards(local, func(id string) (bool, bool) {
		switch id {
		case "industry-off":
			return true, false
		case "industry-on":
			return true, true
		default:
			return false, false
		}
	})
	got := map[string]newTaskExpertCard{}
	for _, card := range cards {
		got[card.ID] = card
	}
	for _, id := range []string{
		"builtin-paper-polish", "builtin-paper-translate", "builtin-pptx-maker",
		builtinLatexExpertID, "expert-notes", "industry-on",
	} {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing %s", id)
		}
	}
	if _, ok := got["industry-off"]; ok {
		t.Fatal("inactive managed expert was included")
	}
	if _, ok := got["expert-blank"]; ok {
		t.Fatal("blank expert was included")
	}
	if got["builtin-pptx-maker"].Name != "幻灯片" || !got["builtin-pptx-maker"].Builtin {
		t.Fatalf("override = %#v", got["builtin-pptx-maker"])
	}
	if got["expert-notes"].Builtin || got["builtin-paper-polish"].UpdatedAt == "" {
		t.Fatalf("polish updated %q notes builtin %v", got["builtin-paper-polish"].UpdatedAt, got["expert-notes"].Builtin)
	}
	if !strings.Contains(got["builtin-paper-translate"].MatchText, "论文翻译") || !strings.Contains(got["builtin-paper-translate"].MatchText, "translate the paper") {
		t.Fatalf("translate match text = %q", got["builtin-paper-translate"].MatchText)
	}
	if strings.Contains(got["expert-notes"].MatchText, "translate the paper") {
		t.Fatal("a user expert must not inherit a built-in hint")
	}
}

func TestSuggestNewTaskExpertAsksTheModelWhenEmbeddingIsUnsure(t *testing.T) {
	catalog := []newTaskExpertCard{
		{ID: "builtin-pptx-maker", Name: "PPT 制作专家"},
		{ID: builtinLatexExpertID, Name: "LaTeX 论文专家"},
		{ID: "builtin-paper-polish", Name: "论文润色专家"},
	}
	embedCalled := false
	llmCalled := false
	got := suggestNewTaskExpert("制作一份庆祝驱动开发网成立并运营26周年的纪念ppt", catalog, taskExpertSuggestDeps{
		embed: func(string, []string) ([]float32, [][]float32, embeddingRead) {
			embedCalled = true
			return nil, nil, embeddingTimeout
		},
		askLLM: func(_ string, seen []newTaskExpertCard) (string, bool) {
			llmCalled = true
			if len(seen) != len(catalog) {
				t.Fatalf("llm catalog = %d", len(seen))
			}
			return `{"expert_id":"builtin-pptx-maker","confidence":0.93}`, true
		},
	})
	if !embedCalled || !llmCalled || !got.Confident || got.ExpertID != "builtin-pptx-maker" || got.Source != "llm" {
		t.Fatalf("timeout = %#v embed=%v llm=%v", got, embedCalled, llmCalled)
	}

	llmCalled = false
	got = suggestNewTaskExpert("做一份演示文稿", catalog, taskExpertSuggestDeps{
		embed: func(string, []string) ([]float32, [][]float32, embeddingRead) {
			return []float32{1, 0}, [][]float32{{0.2, 0.9797958971132712}, {0, 1}, {0, 1}}, embeddingReady
		},
		askLLM: func(string, []newTaskExpertCard) (string, bool) {
			llmCalled = true
			return `{"expert_id":"builtin-pptx-maker","confidence":0.9}`, true
		},
	})
	if !llmCalled || !got.Confident || got.ExpertID != "builtin-pptx-maker" || got.Source != "llm" {
		t.Fatalf("low score = %#v llm=%v", got, llmCalled)
	}

	llmCalled = false
	got = suggestNewTaskExpert("不要做ppt，写一封邮件", catalog, taskExpertSuggestDeps{
		embed: func(string, []string) ([]float32, [][]float32, embeddingRead) {
			return nil, nil, embeddingTimeout
		},
		askLLM: func(string, []newTaskExpertCard) (string, bool) {
			llmCalled = true
			return `{"expert_id":"general","confidence":0.99}`, true
		},
	})
	if !llmCalled || got.Confident || got.ExpertID != "" || got.Reason != "llm-declined" {
		t.Fatalf("model declined = %#v llm=%v", got, llmCalled)
	}
}

func TestExpertFromScoresRequiresFloorAndGap(t *testing.T) {
	catalog := []newTaskExpertCard{
		{ID: "polish", Name: "润色"},
		{ID: "slides", Name: "幻灯片"},
	}
	query := []float32{1, 0, 0}
	clear := expertCosines(query, [][]float32{{1, 0, 0}, {0, 1, 0}})
	if card, ok := expertFromScores(catalog, clear); !ok || card.ID != "polish" {
		t.Fatalf("clear winner = %#v %v", card, ok)
	}
	closeScores := expertCosines([]float32{1, 0}, [][]float32{{0.96, 0.28}, {0.90, 0.4358898943540673}})
	if closeScores[0] < newTaskExpertMinScore || closeScores[1] < newTaskExpertMinScore || closeScores[0]-closeScores[1] >= newTaskExpertMinGap {
		t.Fatalf("close fixture = %v", closeScores)
	}
	if _, ok := expertFromScores(catalog, closeScores); ok {
		t.Fatal("a gap under 0.08 must stay general")
	}
	weak := expertCosines(query, [][]float32{{0.70, 0.714142842854285}, {0, 1, 0}})
	if weak[0] >= newTaskExpertMinScore {
		t.Fatalf("weak fixture = %v", weak)
	}
	if _, ok := expertFromScores(catalog, weak); ok {
		t.Fatal("a score under 0.72 must stay general")
	}
	single := []newTaskExpertCard{{ID: "slides", Name: "幻灯片"}}
	if card, ok := expertFromScores(single, expertCosines(query, [][]float32{{1, 0, 0}})); !ok || card.ID != "slides" {
		t.Fatalf("lone expert = %#v %v", card, ok)
	}
	if _, ok := expertFromScores(single, []float64{-1}); ok {
		t.Fatal("a missing vector must not win")
	}
}

func TestSuggestNewTaskExpertPrefersEmbeddingThenLLM(t *testing.T) {
	catalog := []newTaskExpertCard{
		{ID: "builtin-paper-translate", Name: "论文翻译专家", MatchText: "论文翻译"},
		{ID: "builtin-pptx-maker", Name: "PPT 制作专家", MatchText: "制作PPT"},
	}
	query := []float32{1, 0}
	got := suggestNewTaskExpert("把这篇论文翻译成英文", catalog, taskExpertSuggestDeps{
		embed: func(string, []string) ([]float32, [][]float32, embeddingRead) {
			return query, [][]float32{{1, 0}, {0, 1}}, embeddingReady
		},
		askLLM: func(string, []newTaskExpertCard) (string, bool) {
			t.Fatal("LLM must not run after a confident embedding")
			return "", false
		},
	})
	if !got.Confident || got.ExpertID != "builtin-paper-translate" || got.Source != "local" {
		t.Fatalf("embedding = %#v", got)
	}

	llmCalled := false
	got = suggestNewTaskExpert("帮我做一份项目介绍", catalog, taskExpertSuggestDeps{
		embed: func(string, []string) ([]float32, [][]float32, embeddingRead) {
			return query, [][]float32{{0.96, 0.28}, {0.90, 0.4358898943540673}}, embeddingReady
		},
		askLLM: func(_ string, seen []newTaskExpertCard) (string, bool) {
			llmCalled = true
			if len(seen) != 2 {
				t.Fatalf("llm catalog = %#v", seen)
			}
			return `{"expert_id":"builtin-pptx-maker","confidence":0.91}`, true
		},
	})
	if !llmCalled || !got.Confident || got.ExpertID != "builtin-pptx-maker" || got.ExpertName != "PPT 制作专家" || got.Source != "llm" {
		t.Fatalf("llm = %#v called=%v", got, llmCalled)
	}

	called := false
	got = suggestNewTaskExpert("北京今天天气", catalog, taskExpertSuggestDeps{
		embed: func(string, []string) ([]float32, [][]float32, embeddingRead) {
			return nil, nil, embeddingTimeout
		},
		askLLM: func(string, []newTaskExpertCard) (string, bool) {
			called = true
			return `{"expert_id":"general","confidence":0.99}`, true
		},
	})
	if !called || got.Confident || got.ExpertID != "" || got.Reason != "llm-declined" {
		t.Fatalf("timeout = %#v called=%v", got, called)
	}
}

func TestSuggestNewTaskExpertAsksTheModelWhenEmbeddingIsNotClear(t *testing.T) {
	catalog := []newTaskExpertCard{
		{ID: "polish", Name: "润色", MatchText: "润色"},
		{ID: "slides", Name: "幻灯片", MatchText: "幻灯片"},
	}
	query := []float32{1, 0}
	lowDoc := []float32{0.4, float32(math.Sqrt(1 - 0.16))}
	if scores := expertCosines(query, [][]float32{lowDoc, {0, 1}}); scores[0] >= newTaskExpertAskMinScore {
		t.Fatalf("low fixture = %v", scores)
	}
	called := false
	got := suggestNewTaskExpert("今天天气怎么样", catalog, taskExpertSuggestDeps{
		embed: func(string, []string) ([]float32, [][]float32, embeddingRead) {
			return query, [][]float32{lowDoc, {0, 1}}, embeddingReady
		},
		askLLM: func(string, []newTaskExpertCard) (string, bool) {
			called = true
			return `{"expert_id":"general","confidence":0.97}`, true
		},
	})
	if !called || got.Confident || got.ExpertID != "" || got.Reason != "llm-declined" {
		t.Fatalf("low = %#v called=%v", got, called)
	}

	midDoc := []float32{0.65, float32(math.Sqrt(1 - 0.65*0.65))}
	if scores := expertCosines(query, [][]float32{midDoc, {0, 1}}); scores[0] < newTaskExpertAskMinScore || scores[0] >= newTaskExpertMinScore {
		t.Fatalf("near miss fixture = %v", scores)
	}
	called = false
	got = suggestNewTaskExpert("帮我改一下摘要的措辞", catalog, taskExpertSuggestDeps{
		embed: func(string, []string) ([]float32, [][]float32, embeddingRead) {
			return query, [][]float32{midDoc, {0, 1}}, embeddingReady
		},
		askLLM: func(string, []newTaskExpertCard) (string, bool) {
			called = true
			return `{"expert_id":"polish","confidence":0.9}`, true
		},
	})
	if !called || !got.Confident || got.ExpertID != "polish" || got.Source != "llm" {
		t.Fatalf("near miss = %#v called=%v", got, called)
	}

	called = false
	got = suggestNewTaskExpert("润色摘要", catalog, taskExpertSuggestDeps{
		embed: func(string, []string) ([]float32, [][]float32, embeddingRead) {
			return []float32{1, 0}, [][]float32{{}, {}}, embeddingReady
		},
		askLLM: func(string, []newTaskExpertCard) (string, bool) {
			called = true
			return `{"expert_id":"general","confidence":0.99}`, true
		},
	})
	if !called || got.Confident || got.ExpertID != "" {
		t.Fatalf("unusable vectors = %#v called=%v", got, called)
	}

	called = false
	got = suggestNewTaskExpert("润色摘要", catalog, taskExpertSuggestDeps{
		embed: func(string, []string) ([]float32, [][]float32, embeddingRead) {
			return nil, nil, embeddingUnavailable
		},
		askLLM: func(string, []newTaskExpertCard) (string, bool) {
			called = true
			return `{"expert_id":"general","confidence":0.99}`, true
		},
	})
	if !called || got.Confident || got.ExpertID != "" {
		t.Fatalf("unavailable = %#v called=%v", got, called)
	}

	called = false
	got = suggestNewTaskExpert("润色摘要", catalog, taskExpertSuggestDeps{
		askLLM: func(string, []newTaskExpertCard) (string, bool) {
			called = true
			return `{"expert_id":"polish","confidence":0.88}`, true
		},
	})
	if !called || got.ExpertID != "polish" || got.Source != "llm" {
		t.Fatalf("no embedder = %#v called=%v", got, called)
	}
}

func TestParseNewTaskExpertReplyRejectsAWeakOrUnknownID(t *testing.T) {
	catalog := []newTaskExpertCard{{ID: "builtin-paper-polish", Name: "论文润色专家"}}
	cases := []string{
		`{"expert_id":"missing","confidence":0.9}`,
		`{"expert_id":"builtin-paper-polish","confidence":0.4}`,
		`{"expert_id":"builtin-paper-polish","confidence":"NaN"}`,
		`not json`,
		`{"expert_id":"通用","confidence":0.99}`,
	}
	for _, raw := range cases {
		if _, ok := parseNewTaskExpertReply(raw, catalog); ok {
			t.Fatalf("accepted %s", raw)
		}
	}
	card, ok := parseNewTaskExpertReply("```json\n{\"expertId\":\"Builtin-Paper-Polish\",\"confidence\":\"0.86\"}\n```", catalog)
	if !ok || card.ID != "builtin-paper-polish" {
		t.Fatalf("folded id = %#v %v", card, ok)
	}
}

func TestParseNewTaskExpertReplyAcceptsTheShownName(t *testing.T) {
	catalog := []newTaskExpertCard{
		{ID: "builtin-pptx-maker", Name: "PPT 制作专家"},
		{ID: "builtin-paper-polish", Name: "论文润色专家"},
	}
	card, ok := parseNewTaskExpertReply(`{"expert_id":"PPT制作专家","confidence":0.9}`, catalog)
	if !ok || card.ID != "builtin-pptx-maker" {
		t.Fatalf("name = %#v %v", card, ok)
	}
	card, ok = parseNewTaskExpertReply(`{"expert_id":"builtin-pptx-maker：PPT 制作专家","confidence":93}`, catalog)
	if !ok || card.ID != "builtin-pptx-maker" {
		t.Fatalf("id and name = %#v %v", card, ok)
	}
	card, ok = parseNewTaskExpertReply(`{"expert_id":"builtin-paper-polish","confidence":100}`, catalog)
	if !ok || card.ID != "builtin-paper-polish" {
		t.Fatalf("percent = %#v %v", card, ok)
	}
	if _, ok := parseNewTaskExpertReply(`{"expert_id":"builtin-pptx-maker","confidence":101}`, catalog); ok {
		t.Fatal("accepted a confidence above 100")
	}
	ambiguous := []newTaskExpertCard{
		{ID: "a", Name: "润色"},
		{ID: "b", Name: "润色"},
	}
	if _, ok := parseNewTaskExpertReply(`{"expert_id":"润色","confidence":0.9}`, ambiguous); ok {
		t.Fatal("two cards with one name were accepted")
	}
	pptx := newTaskExpertCard{ID: "builtin-pptx-maker", Name: "PPT 制作专家", Description: "产出 .pptx"}
	echo := pptx.Name + "。" + expertCardScopeLine(pptx)
	card, ok = parseNewTaskExpertReply(`{"expert_id":"`+echo+`","confidence":0.92}`, []newTaskExpertCard{pptx, {ID: "builtin-paper-polish", Name: "论文润色专家"}})
	if !ok || card.ID != pptx.ID {
		t.Fatalf("echoed scope = %#v %v", card, ok)
	}
	if _, ok := parseNewTaskExpertReply(`{"expert_id":"PPT","confidence":0.95}`, []newTaskExpertCard{pptx}); ok {
		t.Fatal("a short token was accepted as the ppt expert")
	}
	if _, ok := parseNewTaskExpertReply(`{"expert_id":"论文润色","confidence":0.95}`, catalog); ok {
		t.Fatal("a clipped name was accepted")
	}
}

func TestExpertModelReplyKeepsADecisionOutsideTheDisplayText(t *testing.T) {
	catalog := []newTaskExpertCard{{ID: "builtin-pptx-maker", Name: "PPT 制作专家"}}
	text, ok := expertModelReply("", `<think>{"expert_id":"builtin-pptx-maker","confidence":0.91}</think>`, "", catalog)
	card, picked := parseNewTaskExpertReply(text, catalog)
	if !ok || !picked || card.ID != "builtin-pptx-maker" {
		t.Fatalf("think tag = %q ok=%v card=%#v", text, ok, card)
	}
	text, ok = expertModelReply(`{"expert_id":"general","confidence":0.95}`, "", `{"expert_id":"builtin-pptx-maker","confidence":0.99}`, catalog)
	if !ok {
		t.Fatal("missing decline")
	}
	if _, picked := parseNewTaskExpertReply(text, catalog); picked {
		t.Fatal("an explicit general was replaced")
	}
	text, ok = expertModelReply("好的", "", `{"expert_id":"PPT 制作专家。产出 .pptx","confidence":0.9}`, catalog)
	card, picked = parseNewTaskExpertReply(text, catalog)
	if !ok || !picked || card.ID != "builtin-pptx-maker" {
		t.Fatalf("reasoning = %q ok=%v card=%#v", text, ok, card)
	}
	if _, ok := expertModelReply("  ", "", "", catalog); ok {
		t.Fatal("empty fields looked like a reply")
	}
}

func TestNewTaskExpertSystemPromptShowsTheBuiltinScope(t *testing.T) {
	prompt := newTaskExpertSystemPrompt([]newTaskExpertCard{
		{ID: "builtin-pptx-maker", Name: "PPT 制作专家", Description: "产出 .pptx"},
		{ID: "expert-notes", Name: "笔记专家", Description: "整理研究笔记"},
	})
	if !strings.Contains(prompt, "builtin-pptx-maker") || !strings.Contains(prompt, "幻灯片") || !strings.Contains(prompt, "演示文稿") {
		t.Fatalf("pptx scope = %s", prompt)
	}
	parts := strings.Split(prompt, "expert-notes")
	if len(parts) < 2 || strings.Contains(parts[1], "幻灯片") {
		t.Fatal("a user expert picked up the ppt hint")
	}
}

func TestSuggestNewTaskExpertModelFailureStaysGeneral(t *testing.T) {
	catalog := []newTaskExpertCard{
		{ID: "builtin-pptx-maker", Name: "PPT 制作专家"},
		{ID: builtinLatexExpertID, Name: "LaTeX 论文专家"},
	}
	near := []float32{0.65, float32(math.Sqrt(1 - 0.65*0.65))}
	got := suggestNewTaskExpert("做一份演示文稿", catalog, taskExpertSuggestDeps{
		embed: func(string, []string) ([]float32, [][]float32, embeddingRead) {
			return []float32{1, 0}, [][]float32{near, {0, 1}}, embeddingReady
		},
		askLLM: func(string, []newTaskExpertCard) (string, bool) {
			return "", false
		},
	})
	if got.Confident || got.ExpertID != "" || got.Reason != "llm-unavailable" {
		t.Fatalf("model down = %#v", got)
	}
	got = suggestNewTaskExpert("做一份演示文稿", catalog, taskExpertSuggestDeps{
		embed: func(string, []string) ([]float32, [][]float32, embeddingRead) {
			return nil, nil, embeddingTimeout
		},
	})
	if got.Confident || got.Reason != "embed-timeout" {
		t.Fatalf("no model = %#v", got)
	}
}

func TestExpertCatalogForLLMPrefersTheStrongerCards(t *testing.T) {
	cards := make([]newTaskExpertCard, 30)
	scores := make([]float64, 30)
	for i := range cards {
		cards[i] = newTaskExpertCard{ID: strconv.Itoa(i), Name: "n"}
		scores[i] = float64(i) / 100
	}
	got := expertCatalogForLLM(cards, scores)
	if len(got) != newTaskExpertLLMCap || got[0].ID != "29" {
		t.Fatalf("ranked = %s len %d", got[0].ID, len(got))
	}
	plain := expertCatalogForLLM(cards, nil)
	if len(plain) != newTaskExpertLLMCap || plain[0].ID != "0" {
		t.Fatalf("plain = %s len %d", plain[0].ID, len(plain))
	}
}

func TestExpertCatalogForLLMKeepsBuiltins(t *testing.T) {
	cards := []newTaskExpertCard{
		{ID: "builtin-pptx-maker", Name: "PPT", Builtin: true},
		{ID: "builtin-latex-paper", Name: "LaTeX", Builtin: true},
	}
	for i := 0; i < 30; i++ {
		cards = append(cards, newTaskExpertCard{ID: "user-" + strconv.Itoa(i), Name: "u"})
	}
	scores := make([]float64, len(cards))
	scores[0] = 0.02
	scores[1] = 0.01
	for i := 2; i < len(scores); i++ {
		scores[i] = 0.5 + float64(i)/1000
	}
	got := expertCatalogForLLM(cards, scores)
	if len(got) != newTaskExpertLLMCap || got[0].ID != "user-29" || got[0].Builtin {
		t.Fatalf("lead = %#v len %d", got[0], len(got))
	}
	seen := map[string]bool{}
	for _, card := range got {
		seen[card.ID] = true
	}
	if !seen["builtin-pptx-maker"] || !seen["builtin-latex-paper"] {
		t.Fatalf("builtins dropped from %d cards", len(got))
	}
}

func TestSuggestNewTaskExpertEmbedsOnlyTheCap(t *testing.T) {
	catalog := make([]newTaskExpertCard, newTaskExpertEmbedCap+5)
	for i := range catalog {
		id := strconv.Itoa(i)
		catalog[i] = newTaskExpertCard{ID: id, Name: "n", MatchText: id}
	}
	var seen []string
	suggestNewTaskExpert("整理笔记", catalog, taskExpertSuggestDeps{
		embed: func(_ string, docs []string) ([]float32, [][]float32, embeddingRead) {
			seen = append([]string(nil), docs...)
			return nil, nil, embeddingUnavailable
		},
		askLLM: func(string, []newTaskExpertCard) (string, bool) { return "", false },
	})
	last := strconv.Itoa(newTaskExpertEmbedCap - 1)
	if len(seen) != newTaskExpertEmbedCap || seen[0] != "0" || seen[len(seen)-1] != last {
		t.Fatalf("embedded %v", seen)
	}
}

func TestCapNewTaskExpertCardsKeepsBuiltinsAndNewest(t *testing.T) {
	cards := []newTaskExpertCard{{
		ID: "builtin-old", Name: "内置", MatchText: "内置", Builtin: true, UpdatedAt: "2020-01-01T00:00:00Z",
	}}
	for i := 0; i < newTaskExpertEmbedCap; i++ {
		cards = append(cards, newTaskExpertCard{
			ID: "mid-" + strconv.Itoa(i), Name: "n", MatchText: "n", UpdatedAt: "2024-01-01T00:00:00Z",
		})
	}
	cards = append(cards,
		newTaskExpertCard{ID: "old", Name: "旧", MatchText: "旧", UpdatedAt: "2019-01-01T00:00:00Z"},
		newTaskExpertCard{ID: "newest", Name: "新", MatchText: "新", UpdatedAt: "2026-06-01T00:00:00Z"},
	)
	got := capNewTaskExpertCards(cards)
	if len(got) != newTaskExpertEmbedCap || got[0].ID != "builtin-old" {
		t.Fatalf("capped = %d first %s", len(got), got[0].ID)
	}
	seen := map[string]int{}
	for i, card := range got {
		seen[card.ID] = i
	}
	if _, ok := seen["newest"]; !ok {
		t.Fatal("newest expert was dropped")
	}
	if _, ok := seen["old"]; ok {
		t.Fatal("old expert was kept")
	}
	if seen["mid-0"] == 0 || seen["mid-1"] == 0 || seen["mid-0"] > seen["mid-1"] {
		t.Fatalf("stable order mid-0=%d mid-1=%d", seen["mid-0"], seen["mid-1"])
	}
}

type scriptExpertEmbedder struct {
	query   []float32
	batch   [][]float32
	err     error
	seen    []string
	embeds  int
	batches int
}

func (s *scriptExpertEmbedder) Embed(text string) ([]float32, error) {
	s.embeds++
	s.seen = append(s.seen, text)
	if s.err != nil {
		return nil, s.err
	}
	return s.query, nil
}

func (s *scriptExpertEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	s.batches++
	s.seen = append(s.seen, texts...)
	if s.err != nil {
		return nil, s.err
	}
	return s.batch, nil
}

func (s *scriptExpertEmbedder) Dim() int { return len(s.query) }
func (s *scriptExpertEmbedder) Close()   {}

func TestEmbedNewTaskExpertVectors(t *testing.T) {
	if _, _, read := embedNewTaskExpertVectors(nil, "润色", []string{"论文"}, time.Second); read != embeddingUnavailable {
		t.Fatalf("nil embedder = %v", read)
	}
	if _, _, read := embedNewTaskExpertVectors(embedding.NoopEmbedder{}, "润色", []string{"论文"}, time.Second); read != embeddingUnavailable {
		t.Fatalf("noop embedder = %v", read)
	}
	script := &scriptExpertEmbedder{query: []float32{1, 0}, batch: [][]float32{{0, 1}}}
	q, docs, read := embedNewTaskExpertVectors(script, "润色论文", []string{"论文润色"}, time.Second)
	if read != embeddingReady || len(q) != 2 || len(docs) != 1 || len(script.seen) < 2 {
		t.Fatalf("ready = %v q=%v docs=%v seen=%v", read, q, docs, script.seen)
	}
	short := &scriptExpertEmbedder{query: []float32{1}, batch: [][]float32{}}
	if _, _, read := embedNewTaskExpertVectors(short, "润色", []string{"论文"}, time.Second); read != embeddingUnavailable {
		t.Fatalf("short batch = %v", read)
	}
}

type blockingExpertEmbedder struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingExpertEmbedder) Embed(string) ([]float32, error) {
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	<-b.release
	return []float32{1}, nil
}

func (b *blockingExpertEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	return out, nil
}

func (b *blockingExpertEmbedder) Dim() int { return 1 }
func (b *blockingExpertEmbedder) Close()   {}

func TestEmbedNewTaskExpertVectorsTimesOut(t *testing.T) {
	emb := &blockingExpertEmbedder{started: make(chan struct{}), release: make(chan struct{})}
	defer close(emb.release)
	_, _, read := embedNewTaskExpertVectors(emb, "润色论文", []string{"论文润色"}, 20*time.Millisecond)
	if read != embeddingTimeout {
		t.Fatalf("read = %v", read)
	}
}

type countingExpertEmbedder struct {
	model   string
	embeds  int
	batches int
}

func (e *countingExpertEmbedder) Embed(string) ([]float32, error) {
	e.embeds++
	return []float32{1, 0}, nil
}

func (e *countingExpertEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	e.batches++
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0, 1}
	}
	return out, nil
}

func (e *countingExpertEmbedder) Dim() int        { return 2 }
func (e *countingExpertEmbedder) Close()          {}
func (e *countingExpertEmbedder) ModelID() string { return e.model }

func TestEmbedNewTaskExpertVectorsCachesDocuments(t *testing.T) {
	resetNewTaskExpertDocVecCache()
	t.Cleanup(resetNewTaskExpertDocVecCache)
	emb := &countingExpertEmbedder{model: "a"}
	docs := []string{"论文润色", "制作PPT"}
	_, vecs, read := embedNewTaskExpertVectors(emb, "润色论文", docs, time.Second)
	if read != embeddingReady || emb.embeds != 1 || emb.batches != 1 || len(vecs) != 2 || vecs[0][1] != 1 {
		t.Fatalf("first read=%v embeds=%d batches=%d vecs=%v", read, emb.embeds, emb.batches, vecs)
	}
	_, vecs, read = embedNewTaskExpertVectors(emb, "再润色", docs, time.Second)
	if read != embeddingReady || emb.embeds != 2 || emb.batches != 1 {
		t.Fatalf("cached read=%v embeds=%d batches=%d", read, emb.embeds, emb.batches)
	}
	vecs[0][0] = 7
	_, vecs, read = embedNewTaskExpertVectors(emb, "第三次", docs, time.Second)
	if read != embeddingReady || emb.batches != 1 || vecs[0][0] != 0 {
		t.Fatalf("copy read=%v batches=%d vec=%v", read, emb.batches, vecs[0])
	}
	emb.model = "b"
	_, _, read = embedNewTaskExpertVectors(emb, "换模型", docs, time.Second)
	if read != embeddingReady || emb.embeds != 4 || emb.batches != 2 {
		t.Fatalf("model change embeds=%d batches=%d read=%v", emb.embeds, emb.batches, read)
	}
}

type gateExpertEmbedder struct {
	embeds  int
	batches int
	gate    chan struct{}
	done    chan struct{}
}

func (g *gateExpertEmbedder) Embed(string) ([]float32, error) {
	g.embeds++
	if g.embeds == 1 {
		<-g.gate
	}
	return []float32{1, 0}, nil
}

func (g *gateExpertEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	g.batches++
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0, 1}
	}
	select {
	case g.done <- struct{}{}:
	default:
	}
	return out, nil
}

func (g *gateExpertEmbedder) Dim() int { return 2 }
func (g *gateExpertEmbedder) Close()   {}

func TestEmbedNewTaskExpertVectorsTimeoutKeepsAFinishedBatch(t *testing.T) {
	resetNewTaskExpertDocVecCache()
	t.Cleanup(resetNewTaskExpertDocVecCache)
	emb := &gateExpertEmbedder{gate: make(chan struct{}), done: make(chan struct{}, 1)}
	_, _, read := embedNewTaskExpertVectors(emb, "润色论文", []string{"论文润色"}, 20*time.Millisecond)
	if read != embeddingTimeout {
		t.Fatalf("read = %v", read)
	}
	close(emb.gate)
	select {
	case <-emb.done:
	case <-time.After(time.Second):
		t.Fatal("abandoned embed did not finish")
	}
	deadline := time.Now().Add(time.Second)
	for {
		newTaskExpertDocVecCache.mu.Lock()
		_, ok := newTaskExpertDocVecCache.docs["论文润色"]
		newTaskExpertDocVecCache.mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a finished document batch was dropped after the deadline")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, _, read = embedNewTaskExpertVectors(emb, "再润色", []string{"论文润色"}, time.Second)
	if read != embeddingReady || emb.batches != 1 || emb.embeds != 2 {
		t.Fatalf("cached after timeout batches=%d embeds=%d read=%v", emb.batches, emb.embeds, read)
	}
}

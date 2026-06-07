package knowledge

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestBuildCardsForNodes(t *testing.T) {
	source := Source{ID: "s1", Kind: SourceKindMarkdown, Title: "Guide", TopicHint: "sqlite, brain", ProjectPath: "D:/project", SourceTrust: 0.9}
	nodes := []DocumentNode{{ID: "n1", SourceID: "s1", Type: "document", Title: "Guide", Text: "Use SQLite as the durable local knowledge substrate. Keep retrieval local and fast.", TokenCount: 40}}

	cards := BuildCardsForNodes(source, nodes)
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(cards))
	}
	card := cards[0]
	if card.SourceID != source.ID || card.NodeID != nodes[0].ID {
		t.Fatalf("card linkage = %#v", card)
	}
	if card.Claim == "" || card.Summary == "" {
		t.Fatalf("expected claim and summary: %#v", card)
	}
	if len(card.Topics) != 2 || card.Topics[0] != "sqlite" || card.Topics[1] != "brain" {
		t.Fatalf("unexpected topics: %#v", card.Topics)
	}
	if !containsString(card.Entities, "SQLite") {
		t.Fatalf("unexpected entities: %#v", card.Entities)
	}
	facts := BuildFactsForCard(source, card)
	if len(facts) == 0 {
		t.Fatalf("expected derived facts")
	}
}

func TestBuildFactsForCardExtractsChineseRelations(t *testing.T) {
	source := Source{ID: "s1", Kind: SourceKindMarkdown, Title: "知识库设计", TopicHint: "知识库", SourceTrust: 0.9}
	cases := []struct {
		name      string
		claim     string
		predicate string
		object    string
	}{
		{name: "identity", claim: "知识库系统是企业外脑。", predicate: "是", object: "企业外脑"},
		{name: "ownership", claim: "知识库数据属于当前项目。", predicate: "属于", object: "当前项目"},
		{name: "responsibility", claim: "知识库系统负责本地检索。", predicate: "负责", object: "本地检索"},
		{name: "provider", claim: "知识库接口提供来源摘要。", predicate: "提供", object: "来源摘要"},
		{name: "purpose", claim: "知识库接口用于本地召回。", predicate: "用于", object: "本地召回"},
		{name: "through", claim: "知识库系统通过SQLite实现本地检索。", predicate: "通过", object: "SQLite"},
		{name: "by", claim: "批量导入由导入服务负责。", predicate: "由", object: "导入服务"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			card := Card{
				ID:       "c1",
				SourceID: source.ID,
				Title:    "知识库系统",
				Claim:    tc.claim,
			}
			facts := BuildFactsForCard(source, card)
			if !containsFact(facts, tc.predicate, tc.object) {
				t.Fatalf("expected predicate %q object %q in facts: %#v", tc.predicate, tc.object, facts)
			}
		})
	}
}

func TestBuildCardsForNodesExtractsQuotedChineseEntities(t *testing.T) {
	source := Source{ID: "s1", Kind: SourceKindMarkdown, Title: "知识库设计", SourceTrust: 0.9}
	nodes := []DocumentNode{{
		ID:       "n1",
		SourceID: source.ID,
		Type:     "document",
		Title:    "知识库设计",
		Text:     "《MaClaw 知识中脑》负责沉淀企业经验，并把「项目A」的文档转成可检索卡片。",
	}}

	cards := BuildCardsForNodes(source, nodes)
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(cards))
	}
	if !containsString(cards[0].Entities, "MaClaw 知识中脑") || !containsString(cards[0].Entities, "项目A") {
		t.Fatalf("expected quoted entities: %#v", cards[0].Entities)
	}
}

func TestBuildFactsForCardExtractsSummarySentences(t *testing.T) {
	source := Source{ID: "s1", Kind: SourceKindMarkdown, Title: "知识库设计", TopicHint: "知识库", SourceTrust: 0.9}
	card := Card{
		ID:       "c1",
		SourceID: source.ID,
		Title:    "知识库接口",
		Claim:    "知识库接口用于系统集成。",
		Summary:  "导入服务负责批量文档解析。知识库接口提供来源摘要。",
	}

	facts := BuildFactsForCard(source, card)
	if !containsFact(facts, "负责", "批量文档解析") || !containsFact(facts, "提供", "来源摘要") {
		t.Fatalf("expected facts from summary sentences: %#v", facts)
	}
}

func TestBuildFactsForCardStopsChineseObjectAtClausePunctuation(t *testing.T) {
	source := Source{ID: "s1", Kind: SourceKindMarkdown, Title: "知识库设计", TopicHint: "知识库", SourceTrust: 0.9}
	card := Card{
		ID:       "c1",
		SourceID: source.ID,
		Title:    "知识库接口",
		Claim:    "知识库接口用于本地召回，并提供来源摘要。",
	}

	facts := BuildFactsForCard(source, card)
	if !containsFact(facts, "用于", "本地召回") || containsFact(facts, "用于", "本地召回，并提供来源摘要") {
		t.Fatalf("expected clause-bounded Chinese object: %#v", facts)
	}
	if !containsExactFact(facts, "知识库接口", "提供", "来源摘要") {
		t.Fatalf("expected omitted-subject clause fact with clean subject: %#v", facts)
	}
	for _, fact := range facts {
		if fact.Predicate == "提供" && strings.Contains(fact.Subject, "本地召回") {
			t.Fatalf("unexpected cross-clause subject: %#v", facts)
		}
	}
}

type mockCardCaller struct {
	configured bool
	response   string
}

func (m mockCardCaller) ChatCall(messages []map[string]string) (string, error) {
	return m.response, nil
}

func (m mockCardCaller) IsConfigured() bool {
	return m.configured
}

func TestLLMCardDistillerNormalizesCards(t *testing.T) {
	distiller := &LLMCardDistiller{Caller: mockCardCaller{configured: true, response: `[
		{"title":"Local Retrieval","claim":"查询阶段应优先使用本地结构化索引。","summary":"写入阶段可以使用 LLM 提升卡片质量，但查询阶段不应强依赖 LLM。","topics":["knowledge"],"tags":["distilled"],"confidence":0.92,"importance":1.4,"node_id":"n1"}
	]`}}
	source := Source{ID: "s1", Kind: SourceKindMarkdown, Title: "Knowledge Design", ProjectPath: "D:/project", SourceTrust: 0.8}
	nodes := []DocumentNode{{ID: "n1", SourceID: "s1", Type: "document", Text: "写入时结构化，查询时本地检索。", TokenCount: 80}}

	cards, err := distiller.DistillCards(context.Background(), source, nodes)
	if err != nil {
		t.Fatalf("DistillCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(cards))
	}
	card := cards[0]
	if card.SourceID != source.ID || card.ProjectPath != source.ProjectPath || card.SourceTrust != source.SourceTrust {
		t.Fatalf("card was not normalized against source: %#v", card)
	}
	if card.ID == "" || card.Claim == "" || card.Summary == "" {
		t.Fatalf("expected normalized card fields: %#v", card)
	}
}

func TestLLMCardDistillerNormalizesFacts(t *testing.T) {
	distiller := &LLMCardDistiller{Caller: mockCardCaller{configured: true, response: `[
		{"title":"Local Retrieval","claim":"MaClaw queries local structured indexes.","summary":"Write-time LLM can improve card and fact quality while query-time retrieval stays local.","topics":["knowledge"],"tags":["distilled"],"confidence":0.92,"importance":1.4,"node_id":"n1","facts":[{"subject":"MaClaw","predicate":"queries","object":"local structured index","confidence":0.91}]}
	]`}}
	source := Source{ID: "s1", Kind: SourceKindMarkdown, Title: "Knowledge Design", ProjectPath: "D:/project", SourceTrust: 0.8}
	nodes := []DocumentNode{{ID: "n1", SourceID: "s1", Type: "document", Text: "MaClaw queries local structured indexes.", TokenCount: 80}}

	cards, err := distiller.DistillCards(context.Background(), source, nodes)
	if err != nil {
		t.Fatalf("DistillCards: %v", err)
	}
	if len(cards) != 1 {
		t.Fatalf("cards = %d, want 1", len(cards))
	}
	card := cards[0]
	if len(card.Facts) != 1 || card.Facts[0].SourceID != source.ID || card.Facts[0].CardID != card.ID || card.Facts[0].Subject != "MaClaw" {
		t.Fatalf("expected normalized LLM facts: %#v", card.Facts)
	}
}

func TestLLMCardDistillerAcceptsFlexibleMarkdownJSONFence(t *testing.T) {
	distiller := &LLMCardDistiller{Caller: mockCardCaller{configured: true, response: "``` JSON\n[\n{\"title\":\"Local Retrieval\",\"claim\":\"MaClaw uses local indexes.\",\"summary\":\"MaClaw uses local indexes for recall.\"}\n]\n```"}}
	source := Source{ID: "s1", Kind: SourceKindMarkdown, Title: "Knowledge Design", ProjectPath: "D:/project", SourceTrust: 0.8}
	nodes := []DocumentNode{{ID: "n1", SourceID: "s1", Type: "document", Text: "MaClaw uses local indexes.", TokenCount: 80}}

	cards, err := distiller.DistillCards(context.Background(), source, nodes)
	if err != nil {
		t.Fatalf("DistillCards: %v", err)
	}
	if len(cards) != 1 || cards[0].Claim != "MaClaw uses local indexes." {
		t.Fatalf("unexpected cards from fenced JSON: %#v", cards)
	}
}

func TestLLMCardDistillerAcceptsWrappedAndSingleCardJSON(t *testing.T) {
	source := Source{ID: "s1", Kind: SourceKindMarkdown, Title: "Knowledge Design", ProjectPath: "D:/project", SourceTrust: 0.8}
	nodes := []DocumentNode{{ID: "n1", SourceID: "s1", Type: "document", Text: "MaClaw uses local indexes.", TokenCount: 80}}
	cases := []struct {
		name     string
		response string
		claim    string
	}{
		{name: "cards envelope", response: `{"cards":[{"title":"Local Retrieval","claim":"MaClaw uses local indexes.","summary":"Recall stays local."}]}`, claim: "MaClaw uses local indexes."},
		{name: "items envelope", response: `{"items":[{"title":"Local Retrieval","claim":"MaClaw indexes cards locally.","summary":"Recall stays local."}]}`, claim: "MaClaw indexes cards locally."},
		{name: "single card", response: `{"title":"Local Retrieval","claim":"MaClaw stores cards locally.","summary":"Recall stays local."}`, claim: "MaClaw stores cards locally."},
		{name: "prose fenced envelope", response: "Here is the result:\n```json\n{\"cards\":[{\"title\":\"Local Retrieval\",\"claim\":\"MaClaw retrieves from SQLite.\",\"summary\":\"Recall stays local.\"}]}\n```\nDone.", claim: "MaClaw retrieves from SQLite."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			distiller := &LLMCardDistiller{Caller: mockCardCaller{configured: true, response: tc.response}}
			cards, err := distiller.DistillCards(context.Background(), source, nodes)
			if err != nil {
				t.Fatalf("DistillCards: %v", err)
			}
			if len(cards) != 1 || cards[0].Claim != tc.claim {
				t.Fatalf("unexpected cards: %#v", cards)
			}
		})
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsFact(facts []Fact, predicate, object string) bool {
	for _, fact := range facts {
		if fact.Predicate == predicate && fact.Object == object {
			return true
		}
	}
	return false
}

func containsExactFact(facts []Fact, subject, predicate, object string) bool {
	for _, fact := range facts {
		if fact.Subject == subject && fact.Predicate == predicate && fact.Object == object {
			return true
		}
	}
	return false
}


func TestBuildCardsForNodesListSplitting(t *testing.T) {
	source := Source{ID: "s1", Kind: SourceKindPDF, Title: "resume-academic", SourceTrust: 0.9}
	// Simulate a publications page with academic citations.
	text := `代表性论文：
1. Ma X, Zhang Y, et al. A novel prompt-tuning method incorporating scenario-specific concepts into a unified model[J]. Expert Systems with Applications, 2024.
2. Ma X, Li W, et al. Enhancing Source Code Classification Effectiveness via Prompt Learning Incorporating Grounding Features[J]. Empirically, 2024.
3. Ma X, et al. PromptSQL: Knowledge-based Prompts for Code Generation[C]. AAAI 2023.
4. Ma X, et al. Dynamic Soft Prompt for Few-Shot Active Learning[J]. NeurIPS 2023.
5. Zhang Y, Ma X, et al. Cross-lingual Transfer via Multilingual Co-training[J]. ACL 2023.`

	nodes := []DocumentNode{{
		ID:       "n1",
		SourceID: source.ID,
		Type:     "page",
		Title:    "resume-academic p.2",
		Text:     text,
	}}

	cards := BuildCardsForNodes(source, nodes)
	// With 5 academic citation lines + 1 header line (6 lines, >40% list items),
	// list splitting should produce multiple cards instead of 1.
	if len(cards) < 2 {
		t.Fatalf("expected list splitting to produce >=2 cards, got %d", len(cards))
	}

	// Verify that individual paper titles appear in card Claims, not just the header.
	foundPaper := false
	for _, card := range cards {
		if strings.Contains(card.Claim, "novel prompt-tuning") || strings.Contains(card.Claim, "PromptSQL") || strings.Contains(card.Claim, "Dynamic Soft Prompt") {
			foundPaper = true
			break
		}
	}
	if !foundPaper {
		claims := make([]string, len(cards))
		for i, c := range cards {
			claims[i] = c.Claim
		}
		t.Fatalf("expected paper titles in card Claims, got: %v", claims)
	}

	// Verify the "list_item" tag is present.
	hasListTag := false
	for _, card := range cards {
		for _, tag := range card.Tags {
			if tag == "list_item" {
				hasListTag = true
				break
			}
		}
	}
	if !hasListTag {
		t.Fatal("expected 'list_item' tag on list-derived cards")
	}
}

func TestBuildCardsForNodesNoListSplittingForProse(t *testing.T) {
	source := Source{ID: "s1", Kind: SourceKindPDF, Title: "guide", SourceTrust: 0.9}
	// Normal prose text should NOT trigger list splitting.
	text := `MacLaw is an AI-powered development environment. It provides tools for
developers to focus on what matters: designing systems, exploring solutions,
and making decisions. The system uses a combination of retrieval and generation
to produce high-quality answers. It supports multiple languages and frameworks.`

	nodes := []DocumentNode{{
		ID:       "n1",
		SourceID: source.ID,
		Type:     "page",
		Title:    "guide p.1",
		Text:     text,
	}}

	cards := BuildCardsForNodes(source, nodes)
	if len(cards) != 1 {
		t.Fatalf("expected 1 card for prose text, got %d", len(cards))
	}
	// Verify no "list_item" tag.
	for _, tag := range cards[0].Tags {
		if tag == "list_item" {
			t.Fatal("unexpected 'list_item' tag on prose-derived card")
		}
	}
}

func TestSplitPDFPageIntoSegments(t *testing.T) {
	// Short page: no splitting.
	short := "Hello world. This is a short page."
	segs := splitPDFPageIntoSegments(short)
	if len(segs) != 1 || segs[0] != short {
		t.Fatalf("short page should not be split, got %d segments", len(segs))
	}

	// Long page with double-newline paragraphs — each para must be large enough
	// that multiple paras exceed targetTextNodeRunes (6000).
	var longParas strings.Builder
	for i := 0; i < 5; i++ {
		// Each paragraph ~2000 runes → total ~10000, forces split into 2+ segments.
		longParas.WriteString(strings.Repeat("段落内容测试文字填充。", 200))
		if i < 4 {
			longParas.WriteString("\n\n")
		}
	}
	segs = splitPDFPageIntoSegments(longParas.String())
	if len(segs) < 2 {
		t.Fatalf("long paragraph page should be split, got %d segments (total runes=%d)", len(segs), len([]rune(longParas.String())))
	}

	// Long page with list items (no double-newlines).
	// Need total > targetTextNodeRunes (6000) to force splitting into 2+ chunks.
	var longList strings.Builder
	for i := 0; i < 80; i++ {
		longList.WriteString(fmt.Sprintf("%d. A comprehensive study on advanced transformer architectures for multilingual natural language understanding et al. [J]. International Journal of Artificial Intelligence Research, 2024.\n", i+1))
	}
	segs = splitPDFPageIntoSegments(longList.String())
	if len(segs) < 2 {
		t.Fatalf("long list page should be split, got %d segments (total runes=%d)", len(segs), len([]rune(longList.String())))
	}
}

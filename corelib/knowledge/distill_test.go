package knowledge

import (
	"context"
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

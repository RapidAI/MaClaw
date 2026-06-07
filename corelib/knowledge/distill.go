package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	cardClaimRunes     = 220
	cardSummaryRunes   = 700
	maxEntitiesPerCard = 12
	maxFactsPerCard    = 10
)

const (
	DistillModeAuto     = "auto"
	DistillModeRules    = "rules_only"
	DistillModeLLMIfAny = "llm_if_available"
)

var entityCandidatePattern = regexp.MustCompile(`[A-Z][A-Za-z0-9_.+-]{2,}|[\p{Han}A-Za-z0-9]{2,18}(?:公司|项目|系统|模型|服务|工具|平台|框架|数据库|知识库|外脑|中脑|内脑|文档|网页|目录|卡片)`)
var quotedEntityPattern = regexp.MustCompile(`[《「『“"]([^》」』”"]{2,40})[》」』”"]`)

var factPatterns = []struct {
	predicate string
	re        *regexp.Regexp
}{
	{"uses", regexp.MustCompile(`(?i)^(.{2,80}?)\s+(?:uses|use|adopts|stores in|stores|queries|imports|saves|generates|supports|depends on|requires|contains)\s+(.{2,120})`)},
	{"使用", regexp.MustCompile(`^(.{2,40}?)(?:使用|采用|支持|依赖|包含|存储|查询|导入|保存|生成)([^，。；;,.!?！？]{2,80})`)},
	{"是", regexp.MustCompile(`^(.{2,40}?)(?:是|为|作为)([^，。；;,.!?！？]{2,80})`)},
	{"属于", regexp.MustCompile(`^(.{2,40}?)(?:属于|归属|隶属于)([^，。；;,.!?！？]{2,80})`)},
	{"负责", regexp.MustCompile(`^(.{2,40}?)(?:负责|管理|维护|治理|编排)([^，。；;,.!?！？]{2,80})`)},
	{"提供", regexp.MustCompile(`^(.{2,40}?)(?:提供|暴露|发布|输出)([^，。；;,.!?！？]{2,80})`)},
	{"用于", regexp.MustCompile(`^(.{2,40}?)(?:用于|用来|面向)([^，。；;,.!?！？]{2,80})`)},
	{"通过", regexp.MustCompile(`^(.{2,40}?)(?:通过|基于|借助)(.{2,80})(?:实现|完成|提供|生成|构建)(?:.{0,40})$`)},
	{"由", regexp.MustCompile(`^(.{2,80}?)(?:由)(.{2,40}?)(?:负责|管理|维护|提供|执行|生成|构建)(?:.{0,40})$`)},
}

var omittedSubjectFactPatterns = []struct {
	predicate string
	re        *regexp.Regexp
}{
	{"使用", regexp.MustCompile(`^(?:并|同时|还|也)?(?:使用|采用|支持|依赖|包含|存储|查询|导入|保存|生成)([^，。；;,.!?！？]{2,80})`)},
	{"负责", regexp.MustCompile(`^(?:并|同时|还|也)?(?:负责|管理|维护|治理|编排)([^，。；;,.!?！？]{2,80})`)},
	{"提供", regexp.MustCompile(`^(?:并|同时|还|也)?(?:提供|暴露|发布|输出)([^，。；;,.!?！？]{2,80})`)},
	{"用于", regexp.MustCompile(`^(?:并|同时|还|也)?(?:用于|用来|面向)([^，。；;,.!?！？]{2,80})`)},
}

var topicSplitter = regexp.MustCompile(`[[:space:],;:/\\|#\x{3001}\x{FF0C}\x{FF1B}]+`)

type CardDistiller interface {
	DistillCards(ctx context.Context, source Source, nodes []DocumentNode) ([]Card, error)
}

type LLMCardCaller interface {
	ChatCall(messages []map[string]string) (string, error)
	IsConfigured() bool
}

type ContextualLLMCardCaller interface {
	ChatCallContext(ctx context.Context, messages []map[string]string) (string, error)
}

type LLMCardDistiller struct {
	Caller       LLMCardCaller
	MaxInputRune int
}

func (d *LLMCardDistiller) DistillCards(ctx context.Context, source Source, nodes []DocumentNode) ([]Card, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if d == nil || d.Caller == nil || !d.Caller.IsConfigured() {
		return nil, fmt.Errorf("LLM card distiller is not configured")
	}
	input := buildCardDistillInput(source, nodes, d.MaxInputRune)
	if strings.TrimSpace(input) == "" {
		return nil, fmt.Errorf("empty distillation input")
	}
	messages := []map[string]string{
		{"role": "system", "content": `You distill long documents into compact knowledge cards for an external brain.
Return ONLY a JSON array. Each object must have: title, claim, summary, topics, tags, confidence, importance, node_id, facts.
Rules:
- Use the source text only. Do not invent facts.
- claim: one precise, self-contained sentence.
- summary: 1-3 concise sentences with concrete facts, names, numbers, paths, dates, constraints, or decisions when present.
- facts: optional array of grounded triples with subject, predicate, object, confidence. Use only explicit source text.
- topics/tags: short arrays. confidence and importance are numbers from 0 to 1.5.
- Prefer 3-8 cards for long input, fewer for short input.`},
		{"role": "user", "content": input},
	}
	var resp string
	var err error
	if caller, ok := d.Caller.(ContextualLLMCardCaller); ok {
		resp, err = caller.ChatCallContext(ctx, messages)
	} else {
		resp, err = d.Caller.ChatCall(messages)
	}
	if err != nil {
		return nil, err
	}
	cards, err := parseLLMCards(resp)
	if err != nil {
		return nil, err
	}
	return NormalizeDistilledCards(source, cards), nil
}

func BuildCardsForNodes(source Source, nodes []DocumentNode) []Card {
	if len(nodes) == 0 {
		return nil
	}
	now := time.Now().UTC()
	cards := make([]Card, 0, len(nodes))
	for _, node := range nodes {
		text := normalizeWhitespace(node.Text)
		if text == "" {
			continue
		}
		// Check if the ORIGINAL text (with newlines preserved) contains
		// list-structured content. normalizeWhitespace collapses newlines to
		// spaces, which destroys list structure detection.
		rawText := strings.TrimSpace(node.Text)
		if listCards := buildCardsForListNode(source, node, rawText, now); len(listCards) > 0 {
			cards = append(cards, listCards...)
			continue
		}
		claim := firstMeaningfulSentence(text)
		if claim == "" {
			claim = truncateRunes(text, cardClaimRunes)
		}
		summary := truncateRunes(text, cardSummaryRunes)
		title := fallbackText(node.Title, source.Title)
		entities := extractEntities(title + " " + claim + " " + summary)
		cards = append(cards, Card{
			ID:          NewID("kcard"),
			SourceID:    source.ID,
			NodeID:      node.ID,
			Title:       title,
			Claim:       truncateRunes(claim, cardClaimRunes),
			Summary:     summary,
			Entities:    entities,
			Topics:      topicsForSource(source),
			Tags:        []string{source.Kind, node.Type},
			ProjectPath: source.ProjectPath,
			OwnerID:     source.OwnerID,
			TenantID:    source.TenantID,
			Confidence:  0.65,
			Importance:  importanceForNode(node),
			SourceTrust: source.SourceTrust,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	return cards
}

// buildCardsForListNode checks if a node's text has list structure (numbered items,
// academic citations, bullet points) and generates one card per item group.
// Returns nil if the text does not have recognizable list structure.
func buildCardsForListNode(source Source, node DocumentNode, text string, now time.Time) []Card {
	lines := strings.Split(text, "\n")
	if len(lines) < 4 {
		return nil
	}
	// Count lines that look like list items.
	listLineCount := 0
	nonEmptyCount := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nonEmptyCount++
		if isDistillListItemLine(line) {
			listLineCount++
		}
	}
	if nonEmptyCount == 0 {
		return nil
	}
	// Require at least 40% of non-empty lines to look like list items.
	if float64(listLineCount)/float64(nonEmptyCount) < 0.4 {
		return nil
	}

	// Group consecutive list items into cards. Each card covers 1-3 items
	// to maintain context while keeping claims focused.
	const maxItemsPerCard = 3
	var cards []Card
	var currentItems []string

	flushItems := func() {
		if len(currentItems) == 0 {
			return
		}
		itemText := strings.Join(currentItems, "\n")
		claim := currentItems[0] // First item as claim
		if len(claim) > cardClaimRunes {
			claim = truncateRunes(claim, cardClaimRunes)
		}
		summary := truncateRunes(itemText, cardSummaryRunes)
		title := fallbackText(node.Title, source.Title)
		entities := extractEntities(title + " " + claim)
		cards = append(cards, Card{
			ID:          NewID("kcard"),
			SourceID:    source.ID,
			NodeID:      node.ID,
			Title:       title,
			Claim:       claim,
			Summary:     summary,
			Entities:    entities,
			Topics:      topicsForSource(source),
			Tags:        []string{source.Kind, node.Type, "list_item"},
			ProjectPath: source.ProjectPath,
			OwnerID:     source.OwnerID,
			TenantID:    source.TenantID,
			Confidence:  0.60,
			Importance:  importanceForNode(node),
			SourceTrust: source.SourceTrust,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		currentItems = nil
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if isDistillListItemLine(line) {
			if len(currentItems) >= maxItemsPerCard {
				flushItems()
			}
			currentItems = append(currentItems, line)
		} else {
			// Non-list line (section header like "代表性论文：").
			// Flush any accumulated list items, but do NOT include this line
			// in the next card's items — it's context, not a searchable entry.
			// It will naturally be part of the node's overall text for the
			// document_nodes FTS index.
			if len(currentItems) > 0 {
				flushItems()
			}
			// Skip non-list lines — they don't become card claims.
		}
	}
	flushItems()

	// Only return list cards if we generated at least 2 (otherwise not worth splitting).
	if len(cards) < 2 {
		return nil
	}
	return cards
}

// isDistillListItemLine delegates to the shared isListItemLine from parse.go
// (same package) — single source of truth for list-item detection patterns.
func isDistillListItemLine(line string) bool {
	return isListItemLine(line)
}

func NormalizeDistillMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", DistillModeAuto:
		return DistillModeAuto
	case "rules", "rule", "no_llm", "local", "local_only", DistillModeRules:
		return DistillModeRules
	case "llm", "llm_optional", "llm_if_configured", DistillModeLLMIfAny:
		return DistillModeLLMIfAny
	default:
		return DistillModeAuto
	}
}

func shouldUseLLMForMode(mode string, source Source, nodes []DocumentNode) bool {
	switch NormalizeDistillMode(mode) {
	case DistillModeRules:
		return false
	case DistillModeLLMIfAny:
		return true
	default:
		return ShouldUseLLMCardDistillation(source, nodes)
	}
}

func NormalizeDistilledCards(source Source, cards []Card) []Card {
	if len(cards) == 0 {
		return nil
	}
	now := time.Now().UTC()
	cleaned := make([]Card, 0, len(cards))
	for _, card := range cards {
		card.Claim = strings.TrimSpace(card.Claim)
		card.Summary = strings.TrimSpace(card.Summary)
		if card.Claim == "" && card.Summary == "" {
			continue
		}
		if card.Claim == "" {
			card.Claim = truncateRunes(card.Summary, cardClaimRunes)
		}
		if card.Summary == "" {
			card.Summary = truncateRunes(card.Claim, cardSummaryRunes)
		}
		if card.ID == "" {
			card.ID = NewID("kcard")
		}
		card.SourceID = source.ID
		if card.Title == "" {
			card.Title = source.Title
		}
		if card.ProjectPath == "" {
			card.ProjectPath = source.ProjectPath
		}
		if card.OwnerID == "" {
			card.OwnerID = source.OwnerID
		}
		if card.TenantID == "" {
			card.TenantID = source.TenantID
		}
		if len(card.Entities) == 0 {
			card.Entities = extractEntities(card.Title + " " + card.Claim + " " + card.Summary)
		} else {
			card.Entities = normalizeStringList(card.Entities, maxEntitiesPerCard)
		}
		if len(card.Topics) == 0 {
			card.Topics = topicsForSource(source)
		} else {
			card.Topics = normalizeStringList(card.Topics, maxEntitiesPerCard)
		}
		if len(card.Tags) == 0 {
			card.Tags = []string{source.Kind, "llm_distilled"}
		} else {
			card.Tags = normalizeStringList(card.Tags, maxEntitiesPerCard)
		}
		card.Facts = normalizeDistilledFacts(source, card, card.Facts, nil)
		if card.Confidence <= 0 {
			card.Confidence = 0.75
		}
		if card.Importance <= 0 {
			card.Importance = 1.2
		}
		if card.SourceTrust <= 0 {
			card.SourceTrust = source.SourceTrust
		}
		if card.CreatedAt.IsZero() {
			card.CreatedAt = now
		}
		if card.UpdatedAt.IsZero() {
			card.UpdatedAt = now
		}
		card.Claim = truncateRunes(card.Claim, cardClaimRunes)
		card.Summary = truncateRunes(card.Summary, cardSummaryRunes)
		cleaned = append(cleaned, card)
	}
	return cleaned
}

func buildCardDistillInput(source Source, nodes []DocumentNode, maxRunes int) string {
	if maxRunes <= 0 {
		maxRunes = 12000
	}
	var b strings.Builder
	fmt.Fprintf(&b, "source_id: %s\nsource_title: %s\nsource_kind: %s\nsource_uri: %s\ntopic_hint: %s\n\n", source.ID, source.Title, source.Kind, fallbackText(source.CanonicalURI, source.URI), source.TopicHint)
	remaining := maxRunes - len([]rune(b.String()))
	for _, node := range nodes {
		if remaining <= 0 {
			break
		}
		text := strings.TrimSpace(node.Text)
		if text == "" {
			continue
		}
		chunk := truncateRunes(text, remaining)
		fmt.Fprintf(&b, "[node]\nid: %s\ntitle: %s\ntype: %s\ntext:\n%s\n\n", node.ID, fallbackText(node.Title, source.Title), node.Type, chunk)
		remaining = maxRunes - len([]rune(b.String()))
	}
	return b.String()
}

func parseLLMCards(raw string) ([]Card, error) {
	body := stripMarkdownJSONFence(raw)
	if body == "" {
		return nil, fmt.Errorf("empty LLM card response")
	}
	var cards []Card
	if err := json.Unmarshal([]byte(body), &cards); err == nil {
		return cards, nil
	}
	if wrapped := parseWrappedLLMCards(body); len(wrapped) > 0 {
		return wrapped, nil
	}
	start := strings.Index(body, "[")
	end := strings.LastIndex(body, "]")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(body[start:end+1]), &cards); err == nil {
			return cards, nil
		}
	}
	return nil, fmt.Errorf("parse LLM card response failed")
}

func parseWrappedLLMCards(body string) []Card {
	var envelope struct {
		Cards []Card `json:"cards"`
		Items []Card `json:"items"`
		Data  []Card `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err == nil {
		for _, cards := range [][]Card{envelope.Cards, envelope.Items, envelope.Data} {
			if len(cards) > 0 {
				return cards
			}
		}
	}
	var single Card
	if err := json.Unmarshal([]byte(body), &single); err == nil && (strings.TrimSpace(single.Claim) != "" || strings.TrimSpace(single.Summary) != "") {
		return []Card{single}
	}
	return nil
}

func stripMarkdownJSONFence(raw string) string {
	body := strings.TrimSpace(raw)
	if !strings.HasPrefix(body, "```") {
		if fenced := firstMarkdownFenceBody(body); fenced != "" {
			return fenced
		}
		return body
	}
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSpace(body)
	firstLineEnd := strings.IndexAny(body, "\r\n")
	if firstLineEnd < 0 {
		return strings.TrimSpace(strings.TrimSuffix(body, "```"))
	}
	firstLine := strings.ToLower(strings.TrimSpace(body[:firstLineEnd]))
	if firstLine == "json" || firstLine == "javascript" || firstLine == "js" {
		body = body[firstLineEnd:]
	}
	body = strings.TrimSpace(body)
	body = strings.TrimSuffix(body, "```")
	return strings.TrimSpace(body)
}

func firstMarkdownFenceBody(body string) string {
	start := strings.Index(body, "```")
	if start < 0 {
		return ""
	}
	rest := body[start+3:]
	end := strings.Index(rest, "```")
	if end < 0 {
		return ""
	}
	return stripMarkdownJSONFence("```" + rest[:end] + "```")
}

func BuildFactsForCard(source Source, card Card) []Fact {
	card = enrichCardStructure(source, card)
	if card.ID == "" || card.SourceID == "" {
		return nil
	}
	facts := make([]Fact, 0, maxFactsPerCard)
	seen := make(map[string]struct{})
	add := func(subject, predicate, object string, confidence float64) {
		if len(facts) >= maxFactsPerCard {
			return
		}
		subject = cleanFactPart(subject)
		predicate = cleanFactPart(predicate)
		object = cleanFactPart(object)
		if subject == "" || predicate == "" || object == "" || strings.EqualFold(subject, object) {
			return
		}
		key := strings.ToLower(subject + "\x00" + predicate + "\x00" + object)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		if confidence <= 0 {
			confidence = 0.55
		}
		facts = append(facts, Fact{ID: NewID("kfact"), CardID: card.ID, SourceID: source.ID, Subject: subject, Predicate: predicate, Object: object, Confidence: confidence})
	}

	for _, fact := range normalizeDistilledFacts(source, card, card.Facts, seen) {
		facts = append(facts, fact)
	}

	subject := fallbackText(firstListValue(card.Entities), card.Title)
	if subject == "" {
		subject = fallbackText(source.Title, source.ID)
	}
	for _, text := range factCandidateTexts(card) {
		for _, pattern := range factPatterns {
			match := pattern.re.FindStringSubmatch(text)
			if len(match) >= 3 {
				add(match[1], pattern.predicate, match[2], 0.72)
			}
		}
		for _, pattern := range omittedSubjectFactPatterns {
			match := pattern.re.FindStringSubmatch(text)
			if len(match) >= 2 {
				add(subject, pattern.predicate, match[1], 0.62)
			}
		}
	}

	for _, topic := range card.Topics {
		add(subject, "topic", topic, 0.62)
	}
	for _, entity := range card.Entities {
		if !strings.EqualFold(entity, subject) {
			add(subject, "mentions", entity, 0.58)
		}
	}
	return facts
}

func factCandidateTexts(card Card) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 1+len(card.Summary)/40)
	add := func(value string) {
		value = normalizeWhitespace(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	addSplit := func(value string) {
		sentences := splitFactSentences(value)
		if len(sentences) == 0 {
			add(value)
			return
		}
		for _, sentence := range sentences {
			add(sentence)
		}
	}
	addSplit(card.Claim)
	addSplit(card.Summary)
	return out
}

func splitFactSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	out := make([]string, 0, 4)
	start := 0
	for i, r := range text {
		switch r {
		case '.', '\u3002', '!', '\uff01', '?', '\uff1f', '\n', ';', '\uff1b', ',', '\uff0c':
			candidate := strings.TrimSpace(text[start : i+len(string(r))])
			start = i + len(string(r))
			if len([]rune(candidate)) >= 4 {
				out = append(out, candidate)
			}
			if len(out) >= 8 {
				return out
			}
		}
	}
	if start < len(text) && len(out) < 8 {
		candidate := strings.TrimSpace(text[start:])
		if len([]rune(candidate)) >= 4 {
			out = append(out, candidate)
		}
	}
	return out
}

func normalizeDistilledFacts(source Source, card Card, input []Fact, seen map[string]struct{}) []Fact {
	if len(input) == 0 {
		return nil
	}
	if seen == nil {
		seen = make(map[string]struct{})
	}
	out := make([]Fact, 0, len(input))
	for _, fact := range input {
		if len(out) >= maxFactsPerCard {
			break
		}
		subject := cleanFactPart(fact.Subject)
		predicate := cleanFactPart(fact.Predicate)
		object := cleanFactPart(fact.Object)
		if subject == "" || predicate == "" || object == "" || strings.EqualFold(subject, object) {
			continue
		}
		key := strings.ToLower(subject + "\x00" + predicate + "\x00" + object)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		fact.ID = NewID("kfact")
		fact.CardID = card.ID
		fact.SourceID = source.ID
		fact.Subject = subject
		fact.Predicate = predicate
		fact.Object = object
		if fact.Confidence <= 0 {
			fact.Confidence = 0.78
		}
		out = append(out, fact)
	}
	return out
}

func enrichCardStructure(source Source, card Card) Card {
	if len(card.Entities) == 0 {
		card.Entities = extractEntities(card.Title + " " + card.Claim + " " + card.Summary)
	} else {
		card.Entities = normalizeStringList(card.Entities, maxEntitiesPerCard)
	}
	if len(card.Topics) == 0 {
		card.Topics = topicsForSource(source)
	} else {
		card.Topics = normalizeStringList(card.Topics, maxEntitiesPerCard)
	}
	if len(card.Tags) > 0 {
		card.Tags = normalizeStringList(card.Tags, maxEntitiesPerCard)
	}
	return card
}

func extractEntities(text string) []string {
	matches := entityCandidatePattern.FindAllString(text, -1)
	entities := make([]string, 0, len(matches))
	for _, match := range matches {
		appendEntityCandidate(&entities, match)
	}
	for _, match := range quotedEntityPattern.FindAllStringSubmatch(text, -1) {
		if len(match) >= 2 {
			appendEntityCandidate(&entities, match[1])
		}
	}
	return normalizeStringList(entities, maxEntitiesPerCard)
}

func appendEntityCandidate(entities *[]string, value string) {
	value = strings.Trim(value, " \t\r\n.,;:!?()[]{}<> '\"，。；：！？（）【】《》「」『』“”")
	if value == "" || isEntityStopWord(value) || !looksEntityName(value) {
		return
	}
	*entities = append(*entities, value)
}

func normalizeStringList(values []string, max int) []string {
	if max <= 0 {
		max = len(values)
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.Trim(value, " \t\r\n.,;:!?()[]{}<> '\"，。；：！？（）【】《》")
		value = normalizeWhitespace(value)
		if value == "" {
			continue
		}
		if len([]rune(value)) > 80 {
			value = truncateRunes(value, 80)
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if len(out) >= max {
			break
		}
	}
	return out
}

func looksEntityName(value string) bool {
	upper := 0
	digit := false
	nonASCII := false
	for _, r := range value {
		switch {
		case r >= 'A' && r <= 'Z':
			upper++
		case r >= '0' && r <= '9':
			digit = true
		case r > 127:
			nonASCII = true
		}
	}
	return nonASCII || digit || upper >= 2 || strings.ContainsAny(value, "._+-")
}

func isEntityStopWord(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "the", "and", "for", "with", "use", "uses", "using", "this", "that", "from", "into", "local", "plain", "guide":
		return true
	default:
		return false
	}
}

func cleanFactPart(value string) string {
	value = normalizeWhitespace(value)
	value = strings.Trim(value, " \t\r\n.,;:!?()[]{}<> '\"，。；：！？（）【】《》")
	return truncateRunes(value, 120)
}

func firstListValue(values []string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func ShouldUseLLMCardDistillation(source Source, nodes []DocumentNode) bool {
	if len(nodes) == 0 {
		return false
	}
	if strings.TrimSpace(source.TopicHint) != "" {
		return true
	}
	totalTokens := 0
	for _, node := range nodes {
		totalTokens += node.TokenCount
		if node.TokenCount >= 600 {
			return true
		}
	}
	return totalTokens >= 1200
}

func normalizeWhitespace(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	fields := strings.Fields(text)
	return strings.Join(fields, " ")
}

func firstMeaningfulSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	start := 0
	for i, r := range text {
		switch r {
		case '.', '\u3002', '!', '\uff01', '?', '\uff1f', '\n':
			candidate := strings.TrimSpace(text[start : i+len(string(r))])
			start = i + len(string(r))
			if len([]rune(candidate)) >= 12 {
				return candidate
			}
		}
	}
	return strings.TrimSpace(text)
}

func truncateRunes(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return strings.TrimSpace(string(runes[:max])) + "..."
}

func topicsForSource(source Source) []string {
	seen := map[string]struct{}{}
	var topics []string
	for _, part := range topicSplitter.Split(source.TopicHint, -1) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		topics = append(topics, part)
	}
	return topics
}

func importanceForNode(node DocumentNode) float64 {
	importance := 1.0
	if node.Type == "webpage" || node.Type == "document" {
		importance += 0.15
	}
	if node.Level > 0 && node.Level <= 2 {
		importance += 0.1
	}
	if node.TokenCount > 1000 {
		importance += 0.15
	}
	return importance
}

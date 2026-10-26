package guiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

// NewTaskExpertSuggestion is the wizard's reading of which installed expert
// should take a new task. ExpertID is set only when Confident is true. An
// empty suggestion means no expert is a clear fit, and the wizard keeps 通用.
type NewTaskExpertSuggestion struct {
	ExpertID   string `json:"expertId"`
	ExpertName string `json:"expertName"`
	Confident  bool   `json:"confident"`
	Reason     string `json:"reason,omitempty"`
	Source     string `json:"source,omitempty"`
}

const (
	// Embedding assigns an expert only when one card is clearly nearer than
	// the others. A near miss, a low score, a timeout, or no embedder is not
	// that reading: the model sees the whole command and may answer general.
	// The command is not matched with a keyword list.
	newTaskExpertMinScore = 0.72
	newTaskExpertMinGap   = 0.08
	// Logged when no model is configured and the best score stays under the
	// floor. A configured model is still asked; the score is not a keyword.
	newTaskExpertAskMinScore = 0.58
	// The embedder call is capped so a long catalog cannot hold task creation
	// open. Built-ins always stay inside the cap. The remaining slots go to
	// the newest other experts.
	newTaskExpertEmbedCap   = 64
	newTaskExpertLLMCap     = 24
	newTaskExpertDescRunes  = 160
	newTaskExpertMatchRunes = 240
)

// newTaskExpertCard is one installed expert the command can be matched to.
// MatchText is the embedder's document. The model sees the name, the
// description, and the same built-in hint, so both readers judge one scope.
type newTaskExpertCard struct {
	ID          string
	Name        string
	Description string
	MatchText   string
	Builtin     bool
	UpdatedAt   string
}

// SuggestNewTaskExpert reads a new-task command against the installed experts.
// It never fails the create: an unsure, thin, or unavailable reading returns
// an empty suggestion so the wizard keeps 通用.
func (a *App) SuggestNewTaskExpert(text string) NewTaskExpertSuggestion {
	command := newTaskCommandForType(text)
	if a == nil || command == "" || newTaskCommandIsThin(command) {
		return NewTaskExpertSuggestion{}
	}
	catalog := loadNewTaskExpertCatalog()
	if len(catalog) == 0 {
		return NewTaskExpertSuggestion{}
	}
	suggestion := suggestNewTaskExpert(command, catalog, a.taskExpertSuggestDeps())
	if suggestion.Confident && strings.TrimSpace(suggestion.ExpertID) != "" {
		log.Printf("[task-expert] suggested=%s source=%s", suggestion.ExpertID, suggestion.Source)
	} else if suggestion.Reason != "" {
		log.Printf("[task-expert] abstain reason=%s", suggestion.Reason)
	}
	return suggestion
}

type taskExpertSuggestDeps struct {
	embed  func(query string, docs []string) ([]float32, [][]float32, embeddingRead)
	askLLM func(command string, catalog []newTaskExpertCard) (string, bool)
}

func (a *App) taskExpertSuggestDeps() taskExpertSuggestDeps {
	deps := taskExpertSuggestDeps{
		embed: func(query string, docs []string) ([]float32, [][]float32, embeddingRead) {
			if a == nil {
				return nil, nil, embeddingUnavailable
			}
			return embedNewTaskExpertVectors(a.intentEmbedder, query, docs, newTaskTypeLocalTimeout)
		},
	}
	// No assistant model means the embedding reason is the abstain. A closure
	// that only fails the call would hide that reason behind llm-unavailable.
	if expertLLMConfigured(a) {
		deps.askLLM = func(command string, catalog []newTaskExpertCard) (string, bool) {
			return a.askNewTaskExpertLLM(command, catalog)
		}
	}
	return deps
}

func expertLLMConfigured(a *App) bool {
	if a == nil {
		return false
	}
	cfg := a.GetMaclawLLMConfig()
	return strings.TrimSpace(cfg.URL) != "" && strings.TrimSpace(cfg.Model) != ""
}

func suggestNewTaskExpert(command string, catalog []newTaskExpertCard, deps taskExpertSuggestDeps) NewTaskExpertSuggestion {
	command = strings.TrimSpace(command)
	if command == "" || newTaskCommandIsThin(command) || len(catalog) == 0 {
		return NewTaskExpertSuggestion{}
	}
	embedded := capNewTaskExpertCards(catalog)
	var scores []float64
	read := embeddingUnavailable
	if deps.embed != nil {
		docs := make([]string, len(embedded))
		for i, card := range embedded {
			docs[i] = card.MatchText
		}
		var vectors [][]float32
		var query []float32
		query, vectors, read = deps.embed(command, docs)
		if read == embeddingReady {
			scores = expertCosines(query, vectors)
			if card, ok := expertFromScores(embedded, scores); ok {
				return expertSuggestion(card, "local")
			}
		}
	}
	// Embedding did not name an expert. A configured model reads the whole
	// command, including a timeout or a low score, and may answer general.
	// No model keeps the embedding reason.
	if deps.askLLM != nil {
		narrowed := expertCatalogForLLM(embedded, scores)
		raw, ok := deps.askLLM(command, narrowed)
		if !ok {
			return newTaskExpertAbstain("llm-unavailable")
		}
		if card, ok := parseNewTaskExpertReply(raw, narrowed); ok {
			return expertSuggestion(card, "llm")
		}
		return newTaskExpertAbstain("llm-declined")
	}
	return newTaskExpertAbstain(expertEmbedAbstainReason(read, embedded, scores))
}

func newTaskExpertAbstain(reason string) NewTaskExpertSuggestion {
	return NewTaskExpertSuggestion{Reason: reason}
}

func expertSuggestion(card newTaskExpertCard, source string) NewTaskExpertSuggestion {
	return NewTaskExpertSuggestion{
		ExpertID:   card.ID,
		ExpertName: card.Name,
		Confident:  true,
		Source:     source,
	}
}

func loadNewTaskExpertCatalog() []newTaskExpertCard {
	local, _, err := defaultExpertStore.List()
	if err != nil {
		log.Printf("[task-expert] expert list failed: %v", err)
		local = nil
	}
	known, active := defaultManagedIndustryExpertStore.localIDFlags()
	return newTaskExpertCards(local, func(id string) (bool, bool) {
		return known[id], active[id]
	})
}

// newTaskExpertCards is the picker catalog: built-ins, user experts, and
// active managed-industry installs. An inactive managed install is omitted.
// managed reports whether an id is a managed-industry expert and whether
// that install is active.
func newTaskExpertCards(local []ExpertDefinition, managed func(id string) (isManaged bool, active bool)) []newTaskExpertCard {
	personal := make([]ExpertDefinition, 0, len(local))
	industry := make([]ExpertDefinition, 0)
	for _, def := range local {
		id := strings.TrimSpace(def.ID)
		if id == "" {
			continue
		}
		if managed != nil {
			if isManaged, active := managed(id); isManaged {
				if active {
					industry = append(industry, def)
				}
				continue
			}
		}
		personal = append(personal, def)
	}
	merged := mergeBuiltinExpertList(personal)
	seen := make(map[string]bool, len(merged)+len(industry))
	out := make([]newTaskExpertCard, 0, len(merged)+len(industry))
	for _, def := range append(merged, industry...) {
		card, ok := newTaskExpertCardFrom(def)
		if !ok || seen[card.ID] {
			continue
		}
		seen[card.ID] = true
		out = append(out, card)
	}
	return out
}

func newTaskExpertCardFrom(def ExpertDefinition) (newTaskExpertCard, bool) {
	id := strings.TrimSpace(def.ID)
	name := strings.TrimSpace(def.Name)
	desc := truncateRunes(strings.TrimSpace(def.Description), newTaskExpertDescRunes)
	if id == "" || (name == "" && desc == "") {
		return newTaskExpertCard{}, false
	}
	if name == "" {
		name = id
	}
	match := strings.TrimSpace(strings.Join([]string{name, desc, builtinExpertMatchHint(id)}, " "))
	match = truncateRunes(match, newTaskExpertMatchRunes)
	if match == "" {
		return newTaskExpertCard{}, false
	}
	return newTaskExpertCard{
		ID: id, Name: name, Description: desc, MatchText: match,
		Builtin: def.Builtin, UpdatedAt: strings.TrimSpace(def.UpdatedAt),
	}, true
}

// capNewTaskExpertCards keeps every built-in, then the newest other experts,
// up to the embed cap. Equal timestamps keep catalog order, so an undated
// catalog still embeds its original prefix. An empty timestamp is the oldest.
func capNewTaskExpertCards(catalog []newTaskExpertCard) []newTaskExpertCard {
	if len(catalog) <= newTaskExpertEmbedCap {
		return catalog
	}
	builtins := make([]newTaskExpertCard, 0, 4)
	rest := make([]newTaskExpertCard, 0, len(catalog))
	for _, card := range catalog {
		if card.Builtin {
			builtins = append(builtins, card)
			continue
		}
		rest = append(rest, card)
	}
	if len(builtins) >= newTaskExpertEmbedCap {
		return builtins[:newTaskExpertEmbedCap]
	}
	sort.SliceStable(rest, func(i, j int) bool {
		return rest[i].UpdatedAt > rest[j].UpdatedAt
	})
	out := make([]newTaskExpertCard, 0, newTaskExpertEmbedCap)
	out = append(out, builtins...)
	out = append(out, rest[:newTaskExpertEmbedCap-len(builtins)]...)
	return out
}

func expertEmbedAbstainReason(read embeddingRead, catalog []newTaskExpertCard, scores []float64) string {
	switch read {
	case embeddingTimeout:
		return "embed-timeout"
	case embeddingReady:
		return expertScoreAbstainReason(catalog, scores)
	default:
		return "embed-unavailable"
	}
}

func expertScoreAbstainReason(catalog []newTaskExpertCard, scores []float64) string {
	if len(catalog) == 0 || len(scores) != len(catalog) {
		return "embed-unusable"
	}
	rank := rankExpertScores(scores)
	if rank.idx < 0 {
		return "embed-unusable"
	}
	id := catalog[rank.idx].ID
	switch {
	case rank.best < newTaskExpertAskMinScore:
		return fmt.Sprintf("below-ask best=%s score=%.2f", id, rank.best)
	case rank.best < newTaskExpertMinScore:
		return fmt.Sprintf("below-floor best=%s score=%.2f", id, rank.best)
	case rank.second >= 0 && rank.best-rank.second < newTaskExpertMinGap:
		return fmt.Sprintf("narrow-gap best=%s score=%.2f gap=%.2f", id, rank.best, rank.best-rank.second)
	default:
		return "embed-unusable"
	}
}

// builtinExpertMatchHint is extra text in the built-in expert's embedding
// document. It is not a keyword scan of the command.
func builtinExpertMatchHint(id string) string {
	switch id {
	case "builtin-paper-polish":
		return "润色论文 学术语言修改 改摘要语法 polish the paper"
	case "builtin-paper-translate":
		return "论文翻译 中英学术互译 把论文译成英文 translate the paper"
	case "builtin-pptx-maker":
		return "制作PPT 演示文稿 幻灯片 make a slide deck"
	case builtinLatexExpertID:
		return "LaTeX 论文 tex 按模板写论文 write a latex paper"
	default:
		return ""
	}
}

// newTaskExpertDocVecCache keeps document vectors for the current embedder.
// A later create embeds only the command. Entries are keyed by the raw match
// text. The space is the embedder pointer, dimension, and model id, so a
// model change drops the map. The create deadline does not assign from a
// late embed, but a document batch that finishes after that deadline is
// still stored. A failed or short batch is not.
type expertDocVecCache struct {
	mu    sync.Mutex
	space string
	docs  map[string][]float32
}

var newTaskExpertDocVecCache expertDocVecCache

func resetNewTaskExpertDocVecCache() {
	newTaskExpertDocVecCache.mu.Lock()
	defer newTaskExpertDocVecCache.mu.Unlock()
	newTaskExpertDocVecCache.space = ""
	newTaskExpertDocVecCache.docs = nil
}

func expertEmbedderSpace(emb embedding.Embedder) string {
	model := ""
	if identified, ok := emb.(interface{ ModelID() string }); ok {
		model = identified.ModelID()
	}
	return fmt.Sprintf("%p|%d|%s", emb, emb.Dim(), model)
}

func (c *expertDocVecCache) take(space string, docs []string) ([][]float32, []bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.space != space || c.docs == nil {
		c.space = space
		c.docs = make(map[string][]float32)
	}
	out := make([][]float32, len(docs))
	hit := make([]bool, len(docs))
	for i, doc := range docs {
		vec, ok := c.docs[doc]
		if !ok || len(vec) == 0 {
			continue
		}
		out[i] = append([]float32(nil), vec...)
		hit[i] = true
	}
	return out, hit
}

func (c *expertDocVecCache) store(space string, docs []string, vecs [][]float32) {
	if len(docs) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.space != space || c.docs == nil {
		return
	}
	for i, doc := range docs {
		if i >= len(vecs) || len(vecs[i]) == 0 {
			continue
		}
		c.docs[doc] = append([]float32(nil), vecs[i]...)
	}
}

func embedNewTaskExpertVectors(emb embedding.Embedder, query string, docs []string, timeout time.Duration) ([]float32, [][]float32, embeddingRead) {
	if emb == nil || embedding.IsNoop(emb) || strings.TrimSpace(query) == "" || len(docs) == 0 {
		return nil, nil, embeddingUnavailable
	}
	if timeout <= 0 {
		timeout = newTaskTypeLocalTimeout
	}
	space := expertEmbedderSpace(emb)
	cached, hit := newTaskExpertDocVecCache.take(space, docs)
	missing := make([]string, 0, len(docs))
	missingAt := make([]int, 0, len(docs))
	for i, doc := range docs {
		if hit[i] {
			continue
		}
		missing = append(missing, doc)
		missingAt = append(missingAt, i)
	}
	type embedOutcome struct {
		query []float32
		docs  [][]float32
		err   error
	}
	ch := make(chan embedOutcome, 1)
	go func() {
		defer func() {
			if recover() != nil {
				select {
				case ch <- embedOutcome{err: fmt.Errorf("embedder panic")}:
				default:
				}
			}
		}()
		q, err := embedding.EmbedAs(emb, query, embedding.RoleQuery)
		if err != nil || len(q) == 0 {
			select {
			case ch <- embedOutcome{err: err}:
			default:
			}
			return
		}
		var vectors [][]float32
		if len(missing) > 0 {
			vectors, err = embedding.EmbedBatchAs(emb, missing, embedding.RoleDocument)
		}
		// Store before reporting. The caller may already have returned on
		// the deadline; the next create still reuses a finished batch.
		if err == nil && len(vectors) == len(missing) {
			storeDocs := make([]string, 0, len(missing))
			storeVecs := make([][]float32, 0, len(missing))
			for i, vec := range vectors {
				if len(vec) == 0 || len(vec) != len(q) {
					continue
				}
				storeDocs = append(storeDocs, missing[i])
				storeVecs = append(storeVecs, vec)
			}
			newTaskExpertDocVecCache.store(space, storeDocs, storeVecs)
		}
		select {
		case ch <- embedOutcome{query: q, docs: vectors, err: err}:
		default:
		}
	}()
	select {
	case got := <-ch:
		if got.err != nil || len(got.query) == 0 || len(got.docs) != len(missing) {
			return nil, nil, embeddingUnavailable
		}
		out := make([][]float32, len(docs))
		for i := range docs {
			if hit[i] {
				out[i] = cached[i]
			}
		}
		for i, vec := range got.docs {
			out[missingAt[i]] = vec
		}
		return got.query, out, embeddingReady
	case <-time.After(timeout):
		return nil, nil, embeddingTimeout
	}
}

func expertCosines(query []float32, docs [][]float32) []float64 {
	scores := make([]float64, len(docs))
	for i, doc := range docs {
		scores[i] = progress.CosineSimilarity(query, doc)
	}
	return scores
}

type expertScoreRank struct {
	idx    int
	best   float64
	second float64
}

func rankExpertScores(scores []float64) expertScoreRank {
	rank := expertScoreRank{idx: -1, best: -1, second: -1}
	for i, score := range scores {
		if !confidenceIsFinite(score) || score < 0 {
			continue
		}
		if score > rank.best {
			rank.second = rank.best
			rank.best = score
			rank.idx = i
			continue
		}
		if score > rank.second {
			rank.second = score
		}
	}
	return rank
}

// expertFromScores picks the nearest expert when its score clears the floor
// and the gap to the next expert is wide enough. A missing vector scores -1
// and is not a rival.
func expertFromScores(catalog []newTaskExpertCard, scores []float64) (newTaskExpertCard, bool) {
	if len(catalog) == 0 || len(scores) != len(catalog) {
		return newTaskExpertCard{}, false
	}
	rank := rankExpertScores(scores)
	if rank.idx < 0 || rank.best < newTaskExpertMinScore {
		return newTaskExpertCard{}, false
	}
	if rank.second >= 0 && rank.best-rank.second < newTaskExpertMinGap {
		return newTaskExpertCard{}, false
	}
	return catalog[rank.idx], true
}

// expertCatalogForLLM keeps the catalog short. When embedding scores exist,
// the stronger cards go first so a near miss is still visible to the model.
// Built-ins stay inside the cap: a long list of closer user experts must not
// hide the PPT or LaTeX expert from the only reader that can still assign it.
func expertCatalogForLLM(catalog []newTaskExpertCard, scores []float64) []newTaskExpertCard {
	if len(catalog) == 0 {
		return nil
	}
	ordered := catalog
	if len(scores) == len(catalog) {
		idx := make([]int, len(catalog))
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(i, j int) bool {
			return scores[idx[i]] > scores[idx[j]]
		})
		ordered = make([]newTaskExpertCard, len(catalog))
		for i, at := range idx {
			ordered[i] = catalog[at]
		}
	}
	if len(ordered) <= newTaskExpertLLMCap {
		return ordered
	}
	keep := make([]bool, len(ordered))
	builtinN := 0
	for i, card := range ordered {
		if !card.Builtin {
			continue
		}
		keep[i] = true
		builtinN++
	}
	if builtinN > newTaskExpertLLMCap {
		kept := 0
		for i, card := range ordered {
			if !card.Builtin {
				continue
			}
			if kept >= newTaskExpertLLMCap {
				keep[i] = false
				continue
			}
			kept++
		}
		builtinN = newTaskExpertLLMCap
	}
	room := newTaskExpertLLMCap - builtinN
	for i, card := range ordered {
		if card.Builtin || room <= 0 {
			continue
		}
		keep[i] = true
		room--
	}
	out := make([]newTaskExpertCard, 0, newTaskExpertLLMCap)
	for i, card := range ordered {
		if keep[i] {
			out = append(out, card)
		}
	}
	return out
}

func (a *App) askNewTaskExpertLLM(command string, catalog []newTaskExpertCard) (string, bool) {
	if a == nil || len(catalog) == 0 {
		return "", false
	}
	cfg := attachLightweightHubHint(a.GetMaclawLLMConfig(), llm.TaskIntent)
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return "", false
	}
	messages := []interface{}{
		map[string]string{"role": "system", "content": newTaskExpertSystemPrompt(catalog)},
		map[string]string{"role": "user", "content": truncateRunes(strings.TrimSpace(command), newTaskCommandMaxRunes)},
	}
	ctx, cancel := context.WithTimeout(context.Background(), newTaskTypeLLMTimeout)
	defer cancel()
	ctx = llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "new-task-expert"})
	client := &http.Client{Timeout: newTaskTypeLLMTimeout}
	resp, err := doSimpleLLMRequest(ctx, cfg, messages, client, newTaskTypeLLMTimeout)
	if err != nil || resp == nil {
		if err != nil {
			log.Printf("[task-expert] LLM classify failed: %v", err)
		}
		return "", false
	}
	text, ok := expertModelReply(resp.Content, resp.RawContent, resp.ReasoningContent, catalog)
	if !ok {
		return "", false
	}
	return text, true
}

func newTaskExpertSystemPrompt(catalog []newTaskExpertCard) string {
	var b strings.Builder
	b.WriteString(`你判断一条「新建任务」命令应该交给哪位已安装的专家。只输出 JSON：{"expert_id":"<专家 id 或 general>","confidence":0到1的小数}

只有命令明确属于某位专家的专业范围时才填写该专家的 id。问答、查资料、写普通邮件或纪要、改代码、解释报错，以及和这些专家职责无关的请求，都用 general。
只是话题相近、或你不确定时，用 general。不要输出解释。

可选专家：
`)
	for _, card := range catalog {
		fmt.Fprintf(&b, "- %s：%s。%s\n", card.ID, oneLineExpertText(card.Name), expertCardScopeLine(card))
	}
	return b.String()
}

// expertCardScopeLine is the scope the model judges. It is the description
// plus the built-in hint the embedder already uses. User experts have no hint.
func expertCardScopeLine(card newTaskExpertCard) string {
	desc := oneLineExpertText(card.Description)
	if desc == "" {
		desc = oneLineExpertText(card.Name)
	}
	hint := oneLineExpertText(builtinExpertMatchHint(card.ID))
	if hint == "" || strings.Contains(desc, hint) {
		return desc
	}
	if desc == "" {
		return hint
	}
	return desc + " " + hint
}

func oneLineExpertText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// expertModelReply picks the field that carries the decision. Display text
// wins when it names a card or explicitly declines. A think-tag filter can
// leave that text empty while the raw provider text or the reasoning field
// still holds the JSON; those are the decision too. An earlier explicit
// general is not replaced by a later card.
func expertModelReply(content, raw, reasoning string, catalog []newTaskExpertCard) (string, bool) {
	var fallback string
	for _, field := range []string{content, raw, reasoning} {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if fallback == "" {
			fallback = field
		}
		if expertReplyDeclines(field) {
			return field, true
		}
		if _, ok := parseNewTaskExpertReply(field, catalog); ok {
			return field, true
		}
	}
	if fallback == "" {
		return "", false
	}
	return fallback, true
}

func expertReplyDeclines(raw string) bool {
	obj, ok := readExpertReplyObject(raw)
	if !ok {
		return false
	}
	switch strings.ToLower(obj.id) {
	case "", "general", "generic", "none", "通用", "通用专家":
		return true
	default:
		return false
	}
}

type expertReplyObject struct {
	id         string
	confidence float64
}

func readExpertReplyObject(raw string) (expertReplyObject, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```JSON")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "```"))
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return expertReplyObject{}, false
	}
	var parsed struct {
		ExpertID   string          `json:"expert_id"`
		ExpertID2  string          `json:"expertId"`
		Confidence json.RawMessage `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return expertReplyObject{}, false
	}
	id := strings.TrimSpace(parsed.ExpertID)
	if id == "" {
		id = strings.TrimSpace(parsed.ExpertID2)
	}
	return expertReplyObject{id: id, confidence: parseNewTaskConfidence(parsed.Confidence)}, true
}

// parseNewTaskExpertReply accepts the JSON object. general, an unknown id,
// and a confidence outside the task-type floor all mean no expert.
func parseNewTaskExpertReply(raw string, catalog []newTaskExpertCard) (newTaskExpertCard, bool) {
	obj, ok := readExpertReplyObject(raw)
	if !ok {
		return newTaskExpertCard{}, false
	}
	switch strings.ToLower(obj.id) {
	case "", "general", "generic", "none", "通用", "通用专家":
		return newTaskExpertCard{}, false
	}
	confidence := obj.confidence
	// Some replies use a percent. 70 and 100 are that scale; 0.70 is already
	// a fraction and stays one. Anything above 100 is not a confidence.
	if confidence > 1 && confidence <= 100 {
		confidence = confidence / 100
	}
	if !newTaskLLMConfidenceOK(confidence) {
		return newTaskExpertCard{}, false
	}
	return findExpertCard(catalog, obj.id)
}

// findExpertCard resolves the model's answer onto a card it was shown.
// An exact id wins. Otherwise one case-insensitive id wins, then one
// name. A reply that repeats "id：name" is tried on each side. A reply
// that starts with one card's id or name and then continues with that
// card's scope still names that card. Two cards sharing a label are not
// a choice.
func findExpertCard(catalog []newTaskExpertCard, raw string) (newTaskExpertCard, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return newTaskExpertCard{}, false
	}
	if card, ok := matchExpertCard(catalog, raw); ok {
		return card, true
	}
	for _, sep := range []string{"：", ":"} {
		at := strings.Index(raw, sep)
		if at <= 0 {
			continue
		}
		if card, ok := matchExpertCard(catalog, raw[:at]); ok {
			return card, true
		}
		if card, ok := matchExpertCard(catalog, raw[at+len(sep):]); ok {
			return card, true
		}
	}
	return newTaskExpertCard{}, false
}

func matchExpertCard(catalog []newTaskExpertCard, label string) (newTaskExpertCard, bool) {
	label = strings.TrimSpace(label)
	if label == "" {
		return newTaskExpertCard{}, false
	}
	var folded, named newTaskExpertCard
	foldedN, namedN := 0, 0
	for _, card := range catalog {
		if card.ID == label {
			return card, true
		}
		if strings.EqualFold(card.ID, label) {
			folded = card
			foldedN++
		}
		if sameExpertLabel(card.Name, label) {
			named = card
			namedN++
		}
	}
	if foldedN == 1 {
		return folded, true
	}
	if namedN == 1 {
		return named, true
	}
	var prefixed newTaskExpertCard
	prefixN := 0
	for _, card := range catalog {
		if expertLabelNamesCard(label, card) {
			prefixed = card
			prefixN++
		}
	}
	if prefixN == 1 {
		return prefixed, true
	}
	return newTaskExpertCard{}, false
}

// expertLabelNamesCard reports whether label starts with this card's id or
// name and the next character ends that label. Echoing "名称。职责说明" is
// that card. A longer name that merely continues the letters is not.
func expertLabelNamesCard(label string, card newTaskExpertCard) bool {
	label = strings.TrimSpace(label)
	if label == "" {
		return false
	}
	if id := strings.TrimSpace(card.ID); id != "" && len(label) > len(id) && strings.EqualFold(label[:len(id)], id) {
		if !expertIDContinue(label[len(id):]) {
			return true
		}
	}
	name := strings.TrimSpace(card.Name)
	return name != "" && expertNameContinues(label, name)
}

func expertIDContinue(rest string) bool {
	r, _ := utf8.DecodeRuneInString(rest)
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
}

// expertNameContinues is true when name is a strict prefix of label and the
// following rune is punctuation or a symbol. Spaces inside either side are
// ignored. An exact name is handled separately.
func expertNameContinues(label, name string) bool {
	li, ni := 0, 0
	for {
		li = skipExpertLabelSpace(label, li)
		ni = skipExpertLabelSpace(name, ni)
		if ni >= len(name) {
			if li >= len(label) {
				return false
			}
			r, _ := utf8.DecodeRuneInString(label[li:])
			return unicode.IsPunct(r) || unicode.IsSymbol(r)
		}
		if li >= len(label) {
			return false
		}
		lr, ls := utf8.DecodeRuneInString(label[li:])
		nr, ns := utf8.DecodeRuneInString(name[ni:])
		if ls <= 0 || ns <= 0 || !strings.EqualFold(string(lr), string(nr)) {
			return false
		}
		li += ls
		ni += ns
	}
}

func skipExpertLabelSpace(text string, at int) int {
	for at < len(text) {
		r, size := utf8.DecodeRuneInString(text[at:])
		if size <= 0 || !unicode.IsSpace(r) {
			return at
		}
		at += size
	}
	return at
}

func sameExpertLabel(got, want string) bool {
	got = strings.Join(strings.Fields(got), "")
	want = strings.Join(strings.Fields(want), "")
	return got != "" && strings.EqualFold(got, want)
}

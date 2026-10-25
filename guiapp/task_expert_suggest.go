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
	// A command has to sit clearly nearer one expert than the others. A close
	// score goes to the LLM. A score under newTaskExpertAskMinScore, or an
	// embedding timeout, stays on 通用.
	newTaskExpertMinScore = 0.72
	newTaskExpertMinGap   = 0.08
	// Below this, embedding already says the command is not near any expert,
	// so the LLM is not asked.
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
// MatchText is what the embedder sees. The LLM sees the name and description.
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
	}
	return suggestion
}

type taskExpertSuggestDeps struct {
	embed  func(query string, docs []string) ([]float32, [][]float32, embeddingRead)
	askLLM func(command string, catalog []newTaskExpertCard) (string, bool)
}

func (a *App) taskExpertSuggestDeps() taskExpertSuggestDeps {
	return taskExpertSuggestDeps{
		embed: func(query string, docs []string) ([]float32, [][]float32, embeddingRead) {
			if a == nil {
				return nil, nil, embeddingUnavailable
			}
			return embedNewTaskExpertVectors(a.intentEmbedder, query, docs, newTaskTypeLocalTimeout)
		},
		askLLM: func(command string, catalog []newTaskExpertCard) (string, bool) {
			return a.askNewTaskExpertLLM(command, catalog)
		},
	}
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
	if deps.askLLM != nil && expertShouldAskLLM(read, scores) {
		narrowed := expertCatalogForLLM(embedded, scores)
		if raw, ok := deps.askLLM(command, narrowed); ok {
			if card, ok := parseNewTaskExpertReply(raw, narrowed); ok {
				return expertSuggestion(card, "llm")
			}
		}
	}
	return NewTaskExpertSuggestion{}
}

// expertShouldAskLLM is true only when the LLM is the remaining signal: the
// embedder is unavailable, its vectors are unusable, or the best score is a
// near miss. A timeout leaves the embedder busy, and a low score already says
// the command is not near any expert, so both stay on 通用.
func expertShouldAskLLM(read embeddingRead, scores []float64) bool {
	if read == embeddingTimeout {
		return false
	}
	if read != embeddingReady {
		return true
	}
	best := -1.0
	for _, score := range scores {
		if !confidenceIsFinite(score) || score < 0 {
			continue
		}
		if score > best {
			best = score
		}
	}
	if best < 0 {
		return true
	}
	return best >= newTaskExpertAskMinScore
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

// builtinExpertMatchHint adds a few purpose phrases to the embedded text of
// a built-in expert. The decision is still the cosine, not a keyword hit.
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
// model change drops the map. A timed-out call does not write this cache.
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
		storeDocs := make([]string, 0, len(missing))
		storeVecs := make([][]float32, 0, len(missing))
		for i, vec := range got.docs {
			out[missingAt[i]] = vec
			if len(vec) == 0 || len(vec) != len(got.query) {
				continue
			}
			storeDocs = append(storeDocs, missing[i])
			storeVecs = append(storeVecs, vec)
		}
		newTaskExpertDocVecCache.store(space, storeDocs, storeVecs)
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

// expertFromScores picks the nearest expert when its score clears the floor
// and the gap to the next expert is wide enough. A missing vector scores -1
// and is not a rival.
func expertFromScores(catalog []newTaskExpertCard, scores []float64) (newTaskExpertCard, bool) {
	if len(catalog) == 0 || len(scores) != len(catalog) {
		return newTaskExpertCard{}, false
	}
	bestIdx := -1
	best := -1.0
	second := -1.0
	for i, score := range scores {
		if !confidenceIsFinite(score) || score < 0 {
			continue
		}
		if score > best {
			second = best
			best = score
			bestIdx = i
			continue
		}
		if score > second {
			second = score
		}
	}
	if bestIdx < 0 || best < newTaskExpertMinScore {
		return newTaskExpertCard{}, false
	}
	if second >= 0 && best-second < newTaskExpertMinGap {
		return newTaskExpertCard{}, false
	}
	return catalog[bestIdx], true
}

// expertCatalogForLLM keeps the catalog short. When embedding scores exist,
// the stronger cards go first so a near miss is still visible to the model.
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
	if len(ordered) > newTaskExpertLLMCap {
		ordered = ordered[:newTaskExpertLLMCap]
	}
	return ordered
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
	if err != nil || resp == nil || strings.TrimSpace(resp.Content) == "" {
		if err != nil {
			log.Printf("[task-expert] LLM classify failed: %v", err)
		}
		return "", false
	}
	return resp.Content, true
}

func newTaskExpertSystemPrompt(catalog []newTaskExpertCard) string {
	var b strings.Builder
	b.WriteString(`你判断一条「新建任务」命令应该交给哪位已安装的专家。只输出 JSON：{"expert_id":"<专家 id 或 general>","confidence":0到1的小数}

只有命令明确属于某位专家的专业范围时才填写该专家的 id。问答、查资料、写普通邮件或纪要、改代码、解释报错，以及和这些专家职责无关的请求，都用 general。
只是话题相近、或你不确定时，用 general。不要输出解释。

可选专家：
`)
	for _, card := range catalog {
		desc := oneLineExpertText(card.Description)
		if desc == "" {
			desc = oneLineExpertText(card.Name)
		}
		fmt.Fprintf(&b, "- %s：%s。%s\n", card.ID, oneLineExpertText(card.Name), desc)
	}
	return b.String()
}

func oneLineExpertText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// parseNewTaskExpertReply accepts the JSON object. general, an unknown id,
// and a confidence outside the task-type floor all mean no expert.
func parseNewTaskExpertReply(raw string, catalog []newTaskExpertCard) (newTaskExpertCard, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```JSON")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "```"))
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return newTaskExpertCard{}, false
	}
	var parsed struct {
		ExpertID   string          `json:"expert_id"`
		ExpertID2  string          `json:"expertId"`
		Confidence json.RawMessage `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return newTaskExpertCard{}, false
	}
	id := strings.TrimSpace(parsed.ExpertID)
	if id == "" {
		id = strings.TrimSpace(parsed.ExpertID2)
	}
	switch strings.ToLower(id) {
	case "", "general", "generic", "none", "通用", "通用专家":
		return newTaskExpertCard{}, false
	}
	if !newTaskLLMConfidenceOK(parseNewTaskConfidence(parsed.Confidence)) {
		return newTaskExpertCard{}, false
	}
	return findExpertCard(catalog, id)
}

func findExpertCard(catalog []newTaskExpertCard, id string) (newTaskExpertCard, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return newTaskExpertCard{}, false
	}
	var folded newTaskExpertCard
	foldedCount := 0
	for _, card := range catalog {
		if card.ID == id {
			return card, true
		}
		if strings.EqualFold(card.ID, id) {
			folded = card
			foldedCount++
		}
	}
	if foldedCount == 1 {
		return folded, true
	}
	return newTaskExpertCard{}, false
}

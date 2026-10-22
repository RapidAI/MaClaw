package guiapp

import (
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// A learned recipe is admitted from its description vector when the user
// restates the task in different words. Keyword overlap misses that, and a
// loose cosine would bind an unrelated skill, which the host then forces the
// model to run. The margin is cosine above the model family's baseline.
// A different recipe within the gap is ambiguous even when it is under the
// margin, and neither is forced. The same description is one recipe.
//
// Qwen3 with prompts uses an instructed query against a plain description.
// RoleSimilarity on both sides was measured on 2026-10-22 (Qwen3-Embedding-0.6B
// Q8): paraphrases started at 0.866 and different tasks reached 0.859, and
// "设计并实现一个愤怒的小鸟游戏" against "加固 php.ini 并重启服务" scored 0.727.
// A 0.50+0.22 bar (raw 0.72) force-binds that pair. The skill instruction
// below, against a plain description, separated the same set by 0.131
// (paraphrases 0.729–0.869, different tasks at most 0.597, including
// "工作区里现在有哪些文件" against a copy-into-workspace recipe at 0.593).
// 0.44+0.22 accepts raw cosine at or above 0.66, inside that gap.
// The instruction text is part of the measured space; changing it requires
// a new measurement and a new doc-space suffix.
//
// The stored description of a learned skill is often the host coding-turn
// wrapper. The vector is the task inside that wrapper, the same cut
// DraftSkill already uses. That task is also the tool text, including a
// task too short to embed. A market or hand-written description is published
// as stored. The cache key is the embedded task text, so this strip does
// not change the doc-space suffix.
//
// BuildToolsForModelRequest republishes admission on every model iteration.
// Description vectors and the query vector stay in the process cache, so the
// same task is not embedded again. Query vectors do not share the description
// map: that map's length is what decides a full re-embed, and a query must
// not spend one of those slots.
const (
	skillAdmissionVectorMargin             = 0.22
	skillAdmissionVectorGap                = 0.04
	skillAdmissionVecCacheMax              = 512
	skillAdmissionQueryCacheMax            = 8
	skillAdmissionVectorBaseline           = 0.20
	skillAdmissionVectorBaselineInstructed = 0.44
	skillAdmissionQueryInstruction         = "Given a user request, retrieve a skill description that performs the same task"
	skillAdmissionDocSpace                 = "skill-doc-v1"
	// skillAdmissionQueryKeyPrefix marks a query in its own map. A task can
	// repeat a description word for word, and the similarity space embeds
	// that task as the raw text.
	skillAdmissionQueryKeyPrefix = "\x00q\x00"
)

var skillAdmissionVecCache = struct {
	mu      sync.Mutex
	stamp   string
	vecs    map[string][]float32
	queries map[string][]float32
}{vecs: map[string][]float32{}, queries: map[string][]float32{}}

func admitLearnedSkillByVector(exec *SkillExecutor, userText, domain string, emb embedding.Embedder) *admittedSkillBinding {
	if exec == nil || emb == nil || embedding.IsNoop(emb) {
		return nil
	}
	userText = strings.TrimSpace(userText)
	if len([]rune(userText)) < 4 {
		return nil
	}
	// Prompt-disabled Qwen3 has no measured accept region. A keyword miss stays
	// a miss instead of force-calling whatever sits near a shared prefix.
	if embedding.EmbedderFamily(emb) == embedding.FamilyQwen3 && !skillAdmissionInstructed(emb) {
		return nil
	}
	agentDomain := corelib.NormalizeSkillExperienceDomain(domain)
	var cands []corelib.NLSkillEntry
	var docs []string
	for _, entry := range exec.loadSkills() {
		if strings.TrimSpace(entry.Name) == "" || strings.TrimSpace(entry.Description) == "" {
			continue
		}
		// Market and hand-written skills stay on name and trigger identity.
		// A learned recipe with a session path still in its steps is not
		// runnable; skillCompleteForAdmission rejects it below.
		if !learnedSkillSource(entry.Source) {
			continue
		}
		view := admissionSkillView(entry)
		if agentDomain != corelib.SkillDomainUniversal && !corelib.SkillVisibleInExperienceDomain(view.ExperienceDomain, agentDomain) {
			continue
		}
		guided, runnable := skillCompleteForAdmission(entry)
		if guided || !runnable {
			continue
		}
		// The stored description is often the host coding-turn wrapper.
		// The vector is the task DraftSkill already treats as the skill's
		// identity. A wrapper with no task, or a task under 4 runes, is
		// not a recipe.
		doc := skillAdmissionDocText(view.Description)
		if doc == "" {
			continue
		}
		cands = append(cands, entry)
		docs = append(docs, doc)
	}
	if len(cands) == 0 {
		return nil
	}
	// Qwen3: instructed query, plain description, one EmbedBatch. Other
	// embedders stay on RoleSimilarity (the unit-test space). The coding
	// retrieval prompt is not used; it asks for programming tools.
	query, vectors, ok := embedSkillAdmissionVectors(emb, userText, docs)
	if !ok || len(query) == 0 || len(vectors) != len(cands) {
		return nil
	}
	baseline := skillAdmissionVectorBaselineFor(emb)
	type scored struct {
		idx int
		raw float64
	}
	ranked := make([]scored, len(vectors))
	for i, vec := range vectors {
		ranked[i] = scored{idx: i, raw: float64(cosine32(query, vec))}
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].raw != ranked[j].raw {
			return ranked[i].raw > ranked[j].raw
		}
		return ranked[i].idx < ranked[j].idx
	})
	best := ranked[0]
	if best.raw-baseline < skillAdmissionVectorMargin {
		return nil
	}
	// The runner-up counts even when it sits under the accept line. A winner
	// at 0.67 and a different recipe at 0.65 are the same ambiguity as two
	// winners. Identical task texts are one recipe saved twice.
	for _, other := range ranked[1:] {
		if best.raw-other.raw >= skillAdmissionVectorGap {
			break
		}
		if docs[other.idx] != docs[best.idx] {
			log.Printf("[skill-admission] vector ambiguous skill=%s cosine=%.3f other=%s cosine=%.3f",
				cands[best.idx].Name, best.raw, cands[other.idx].Name, other.raw)
			return nil
		}
	}
	entry := cands[best.idx]
	log.Printf("[skill-admission] vector hit skill=%s cosine=%.3f", entry.Name, best.raw)
	binding := admittedBindingForEntry(entry)
	return &binding
}

// skillAdmissionDocText is the task inside a recorded host prompt. A plain
// description is unchanged. Scaffolding alone, and a task shorter than 4
// runes, are not embedded.
func skillAdmissionDocText(description string) string {
	task := strings.TrimSpace(learnedSkillTaskFromHostPrompt(description))
	if len([]rune(task)) < 4 {
		return ""
	}
	return task
}

// skillAdmissionPublishedDescription is the task the model is told to run.
// Only a learned skill is cut: its stored text is often the host wrapper.
// A non-empty task is the tool text even when it is too short to embed.
// A market or hand-written description is published as stored. Scaffolding
// with no task keeps the stored text.
func skillAdmissionPublishedDescription(source, description string) string {
	raw := strings.TrimSpace(description)
	if !learnedSkillSource(source) {
		return raw
	}
	task := strings.TrimSpace(learnedSkillTaskFromHostPrompt(raw))
	if task == "" {
		return raw
	}
	return task
}

func skillAdmissionInstructed(emb embedding.Embedder) bool {
	if emb == nil || !embedding.PromptsEnabled() || embedding.EmbedderFamily(emb) != embedding.FamilyQwen3 {
		return false
	}
	// RoleEmbedder means plain Embed does not add its own instruction, so the
	// query string below is the whole prompt. Qwen3Embedder works this way.
	_, ok := emb.(embedding.RoleEmbedder)
	return ok
}

func skillAdmissionQueryText(userText string) string {
	return "Instruct: " + skillAdmissionQueryInstruction + "\nQuery: " + userText
}

func skillAdmissionQueryCacheKey(embedded string) string {
	return skillAdmissionQueryKeyPrefix + embedded
}

func skillAdmissionVectorBaselineFor(emb embedding.Embedder) float64 {
	if skillAdmissionInstructed(emb) {
		return skillAdmissionVectorBaselineInstructed
	}
	return skillAdmissionVectorBaseline
}

func admittedBindingForEntry(entry corelib.NLSkillEntry) admittedSkillBinding {
	return admittedSkillBinding{
		Alias:         skillAdmissionAlias(entry, map[string]bool{}),
		Name:          strings.TrimSpace(entry.Name),
		StableID:      agentservice.DynamicSkillStableID(entry),
		Version:       strings.TrimSpace(entry.Version),
		ContentDigest: agentservice.DynamicSkillContentDigest(entry),
		Description:   skillAdmissionPublishedDescription(entry.Source, entry.Description),
		Params:        skill.CompleteParamsForRunner(entry.Params, entry.Steps, entry.RequiredArgs),
	}
}

func resetSkillAdmissionVecCache() {
	skillAdmissionVecCache.mu.Lock()
	skillAdmissionVecCache.stamp = ""
	skillAdmissionVecCache.vecs = map[string][]float32{}
	skillAdmissionVecCache.queries = map[string][]float32{}
	skillAdmissionVecCache.mu.Unlock()
}

// skillAdmissionVecStamp identifies the vector space. A Qwen3 ModelID already
// includes dimension, prompt regime, and the instruct recipe. Test doubles
// fall back to family, dimension, and SpaceRecipe. The suffix separates plain
// skill descriptions from RoleSimilarity vectors so the two are never mixed.
func skillAdmissionVecStamp(emb embedding.Embedder) string {
	space := embedding.EmbedderFamily(emb) + ":" + strconv.Itoa(emb.Dim()) + ":" + embedding.SpaceRecipe()
	if m, ok := emb.(interface{ ModelID() string }); ok {
		if mid := strings.TrimSpace(m.ModelID()); mid != "" {
			space = mid
		}
	}
	if skillAdmissionInstructed(emb) {
		return space + ":" + skillAdmissionDocSpace
	}
	return space + ":similarity"
}

// embedSkillAdmissionVectors embeds userText together with every doc the
// cache does not already hold. The query is cached under its own key, so a
// later admission of the same task embeds nothing. An empty userText is the
// description-only path and is not cached as a query. The returned slices
// belong to this call: a later cache swap cannot drop them. Publishing into
// the cache is best-effort.
func embedSkillAdmissionVectors(emb embedding.Embedder, userText string, docs []string) ([]float32, [][]float32, bool) {
	if len(docs) == 0 {
		return nil, nil, false
	}
	stamp := skillAdmissionVecStamp(emb)
	have, missing, rebuild := skillAdmissionVecPrepare(stamp, docs)

	includeQuery := userText != ""
	instructed := skillAdmissionInstructed(emb)
	embeddedQuery := userText
	if instructed {
		embeddedQuery = skillAdmissionQueryText(userText)
	}
	var cachedQuery []float32
	queryKey := ""
	if includeQuery {
		queryKey = skillAdmissionQueryCacheKey(embeddedQuery)
		// After prepare, so a stamp change has already dropped the old space.
		if vec, ok := skillAdmissionVecQuerySnapshot(stamp, queryKey); ok {
			cachedQuery = vec
		}
	}

	batch := make([]string, 0, len(missing)+1)
	if includeQuery && len(cachedQuery) == 0 {
		batch = append(batch, embeddedQuery)
	}
	for _, text := range missing {
		// In the similarity space the query and a same-text description are one
		// vector. The instructed query is a different string from the plain
		// description, including when the user repeats the description verbatim.
		if !instructed && includeQuery && text == userText {
			continue
		}
		batch = append(batch, text)
	}

	var fresh [][]float32
	if len(batch) > 0 {
		var err error
		if instructed {
			fresh, err = emb.EmbedBatch(batch)
		} else {
			fresh, err = embedding.EmbedBatchAs(emb, batch, embedding.RoleSimilarity)
		}
		if err != nil || len(fresh) != len(batch) {
			return nil, nil, false
		}
		for _, vec := range fresh {
			if len(vec) == 0 {
				return nil, nil, false
			}
		}
	}

	byText := make(map[string][]float32, len(missing))
	var query []float32
	queryFromBatch := includeQuery && len(cachedQuery) == 0
	if queryFromBatch {
		if len(fresh) == 0 {
			return nil, nil, false
		}
		query = fresh[0]
		fresh = fresh[1:]
	} else if includeQuery {
		query = cachedQuery
	}
	fi := 0
	for _, text := range missing {
		if !instructed && includeQuery && text == userText {
			byText[text] = query
			continue
		}
		if fi >= len(fresh) {
			return nil, nil, false
		}
		byText[text] = fresh[fi]
		fi++
	}
	if fi != len(fresh) {
		return nil, nil, false
	}

	out := make([][]float32, len(docs))
	storeTexts := make([]string, 0, len(missing))
	storeVecs := make([][]float32, 0, len(missing))
	stored := make(map[string]struct{}, len(missing))
	for i, text := range docs {
		if vec := have[i]; len(vec) > 0 {
			out[i] = vec
			continue
		}
		vec, ok := byText[text]
		if !ok || len(vec) == 0 {
			return nil, nil, false
		}
		out[i] = vec
		if _, dup := stored[text]; dup {
			continue
		}
		stored[text] = struct{}{}
		storeTexts = append(storeTexts, text)
		storeVecs = append(storeVecs, vec)
	}
	if len(storeTexts) > 0 {
		skillAdmissionVecStore(stamp, storeTexts, storeVecs, rebuild)
	}
	// Descriptions and queries are separate maps. A description rebuild must
	// not drop the query, and a query must not occupy a description slot.
	if includeQuery && len(query) > 0 && len(cachedQuery) == 0 {
		skillAdmissionVecQueryStore(stamp, queryKey, query)
	}
	return query, out, true
}

func cachedSkillAdmissionVectors(emb embedding.Embedder, texts []string) ([][]float32, bool) {
	_, vectors, ok := embedSkillAdmissionVectors(emb, "", texts)
	return vectors, ok
}

// skillAdmissionVecPrepare reports cached vectors for texts and the texts
// that still need an embed. When the cache cannot hold the new texts it sets
// rebuild and lists every text in this request, but it does not clear the
// map. Concurrent readers keep the old map until skillAdmissionVecStore swaps
// in the freshly embedded texts. A stamp change drops the old space
// immediately: those vectors are not comparable.
func skillAdmissionVecPrepare(stamp string, texts []string) (have [][]float32, missing []string, rebuild bool) {
	skillAdmissionVecCache.mu.Lock()
	defer skillAdmissionVecCache.mu.Unlock()
	if skillAdmissionVecCache.stamp != stamp {
		skillAdmissionVecCache.stamp = stamp
		skillAdmissionVecCache.vecs = map[string][]float32{}
		skillAdmissionVecCache.queries = map[string][]float32{}
	}
	have = make([][]float32, len(texts))
	seen := make(map[string]struct{}, len(texts))
	for i, text := range texts {
		if vec, ok := skillAdmissionVecCache.vecs[text]; ok && len(vec) > 0 {
			have[i] = vec
		}
		if _, dup := seen[text]; dup {
			continue
		}
		seen[text] = struct{}{}
		if have[i] == nil {
			missing = append(missing, text)
		}
	}
	if len(missing) > 0 && len(skillAdmissionVecCache.vecs)+len(missing) > skillAdmissionVecCacheMax {
		rebuild = true
		missing = uniqueSkillAdmissionTexts(texts, nil)
		for i := range have {
			have[i] = nil
		}
	}
	return have, missing, rebuild
}

func uniqueSkillAdmissionTexts(texts []string, have map[string][]float32) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, text := range texts {
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		if have != nil {
			if _, ok := have[text]; ok {
				continue
			}
		}
		out = append(out, text)
	}
	return out
}

func skillAdmissionVecStore(stamp string, texts []string, vecs [][]float32, rebuild bool) bool {
	if len(texts) != len(vecs) {
		return false
	}
	copied := make([][]float32, len(vecs))
	for i, vec := range vecs {
		if len(vec) == 0 {
			return false
		}
		copied[i] = append([]float32(nil), vec...)
	}
	skillAdmissionVecCache.mu.Lock()
	defer skillAdmissionVecCache.mu.Unlock()
	if skillAdmissionVecCache.stamp != stamp {
		return false
	}
	if rebuild {
		next := make(map[string][]float32, len(texts))
		for i, text := range texts {
			next[text] = copied[i]
		}
		skillAdmissionVecCache.vecs = next
		return true
	}
	for i, text := range texts {
		skillAdmissionVecCache.vecs[text] = copied[i]
	}
	return true
}

func skillAdmissionVecQuerySnapshot(stamp, key string) ([]float32, bool) {
	skillAdmissionVecCache.mu.Lock()
	defer skillAdmissionVecCache.mu.Unlock()
	if skillAdmissionVecCache.stamp != stamp || key == "" {
		return nil, false
	}
	vec, ok := skillAdmissionVecCache.queries[key]
	if !ok || len(vec) == 0 {
		return nil, false
	}
	return vec, true
}

// skillAdmissionVecQueryStore keeps the query vector next to descriptions
// without counting it as one. The cap drops earlier queries. The query just
// stored stays, so the task now being admitted is the one a tool rebuild
// reuses. A stamp change between embed and store leaves this call's vector
// in the caller's hands and skips the publish.
func skillAdmissionVecQueryStore(stamp, key string, vec []float32) bool {
	if key == "" || len(vec) == 0 {
		return false
	}
	copied := append([]float32(nil), vec...)
	skillAdmissionVecCache.mu.Lock()
	defer skillAdmissionVecCache.mu.Unlock()
	if skillAdmissionVecCache.stamp != stamp {
		return false
	}
	if skillAdmissionVecCache.queries == nil {
		skillAdmissionVecCache.queries = map[string][]float32{}
	}
	skillAdmissionVecCache.queries[key] = copied
	if len(skillAdmissionVecCache.queries) <= skillAdmissionQueryCacheMax {
		return true
	}
	drop := make([]string, 0, len(skillAdmissionVecCache.queries)-skillAdmissionQueryCacheMax)
	for old := range skillAdmissionVecCache.queries {
		if old == key {
			continue
		}
		drop = append(drop, old)
	}
	for _, old := range drop {
		if len(skillAdmissionVecCache.queries) <= skillAdmissionQueryCacheMax {
			break
		}
		delete(skillAdmissionVecCache.queries, old)
	}
	return true
}

func skillAdmissionVecSnapshot(stamp string, texts []string) ([][]float32, bool) {
	skillAdmissionVecCache.mu.Lock()
	defer skillAdmissionVecCache.mu.Unlock()
	if skillAdmissionVecCache.stamp != stamp {
		return nil, false
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		vec, ok := skillAdmissionVecCache.vecs[text]
		if !ok || len(vec) == 0 {
			return nil, false
		}
		out[i] = vec
	}
	return out, true
}

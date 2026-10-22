package guiapp

import (
	"fmt"
	"math"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

type skillAdmissionTestEmbedder struct {
	vecs map[string][]float32
}

func (e skillAdmissionTestEmbedder) Embed(text string) ([]float32, error) {
	if vec, ok := e.vecs[text]; ok {
		return append([]float32(nil), vec...), nil
	}
	return []float32{0, 1}, nil
}

func (e skillAdmissionTestEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(text)
		if err != nil {
			return nil, err
		}
		out[i] = vec
	}
	return out, nil
}

func (skillAdmissionTestEmbedder) Dim() int { return 2 }
func (skillAdmissionTestEmbedder) Close()   {}

type similaritySkillEmbedder struct {
	skillAdmissionTestEmbedder
	roles []embedding.Role
}

func (e *similaritySkillEmbedder) EmbedWithRole(text string, role embedding.Role) ([]float32, error) {
	e.roles = append(e.roles, role)
	return e.skillAdmissionTestEmbedder.Embed(text)
}

func (e *similaritySkillEmbedder) EmbedBatchWithRole(texts []string, role embedding.Role) ([][]float32, error) {
	e.roles = append(e.roles, role)
	return e.skillAdmissionTestEmbedder.EmbedBatch(texts)
}

type instructedAdmissionEmbedder struct {
	vecs    map[string][]float32
	batches [][]string
}

func (e *instructedAdmissionEmbedder) Embed(text string) ([]float32, error) {
	if vec, ok := e.vecs[text]; ok {
		return append([]float32(nil), vec...), nil
	}
	return []float32{0, 1}, nil
}

func (e *instructedAdmissionEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	e.batches = append(e.batches, append([]string(nil), texts...))
	out := make([][]float32, len(texts))
	for i, text := range texts {
		vec, err := e.Embed(text)
		if err != nil {
			return nil, err
		}
		out[i] = vec
	}
	return out, nil
}

func (e *instructedAdmissionEmbedder) EmbedWithRole(text string, role embedding.Role) ([]float32, error) {
	return e.Embed(text)
}

func (e *instructedAdmissionEmbedder) EmbedBatchWithRole(texts []string, role embedding.Role) ([][]float32, error) {
	return e.EmbedBatch(texts)
}

func (instructedAdmissionEmbedder) Dim() int { return 2 }
func (instructedAdmissionEmbedder) Close()   {}
func (instructedAdmissionEmbedder) ModelID() string {
	return "qwen3-test:2:prompt-v1:qwen3-instruct-v1"
}

func copyToWorkspaceLearnedSkill() corelib.NLSkillEntry {
	return learnedRecipe("craft_copy_into_workspace", "把指定文件复制到当前工作区")
}

func learnedRecipe(name, description string) corelib.NLSkillEntry {
	return corelib.NLSkillEntry{
		Name:        name,
		Status:      "active",
		Source:      "learned",
		Description: description,
		Triggers:    []string{description},
		Steps:       []corelib.NLSkillStep{{Action: "bash", Params: map[string]interface{}{"command": "Copy-Item -Path $src -Destination $dst"}}},
	}
}

// vecAtCosine is a vector whose cosine with {1, 0} is c.
func vecAtCosine(c float64) []float32 {
	return []float32{1, float32(math.Sqrt(1/(c*c) - 1))}
}

func TestSkillAdmissionVectorRecallsAParaphrase(t *testing.T) {
	resetSkillAdmissionVecCache()
	const (
		desc = "把指定文件复制到当前工作区"
		para = "帮我把那份手册拷进工作目录"
	)
	app, _ := newSkillExecutorWithEntries(t, copyToWorkspaceLearnedSkill())
	h := &IMMessageHandler{app: app}
	h.interruptHandler = newIMInterruptHandler(h)
	emb := &similaritySkillEmbedder{skillAdmissionTestEmbedder: skillAdmissionTestEmbedder{vecs: map[string][]float32{
		desc: {1, 0},
		para: {1, 0},
	}}}
	h.interruptHandler.SetEmbedder(emb)
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), para)
	if decision.Mode != skillAdmissionBound || len(decision.Bound) != 1 || decision.Bound[0].Name != "craft_copy_into_workspace" {
		t.Fatalf("paraphrase decision=%#v", decision)
	}
	again := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), para)
	if again.Mode != skillAdmissionBound || len(again.Bound) != 1 {
		t.Fatalf("warm paraphrase decision=%#v", again)
	}
	// Cold cache embeds the query and the description once. The next admission
	// of the same task reuses both vectors.
	if len(emb.roles) != 1 || emb.roles[0] != embedding.RoleSimilarity {
		t.Fatalf("roles = %v, want one sentence-similarity batch", emb.roles)
	}
}

func TestSkillAdmissionVectorUsesInstructedQueryForQwen3(t *testing.T) {
	resetSkillAdmissionVecCache()
	prev := embedding.PromptsEnabled()
	embedding.SetPromptsEnabled(true)
	t.Cleanup(func() { embedding.SetPromptsEnabled(prev) })

	const (
		desc  = "把指定文件复制到当前工作区"
		para  = "帮我把那份手册拷进工作目录"
		other = "设计并实现一个愤怒的小鸟游戏"
	)
	query := skillAdmissionQueryText(para)
	app, _ := newSkillExecutorWithEntries(t, copyToWorkspaceLearnedSkill())
	h := &IMMessageHandler{app: app}
	h.interruptHandler = newIMInterruptHandler(h)
	emb := &instructedAdmissionEmbedder{vecs: map[string][]float32{
		query:                          {1, 0},
		skillAdmissionQueryText(other): {0, 1},
		desc:                           {1, 0},
	}}
	h.interruptHandler.SetEmbedder(emb)

	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), para)
	if decision.Mode != skillAdmissionBound || len(decision.Bound) != 1 || decision.Bound[0].Name != "craft_copy_into_workspace" {
		t.Fatalf("instructed paraphrase decision=%#v", decision)
	}
	if len(emb.batches) != 1 || len(emb.batches[0]) != 2 || emb.batches[0][0] != query || emb.batches[0][1] != desc {
		t.Fatalf("cold batch = %#v, want instructed query then plain description", emb.batches)
	}

	again := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), para)
	if again.Mode != skillAdmissionBound || len(again.Bound) != 1 {
		t.Fatalf("warm instructed decision=%#v", again)
	}
	if len(emb.batches) != 1 {
		t.Fatalf("warm batches = %#v, want no second embed", emb.batches)
	}

	unrelated := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), other)
	if unrelated.Mode == skillAdmissionBound {
		t.Fatalf("unrelated instructed task bound %#v", unrelated.Bound)
	}
}

func TestSkillAdmissionVectorEmbedsTheTaskNotTheHostBanner(t *testing.T) {
	resetSkillAdmissionVecCache()
	prev := embedding.PromptsEnabled()
	embedding.SetPromptsEnabled(true)
	t.Cleanup(func() { embedding.SetPromptsEnabled(prev) })

	const (
		stored = "请执行以下编码任务： ## T3: Add runVisualPolishChecks self-test"
		para   = "给视觉打磨补一个自测"
	)
	task := learnedSkillTaskFromHostPrompt(stored)
	if task != "Add runVisualPolishChecks self-test" {
		t.Fatalf("task = %q", task)
	}
	query := skillAdmissionQueryText(para)
	app, _ := newSkillExecutorWithEntries(t, learnedRecipe("craft_visual_polish_selftest", stored))
	h := &IMMessageHandler{app: app}
	h.interruptHandler = newIMInterruptHandler(h)
	emb := &instructedAdmissionEmbedder{vecs: map[string][]float32{
		query: {1, 0},
		task:  {1, 0},
	}}
	h.interruptHandler.SetEmbedder(emb)

	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), para)
	if decision.Mode != skillAdmissionBound || len(decision.Bound) != 1 || decision.Bound[0].Name != "craft_visual_polish_selftest" {
		t.Fatalf("banner paraphrase decision=%#v", decision)
	}
	if decision.Bound[0].Description != task {
		t.Fatalf("published description = %q", decision.Bound[0].Description)
	}
	if len(emb.batches) != 1 || len(emb.batches[0]) != 2 || emb.batches[0][0] != query || emb.batches[0][1] != task {
		t.Fatalf("cold batch = %#v, want instructed query then the task", emb.batches)
	}
}

func TestSkillAdmissionVecCacheKeepsRequestedTextsWhenFull(t *testing.T) {
	resetSkillAdmissionVecCache()
	emb := skillAdmissionTestEmbedder{}
	filled := make([]string, skillAdmissionVecCacheMax)
	for i := range filled {
		filled[i] = fmt.Sprintf("doc-%d", i)
	}
	if _, ok := cachedSkillAdmissionVectors(emb, filled); !ok {
		t.Fatal("filling the cache failed")
	}
	out, ok := cachedSkillAdmissionVectors(emb, []string{filled[0], "brand-new"})
	if !ok || len(out) != 2 || len(out[0]) == 0 || len(out[1]) == 0 {
		t.Fatalf("cache overflow dropped a requested vector: ok=%v out=%v", ok, out)
	}
}

type countingAdmissionEmbedder struct {
	skillAdmissionTestEmbedder
	batches int
}

func (e *countingAdmissionEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	e.batches++
	return e.skillAdmissionTestEmbedder.EmbedBatch(texts)
}

func TestSkillAdmissionVecQueryCacheStaysOffTheDocBudget(t *testing.T) {
	resetSkillAdmissionVecCache()
	const doc = "把指定文件复制到当前工作区"
	emb := &countingAdmissionEmbedder{}
	if _, ok := cachedSkillAdmissionVectors(emb, []string{doc}); !ok {
		t.Fatal("description embed failed")
	}
	filled := emb.batches
	var last string
	for i := 0; i < skillAdmissionQueryCacheMax+3; i++ {
		last = fmt.Sprintf("任务%04d", i)
		query, vectors, ok := embedSkillAdmissionVectors(emb, last, []string{doc})
		if !ok || len(query) == 0 || len(vectors) != 1 || len(vectors[0]) == 0 {
			t.Fatalf("query %d embed ok=%v", i, ok)
		}
	}
	stamp := skillAdmissionVecStamp(emb)
	skillAdmissionVecCache.mu.Lock()
	docs := len(skillAdmissionVecCache.vecs)
	queries := len(skillAdmissionVecCache.queries)
	_, queryInDocs := skillAdmissionVecCache.vecs[skillAdmissionQueryCacheKey(last)]
	skillAdmissionVecCache.mu.Unlock()
	if docs != 1 || queryInDocs {
		t.Fatalf("description map = %d, query key inside it = %v", docs, queryInDocs)
	}
	if queries > skillAdmissionQueryCacheMax {
		t.Fatalf("query cache grew to %d", queries)
	}
	if _, ok := skillAdmissionVecQuerySnapshot(stamp, skillAdmissionQueryCacheKey(last)); !ok {
		t.Fatal("latest query was evicted")
	}
	if _, ok := skillAdmissionVecSnapshot(stamp, []string{doc}); !ok {
		t.Fatal("description was evicted")
	}
	if !skillAdmissionVecStore(stamp, []string{"other-doc"}, [][]float32{{1, 0}}, true) {
		t.Fatal("description rebuild store failed")
	}
	if _, ok := skillAdmissionVecQuerySnapshot(stamp, skillAdmissionQueryCacheKey(last)); !ok {
		t.Fatal("description rebuild dropped the query")
	}
	beforeWarm := emb.batches
	again, _, ok := embedSkillAdmissionVectors(emb, last, []string{"other-doc"})
	if !ok || len(again) == 0 {
		t.Fatal("warm query after rebuild failed")
	}
	if emb.batches != beforeWarm {
		t.Fatalf("warm admission embedded again: batches %d -> %d", beforeWarm, emb.batches)
	}
	if beforeWarm != filled+skillAdmissionQueryCacheMax+3 {
		t.Fatalf("batches = %d, want one description fill and %d queries", beforeWarm, skillAdmissionQueryCacheMax+3)
	}
}

func TestSkillAdmissionVecRebuildWaitsUntilStore(t *testing.T) {
	resetSkillAdmissionVecCache()
	emb := skillAdmissionTestEmbedder{vecs: map[string][]float32{
		"brand-new": {1, 1},
	}}
	filled := make([]string, skillAdmissionVecCacheMax)
	for i := range filled {
		filled[i] = fmt.Sprintf("doc-%d", i)
	}
	if _, ok := cachedSkillAdmissionVectors(emb, filled); !ok {
		t.Fatal("filling the cache failed")
	}
	stamp := skillAdmissionVecStamp(emb)
	_, missing, rebuild := skillAdmissionVecPrepare(stamp, []string{filled[0], "brand-new"})
	if !rebuild || len(missing) != 2 {
		t.Fatalf("rebuild=%v missing=%v", rebuild, missing)
	}
	if _, ok := skillAdmissionVecSnapshot(stamp, []string{filled[1]}); !ok {
		t.Fatal("cache was cleared before the new vectors were stored")
	}
	vecs := make([][]float32, len(missing))
	for i, text := range missing {
		vec, err := emb.Embed(text)
		if err != nil {
			t.Fatal(err)
		}
		vecs[i] = vec
	}
	if !skillAdmissionVecStore(stamp, missing, vecs, true) {
		t.Fatal("store failed")
	}
	if _, ok := skillAdmissionVecSnapshot(stamp, []string{filled[1]}); ok {
		t.Fatal("rebuild kept a vector outside this request")
	}
	if _, ok := skillAdmissionVecSnapshot(stamp, []string{filled[0], "brand-new"}); !ok {
		t.Fatal("rebuild dropped a requested vector")
	}
}

func TestSkillAdmissionVectorIgnoresAnUnrelatedTask(t *testing.T) {
	resetSkillAdmissionVecCache()
	app, _ := newSkillExecutorWithEntries(t, copyToWorkspaceLearnedSkill())
	h := &IMMessageHandler{app: app}
	h.interruptHandler = newIMInterruptHandler(h)
	h.interruptHandler.SetEmbedder(skillAdmissionTestEmbedder{vecs: map[string][]float32{
		"把指定文件复制到当前工作区":    {1, 0},
		"加固 php.ini 并重启服务": {0, 1},
	}})
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), "加固 php.ini 并重启服务")
	if decision.Mode == skillAdmissionBound {
		t.Fatalf("unrelated task bound %#v", decision.Bound)
	}
}

func TestSkillAdmissionVectorRefusesANearMissUnderTheMargin(t *testing.T) {
	resetSkillAdmissionVecCache()
	const (
		para   = "帮我把那份手册拷进工作目录"
		winner = "把指定文件复制到当前工作区"
		near   = "查询服务器监听端口和 PHP 版本"
	)
	app, _ := newSkillExecutorWithEntries(t,
		learnedRecipe("craft_copy_into_workspace", winner),
		learnedRecipe("craft_query_listen_ports", near),
	)
	h := &IMMessageHandler{app: app}
	h.interruptHandler = newIMInterruptHandler(h)
	h.interruptHandler.SetEmbedder(skillAdmissionTestEmbedder{vecs: map[string][]float32{
		para:   {1, 0},
		winner: vecAtCosine(0.45),
		near:   vecAtCosine(0.415),
	}})
	rawWin := float64(cosine32([]float32{1, 0}, vecAtCosine(0.45)))
	rawNear := float64(cosine32([]float32{1, 0}, vecAtCosine(0.415)))
	if rawWin-skillAdmissionVectorBaseline < skillAdmissionVectorMargin {
		t.Fatalf("fixture winner cosine %.4f is under the accept line", rawWin)
	}
	if rawNear-skillAdmissionVectorBaseline >= skillAdmissionVectorMargin {
		t.Fatalf("fixture runner-up cosine %.4f cleared the accept line", rawNear)
	}
	if rawWin-rawNear >= skillAdmissionVectorGap {
		t.Fatalf("fixture gap %.4f is not ambiguous", rawWin-rawNear)
	}
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), para)
	if decision.Mode == skillAdmissionBound {
		t.Fatalf("near miss under the margin still bound %#v", decision.Bound)
	}
}

func TestSkillAdmissionVectorBindsWhenTheRunnerUpIsFar(t *testing.T) {
	resetSkillAdmissionVecCache()
	const (
		para   = "帮我把那份手册拷进工作目录"
		winner = "把指定文件复制到当前工作区"
		far    = "今天北京天气怎么样"
	)
	app, _ := newSkillExecutorWithEntries(t,
		learnedRecipe("craft_copy_into_workspace", winner),
		learnedRecipe("craft_weather", far),
	)
	h := &IMMessageHandler{app: app}
	h.interruptHandler = newIMInterruptHandler(h)
	h.interruptHandler.SetEmbedder(skillAdmissionTestEmbedder{vecs: map[string][]float32{
		para:   {1, 0},
		winner: vecAtCosine(0.9),
		far:    vecAtCosine(0.2),
	}})
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), para)
	if decision.Mode != skillAdmissionBound || len(decision.Bound) != 1 || decision.Bound[0].Name != "craft_copy_into_workspace" {
		t.Fatalf("far runner-up decision=%#v", decision)
	}
}

func TestSkillAdmissionVectorBindsOneOfTwoIdenticalRecipes(t *testing.T) {
	resetSkillAdmissionVecCache()
	const (
		para = "帮我把那份手册拷进工作目录"
		desc = "把指定文件复制到当前工作区"
	)
	app, _ := newSkillExecutorWithEntries(t,
		learnedRecipe("craft_copy_into_workspace", desc),
		learnedRecipe("craft_copy_into_workspace_again", desc),
	)
	h := &IMMessageHandler{app: app}
	h.interruptHandler = newIMInterruptHandler(h)
	h.interruptHandler.SetEmbedder(skillAdmissionTestEmbedder{vecs: map[string][]float32{
		para: {1, 0},
		desc: {1, 0},
	}})
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), para)
	if decision.Mode != skillAdmissionBound || len(decision.Bound) != 1 {
		t.Fatalf("duplicate recipe decision=%#v", decision)
	}
	name := decision.Bound[0].Name
	if name != "craft_copy_into_workspace" && name != "craft_copy_into_workspace_again" {
		t.Fatalf("duplicate recipe bound %q", name)
	}
}

func TestSkillAdmissionVectorDoesNotReplaceANamedSkill(t *testing.T) {
	resetSkillAdmissionVecCache()
	const (
		desc = "把指定文件复制到当前工作区"
		text = "使用 book-pdf skill 把手册做成书"
	)
	app, _ := newSkillExecutorWithEntries(t, learnedRecipe("craft_copy_into_workspace", desc))
	h := &IMMessageHandler{app: app}
	h.interruptHandler = newIMInterruptHandler(h)
	h.interruptHandler.SetEmbedder(skillAdmissionTestEmbedder{vecs: map[string][]float32{
		text: {1, 0},
		desc: {1, 0},
	}})
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), text)
	if decision.Mode == skillAdmissionBound {
		t.Fatalf("named skill miss was replaced by a learned recipe: %#v", decision.Bound)
	}
}

func TestSkillAdmissionWithoutEmbedderKeepsKeywordMiss(t *testing.T) {
	app, _ := newSkillExecutorWithEntries(t, copyToWorkspaceLearnedSkill())
	h := &IMMessageHandler{app: app}
	decision := h.skillAdmissionForTurn(fullDocumentLoop(intent.LabelDocumentGenerate), "帮我把那份手册拷进工作目录")
	if decision.Mode == skillAdmissionBound {
		t.Fatalf("keyword miss bound without an embedder: %#v", decision.Bound)
	}
}

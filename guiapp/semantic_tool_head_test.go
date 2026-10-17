package guiapp

import (
	"encoding/json"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

type toolHeadTestEmb struct {
	id  string
	dim int
}

func (e toolHeadTestEmb) Embed(string) ([]float32, error) { return make([]float32, e.dim), nil }
func (e toolHeadTestEmb) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i], _ = e.Embed("")
	}
	return out, nil
}
func (e toolHeadTestEmb) Dim() int        { return e.dim }
func (e toolHeadTestEmb) Close()          {}
func (e toolHeadTestEmb) ModelID() string { return e.id }

func semanticTestToolHead(t *testing.T, tools []string, rows [][]float64) *tool.ToolHead {
	t.Helper()
	trained := make([]bool, len(tools))
	W := make([][]float64, len(tools))
	for i := range tools {
		trained[i] = true
		W[i] = rows[i]
	}
	raw, err := json.Marshal(map[string]any{
		"format": tool.ToolHeadFormat, "embedder_model_id": "qwen3:4", "embed_role": "classification",
		"dims": 4, "l2norm": true, "label_table_version": tool.ToolLabelTableVersion(tools),
		"tools": tools, "trained": trained, "W": W, "b": make([]float64, len(tools)),
		"theta": 0.3, "top_k": 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := tool.ParseToolHead(raw)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestUnionToolHeadNeedsUsesQueryVec(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	// web_search ~0.99 (read-only, add), asr ~0.99 (needs an audio input, hold),
	// ssh ~0.88 (effectful, at the write floor, add), generate_pdf ~0.60 (hold),
	// web_fetch ~0.99 (latent on a pure lookup; a document composite promotes it),
	// session_search ~0.62 (above theta, private read, hold).
	head := semanticTestToolHead(t,
		[]string{"web_search", "asr", "ssh", "generate_pdf", "web_fetch", "session_search"},
		[][]float64{
			{5, 0, 0, 0},
			{5, 0, 0, 0},
			{2, 0, 0, 0},
			{0.4, 0, 0, 0},
			{5, 0, 0, 0},
			{0.5, 0, 0, 0},
		},
	)
	router := NewToolRouter(nil)
	if err := router.SetToolHead(head, toolHeadTestEmb{id: "qwen3:4", dim: 4}, 0.3, 6); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{toolRouter: router}
	needs := []tool.CapabilityNeed{{
		ID: "need:office", Capability: tool.CapabilityDocumentWriteOffice, Required: true,
	}}
	got := h.unionToolHeadNeeds(registry, needs, intent.ClassificationResult{
		Primary: intent.LabelSearch, Confidence: 0.95, QueryVec: []float32{1, 0, 0, 0},
	})
	if !capabilityNeedPresent(got, "information.search.web") {
		t.Fatalf("read-only web_search was not unioned: %#v", got)
	}
	if got := countCapabilityNeeds(got, tool.CapabilityShellExecuteRemoteHost); got != 8 {
		t.Fatalf("ssh repeat budget = %d, want the reviewed 8", got)
	}
	if capabilityNeedPresent(got, "document.generate.file") {
		t.Fatal("generate_pdf below the write floor was granted")
	}
	if capabilityNeedPresent(got, tool.CapabilityAudioTranscribeSpeech) {
		t.Fatal("asr was minted without an audio input")
	}
	if capabilityNeedPresent(got, tool.CapabilityInformationFetchWeb) {
		t.Fatal("pure lookup must keep web_fetch latent")
	}
	composite := h.unionToolHeadNeeds(registry, needs, intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate},
		Confidence: 0.92, QueryVec: []float32{1, 0, 0, 0},
	})
	if got := countCapabilityNeeds(composite, tool.CapabilityInformationFetchWeb); got != 5 {
		t.Fatalf("document composite fetch family = %d, want the reviewed 5", got)
	}
	for _, need := range composite {
		if need.Capability == tool.CapabilityInformationFetchWeb && need.Required {
			t.Fatal("tool-head fetch must stay optional so search still leads")
		}
	}
	if capabilityNeedPresent(got, tool.CapabilitySecurityAuditRead) {
		t.Fatal("session_search below the write floor was granted")
	}
	for _, need := range got {
		if need.Capability == "information.search.web" && need.Required {
			t.Fatal("tool-head search must stay optional so a light budget can drop it")
		}
	}
	// No vector: the head is not a second embedder.
	same := h.unionToolHeadNeeds(registry, needs, intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.95})
	if len(same) != 1 {
		t.Fatalf("missing vector changed needs: %#v", same)
	}
}

func TestToolHeadPromotesLatentFetchOnlyForDocumentComposites(t *testing.T) {
	high := 0.93
	low := 0.70
	if toolHeadPromotesLatentFetch(intent.ClassificationResult{Primary: intent.LabelSearch}, high) {
		t.Fatal("pure search must keep fetch latent")
	}
	if toolHeadPromotesLatentFetch(intent.ClassificationResult{Primary: intent.LabelLiveData}, high) {
		t.Fatal("pure live data must keep fetch latent")
	}
	weatherPDF := intent.ClassificationResult{
		Primary: intent.LabelLiveData, Secondary: []intent.IntentLabel{intent.LabelDocumentGenerate},
	}
	if !toolHeadPromotesLatentFetch(weatherPDF, high) {
		t.Fatal("weather+pdf at the write floor must promote fetch")
	}
	if toolHeadPromotesLatentFetch(weatherPDF, low) {
		t.Fatal("weather+pdf below the write floor must stay latent")
	}
	if !toolHeadPromotesLatentFetch(intent.ClassificationResult{Primary: intent.LabelOffice}, high) {
		t.Fatal("office at the write floor must promote fetch")
	}
}

func countCapabilityNeeds(needs []tool.CapabilityNeed, capability tool.CapabilityID) int {
	n := 0
	for _, need := range needs {
		if need.Capability == capability {
			n++
		}
	}
	return n
}

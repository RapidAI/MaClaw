package guiapp

import (
	"context"
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
	// web_fetch ~0.99 (read-only declaration, add the reviewed family),
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
	got := h.unionToolHeadNeeds(context.Background(), registry, needs, intent.ClassificationResult{
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
	if got := countCapabilityNeeds(got, tool.CapabilityInformationFetchWeb); got != 5 {
		t.Fatalf("fetch family = %d, want the reviewed 5", got)
	}
	for _, need := range got {
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
	same := h.unionToolHeadNeeds(context.Background(), registry, needs, intent.ClassificationResult{Primary: intent.LabelOffice, Confidence: 0.95})
	if len(same) != 1 {
		t.Fatalf("missing vector changed needs: %#v", same)
	}
}

func TestLatentFetchBelowWriteFloorStaysOffThePlan(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	// ~0.73: above the legacy visibility threshold, below the grant floor.
	head := semanticTestToolHead(t, []string{"web_fetch"}, [][]float64{{1, 0, 0, 0}})
	router := NewToolRouter(nil)
	if err := router.SetToolHead(head, toolHeadTestEmb{id: "qwen3:4", dim: 4}, 0.3, 5); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{toolRouter: router}
	got := h.unionToolHeadNeeds(context.Background(), registry, nil, intent.ClassificationResult{
		Primary: intent.LabelSearch, Confidence: 0.95, QueryVec: []float32{1, 0, 0, 0},
	})
	if capabilityNeedPresent(got, tool.CapabilityInformationFetchWeb) {
		t.Fatalf("latent fetch below the grant floor was planned: %#v", got)
	}
}

func TestPetitionRedeclareKeepsParentToolHeadFamily(t *testing.T) {
	registry := newIMSemanticCapabilityRegistry()
	head := semanticTestToolHead(t, []string{"web_search"}, [][]float64{{5, 0, 0, 0}})
	router := NewToolRouter(nil)
	if err := router.SetToolHead(head, toolHeadTestEmb{id: "qwen3:4", dim: 4}, 0.3, 5); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{toolRouter: router}
	classification := intent.ClassificationResult{
		Primary: intent.LabelOffice, Confidence: 0.95, QueryVec: []float32{1, 0, 0, 0},
	}
	office := []tool.CapabilityNeed{{
		ID: "need:office", Capability: tool.CapabilityDocumentWriteOffice, Required: true,
	}}
	parent := h.unionToolHeadNeeds(context.Background(), registry, office, classification)
	var selections []tool.PlannedSelection
	var parentSearchIDs []string
	for _, need := range parent {
		if need.Capability != "information.search.web" {
			continue
		}
		parentSearchIDs = append(parentSearchIDs, need.ID)
		selections = append(selections, tool.PlannedSelection{NeedID: need.ID})
	}
	if len(parentSearchIDs) != 5 {
		t.Fatalf("parent search family = %d, want 5", len(parentSearchIDs))
	}
	// The petitioned label minted search again, under its own id.
	shadowID := "need:information.search.web:petition"
	shadow := append(append([]tool.CapabilityNeed{}, office...), tool.CapabilityNeed{
		ID: shadowID, Capability: "information.search.web", Required: true,
		Qualifiers: map[string]string{"freshness": "reference"},
	})
	withoutParent := h.unionToolHeadNeeds(context.Background(), registry, shadow, classification)
	if countCapabilityNeeds(withoutParent, "information.search.web") != 1 || !capabilityNeedPresent(withoutParent, "information.search.web") {
		t.Fatalf("cold redeclare must not open a second search family: %#v", withoutParent)
	}
	for _, need := range withoutParent {
		if need.ID == parentSearchIDs[0] {
			t.Fatal("cold redeclare reused the parent tool-head id")
		}
	}
	ctx := withSemanticPetitionParentFamilies(context.Background(), tool.ToolPlan{Selections: selections})
	kept := h.unionToolHeadNeeds(ctx, registry, shadow, classification)
	got := map[string]bool{}
	for _, need := range kept {
		if need.Capability == "information.search.web" {
			got[need.ID] = true
		}
	}
	if got[shadowID] {
		t.Fatal("petition kept a second need id for the same search contract")
	}
	for _, id := range parentSearchIDs {
		if !got[id] {
			t.Fatalf("parent tool-head id %s missing from petition re-plan: %#v", id, kept)
		}
	}
	currentID := "need:information.search.web:current"
	current := append(append([]tool.CapabilityNeed{}, office...), tool.CapabilityNeed{
		ID: currentID, Capability: "information.search.web", Required: true,
		Qualifiers: map[string]string{"freshness": "current"},
	})
	both := h.unionToolHeadNeeds(ctx, registry, current, classification)
	if !needIDPresent(both, currentID) || !needIDPresent(both, parentSearchIDs[0]) {
		t.Fatalf("current search must sit beside the parent reference family: %#v", both)
	}
}

func needIDPresent(needs []tool.CapabilityNeed, id string) bool {
	for _, need := range needs {
		if need.ID == id {
			return true
		}
	}
	return false
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

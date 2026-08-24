package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestGetLLMClassTrafficHandlerEmitsFrozenRowsAndNoHintSamples(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	recordLLMClassTraffic(sys, []string{"coding-auto"}, llmservice.OfficialForwardMeta{
		WorkloadClass: llmpool.WorkloadClassCode,
		ClassSource:   llmpool.ClassSourceHint,
		ResolvedModel: "official-mid",
	}, corelib.TokenUsageStat{InputTokens: 10, OutputTokens: 4}, "should-not-sample")
	recordLLMClassTraffic(sys, []string{"coding-auto"}, llmservice.OfficialForwardMeta{
		WorkloadClass: llmpool.WorkloadFallbackBalanced,
		ClassSource:   llmpool.ClassSourceFallback,
		ResolvedModel: "official-mid",
	}, corelib.TokenUsageStat{InputTokens: 3, OutputTokens: 1}, "write a weekly report")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/class-traffic?service_group_id=coding-auto&window=24h", nil)
	rec := httptest.NewRecorder()
	GetLLMClassTrafficHandler(sys)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got llmClassTrafficResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Window != "24h" || got.ServiceGroupID != "coding-auto" {
		t.Fatalf("meta=%#v", got)
	}
	if len(got.Rows) != len(llmpool.FrozenWorkloadClasses)+3 {
		t.Fatalf("rows=%d want %d", len(got.Rows), len(llmpool.FrozenWorkloadClasses)+3)
	}
	byClass := map[string]llmClassTrafficRow{}
	for _, row := range got.Rows {
		byClass[row.Class] = row
	}
	if byClass["code"].Requests != 1 || byClass["balanced"].Requests != 1 || byClass["total"].Requests != 2 {
		t.Fatalf("rows=%#v", got.Rows)
	}
	if byClass["total"].InputTokens != 13 || byClass["total"].OutputTokens != 5 {
		t.Fatalf("total tokens=%#v", byClass["total"])
	}
	if got.Sources[llmpool.ClassSourceHint] != 1 || got.Sources[llmpool.ClassSourceFallback] != 1 {
		t.Fatalf("sources=%#v", got.Sources)
	}
	if len(got.Samples) != 1 || got.Samples[0].Preview != "write a weekly report" || got.Samples[0].Source != llmpool.ClassSourceFallback {
		t.Fatalf("samples=%#v", got.Samples)
	}
}

func TestPostLLMClassifyPreviewIncludesAttribution(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{{
		ID:     "coding-auto",
		Name:   "Coding Auto",
		Kind:   llmpool.ServiceGroupKindDynamic,
		Routes: llmpool.DefaultOfficialAutoRoutes(),
		Models: []llmservice.ModelServiceModel{
			{Name: "auto"},
			{Name: llmpool.OfficialTierHigh},
			{Name: llmpool.OfficialTierMid},
			{Name: llmpool.OfficialTierLow},
		},
	}}}
	if err := llmservice.SaveRegistry(t.Context(), sys, reg); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(llmClassifyPreviewRequest{
		Headers: map[string]string{llmpool.WorkloadClassHeader: "plan"},
		Body:    map[string]any{"model": "auto"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/classify-preview?service_group_id=coding-auto", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	PostLLMClassifyPreviewHandler(sys)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var dec llmpool.WorkloadDecision
	if err := json.Unmarshal(rec.Body.Bytes(), &dec); err != nil {
		t.Fatal(err)
	}
	attr := dec.Attribution
	if attr.RequestedGroup != "coding-auto" || attr.RequestedModel != "auto" || attr.WorkloadClass != llmpool.WorkloadClassPlan {
		t.Fatalf("attribution = %#v", attr)
	}
	if attr.ResolvedModel != llmpool.OfficialTierHigh || attr.SelectionReason != "dynamic workload route" {
		t.Fatalf("resolved = %#v", attr)
	}
}

func groupPageDynamicGroup(id string) llmservice.ModelServiceGroup {
	return llmservice.ModelServiceGroup{
		ID:     id,
		Name:   id,
		Kind:   llmpool.ServiceGroupKindDynamic,
		Routes: llmpool.DefaultOfficialAutoRoutes(),
		Models: []llmservice.ModelServiceModel{
			{Name: "auto"},
			{Name: llmpool.OfficialTierHigh},
			{Name: llmpool.OfficialTierMid},
			{Name: llmpool.OfficialTierLow},
		},
	}
}

func TestGroupPageAPIsIsolateHeadsTrafficAndAttribution(t *testing.T) {
	sys := &stubSystemSettings{data: map[string]string{}}
	reg := &llmservice.Registry{ModelServiceGroups: []llmservice.ModelServiceGroup{
		groupPageDynamicGroup("coding-auto"),
		groupPageDynamicGroup("writing-auto"),
	}}
	if err := llmservice.SaveRegistry(t.Context(), sys, reg); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	GetLLMServicesAdminHandler(sys)(rec, httptest.NewRequest(http.MethodGet, "/api/admin/llm/services?include_cards=false", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("services status=%d body=%s", rec.Code, rec.Body.String())
	}
	var services llmServiceAdminResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &services); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, group := range services.ModelServiceGroups {
		if group.Kind == llmpool.ServiceGroupKindDynamic {
			ids[group.ID] = true
		}
	}
	if !ids["coding-auto"] || !ids["writing-auto"] {
		t.Fatalf("dynamic groups missing: %#v", services.ModelServiceGroups)
	}

	preview := func(groupID, class string) llmpool.WorkloadDecision {
		t.Helper()
		body, _ := json.Marshal(llmClassifyPreviewRequest{
			Headers: map[string]string{llmpool.WorkloadClassHeader: class},
			Body:    map[string]any{"model": "auto"},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/admin/llm/classify-preview?service_group_id="+groupID, bytes.NewReader(body))
		out := httptest.NewRecorder()
		PostLLMClassifyPreviewHandler(sys)(out, req)
		if out.Code != http.StatusOK {
			t.Fatalf("preview %s status=%d body=%s", groupID, out.Code, out.Body.String())
		}
		var dec llmpool.WorkloadDecision
		if err := json.Unmarshal(out.Body.Bytes(), &dec); err != nil {
			t.Fatal(err)
		}
		if dec.Attribution.RequestedGroup != groupID || dec.Attribution.RequestedModel != "auto" {
			t.Fatalf("preview %s attribution=%#v", groupID, dec.Attribution)
		}
		if dec.Attribution.WorkloadClass != class || dec.Attribution.SelectionReason != "dynamic workload route" {
			t.Fatalf("preview %s class/reason=%#v", groupID, dec.Attribution)
		}
		return dec
	}
	if got := preview("coding-auto", llmpool.WorkloadClassPlan); got.Attribution.ResolvedModel != llmpool.OfficialTierHigh {
		t.Fatalf("coding plan resolved=%#v", got.Attribution)
	}
	if got := preview("writing-auto", llmpool.WorkloadClassDocWrite); got.Attribution.ResolvedModel != llmpool.OfficialTierMid {
		t.Fatalf("writing doc resolved=%#v", got.Attribution)
	}

	recordLLMClassTraffic(sys, []string{"coding-auto"}, llmservice.OfficialForwardMeta{
		WorkloadClass: llmpool.WorkloadClassPlan,
		ClassSource:   llmpool.ClassSourceHint,
		ResolvedModel: llmpool.OfficialTierHigh,
	}, corelib.TokenUsageStat{InputTokens: 8, OutputTokens: 2, Requests: 1}, "write a weekly report")
	traffic := func(groupID string) llmClassTrafficResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/class-traffic?service_group_id="+groupID+"&window=24h", nil)
		out := httptest.NewRecorder()
		GetLLMClassTrafficHandler(sys)(out, req)
		if out.Code != http.StatusOK {
			t.Fatalf("traffic %s status=%d body=%s", groupID, out.Code, out.Body.String())
		}
		var got llmClassTrafficResponse
		if err := json.Unmarshal(out.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	codingTraffic := traffic("coding-auto")
	writingTraffic := traffic("writing-auto")
	if codingTraffic.ServiceGroupID != "coding-auto" || writingTraffic.ServiceGroupID != "writing-auto" {
		t.Fatalf("traffic meta coding=%#v writing=%#v", codingTraffic, writingTraffic)
	}
	codingByClass := map[string]llmClassTrafficRow{}
	for _, row := range codingTraffic.Rows {
		codingByClass[row.Class] = row
	}
	if codingByClass["plan"].Requests != 1 || codingByClass["total"].Requests != 1 {
		t.Fatalf("coding traffic=%#v", codingTraffic.Rows)
	}
	for _, row := range writingTraffic.Rows {
		if row.Requests != 0 {
			t.Fatalf("writing traffic leaked: %#v", writingTraffic.Rows)
		}
	}

	recordLLMClassHeadSample(sys, []string{"coding-auto"}, llmservice.OfficialForwardMeta{
		Preview: "design a service", RuleClass: llmpool.WorkloadClassDesign, RuleSource: llmpool.ClassSourceHint,
	})
	head := func(groupID string) llmClassHeadResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/class-head?group_id="+groupID, nil)
		out := httptest.NewRecorder()
		GetLLMClassHeadHandler(sys)(out, req)
		if out.Code != http.StatusOK {
			t.Fatalf("head %s status=%d body=%s", groupID, out.Code, out.Body.String())
		}
		var got llmClassHeadResponse
		if err := json.Unmarshal(out.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	codingHead := head("coding-auto")
	writingHead := head("writing-auto")
	if len(codingHead.Samples) != 1 || codingHead.Samples[0].GroupID != "coding-auto" {
		t.Fatalf("coding head=%#v", codingHead.Samples)
	}
	if len(writingHead.Samples) != 0 {
		t.Fatalf("writing head leaked: %#v", writingHead.Samples)
	}
}

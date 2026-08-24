package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

type classHeadSettingsStub struct {
	data map[string]string
}

func (s *classHeadSettingsStub) Set(_ context.Context, key, value string) error {
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[key] = value
	return nil
}

func (s *classHeadSettingsStub) Get(_ context.Context, key string) (string, error) {
	return s.data[key], nil
}

func (s *classHeadSettingsStub) List(_ context.Context) ([]*store.SystemSettingEntry, error) {
	return nil, nil
}

func seedPublishedOfficialHead(t *testing.T, stub *classHeadSettingsStub, version int) {
	t.Helper()
	head := llmpool.EmptyHead(version, llmpool.DefaultHeadTau)
	payload, err := json.Marshal(map[string]any{
		"version":  1,
		"pipeline": llmpool.PipelineOn,
		"status":   llmpool.HeadStatusPromoted,
		"current":  head,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := stub.Set(t.Context(), llmservice.OfficialClassHeadKey, string(payload)); err != nil {
		t.Fatal(err)
	}
}

func TestAdminClassHeadGroupIDIgnoresQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/llm/class-head?group_id=coding-auto", nil)
	if got := adminClassHeadGroupID(req); got != "" {
		t.Fatalf("admin group id = %q", got)
	}
	if got := adminClassHeadQueryGroupID(req); got != "coding-auto" {
		t.Fatalf("query group id = %q", got)
	}
}

func TestAdminClassHeadHandlersUseOfficialStore(t *testing.T) {
	stub := &classHeadSettingsStub{data: map[string]string{}}
	seedPublishedOfficialHead(t, stub, 8)
	svc := llmservice.NewService(stub)

	missing := httptest.NewRecorder()
	adminLLMClassHeadPullOfficialHandler(svc)(missing, httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pull-official", bytes.NewReader([]byte("{}"))))
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing group_id status=%d body=%s", missing.Code, missing.Body.String())
	}

	self := httptest.NewRecorder()
	adminLLMClassHeadPullOfficialHandler(svc)(self, httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pull-official?group_id="+llmpool.OfficialGroupID, bytes.NewReader([]byte("{}"))))
	if self.Code != http.StatusBadRequest {
		t.Fatalf("self pull status=%d body=%s", self.Code, self.Body.String())
	}

	pull := httptest.NewRecorder()
	adminLLMClassHeadPullOfficialHandler(svc)(pull, httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pull-official?group_id=coding-auto", bytes.NewReader([]byte("{}"))))
	if pull.Code != http.StatusBadRequest {
		t.Fatalf("group pull status=%d body=%s", pull.Code, pull.Body.String())
	}
	if _, ok := stub.data["llm_class_head_v1:coding-auto"]; ok {
		t.Fatal("pull-official must not write a per-group store")
	}

	shadowBody, _ := json.Marshal(map[string]string{"mode": llmpool.PipelineShadow})
	shadow := httptest.NewRecorder()
	adminLLMClassHeadPipelineHandler(svc)(shadow, httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pipeline?group_id=coding-auto", bytes.NewReader(shadowBody)))
	if shadow.Code != http.StatusOK {
		t.Fatalf("shadow status=%d body=%s", shadow.Code, shadow.Body.String())
	}
	body, _ := json.Marshal(map[string]string{
		"mode":     llmpool.PipelineCanary,
		"override": llmpool.PromoteOverride,
		"reason":   "lab",
	})
	pipe := httptest.NewRecorder()
	adminLLMClassHeadPipelineHandler(svc)(pipe, httptest.NewRequest(http.MethodPost, "/api/admin/llm/class-head/pipeline?group_id=writing-auto", bytes.NewReader(body)))
	if pipe.Code != http.StatusOK {
		t.Fatalf("pipeline status=%d body=%s", pipe.Code, pipe.Body.String())
	}

	get := func(groupID string) llmservice.OfficialClassHeadView {
		t.Helper()
		path := "/api/admin/llm/class-head"
		if groupID != "" {
			path += "?group_id=" + groupID
		}
		out := httptest.NewRecorder()
		adminLLMClassHeadHandler(svc)(out, httptest.NewRequest(http.MethodGet, path, nil))
		if out.Code != http.StatusOK {
			t.Fatalf("get %q status=%d body=%s", groupID, out.Code, out.Body.String())
		}
		var got llmservice.OfficialClassHeadView
		if err := json.Unmarshal(out.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	coding := get("coding-auto")
	writing := get("writing-auto")
	official := get(llmpool.OfficialGroupID)
	bare := get("")
	if coding.StoreKey != llmservice.OfficialClassHeadKey || writing.StoreKey != llmservice.OfficialClassHeadKey || official.StoreKey != llmservice.OfficialClassHeadKey || bare.StoreKey != llmservice.OfficialClassHeadKey {
		t.Fatalf("store keys coding=%q writing=%q official=%q bare=%q", coding.StoreKey, writing.StoreKey, official.StoreKey, bare.StoreKey)
	}
	if coding.Version != 8 || coding.Pipeline != llmpool.PipelineCanary {
		t.Fatalf("coding = %#v", coding)
	}
	if writing.Version != 8 || writing.Pipeline != llmpool.PipelineCanary {
		t.Fatalf("writing = %#v", writing)
	}
	if official.Pipeline != llmpool.PipelineCanary || official.Version != 8 {
		t.Fatalf("official = %#v", official)
	}
	if bare.Pipeline != llmpool.PipelineCanary || bare.Version != 8 {
		t.Fatalf("bare = %#v", bare)
	}
}

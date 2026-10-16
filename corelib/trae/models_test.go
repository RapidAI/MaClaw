package trae

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestParseModelCatalogReadsConfigInfoListArray(t *testing.T) {
	raw := []byte(`{
		"config_info_list": [
			{"config_name":"auto","display_config":{"display_name":"Auto"}},
			{"config_name":"glm-5.2","display_config":{"display_name":"GLM-5.2"}},
			{"config_name":"kimi-k2.7-code","display_name":"Kimi"}
		]
	}`)
	models, err := parseModelCatalog(NameCN, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %#v", models)
	}
	if models[0].ID != "glm-5.2" || models[0].DisplayName != "GLM-5.2" {
		t.Fatalf("first = %#v", models[0])
	}
	if models[1].ID != "kimi-k2.7-code" || models[1].DisplayName != "Kimi" {
		t.Fatalf("second = %#v", models[1])
	}
}

func TestParseModelCatalogReadsNestedConfigInfoList(t *testing.T) {
	raw := []byte(`{"Result":{"ConfigInfoList":[{"config_name":"glm-5.2","display_config":{"display_name":"GLM-5.2"}}]}}`)
	models, err := parseModelCatalog(NameCN, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "glm-5.2" || models[0].DisplayName != "GLM-5.2" {
		t.Fatalf("models = %#v", models)
	}
}

func TestParseModelCatalogNamesMissingListKeys(t *testing.T) {
	_, err := parseModelCatalog(NameCN, []byte(`{"code":4001,"message":"invalid"}`))
	if err == nil || !strings.Contains(err.Error(), "code,message") {
		t.Fatalf("err = %v", err)
	}
}

func TestListModelsSkipsPoolThatRejectsConfig(t *testing.T) {
	orig := catalogDo
	t.Cleanup(func() { catalogDo = orig })
	var mu sync.Mutex
	functions := map[string]struct{}{}
	catalogDo = func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("X-Ide-Version"); got != CNProfile().ClientVersion {
			t.Errorf("catalog X-Ide-Version = %q", got)
		}
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var body map[string]any
		if json.Unmarshal(raw, &body) != nil {
			t.Errorf("catalog body is not JSON: %s", raw)
			return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader("bad")), Header: make(http.Header)}, nil
		}
		fn := asString(body["function"])
		mu.Lock()
		functions[fn] = struct{}{}
		mu.Unlock()
		payload := `{"code":4001,"message":"invalid"}`
		switch fn {
		case "chat_v3":
			payload = `{"config_info_list":[{"config_name":"glm-5.2","display_name":"GLM-5.2"}]}`
		case "solo_agent":
			payload = `{"config_info_list":[{"config_name":"glm-5.2","display_name":"dup"},{"config_name":"kimi-k2.6","display_name":"Kimi"}]}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(payload)),
		}, nil
	}
	models, def, err := ListModels(context.Background(), CNProfile(), "token", "mach", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if def != CNProfile().DefaultModel {
		t.Fatalf("default = %q", def)
	}
	if len(functions) != len(chatFunctionOrder) {
		t.Fatalf("pools = %v", functions)
	}
	for _, fn := range chatFunctionOrder {
		if _, ok := functions[fn]; !ok {
			t.Fatalf("pools = %v, missing %s", functions, fn)
		}
	}
	if len(models) != 2 || models[0].ID != "glm-5.2" || models[0].DisplayName != "GLM-5.2" || models[1].ID != "kimi-k2.6" {
		t.Fatalf("models = %#v", models)
	}
}

func TestParseModelCatalogDropsNonChatEntries(t *testing.T) {
	raw := []byte(`{
		"config_info_list": [
			{"config_name":"auto","display_name":"Auto"},
			{"config_name":"summary","usage":"summary","display_name":"Summary"},
			{"config_name":"glm-off","usage":"chat_completion","config_switch":false,"display_name":"Off"},
			{"config_name":"custom_model_x","usage":"chat_completion","display_name":"Custom"},
			{"config_name":"bound","is_custom_model":true,"display_name":"Bound"},
			{"config_name":"slot","display_config":{"display_name":"Slot","is_custom_model":true}},
			{"config_name":"glm-5.2","usage":"chat_completion","display_config":{"display_name":"GLM-5.2"}},
			{"config_name":"kimi-k2.6","display_name":"Kimi"},
			{"config_name":"hidden-channel","usage":"chat_completion","is_invisible_to_user":true,"display_name":"Hidden"}
		]
	}`)
	cn, err := parseModelCatalog(NameCN, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(cn) != 3 || cn[0].ID != "glm-5.2" || cn[1].ID != "kimi-k2.6" || cn[2].ID != "hidden-channel" {
		t.Fatalf("cn = %#v", cn)
	}
	global, err := parseModelCatalog(NameGlobal, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(global) != 2 || global[0].ID != "glm-5.2" || global[1].ID != "kimi-k2.6" {
		t.Fatalf("global = %#v", global)
	}
}

func TestProbeModelsSkipsHelperOnlyPool(t *testing.T) {
	orig := catalogDo
	t.Cleanup(func() { catalogDo = orig })
	var mu sync.Mutex
	var functions []string
	catalogDo = func(req *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		fn := asString(body["function"])
		mu.Lock()
		functions = append(functions, fn)
		mu.Unlock()
		payload := `{"config_info_list":[{"config_name":"summary","usage":"summary","display_name":"Summary"}]}`
		if fn == "chat_v3" {
			payload = `{"config_info_list":[{"config_name":"glm-5.2","usage":"chat_completion","display_name":"GLM-5.2"}]}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(payload)),
		}, nil
	}
	models, err := ProbeModels(context.Background(), CNProfile(), "token", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(functions) != 2 || functions[0] != soloChatFunction || functions[1] != "chat_v3" {
		t.Fatalf("pools = %v", functions)
	}
	if len(models) != 1 || models[0].ID != "glm-5.2" {
		t.Fatalf("models = %#v", models)
	}
}

func TestParseModelCatalogPrefersResultOverDecoy(t *testing.T) {
	raw := []byte(`{
		"aaa": {"config_info_list": [{"config_name":"decoy","display_name":"Decoy"}]},
		"Result": {"config_info_list": [{"config_name":"glm-5.2","display_name":"GLM-5.2"}]}
	}`)
	models, err := parseModelCatalog(NameCN, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "glm-5.2" {
		t.Fatalf("models = %#v", models)
	}
}

func TestProbeModelsStopsAtFirstList(t *testing.T) {
	orig := catalogDo
	t.Cleanup(func() { catalogDo = orig })
	var mu sync.Mutex
	var functions []string
	catalogDo = func(req *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		fn := asString(body["function"])
		mu.Lock()
		functions = append(functions, fn)
		mu.Unlock()
		if fn != soloChatFunction {
			t.Errorf("probe continued to %s", fn)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"config_info_list":[{"config_name":"glm-5.2","display_name":"GLM-5.2"}]}`)),
		}, nil
	}
	models, err := ProbeModels(context.Background(), CNProfile(), "token", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "glm-5.2" || len(functions) != 1 {
		t.Fatalf("models=%#v pools=%v", models, functions)
	}
}

func TestProbeModelsSkipsMembershipRefusal(t *testing.T) {
	orig := catalogDo
	t.Cleanup(func() { catalogDo = orig })
	var mu sync.Mutex
	var functions []string
	catalogDo = func(req *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		fn := asString(body["function"])
		mu.Lock()
		functions = append(functions, fn)
		mu.Unlock()
		payload := `{"code":4001,"message":"invalid"}`
		if fn == "chat_v3" {
			payload = `{"config_info_list":[{"config_name":"gpt-5","display_name":"GPT-5"}]}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(payload)),
		}, nil
	}
	models, err := ProbeModels(context.Background(), GlobalProfile(), "token", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(functions) != 2 || functions[0] != soloChatFunction || functions[1] != "chat_v3" {
		t.Fatalf("pools = %v", functions)
	}
	if len(models) != 1 || models[0].ID != "gpt-5" {
		t.Fatalf("models = %#v", models)
	}
}

func TestListModelsStopsOnQuotaRefusal(t *testing.T) {
	orig := catalogDo
	t.Cleanup(func() { catalogDo = orig })
	var mu sync.Mutex
	calls := 0
	catalogDo = func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":1005,"message":"plan limit"}`)),
		}, nil
	}
	_, _, err := ListModels(context.Background(), CNProfile(), "token", "", "")
	if err == nil || !strings.Contains(err.Error(), "1005") {
		t.Fatalf("err = %v", err)
	}
	if calls != len(chatFunctionOrder) {
		t.Fatalf("quota calls = %d", calls)
	}
}

func TestListModelsStopsOnHostFailure(t *testing.T) {
	orig := catalogDo
	t.Cleanup(func() { catalogDo = orig })
	var mu sync.Mutex
	calls := 0
	catalogDo = func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("down")),
		}, nil
	}
	_, _, err := ListModels(context.Background(), GlobalProfile(), "token", "", "")
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("err = %v", err)
	}
	if calls != len(chatFunctionOrder) {
		t.Fatalf("host failure calls = %d", calls)
	}
}

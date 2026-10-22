package websearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestSearchYuanbaoRequestAndPages(t *testing.T) {
	var gotBody map[string]any
	var gotAuth, gotType, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotType = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("request JSON: %v", err)
		}
		long := strings.Repeat("词", serperSnippetLimit+5)
		page, _ := json.Marshal(map[string]string{
			"title":   "",
			"url":     "https://example.com/a",
			"passage": "passage only",
			"content": long,
			"site":    "Example",
		})
		noURL, _ := json.Marshal(map[string]string{"passage": "direct fact"})
		empty, _ := json.Marshal(map[string]string{"title": "drop me"})
		siteOnly, _ := json.Marshal(map[string]string{"site": "Site Name", "url": "https://example.com/b", "passage": "from passage"})
		w.Header().Set("Content-Type", "application/json")
		env, _ := json.Marshal(map[string]any{"Response": map[string]any{"Pages": []string{string(page), string(noURL), "not-json", string(empty), string(siteOnly)}}})
		_, _ = w.Write(env)
	}))
	defer server.Close()

	results, err := searchYuanbao(t.Context(), corelib.WebSearchProvider{
		Type: "yuanbao", Key: "sk-test", BaseURL: server.URL + "/SearchPro",
	}, "https://arxiv.org/abs/1 transformers", 10)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/SearchPro" || gotAuth != "Bearer sk-test" || !strings.Contains(gotType, "application/json") || !strings.Contains(gotType, "charset=utf-8") {
		t.Fatalf("path=%q auth=%q type=%q", gotPath, gotAuth, gotType)
	}
	if gotBody["Query"] != "https://arxiv.org/abs/1 transformers" || gotBody["Site"] != "arxiv.org" {
		t.Fatalf("body = %#v", gotBody)
	}
	for _, banned := range []string{"Mode", "Cnt", "Industry", "Freshness", "Deeplinks", "FromTime", "ToTime"} {
		if _, ok := gotBody[banned]; ok {
			t.Fatalf("request included %s: %#v", banned, gotBody)
		}
	}
	if len(results) != 3 {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Title != "Example" || results[0].URL != "https://example.com/a" || !strings.HasPrefix(results[0].Snippet, "词") || !strings.HasSuffix(results[0].Snippet, "…") || len([]rune(results[0].Snippet)) != serperSnippetLimit+1 {
		t.Fatalf("content row = %#v", results[0])
	}
	if results[1].Title != "direct fact" || results[1].URL != "" || results[1].Snippet != "direct fact" {
		t.Fatalf("url-less row = %#v", results[1])
	}
	if results[2].Title != "Site Name" || results[2].Snippet != "from passage" {
		t.Fatalf("passage row = %#v", results[2])
	}
}

func TestTestProviderYuanbaoUsesDocumentedProbe(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"Query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotQuery = body.Query
		_, _ = w.Write([]byte(`{"Response":{"Pages":[]}}`))
	}))
	defer server.Close()

	err := TestProvider(context.Background(), corelib.WebSearchProvider{Type: " yuanbao ", Key: "sk-test", BaseURL: server.URL + "/SearchPro"})
	if err == nil || !strings.Contains(err.Error(), "no results") {
		t.Fatalf("error = %v", err)
	}
	if gotQuery != "北京天气" {
		t.Fatalf("probe = %q", gotQuery)
	}
}

func TestSearchYuanbaoStripsPastedBearerPrefix(t *testing.T) {
	for _, key := range []string{"Bearer sk-test", "bearer  sk-test", "BEARER sk-test"} {
		var gotAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"Response":{"Pages":[]}}`))
		}))
		_, err := searchYuanbao(t.Context(), corelib.WebSearchProvider{Type: "yuanbao", Key: key, BaseURL: server.URL + "/SearchPro"}, "北京天气", 3)
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if gotAuth != "Bearer sk-test" {
			t.Fatalf("key %q authorization = %q", key, gotAuth)
		}
	}
}

func TestSearchYuanbaoOmitsSiteWithoutDomain(t *testing.T) {
	for _, query := range []string{"openreview iclr 2024", "golang generics", "people.com.cn notice"} {
		var got map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &got)
			_, _ = w.Write([]byte(`{"Response":{"Pages":[]}}`))
		}))
		_, err := searchYuanbao(t.Context(), corelib.WebSearchProvider{Type: "yuanbao", Key: "sk-test", BaseURL: server.URL + "/SearchPro"}, query, 3)
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got["Site"]; ok {
			t.Fatalf("query %q sent Site: %#v", query, got)
		}
	}
}

func TestSearchYuanbaoHidesProviderErrorBody(t *testing.T) {
	const secret = "sk-secret-echo"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Response":{"Error":{"Code":"UnauthorizedOperation","Message":"bad key ` + secret + `"}}}`))
	}))
	defer server.Close()
	_, err := searchYuanbao(t.Context(), corelib.WebSearchProvider{Type: "yuanbao", Key: secret, BaseURL: server.URL + "/SearchPro"}, "weather", 3)
	if err == nil || classifySearchError(err) != "invalid_key" || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "bad key") {
		t.Fatalf("error = %v", err)
	}
}

func TestSearchYuanbaoHTTP401DropsBody(t *testing.T) {
	const secret = "sk-body-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "leaked "+secret, http.StatusUnauthorized)
	}))
	defer server.Close()
	_, err := searchYuanbao(t.Context(), corelib.WebSearchProvider{Type: "yuanbao", Key: secret, BaseURL: server.URL + "/SearchPro"}, "weather", 3)
	if err == nil || classifySearchError(err) != "invalid_key" || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "leaked") {
		t.Fatalf("error = %v", err)
	}
}

func TestSearchYuanbaoEmptyPages(t *testing.T) {
	for _, body := range []string{`{"Response":{"Pages":[]}}`, `{"Response":{}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		results, err := searchYuanbao(t.Context(), corelib.WebSearchProvider{Type: "yuanbao", Key: "sk-test", BaseURL: server.URL + "/SearchPro"}, "weather", 3)
		server.Close()
		if err != nil || results != nil {
			t.Fatalf("body %s results=%#v err=%v", body, results, err)
		}
	}
}

func TestSearchYuanbaoTruncatesQueryOnRuneBoundary(t *testing.T) {
	query := strings.Repeat("a", 1498) + "中" + strings.Repeat("b", 20)
	if len(query) <= yuanbaoQueryMaxBytes {
		t.Fatalf("fixture length = %d", len(query))
	}
	var sent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query string `json:"Query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent = body.Query
		_, _ = w.Write([]byte(`{"Response":{}}`))
	}))
	defer server.Close()
	if _, err := searchYuanbao(t.Context(), corelib.WebSearchProvider{Type: "yuanbao", Key: "sk-test", BaseURL: server.URL + "/SearchPro"}, query, 3); err != nil {
		t.Fatal(err)
	}
	if len(sent) > yuanbaoQueryMaxBytes || !utf8.ValidString(sent) || !strings.HasPrefix(query, sent) || strings.Contains(sent, "中") {
		t.Fatalf("sent length=%d valid=%v sent=%q", len(sent), utf8.ValidString(sent), sent)
	}
}

package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

type knowledgeImageOCRRuntimeStub struct {
	available bool
	results   []browser.OCRResult
	called    bool
}

func (s *knowledgeImageOCRRuntimeStub) Recognize(string) ([]browser.OCRResult, error) {
	s.called = true
	return append([]browser.OCRResult(nil), s.results...), nil
}

func (s *knowledgeImageOCRRuntimeStub) IsAvailable() bool { return s.available }

func TestKnowledgeImageOCRAdapterConvertsSharedRuntimeResults(t *testing.T) {
	runtime := &knowledgeImageOCRRuntimeStub{
		available: true,
		results:   []browser.OCRResult{{Text: "Gateway", Confidence: 0.92, BBox: [4]int{1, 2, 3, 4}}},
	}
	adapter := knowledgeImageOCRAdapter{runtime: runtime}
	results, err := adapter.Recognize("ZmFrZQ==")
	if err != nil {
		t.Fatal(err)
	}
	if !runtime.called || len(results) != 1 || results[0].Text != "Gateway" || results[0].Score != 0.92 {
		t.Fatalf("converted OCR results = %#v", results)
	}
	if len(results[0].Box) != 1 || fmt.Sprint(results[0].Box[0]) != "[1 2 3 4]" {
		t.Fatalf("converted OCR box = %#v", results[0].Box)
	}
	if !adapter.IsAvailable() {
		t.Fatal("adapter should reflect shared runtime availability")
	}
}

func TestKnowledgeImageOCRAdapterNilRuntimeReturnsError(t *testing.T) {
	adapter := knowledgeImageOCRAdapter{}
	results, err := adapter.Recognize("ZmFrZQ==")
	if err == nil || !strings.Contains(err.Error(), "unavailable") || results != nil {
		t.Fatalf("nil OCR runtime = results=%#v err=%v", results, err)
	}
	if adapter.IsAvailable() {
		t.Fatal("nil OCR runtime must not be available")
	}
}

func TestKnowledgeImageOCRAdapterHonorsDisabledSetting(t *testing.T) {
	runtime := &knowledgeImageOCRRuntimeStub{available: true}
	adapter := knowledgeImageOCRAdapter{runtime: runtime, enabled: func() bool { return false }}
	results, err := adapter.Recognize("ZmFrZQ==")
	if err == nil || !strings.Contains(err.Error(), "disabled") || results != nil {
		t.Fatalf("disabled OCR = results=%#v err=%v", results, err)
	}
	if runtime.called || adapter.IsAvailable() {
		t.Fatalf("disabled OCR must not invoke the runtime: called=%v available=%v", runtime.called, adapter.IsAvailable())
	}
}

func TestConfiguredKnowledgeImageDescriberIndexesOCRForImageSearch(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := knowledge.NewImageAssetManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	ocr := &knowledgeImageOCRRuntimeStub{
		available: true,
		results:   []browser.OCRResult{{Text: "Gateway topology production", Confidence: 0.99}},
	}
	(&App{}).configureKnowledgeImageDescriberWithOCR(store, ocr)

	if err := store.SaveSource(ctx, knowledge.Source{
		ID: "gateway-image", Kind: knowledge.SourceKindImage, URI: "file://gateway.png",
		OwnerID: "owner", TenantID: "tenant", Title: "Gateway", Status: knowledge.StatusParsed,
	}); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(t.TempDir(), "gateway.png")
	if err := writeKnowledgeTinyPNG(imagePath); err != nil {
		t.Fatal(err)
	}
	nodes := store.ProcessStandaloneImage(ctx, knowledge.Source{
		ID: "gateway-image", Kind: knowledge.SourceKindImage, URI: "file://gateway.png",
		OwnerID: "owner", TenantID: "tenant", Title: "Gateway", Status: knowledge.StatusParsed,
	}, imagePath, nil)
	if len(nodes) != 1 || !ocr.called {
		t.Fatalf("nodes=%#v OCR_called=%v", nodes, ocr.called)
	}
	if err := store.SaveDocumentNode(ctx, nodes[0]); err != nil {
		t.Fatal(err)
	}
	results, err := store.SearchImages(ctx, knowledge.ImageSearchOptions{SearchOptions: knowledge.SearchOptions{
		Query: "gateway topology", OwnerID: "owner", TenantID: "tenant", Limit: 8,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Media == nil || results[0].Media.AssetID != "gateway-image" {
		t.Fatalf("OCR image search results = %#v", results)
	}
	if results[0].Snippet == "" || !containsFold(results[0].Snippet, "topology") {
		t.Fatalf("OCR text absent from image result: %#v", results[0])
	}
}

func writeKnowledgeTinyPNG(path string) error {
	const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAYAAABytg0kAAAADElEQVR42mP4z8AAAAMBAQDJ/pLvAAAAAElFTkSuQmCC"
	data, err := base64.StdEncoding.DecodeString(pngBase64)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func containsFold(value, wanted string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(wanted))
}

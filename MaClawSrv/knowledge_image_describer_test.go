package main

import (
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func writeKnowledgeImageFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

type srvKnowledgeImageOCRStub struct {
	text string
}

func (s srvKnowledgeImageOCRStub) Recognize(string) ([]knowledge.OCRResult, error) {
	return []knowledge.OCRResult{{Text: s.text, Score: 0.99}}, nil
}
func (srvKnowledgeImageOCRStub) IsAvailable() bool { return true }
func (srvKnowledgeImageOCRStub) Close()            {}

func TestSrvKnowledgeImageDescriberUsesOwningScopeConfig(t *testing.T) {
	ctx := context.Background()
	imagePath := writeKnowledgeImageFixture(t)
	configs := map[string]corelib.AppConfig{
		"tenant-a/user-a": {OCREnabled: true, OCRModelTier: "tiny"},
		"tenant-b/user-b": {OCREnabled: true, OCRModelTier: "medium"},
	}
	var mu sync.Mutex
	var gotScopes, gotTiers []string
	describer := &srvKnowledgeImageDescriber{
		loadConfig: func(_ context.Context, hints knowledge.ImageHints) (corelib.AppConfig, error) {
			key := hints.TenantID + "/" + hints.OwnerID
			mu.Lock()
			gotScopes = append(gotScopes, key)
			mu.Unlock()
			return configs[key], nil
		},
		ocrForTier: func(tier string) knowledge.OCRProvider {
			mu.Lock()
			gotTiers = append(gotTiers, tier)
			mu.Unlock()
			return srvKnowledgeImageOCRStub{text: "ocr-" + tier}
		},
		newVision: knowledge.NewVisionDescriber,
	}
	for _, hints := range []knowledge.ImageHints{
		{TenantID: "tenant-a", OwnerID: "user-a", FileName: "a.png"},
		{TenantID: "tenant-b", OwnerID: "user-b", FileName: "b.png"},
	} {
		desc, err := describer.Describe(ctx, imagePath, hints)
		if err != nil {
			t.Fatalf("Describe: %v", err)
		}
		if want := "ocr-" + configs[hints.TenantID+"/"+hints.OwnerID].OCRModelTier; !strings.Contains(desc.OCRText, want) {
			t.Fatalf("description has wrong OCR result: %#v", desc)
		}
	}
	if got, want := strings.Join(gotScopes, ","), "tenant-a/user-a,tenant-b/user-b"; got != want {
		t.Fatalf("config scopes = %q, want %q", got, want)
	}
	if got, want := strings.Join(gotTiers, ","), "tiny,medium"; got != want {
		t.Fatalf("OCR tiers = %q, want %q", got, want)
	}
}

func TestSrvKnowledgeImageDescriberDoesNotCrossScopeVisionCredentials(t *testing.T) {
	ctx := context.Background()
	imagePath := writeKnowledgeImageFixture(t)
	var visionCalls int
	describer := &srvKnowledgeImageDescriber{
		loadConfig: func(_ context.Context, hints knowledge.ImageHints) (corelib.AppConfig, error) {
			if hints.OwnerID == "unverified" {
				return corelib.AppConfig{OCREnabled: true, KnowledgeVisionLLM: corelib.KnowledgeVisionLLMConfig{Enabled: true, BaseURL: "https://vision.example", APIKey: "other-user-key", Model: "vision", Verified: false}}, nil
			}
			return corelib.AppConfig{OCREnabled: true}, nil
		},
		ocrForTier: func(string) knowledge.OCRProvider { return srvKnowledgeImageOCRStub{text: "safe fallback"} },
		newVision: func(*knowledge.VisionLLMConfig, knowledge.ConfigPersister) *knowledge.VisionDescriber {
			visionCalls++
			return nil
		},
	}
	desc, err := describer.Describe(ctx, imagePath, knowledge.ImageHints{TenantID: "tenant", OwnerID: "unverified", FileName: "diagram.png"})
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if visionCalls != 0 || !strings.Contains(desc.OCRText, "safe fallback") {
		t.Fatalf("unverified vision must not be invoked: calls=%d desc=%#v", visionCalls, desc)
	}
}

func TestKnowledgeImportImageIndexesOCRFromOwningUserConfiguration(t *testing.T) {
	ctx := context.Background()
	dataRoot := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: dataRoot, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	if _, err := svc.UpdateUserConfig(ctx, principal, corelib.AppConfig{OCREnabled: true, OCRModelTier: "tiny"}); err != nil {
		t.Fatal(err)
	}
	store, err := knowledge.NewSQLiteStore(filepath.Join(dataRoot, "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets, err := knowledge.NewImageAssetManager(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	store.SetImageAssetManager(assets)
	ocr := &srvKnowledgeImageOCRStub{text: "zero-trust gateway"}
	describer := newSrvKnowledgeImageDescriber(svc, dataRoot)
	describer.ocrForTier = func(tier string) knowledge.OCRProvider {
		if tier != "tiny" {
			t.Fatalf("OCR tier = %q, want tiny", tier)
		}
		return ocr
	}
	store.SetImageDescriber(describer)

	imagePath := writeKnowledgeImageFixture(t)
	if err := store.SaveSource(ctx, knowledge.Source{ID: "gateway-image", Kind: knowledge.SourceKindImage, URI: "file://gateway.png", OwnerID: user.ID, TenantID: tenant.ID, Title: "Gateway", Status: knowledge.StatusParsed}); err != nil {
		t.Fatal(err)
	}
	nodes := store.ProcessStandaloneImage(ctx, knowledge.Source{ID: "gateway-image", Kind: knowledge.SourceKindImage, URI: "file://gateway.png", OwnerID: user.ID, TenantID: tenant.ID, Title: "Gateway", Status: knowledge.StatusParsed}, imagePath, nil)
	if len(nodes) != 1 || !strings.Contains(nodes[0].Text, "zero-trust gateway") {
		t.Fatalf("image OCR node = %#v", nodes)
	}
	if err := store.SaveDocumentNode(ctx, nodes[0]); err != nil {
		t.Fatal(err)
	}
	results, err := store.SearchImages(ctx, knowledge.ImageSearchOptions{SearchOptions: knowledge.SearchOptions{Query: "zero trust", TenantID: tenant.ID, OwnerID: user.ID, Limit: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Media == nil || results[0].Media.AssetID != "gateway-image" {
		encoded, _ := json.Marshal(results)
		t.Fatalf("OCR image search results = %s", encoded)
	}
}

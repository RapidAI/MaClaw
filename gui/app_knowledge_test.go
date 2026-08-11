package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func knowledgeImageTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return encoded.Bytes()
}

func TestKnowledgeImageAssetOriginalPath(t *testing.T) {
	dataDir := t.TempDir()
	assetDir := filepath.Join(dataDir, "knowledge_assets", "asset-123")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(assetDir, "original.jpg")
	if err := os.WriteFile(want, knowledgeImageTestJPEG(t, 1, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := knowledgeImageAssetOriginalPath(dataDir, "asset-123")
	if err != nil {
		t.Fatalf("resolve canonical original: %v", err)
	}
	if got != want {
		t.Fatalf("canonical original path = %q, want %q", got, want)
	}
	for _, assetID := range []string{"", "../secret", `..\\secret`, "missing", " asset-123", "asset-123 "} {
		if _, err := knowledgeImageAssetOriginalPath(dataDir, assetID); err == nil {
			t.Fatalf("asset ID %q should not resolve", assetID)
		}
	}
}

func TestKnowledgeGetImageAssetPathsDoesNotExposeHostPaths(t *testing.T) {
	homeDir := t.TempDir()
	a := &App{testHomeDir: homeDir}
	store, err := a.openKnowledgeStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assets := store.ImageAssets()
	if assets == nil {
		t.Fatal("image asset manager is not configured")
	}
	if err := store.SaveSource(context.Background(), knowledge.Source{ID: "asset-123", Kind: knowledge.SourceKindImage, URI: "file://diagram", Status: knowledge.StatusParsed}); err != nil {
		t.Fatal(err)
	}
	thumbnail := knowledgeImageTestJPEG(t, 1, 1)
	if _, err := assets.SaveImageFromBytes("asset-123", thumbnail, ".jpg"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(context.Background(), knowledge.DocumentNode{ID: "asset-node", SourceID: "asset-123", Type: knowledge.NodeTypeImage, Metadata: map[string]string{knowledge.MetaImageAssetID: "asset-123"}}); err != nil {
		t.Fatal(err)
	}
	got := a.KnowledgeGetImageAssetPaths("asset-123")
	thumbURL := got["thumb_data_url"]
	const thumbPrefix = "data:image/jpeg;base64,"
	if !strings.HasPrefix(thumbURL, thumbPrefix) {
		t.Fatalf("thumbnail data URL = %q, want JPEG data URL", thumbURL)
	}
	thumbBytes, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(thumbURL, thumbPrefix))
	if err != nil || !knowledge.IsSafeKnowledgeImageThumbnail(thumbBytes) {
		t.Fatalf("thumbnail data URL is not a safe managed JPEG: %v", err)
	}
	if len(got) != 1 || got["preview"] != "" || got["original"] != "" {
		t.Fatalf("display response leaked non-thumbnail values: %#v", got)
	}
}

func TestKnowledgeImageAssetPresentationRejectsInvalidIDsAndThumbnails(t *testing.T) {
	homeDir := t.TempDir()
	a := &App{testHomeDir: homeDir}
	dataDir := a.GetDataDir()
	for _, assetID := range []string{"asset.123", "asset space", "asset@123", "../secret", " asset-123", "asset-123 "} {
		if got := a.KnowledgeGetImageAssetPaths(assetID); len(got) != 0 {
			t.Fatalf("unsafe asset ID %q returned %#v", assetID, got)
		}
		if _, err := knowledgeImageAssetOriginalPath(dataDir, assetID); err == nil {
			t.Fatalf("unsafe asset ID %q resolved an original", assetID)
		}
	}

	store, err := a.openKnowledgeStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveSource(context.Background(), knowledge.Source{ID: "asset-123", Kind: knowledge.SourceKindImage, URI: "file://diagram", Status: knowledge.StatusParsed}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDocumentNode(context.Background(), knowledge.DocumentNode{ID: "asset-node", SourceID: "asset-123", Type: knowledge.NodeTypeImage, Metadata: map[string]string{knowledge.MetaImageAssetID: "asset-123"}}); err != nil {
		t.Fatal(err)
	}
	assetDir := filepath.Join(dataDir, "knowledge_assets", "asset-123")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "thumb_120.jpg"), []byte("not a jpeg"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := a.KnowledgeGetImageAssetPaths("asset-123"); len(got) != 0 {
		t.Fatalf("invalid thumbnail returned %#v", got)
	}
}

func TestKnowledgeImagePresentationRequiresRegisteredAsset(t *testing.T) {
	a := &App{testHomeDir: t.TempDir()}
	dataDir := a.GetDataDir()
	assets, err := knowledge.NewImageAssetManager(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := assets.SaveImageFromBytes("orphan-image", knowledgeImageTestJPEG(t, 1, 1), ".jpg"); err != nil {
		t.Fatal(err)
	}
	if got := a.KnowledgeGetImageAssetPaths("orphan-image"); len(got) != 0 {
		t.Fatalf("orphan asset was exposed to the WebView: %#v", got)
	}
	if err := a.KnowledgeOpenImageAsset("orphan-image"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("orphan asset open error = %v, want authorization rejection", err)
	}
}

func TestKnowledgeImageAssetOriginalPathRejectsNonRasterOriginal(t *testing.T) {
	dataDir := t.TempDir()
	assetDir := filepath.Join(dataDir, "knowledge_assets", "vector-only")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetDir, "original.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := knowledgeImageAssetOriginalPath(dataDir, "vector-only"); err == nil {
		t.Fatal("vector original must not be returned through image open boundary")
	}
}

func TestKnowledgeOpenImageFileRejectsPathBridge(t *testing.T) {
	a := &App{testHomeDir: t.TempDir()}
	assetPath := filepath.Join(a.GetDataDir(), "knowledge_assets", "asset-123", "original.jpg")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, knowledgeImageTestJPEG(t, 1, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	err := a.KnowledgeOpenImageFile(assetPath)
	if err == nil || !strings.Contains(err.Error(), "image asset ID") {
		t.Fatalf("path bridge error = %v, want asset-ID-only rejection", err)
	}
}

func TestOpenKnowledgeStoreWithRetryRetriesLockedErrors(t *testing.T) {
	attempts := 0
	var sleeps []time.Duration
	store, err := openKnowledgeStoreWithRetry(context.Background(), func() (*knowledge.SQLiteStore, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New(`knowledge sqlite pragma "PRAGMA foreign_keys=ON": database is locked (261)`)
		}
		return nil, nil
	}, func(_ context.Context, delay time.Duration) bool {
		sleeps = append(sleeps, delay)
		return true
	})
	if err != nil {
		t.Fatalf("openKnowledgeStoreWithRetry: %v", err)
	}
	if store != nil {
		t.Fatalf("expected nil test store, got %#v", store)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if len(sleeps) != 2 || sleeps[0] != 50*time.Millisecond || sleeps[1] != 100*time.Millisecond {
		t.Fatalf("retry sleeps = %#v, want 50ms then 100ms", sleeps)
	}
}

func TestOpenKnowledgeStoreWithRetryDoesNotRetryPermanentErrors(t *testing.T) {
	attempts := 0
	_, err := openKnowledgeStoreWithRetry(context.Background(), func() (*knowledge.SQLiteStore, error) {
		attempts++
		return nil, errors.New("knowledge sqlite open: permission denied")
	}, func(context.Context, time.Duration) bool {
		t.Fatal("should not sleep for a non-lock error")
		return false
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v, want permission denied", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestOpenKnowledgeStoreWithRetryHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	_, err := openKnowledgeStoreWithRetry(ctx, func() (*knowledge.SQLiteStore, error) {
		attempts++
		return nil, errors.New("database is locked")
	}, func(context.Context, time.Duration) bool {
		t.Fatal("should not sleep after context cancellation")
		return false
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

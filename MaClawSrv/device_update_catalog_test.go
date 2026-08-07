package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

func testFirmwareIdentity(deviceID string) coreim.FirmwareIdentity {
	return coreim.FirmwareIdentity{DeviceID: deviceID, ProductID: "maclaw-clawmate", BoardID: "bread-compact-wifi-lcd-v1", HardwareRev: "1", LayoutID: "maclaw-s3-16m-factory-v2", CompatibilityID: "maclaw-clawmate:bread-compact-wifi-lcd-v1:maclaw-s3-16m-factory-v2", ReleaseSequence: 7, AppVersion: "v7", ELFSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
}

func TestFirmwareUpdateCatalogRequiresBoundIdentityAndReturnsMetadataOnly(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: root, TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Update tenant"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "Update user"})
	if err != nil {
		t.Fatal(err)
	}
	p := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	cfg := testLLMConfig()
	cfg.ThirdPartyGatewayEnabled = true
	cfg.ThirdPartyGatewayToken = "firmware-update-token"
	if _, err := svc.UpdateUserConfig(ctx, p, cfg); err != nil {
		t.Fatal(err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	identity := testFirmwareIdentity("esp32s3-abcdef123456")
	release := srvDeviceUpdateCatalogDocument{SchemaVersion: srvDeviceUpdateCatalogSchemaVersion, Source: "github-release", Repository: srvOfficialFirmwareRepository, ReleaseID: 101, ReleaseTag: "v8.0.0", VerifiedAt: time.Now().UTC().UnixMilli(), MaxAgeSeconds: int(srvReleaseCatalogMaxAge / time.Second), Releases: []srvDeviceUpdateRelease{{ProductID: identity.ProductID, BoardID: identity.BoardID, HardwareRev: identity.HardwareRev, LayoutID: identity.LayoutID, CompatibilityID: identity.CompatibilityID, Channel: srvDeviceUpdateChannel, ReleaseSequence: 8, DisplayVersion: "v8", ReleaseTag: "v8.0.0", PublishedAt: 1770000000000, Severity: "important", MinimumMakerVersion: "1.0.0", PackageID: "bread-v8", ManifestSHA256: "sha256:abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd", ArchiveSHA256: "sha256:abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd", SourceAssetID: 12, SourceAssetName: "MaClaw-ESP32S3-Bread-Compact-firmware.clawfw", SourceAssetSize: 1024, NotesSummary: "安全修复", NotesSHA256: "sha256:abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd", CheckAfterSeconds: 86400}}}
	raw, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, srvDeviceUpdateCatalogFile), raw, 0600); err != nil {
		t.Fatal(err)
	}

	requestBody := func(id coreim.FirmwareIdentity) *bytes.Buffer {
		body, err := json.Marshal(map[string]any{"clientId": id.DeviceID, "protocolVersion": "1.1", "capabilities": map[string]any{"firmwareIdentity": id}})
		if err != nil {
			t.Fatal(err)
		}
		return bytes.NewBuffer(body)
	}
	// The first identity-bearing handshake is rejected before a pairing binding
	// exists. A user bearer alone must not enumerate catalog entries.
	req := httptest.NewRequest(http.MethodPost, "/api/im-gateway/v1/handshake", requestBody(identity))
	req.Header.Set("Authorization", "Bearer firmware-update-token")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden || bytes.Contains(w.Body.Bytes(), []byte("releaseTag")) {
		t.Fatalf("unbound handshake = %d %s", w.Code, w.Body.String())
	}

	if err := server.bindHardwareDeviceForPairing(ctx, srvThirdPartyPrincipal{Principal: p, Config: cfg}, identity); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/im-gateway/v1/handshake", requestBody(identity))
	req.Header.Set("Authorization", "Bearer firmware-update-token")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bound handshake = %d %s", w.Code, w.Body.String())
	}
	var response coreim.ThirdPartyGatewayHandshakeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Update == nil || !response.Update.Available || !response.Update.RequiresComputer || response.Update.ReleaseSequence != 8 || response.Update.ReleaseTag != "v8.0.0" {
		t.Fatalf("update metadata = %#v", response.Update)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("http://")) || bytes.Contains(w.Body.Bytes(), []byte("https://")) || bytes.Contains(w.Body.Bytes(), []byte("browser_download_url")) {
		t.Fatalf("update response leaked download location: %s", w.Body.String())
	}
}

func TestDeviceBindingStoreDoesNotRetainFailedPersistence(t *testing.T) {
	root := t.TempDir()
	store := newSrvDeviceUpdateBindingStore(root)
	// Force saveLocked's directory creation to fail after the in-memory map is
	// updated.  Authorization must continue to see the pre-call state.
	store.path = filepath.Join(root, "not-a-directory", "bindings.json")
	if err := os.WriteFile(filepath.Join(root, "not-a-directory"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	p := srvThirdPartyPrincipal{Principal: agentservice.Principal{TenantID: "tenant-a", UserID: "user-a"}}
	identity := testFirmwareIdentity("esp32s3-persist-failure")
	if err := store.bind(context.Background(), p, identity, true); err == nil {
		t.Fatal("binding unexpectedly persisted through invalid parent path")
	}
	if len(store.data) != 0 {
		t.Fatalf("failed binding remained in memory: %#v", store.data)
	}
	if store.lookup(p, identity) {
		t.Fatal("failed binding became authorized")
	}
}

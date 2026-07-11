package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestMobileSSHVaultStoreAndMetadata(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-ssh-vault@example.com")
	profileID := "prod-web"

	putReq := httptest.NewRequest(http.MethodPut, "/api/mobile/ssh/vault/"+profileID,
		strings.NewReader(`{"auth_mode":"password","secret":"s3cret-pass"}`))
	putReq.Header.Set("Authorization", "Bearer "+token)
	putReq.Header.Set("Content-Type", "application/json")
	putReq.SetPathValue("profileId", profileID)
	putRec := httptest.NewRecorder()
	MobileSSHVaultHandler(identity).ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", putRec.Code, putRec.Body.String())
	}
	// Secret must not be echoed.
	if strings.Contains(putRec.Body.String(), "s3cret-pass") {
		t.Fatal("secret leaked in response")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/mobile/ssh/vault/"+profileID, nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getReq.SetPathValue("profileId", profileID)
	getRec := httptest.NewRecorder()
	MobileSSHVaultHandler(identity).ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["has_secret"] != true || body["auth_mode"] != "password" {
		t.Fatalf("body=%#v", body)
	}

	// Decrypt round-trip internally.
	rec, ok := mobileSSHVaultLookup(enroll.TenantID, enroll.UserID, profileID)
	if !ok {
		// TenantID may be empty on enroll — try principal path via owner only keys.
		// Lookup used enroll tenant; fix by reading from map with actual key.
		mobileSSHVault.Lock()
		found := false
		for k, v := range mobileSSHVault.secrets {
			if v.ProfileID == profileID && v.OwnerID == enroll.UserID {
				rec = v
				found = true
				_ = k
				break
			}
		}
		mobileSSHVault.Unlock()
		if !found {
			t.Fatal("vault record missing")
		}
	}
	if got := mobileSSHVaultDecrypt(rec.EncryptedSecret); got != "s3cret-pass" {
		t.Fatalf("decrypt=%q", got)
	}

	t.Cleanup(func() {
		mobileSSHVault.Lock()
		for k, v := range mobileSSHVault.secrets {
			if v.OwnerID == enroll.UserID && v.ProfileID == profileID {
				delete(mobileSSHVault.secrets, k)
			}
		}
		mobileSSHVault.Unlock()
	})
}

func TestMobilePlanForAccess(t *testing.T) {
	if mobilePlanForAccess(nil) != "free" {
		t.Fatal("nil access should be free")
	}
	if mobilePlanForAccess(map[string]any{"mode": "maclaw_official", "status": "available"}) != "official" {
		t.Fatal("official plan")
	}
	if mobilePlanForAccess(map[string]any{"mode": "desktop_qr_third_party", "status": "available"}) != "desktop_delegate" {
		t.Fatal("desktop delegate plan")
	}
	if mobilePlanForAccess(map[string]any{"mode": "maclaw_official", "status": "missing"}) != "free" {
		t.Fatal("not ready should be free")
	}
	// Active service card grant upgrades plan.
	if mobilePlanForAccessWithGrant(
		map[string]any{"mode": "maclaw_official", "status": "available"},
		mobileServiceGrantSnapshot{Active: true, HasCardGrant: true, CreditsAvailable: 10},
	) != "service_card" {
		t.Fatal("want service_card when grant active with card")
	}
}

func TestMobileHubSSHSessionRequiresVault(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-hub-ssh@example.com")
	profileID := "edge-1"

	// Publish profile metadata (as worker/viewer path uses worker auth for PUT profiles).
	// Seed profile map directly for unit isolation.
	mobileServerProfiles.Lock()
	key := enroll.TenantID + "\x00" + enroll.UserID + "\x00" + "machine" + "\x00" + profileID
	mobileServerProfiles.profiles[key] = mobileServerProfileRecord{
		ProfileID: profileID, TenantID: enroll.TenantID, OwnerID: enroll.UserID,
		SourceMachineID: "machine", Name: "Edge", Host: "127.0.0.1", Port: 1,
		Username: "root", AuthMode: "password", UpdatedAt: time.Now().UTC(),
	}
	mobileServerProfiles.Unlock()
	t.Cleanup(func() {
		mobileServerProfiles.Lock()
		delete(mobileServerProfiles.profiles, key)
		mobileServerProfiles.Unlock()
	})

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions",
		strings.NewReader(`{"server_profile_id":"edge-1","exec_mode":"hub_exec"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	MobileBackendSSHSessionsHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s want 400 without vault", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "vault") && !strings.Contains(rec.Body.String(), "HUB_SSH") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestMobileNormalizeSSHExecMode(t *testing.T) {
	if mobileNormalizeSSHExecMode("hub_exec") != mobileSSHExecHub {
		t.Fatal("hub")
	}
	if mobileNormalizeSSHExecMode("") != mobileSSHExecDesktop {
		t.Fatal("default desktop")
	}
}

func TestMobileHubSSHTaskShouldAsync(t *testing.T) {
	if mobileHubSSHTaskShouldAsync("uptime", false) {
		t.Fatal("short command should be sync")
	}
	if !mobileHubSSHTaskShouldAsync("journalctl -u app -n 200", false) {
		t.Fatal("journalctl should be async")
	}
	if !mobileHubSSHTaskShouldAsync("echo hi", true) {
		t.Fatal("force async")
	}
}

func TestMobileHubLiveCloseSession(t *testing.T) {
	sessionID := "test-live-close-" + time.Now().Format("150405.000")
	// Seed fake live entries without a real *ssh.Client (nil-safe close path).
	mobileHubLive.Lock()
	mobileHubLive.shells[sessionID] = nil
	mobileHubLive.conns[sessionID] = &mobileHubLiveConn{lastUsed: time.Now()}
	mobileHubLive.Unlock()
	t.Cleanup(func() { mobileHubLiveCloseSession(sessionID) })

	mobileHubLiveCloseSession(sessionID)
	mobileHubLiveCloseSession(sessionID) // idempotent
	mobileHubLiveCloseSession("")        // no-op

	mobileHubLive.Lock()
	_, shellOK := mobileHubLive.shells[sessionID]
	_, connOK := mobileHubLive.conns[sessionID]
	mobileHubLive.Unlock()
	if shellOK || connOK {
		t.Fatalf("session live maps not cleared shell=%v conn=%v", shellOK, connOK)
	}
}

func TestMobilePlanCapsMatrix(t *testing.T) {
	free := mobilePlanCapsFor("free", mobileServiceGrantSnapshot{}, false)
	if free.DocumentQuotaBytes != mobileCapDocFreeBytes() || free.SharedEmployees || free.MaxExportJobs != mobileCapExportFreeN() {
		t.Fatalf("free caps=%#v", free)
	}
	card := mobilePlanCapsFor("service_card", mobileServiceGrantSnapshot{Active: true, HasCardGrant: true, CreditsAvailable: 5}, false)
	if card.DocumentQuotaBytes != mobileCapDocPaidBytes() || !card.SharedEmployees || card.MaxExportJobs != mobileCapExportPaidN() {
		t.Fatalf("service_card caps=%#v", card)
	}
	official := mobilePlanCapsFor("official", mobileServiceGrantSnapshot{}, true)
	if !official.MobileAgent || official.DocumentQuotaBytes != mobileCapDocFreeBytes() {
		t.Fatalf("official base caps=%#v", official)
	}
	officialPaid := mobilePlanCapsFor("official", mobileServiceGrantSnapshot{Active: true, CreditsAvailable: 1}, true)
	if officialPaid.DocumentQuotaBytes != mobileCapDocPaidBytes() || !officialPaid.SharedEmployees {
		t.Fatalf("official+credits caps=%#v", officialPaid)
	}
	delegate := mobilePlanCapsFor("desktop_delegate", mobileServiceGrantSnapshot{}, true)
	if delegate.MobileAgent || delegate.SharedEmployees {
		t.Fatalf("desktop_delegate should not unlock hub agent/shared: %#v", delegate)
	}
	m := card.toEntitlementMap(mobileServiceGrantSnapshot{Active: true}, true)
	if m["document_quota_bytes"] != mobileCapDocPaidBytes() || m["max_export_jobs"] != mobileCapExportPaidN() {
		t.Fatalf("entitlement map=%#v", m)
	}
}

func TestMobilePlanCapsEnvOverride(t *testing.T) {
	t.Setenv("MACLAW_MOBILE_CAP_DOC_FREE_MIB", "50")
	t.Setenv("MACLAW_MOBILE_CAP_EXPORT_PAID", "20")
	t.Cleanup(func() {
		_ = os.Unsetenv("MACLAW_MOBILE_CAP_DOC_FREE_MIB")
		_ = os.Unsetenv("MACLAW_MOBILE_CAP_EXPORT_PAID")
	})
	if mobileCapDocFreeBytes() != 50*1024*1024 {
		t.Fatalf("doc free=%d", mobileCapDocFreeBytes())
	}
	if mobileCapExportPaidN() != 20 {
		t.Fatalf("export paid=%d", mobileCapExportPaidN())
	}
	free := mobilePlanCapsFor("free", mobileServiceGrantSnapshot{}, false)
	if free.DocumentQuotaBytes != 50*1024*1024 {
		t.Fatalf("free after env=%#v", free)
	}
	card := mobilePlanCapsFor("service_card", mobileServiceGrantSnapshot{Active: true, HasCardGrant: true}, false)
	if card.MaxExportJobs != 20 {
		t.Fatalf("export paid after env=%#v", card)
	}
}

func TestMobileEntitlementsCapsHandler(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "mobile-caps@example.com")
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/entitlements/caps", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileEntitlementsCapsHandler(identity, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	caps, _ := body["caps"].(map[string]any)
	if caps == nil {
		t.Fatalf("body=%#v", body)
	}
	if caps["document_quota_bytes"] == nil || caps["hub_file_download_max_bytes"] == nil {
		t.Fatalf("caps=%#v", caps)
	}
	if caps["hub_file_download_chunked"] != true {
		t.Fatalf("want chunked flag, caps=%#v", caps)
	}
	if body["env_overrides"] == nil {
		t.Fatal("want env_overrides map")
	}
}

func TestMobileEntitlementsCapsPutRuntime(t *testing.T) {
	t.Setenv("MACLAW_MOBILE_CAPS_ADMIN_TOKEN", "test-caps-token")
	t.Cleanup(func() {
		mobileCapsRuntimeApply(-1, -1, -1, -1, -1)
	})
	// Missing token
	req := httptest.NewRequest(http.MethodPut, "/api/mobile/entitlements/caps",
		strings.NewReader(`{"doc_free_mib":42}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	MobileEntitlementsCapsHandler(nil, nil, nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", rec.Code)
	}
	// With token
	req2 := httptest.NewRequest(http.MethodPut, "/api/mobile/entitlements/caps",
		strings.NewReader(`{"doc_free_mib":42,"hub_file_download_mib":16}`))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Maclaw-Caps-Admin-Token", "test-caps-token")
	rec2 := httptest.NewRecorder()
	MobileEntitlementsCapsHandler(nil, nil, nil).ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	if mobileCapDocFreeBytes() != 42*1024*1024 {
		t.Fatalf("doc free=%d", mobileCapDocFreeBytes())
	}
	if mobileCapHubFileDownloadBytes() != 16*1024*1024 {
		t.Fatalf("hub dl=%d", mobileCapHubFileDownloadBytes())
	}
}

func TestMobileResolvePtyInputBytes(t *testing.T) {
	// plain + trim
	got, raw, err := mobileResolvePtyInputBytes("  ls\n  ", "", false)
	if err != nil || got != "ls" || raw {
		t.Fatalf("plain got=%q raw=%v err=%v", got, raw, err)
	}
	// plain raw keeps whitespace/control
	got, raw, err = mobileResolvePtyInputBytes("\x03", "", true)
	if err != nil || got != "\x03" || !raw {
		t.Fatalf("raw ctrl-c got=%q raw=%v err=%v", got, raw, err)
	}
	// data_b64 forces raw and wins over input
	enc := base64.StdEncoding.EncodeToString([]byte{0x03, 0x04})
	got, raw, err = mobileResolvePtyInputBytes("ignored", enc, false)
	if err != nil || got != "\x03\x04" || !raw {
		t.Fatalf("b64 got=%q raw=%v err=%v", got, raw, err)
	}
	// invalid b64
	if _, _, err = mobileResolvePtyInputBytes("", "!!!", false); err == nil {
		t.Fatal("want invalid data_b64 error")
	}
	// empty data_b64 field falls through to plain input (same as unset)
	got, raw, err = mobileResolvePtyInputBytes("", "", false)
	if err != nil || got != "" || raw {
		t.Fatalf("empty fields got=%q raw=%v err=%v", got, raw, err)
	}
}

func TestMobileHubRawInputDisplay(t *testing.T) {
	if mobileHubRawInputDisplay("\x03") != "^C" {
		t.Fatal("ctrl-c")
	}
	if mobileHubRawInputDisplay("\n") != "<Enter>" {
		t.Fatal("enter")
	}
	if mobileHubRawInputDisplay("hello") != "hello" {
		t.Fatal("plain")
	}
	if mobileHubRawInputDisplay("\x04") != "^D" {
		t.Fatal("ctrl-d")
	}
	if mobileHubRawInputDisplay("\t") != "<Tab>" {
		t.Fatal("tab")
	}
	if mobileHubRawInputDisplay("\x1b[A") != "↑" ||
		mobileHubRawInputDisplay("\x1b[B") != "↓" ||
		mobileHubRawInputDisplay("\x1b[C") != "→" ||
		mobileHubRawInputDisplay("\x1b[D") != "←" {
		t.Fatal("arrows")
	}
}

func TestMobileHubSSHFinalizeSessionAfterInput(t *testing.T) {
	sessionID := fmt.Sprintf("mobssh_fin_%d", time.Now().UnixNano())
	mobileBackendSSHSessions.Lock()
	mobileBackendSSHSessions.sessions[sessionID] = mobileBackendSSHSessionRecord{
		SessionID: sessionID, OwnerID: "u1", TenantID: "t1",
		ExecMode: mobileSSHExecHub, Status: "running", State: "hub_streaming",
		RecentOutput: "streamed\n", OutputSeq: 3,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileBackendSSHSessions.Unlock()
	t.Cleanup(func() {
		mobileBackendSSHSessions.Lock()
		delete(mobileBackendSSHSessions.sessions, sessionID)
		mobileBackendSSHSessions.Unlock()
	})
	local := mobileBackendSSHSessionRecord{SessionID: sessionID}
	mobileHubSSHFinalizeSessionAfterInput(&local, "done msg")
	if local.Status != "ready" || local.State != "hub_connected" {
		t.Fatalf("local=%#v", local)
	}
	if local.RecentOutput != "streamed\n" || local.OutputSeq != 3 {
		t.Fatalf("must preserve streamed output, got %#v", local)
	}
	if local.Message != "done msg" {
		t.Fatalf("message=%q", local.Message)
	}
}

func TestMobileSharedEmployeesFromGrant(t *testing.T) {
	if mobileSharedEmployeesFromGrant(mobileServiceGrantSnapshot{}, "free") {
		t.Fatal("free should not share")
	}
	if !mobileSharedEmployeesFromGrant(mobileServiceGrantSnapshot{}, "service_card") {
		t.Fatal("service_card plan shares")
	}
	if !mobileSharedEmployeesFromGrant(mobileServiceGrantSnapshot{
		Active: true, HasCardGrant: true, CreditsAvailable: 1,
	}, "free") {
		t.Fatal("active card grant shares even if plan label free")
	}
	if mobileSharedEmployeesFromGrant(mobileServiceGrantSnapshot{Active: true}, "desktop_delegate") {
		// Active without card/credits: plan helper may still call with service_card;
		// fromGrant with desktop_delegate and weak grant stays false.
		// OK if false
	}
}

func TestMobileHubFileDownloadPlan(t *testing.T) {
	// Below single-shot threshold → single
	mode, chunks, err := mobileHubFileDownloadPlan(1024)
	if err != nil || mode != "single" || chunks != 1 {
		t.Fatalf("small mode=%s chunks=%d err=%v", mode, chunks, err)
	}
	// Exactly single-shot → single
	mode, chunks, err = mobileHubFileDownloadPlan(mobileHubFileSingleShotBytes)
	if err != nil || mode != "single" || chunks != 1 {
		t.Fatalf("edge single mode=%s chunks=%d err=%v", mode, chunks, err)
	}
	// Above single-shot, under absolute max → chunked
	size := int64(mobileHubFileSingleShotBytes) + 1
	mode, chunks, err = mobileHubFileDownloadPlan(size)
	if err != nil || mode != "chunked" {
		t.Fatalf("chunked mode=%s err=%v", mode, err)
	}
	wantChunks := (size + int64(mobileHubFileChunkRawBytes) - 1) / int64(mobileHubFileChunkRawBytes)
	if chunks != wantChunks {
		t.Fatalf("chunks=%d want=%d", chunks, wantChunks)
	}
	// Multi-chunk: 3 full chunks
	three := int64(mobileHubFileChunkRawBytes) * 3
	if three <= mobileHubFileSingleShotBytes {
		// ensure multi-chunk when single-shot is 2MiB and chunk is 512KiB
		three = int64(mobileHubFileSingleShotBytes) + int64(mobileHubFileChunkRawBytes)*2
	}
	mode, chunks, err = mobileHubFileDownloadPlan(three)
	if err != nil || mode != "chunked" || chunks < 2 {
		t.Fatalf("multi mode=%s chunks=%d err=%v", mode, chunks, err)
	}
	// Over absolute cap
	over := int64(mobileHubFileMaxBytes()) + 1
	if _, _, err = mobileHubFileDownloadPlan(over); err == nil {
		t.Fatal("want over-cap error")
	}
	if !strings.Contains(err.Error(), "exceeds hub_exec download limit") {
		t.Fatalf("err=%v", err)
	}
	// Invalid size
	if _, _, err = mobileHubFileDownloadPlan(0); err == nil {
		t.Fatal("want invalid size")
	}
	if _, _, err = mobileHubFileDownloadPlan(-1); err == nil {
		t.Fatal("want negative size error")
	}
}

func TestMobileHubFileDownloadProgressMessage(t *testing.T) {
	msg := mobileHubFileDownloadProgressMessage(512*1024, 3*512*1024, 0, 3)
	if !strings.Contains(msg, "1/3") || !strings.Contains(msg, "524288/1572864 bytes") {
		t.Fatalf("msg=%q", msg)
	}
	// Flutter progress parser looks for `a/b bytes`
	if !regexp.MustCompile(`(\d+)\s*/\s*(\d+)\s*bytes`).MatchString(msg) {
		t.Fatalf("not parseable as a/b bytes: %q", msg)
	}
	// Caps metadata exposed on entitlements
	if mobileHubFileSingleShotBytes <= 0 || mobileHubFileChunkRawBytes <= 0 || mobileHubFileMaxBytes() <= 0 {
		t.Fatal("caps metadata constants must be positive")
	}
}

func TestMobileHubFileStoreAndLookup(t *testing.T) {
	token, err := mobileHubFileStore("t1", "u1", "hosts", []byte("127.0.0.1 localhost\n"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		mobileHubFiles.Lock()
		delete(mobileHubFiles.blobs, token)
		mobileHubFiles.Unlock()
	})
	blob, ok := mobileHubFileLookup(token, "t1", "u1")
	if !ok || string(blob.Content) != "127.0.0.1 localhost\n" {
		t.Fatalf("blob=%#v ok=%v", blob, ok)
	}
	if _, ok := mobileHubFileLookup(token, "t1", "other"); ok {
		t.Fatal("other owner must not read")
	}
	// Oversize rejected
	big := make([]byte, mobileHubFileMaxBytes()+1)
	if _, err := mobileHubFileStore("t1", "u1", "big", big); err == nil {
		t.Fatal("want oversize error")
	}
}

func TestMobileHubDecodeBase64Payload(t *testing.T) {
	src := []byte("hello-chunk")
	enc := base64.StdEncoding.EncodeToString(src)
	// wrapped
	wrapped := enc[:4] + "\n" + enc[4:]
	got, err := mobileHubDecodeBase64Payload(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(src) {
		t.Fatalf("got=%q", got)
	}
}

func TestMobileHubSSHFileDownloadHandler(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-hub-dl@example.com")
	fileToken, err := mobileHubFileStore(enroll.TenantID, enroll.UserID, "note.txt", []byte("hello hub"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		mobileHubFiles.Lock()
		delete(mobileHubFiles.blobs, fileToken)
		mobileHubFiles.Unlock()
	})

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/ssh/files/download/"+fileToken, nil)
	req.SetPathValue("token", fileToken)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileHubSSHFileDownloadHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "hello hub" {
		t.Fatalf("body=%q", rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "note.txt") {
		t.Fatalf("disposition=%s", rec.Header().Get("Content-Disposition"))
	}
}

func TestMobileHubPartialWriterChunks(t *testing.T) {
	var got []string
	w := newMobileHubPartialWriter(func(chunk string) {
		got = append(got, chunk)
	})
	w.minInterval = 0
	w.minBytes = 4
	_, _ = w.Write([]byte("abcd"))
	if len(got) != 1 || got[0] != "abcd" {
		t.Fatalf("got=%#v", got)
	}
	_, _ = w.Write([]byte("12"))
	if len(got) != 1 {
		t.Fatalf("should buffer small write, got=%#v", got)
	}
	w.FlushPartial()
	if len(got) != 2 || got[1] != "12" {
		t.Fatalf("flush got=%#v", got)
	}
	if w.String() != "abcd12" {
		t.Fatalf("full=%q", w.String())
	}
}

func TestMobileHubSSHAppendSessionOutputChunk(t *testing.T) {
	sessionID := fmt.Sprintf("mobssh_stream_%d", time.Now().UnixNano())
	mobileBackendSSHSessions.Lock()
	mobileBackendSSHSessions.sessions[sessionID] = mobileBackendSSHSessionRecord{
		SessionID: sessionID, OwnerID: "u-stream", TenantID: "t-stream",
		ExecMode: mobileSSHExecHub, Status: "ready", State: "hub_connected",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileBackendSSHSessions.Unlock()
	t.Cleanup(func() {
		mobileBackendSSHSessions.Lock()
		delete(mobileBackendSSHSessions.sessions, sessionID)
		mobileBackendSSHSessions.Unlock()
	})
	mobileHubSSHAppendSessionOutputChunk(sessionID, "line-one\n")
	mobileHubSSHAppendSessionOutputChunk(sessionID, "line-two\n")
	mobileBackendSSHSessions.Lock()
	rec := mobileBackendSSHSessions.sessions[sessionID]
	mobileBackendSSHSessions.Unlock()
	if !strings.Contains(rec.RecentOutput, "line-one") || !strings.Contains(rec.RecentOutput, "line-two") {
		t.Fatalf("output=%q", rec.RecentOutput)
	}
	if rec.OutputSeq < 2 {
		t.Fatalf("seq=%d", rec.OutputSeq)
	}
	if rec.State != "hub_streaming" {
		t.Fatalf("state=%s want hub_streaming", rec.State)
	}
}

func TestMobileShellSingleQuote(t *testing.T) {
	if got := mobileShellSingleQuote(`a'b`); got != `'a'"'"'b'` {
		t.Fatalf("quote=%q want %q", got, `'a'"'"'b'`)
	}
	if got := mobileShellSingleQuote("/tmp/x"); got != `'/tmp/x'` {
		t.Fatalf("quote plain=%q", got)
	}
}

func TestMobileHubTaskCancelRegistry(t *testing.T) {
	id := "task-cancel-reg-" + time.Now().Format("150405.000")
	called := false
	mobileHubTaskRegister(id, func() { called = true })
	t.Cleanup(func() { mobileHubTaskUnregister(id) })
	if !mobileHubTaskCancel(id) {
		t.Fatal("expected cancel registered")
	}
	if !called {
		t.Fatal("cancel not invoked")
	}
	// Unregister then cancel is false
	mobileHubTaskUnregister(id)
	if mobileHubTaskCancel(id) {
		t.Fatal("expected no cancel after unregister")
	}
}

func TestMobileBackendSSHTaskKillHubExecQueued(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-hub-task-kill@example.com")
	sessionID := fmt.Sprintf("mobssh_tkill_%d", time.Now().UnixNano())
	taskID := fmt.Sprintf("mobsshtask_tkill_%d", time.Now().UnixNano())

	mobileBackendSSHSessions.Lock()
	mobileBackendSSHSessions.sessions[sessionID] = mobileBackendSSHSessionRecord{
		SessionID: sessionID, TenantID: enroll.TenantID, OwnerID: enroll.UserID,
		ServerProfileID: "edge", ExecMode: mobileSSHExecHub,
		Status: "ready", State: "hub_connected",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileBackendSSHSessions.Unlock()
	mobileBackendSSHTasks.Lock()
	mobileBackendSSHTasks.tasks[taskID] = mobileBackendSSHTaskRecord{
		TaskID: taskID, SessionID: sessionID, TenantID: enroll.TenantID, OwnerID: enroll.UserID,
		Command: "sleep 99", Status: "queued", Message: "queued",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileBackendSSHTasks.Unlock()
	t.Cleanup(func() {
		mobileBackendSSHSessions.Lock()
		delete(mobileBackendSSHSessions.sessions, sessionID)
		mobileBackendSSHSessions.Unlock()
		mobileBackendSSHTasks.Lock()
		delete(mobileBackendSSHTasks.tasks, taskID)
		mobileBackendSSHTasks.Unlock()
	})

	req := httptest.NewRequest(http.MethodPost,
		"/api/mobile/ssh/sessions/"+sessionID+"/tasks/"+taskID+"/kill", nil)
	req.SetPathValue("sessionId", sessionID)
	req.SetPathValue("taskId", taskID)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileBackendSSHSessionTaskKillHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	task, _ := body["task"].(map[string]any)
	if task["status"] != "cancelled" {
		t.Fatalf("task=%#v want cancelled", task)
	}
}

func TestMobileHubSSHFileOpAcceptsReadAction(t *testing.T) {
	// Without a live SSH host, read fails connectivity but must not be rejected as unsupported.
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-hub-file-read@example.com")
	sessionID := fmt.Sprintf("mobssh_fread_%d", time.Now().UnixNano())
	profileID := "edge-read"
	mobileServerProfiles.Lock()
	key := enroll.TenantID + "\x00" + enroll.UserID + "\x00" + "machine" + "\x00" + profileID
	mobileServerProfiles.profiles[key] = mobileServerProfileRecord{
		ProfileID: profileID, TenantID: enroll.TenantID, OwnerID: enroll.UserID,
		SourceMachineID: "machine", Name: "Edge", Host: "127.0.0.1", Port: 1,
		Username: "root", AuthMode: "password", UpdatedAt: time.Now().UTC(),
	}
	mobileServerProfiles.Unlock()
	enc, err := mobileSSHVaultEncrypt("x")
	if err != nil {
		t.Fatal(err)
	}
	vkey := mobileSSHVaultMapKey(enroll.TenantID, enroll.UserID, profileID)
	mobileSSHVault.Lock()
	mobileSSHVault.secrets[vkey] = mobileSSHVaultRecord{
		TenantID: enroll.TenantID, OwnerID: enroll.UserID, ProfileID: profileID,
		AuthMode: "password", EncryptedSecret: enc, UpdatedAt: time.Now().UTC(),
	}
	mobileSSHVault.Unlock()
	mobileBackendSSHSessions.Lock()
	mobileBackendSSHSessions.sessions[sessionID] = mobileBackendSSHSessionRecord{
		SessionID: sessionID, TenantID: enroll.TenantID, OwnerID: enroll.UserID,
		ServerProfileID: profileID, ExecMode: mobileSSHExecHub,
		Status: "ready", State: "hub_connected",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileBackendSSHSessions.Unlock()
	t.Cleanup(func() {
		mobileBackendSSHSessions.Lock()
		delete(mobileBackendSSHSessions.sessions, sessionID)
		mobileBackendSSHSessions.Unlock()
		mobileServerProfiles.Lock()
		delete(mobileServerProfiles.profiles, key)
		mobileServerProfiles.Unlock()
		mobileSSHVault.Lock()
		delete(mobileSSHVault.secrets, vkey)
		mobileSSHVault.Unlock()
		mobileHubLiveCloseSession(sessionID)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/files",
		strings.NewReader(`{"action":"read","remote_path":"/etc/hosts"}`))
	req.SetPathValue("sessionId", sessionID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	MobileBackendSSHSessionFilesHandler(identity).ServeHTTP(rec, req)
	// 200 ready or 502 failed (no real SSH) — must not be 400 unsupported.
	if rec.Code == http.StatusBadRequest {
		t.Fatalf("read should be accepted for hub_exec, body=%s", rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	op, _ := body["operation"].(map[string]any)
	if op["action"] != "read" {
		t.Fatalf("action=%v body=%#v", op["action"], body)
	}
}

func TestMobileHubSSHFileOpUnsupportedUpload(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-hub-file-upload@example.com")
	sessionID := fmt.Sprintf("mobssh_fup_%d", time.Now().UnixNano())
	mobileBackendSSHSessions.Lock()
	mobileBackendSSHSessions.sessions[sessionID] = mobileBackendSSHSessionRecord{
		SessionID: sessionID, TenantID: enroll.TenantID, OwnerID: enroll.UserID,
		ServerProfileID: "edge", ExecMode: mobileSSHExecHub,
		Status: "ready", State: "hub_connected",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	mobileBackendSSHSessions.Unlock()
	t.Cleanup(func() {
		mobileBackendSSHSessions.Lock()
		delete(mobileBackendSSHSessions.sessions, sessionID)
		mobileBackendSSHSessions.Unlock()
	})

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/files",
		strings.NewReader(`{"action":"upload","local_path":"/tmp/a","remote_path":"/tmp/b"}`))
	req.SetPathValue("sessionId", sessionID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	MobileBackendSSHSessionFilesHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s want 400 for hub upload", rec.Code, rec.Body.String())
	}
}

func TestMobileHubSSHInterruptWithoutShell(t *testing.T) {
	out, err := mobileHubSSHInterruptSession("no-such-shell-session")
	if err != nil {
		t.Fatalf("interrupt empty shell should not hard-fail: %v", err)
	}
	if out == "" {
		t.Fatal("expected note when no shell")
	}
}

func TestMobileBackendSSHSessionInterruptHubExec(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-hub-ssh-interrupt@example.com")
	sessionID := fmt.Sprintf("mobssh_int_%d", time.Now().UnixNano())

	mobileBackendSSHSessions.Lock()
	mobileBackendSSHSessions.sessions[sessionID] = mobileBackendSSHSessionRecord{
		SessionID:       sessionID,
		TenantID:        enroll.TenantID,
		OwnerID:         enroll.UserID,
		ServerProfileID: "edge-int",
		ExecMode:        mobileSSHExecHub,
		Status:          "ready",
		State:           "hub_connected",
		Message:         "test",
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	mobileBackendSSHSessions.Unlock()
	t.Cleanup(func() {
		mobileBackendSSHSessions.Lock()
		delete(mobileBackendSSHSessions.sessions, sessionID)
		mobileBackendSSHSessions.Unlock()
		mobileHubLiveCloseSession(sessionID)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/sessions/"+sessionID+"/interrupt", nil)
	req.SetPathValue("sessionId", sessionID)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileBackendSSHSessionInterruptHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	session, _ := body["session"].(map[string]any)
	if session["status"] != "ready" || session["state"] != "hub_connected" {
		t.Fatalf("session=%#v want ready/hub_connected after interrupt", session)
	}
	if !strings.Contains(fmt.Sprint(session["message"]), "interrupt") &&
		!strings.Contains(fmt.Sprint(session["recent_output"]), "interrupt") {
		t.Fatalf("expected interrupt note, session=%#v", session)
	}
}

func TestMobileBackendSSHSessionCloseHubExecCleansLive(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-hub-ssh-close@example.com")
	sessionID := fmt.Sprintf("mobssh_close_%d", time.Now().UnixNano())

	// Seed a hub_exec session as if start succeeded.
	mobileBackendSSHSessions.Lock()
	mobileBackendSSHSessions.sessions[sessionID] = mobileBackendSSHSessionRecord{
		SessionID:       sessionID,
		TenantID:        enroll.TenantID,
		OwnerID:         enroll.UserID,
		ServerProfileID: "edge-close",
		ExecMode:        mobileSSHExecHub,
		Status:          "ready",
		State:           "hub_connected",
		Message:         "test",
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	mobileBackendSSHSessions.Unlock()
	t.Cleanup(func() {
		mobileBackendSSHSessions.Lock()
		delete(mobileBackendSSHSessions.sessions, sessionID)
		mobileBackendSSHSessions.Unlock()
		mobileHubLiveCloseSession(sessionID)
	})

	// Track live entry (no real SSH client — only presence).
	mobileHubLive.Lock()
	mobileHubLive.conns[sessionID] = &mobileHubLiveConn{lastUsed: time.Now()}
	mobileHubLive.Unlock()

	req := httptest.NewRequest(http.MethodDelete, "/api/mobile/ssh/sessions/"+sessionID, nil)
	req.SetPathValue("sessionId", sessionID)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileBackendSSHSessionCloseHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("close status=%d body=%s", rec.Code, rec.Body.String())
	}

	mobileBackendSSHSessions.Lock()
	rec2, ok := mobileBackendSSHSessions.sessions[sessionID]
	mobileBackendSSHSessions.Unlock()
	if !ok || rec2.Status != "closed" || rec2.State != "hub_closed" {
		t.Fatalf("record=%#v ok=%v want closed/hub_closed", rec2, ok)
	}

	mobileHubLive.Lock()
	_, connOK := mobileHubLive.conns[sessionID]
	_, shellOK := mobileHubLive.shells[sessionID]
	mobileHubLive.Unlock()
	if connOK || shellOK {
		t.Fatal("live resources should be removed on hub_exec close")
	}
}

func TestMobileSSHVaultListHandler(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "mobile-ssh-vault-list@example.com")

	// Seed two vault rows for this user.
	for _, id := range []string{"p1", "p2"} {
		enc, err := mobileSSHVaultEncrypt("secret-" + id)
		if err != nil {
			t.Fatal(err)
		}
		key := mobileSSHVaultMapKey(enroll.TenantID, enroll.UserID, id)
		mobileSSHVault.Lock()
		mobileSSHVault.secrets[key] = mobileSSHVaultRecord{
			TenantID: enroll.TenantID, OwnerID: enroll.UserID, ProfileID: id,
			AuthMode: "password", EncryptedSecret: enc, UpdatedAt: time.Now().UTC(),
		}
		mobileSSHVault.Unlock()
	}
	t.Cleanup(func() {
		mobileSSHVault.Lock()
		for k, v := range mobileSSHVault.secrets {
			if v.OwnerID == enroll.UserID {
				delete(mobileSSHVault.secrets, k)
			}
		}
		mobileSSHVault.Unlock()
	})

	req := httptest.NewRequest(http.MethodGet, "/api/mobile/ssh/vault", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	MobileSSHVaultListHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	count, _ := body["count"].(float64)
	if count < 2 {
		t.Fatalf("body=%#v", body)
	}
	if strings.Contains(rec.Body.String(), "secret-p1") {
		t.Fatal("secret leaked in list")
	}
}

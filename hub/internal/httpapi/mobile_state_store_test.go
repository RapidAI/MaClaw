package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setMobileStatePathForTest(t *testing.T, path string) {
	t.Helper()
	t.Setenv(mobileStatePathEnv, path)
	t.Cleanup(mobileCloseStateDBForTest)
}

func mobileStateDBForTest(t *testing.T, statePath string) *sql.DB {
	t.Helper()
	db, fallback, err := mobileOpenStateDB(statePath)
	if err != nil || fallback {
		t.Fatalf("open mobile state db: fallback=%v err=%v", fallback, err)
	}
	return db
}

func readMobileDraftForTest(t *testing.T, statePath, tenantID, id string) mobileDocumentDraftRecord {
	t.Helper()
	payload, err := mobileStateRowPayloadForTest(mobileStateDBForTest(t, statePath), "drafts", tenantID, id)
	if err != nil {
		t.Fatalf("read draft %q: %v", id, err)
	}
	var record mobileDocumentDraftRecord
	if err := json.Unmarshal([]byte(payload), &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestMobileStateMigrationAndIncrementalReopen(t *testing.T) {
	clearMobileStateForTest(t)
	root := t.TempDir()
	statePath := filepath.Join(root, "custom-mobile.state")
	setMobileStatePathForTest(t, statePath)
	t.Setenv(mobileBlobDirEnv, filepath.Join(root, "blobs"))
	now := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	legacy := mobilePersistentState{
		Drafts: map[string]mobileDocumentDraftRecord{
			"draft-a": {ID: "draft-a", TenantID: "tenant-a", OwnerID: "user-a", Title: "A", Markdown: "# A", SourceBytes: []byte("blob-a"), SourceFilename: "a.txt", UpdatedAt: now},
			"draft-b": {ID: "draft-b", TenantID: "tenant-b", OwnerID: "user-a", Title: "B", Markdown: "# B", UpdatedAt: now},
		},
		Exports: map[string]mobileDocumentExportRecord{
			"export-a": {JobID: "export-a", DraftID: "draft-a", TenantID: "tenant-a", OwnerID: "user-a", Status: "ready", CreatedAt: now},
		},
		Uploads: map[string]mobileDocumentUploadRecord{
			"upload-a": {TaskID: "upload-a", DraftID: "draft-a", TenantID: "tenant-a", OwnerID: "user-a", Status: "ready", SourceBytes: []byte("upload-a"), UpdatedAt: now},
		},
		MeetingRecordings: map[string]mobileMeetingRecording{
			"meeting-a": {ID: "meeting-a", TenantID: "tenant-a", OwnerID: "user-a", Status: "ready", Transcript: "transcript", UpdatedAt: now},
		},
		DigitalEmployeeTasks: map[string]mobileDigitalEmployeeTaskRecord{
			"task-a": {TaskID: "task-a", EmployeeID: "employee-a", TenantID: "tenant-a", OwnerID: "user-a", Status: "done", Result: "result", CreatedAt: now, UpdatedAt: now},
		},
		PushDevices: map[string][]mobilePushDevice{
			mobilePushUserKey("tenant-a", "user-a"): {{Platform: "fcm", Token: "token-a", DeviceID: "device-a", UpdatedAt: now}},
		},
		PushPending: map[string][]mobilePushPendingItem{
			mobilePushUserKey("tenant-a", "user-a"): {{ID: "pending-a", TaskID: "task-a", Status: "done", CreatedAt: time.Now().UTC()}},
		},
		ServerProfiles: map[string]mobileServerProfileRecord{
			"profile-key": {ProfileID: "profile-a", SourceMachineID: "machine-a", TenantID: "tenant-a", OwnerID: "user-a", Host: "example.test", UpdatedAt: now},
		},
		SSHVaultSecrets: map[string]mobileSSHVaultRecord{
			"vault-key": {ProfileID: "profile-a", TenantID: "tenant-a", OwnerID: "user-a", EncryptedSecret: "encrypted-vault-payload", UpdatedAt: now},
		},
		SavedAt: now,
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	mobileResetStatePersistenceForTest()
	mobileEnsureStateLoaded()

	if got := mobileStateDBPath(statePath); got != filepath.Join(root, "custom-mobile.db") {
		t.Fatalf("derived db path = %q", got)
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("legacy state still active: %v", err)
	}
	if _, err := os.Stat(statePath + ".migrated.bak"); err != nil {
		t.Fatalf("migration backup: %v", err)
	}
	for _, table := range mobileEntityTables {
		var count int
		if err := mobileStateDBForTest(t, statePath).QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			t.Fatalf("table %s was not migrated", table)
		}
	}
	persisted := readMobileDraftForTest(t, statePath, "tenant-a", "draft-a")
	if len(persisted.SourceBytes) != 0 || persisted.SourcePath == "" {
		t.Fatalf("draft blob was not externalized: %#v", persisted)
	}
	if blob, err := mobileReadDocumentBlob(persisted.SourcePath); err != nil || string(blob) != "blob-a" {
		t.Fatalf("external draft blob = %q, err=%v", blob, err)
	}

	beforeB, err := mobileStateRowPayloadForTest(mobileStateDBForTest(t, statePath), "drafts", "tenant-b", "draft-b")
	if err != nil {
		t.Fatal(err)
	}
	persisted.Markdown = "# A incremental"
	persisted.UpdatedAt = now.Add(time.Minute)
	mobilePersistDraft(persisted)
	afterB, err := mobileStateRowPayloadForTest(mobileStateDBForTest(t, statePath), "drafts", "tenant-b", "draft-b")
	if err != nil {
		t.Fatal(err)
	}
	if beforeB != afterB {
		t.Fatal("unrelated draft payload changed during incremental write")
	}

	mobileDocuments.Lock()
	mobileDocuments.drafts = make(map[string]mobileDocumentDraftRecord)
	mobileDocuments.exports = make(map[string]mobileDocumentExportRecord)
	mobileDocuments.uploads = make(map[string]mobileDocumentUploadRecord)
	mobileDocuments.Unlock()
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items = make(map[string]mobileMeetingRecording)
	mobileMeetingRecordings.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	mobileDigitalEmployeeTasks.tasks = make(map[string]mobileDigitalEmployeeTaskRecord)
	mobileDigitalEmployeeTasks.Unlock()
	mobileServerProfiles.Lock()
	mobileServerProfiles.profiles = make(map[string]mobileServerProfileRecord)
	mobileServerProfiles.Unlock()
	mobileSSHVault.Lock()
	mobileSSHVault.secrets = make(map[string]mobileSSHVaultRecord)
	mobileSSHVault.Unlock()
	mobileResetStatePersistenceForTest()
	mobileEnsureStateLoaded()

	mobileDocuments.Lock()
	reopened := mobileDocuments.drafts["draft-a"]
	otherTenant := mobileDocuments.drafts["draft-b"]
	reopenedUpload := mobileDocuments.uploads["upload-a"]
	mobileDocuments.Unlock()
	mobileMeetingRecordings.Lock()
	reopenedMeeting := mobileMeetingRecordings.items["meeting-a"]
	mobileMeetingRecordings.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	reopenedTask := mobileDigitalEmployeeTasks.tasks["task-a"]
	mobileDigitalEmployeeTasks.Unlock()
	mobileSSHVault.Lock()
	reopenedVault := mobileSSHVault.secrets[mobileSSHVaultMapKey("tenant-a", "user-a", "profile-a")]
	mobileSSHVault.Unlock()
	if reopened.Markdown != "# A incremental" || otherTenant.TenantID != "tenant-b" || reopenedUpload.TaskID != "upload-a" || reopenedMeeting.Transcript != "transcript" || reopenedTask.Result != "result" {
		t.Fatalf("reopened state mismatch: draft=%#v other=%#v upload=%#v meeting=%#v task=%#v", reopened, otherTenant, reopenedUpload, reopenedMeeting, reopenedTask)
	}
	if reopenedVault.EncryptedSecret != "encrypted-vault-payload" {
		t.Fatalf("vault payload changed: %#v", reopenedVault)
	}
	if devices := mobilePushListDevices("tenant-a", "user-a"); len(devices) != 1 || devices[0].Token != "token-a" {
		t.Fatalf("push devices after reopen = %#v", devices)
	}
	if pending := mobilePushListPending("tenant-a", "user-a"); len(pending) != 1 || pending[0].ID != "pending-a" {
		t.Fatalf("push pending after reopen = %#v", pending)
	}
}

func TestMobileStateMigrationFailureFallsBackToJSON(t *testing.T) {
	clearMobileStateForTest(t)
	root := t.TempDir()
	statePath := filepath.Join(root, "mobile", "state.json")
	setMobileStatePathForTest(t, statePath)
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := mobilePersistentState{Drafts: map[string]mobileDocumentDraftRecord{
		"draft-a": {ID: "draft-a", TenantID: "tenant-a", OwnerID: "user-a", Markdown: "legacy"},
	}}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	mobileStateBeforeMigrationCommitForTest = func() error { return errors.New("injected migration failure") }
	t.Cleanup(func() { mobileStateBeforeMigrationCommitForTest = nil })
	mobileResetStatePersistenceForTest()
	mobileEnsureStateLoaded()
	mobileDocuments.Lock()
	fallbackDraft := mobileDocuments.drafts["draft-a"]
	mobileDocuments.Unlock()
	if fallbackDraft.Markdown != "legacy" {
		t.Fatalf("fallback draft = %#v", fallbackDraft)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("legacy file removed after failed migration: %v", err)
	}
	if _, err := os.Stat(statePath + ".migrated.bak"); !os.IsNotExist(err) {
		t.Fatalf("backup exists after failed migration: %v", err)
	}
	var count int
	db, _, err := mobileOpenStateDB(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM drafts`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed migration committed %d rows", count)
	}

	fallbackDraft.Markdown = "legacy incremental"
	mobileDocuments.Lock()
	mobileDocuments.drafts["draft-a"] = fallbackDraft
	mobileDocuments.Unlock()
	mobilePersistDraft(fallbackDraft)
	fallbackRaw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var fallbackState mobilePersistentState
	if err := json.Unmarshal(fallbackRaw, &fallbackState); err != nil {
		t.Fatal(err)
	}
	if fallbackState.Drafts["draft-a"].Markdown != "legacy incremental" {
		t.Fatalf("JSON fallback did not persist update: %#v", fallbackState.Drafts["draft-a"])
	}

	mobileStateBeforeMigrationCommitForTest = nil
	mobileDocuments.Lock()
	mobileDocuments.drafts = make(map[string]mobileDocumentDraftRecord)
	mobileDocuments.Unlock()
	mobileResetStatePersistenceForTest()
	mobileEnsureStateLoaded()
	mobileDocuments.Lock()
	migrated := mobileDocuments.drafts["draft-a"]
	mobileDocuments.Unlock()
	if migrated.Markdown != "legacy incremental" {
		t.Fatalf("retry migration lost fallback update: %#v", migrated)
	}
	if _, err := os.Stat(statePath + ".migrated.bak"); err != nil {
		t.Fatalf("retry migration backup: %v", err)
	}
}

func TestMobileStateMigrationPreservesReadResponses(t *testing.T) {
	clearMobileStateForTest(t)
	resetMobilePurgeState(t)
	identity, _, _ := newHTTPAPITestServices(t)
	token, enrollment := issueViewerToken(t, identity, "mobile-migration-read@example.com")
	root := t.TempDir()
	statePath := filepath.Join(root, "mobile", "state.json")
	setMobileStatePathForTest(t, statePath)
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)
	tenantID, ownerID := enrollment.TenantID, enrollment.UserID
	legacy := mobilePersistentState{
		Drafts: map[string]mobileDocumentDraftRecord{
			"draft-a": {ID: "draft-a", TenantID: tenantID, OwnerID: ownerID, Title: "Report", Template: "report", Markdown: "# Report\n\nBody", UpdatedAt: now},
		},
		Exports: map[string]mobileDocumentExportRecord{
			"export-a": {JobID: "export-a", DraftID: "draft-a", TenantID: tenantID, OwnerID: ownerID, Format: "markdown", Status: "ready", CreatedAt: now},
		},
		Uploads: map[string]mobileDocumentUploadRecord{
			"upload-a": {TaskID: "upload-a", DraftID: "draft-a", TenantID: tenantID, OwnerID: ownerID, Filename: "report.md", Status: "ready", UploadedAt: now, UpdatedAt: now},
		},
		MeetingRecordings: map[string]mobileMeetingRecording{
			"meeting-a": {ID: "meeting-a", TenantID: tenantID, OwnerID: ownerID, Title: "Meeting", Status: "ready", Transcript: "Transcript", CreatedAt: now, UpdatedAt: now},
		},
		DigitalEmployeeTasks: map[string]mobileDigitalEmployeeTaskRecord{
			"task-a": {TaskID: "task-a", EmployeeID: "employee-a", TenantID: tenantID, OwnerID: ownerID, Status: "done", Result: "Result", CreatedAt: now, UpdatedAt: now},
		},
		PushDevices: map[string][]mobilePushDevice{
			mobilePushUserKey(tenantID, ownerID): {{Platform: "fcm", Token: "token-a", DeviceID: "device-a", UpdatedAt: now}},
		},
		PushPending: map[string][]mobilePushPendingItem{
			mobilePushUserKey(tenantID, ownerID): {{ID: "pending-a", Type: "job", TaskID: "task-a", Status: "done", CreatedAt: time.Now().UTC()}},
		},
		ServerProfiles: map[string]mobileServerProfileRecord{
			"profile-key": {ProfileID: "profile-a", SourceMachineID: "machine-a", TenantID: tenantID, OwnerID: ownerID, Name: "Server", Host: "example.test", Port: 22, Username: "alice", UpdatedAt: now},
		},
		SSHVaultSecrets: map[string]mobileSSHVaultRecord{
			"vault-key": {ProfileID: "profile-a", TenantID: tenantID, OwnerID: ownerID, AuthMode: "password", EncryptedSecret: "encrypted", UpdatedAt: now},
		},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	type readCase struct {
		name       string
		path       string
		pathValues map[string]string
		handler    http.HandlerFunc
	}
	cases := []readCase{
		{name: "draft list", path: "/api/mobile/documents/drafts", handler: MobileDocumentDraftsListHandler(identity)},
		{name: "draft detail", path: "/api/mobile/documents/drafts/draft-a", pathValues: map[string]string{"draftId": "draft-a"}, handler: MobileDocumentDraftsListHandler(identity)},
		{name: "export status", path: "/api/mobile/documents/exports/export-a", pathValues: map[string]string{"jobId": "export-a"}, handler: MobileDocumentExportStatusHandler(identity)},
		{name: "export download", path: "/api/mobile/documents/exports/export-a/download", pathValues: map[string]string{"jobId": "export-a"}, handler: MobileDocumentExportDownloadHandler(identity)},
		{name: "upload status", path: "/api/mobile/documents/upload/upload-a", pathValues: map[string]string{"taskId": "upload-a"}, handler: MobileDocumentUploadStatusHandler(identity)},
		{name: "meeting", path: "/api/mobile/meetings/recordings/meeting-a", pathValues: map[string]string{"recordingId": "meeting-a"}, handler: MobileMeetingRecordingsHandler(identity)},
		{name: "digital employee task", path: "/api/mobile/digital-employees/tasks/task-a", pathValues: map[string]string{"taskId": "task-a"}, handler: MobileDigitalEmployeeTaskStatusHandler(identity)},
		{name: "server profiles", path: "/api/mobile/server-profiles", handler: MobileServerProfilesHandler(identity)},
		{name: "push devices", path: "/api/mobile/push/devices", handler: MobilePushDevicesHandler(identity)},
		{name: "push pending", path: "/api/mobile/push/pending", handler: MobilePushPendingHandler(identity)},
		{name: "ssh vault", path: "/api/mobile/ssh/vault/profile-a", pathValues: map[string]string{"profileId": "profile-a"}, handler: MobileSSHVaultHandler(identity)},
	}
	readAll := func() map[string]string {
		out := make(map[string]string, len(cases))
		for _, tc := range cases {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer "+token)
			for key, value := range tc.pathValues {
				req.SetPathValue(key, value)
			}
			resp := httptest.NewRecorder()
			tc.handler.ServeHTTP(resp, req)
			if resp.Code != http.StatusOK {
				t.Fatalf("%s status = %d body=%s", tc.name, resp.Code, resp.Body.String())
			}
			out[tc.name] = resp.Body.String()
		}
		return out
	}

	mobileStateBeforeMigrationCommitForTest = func() error { return errors.New("force legacy read") }
	mobileResetStatePersistenceForTest()
	mobileEnsureStateLoaded()
	before := readAll()
	mobileStateBeforeMigrationCommitForTest = nil

	mobileDocuments.Lock()
	mobileDocuments.drafts = make(map[string]mobileDocumentDraftRecord)
	mobileDocuments.exports = make(map[string]mobileDocumentExportRecord)
	mobileDocuments.uploads = make(map[string]mobileDocumentUploadRecord)
	mobileDocuments.Unlock()
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items = make(map[string]mobileMeetingRecording)
	mobileMeetingRecordings.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	mobileDigitalEmployeeTasks.tasks = make(map[string]mobileDigitalEmployeeTaskRecord)
	mobileDigitalEmployeeTasks.Unlock()
	mobileServerProfiles.Lock()
	mobileServerProfiles.profiles = make(map[string]mobileServerProfileRecord)
	mobileServerProfiles.Unlock()
	mobileSSHVault.Lock()
	mobileSSHVault.secrets = make(map[string]mobileSSHVaultRecord)
	mobileSSHVault.Unlock()
	mobileResetStatePersistenceForTest()
	mobileEnsureStateLoaded()
	after := readAll()
	for _, tc := range cases {
		if before[tc.name] != after[tc.name] {
			t.Errorf("%s response changed:\nbefore=%s\nafter=%s", tc.name, before[tc.name], after[tc.name])
		}
	}
}

func TestMobileStateLateWritesCannotResurrectPurgedOwner(t *testing.T) {
	resetMobilePurgeState(t)
	t.Cleanup(mobileResetKnowledgePurgeStateForTest)
	statePath := filepath.Join(t.TempDir(), "mobile", "state.json")
	setMobileStatePathForTest(t, statePath)
	const tenantID, ownerID = "tenant-late-write", "user-late-write"
	draft := mobileDocumentDraftRecord{ID: "draft-late", TenantID: tenantID, OwnerID: ownerID, Markdown: "private"}
	mobileDocuments.Lock()
	mobileDocuments.drafts[draft.ID] = draft
	mobileDocuments.Unlock()
	mobilePersistDraft(draft)
	(&UserDataPurger{}).purgeMobileUserData(context.Background(), tenantID, ownerID, nil, func(string, error) {})

	mobileKnowledgePurgeState.Lock()
	mobileKnowledgePurgeState.purged = map[string]struct{}{}
	mobileKnowledgePurgeState.Unlock()
	mobilePersistDraft(draft)
	mobilePersistMeeting(mobileMeetingRecording{ID: "meeting-late", TenantID: tenantID, OwnerID: ownerID})
	mobilePersistDigitalTask(mobileDigitalEmployeeTaskRecord{TaskID: "task-late", TenantID: tenantID, OwnerID: ownerID})
	mobilePersistServerProfiles(tenantID, ownerID, "machine-late", false, []mobileServerProfileRecord{{ProfileID: "profile-late", TenantID: tenantID, OwnerID: ownerID}})
	mobilePersistSSHConfig(mobileSSHVaultRecord{ProfileID: "profile-late", TenantID: tenantID, OwnerID: ownerID, EncryptedSecret: "encrypted"})
	mobilePersistPushUser(tenantID, ownerID)

	db := mobileStateDBForTest(t, statePath)
	for _, table := range mobileEntityTables {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM `+table+` WHERE tenant_id=? AND owner_id=?`, tenantID, ownerID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("late write restored %d rows in %s", count, table)
		}
	}
}

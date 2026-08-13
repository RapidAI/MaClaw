package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestUserDataPurgerRemovesMobileUserDataAndPreservesOtherTenant(t *testing.T) {
	tmp := t.TempDir()
	initMobileCoreAgentForTest(t, tmp)
	statePath := filepath.Join(tmp, "mobile", "state.json")
	t.Setenv(mobileStatePathEnv, statePath)
	t.Setenv(mobileBlobDirEnv, filepath.Join(tmp, "blobs"))

	resetMobilePurgeState(t)
	const tenantA, tenantB, user = "tenant-a", "tenant-b", "user-1"
	otherUser := "user-2"

	draftBlob, err := mobileWriteDocumentBlob(user, "draft", "draft-1", []byte("draft"))
	if err != nil {
		t.Fatal(err)
	}
	uploadBlob, err := mobileWriteDocumentBlob(user, "upload", "upload-1", []byte("upload"))
	if err != nil {
		t.Fatal(err)
	}
	imageBlob, err := mobileWriteDocumentBlob(user, "image", "image-1", []byte("image"))
	if err != nil {
		t.Fatal(err)
	}
	meetingDir := filepath.Join(tmp, "meeting-user")
	if err := os.MkdirAll(meetingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(meetingDir, "audio.wav"), []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}

	mobileDocuments.Lock()
	mobileDocuments.drafts["draft-1"] = mobileDocumentDraftRecord{ID: "draft-1", OwnerID: user, TenantID: tenantA, SourcePath: draftBlob, Images: []mobileDocumentDraftImage{{SourcePath: imageBlob}}}
	mobileDocuments.uploads["upload-1"] = mobileDocumentUploadRecord{TaskID: "upload-1", OwnerID: user, TenantID: tenantA, SourcePath: uploadBlob}
	mobileDocuments.exports["export-1"] = mobileDocumentExportRecord{JobID: "export-1", OwnerID: user, TenantID: tenantA}
	mobileDocuments.drafts["other-draft"] = mobileDocumentDraftRecord{ID: "other-draft", OwnerID: user, TenantID: tenantB}
	mobileDocuments.Unlock()
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items["meeting-1"] = mobileMeetingRecording{ID: "meeting-1", OwnerID: user, TenantID: tenantA, Dir: meetingDir}
	mobileMeetingRecordings.items["meeting-other"] = mobileMeetingRecording{ID: "meeting-other", OwnerID: user, TenantID: tenantB}
	mobileMeetingRecordings.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	mobileDigitalEmployeeTasks.tasks["ve-1"] = mobileDigitalEmployeeTaskRecord{TaskID: "ve-1", OwnerID: user, TenantID: tenantA}
	mobileDigitalEmployeeTasks.tasks["ve-other"] = mobileDigitalEmployeeTaskRecord{TaskID: "ve-other", OwnerID: otherUser, TenantID: tenantA}
	mobileDigitalEmployeeTasks.Unlock()
	mobileDocumentProcessJobs.Lock()
	mobileDocumentProcessJobs.jobs["process-1"] = mobileDocumentProcessJobRecord{JobID: "process-1", OwnerID: user, TenantID: tenantA}
	mobileDocumentProcessJobs.Unlock()
	mobileAgentJobs.Lock()
	mobileAgentJobs.jobs["agent-1"] = mobileAgentJobRecord{JobID: "agent-1", OwnerID: user, TenantID: tenantA}
	mobileAgentJobs.Unlock()
	mobileServerProfiles.Lock()
	mobileServerProfiles.profiles["profile-1"] = mobileServerProfileRecord{ProfileID: "profile-1", OwnerID: user, TenantID: tenantA}
	mobileServerProfiles.profiles["profile-other"] = mobileServerProfileRecord{ProfileID: "profile-other", OwnerID: user, TenantID: tenantB}
	mobileServerProfiles.Unlock()
	mobileSSHVault.Lock()
	mobileSSHVault.secrets["vault-1"] = mobileSSHVaultRecord{OwnerID: user, TenantID: tenantA}
	mobileSSHVault.Unlock()
	mobileLlmAuthorizations.Lock()
	mobileLlmAuthorizations.authorizations["auth-1"] = mobileLlmAuthorizationRecord{OwnerID: user, TenantID: tenantA, APIKey: "secret"}
	mobileLlmAuthorizations.qrSessions["qr-1"] = mobileLlmQRSessionRecord{OwnerID: user, TenantID: tenantA}
	mobileLlmAuthorizations.Unlock()
	mobileBackendSSHSessions.Lock()
	mobileBackendSSHSessions.sessions["session-1"] = mobileBackendSSHSessionRecord{SessionID: "session-1", OwnerID: user, TenantID: tenantA}
	mobileBackendSSHSessions.Unlock()
	mobileBackendSSHTasks.Lock()
	mobileBackendSSHTasks.tasks["task-1"] = mobileBackendSSHTaskRecord{TaskID: "task-1", OwnerID: user, TenantID: tenantA}
	mobileBackendSSHTasks.Unlock()
	mobileBackendSSHFileOperations.Lock()
	mobileBackendSSHFileOperations.operations["op-1"] = mobileBackendSSHFileOperationRecord{OperationID: "op-1", OwnerID: user, TenantID: tenantA}
	mobileBackendSSHFileOperations.Unlock()
	mobilePushState.Lock()
	mobilePushState.devices[mobilePushUserKey(tenantA, user)] = []mobilePushDevice{{Token: "token"}}
	mobilePushState.pending[mobilePushUserKey(tenantA, user)] = []mobilePushPendingItem{{ID: "pending"}}
	mobilePushState.devices[mobilePushUserKey(tenantB, user)] = []mobilePushDevice{{Token: "other"}}
	mobilePushState.Unlock()
	mobileHubFiles.Lock()
	mobileHubFiles.blobs["blob-1"] = mobileHubFileBlob{Token: "blob-1", OwnerID: user, TenantID: tenantA, Content: []byte("private"), ExpiresAt: time.Now().Add(time.Hour)}
	mobileHubFiles.blobs["blob-other"] = mobileHubFileBlob{Token: "blob-other", OwnerID: user, TenantID: tenantB, Content: []byte("other"), ExpiresAt: time.Now().Add(time.Hour)}
	mobileHubFiles.Unlock()

	(&UserDataPurger{}).purgeMobileUserData(context.Background(), tenantA, user, nil, func(area string, err error) {
		t.Errorf("%s: %v", area, err)
	})

	assertMobilePurgeMissing(t, tenantA, user)
	if _, err := os.Stat(filepath.Join(meetingDir, "audio.wav")); !os.IsNotExist(err) {
		t.Fatalf("meeting audio still exists: %v", err)
	}
	for _, rel := range []string{draftBlob, uploadBlob, imageBlob} {
		if _, err := mobileReadDocumentBlob(rel); err == nil {
			t.Fatalf("blob %q still exists", rel)
		}
	}
	mobileDocuments.Lock()
	_, draftKept := mobileDocuments.drafts["other-draft"]
	mobileDocuments.Unlock()
	mobileMeetingRecordings.Lock()
	_, recordingKept := mobileMeetingRecordings.items["meeting-other"]
	mobileMeetingRecordings.Unlock()
	mobileServerProfiles.Lock()
	_, profileKept := mobileServerProfiles.profiles["profile-other"]
	mobileServerProfiles.Unlock()
	mobileHubFiles.Lock()
	_, fileKept := mobileHubFiles.blobs["blob-other"]
	mobileHubFiles.Unlock()
	if !draftKept || !recordingKept || !profileKept || !fileKept {
		t.Fatal("other tenant mobile data was removed")
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state mobilePersistentState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if _, found := state.Drafts["draft-1"]; found {
		t.Fatal("removed draft was written back to state.json")
	}
	if _, found := state.Drafts["other-draft"]; !found {
		t.Fatal("other-tenant draft was not retained in state.json")
	}
}

func TestUserDataPurgerRemovesMobileKnowledgeForUserAndEmailAlias(t *testing.T) {
	initMobileCoreAgentForTest(t, t.TempDir())
	if _, _, err := mobileEnsureCoreAgent(); err != nil {
		t.Fatal(err)
	}
	if mobileKnowledgeStore == nil {
		t.Fatal("mobile knowledge store was not initialized")
	}

	ctx := context.Background()
	const tenantA, tenantB = "tenant-a", "tenant-b"
	const userID, legacyEmail, otherUser = "user-1", "legacy@example.com", "user-2"
	for _, seed := range []struct {
		tenantID string
		ownerID  string
		text     string
	}{
		{tenantA, userID, "current user source"},
		{tenantA, legacyEmail, "legacy email source"},
		{tenantA, otherUser, "other user source"},
		{tenantB, userID, "other tenant source"},
	} {
		if _, err := mobileKnowledgeStore.SaveText(ctx, knowledge.TextSaveRequest{
			Title: seed.text, Text: seed.text, Kind: "mobile_note", TenantID: seed.tenantID, OwnerID: seed.ownerID,
		}); err != nil {
			t.Fatalf("seed knowledge source %q: %v", seed.text, err)
		}
	}

	(&UserDataPurger{}).purgeMobileUserData(ctx, tenantA, userID, []string{legacyEmail}, func(area string, err error) {
		t.Errorf("%s: %v", area, err)
	})

	for _, filter := range []knowledge.ListSourcesOptions{
		{TenantID: tenantA, OwnerID: userID},
		{TenantID: tenantA, OwnerID: legacyEmail},
	} {
		sources, err := mobileKnowledgeStore.ListSources(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		if len(sources) != 0 {
			t.Fatalf("purged sources remain for %+v: %#v", filter, sources)
		}
	}
	for _, filter := range []knowledge.ListSourcesOptions{
		{TenantID: tenantA, OwnerID: otherUser},
		{TenantID: tenantB, OwnerID: userID},
	} {
		sources, err := mobileKnowledgeStore.ListSources(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		if len(sources) != 1 {
			t.Fatalf("unrelated sources were removed for %+v: %#v", filter, sources)
		}
	}

	if _, err := mobileSaveKnowledgeForOwner(ctx, tenantA, userID, knowledge.TextSaveRequest{
		Title: "stale token", Text: "must not recreate deleted knowledge", Kind: "mobile_note", TenantID: tenantA, OwnerID: userID,
	}); err != errMobileOwnerPurged {
		t.Fatalf("save after purge error = %v, want %v", err, errMobileOwnerPurged)
	}
}

func TestUserDataPurgerBlocksStaleMobileStateWrites(t *testing.T) {
	resetMobilePurgeState(t)
	const tenantID, userID = "tenant-a", "user-1"
	(&UserDataPurger{}).purgeMobileUserData(context.Background(), tenantID, userID, nil, func(area string, err error) {
		t.Errorf("%s: %v", area, err)
	})

	mobilePushUpsertDevice(tenantID, userID, mobilePushDevice{Platform: "fcm", Token: "stale-device"})
	pending := mobilePushEnqueue(tenantID, userID, mobilePushPendingItem{Type: "job", Title: "stale"})
	if pending.ID != "" {
		t.Fatalf("purged user push was enqueued: %#v", pending)
	}
	if got := mobilePushListDevices(tenantID, userID); len(got) != 0 {
		t.Fatalf("purged user push device was stored: %#v", got)
	}
	if client, cleanup := mobileRealtimeRegister(tenantID, userID, &mobileRealtimeFakeWriter{}); client != nil {
		cleanup()
		t.Fatal("purged user realtime client was registered")
	}
	// A terminal worker event may arrive after the purger removed its job entry.
	// It must neither repopulate the offline queue nor issue a remote push.
	mobilePushOnRealtimeEvent(tenantID, userID, map[string]any{
		"type": "agent_job", "status": "completed", "job_id": "stale-job",
	}, 0)
	if items := mobilePushListPending(tenantID, userID); len(items) != 0 {
		t.Fatalf("purged user received stale completion push: %#v", items)
	}
}

func TestUserDataPurgerRemovesMobileAgentRuntimeData(t *testing.T) {
	root := t.TempDir()
	initMobileCoreAgentForTest(t, root)
	resetMobilePurgeState(t)
	const tenantID, userID = "tenant-agent-purge", "user-agent-purge"
	ctx := context.Background()
	_, svc, err := mobileEnsureCoreAgent()
	if err != nil {
		t.Fatal(err)
	}
	principal := agentservice.Principal{TenantID: tenantID, UserID: userID, Roles: []string{"mobile"}}
	if err := svc.EnsurePrincipal(ctx, principal, "purged-agent@example.com", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateStructuredRecord(ctx, principal, agentservice.CreateStructuredRecordInput{
		Collection: "private_notes",
		Title:      "Private note",
		Data:       map[string]any{"text": "must be deleted"},
	}); err != nil {
		t.Fatal(err)
	}
	userDir := filepath.Join(root, "mobile-agent", "tenants", tenantID, "users", userID)
	if _, err := os.Stat(userDir); err != nil {
		t.Fatalf("agent user directory missing before purge: %v", err)
	}
	privateWorkspaceFile := filepath.Join(userDir, "data", "workspace", "private.txt")
	if err := os.MkdirAll(filepath.Dir(privateWorkspaceFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(privateWorkspaceFile, []byte("private workspace data"), 0o600); err != nil {
		t.Fatal(err)
	}

	(&UserDataPurger{}).purgeMobileUserData(ctx, tenantID, userID, nil, func(area string, err error) {
		t.Errorf("%s: %v", area, err)
	})
	if _, err := svc.GetUserConfig(ctx, principal); !errors.Is(err, agentservice.ErrUserNotFound) {
		t.Fatalf("agent config still exists after purge: %v", err)
	}
	if records, err := svc.ListStructuredRecords(ctx, principal, agentservice.ListStructuredRecordsInput{Collection: "private_notes"}); err != nil || len(records) != 0 {
		t.Fatalf("agent structured records after purge = %#v, err=%v", records, err)
	}
	if events, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{TenantID: tenantID, UserID: userID}); err != nil || len(events) != 0 {
		t.Fatalf("agent audit events after purge = %#v, err=%v", events, err)
	}
	if _, err := os.Stat(userDir); !os.IsNotExist(err) {
		t.Fatalf("agent user directory remains after purge: %v", err)
	}
}

func TestUserDataPurgerSerializesWithMobileRuntimeWrites(t *testing.T) {
	resetMobilePurgeState(t)
	const tenantID, userID = "tenant-race", "user-race"
	const attempts = 64

	start := make(chan struct{})
	var writers sync.WaitGroup
	for i := 0; i < attempts; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			mobilePushUpsertDevice(tenantID, userID, mobilePushDevice{Platform: "fcm", Token: "race-device"})
			mobilePushEnqueue(tenantID, userID, mobilePushPendingItem{Type: "job", Title: "race"})
			_, cleanup := mobileRealtimeRegister(tenantID, userID, &mobileRealtimeFakeWriter{})
			defer cleanup()
		}()
	}
	close(start)
	(&UserDataPurger{}).purgeMobileUserData(context.Background(), tenantID, userID, nil, func(area string, err error) {
		t.Errorf("%s: %v", area, err)
	})
	writers.Wait()

	mobilePushState.Lock()
	devices := mobilePushState.devices[mobilePushUserKey(tenantID, userID)]
	pending := mobilePushState.pending[mobilePushUserKey(tenantID, userID)]
	mobilePushState.Unlock()
	if len(devices) != 0 || len(pending) != 0 {
		t.Fatalf("runtime state was restored after purge: devices=%#v pending=%#v", devices, pending)
	}
	mobileRealtimeClients.Lock()
	clients := mobileRealtimeClients.clients[mobileRealtimeKey(tenantID, userID)]
	mobileRealtimeClients.Unlock()
	if len(clients) != 0 {
		t.Fatalf("realtime clients were restored after purge: %d", len(clients))
	}
}

func TestUserDataPurgerPreventsMeetingWorkerFromRestoringDeletedData(t *testing.T) {
	resetMobilePurgeState(t)
	const tenantID, userID, recordingID = "tenant-meeting-purge", "user-meeting-purge", "meeting-purge"
	stale := mobileMeetingRecording{ID: recordingID, OwnerID: userID, TenantID: tenantID, Title: "Private meeting"}

	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[recordingID] = stale
	mobileMeetingRecordings.Unlock()
	mobileMarkOwnersPurged(tenantID, map[string]struct{}{userID: {}})
	mobileMeetingRecordings.Lock()
	delete(mobileMeetingRecordings.items, recordingID)
	mobileMeetingRecordings.Unlock()

	if err := mobileStoreMeetingResultDocuments(stale, "private transcript", "private minutes"); err != nil {
		t.Fatal(err)
	}
	mobileMeetingRecordingUpdate(recordingID, func(rec *mobileMeetingRecording) {
		rec.Status = "ready"
	})

	mobileDocuments.Lock()
	for _, draft := range mobileDocuments.drafts {
		if draft.OwnerID == userID && mobileMeetingRecordingTenantMatches(tenantID, draft.TenantID) {
			mobileDocuments.Unlock()
			t.Fatalf("stale meeting worker restored draft: %#v", draft)
		}
	}
	mobileDocuments.Unlock()
	mobileMeetingRecordings.Lock()
	_, found := mobileMeetingRecordings.items[recordingID]
	mobileMeetingRecordings.Unlock()
	if found {
		t.Fatal("stale meeting worker restored recording")
	}
}

func TestUserDataPurgerMakesCleanStateSnapshotWinOverOlderSnapshot(t *testing.T) {
	resetMobilePurgeState(t)
	statePath := filepath.Join(t.TempDir(), "mobile", "state.json")
	t.Setenv(mobileStatePathEnv, statePath)
	const tenantID, userID, draftID = "tenant-state-purge", "user-state-purge", "draft-state-purge"

	mobileDocuments.Lock()
	mobileDocuments.drafts[draftID] = mobileDocumentDraftRecord{ID: draftID, OwnerID: userID, TenantID: tenantID, Markdown: "private"}
	mobileDocuments.Unlock()

	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	mobilePersistStateBeforeRenameForTest = func() {
		once.Do(func() {
			close(entered)
			<-release
		})
	}
	t.Cleanup(func() { mobilePersistStateBeforeRenameForTest = nil })

	stalePersisted := make(chan struct{})
	go func() {
		mobilePersistState()
		close(stalePersisted)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("stale snapshot did not reach durable-write barrier")
	}

	purgeDone := make(chan struct{})
	go func() {
		(&UserDataPurger{}).purgeMobileUserData(context.Background(), tenantID, userID, nil, func(string, error) {})
		close(purgeDone)
	}()
	close(release)
	select {
	case <-stalePersisted:
	case <-time.After(5 * time.Second):
		t.Fatal("stale snapshot did not finish")
	}
	select {
	case <-purgeDone:
	case <-time.After(10 * time.Second):
		t.Fatal("purge did not finish")
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state mobilePersistentState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if _, found := state.Drafts[draftID]; found {
		t.Fatal("stale state snapshot restored purged draft")
	}
}

func TestPurgedMobileOwnerCannotRecreateDraftMeetingOrSSHState(t *testing.T) {
	resetMobilePurgeState(t)
	const tenantID, userID = "tenant-stale-writer", "user-stale-writer"
	mobileMarkOwnersPurged(tenantID, map[string]struct{}{userID: {}})

	draft := mobileDocumentDraftRecord{ID: "stale-draft", OwnerID: userID, TenantID: tenantID, Markdown: "private"}
	upload := mobileDocumentUploadRecord{TaskID: "stale-upload", OwnerID: userID, TenantID: tenantID}
	if payload := mobileStoreDraftAndUpload(draft, &upload, false); payload != nil {
		t.Fatalf("purged owner draft write succeeded: %#v", payload)
	}
	mobileDocuments.Lock()
	_, draftFound := mobileDocuments.drafts[draft.ID]
	_, uploadFound := mobileDocuments.uploads[upload.TaskID]
	mobileDocuments.Unlock()
	if draftFound || uploadFound {
		t.Fatal("purged owner restored document state")
	}

	if _, err := mobileSSHQuickConnectStore(&auth.ViewerPrincipal{TenantID: tenantID, UserID: userID}, "example.test", "alice", "secret", 22, ""); err == nil {
		t.Fatal("purged owner stored SSH credentials")
	}
	mobileServerProfiles.Lock()
	profileCount := len(mobileServerProfiles.profiles)
	mobileServerProfiles.Unlock()
	mobileSSHVault.Lock()
	vaultCount := len(mobileSSHVault.secrets)
	mobileSSHVault.Unlock()
	if profileCount != 0 || vaultCount != 0 {
		t.Fatalf("purged owner restored SSH state: profiles=%d vault=%d", profileCount, vaultCount)
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"title":"private","content_type":"audio/wav"}`))
	resp := httptest.NewRecorder()
	mobileMeetingRecordingCreateWithHardware(resp, req, &auth.ViewerPrincipal{TenantID: tenantID, UserID: userID}, userID, "")
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("meeting create after purge status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
	mobileMeetingRecordings.Lock()
	meetingCount := len(mobileMeetingRecordings.items)
	mobileMeetingRecordings.Unlock()
	if meetingCount != 0 {
		t.Fatal("purged owner restored meeting state")
	}
}

func TestPurgedMobileOwnerCannotPublishServerProfilesOrFiles(t *testing.T) {
	resetMobilePurgeState(t)
	identity, _, _ := newHTTPAPITestServices(t)
	viewerToken, enrollment := issueViewerToken(t, identity, "mobile-purged-server-profile@example.com")
	mobileMarkOwnersPurged(enrollment.TenantID, map[string]struct{}{enrollment.UserID: {}})
	t.Cleanup(mobileResetKnowledgePurgeStateForTest)

	request := httptest.NewRequest(http.MethodPost, "/api/mobile/server-profiles", strings.NewReader(`{
		"profiles":[{"id":"private-host","host":"example.test","port":22,"username":"alice"}]
	}`))
	request.Header.Set("Authorization", "Bearer "+viewerToken)
	response := httptest.NewRecorder()
	MobileServerProfilesHandler(identity).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("profile publish after purge status = %d body=%s, want unauthorized", response.Code, response.Body.String())
	}

	mobileServerProfiles.Lock()
	profileCount := len(mobileServerProfiles.profiles)
	mobileServerProfiles.Unlock()
	if profileCount != 0 {
		t.Fatalf("purged owner restored server profiles: %d", profileCount)
	}

	if _, err := mobileHubFileStore(enrollment.TenantID, enrollment.UserID, "private.txt", []byte("private")); err == nil {
		t.Fatal("purged owner stored a temporary SSH download")
	}
	mobileHubFiles.Lock()
	fileCount := len(mobileHubFiles.blobs)
	mobileHubFiles.Unlock()
	if fileCount != 0 {
		t.Fatalf("purged owner restored temporary SSH downloads: %d", fileCount)
	}
}

func TestPurgedMobileOwnerCannotReceiveHubSSHWorkerUpdates(t *testing.T) {
	resetMobilePurgeState(t)
	const tenantID, userID = "tenant-stale-ssh-worker", "user-stale-ssh-worker"
	const sessionID, taskID, operationID = "session-stale", "task-stale", "operation-stale"

	mobileBackendSSHSessions.Lock()
	mobileBackendSSHSessions.sessions[sessionID] = mobileBackendSSHSessionRecord{SessionID: sessionID, OwnerID: userID, TenantID: tenantID, Status: "running"}
	mobileBackendSSHSessions.Unlock()
	mobileBackendSSHTasks.Lock()
	mobileBackendSSHTasks.tasks[taskID] = mobileBackendSSHTaskRecord{TaskID: taskID, OwnerID: userID, TenantID: tenantID, Status: "running"}
	mobileBackendSSHTasks.Unlock()
	mobileBackendSSHFileOperations.Lock()
	mobileBackendSSHFileOperations.operations[operationID] = mobileBackendSSHFileOperationRecord{OperationID: operationID, OwnerID: userID, TenantID: tenantID, Status: "running"}
	mobileBackendSSHFileOperations.Unlock()

	mobileMarkOwnersPurged(tenantID, map[string]struct{}{userID: {}})
	mobileHubSSHAppendSessionOutputChunk(sessionID, "private output")
	if mobileUpdateHubSSHTaskIfPresent(&mobileBackendSSHTaskRecord{TaskID: taskID, OwnerID: userID, TenantID: tenantID, Status: "ready"}) {
		t.Fatal("purged owner hub SSH task was updated")
	}
	if mobileUpdateHubSSHFileOperationIfPresent(&mobileBackendSSHFileOperationRecord{OperationID: operationID, OwnerID: userID, TenantID: tenantID, Status: "ready"}) {
		t.Fatal("purged owner hub SSH file operation was updated")
	}

	mobileBackendSSHSessions.Lock()
	session := mobileBackendSSHSessions.sessions[sessionID]
	mobileBackendSSHSessions.Unlock()
	if session.RecentOutput != "" || session.OutputChunk != "" || session.OutputSeq != 0 {
		t.Fatalf("purged owner hub SSH session was updated: %#v", session)
	}
	mobileBackendSSHTasks.Lock()
	task := mobileBackendSSHTasks.tasks[taskID]
	mobileBackendSSHTasks.Unlock()
	if task.Status != "running" {
		t.Fatalf("purged owner hub SSH task state changed: %#v", task)
	}
	mobileBackendSSHFileOperations.Lock()
	op := mobileBackendSSHFileOperations.operations[operationID]
	mobileBackendSSHFileOperations.Unlock()
	if op.Status != "running" {
		t.Fatalf("purged owner hub SSH file operation state changed: %#v", op)
	}
}

func assertMobilePurgeMissing(t *testing.T, tenantID, userID string) {
	t.Helper()
	matches := func(ownerID, recordTenantID string) bool {
		return ownerID == userID && mobileMeetingRecordingTenantMatches(tenantID, recordTenantID)
	}
	mobileDocuments.Lock()
	for _, record := range mobileDocuments.drafts {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("draft remains")
		}
	}
	for _, record := range mobileDocuments.uploads {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("upload remains")
		}
	}
	for _, record := range mobileDocuments.exports {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("export remains")
		}
	}
	mobileDocuments.Unlock()
	mobileMeetingRecordings.Lock()
	for _, record := range mobileMeetingRecordings.items {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("meeting remains")
		}
	}
	mobileMeetingRecordings.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	for _, record := range mobileDigitalEmployeeTasks.tasks {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("digital task remains")
		}
	}
	mobileDigitalEmployeeTasks.Unlock()
	mobileDocumentProcessJobs.Lock()
	for _, record := range mobileDocumentProcessJobs.jobs {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("process job remains")
		}
	}
	mobileDocumentProcessJobs.Unlock()
	mobileAgentJobs.Lock()
	for _, record := range mobileAgentJobs.jobs {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("agent job remains")
		}
	}
	mobileAgentJobs.Unlock()
	mobileServerProfiles.Lock()
	for _, record := range mobileServerProfiles.profiles {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("profile remains")
		}
	}
	mobileServerProfiles.Unlock()
	mobileSSHVault.Lock()
	for _, record := range mobileSSHVault.secrets {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("vault secret remains")
		}
	}
	mobileSSHVault.Unlock()
	mobileLlmAuthorizations.Lock()
	for _, record := range mobileLlmAuthorizations.authorizations {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("LLM authorization remains")
		}
	}
	for _, record := range mobileLlmAuthorizations.qrSessions {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("LLM QR session remains")
		}
	}
	mobileLlmAuthorizations.Unlock()
	mobileBackendSSHSessions.Lock()
	for _, record := range mobileBackendSSHSessions.sessions {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("SSH session remains")
		}
	}
	mobileBackendSSHSessions.Unlock()
	mobileBackendSSHTasks.Lock()
	for _, record := range mobileBackendSSHTasks.tasks {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("SSH task remains")
		}
	}
	mobileBackendSSHTasks.Unlock()
	mobileBackendSSHFileOperations.Lock()
	for _, record := range mobileBackendSSHFileOperations.operations {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("SSH file operation remains")
		}
	}
	mobileBackendSSHFileOperations.Unlock()
	mobilePushState.Lock()
	_, deviceFound := mobilePushState.devices[mobilePushUserKey(tenantID, userID)]
	_, pendingFound := mobilePushState.pending[mobilePushUserKey(tenantID, userID)]
	mobilePushState.Unlock()
	if deviceFound || pendingFound {
		t.Fatal("push state remains")
	}
	mobileHubFiles.Lock()
	for _, record := range mobileHubFiles.blobs {
		if matches(record.OwnerID, record.TenantID) {
			t.Fatal("temporary SSH download remains")
		}
	}
	mobileHubFiles.Unlock()
}

func resetMobilePurgeState(t *testing.T) {
	t.Helper()
	mobileDocuments.Lock()
	oldDrafts, oldExports, oldUploads := mobileDocuments.drafts, mobileDocuments.exports, mobileDocuments.uploads
	mobileDocuments.drafts = make(map[string]mobileDocumentDraftRecord)
	mobileDocuments.exports = make(map[string]mobileDocumentExportRecord)
	mobileDocuments.uploads = make(map[string]mobileDocumentUploadRecord)
	mobileDocuments.Unlock()
	mobileMeetingRecordings.Lock()
	oldMeetings := mobileMeetingRecordings.items
	mobileMeetingRecordings.items = make(map[string]mobileMeetingRecording)
	mobileMeetingRecordings.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	oldVE := mobileDigitalEmployeeTasks.tasks
	mobileDigitalEmployeeTasks.tasks = make(map[string]mobileDigitalEmployeeTaskRecord)
	mobileDigitalEmployeeTasks.Unlock()
	mobileDocumentProcessJobs.Lock()
	oldProcess := mobileDocumentProcessJobs.jobs
	mobileDocumentProcessJobs.jobs = make(map[string]mobileDocumentProcessJobRecord)
	mobileDocumentProcessJobs.Unlock()
	mobileAgentJobs.Lock()
	oldAgent := mobileAgentJobs.jobs
	mobileAgentJobs.jobs = make(map[string]mobileAgentJobRecord)
	mobileAgentJobs.Unlock()
	mobileServerProfiles.Lock()
	oldProfiles := mobileServerProfiles.profiles
	mobileServerProfiles.profiles = make(map[string]mobileServerProfileRecord)
	mobileServerProfiles.Unlock()
	mobileSSHVault.Lock()
	oldVault := mobileSSHVault.secrets
	mobileSSHVault.secrets = make(map[string]mobileSSHVaultRecord)
	mobileSSHVault.Unlock()
	mobileLlmAuthorizations.Lock()
	oldAuthorizations, oldQR := mobileLlmAuthorizations.authorizations, mobileLlmAuthorizations.qrSessions
	mobileLlmAuthorizations.authorizations = make(map[string]mobileLlmAuthorizationRecord)
	mobileLlmAuthorizations.qrSessions = make(map[string]mobileLlmQRSessionRecord)
	mobileLlmAuthorizations.Unlock()
	mobileBackendSSHSessions.Lock()
	oldSessions := mobileBackendSSHSessions.sessions
	mobileBackendSSHSessions.sessions = make(map[string]mobileBackendSSHSessionRecord)
	mobileBackendSSHSessions.Unlock()
	mobileBackendSSHTasks.Lock()
	oldTasks := mobileBackendSSHTasks.tasks
	mobileBackendSSHTasks.tasks = make(map[string]mobileBackendSSHTaskRecord)
	mobileBackendSSHTasks.Unlock()
	mobileBackendSSHFileOperations.Lock()
	oldOperations := mobileBackendSSHFileOperations.operations
	mobileBackendSSHFileOperations.operations = make(map[string]mobileBackendSSHFileOperationRecord)
	mobileBackendSSHFileOperations.Unlock()
	mobilePushState.Lock()
	oldDevices, oldPending, oldLastPush := mobilePushState.devices, mobilePushState.pending, mobilePushState.lastPush
	mobilePushState.devices = make(map[string][]mobilePushDevice)
	mobilePushState.pending = make(map[string][]mobilePushPendingItem)
	mobilePushState.lastPush = make(map[string]time.Time)
	mobilePushState.Unlock()
	mobileHubFiles.Lock()
	oldFiles := mobileHubFiles.blobs
	mobileHubFiles.blobs = make(map[string]mobileHubFileBlob)
	mobileHubFiles.Unlock()
	t.Cleanup(func() {
		mobileDocuments.Lock()
		mobileDocuments.drafts, mobileDocuments.exports, mobileDocuments.uploads = oldDrafts, oldExports, oldUploads
		mobileDocuments.Unlock()
		mobileMeetingRecordings.Lock()
		mobileMeetingRecordings.items = oldMeetings
		mobileMeetingRecordings.Unlock()
		mobileDigitalEmployeeTasks.Lock()
		mobileDigitalEmployeeTasks.tasks = oldVE
		mobileDigitalEmployeeTasks.Unlock()
		mobileDocumentProcessJobs.Lock()
		mobileDocumentProcessJobs.jobs = oldProcess
		mobileDocumentProcessJobs.Unlock()
		mobileAgentJobs.Lock()
		mobileAgentJobs.jobs = oldAgent
		mobileAgentJobs.Unlock()
		mobileServerProfiles.Lock()
		mobileServerProfiles.profiles = oldProfiles
		mobileServerProfiles.Unlock()
		mobileSSHVault.Lock()
		mobileSSHVault.secrets = oldVault
		mobileSSHVault.Unlock()
		mobileLlmAuthorizations.Lock()
		mobileLlmAuthorizations.authorizations, mobileLlmAuthorizations.qrSessions = oldAuthorizations, oldQR
		mobileLlmAuthorizations.Unlock()
		mobileBackendSSHSessions.Lock()
		mobileBackendSSHSessions.sessions = oldSessions
		mobileBackendSSHSessions.Unlock()
		mobileBackendSSHTasks.Lock()
		mobileBackendSSHTasks.tasks = oldTasks
		mobileBackendSSHTasks.Unlock()
		mobileBackendSSHFileOperations.Lock()
		mobileBackendSSHFileOperations.operations = oldOperations
		mobileBackendSSHFileOperations.Unlock()
		mobilePushState.Lock()
		mobilePushState.devices, mobilePushState.pending, mobilePushState.lastPush = oldDevices, oldPending, oldLastPush
		mobilePushState.Unlock()
		mobileHubFiles.Lock()
		mobileHubFiles.blobs = oldFiles
		mobileHubFiles.Unlock()
	})
}

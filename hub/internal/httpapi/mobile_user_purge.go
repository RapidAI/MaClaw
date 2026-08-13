package httpapi

import (
	"context"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// purgeMobileUserData removes Mobile state that lives outside SQLite. Its
// persistent maps are rewritten to state.json, preventing data resurrection
// after an account is unbound and the Hub restarts.
func (p *UserDataPurger) purgeMobileUserData(ctx context.Context, tenantID, userID string, ownerAliases []string, logErr func(string, error)) {
	ownerIDs := mobilePurgeOwnerIDs(userID, ownerAliases)
	if len(ownerIDs) == 0 {
		return
	}
	mobileMarkOwnersPurged(tenantID, ownerIDs)
	matches := func(ownerID, recordTenantID string) bool {
		_, found := ownerIDs[strings.TrimSpace(ownerID)]
		return found && mobileMeetingRecordingTenantMatches(tenantID, recordTenantID)
	}

	// Knowledge sources live in a separate SQLite database from state.json.
	// Mark the owners first so an already-authorized request or background
	// document worker cannot write a source while this cleanup is underway.
	p.purgeMobileUserKnowledgeData(ctx, tenantID, ownerIDs, logErr)
	// The full Mobile agent has its own control-plane state, user config, MCP
	// secrets, installed skills, workspace and structured record store under
	// mobile-agent/. Remove it before the Hub identity is deleted as well.
	if err := purgeMobileAgentUserData(ctx, tenantID, ownerIDs); err != nil {
		logErr("mobile_agent_runtime", err)
	}

	// Serialize the map deletions with the full state.json snapshot and commit
	// the clean snapshot before allowing a pre-existing persistence operation to
	// finish. Without this, an older snapshot can rename over this purge's
	// state.json and resurrect deleted data after a restart.
	mobileStateWriteMu.Lock()

	// Remove recordings before documents. A meeting worker checks this map right
	// before materializing transcript/minutes; if it started earlier, the later
	// document cleanup below still removes anything it has already materialized.
	var recordingDirs []string
	mobileMeetingRecordings.Lock()
	for id, recording := range mobileMeetingRecordings.items {
		if !matches(recording.OwnerID, recording.TenantID) {
			continue
		}
		recordingDirs = append(recordingDirs, recording.Dir)
		delete(mobileMeetingRecordings.items, id)
	}
	mobileMeetingRecordings.Unlock()

	var blobPaths []string
	var images []mobileDocumentDraftImage
	mobileDocuments.Lock()
	for id, draft := range mobileDocuments.drafts {
		if !matches(draft.OwnerID, draft.TenantID) {
			continue
		}
		blobPaths = append(blobPaths, draft.SourcePath)
		images = append(images, draft.Images...)
		delete(mobileDocuments.drafts, id)
	}
	for id, upload := range mobileDocuments.uploads {
		if !matches(upload.OwnerID, upload.TenantID) {
			continue
		}
		blobPaths = append(blobPaths, upload.SourcePath)
		delete(mobileDocuments.uploads, id)
	}
	for id, export := range mobileDocuments.exports {
		if matches(export.OwnerID, export.TenantID) {
			delete(mobileDocuments.exports, id)
		}
	}
	mobileDocuments.Unlock()

	mobileDigitalEmployeeTasks.Lock()
	for id, task := range mobileDigitalEmployeeTasks.tasks {
		if matches(task.OwnerID, task.TenantID) {
			delete(mobileDigitalEmployeeTasks.tasks, id)
		}
	}
	mobileDigitalEmployeeTasks.Unlock()

	mobileDocumentProcessJobs.Lock()
	for id, job := range mobileDocumentProcessJobs.jobs {
		if matches(job.OwnerID, job.TenantID) {
			delete(mobileDocumentProcessJobs.jobs, id)
		}
	}
	mobileDocumentProcessJobs.Unlock()

	mobileAgentJobs.Lock()
	for id, job := range mobileAgentJobs.jobs {
		if matches(job.OwnerID, job.TenantID) {
			delete(mobileAgentJobs.jobs, id)
		}
	}
	mobileAgentJobs.Unlock()

	mobileServerProfiles.Lock()
	for id, profile := range mobileServerProfiles.profiles {
		if matches(profile.OwnerID, profile.TenantID) {
			delete(mobileServerProfiles.profiles, id)
		}
	}
	mobileServerProfiles.Unlock()

	mobileSSHVault.Lock()
	for id, secret := range mobileSSHVault.secrets {
		if matches(secret.OwnerID, secret.TenantID) {
			delete(mobileSSHVault.secrets, id)
		}
	}
	mobileSSHVault.Unlock()

	mobileLlmAuthorizations.Lock()
	for id, authorization := range mobileLlmAuthorizations.authorizations {
		if matches(authorization.OwnerID, authorization.TenantID) {
			delete(mobileLlmAuthorizations.authorizations, id)
		}
	}
	for id, session := range mobileLlmAuthorizations.qrSessions {
		if matches(session.OwnerID, session.TenantID) {
			delete(mobileLlmAuthorizations.qrSessions, id)
		}
	}
	mobileLlmAuthorizations.Unlock()

	var liveSessionIDs []string
	mobileBackendSSHSessions.Lock()
	for id, session := range mobileBackendSSHSessions.sessions {
		if !matches(session.OwnerID, session.TenantID) {
			continue
		}
		liveSessionIDs = append(liveSessionIDs, session.SessionID)
		delete(mobileBackendSSHSessions.sessions, id)
	}
	mobileBackendSSHSessions.Unlock()

	var hubTaskIDs []string
	mobileBackendSSHTasks.Lock()
	for id, task := range mobileBackendSSHTasks.tasks {
		if !matches(task.OwnerID, task.TenantID) {
			continue
		}
		hubTaskIDs = append(hubTaskIDs, task.TaskID)
		delete(mobileBackendSSHTasks.tasks, id)
	}
	mobileBackendSSHTasks.Unlock()

	mobileBackendSSHFileOperations.Lock()
	for id, operation := range mobileBackendSSHFileOperations.operations {
		if matches(operation.OwnerID, operation.TenantID) {
			delete(mobileBackendSSHFileOperations.operations, id)
		}
	}
	mobileBackendSSHFileOperations.Unlock()

	mobilePushState.Lock()
	for ownerID := range ownerIDs {
		pushKey := mobilePushUserKey(tenantID, ownerID)
		delete(mobilePushState.devices, pushKey)
		delete(mobilePushState.pending, pushKey)
		delete(mobilePushState.lastPush, pushKey)
	}
	mobilePushState.Unlock()

	mobileHubFiles.Lock()
	for token, blob := range mobileHubFiles.blobs {
		if matches(blob.OwnerID, blob.TenantID) {
			delete(mobileHubFiles.blobs, token)
		}
	}
	mobileHubFiles.Unlock()

	// Existing realtime connections are already authenticated. Drop them so an
	// unbind takes effect immediately rather than waiting for socket expiry.
	mobileRealtimeClients.Lock()
	var clients []*mobileRealtimeClient
	for ownerID := range ownerIDs {
		key := mobileRealtimeKey(tenantID, ownerID)
		for client := range mobileRealtimeClients.clients[key] {
			clients = append(clients, client)
		}
		delete(mobileRealtimeClients.clients, key)
	}
	mobileRealtimeClients.Unlock()
	for _, client := range clients {
		if closer, ok := client.conn.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				logErr("mobile_realtime_close", err)
			}
		}
	}

	// Persist while holding mobileStateWriteMu. External work below intentionally
	// happens after this durable cleanup and does not need to block state writes.
	mobilePersistStateLocked()
	mobileStateWriteMu.Unlock()

	// Do external work only after releasing state-map locks.
	for _, sessionID := range liveSessionIDs {
		mobileHubLiveCloseSession(sessionID)
	}
	for _, taskID := range hubTaskIDs {
		mobileHubTaskCancel(taskID)
	}
	for _, path := range blobPaths {
		mobileDeleteDocumentBlob(path)
	}
	mobileDraftDeleteImages(images)
	for _, dir := range recordingDirs {
		if dir != "" {
			if err := os.RemoveAll(dir); err != nil {
				logErr("mobile_meeting_recording_files", err)
			}
		}
	}
	for ownerID := range ownerIDs {
		if err := deletePersistedMobileLLMAuthorization(ctx, tenantID, ownerID); err != nil {
			logErr("mobile_llm_authorization", err)
		}
	}

}

func mobilePurgeOwnerIDs(userID string, aliases []string) map[string]struct{} {
	owners := make(map[string]struct{}, len(aliases)+1)
	if userID = strings.TrimSpace(userID); userID != "" {
		owners[userID] = struct{}{}
	}
	for _, alias := range aliases {
		if alias = strings.TrimSpace(alias); alias != "" {
			owners[alias] = struct{}{}
		}
	}
	return owners
}

// purgeMobileUserKnowledgeData removes sources and all derived knowledge data
// for the account's current ID plus legacy email-owned Mobile records. The
// store owns the dependent cards, facts, FTS rows, vectors and image assets.
func (p *UserDataPurger) purgeMobileUserKnowledgeData(ctx context.Context, tenantID string, ownerIDs map[string]struct{}, logErr func(string, error)) {
	if len(ownerIDs) == 0 {
		return
	}
	// Open the durable store even when the Mobile agent has not been used in the
	// current process; otherwise a restart could retain its existing DB data.
	if _, _, err := mobileEnsureCoreAgent(); err != nil {
		logErr("mobile_knowledge_initialize", err)
	}

	if mobileKnowledgeStore == nil {
		return
	}
	for ownerID := range ownerIDs {
		if _, err := mobileKnowledgeStore.DeleteSourcesByFilter(ctx, knowledge.ListSourcesOptions{
			TenantID: mobileMeetingRecordingTenantID(tenantID),
			OwnerID:  ownerID,
		}); err != nil {
			logErr("mobile_knowledge", err)
		}
	}
}

package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

type testMeetingTranscriber struct{}

func (testMeetingTranscriber) Transcribe(context.Context, string, string) (string, error) {
	return "Alice: approved the launch date.", nil
}

type testMeetingMinutes struct{}

func (testMeetingMinutes) Summarize(context.Context, string, string, string) (string, error) {
	return "# Meeting minutes\n- Decision: launch approved.", nil
}

type failIfCalledMeetingTranscriber struct{}

func (failIfCalledMeetingTranscriber) Transcribe(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("transcriber must not be called for keep mode")
}

type failIfCalledMeetingMinutes struct{}

func (failIfCalledMeetingMinutes) Summarize(context.Context, string, string, string) (string, error) {
	return "", fmt.Errorf("minutes worker must not be called for keep mode")
}

type testMeetingSegmenter struct{}

func (testMeetingSegmenter) Segment(context.Context, string, string, string) ([]mobileMeetingSpeakerSegment, error) {
	return []mobileMeetingSpeakerSegment{{Speaker: "Speaker 1", StartSec: 0, EndSec: 1.5, Text: "Alice: approved the launch date."}}, nil
}

func TestCommandMeetingWorkersUseStrictJSONContract(t *testing.T) {
	transcribeCommand := `printf '{"transcript":"verified transcript"}'`
	minutesCommand := `printf '{"minutes":"verified minutes"}'`
	if runtime.GOOS == "windows" {
		transcribeCommand = `echo {"transcript":"verified transcript"}`
		minutesCommand = `echo {"minutes":"verified minutes"}`
	}
	transcript, err := (commandMeetingTranscriber{command: transcribeCommand}).Transcribe(context.Background(), "recording.m4a", "audio/mp4")
	if err != nil || transcript != "verified transcript" {
		t.Fatalf("transcribe=%q err=%v", transcript, err)
	}
	minutes, err := (commandMeetingMinutes{command: minutesCommand}).Summarize(context.Background(), "Review", "Purpose", transcript)
	if err != nil || minutes != "verified minutes" {
		t.Fatalf("minutes=%q err=%v", minutes, err)
	}
}

func TestMobileMeetingRecordingCapabilitiesReflectConfiguredWorkers(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "meeting-capabilities@example.com")
	SetMeetingRecordingWorkers(testMeetingTranscriber{}, testMeetingMinutes{})
	t.Cleanup(func() { SetMeetingRecordingWorkers(nil, nil) })
	req := httptest.NewRequest(http.MethodGet, "/api/mobile/meeting-recordings/capabilities", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	MobileMeetingRecordingCapabilitiesHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("capabilities=%d %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Modes map[string]bool `json:"modes"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Modes["keep"] || !body.Modes["transcript"] || !body.Modes["minutes"] {
		t.Fatalf("unexpected capabilities: %#v", body.Modes)
	}
}

func TestMobileMeetingRecordingRejectsUnavailableProcessingMode(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-mode-unavailable@example.com")
	SetMeetingRecordingWorkers(nil, nil)
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/recording.m4a", []byte("\x00\x00\x00\x18ftypM4A \x00\x00\x00\x00audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := mobileMeetingRecording{ID: "meeting-mode-unavailable", OwnerID: enroll.UserID, TenantID: "default", Dir: dir, ContentType: "audio/mp4", Status: "uploaded", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
		_ = os.RemoveAll(dir)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/meeting-recordings/"+rec.ID+"/process", strings.NewReader(`{"mode":"minutes"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "PROCESSING_MODE_UNAVAILABLE") {
		t.Fatalf("unavailable mode=%d %s", resp.Code, resp.Body.String())
	}
	stored, ok := mobileMeetingRecordingOwned(enroll.UserID, rec.ID)
	if !ok || stored.Status != "uploaded" || stored.RetryCount != 0 {
		t.Fatalf("unavailable mode should not mutate recording: %#v", stored)
	}
}

func TestMobileMeetingRecordingUploadAndProcess(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-recording@example.com")
	SetMeetingRecordingWorkers(testMeetingTranscriber{}, testMeetingMinutes{})
	SetMeetingSpeakerSegmentationWorker(testMeetingSegmenter{})
	t.Cleanup(func() {
		SetMeetingRecordingWorkers(nil, nil)
		SetMeetingSpeakerSegmentationWorker(nil)
	})

	create := httptest.NewRequest(http.MethodPost, "/api/mobile/meeting-recordings", strings.NewReader(`{"title":"Product review","content_type":"audio/mp4"}`))
	create.Header.Set("Authorization", "Bearer "+token)
	create.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(createRec, create)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["recording_id"].(string)
	if id == "" {
		t.Fatalf("missing id: %#v", created)
	}

	chunk := []byte("\x00\x00\x00\x18ftypM4A \x00\x00\x00\x00minimal-m4a-payload")
	sum := sha256.Sum256(chunk)
	put := httptest.NewRequest(http.MethodPut, "/api/mobile/meeting-recordings/"+id+"/chunks/0", strings.NewReader(string(chunk)))
	put.Header.Set("Authorization", "Bearer "+token)
	put.Header.Set("X-Chunk-SHA256", hex.EncodeToString(sum[:]))
	put.SetPathValue("recordingId", id)
	put.SetPathValue("chunkIndex", "0")
	putRec := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(putRec, put)
	if putRec.Code != http.StatusNoContent {
		t.Fatalf("put=%d %s", putRec.Code, putRec.Body.String())
	}

	complete := httptest.NewRequest(http.MethodPost, "/api/mobile/meeting-recordings/"+id+"/complete", strings.NewReader(`{"chunks":1}`))
	complete.Header.Set("Authorization", "Bearer "+token)
	complete.Header.Set("Content-Type", "application/json")
	complete.SetPathValue("recordingId", id)
	completeRec := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(completeRec, complete)
	if completeRec.Code != http.StatusOK {
		t.Fatalf("complete=%d %s", completeRec.Code, completeRec.Body.String())
	}

	process := httptest.NewRequest(http.MethodPost, "/api/mobile/meeting-recordings/"+id+"/process", strings.NewReader(`{"mode":"minutes"}`))
	process.Header.Set("Authorization", "Bearer "+token)
	process.Header.Set("Content-Type", "application/json")
	process.SetPathValue("recordingId", id)
	processRec := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(processRec, process)
	if processRec.Code != http.StatusAccepted {
		t.Fatalf("process=%d %s", processRec.Code, processRec.Body.String())
	}

	deadline := time.Now().Add(time.Second)
	ready := false
	for time.Now().Before(deadline) {
		get := httptest.NewRequest(http.MethodGet, "/api/mobile/meeting-recordings/"+id, nil)
		get.Header.Set("Authorization", "Bearer "+token)
		get.SetPathValue("recordingId", id)
		getRec := httptest.NewRecorder()
		MobileMeetingRecordingsHandler(identity).ServeHTTP(getRec, get)
		var body map[string]any
		_ = json.Unmarshal(getRec.Body.Bytes(), &body)
		if body["status"] == "ready" {
			if body["transcript"] == "" || body["minutes"] == "" {
				t.Fatalf("missing result: %#v", body)
			}
			if body["transcript_draft_id"] == "" || body["minutes_draft_id"] == "" {
				t.Fatalf("result documents were not created: %#v", body)
			}
			if body["retry_count"] != float64(1) || body["failure_code"] != "" {
				t.Fatalf("unexpected processing lifecycle payload: %#v", body)
			}
			segments, _ := body["speaker_segments"].([]any)
			if len(segments) != 1 {
				t.Fatalf("speaker segments missing: %#v", body)
			}
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("recording did not finish before deadline")
	}
	mobileMeetingRecordings.Lock()
	rec := mobileMeetingRecordings.items[id]
	delete(mobileMeetingRecordings.items, id)
	mobileMeetingRecordings.Unlock()
	if rec.OwnerID != enroll.UserID {
		t.Fatalf("owner=%q", rec.OwnerID)
	}
	mobileDocuments.Lock()
	transcriptDraft, transcriptOK := mobileDocuments.drafts[rec.TranscriptDraftID]
	minutesDraft, minutesOK := mobileDocuments.drafts[rec.MinutesDraftID]
	delete(mobileDocuments.drafts, rec.TranscriptDraftID)
	delete(mobileDocuments.drafts, rec.MinutesDraftID)
	mobileDocuments.Unlock()
	if !transcriptOK || !strings.Contains(transcriptDraft.Markdown, "Alice: approved") {
		t.Fatalf("transcript draft missing or invalid: %#v", transcriptDraft)
	}
	if !strings.Contains(transcriptDraft.Markdown, "Speaker 1") ||
		!strings.Contains(transcriptDraft.Markdown, "00:00:00–00:00:01") {
		t.Fatalf("transcript draft did not render speaker segments: %q", transcriptDraft.Markdown)
	}
	if !minutesOK || !strings.Contains(minutesDraft.Markdown, "launch approved") {
		t.Fatalf("minutes draft missing or invalid: %#v", minutesDraft)
	}
	_ = os.RemoveAll(rec.Dir)
}

func TestMobileCleanupExpiredMeetingRecordingsPreservesResultMetadata(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/recording.m4a", []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := mobileMeetingRecording{
		ID:                "expired-meeting",
		OwnerID:           "user-1",
		Title:             "Expired meeting",
		Dir:               dir,
		Transcript:        "stored transcript",
		Minutes:           "stored minutes",
		TranscriptDraftID: "draft-transcript",
		MinutesDraftID:    "draft-minutes",
		RetentionUntil:    time.Now().UTC().Add(-time.Minute),
		CreatedAt:         time.Now().UTC().Add(-31 * 24 * time.Hour),
	}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	if got := mobileCleanupExpiredMeetingRecordings(time.Now().UTC()); got != 1 {
		t.Fatalf("cleaned=%d want 1", got)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("audio dir exists after retention cleanup: %v", err)
	}
	stored, ok := mobileMeetingRecordingOwned(rec.OwnerID, rec.ID)
	if !ok || stored.Dir != "" || stored.Transcript != rec.Transcript || stored.Minutes != rec.Minutes || stored.MinutesDraftID != rec.MinutesDraftID {
		t.Fatalf("recording result was not preserved: %#v", stored)
	}
}

func TestMobileMeetingRecordingPayloadReportsUnavailableWhenAudioFileMissing(t *testing.T) {
	dir := t.TempDir()
	rec := mobileMeetingRecording{ID: "missing-payload", Dir: dir, ContentType: "audio/mp4"}
	payload := mobileMeetingRecordingPayload(rec)
	if available, ok := payload["audio_available"].(bool); !ok || available {
		t.Fatalf("audio availability payload=%#v", payload)
	}
	if err := os.WriteFile(dir+"/recording.m4a", []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload = mobileMeetingRecordingPayload(rec)
	if available, ok := payload["audio_available"].(bool); !ok || !available {
		t.Fatalf("audio availability payload=%#v", payload)
	}
}

func TestMobileMeetingRecordingRejectsUnsupportedContentType(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "meeting-type@example.com")
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/meeting-recordings", strings.NewReader(`{"title":"Unsupported","content_type":"audio/flac"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), "UNSUPPORTED_AUDIO_TYPE") {
		t.Fatalf("create=%d %s", resp.Code, resp.Body.String())
	}
}

func TestMobileMeetingRecordingRejectsNonNumericChunkIndex(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-chunk@example.com")
	dir := t.TempDir()
	rec := mobileMeetingRecording{ID: "meeting-invalid-chunk", OwnerID: enroll.UserID, TenantID: "default", Dir: dir, ContentType: "audio/mp4", Status: "uploading", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	req := httptest.NewRequest(http.MethodPut, "/api/mobile/meeting-recordings/"+rec.ID+"/chunks/not-a-number", strings.NewReader("audio"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	req.SetPathValue("chunkIndex", "not-a-number")
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), "INVALID_CHUNK") {
		t.Fatalf("put=%d %s", resp.Code, resp.Body.String())
	}
}

func TestMobileMeetingRecordingRequiresChunkHash(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-hash-required@example.com")
	dir := t.TempDir()
	rec := mobileMeetingRecording{ID: "meeting-hash-required", OwnerID: enroll.UserID, TenantID: "default", Dir: dir, ContentType: "audio/mp4", Status: "uploading", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	req := httptest.NewRequest(http.MethodPut, "/api/mobile/meeting-recordings/"+rec.ID+"/chunks/0", strings.NewReader("audio"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	req.SetPathValue("chunkIndex", "0")
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), "CHUNK_HASH_REQUIRED") {
		t.Fatalf("missing hash=%d %s", resp.Code, resp.Body.String())
	}
}

func TestMobileMeetingRecordingRejectsMismatchedCompletedAudioType(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-type-mismatch@example.com")
	dir := t.TempDir()
	chunk := []byte("not an m4a file")
	if err := os.WriteFile(dir+"/chunk-0", chunk, 0o600); err != nil {
		t.Fatal(err)
	}
	rec := mobileMeetingRecording{ID: "meeting-type-mismatch", OwnerID: enroll.UserID, TenantID: "default", Dir: dir, ContentType: "audio/mp4", Status: "uploading", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/meeting-recordings/"+rec.ID+"/complete", strings.NewReader(`{"chunks":1}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), "AUDIO_TYPE_MISMATCH") {
		t.Fatalf("type mismatch=%d %s", resp.Code, resp.Body.String())
	}
	stored, ok := mobileMeetingRecordingOwned(enroll.UserID, rec.ID)
	if !ok || stored.Status != "uploading" {
		t.Fatalf("failed finalization should reopen upload: %#v", stored)
	}
}

func TestMobileMeetingRecordingRejectsChunkAfterFinalizeClaim(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-finalize-claim@example.com")
	dir := t.TempDir()
	rec := mobileMeetingRecording{ID: "meeting-finalize-claim", OwnerID: enroll.UserID, TenantID: "default", Dir: dir, ContentType: "audio/mp4", Status: "uploading", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	if _, claim := mobileMeetingRecordingClaimFinalize(enroll.UserID, rec.ID); claim != "claimed" {
		t.Fatalf("claim=%q", claim)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/mobile/meeting-recordings/"+rec.ID+"/chunks/0", strings.NewReader("audio"))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	req.SetPathValue("chunkIndex", "0")
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "UPLOAD_CLOSED") {
		t.Fatalf("put after claim=%d %s", resp.Code, resp.Body.String())
	}
}

func TestMobileMeetingRecordingCompleteReturnsFinalizingForConcurrentRequest(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-complete-finalizing@example.com")
	dir := t.TempDir()
	rec := mobileMeetingRecording{ID: "meeting-complete-finalizing", OwnerID: enroll.UserID, TenantID: "default", Dir: dir, ContentType: "audio/mp4", Status: "uploading", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	if _, claim := mobileMeetingRecordingClaimFinalize(enroll.UserID, rec.ID); claim != "claimed" {
		t.Fatalf("claim=%q", claim)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/meeting-recordings/"+rec.ID+"/complete", strings.NewReader(`{"chunks":1}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "FINALIZING") {
		t.Fatalf("complete while finalizing=%d %s", resp.Code, resp.Body.String())
	}
}

func TestMobileMeetingRecordingRejectsInvalidDuration(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-duration@example.com")
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/chunk-0", []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := mobileMeetingRecording{ID: "meeting-invalid-duration", OwnerID: enroll.UserID, TenantID: "default", Dir: dir, ContentType: "audio/mp4", Status: "uploading", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/meeting-recordings/"+rec.ID+"/complete", strings.NewReader(`{"chunks":1,"duration_sec":86401}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), "INVALID_DURATION") {
		t.Fatalf("complete=%d %s", resp.Code, resp.Body.String())
	}
}

func TestMobileMeetingRecordingRejectsWAVDurationBeyondQuota(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-wav-duration@example.com")
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/chunk-0", []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := mobileMeetingRecording{ID: "meeting-wav-duration", OwnerID: enroll.UserID, TenantID: "default", Dir: dir, ContentType: "audio/wav", Status: "uploading", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	body := fmt.Sprintf(`{"chunks":1,"duration_sec":%d}`, meetingRecordingMaxWAVDurationSec+1)
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/meeting-recordings/"+rec.ID+"/complete", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), "INVALID_DURATION") {
		t.Fatalf("complete=%d %s", resp.Code, resp.Body.String())
	}
}

func TestMobileMeetingRecordingProcessRetriesFailedRecording(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-retry@example.com")
	dir := t.TempDir()
	rec := mobileMeetingRecording{
		ID:          "meeting-retry",
		OwnerID:     enroll.UserID,
		TenantID:    "default",
		Title:       "Retry meeting",
		Dir:         dir,
		ContentType: "audio/mp4",
		Status:      "failed",
		FailureCode: "ASR_TRANSCRIPTION_FAILED",
		RetryCount:  1,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})

	req := httptest.NewRequest(http.MethodPost, "/api/mobile/meeting-recordings/"+rec.ID+"/process", strings.NewReader(`{"mode":"transcript"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("recordingId", rec.ID)
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("retry=%d %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "processing" || body["failure_code"] != "" || body["retry_count"] != float64(2) {
		t.Fatalf("unexpected retry payload: %#v", body)
	}
}

func TestMobileMeetingRecordingRejectsRetryWhenAudioExpired(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-retry-expired@example.com")
	rec := mobileMeetingRecording{
		ID:          "meeting-retry-expired",
		OwnerID:     enroll.UserID,
		TenantID:    "default",
		Title:       "Expired retry meeting",
		Status:      "failed",
		FailureCode: "ASR_TRANSCRIPTION_FAILED",
		ContentType: "audio/mp4",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/meeting-recordings/"+rec.ID+"/process", strings.NewReader(`{"mode":"minutes"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "AUDIO_MISSING_FOR_RETRY") {
		t.Fatalf("retry missing audio=%d %s", resp.Code, resp.Body.String())
	}
	stored, ok := mobileMeetingRecordingOwned(enroll.UserID, rec.ID)
	if !ok || stored.Status != "failed" || stored.FailureCode != "AUDIO_MISSING_FOR_RETRY" || stored.RetryCount != 0 {
		t.Fatalf("unexpected expired retry state: %#v", stored)
	}
}

func TestMobileMeetingRecordingRejectsKeepWhenAudioMissing(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-keep-missing@example.com")
	rec := mobileMeetingRecording{ID: "meeting-keep-missing", OwnerID: enroll.UserID, TenantID: "default", Title: "Missing archive", Status: "uploaded", ContentType: "audio/mp4", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/meeting-recordings/"+rec.ID+"/process", strings.NewReader(`{"mode":"keep"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "AUDIO_MISSING_FOR_RETRY") {
		t.Fatalf("keep missing audio=%d %s", resp.Code, resp.Body.String())
	}
	stored, ok := mobileMeetingRecordingOwned(enroll.UserID, rec.ID)
	if !ok || stored.Status != "failed" || stored.FailureCode != "AUDIO_MISSING_FOR_RETRY" {
		t.Fatalf("unexpected missing archive state: %#v", stored)
	}
}

func TestMobileMeetingRecordingKeepModeArchivesWithoutWorkers(t *testing.T) {
	SetMeetingRecordingWorkers(failIfCalledMeetingTranscriber{}, failIfCalledMeetingMinutes{})
	t.Cleanup(func() { SetMeetingRecordingWorkers(nil, nil) })
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/recording.m4a", []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := mobileMeetingRecording{
		ID:          "meeting-keep",
		OwnerID:     "keep-user",
		Title:       "Archive only",
		Dir:         dir,
		ContentType: "audio/mp4",
		Status:      "processing",
		ProcessMode: "keep",
		CreatedAt:   time.Now().UTC(),
	}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	mobileRunMeetingRecording(rec.ID)
	stored, ok := mobileMeetingRecordingOwned(rec.OwnerID, rec.ID)
	if !ok || stored.Status != "ready" || stored.Message != "audio archived" || stored.Transcript != "" || stored.Minutes != "" {
		t.Fatalf("unexpected keep result: %#v", stored)
	}
}

func TestMobileMeetingRecordingDeleteAudioKeepsDocuments(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-delete-audio@example.com")
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/recording.m4a", []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := mobileMeetingRecording{
		ID:                "meeting-delete-audio",
		OwnerID:           enroll.UserID,
		Title:             "Delete raw audio",
		Dir:               dir,
		Status:            "ready",
		Transcript:        "keep transcript",
		Minutes:           "keep minutes",
		TranscriptDraftID: "draft-transcript",
		MinutesDraftID:    "draft-minutes",
		CreatedAt:         time.Now().UTC(),
	}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/mobile/meeting-recordings/"+rec.ID+"/audio", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("delete audio=%d %s", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if available, ok := payload["audio_available"].(bool); !ok || available {
		t.Fatalf("audio availability payload=%#v", payload)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("raw audio dir remains: %v", err)
	}
	stored, ok := mobileMeetingRecordingOwned(enroll.UserID, rec.ID)
	if !ok || stored.Dir != "" || stored.Transcript != rec.Transcript || stored.Minutes != rec.Minutes || stored.MinutesDraftID != rec.MinutesDraftID {
		t.Fatalf("unexpected recording after audio delete: %#v", stored)
	}
}

func TestMobileMeetingRecordingRejectsAudioDeleteWhileProcessing(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-audio-in-use@example.com")
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/recording.m4a", []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := mobileMeetingRecording{ID: "meeting-audio-in-use", OwnerID: enroll.UserID, TenantID: "default", Dir: dir, ContentType: "audio/mp4", Status: "processing", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
		_ = os.RemoveAll(dir)
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/mobile/meeting-recordings/"+rec.ID+"/audio", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "AUDIO_IN_USE") {
		t.Fatalf("delete while processing=%d %s", resp.Code, resp.Body.String())
	}
	if _, err := os.Stat(dir + "/recording.m4a"); err != nil {
		t.Fatalf("raw audio should remain while processing: %v", err)
	}
}

func TestMobileMeetingRecordingRejectsFullDeleteWhileProcessing(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, enroll := issueViewerToken(t, identity, "meeting-delete-in-use@example.com")
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/recording.m4a", []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := mobileMeetingRecording{ID: "meeting-delete-in-use", OwnerID: enroll.UserID, TenantID: "default", Dir: dir, ContentType: "audio/mp4", Status: "processing", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
		_ = os.RemoveAll(dir)
	})
	req := httptest.NewRequest(http.MethodDelete, "/api/mobile/meeting-recordings/"+rec.ID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.SetPathValue("recordingId", rec.ID)
	resp := httptest.NewRecorder()
	MobileMeetingRecordingsHandler(identity).ServeHTTP(resp, req)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), "RECORDING_IN_USE") {
		t.Fatalf("full delete while processing=%d %s", resp.Code, resp.Body.String())
	}
	if _, err := os.Stat(dir + "/recording.m4a"); err != nil {
		t.Fatalf("raw audio should remain while processing: %v", err)
	}
	if _, ok := mobileMeetingRecordingOwned(enroll.UserID, rec.ID); !ok {
		t.Fatal("processing recording should remain")
	}
}

func TestMobileResumeMeetingRecordingWorkersRestartsInterruptedProcessing(t *testing.T) {
	SetMeetingRecordingWorkers(testMeetingTranscriber{}, testMeetingMinutes{})
	t.Cleanup(func() { SetMeetingRecordingWorkers(nil, nil) })
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/recording.m4a", []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := mobileMeetingRecording{
		ID:          "meeting-resume",
		OwnerID:     "user-resume",
		TenantID:    "default",
		Title:       "Interrupted meeting",
		Dir:         dir,
		ContentType: "audio/mp4",
		Status:      "processing",
		ProcessMode: "minutes",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	t.Cleanup(func() {
		mobileMeetingRecordings.Lock()
		delete(mobileMeetingRecordings.items, rec.ID)
		mobileMeetingRecordings.Unlock()
	})
	mobileResumeMeetingRecordingWorkers()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stored, ok := mobileMeetingRecordingOwned(rec.OwnerID, rec.ID)
		if ok && stored.Status == "ready" && stored.MinutesDraftID != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	stored, _ := mobileMeetingRecordingOwned(rec.OwnerID, rec.ID)
	t.Fatalf("interrupted processing was not resumed: %#v", stored)
}

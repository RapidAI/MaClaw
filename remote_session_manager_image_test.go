package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// newTestSDKHandle creates an SDKExecutionHandle with controllable channels
// for testing runSDKOutputLoop without launching a real process.
func newTestSDKHandle() *SDKExecutionHandle {
	return &SDKExecutionHandle{
		pid:       999,
		outputCh:  make(chan []byte, 128),
		exitCh:    make(chan PTYExit, 1),
		msgCh:     make(chan SDKMessage, 64),
		ctrlReqCh: make(chan SDKControlRequest, 16),
	}
}

func TestRunSDKOutputLoop_ImageInterception_ValidImage(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	manager.pipelineFactory = func() *OutputPipeline {
		return NewOutputPipeline()
	}
	// hubClient is nil — SendSessionImage is a no-op, but the interception
	// logic (validation, NewImageTransferMessage) still executes.

	sdkHandle := newTestSDKHandle()
	session := &RemoteSession{
		ID:      "sess-img-1",
		Tool:    "claude",
		Title:   "image test",
		Status:  SessionStarting,
		Summary: SessionSummary{SessionID: "sess-img-1", Status: string(SessionStarting)},
		Exec:    sdkHandle,
	}

	done := make(chan struct{})
	go func() {
		manager.runSDKOutputLoop(session)
		close(done)
	}()

	// Send an assistant message with a valid image block
	validB64 := base64.StdEncoding.EncodeToString([]byte("fake-png-data"))
	sdkHandle.msgCh <- SDKMessage{
		Type: "assistant",
		Message: &SDKAssistantPayload{
			Role: "assistant",
			Content: []SDKContentBlock{
				{
					Type: "image",
					Source: &SDKImageSource{
						Type:      "base64",
						MediaType: "image/png",
						Data:      validB64,
					},
				},
			},
		},
	}

	// Give the loop time to process
	time.Sleep(100 * time.Millisecond)

	// Close channels to terminate the loop
	close(sdkHandle.msgCh)
	close(sdkHandle.outputCh)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runSDKOutputLoop did not finish")
	}

	// Session should be in busy state after processing assistant message
	session.mu.RLock()
	status := session.Status
	session.mu.RUnlock()
	if status != SessionBusy {
		t.Fatalf("session status = %q, want %q", status, SessionBusy)
	}
}

func TestRunSDKOutputLoop_ImageInterception_InvalidMediaType(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	manager.pipelineFactory = func() *OutputPipeline {
		return NewOutputPipeline()
	}

	sdkHandle := newTestSDKHandle()
	session := &RemoteSession{
		ID:      "sess-img-2",
		Tool:    "claude",
		Title:   "invalid media type test",
		Status:  SessionStarting,
		Summary: SessionSummary{SessionID: "sess-img-2", Status: string(SessionStarting)},
		Exec:    sdkHandle,
	}

	done := make(chan struct{})
	go func() {
		manager.runSDKOutputLoop(session)
		close(done)
	}()

	// Send an assistant message with an unsupported media type
	validB64 := base64.StdEncoding.EncodeToString([]byte("fake-bmp-data"))
	sdkHandle.msgCh <- SDKMessage{
		Type: "assistant",
		Message: &SDKAssistantPayload{
			Role: "assistant",
			Content: []SDKContentBlock{
				{
					Type: "image",
					Source: &SDKImageSource{
						Type:      "base64",
						MediaType: "image/bmp",
						Data:      validB64,
					},
				},
			},
		},
	}

	time.Sleep(100 * time.Millisecond)
	close(sdkHandle.msgCh)
	close(sdkHandle.outputCh)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runSDKOutputLoop did not finish")
	}

	// Should still process without crashing — invalid image is skipped
	session.mu.RLock()
	status := session.Status
	session.mu.RUnlock()
	if status != SessionBusy {
		t.Fatalf("session status = %q, want %q", status, SessionBusy)
	}
}

func TestRunSDKOutputLoop_ImageInterception_OversizedImage(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	manager.pipelineFactory = func() *OutputPipeline {
		return NewOutputPipeline()
	}

	sdkHandle := newTestSDKHandle()
	session := &RemoteSession{
		ID:      "sess-img-3",
		Tool:    "claude",
		Title:   "oversized image test",
		Status:  SessionStarting,
		Summary: SessionSummary{SessionID: "sess-img-3", Status: string(SessionStarting)},
		Exec:    sdkHandle,
	}

	done := make(chan struct{})
	go func() {
		manager.runSDKOutputLoop(session)
		close(done)
	}()

	// Create data that exceeds 10MB when decoded
	bigData := strings.Repeat("A", ImageOutputSizeLimit+1)
	bigB64 := base64.StdEncoding.EncodeToString([]byte(bigData))
	sdkHandle.msgCh <- SDKMessage{
		Type: "assistant",
		Message: &SDKAssistantPayload{
			Role: "assistant",
			Content: []SDKContentBlock{
				{
					Type: "image",
					Source: &SDKImageSource{
						Type:      "base64",
						MediaType: "image/png",
						Data:      bigB64,
					},
				},
			},
		},
	}

	time.Sleep(200 * time.Millisecond)
	close(sdkHandle.msgCh)
	close(sdkHandle.outputCh)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runSDKOutputLoop did not finish")
	}

	// Should still process without crashing — oversized image is skipped
	session.mu.RLock()
	status := session.Status
	session.mu.RUnlock()
	if status != SessionBusy {
		t.Fatalf("session status = %q, want %q", status, SessionBusy)
	}
}

func TestRunSDKOutputLoop_ImageInterception_NilSource(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	manager.pipelineFactory = func() *OutputPipeline {
		return NewOutputPipeline()
	}

	sdkHandle := newTestSDKHandle()
	session := &RemoteSession{
		ID:      "sess-img-4",
		Tool:    "claude",
		Title:   "nil source test",
		Status:  SessionStarting,
		Summary: SessionSummary{SessionID: "sess-img-4", Status: string(SessionStarting)},
		Exec:    sdkHandle,
	}

	done := make(chan struct{})
	go func() {
		manager.runSDKOutputLoop(session)
		close(done)
	}()

	// Image block with nil Source should be silently skipped
	sdkHandle.msgCh <- SDKMessage{
		Type: "assistant",
		Message: &SDKAssistantPayload{
			Role: "assistant",
			Content: []SDKContentBlock{
				{Type: "image", Source: nil},
			},
		},
	}

	time.Sleep(100 * time.Millisecond)
	close(sdkHandle.msgCh)
	close(sdkHandle.outputCh)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runSDKOutputLoop did not finish")
	}

	session.mu.RLock()
	status := session.Status
	session.mu.RUnlock()
	if status != SessionBusy {
		t.Fatalf("session status = %q, want %q", status, SessionBusy)
	}
}

func TestRunSDKOutputLoop_ImageInterception_MixedContent(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	manager.pipelineFactory = func() *OutputPipeline {
		return NewOutputPipeline()
	}

	sdkHandle := newTestSDKHandle()
	session := &RemoteSession{
		ID:      "sess-img-5",
		Tool:    "claude",
		Title:   "mixed content test",
		Status:  SessionStarting,
		Summary: SessionSummary{SessionID: "sess-img-5", Status: string(SessionStarting)},
		Exec:    sdkHandle,
	}

	done := make(chan struct{})
	go func() {
		manager.runSDKOutputLoop(session)
		close(done)
	}()

	// Message with text, tool_use, and image blocks — all should be processed
	validB64 := base64.StdEncoding.EncodeToString([]byte("fake-jpeg-data"))
	sdkHandle.msgCh <- SDKMessage{
		Type: "assistant",
		Message: &SDKAssistantPayload{
			Role: "assistant",
			Content: []SDKContentBlock{
				{Type: "text", Text: "Here is the image:"},
				{
					Type: "image",
					Source: &SDKImageSource{
						Type:      "base64",
						MediaType: "image/jpeg",
						Data:      validB64,
					},
				},
				{Type: "tool_use", ID: "tu_1", Name: "Read"},
			},
		},
	}

	time.Sleep(100 * time.Millisecond)
	close(sdkHandle.msgCh)
	close(sdkHandle.outputCh)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runSDKOutputLoop did not finish")
	}

	// Should have processed the tool_use event
	session.mu.RLock()
	eventCount := len(session.Events)
	status := session.Status
	session.mu.RUnlock()

	if status != SessionBusy {
		t.Fatalf("session status = %q, want %q", status, SessionBusy)
	}
	if eventCount == 0 {
		t.Fatal("expected at least one event from tool_use block")
	}
}


func TestWriteImageInput_SessionNotFound(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)

	err := manager.WriteImageInput("nonexistent", ImageTransferMessage{
		SessionID: "nonexistent",
		MediaType: "image/png",
		Data:      base64.StdEncoding.EncodeToString([]byte("data")),
	})
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteImageInput_PTYSessionRejected(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)

	// Store a PTY session (using fakeExecutionHandle which is NOT *SDKExecutionHandle)
	ptyHandle := newFakeExecutionHandle(10)
	session := &RemoteSession{
		ID:     "sess-pty-1",
		Tool:   "cursor",
		Status: SessionRunning,
		Exec:   ptyHandle,
	}
	manager.mu.Lock()
	manager.sessions[session.ID] = session
	manager.mu.Unlock()

	err := manager.WriteImageInput("sess-pty-1", ImageTransferMessage{
		SessionID: "sess-pty-1",
		MediaType: "image/png",
		Data:      base64.StdEncoding.EncodeToString([]byte("data")),
	})
	if err == nil {
		t.Fatal("expected error for PTY session")
	}
	if !strings.Contains(err.Error(), "Image transfer is only supported in SDK mode sessions") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestWriteImageInput_InvalidMediaType(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)

	sdkHandle := newTestSDKHandle()
	session := &RemoteSession{
		ID:     "sess-sdk-1",
		Tool:   "claude",
		Status: SessionRunning,
		Exec:   sdkHandle,
	}
	manager.mu.Lock()
	manager.sessions[session.ID] = session
	manager.mu.Unlock()

	err := manager.WriteImageInput("sess-sdk-1", ImageTransferMessage{
		SessionID: "sess-sdk-1",
		MediaType: "image/bmp",
		Data:      base64.StdEncoding.EncodeToString([]byte("data")),
	})
	if err == nil {
		t.Fatal("expected error for unsupported media type")
	}
	if !strings.Contains(err.Error(), "unsupported media_type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteImageInput_EmptyData(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)

	sdkHandle := newTestSDKHandle()
	session := &RemoteSession{
		ID:     "sess-sdk-2",
		Tool:   "claude",
		Status: SessionRunning,
		Exec:   sdkHandle,
	}
	manager.mu.Lock()
	manager.sessions[session.ID] = session
	manager.mu.Unlock()

	err := manager.WriteImageInput("sess-sdk-2", ImageTransferMessage{
		SessionID: "sess-sdk-2",
		MediaType: "image/png",
		Data:      "",
	})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
	if !strings.Contains(err.Error(), "data is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteImageInput_OversizedUpload(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)

	sdkHandle := newTestSDKHandle()
	session := &RemoteSession{
		ID:     "sess-sdk-3",
		Tool:   "claude",
		Status: SessionRunning,
		Exec:   sdkHandle,
	}
	manager.mu.Lock()
	manager.sessions[session.ID] = session
	manager.mu.Unlock()

	// Create data exceeding 5MB upload limit
	bigData := strings.Repeat("A", ImageUploadSizeLimit+1)
	bigB64 := base64.StdEncoding.EncodeToString([]byte(bigData))

	err := manager.WriteImageInput("sess-sdk-3", ImageTransferMessage{
		SessionID: "sess-sdk-3",
		MediaType: "image/png",
		Data:      bigB64,
	})
	if err == nil {
		t.Fatal("expected error for oversized upload")
	}
	if !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteImageInput_InvalidBase64(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)

	sdkHandle := newTestSDKHandle()
	session := &RemoteSession{
		ID:     "sess-sdk-4",
		Tool:   "claude",
		Status: SessionRunning,
		Exec:   sdkHandle,
	}
	manager.mu.Lock()
	manager.sessions[session.ID] = session
	manager.mu.Unlock()

	err := manager.WriteImageInput("sess-sdk-4", ImageTransferMessage{
		SessionID: "sess-sdk-4",
		MediaType: "image/png",
		Data:      "not-valid-base64!!!",
	})
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
	if !strings.Contains(err.Error(), "invalid base64") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteImageInput_NilExec(t *testing.T) {
	app := &App{}
	manager := NewRemoteSessionManager(app)

	session := &RemoteSession{
		ID:     "sess-nil-exec",
		Tool:   "claude",
		Status: SessionRunning,
		Exec:   nil,
	}
	manager.mu.Lock()
	manager.sessions[session.ID] = session
	manager.mu.Unlock()

	err := manager.WriteImageInput("sess-nil-exec", ImageTransferMessage{
		SessionID: "sess-nil-exec",
		MediaType: "image/png",
		Data:      base64.StdEncoding.EncodeToString([]byte("data")),
	})
	if err == nil {
		t.Fatal("expected error for nil exec")
	}
	if !strings.Contains(err.Error(), "session execution not available") {
		t.Fatalf("unexpected error: %v", err)
	}
}

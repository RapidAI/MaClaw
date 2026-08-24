package main

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const activeLocalDocumentContextLifetime = 30 * time.Minute

// activeLocalDocumentContext is a host-owned grant to a desktop file selected
// in the current task. The path never reaches a model schema or conversation
// history. A later turn must revalidate the exact observed file before bytes
// can be admitted to a fresh, one-shot semantic artifact grant.
type activeLocalDocumentContext struct {
	CanonicalPath string
	Format        string
	MIMEType      string
	Size          int64
	ModTimeNS     int64
	Digest        string
	CapturedAt    time.Time
	ExpiresAt     time.Time
}

type activeLocalDocumentContextUse int

const (
	activeLocalDocumentUnavailable activeLocalDocumentContextUse = iota
	activeLocalDocumentReuse
	activeLocalDocumentPickerMismatch
)

func activeLocalDocumentContextKey(userID, channel, destination string) string {
	return sessionGovernedTaskKey(userID, channel, destination)
}

// captureSelectedLocalDocuments supersedes the prior document context only
// when the host has received a *current* desktop-picker marker. It never
// trusts a historical marker or free-form path mentioned by the model/user.
func (h *IMMessageHandler) captureSelectedLocalDocuments(userID, channel, destination, userText string) error {
	if h == nil || semanticChannelScope(channel) != "desktop" || agent.CurrentLocalFilePathPromptIndex(userText) < 0 {
		return nil
	}
	key := activeLocalDocumentContextKey(userID, channel, destination)
	// A new picker gesture is a task-resource boundary even if its contents are
	// unsupported or invalid; it must never leave an older document reusable.
	h.activeLocalDocuments.Delete(key)
	paths := agent.SelectedLocalFilePathsFromPrompt(userText)
	contexts := make([]activeLocalDocumentContext, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	var captureErr error
	for _, path := range paths {
		if !agent.IsDocumentFilePath(path) {
			continue
		}
		context, err := snapshotActiveLocalDocument(path)
		if err != nil {
			// Do not retain a partially captured multi-document set. The user can
			// reselect the document, while any valid siblings remain usable for
			// the current request through normal auto-extraction.
			captureErr = err
			break
		}
		identity := strings.ToLower(context.CanonicalPath)
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		contexts = append(contexts, context)
	}
	if captureErr == nil && len(contexts) > 0 {
		h.activeLocalDocuments.Store(key, contexts)
	}
	return captureErr
}

func snapshotActiveLocalDocument(path string) (activeLocalDocumentContext, error) {
	clean, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return activeLocalDocumentContext{}, fmt.Errorf("trusted_document_context_path_invalid")
	}
	// Resolve a link once at authorization time so a later link retarget cannot
	// quietly turn an existing grant into access to another document.
	if resolved, resolveErr := filepath.EvalSymlinks(clean); resolveErr == nil {
		clean = resolved
	} else if !os.IsNotExist(resolveErr) {
		return activeLocalDocumentContext{}, fmt.Errorf("trusted_document_context_unavailable")
	}
	format, mimeType, ok := semanticDocumentFormat(filepath.Base(clean), "")
	if !ok {
		return activeLocalDocumentContext{}, fmt.Errorf("trusted_document_context_unsupported")
	}
	data, info, err := readStableActiveLocalDocument(clean)
	if err != nil {
		return activeLocalDocumentContext{}, err
	}
	digest := sha256.Sum256(data)
	capturedAt := time.Now().UTC()
	return activeLocalDocumentContext{
		CanonicalPath: clean, Format: format, MIMEType: mimeType, Size: info.Size(),
		ModTimeNS: info.ModTime().UnixNano(), Digest: fmt.Sprintf("%x", digest[:]), CapturedAt: capturedAt, ExpiresAt: capturedAt.Add(activeLocalDocumentContextLifetime),
	}, nil
}

func readStableActiveLocalDocument(path string) ([]byte, os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("trusted_document_context_unavailable")
	}
	defer file.Close()
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > agent.MaxOfficeReadFileBytes {
		return nil, nil, fmt.Errorf("trusted_document_context_invalid")
	}
	data, err := io.ReadAll(io.LimitReader(file, agent.MaxOfficeReadFileBytes+1))
	if err != nil || int64(len(data)) != before.Size() || int64(len(data)) > agent.MaxOfficeReadFileBytes {
		return nil, nil, fmt.Errorf("trusted_document_context_unavailable")
	}
	after, err := file.Stat()
	if err != nil || after.Size() != before.Size() || after.ModTime() != before.ModTime() {
		return nil, nil, fmt.Errorf("trusted_document_context_changed")
	}
	// Fstat alone follows the opened handle. Re-stat the pathname as well, so
	// a replace/rename between open and read cannot keep an old handle usable
	// under a now-different remembered resource reference.
	current, err := os.Stat(path)
	if err != nil || !os.SameFile(before, current) || current.Size() != before.Size() || current.ModTime() != before.ModTime() {
		return nil, nil, fmt.Errorf("trusted_document_context_changed")
	}
	return data, before, nil
}

func (h *IMMessageHandler) hasActiveLocalDocument(userID, channel, destination string) bool {
	if h == nil || semanticChannelScope(channel) != "desktop" {
		return false
	}
	key := activeLocalDocumentContextKey(userID, channel, destination)
	value, ok := h.activeLocalDocuments.Load(key)
	if !ok {
		return false
	}
	contexts, valid := value.([]activeLocalDocumentContext)
	if !valid || len(contexts) == 0 || activeLocalDocumentContextsExpired(contexts, time.Now().UTC()) {
		h.activeLocalDocuments.Delete(key)
		return false
	}
	return true
}

func activeLocalDocumentContextsExpired(contexts []activeLocalDocumentContext, now time.Time) bool {
	for _, context := range contexts {
		if context.ExpiresAt.IsZero() || !now.Before(context.ExpiresAt) {
			return true
		}
	}
	return false
}

func activeDocumentContinuationIntent(result intent.ClassificationResult, userText string) bool {
	if activeDocumentRequestSupersedesContext(userText) {
		return false
	}
	if classificationHasLabel(result, intent.LabelDocumentRead) {
		return true
	}
	if result.Primary == intent.LabelContinuation {
		return explicitActiveDocumentContinuationCue(userText)
	}
	if result.Primary != intent.LabelUnknown && result.Primary != intent.LabelAmbiguous && result.Primary != intent.LabelNonCoding {
		return false
	}
	text := strings.ToLower(strings.TrimSpace(semanticUserIntentText(userText)))
	return explicitActiveDocumentContinuationCue(text)
}

// An explicitly named/reselected source is a new resource request, not a
// continuation of the old desktop-picker grant. Never silently substitute the
// active document merely because the classifier emitted document.read.local.
func activeDocumentRequestSupersedesContext(userText string) bool {
	if hasCurrentLocalDocumentPickerInput(userText) {
		return false
	}
	if len(extractTaskAnchorSourcePaths(userText)) > 0 {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(semanticUserIntentText(userText)))
	for _, cue := range []string{"另一份", "另一个文件", "其他文件", "新文件", "重新选择", "different file", "another file", "new file", "reselect"} {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

// activeLocalDocumentUsableForTurn is the final authority for reuse. A
// current picker marker can use an active context only if it exactly matches
// the context captured before planning; this makes ordering mistakes and
// alternate planner entry points fail closed rather than reading an older file.
func (h *IMMessageHandler) activeLocalDocumentUseForTurn(userID, channel, destination, userText string) activeLocalDocumentContextUse {
	if !h.hasActiveLocalDocument(userID, channel, destination) || activeDocumentRequestSupersedesContext(userText) {
		return activeLocalDocumentUnavailable
	}
	if !hasCurrentLocalDocumentPickerInput(userText) {
		return activeLocalDocumentReuse
	}
	value, ok := h.activeLocalDocuments.Load(activeLocalDocumentContextKey(userID, channel, destination))
	if !ok {
		return activeLocalDocumentPickerMismatch
	}
	contexts, ok := value.([]activeLocalDocumentContext)
	if ok && activeLocalDocumentContextsMatchPicker(contexts, userText) {
		return activeLocalDocumentReuse
	}
	return activeLocalDocumentPickerMismatch
}

func (h *IMMessageHandler) activeLocalDocumentUsableForTurn(userID, channel, destination, userText string) bool {
	return h.activeLocalDocumentUseForTurn(userID, channel, destination, userText) == activeLocalDocumentReuse
}

func activeLocalDocumentContextsMatchPicker(contexts []activeLocalDocumentContext, userText string) bool {
	want := make(map[string]struct{})
	for _, path := range agent.SelectedLocalFilePathsFromPrompt(userText) {
		if !agent.IsDocumentFilePath(path) {
			continue
		}
		canonical, err := filepath.Abs(strings.TrimSpace(path))
		if err != nil {
			return false
		}
		if resolved, err := filepath.EvalSymlinks(canonical); err == nil {
			canonical = resolved
		} else {
			return false
		}
		want[strings.ToLower(filepath.Clean(canonical))] = struct{}{}
	}
	if len(want) == 0 || len(want) != len(contexts) {
		return false
	}
	for _, context := range contexts {
		if _, ok := want[strings.ToLower(filepath.Clean(context.CanonicalPath))]; !ok {
			return false
		}
	}
	return true
}

// A current picker marker is host-authorized input. It is allowed to resolve
// through the active context because captureSelectedLocalDocuments refreshed
// that context before semantic planning. A bare path in ordinary prose is not
// equivalent authority and must never select the previous document.
func hasCurrentLocalDocumentPickerInput(userText string) bool {
	if agent.CurrentLocalFilePathPromptIndex(userText) < 0 {
		return false
	}
	for _, path := range agent.SelectedLocalFilePathsFromPrompt(userText) {
		if agent.IsDocumentFilePath(path) {
			return true
		}
	}
	return false
}

func explicitActiveDocumentContinuationCue(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	for _, cue := range []string{"总结", "概括", "浓缩", "压缩", "精简", "改写", "润色", "扩写", "提炼", "翻译", "重写", "基于上述", "根据上述", "按这份", "这份文档", "这篇", "这份材料", "summari", "condens", "rewrite", "expand", "translate", "polish", "this document", "the document"} {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func classificationWithActiveDocumentRead(current intent.ClassificationResult) intent.ClassificationResult {
	if classificationHasLabel(current, intent.LabelDocumentRead) {
		return current
	}
	current.Primary = intent.LabelDocumentRead
	current.Secondary = append([]intent.IntentLabel(nil), current.Secondary...)
	if current.Confidence < .90 {
		current.Confidence = .90
	}
	current.Degraded = false
	current.Layer = 3
	return current
}

func semanticDocumentReadNeedPresent(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		if need.Capability == "document.read.local" {
			return true
		}
	}
	return false
}

// semanticActiveLocalDocumentInputsForTurn turns an already-authorized local
// context into this turn's artifact. Revalidation is intentionally performed
// on every use; a changed/missing file fails closed instead of following its
// remembered path.
func (h *IMMessageHandler) semanticActiveLocalDocumentInputsForTurn(rootTaskID, turnID, userID, channel, destination string) ([]semanticTrustedArtifactInput, bool, error) {
	if !h.hasActiveLocalDocument(userID, channel, destination) {
		return nil, false, nil
	}
	value, _ := h.activeLocalDocuments.Load(activeLocalDocumentContextKey(userID, channel, destination))
	contexts, ok := value.([]activeLocalDocumentContext)
	if !ok || len(contexts) == 0 {
		h.activeLocalDocuments.Delete(activeLocalDocumentContextKey(userID, channel, destination))
		return nil, true, fmt.Errorf("trusted_document_context_invalid")
	}
	inputs := make([]semanticTrustedArtifactInput, 0, len(contexts))
	scope := tool.InvocationScope{RootTaskID: rootTaskID, PlanID: "input:" + strings.TrimSpace(turnID), SessionID: userID, TurnID: turnID, PrincipalID: userID}
	for _, context := range contexts {
		if context.ExpiresAt.IsZero() || !time.Now().UTC().Before(context.ExpiresAt) || len(context.Digest) < 24 {
			h.activeLocalDocuments.Delete(activeLocalDocumentContextKey(userID, channel, destination))
			return nil, true, fmt.Errorf("trusted_document_context_expired")
		}
		data, info, err := readStableActiveLocalDocument(context.CanonicalPath)
		if err != nil {
			h.activeLocalDocuments.Delete(activeLocalDocumentContextKey(userID, channel, destination))
			return nil, true, fmt.Errorf("trusted_document_context_stale")
		}
		digest := sha256.Sum256(data)
		if info.Size() != context.Size || info.ModTime().UnixNano() != context.ModTimeNS || !strings.EqualFold(fmt.Sprintf("%x", digest[:]), context.Digest) {
			h.activeLocalDocuments.Delete(activeLocalDocumentContextKey(userID, channel, destination))
			return nil, true, fmt.Errorf("trusted_document_context_stale")
		}
		payload, err := tool.NewArtifactPayload(scope, "trusted-input:desktop-picker:"+context.Digest[:24], "document", context.MIMEType, base64.StdEncoding.EncodeToString(data), time.Now().UTC())
		if err != nil {
			return nil, true, fmt.Errorf("trusted_document_context_invalid")
		}
		inputs = append(inputs, semanticTrustedArtifactInput{Payload: payload, Format: context.Format, Suffix: semanticDocumentTempSuffix(context.CanonicalPath, context.Format)})
	}
	return inputs, true, nil
}

func (h *IMMessageHandler) clearActiveLocalDocumentsForUser(userID string) {
	if h == nil {
		return
	}
	prefix := strings.TrimSpace(userID) + "\x1f"
	h.activeLocalDocuments.Range(func(key, _ any) bool {
		if text, ok := key.(string); ok && strings.HasPrefix(text, prefix) {
			h.activeLocalDocuments.Delete(key)
		}
		return true
	})
}

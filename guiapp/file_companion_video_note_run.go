package guiapp

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
)

// videoNoteMedia is the process and download boundary for one note.
// Tests replace it. Production uses ffmpeg, and only after the user asks for a note.
type videoNoteMedia interface {
	Locate() (ffmpeg, ffprobe string, ok bool)
	Install(ctx context.Context) (ffmpeg, ffprobe string, err error)
	Probe(ctx context.Context, media string) (videoNoteProbeInfo, error)
	ExtractWAV(ctx context.Context, media string, start time.Duration, seconds int, dest string) error
	ExtractSubs(ctx context.Context, media, dest string) error
	ExtractGray(ctx context.Context, media string, at time.Duration) ([]byte, error)
	ExtractJPEG(ctx context.Context, media string, at time.Duration) ([]byte, error)
}

type videoNoteProbeInfo struct {
	Duration time.Duration
	HasAudio bool
	HasText  bool
	Chapters []videoNoteChapter
	Corrupt  bool
}

type videoNoteChapter struct {
	Start time.Duration
	Title string
}

// fileCompanionNoteModelRequest is the one model call for a note.
// Frames are not IMUserMessage.Attachments and are not written into history.
type fileCompanionNoteModelRequest struct {
	Msg    IMUserMessage
	Frames []videoNoteFrame
}

var fileCompanionVideoNoteMediaHook func() videoNoteMedia

var fileCompanionNoteModelHook func(fileCompanionNoteModelRequest) *IMAgentResponse

// fileCompanionNoteModelSlotBudget is how long one note may hold a foreground
// scheduler slot. The note still waits for the reply until its context ends.
var fileCompanionNoteModelSlotBudget = 2 * time.Minute

// fileCompanionNoteLLMConfigHook replaces profile lookup in tests.
var fileCompanionNoteLLMConfigHook func() corelib.MaclawLLMConfig

// fileCompanionNoteVisionHook overrides SupportsVision in tests.
var fileCompanionNoteVisionHook func() bool

// fileCompanionNoteASRHook replaces local SenseVoice for a note. Nil uses TranscribeWAVBytes.
var fileCompanionNoteASRHook func(wav []byte) (string, error)

// fileCompanionNoteOCRHook replaces PP-OCRv6 for a note. Nil uses the shared provider.
var fileCompanionNoteOCRHook func(jpeg []byte) (string, error)

// fileCompanionNoteOCRStateHook reports (enabled, model file present). Nil reads the app.
var fileCompanionNoteOCRStateHook func() (enabled, model bool)

func (a *App) fileCompanionNoteMedia() videoNoteMedia {
	if fileCompanionVideoNoteMediaHook != nil {
		return fileCompanionVideoNoteMediaHook()
	}
	return &fileCompanionFFmpegMedia{app: a}
}

func (a *App) fileCompanionChatGen(userID string) uint64 {
	if a == nil {
		return 0
	}
	st := a.companionRuntime()
	st.chatMu.Lock()
	defer st.chatMu.Unlock()
	if st.chatGen == nil {
		return 0
	}
	return st.chatGen[userID]
}

func (a *App) fileCompanionBeginNote(sessionID string, parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	if a == nil {
		return ctx, cancel
	}
	st := a.companionRuntime()
	st.noteMu.Lock()
	if st.noteCancel == nil {
		st.noteCancel = map[string]context.CancelFunc{}
	}
	if prev := st.noteCancel[sessionID]; prev != nil {
		prev()
	}
	st.noteCancel[sessionID] = cancel
	st.noteMu.Unlock()
	return ctx, cancel
}

func (a *App) fileCompanionEndNote(sessionID string, cancel context.CancelFunc) {
	if cancel != nil {
		cancel()
	}
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.noteMu.Lock()
	if st.noteCancel != nil {
		delete(st.noteCancel, sessionID)
	}
	st.noteMu.Unlock()
}

func (a *App) fileCompanionCancelNote(sessionID string) {
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.noteMu.Lock()
	cancel := st.noteCancel[sessionID]
	st.noteMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// FileCompanionCancelNote cancels the in-flight note for an open file.
// It does not bump chatGen and does not drop the transcript.
func (a *App) FileCompanionCancelNote(path string) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	_, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return err
	}
	a.fileCompanionCancelNote(fileCompanionSessionID(canonical))
	return nil
}

func (a *App) fileCompanionFailNote(requestID, userID, sentence string) error {
	if code := fileCompanionNoteFailureCode(sentence); code != "" {
		sessionID := strings.TrimPrefix(userID, "file-companion:")
		fileCompanionLogNote(sessionID, "fail", code, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	}
	resp := &IMAgentResponse{Text: "", Error: sentence}
	a.emitFileCompanionTurn(requestID, userID, resp)
	return errors.New(sentence)
}

// fileCompanionNoteStopped reports that this note must not be published.
// Closing a tab or pressing 取消 cancels the note context and does not bump
// chatGen. /clear bumps chatGen. Either one discards the files.
func (a *App) fileCompanionNoteStopped(ctx context.Context, userID string, clearGen uint64) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	if a == nil || userID == "" {
		return false
	}
	return a.fileCompanionChatGen(userID) != clearGen
}

func fileCompanionNoteFailureCode(sentence string) string {
	switch sentence {
	case fileCompanionErrVideoTooLong, fileCompanionErrAudioTooLong:
		return "too-long"
	case fileCompanionErrNoteCancelled:
		return "cancelled"
	case fileCompanionErrDecoderNoSubs, fileCompanionErrDecoderAudio, fileCompanionErrNativeLimit, fileCompanionErrOggNotOpus:
		return "no-decoder"
	default:
		return ""
	}
}

func boolToBit(on bool) int {
	if on {
		return 1
	}
	return 0
}

func fileCompanionLogNote(sessionID, stage, code string, duration time.Duration, segments, frames, dropped, ffmpeg, install, asr, subs, ocr, vision int) {
	logFileCompanion("session=%s stage=%s duration=%d segments=%d frames=%d dropped=%d ffmpeg=%d install=%d asr=%d subs=%d ocr=%d vision=%d code=%s",
		sessionID, stage, int(duration/time.Second), segments, frames, dropped, ffmpeg, install, asr, subs, ocr, vision, code)
}

func (a *App) fileCompanionLogFinishedNote(sessionID string, duration time.Duration, coverOK bool, transcript string, ffmpegOK, installed, sideOK, ocrRan bool, dropped int, ev videoNoteEvidence) {
	code := ""
	ocrFlag := 0
	if ocrRan {
		ocrFlag = 1
	} else if ev.Kind == "video" && !ev.Vision && len(ev.Frames) > 0 {
		code = "ocr-skipped"
	}
	segments := 0
	if !coverOK && duration > 0 {
		segments = int((duration + fileCompanionNoteSegment - 1) / fileCompanionNoteSegment)
		if segments > 180 {
			segments = 180
		}
	}
	asrFlag := 0
	if !coverOK && strings.TrimSpace(transcript) != "" {
		asrFlag = 1
	}
	fileCompanionLogNote(sessionID, "write", code, duration, segments, len(ev.Frames), dropped, boolToBit(ffmpegOK), boolToBit(installed), asrFlag, boolToBit(sideOK), ocrFlag, boolToBit(ev.Vision))
}

func (a *App) fileCompanionEmitNoteProgress(requestID, userID, text string) {
	if a == nil || strings.TrimSpace(text) == "" {
		return
	}
	payload, err := json.Marshal(AIAssistantStreamEvent{
		RequestID:  requestID,
		SessionKey: userID,
		Text:       text,
	})
	if err != nil {
		return
	}
	a.recordCompanionEvent("ai-assistant-progress", string(payload))
	emitter := newAIAssistantStreamEventEmitter(a, requestID, userID)
	emitter.emit("ai-assistant-progress", text)
}

func (a *App) fileCompanionRunVideoNote(path, canonical string, req FileCompanionTurnRequest) error {
	sessionID := fileCompanionSessionID(canonical)
	userID := "file-companion:" + sessionID
	if err := a.ensureFileCompanionHandler(userID, sessionID); err != nil {
		return err
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = "file-companion-note"
	}
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	clearGen := a.fileCompanionCaptureChatGen(userID)
	ctx, cancel := a.fileCompanionBeginNote(sessionID, parent)
	ctx = context.WithValue(ctx, noteTurnIDsKey{}, noteTurnIDs{RequestID: requestID, UserID: userID})
	defer a.fileCompanionEndNote(sessionID, cancel)
	work := filepath.Join(a.GetDataDir(), "file-companion", "work", sessionID)
	defer os.RemoveAll(work)

	kind := fileCompanionMediaKind(canonical)
	title := "视频笔记"
	if kind == "audio" {
		title = "音频笔记"
	}
	info, err := os.Stat(canonical)
	if err != nil || info.IsDir() {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionCorruptSentence(kind))
	}
	if info.Size() == 0 {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrEmptyFile)
	}
	if info.Size() > fileCompanionNoteMaxBytes {
		sentence := fileCompanionErrVideoTooBig
		if kind == "audio" {
			sentence = fileCompanionErrAudioTooBig
		}
		return a.fileCompanionFailNote(requestID, userID, sentence)
	}
	if ctx.Err() != nil {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
	}
	a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionNoteProgressCheck)

	sideText, cues, sideDur, truncated, sideOK := fileCompanionLoadSidecar(canonical)
	if sideOK {
		a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionNoteProgressSubs)
	}
	ev := videoNoteEvidence{Kind: kind}
	if truncated {
		ev.Notes = append(ev.Notes, fileCompanionErrSubsTruncated)
	}
	media := a.fileCompanionNoteMedia()
	route := fileCompanionAudioDecodeRoute(canonical, info.Size())
	needDecoder := kind == "video" || route == "ffmpeg" || route == "ffmpeg-ogg" || route == "ffmpeg-limit"

	var probed videoNoteProbeInfo
	probedOK := false
	ffmpegOK := false
	installed := false
	if needDecoder {
		if _, _, ok := media.Locate(); ok {
			ffmpegOK = true
		} else {
			a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionNoteProgressInstall)
			if _, _, instErr := media.Install(ctx); instErr != nil {
				if ctx.Err() != nil {
					return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
				}
				if sideOK && sidecarCovers(cues, sideDur) {
					ev.Notes = append(ev.Notes, fileCompanionErrFrameUnread)
				} else if kind == "audio" && route == "ffmpeg-ogg" {
					return a.fileCompanionFailNote(requestID, userID, fileCompanionErrOggNotOpus)
				} else if kind == "audio" && route == "ffmpeg-limit" {
					return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNativeLimit)
				} else if kind == "audio" {
					return a.fileCompanionFailNote(requestID, userID, fileCompanionErrDecoderAudio)
				} else if !sideOK {
					return a.fileCompanionFailNote(requestID, userID, fileCompanionErrDecoderNoSubs)
				}
			} else {
				ffmpegOK = true
				installed = true
			}
		}
		if ctx.Err() != nil {
			return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
		}
		if ffmpegOK {
			if info, probeErr := media.Probe(ctx, canonical); probeErr == nil {
				probed = info
				probedOK = true
			} else if ctx.Err() != nil {
				return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
			} else if sideOK {
				ev.Notes = append(ev.Notes, fileCompanionErrContainerUnread)
			} else if kind == "video" {
				return a.fileCompanionFailNote(requestID, userID, fileCompanionErrVideoCorrupt)
			}
		}
	}

	duration := sideDur
	if probedOK && probed.Duration > 0 {
		duration = probed.Duration
	}
	if duration > fileCompanionNoteMaxDuration {
		sentence := fileCompanionErrVideoTooLong
		if kind == "audio" {
			sentence = fileCompanionErrAudioTooLong
		}
		return a.fileCompanionFailNote(requestID, userID, sentence)
	}
	ev.Duration = duration

	coverOK := sideOK && sidecarCovers(cues, durationIf(duration, sideDur))
	transcript := ""
	if coverOK {
		transcript = sideText
	} else if kind == "audio" && (route == "pcm-slice" || route == "native-whole") {
		text, tooLarge, decErr := a.fileCompanionTranscribeNative(ctx, canonical, work, route)
		if ctx.Err() != nil {
			return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
		}
		if tooLarge {
			a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionNoteProgressInstall)
			if _, _, instErr := media.Install(ctx); instErr != nil {
				if ctx.Err() != nil {
					return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
				}
				if route == "pcm-slice" {
					return a.fileCompanionFailNote(requestID, userID, fileCompanionErrDecoderAudio)
				}
				return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNativeLimit)
			}
			text, decErr = a.fileCompanionTranscribeFFmpeg(ctx, media, canonical, work, duration)
			if decErr != nil && ctx.Err() != nil {
				return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
			}
		} else if decErr != nil {
			return a.fileCompanionFailNote(requestID, userID, fileCompanionErrAudioCorrupt)
		}
		transcript = text
	} else if needDecoder && probedOK {
		// A video with no audio track can still use picture text. The no-track
		// sentence is only returned after frame collection when nothing usable remains.
		if !probed.HasAudio && kind == "audio" {
			return a.fileCompanionFailNote(requestID, userID, fileCompanionErrAudioNoTrack)
		}
		if probed.HasText && !coverOK {
			if subs, subErr := a.fileCompanionExportEmbeddedSubs(ctx, media, canonical, work); subErr == nil && strings.TrimSpace(subs) != "" {
				transcript = subs
				coverOK = true
			}
		}
		if !coverOK && probed.HasAudio {
			a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionNoteProgressAudio)
			text, decErr := a.fileCompanionTranscribeFFmpeg(ctx, media, canonical, work, duration)
			if ctx.Err() != nil {
				return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
			}
			if decErr != nil && strings.TrimSpace(sideText) == "" && strings.TrimSpace(text) == "" {
				return a.fileCompanionFailNote(requestID, userID, fileCompanionCorruptSentence(kind))
			}
			transcript = text
		}
	} else if kind == "audio" && !sideOK {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrDecoderAudio)
	}

	if strings.TrimSpace(transcript) == "" && !sideOK && kind == "audio" {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoSpeech)
	}
	pictureOnly := kind == "video" && probedOK && !probed.HasAudio && !probed.HasText && !sideOK && strings.TrimSpace(transcript) == ""
	if !pictureOnly && !a.fileCompanionASRReady() && !coverOK && strings.TrimSpace(transcript) == "" {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrASRNotReady)
	}
	if !a.fileCompanionASRReady() && (coverOK || sideOK) && strings.TrimSpace(transcript) != "" {
		ev.Notes = append([]string{fileCompanionErrSubsOnly}, ev.Notes...)
	}
	if strings.TrimSpace(transcript) == "" && sideOK {
		transcript = sideText
	}
	ev.Text = fileCompanionBudgetTranscript(transcript, duration)
	var probeChapters []videoNoteChapter
	if probedOK {
		probeChapters = probed.Chapters
	}
	ev.Chapters, ev.ChaptersInferred = fileCompanionResolveChapters(probeChapters, cues)

	dropped := 0
	ocrRan := false
	if kind == "video" {
		a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionNoteProgressFrames)
		var frames []videoNoteFrame
		frames, dropped = a.fileCompanionCollectFrames(ctx, media, canonical, duration)
		if ctx.Err() != nil {
			return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
		}
		ev.Vision = fileCompanionNoteVision(a)
		if !ev.Vision {
			a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionNoteProgressOCR)
			ocrRan = a.fileCompanionOCRFrames(ctx, frames)
		}
		ev.Frames = frames
		if len(frames) == 0 && !sideOK && strings.TrimSpace(transcript) == "" {
			return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoTrack)
		}
		if fileCompanionSlideSpoken(transcript, duration, frames) {
			ev.Notes = append([]string{fileCompanionErrSlideSpoken}, ev.Notes...)
		}
	}

	if fileCompanionNoteContextTokens(a) < fileCompanionVideoNoteMinWindow {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrWindowTooSmall)
	}
	ev = fileCompanionFitNoteBudget(a, ev)
	if fileCompanionNoteContextTokens(a) < fileCompanionVideoNoteMinWindow {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrWindowTooSmall)
	}

	a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionNoteProgressWrite)
	prompt := buildNoteModelPrompt(filepath.Base(canonical), kind, ev)
	msg := IMUserMessage{
		RequestID:              requestID,
		UserID:                 userID,
		Platform:               desktopLaunchFileCompanion,
		Text:                   prompt,
		NoWorkflowInterception: true,
		AssistantBinding: &agent.AssistantBinding{
			Mode:          "file-companion",
			InitialPrompt: prompt,
		},
		CancelCtx: ctx,
	}
	var sent []videoNoteFrame
	if ev.Vision {
		sent = append([]videoNoteFrame(nil), ev.Frames...)
	}
	if a.fileCompanionNoteStopped(ctx, userID, clearGen) {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
	}
	reply, askErr := a.fileCompanionAskNoteModel(ctx, msg, sent)
	if a.fileCompanionNoteStopped(ctx, userID, clearGen) {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
	}
	if askErr != nil {
		return a.fileCompanionFailNote(requestID, userID, askErr.Error())
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteEmpty)
	}
	if len(ev.Notes) > 0 {
		var head strings.Builder
		for _, note := range ev.Notes {
			head.WriteString(note)
			head.WriteByte('\n')
		}
		reply = head.String() + reply
	}
	// Zero from the container means the length was not measured. An empty
	// web recording passes 0 itself, and that length does drop late clocks.
	stampLimit := duration
	if duration <= 0 {
		stampLimit = -1
	}
	reply = fileCompanionRewriteNoteTimestamps(reply, stampLimit)
	// Vision already saw the frames. The note the user reads still keeps
	// them, under the chapter they belong to.
	embed := ev.Frames
	pdfPath, err := fileCompanionWriteNoteFiles(a, canonical, sessionID, userID, clearGen, title, reply, embed, ctx)
	if err != nil {
		if err.Error() == fileCompanionErrNoteCancelled {
			return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
		}
		return a.fileCompanionFailNote(requestID, userID, err.Error())
	}
	visible := reply
	var keyframes []FileCompanionFigure
	if md := a.fileCompanionFindNoteMarkdown(sessionID); md != "" {
		if raw, readErr := os.ReadFile(md); readErr == nil {
			visible = fileCompanionNoteProse(string(raw))
			keyframes = a.fileCompanionNoteKeyframes(userID, string(raw), filepath.Join(filepath.Dir(md), "frames"))
		}
	}
	a.fileCompanionRememberNote(userID, strings.TrimSpace(req.Text), canonical)
	a.fileCompanionLogFinishedNote(sessionID, duration, coverOK, transcript, ffmpegOK, installed, sideOK, ocrRan, dropped, ev)
	resp := &IMAgentResponse{
		Text:                 visible,
		NoteCardPath:         pdfPath,
		LocalFilePath:        "",
		FileCompanionFigures: keyframes,
	}
	a.emitFileCompanionTurn(requestID, userID, resp)
	return nil
}

func fileCompanionCorruptSentence(kind string) string {
	if kind == "audio" {
		return fileCompanionErrAudioCorrupt
	}
	return fileCompanionErrVideoCorrupt
}

func durationIf(primary, fallback time.Duration) time.Duration {
	if primary > 0 {
		return primary
	}
	return fallback
}

// fileCompanionNoteASRReadyHook forces the ASR gate in tests. Production
// leaves it nil. A transcribe hook still counts as ready.
var fileCompanionNoteASRReadyHook func() bool

func (a *App) fileCompanionASRReady() bool {
	if fileCompanionNoteASRReadyHook != nil {
		return fileCompanionNoteASRReadyHook()
	}
	if fileCompanionNoteASRHook != nil {
		return true
	}
	if a == nil {
		return false
	}
	return a.IsASRReady()
}

func fileCompanionNoteVision(a *App) bool {
	if fileCompanionNoteVisionHook != nil {
		return fileCompanionNoteVisionHook()
	}
	if a == nil || a.imHandler == nil {
		return false
	}
	return a.imHandler.getMaclawLLMConfig().SupportsVision
}

func (a *App) fileCompanionAskNoteModel(ctx context.Context, msg IMUserMessage, frames []videoNoteFrame) (string, error) {
	if fileCompanionNoteModelHook != nil {
		resp := fileCompanionNoteModelHook(fileCompanionNoteModelRequest{Msg: msg, Frames: frames})
		if resp == nil {
			return "", errors.New(fileCompanionErrNoteEmpty)
		}
		if strings.TrimSpace(resp.Error) != "" {
			return "", errors.New(resp.Error)
		}
		return resp.Text, nil
	}
	if fileCompanionTurnModelHook != nil {
		resp := fileCompanionTurnModelHook(msg)
		if resp == nil {
			return "", errors.New(fileCompanionErrNoteEmpty)
		}
		if strings.TrimSpace(resp.Error) != "" {
			return "", errors.New(resp.Error)
		}
		return resp.Text, nil
	}
	var cfg corelib.MaclawLLMConfig
	if fileCompanionNoteLLMConfigHook != nil {
		cfg = fileCompanionNoteLLMConfigHook()
	} else if a != nil {
		cfg = a.GetMaclawLLMConfig()
	}
	content := []interface{}{
		map[string]interface{}{"type": "text", "text": msg.Text},
	}
	for _, frame := range frames {
		if len(frame.JPEG) == 0 {
			continue
		}
		b64 := base64.StdEncoding.EncodeToString(frame.JPEG)
		var block interface{}
		if cfg.Protocol == "anthropic" {
			block = map[string]interface{}{
				"type": "image",
				"source": map[string]interface{}{
					"type": "base64", "media_type": "image/jpeg", "data": b64,
				},
			}
		} else {
			block = map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]interface{}{"url": "data:image/jpeg;base64," + b64},
			}
		}
		content = append(content, block)
	}
	messages := []interface{}{
		map[string]interface{}{"role": "user", "content": content},
	}
	// The client has no deadline. fileCompanionNoteModelSlotBudget only
	// releases the foreground scheduler slot. AwaitResponse waits for the
	// body until the note context ends. A budget error here is reported as
	// 「没有得到笔记内容」 and the reply still on the wire is dropped.
	client := &http.Client{}
	resp, err := doSimpleLLMRequestWithOptions(ctx, cfg, messages, client, fileCompanionNoteModelSlotBudget, simpleLLMRequestOptions{AwaitResponse: true})
	if err != nil {
		if strings.Contains(err.Error(), "LLM_SERVICE_CREDITS_INSUFFICIENT_FOR_REQUEST") {
			return "", err
		}
		return "", errors.New(fileCompanionErrNoteEmpty)
	}
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		return "", errors.New(fileCompanionErrNoteEmpty)
	}
	return resp.Content, nil
}

// fileCompanionAudioDecodeRoute reports how a media file is decoded.
// ffmpeg-ogg and ffmpeg-limit remember why, so a failed install can use the closed sentence.
func fileCompanionAudioDecodeRoute(path string, size int64) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".wav":
		format, ok := wavAudioFormat(fileCompanionReadHead(path, 4096))
		if ok && format == 1 {
			return "pcm-slice"
		}
		return "ffmpeg"
	case ".mp3", ".opus", ".silk":
		if size > audioconv.MaxNativeAudioInputBytes {
			return "ffmpeg-limit"
		}
		return "native-whole"
	case ".ogg", ".oga":
		head := fileCompanionReadHead(path, 64<<10)
		if oggHasOpusHead(head) && size <= audioconv.MaxNativeAudioInputBytes {
			return "native-whole"
		}
		return "ffmpeg-ogg"
	default:
		return "ffmpeg"
	}
}

func fileCompanionReadHead(path string, n int) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, n)
	got, _ := f.Read(buf)
	return buf[:got]
}

func (a *App) fileCompanionTranscribeNative(ctx context.Context, path, work, route string) (string, bool, error) {
	if err := os.MkdirAll(work, 0o755); err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, err
	}
	var pieces [][]byte
	if route == "pcm-slice" || (strings.EqualFold(filepath.Ext(path), ".wav") && wavLooksPCM(data)) {
		if !wavLooksPCM(data) {
			return "", true, nil
		}
		slices, sliceErr := slicePCMWavForNative(data)
		if sliceErr != nil {
			return "", false, sliceErr
		}
		for _, slice := range slices {
			wav, convErr := fileCompanionNativeToWAV(slice, "")
			if nativeAudioTooLarge(convErr) {
				return "", true, nil
			}
			if convErr != nil {
				return "", false, convErr
			}
			pieces = append(pieces, wav)
		}
	} else {
		if len(data) > audioconv.MaxNativeAudioInputBytes {
			return "", true, nil
		}
		wav, convErr := fileCompanionNativeToWAV(data, "")
		if nativeAudioTooLarge(convErr) {
			return "", true, nil
		}
		if convErr != nil {
			return "", false, convErr
		}
		pieces, err = slicePCMWavForNative(wav)
		if err != nil {
			pieces = [][]byte{wav}
			err = nil
		}
	}
	return a.fileCompanionTranscribePieces(ctx, work, pieces, 0)
}

func wavAudioFormat(data []byte) (int, bool) {
	if len(data) < 20 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return 0, false
	}
	off := 12
	for off+8 <= len(data) {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		off += 8
		if id == "fmt " && off+2 <= len(data) {
			return int(binary.LittleEndian.Uint16(data[off : off+2])), true
		}
		if size < 0 || off+size > len(data) {
			break
		}
		off += size
		if size%2 == 1 {
			off++
		}
	}
	return 0, false
}

func wavLooksPCM(data []byte) bool {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return false
	}
	format, _, _, _, _, _, err := wavPCMLayout(data)
	return err == nil && format == 1
}

type noteTurnIDs struct {
	RequestID string
	UserID    string
}

type noteTurnIDsKey struct{}

func noteTurnFrom(ctx context.Context) (string, string) {
	if ctx == nil {
		return "", ""
	}
	ids, _ := ctx.Value(noteTurnIDsKey{}).(noteTurnIDs)
	return ids.RequestID, ids.UserID
}

func fileCompanionSegmentProgress(index, total int) string {
	return "正在识别语音（第 " + strconv.Itoa(index) + "/" + strconv.Itoa(total) + " 段）"
}

func (a *App) fileCompanionTranscribePieces(ctx context.Context, work string, pieces [][]byte, duration time.Duration) (string, bool, error) {
	if len(pieces) == 0 {
		return "", false, errors.New("no audio")
	}
	budget := fileCompanionASRBudget(duration, len(pieces))
	deadline := time.Now().Add(budget)
	// The budget deadline is on this context so a segment timeout is measured
	// after the model lock is free. The note context stays the parent.
	budgetCtx, budgetCancel := context.WithDeadline(ctx, deadline)
	defer budgetCancel()
	var b strings.Builder
	anyText := false
	for i, piece := range pieces {
		if ctx.Err() != nil {
			return b.String(), false, ctx.Err()
		}
		if requestID, userID := noteTurnFrom(ctx); requestID != "" {
			a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionSegmentProgress(i+1, len(pieces)))
		}
		remain := time.Until(deadline)
		if remain <= 0 {
			b.WriteString(fileCompanionErrSegmentUnread)
			b.WriteByte('\n')
			continue
		}
		if len(piece) == 0 {
			b.WriteString(formatNoteStamp(time.Duration(i) * fileCompanionNoteSegment))
			b.WriteByte(' ')
			b.WriteString(fileCompanionErrSegmentUnread)
			b.WriteByte('\n')
			continue
		}
		if fileCompanionWorkOverLimit(work) {
			b.WriteString(fileCompanionErrSegmentUnread)
			b.WriteByte('\n')
			continue
		}
		dest := filepath.Join(work, "segment.wav")
		if err := os.WriteFile(dest, piece, 0o644); err != nil {
			return "", false, err
		}
		wav, readErr := os.ReadFile(dest)
		text := ""
		var asrErr error
		if readErr == nil {
			text, asrErr = a.fileCompanionTranscribeWAV(budgetCtx, wav, pieceDuration(piece))
		}
		_ = os.Remove(dest)
		if asrErr != nil && ctx.Err() != nil {
			return b.String(), false, ctx.Err()
		}
		if asrErr != nil && (errors.Is(asrErr, context.DeadlineExceeded) || budgetCtx.Err() != nil) {
			b.WriteString(fileCompanionErrSegmentUnread)
			b.WriteByte('\n')
			continue
		}
		stamp := formatNoteStamp(time.Duration(i) * fileCompanionNoteSegment)
		if strings.TrimSpace(text) == "" {
			b.WriteString(stamp)
			b.WriteByte(' ')
			b.WriteString(fileCompanionErrSegmentMiss)
			b.WriteByte('\n')
			continue
		}
		anyText = true
		b.WriteString(stamp)
		b.WriteByte(' ')
		b.WriteString(strings.TrimSpace(text))
		b.WriteByte('\n')
	}
	if !anyText {
		return "", false, nil
	}
	return b.String(), false, nil
}

func pieceDuration(wav []byte) time.Duration {
	format, rate, _, _, _, dataLen, err := wavPCMLayout(wav)
	if err != nil || format != 1 || rate <= 0 {
		return fileCompanionNoteSegment
	}
	_, _, channels, bits, _, _, err2 := wavPCMLayout(wav)
	if err2 != nil || channels <= 0 || bits < 8 {
		return fileCompanionNoteSegment
	}
	block := channels * bits / 8
	if block <= 0 {
		return fileCompanionNoteSegment
	}
	sec := float64(dataLen) / float64(rate*block)
	return time.Duration(sec * float64(time.Second))
}

func fileCompanionSegmentTimeout(remain, segment time.Duration) time.Duration {
	if remain <= 0 {
		return 0
	}
	timeout := segment
	if timeout < 30*time.Second {
		timeout = 30 * time.Second
	}
	if remain < timeout {
		return remain
	}
	return timeout
}

func fileCompanionASRBudget(duration time.Duration, segments int) time.Duration {
	if duration <= 0 {
		duration = time.Duration(segments) * fileCompanionNoteSegment
	}
	budget := duration / 2
	if budget < 2*time.Minute {
		budget = 2 * time.Minute
	}
	if budget > 45*time.Minute {
		budget = 45 * time.Minute
	}
	return budget
}

// fileCompanionNoteASRGate admits one note transcription at a time.
// SenseVoice keeps its lock until Transcribe returns, and that call has no
// context. The segment timeout must not start another call while the timed-out
// one still holds the model.
var fileCompanionNoteASRGate = func() chan struct{} {
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	return ch
}()

func acquireFileCompanionNoteASR(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return false
	case <-fileCompanionNoteASRGate:
		return true
	}
}

func releaseFileCompanionNoteASR() {
	fileCompanionNoteASRGate <- struct{}{}
}

func (a *App) fileCompanionTranscribeWAV(ctx context.Context, wav []byte, seg time.Duration) (string, error) {
	if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	if !acquireFileCompanionNoteASR(ctx) {
		return "", ctx.Err()
	}
	held := true
	release := func() {
		if held {
			held = false
			releaseFileCompanionNoteASR()
		}
	}
	defer release()
	// The gate can be acquired in the same instant the budget expires.
	// Do not start SenseVoice for a wait that is already over.
	if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	if fileCompanionNoteASRHook == nil && !a.fileCompanionASRReady() {
		return "", errors.New(fileCompanionErrASRNotReady)
	}
	timeout := 30 * time.Second
	if seg > timeout {
		timeout = seg
	}
	if ctx != nil {
		if deadline, ok := ctx.Deadline(); ok {
			left := time.Until(deadline)
			if left < time.Second {
				return "", context.DeadlineExceeded
			}
			if left < timeout {
				timeout = left
			}
		}
	}
	if timeout < time.Second {
		timeout = time.Second
	}
	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	held = false
	go func() {
		defer releaseFileCompanionNoteASR()
		var text string
		var err error
		if fileCompanionNoteASRHook != nil {
			text, err = fileCompanionNoteASRHook(wav)
		} else {
			text, err = a.transcribeWAVBytes(wav, asrTranscribeOpts{Quiet: true, SkipVAD: true})
		}
		ch <- result{text, err}
	}()
	select {
	case <-tctx.Done():
		return "", tctx.Err()
	case got := <-ch:
		return got.text, got.err
	}
}

func (a *App) fileCompanionTranscribeFFmpeg(ctx context.Context, media videoNoteMedia, path, work string, duration time.Duration) (string, error) {
	if media == nil {
		return "", errors.New("no decoder")
	}
	if duration <= 0 {
		duration = fileCompanionNoteSegment
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return "", err
	}
	segments := int((duration + fileCompanionNoteSegment - 1) / fileCompanionNoteSegment)
	if segments < 1 {
		segments = 1
	}
	if segments > 180 {
		segments = 180
	}
	var pieces [][]byte
	extractBudget := duration / 4
	if extractBudget < 2*time.Minute {
		extractBudget = 2 * time.Minute
	}
	if extractBudget > 45*time.Minute {
		extractBudget = 45 * time.Minute
	}
	exCtx, exCancel := context.WithTimeout(ctx, extractBudget)
	defer exCancel()
	for i := 0; i < segments; i++ {
		if exCtx.Err() != nil {
			break
		}
		if fileCompanionWorkOverLimit(work) {
			for j := i; j < segments; j++ {
				pieces = append(pieces, nil)
			}
			break
		}
		dest := filepath.Join(work, "segment.wav")
		_ = os.Remove(dest)
		start := time.Duration(i) * fileCompanionNoteSegment
		sec := 60
		if remain := duration - start; remain > 0 && remain < fileCompanionNoteSegment {
			sec = int(remain/time.Second) + 1
			if sec < 1 {
				sec = 1
			}
		}
		if err := media.ExtractWAV(exCtx, path, start, sec, dest); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			pieces = append(pieces, nil)
			continue
		}
		wav, err := os.ReadFile(dest)
		_ = os.Remove(dest)
		if err != nil {
			pieces = append(pieces, nil)
			continue
		}
		pieces = append(pieces, wav)
	}
	text, _, err := a.fileCompanionTranscribePieces(ctx, work, pieces, duration)
	return text, err
}

func (a *App) fileCompanionExportEmbeddedSubs(ctx context.Context, media videoNoteMedia, path, work string) (string, error) {
	if media == nil {
		return "", errors.New("no decoder")
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(work, "embedded.srt")
	if err := media.ExtractSubs(ctx, path, dest); err != nil {
		return "", err
	}
	body, err := os.ReadFile(dest)
	_ = os.Remove(dest)
	if err != nil {
		return "", err
	}
	cues := parseSidecarCues(string(body))
	if len(cues) == 0 {
		return "", nil
	}
	var b strings.Builder
	for _, cue := range cues {
		b.WriteString(formatNoteStamp(cue.Start))
		b.WriteByte(' ')
		b.WriteString(strings.TrimSpace(cue.Text))
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func (a *App) fileCompanionCollectFrames(ctx context.Context, media videoNoteMedia, path string, duration time.Duration) ([]videoNoteFrame, int) {
	if media == nil || duration <= 0 {
		return nil, 0
	}
	step := duration / 96
	if step < 15*time.Second {
		step = 15 * time.Second
	}
	var candidates []videoNoteFrame
	for t := time.Duration(0); t < duration && len(candidates) < 97; t += step {
		if ctx.Err() != nil {
			return nil, len(candidates)
		}
		gray, err := media.ExtractGray(ctx, path, t)
		if err != nil || len(gray) != 256 {
			if t == 0 && step > 0 {
				continue
			}
			continue
		}
		candidates = append(candidates, videoNoteFrame{At: t, Gray: append([]byte(nil), gray...)})
		if t == 0 {
			// the next iteration adds step because t += step.
		}
	}
	kept := dedupVideoNoteFrames(candidates)
	dropped := len(candidates) - len(kept)
	var out []videoNoteFrame
	for _, frame := range kept {
		if ctx.Err() != nil {
			break
		}
		jpeg, err := media.ExtractJPEG(ctx, path, frame.At)
		if err != nil || len(jpeg) == 0 || len(jpeg) > 200<<10 {
			dropped++
			continue
		}
		frame.JPEG = jpeg
		out = append(out, frame)
	}
	capped, extra := capVideoNoteJPEGBytes(out)
	return capped, dropped + extra
}

func (a *App) fileCompanionOCRFrames(ctx context.Context, frames []videoNoteFrame) bool {
	enabled, model := true, true
	if fileCompanionNoteOCRStateHook != nil {
		enabled, model = fileCompanionNoteOCRStateHook()
	} else if a != nil {
		enabled = a.GetOCREnabled()
		info := a.CheckOCRModel()
		present, _ := info["exists"].(bool)
		model = present
	}
	if !enabled || !model {
		return false
	}
	for i := range frames {
		if ctx != nil && ctx.Err() != nil {
			return false
		}
		var text string
		var err error
		if fileCompanionNoteOCRHook != nil {
			text, err = fileCompanionNoteOCRHook(frames[i].JPEG)
		} else {
			text, err = fileCompanionRecognizeJPEG(frames[i].JPEG)
		}
		if err != nil || strings.TrimSpace(text) == "" {
			frames[i].OCR = ""
			continue
		}
		frames[i].OCR = fileCompanionTruncateRunes(strings.TrimSpace(text), 400)
	}
	return true
}

func fileCompanionRecognizeJPEG(jpeg []byte) (string, error) {
	provider := sharedNativeOCRProvider()
	if provider == nil || len(jpeg) == 0 {
		return "", errors.New("ocr missing")
	}
	results, err := provider.Recognize(base64.StdEncoding.EncodeToString(jpeg))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, item := range results {
		line := strings.TrimSpace(item.Text)
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String(), nil
}

func fileCompanionSlideSpoken(transcript string, duration time.Duration, frames []videoNoteFrame) bool {
	if duration <= 0 || len(frames) < 4 {
		return false
	}
	han := 0
	for _, r := range transcript {
		if r >= 0x4E00 && r <= 0x9FFF {
			han++
		}
	}
	windows := float64(duration) / float64(10*time.Minute)
	if windows < 1.0/600 {
		windows = 1.0 / 600
	}
	if float64(han)/windows >= 200 {
		return false
	}
	for i := 1; i < len(frames); i++ {
		if fileCompanionNoteMAD(frames[i-1].Gray, frames[i].Gray) < fileCompanionNoteMADThreshold {
			return false
		}
	}
	return true
}

func fileCompanionFitNoteBudget(a *App, ev videoNoteEvidence) videoNoteEvidence {
	tokens := fileCompanionNoteContextTokens(a)
	imageReserve := 0
	if ev.Vision {
		imageReserve = fileCompanionVideoNoteImageTokens * len(ev.Frames)
	}
	textBudget := tokens - fileCompanionVideoNoteReserveTokens - imageReserve
	for textBudget < 4000 && len(ev.Frames) > 1 {
		ev.Frames = ev.Frames[1:]
		imageReserve = 0
		if ev.Vision {
			imageReserve = fileCompanionVideoNoteImageTokens * len(ev.Frames)
		}
		textBudget = tokens - fileCompanionVideoNoteReserveTokens - imageReserve
	}
	if textBudget < 1000 {
		textBudget = 1000
	}
	if utf8.RuneCountInString(ev.Text) > textBudget {
		ev.Text = fileCompanionSampleMiddle(ev.Text, textBudget)
	}
	return ev
}

func fileCompanionSampleMiddle(text string, budget int) string {
	text = strings.TrimRight(text, "\n")
	if budget < 1 || utf8.RuneCountInString(text) <= budget {
		return text
	}
	lines := strings.Split(text, "\n")
	if len(lines) < 3 {
		return fileCompanionSampleRunes(text, budget)
	}
	rows := make([]noteSampleRow, len(lines))
	for i, line := range lines {
		stamp, _, ok := splitNoteStamp(strings.TrimSpace(line))
		rows[i] = noteSampleRow{line: line, runes: utf8.RuneCountInString(line), stamp: stamp, has: ok}
	}
	headBudget := budget / 10
	if headBudget < 1 {
		headBudget = 1
	}
	tailBudget := headBudget
	headEnd := 0
	used := 0
	for headEnd < len(rows) {
		extra := rows[headEnd].runes
		if headEnd > 0 {
			extra++
		}
		if used > 0 && used+extra > headBudget {
			break
		}
		used += extra
		headEnd++
		if used >= headBudget {
			break
		}
	}
	tailStart := len(rows)
	usedTail := 0
	for tailStart > headEnd {
		extra := rows[tailStart-1].runes
		if usedTail > 0 {
			extra++
		}
		if usedTail > 0 && usedTail+extra > tailBudget {
			break
		}
		usedTail += extra
		tailStart--
		if usedTail >= tailBudget {
			break
		}
	}
	headText := fileCompanionJoinRows(rows[:headEnd])
	tailText := fileCompanionJoinRows(rows[tailStart:])
	middle := rows[headEnd:tailStart]
	remain := budget - utf8.RuneCountInString(headText) - utf8.RuneCountInString(tailText) - 24
	if remain < 1 {
		remain = 1
	}
	picked := fileCompanionPickMiddleRows(middle, remain)
	var b strings.Builder
	if headText != "" {
		b.WriteString(headText)
		b.WriteByte('\n')
	}
	cursor := headEnd
	for _, item := range picked {
		abs := headEnd + item
		if abs > cursor {
			b.WriteString(fileCompanionOmission(rows, cursor, abs))
			b.WriteByte('\n')
		}
		b.WriteString(rows[abs].line)
		b.WriteByte('\n')
		cursor = abs + 1
	}
	if tailStart > cursor {
		b.WriteString(fileCompanionOmission(rows, cursor, tailStart))
		b.WriteByte('\n')
	}
	b.WriteString(tailText)
	out := strings.TrimRight(b.String(), "\n")
	if utf8.RuneCountInString(out) > budget {
		return fileCompanionSampleRunes(out, budget)
	}
	return out
}

func fileCompanionSampleRunes(text string, budget int) string {
	rs := []rune(text)
	if len(rs) <= budget {
		return text
	}
	marker := "\n（中间已省略）\n"
	markerRunes := utf8.RuneCountInString(marker)
	if budget <= markerRunes+2 {
		return string(rs[:budget])
	}
	head := (budget - markerRunes) / 10
	tail := head
	if head < 1 {
		head = 1
	}
	if head+tail+markerRunes > budget {
		head = 1
		tail = 1
	}
	if head+tail >= len(rs) {
		return string(rs[:budget])
	}
	return string(rs[:head]) + marker + string(rs[len(rs)-tail:])
}

type noteSampleRow struct {
	line  string
	runes int
	stamp time.Duration
	has   bool
}

func fileCompanionJoinRows(rows []noteSampleRow) string {
	if len(rows) == 0 {
		return ""
	}
	parts := make([]string, len(rows))
	for i, row := range rows {
		parts[i] = row.line
	}
	return strings.Join(parts, "\n")
}

func fileCompanionPickMiddleRows(rows []noteSampleRow, budget int) []int {
	if len(rows) == 0 || budget < 1 {
		return nil
	}
	var picked []int
	used := 0
	step := 1
	if len(rows) > 8 {
		step = len(rows) / 8
		if step < 1 {
			step = 1
		}
	}
	for i := 0; i < len(rows); i += step {
		extra := rows[i].runes + 1
		if used > 0 && used+extra > budget {
			break
		}
		picked = append(picked, i)
		used += extra
		if used >= budget {
			break
		}
	}
	return picked
}

func fileCompanionOmission(rows []noteSampleRow, from, to int) string {
	if from < 0 {
		from = 0
	}
	if to > len(rows) {
		to = len(rows)
	}
	var start, end time.Duration
	ok := false
	for i := from; i < to; i++ {
		if !rows[i].has {
			continue
		}
		if !ok {
			start = rows[i].stamp
			ok = true
		}
		end = rows[i].stamp
	}
	if !ok {
		return "（中间已省略）"
	}
	return "（中间 " + formatNoteStamp(start) + "–" + formatNoteStamp(end) + " 已省略）"
}

func fileCompanionResolveChapters(probe []videoNoteChapter, cues []sidecarCue) ([]videoNoteChapter, bool) {
	if len(probe) > 0 {
		return probe, false
	}
	if len(cues) == 0 {
		return nil, true
	}
	prev := time.Duration(0)
	hasGap := false
	for i, cue := range cues {
		if i > 0 && cue.Start-prev > 20*time.Second {
			hasGap = true
			break
		}
		if cue.End > prev {
			prev = cue.End
		}
	}
	if !hasGap {
		return nil, true
	}
	var out []videoNoteChapter
	prev = 0
	for i, cue := range cues {
		if i == 0 || cue.Start-prev > 20*time.Second {
			title := strings.TrimSpace(cue.Text)
			if title == "" {
				title = "字幕分段"
			}
			out = append(out, videoNoteChapter{Start: cue.Start, Title: title})
		}
		if cue.End > prev {
			prev = cue.End
		}
	}
	return out, false
}

func fileCompanionBudgetTranscript(text string, duration time.Duration) string {
	return text
}

func sidecarCovers(cues []sidecarCue, duration time.Duration) bool {
	if duration <= 0 || len(cues) == 0 {
		return false
	}
	return sidecarCoverageRatio(cues, duration) >= 0.8
}

func sidecarCoverageRatio(cues []sidecarCue, duration time.Duration) float64 {
	if duration <= 0 || len(cues) == 0 {
		return 0
	}
	type span struct{ a, b time.Duration }
	var spans []span
	for _, cue := range cues {
		a, b := cue.Start, cue.End
		if b < a {
			a, b = b, a
		}
		if b <= 0 {
			continue
		}
		spans = append(spans, span{a, b})
	}
	if len(spans) == 0 {
		return 0
	}
	for i := 1; i < len(spans); i++ {
		j := i
		for j > 0 && spans[j-1].a > spans[j].a {
			spans[j-1], spans[j] = spans[j], spans[j-1]
			j--
		}
	}
	var covered time.Duration
	cur := spans[0]
	for _, sp := range spans[1:] {
		if sp.a <= cur.b {
			if sp.b > cur.b {
				cur.b = sp.b
			}
			continue
		}
		covered += cur.b - cur.a
		cur = sp
	}
	covered += cur.b - cur.a
	if covered > duration {
		covered = duration
	}
	return float64(covered) / float64(duration)
}

func fileCompanionSidecarSameDir(media, candidate string) bool {
	info, err := os.Lstat(candidate)
	if err != nil || info.IsDir() {
		return false
	}
	mediaResolved, err := filepath.EvalSymlinks(media)
	if err != nil {
		mediaResolved = media
	}
	sideResolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return false
	}
	mediaDir := filepath.Clean(filepath.Dir(mediaResolved))
	sideDir := filepath.Clean(filepath.Dir(sideResolved))
	if runtime.GOOS == "windows" {
		return strings.EqualFold(mediaDir, sideDir)
	}
	return mediaDir == sideDir
}

func fileCompanionLoadSidecar(mediaPath string) (text string, cues []sidecarCue, duration time.Duration, truncated, ok bool) {
	ext := filepath.Ext(mediaPath)
	stem := strings.TrimSuffix(mediaPath, ext)
	for _, cand := range []string{".srt", ".vtt", ".ass", ".ssa"} {
		path := stem + cand
		matches, _ := filepath.Glob(stem + cand)
		if len(matches) == 0 {
			if info, err := os.Stat(path); err != nil || info.IsDir() {
				continue
			}
			matches = []string{path}
		}
		if !fileCompanionSidecarSameDir(mediaPath, matches[0]) {
			continue
		}
		info, err := os.Stat(matches[0])
		if err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		if info.Size() > 2<<20 {
			truncated = true
		}
		f, err := os.Open(matches[0])
		if err != nil {
			continue
		}
		buf := make([]byte, 2<<20)
		n, _ := f.Read(buf)
		_ = f.Close()
		body := string(buf[:n])
		parsed := parseSidecarCues(body)
		if len(parsed) == 0 {
			continue
		}
		var b strings.Builder
		var end time.Duration
		for _, cue := range parsed {
			if cue.End > end {
				end = cue.End
			}
			b.WriteString(formatNoteStamp(cue.Start))
			b.WriteByte(' ')
			b.WriteString(strings.TrimSpace(cue.Text))
			b.WriteByte('\n')
		}
		return b.String(), parsed, end, truncated, true
	}
	return "", nil, 0, false, false
}

func fileCompanionRewriteNoteTimestamps(markdown string, duration time.Duration) string {
	lines := strings.Split(markdown, "\n")
	section := ""
	var uncertain []string
	inMath := false
	mathClose := ""
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "## ") {
			section = trim
			inMath = false
			continue
		}
		if section != "## 章节" && section != "## 要点" && section != "## 金句" {
			continue
		}
		if trim == "" {
			continue
		}
		// The prompt asks for a formula on its own lines ($$...$$). That is
		// not a chapter missing a time. A list marker is not part of the
		// delimiter: "- $$" is the same opener as "$$". A formula row may
		// start with 0:00; that is not the next chapter. A chapter sentence
		// still ends a block that was left open.
		mathLine := fileCompanionDisplayMathLine(trim)
		if inMath {
			if fileCompanionDisplayMathCloses(mathLine, mathClose) {
				inMath = false
				if mathLine != trim {
					lines[i] = mathLine
				}
				continue
			}
			if _, _, prose, ok := fileCompanionCanonicalStampLine(trim); !ok || fileCompanionFormulaBody(prose) {
				continue
			}
			inMath = false
		}
		if fileCompanionDisplayMathWhole(mathLine) {
			if mathLine != trim {
				lines[i] = mathLine
			}
			continue
		}
		if closeTok, ok := fileCompanionDisplayMathOpens(mathLine); ok {
			inMath = true
			mathClose = closeTok
			if mathLine != trim {
				lines[i] = mathLine
			}
			continue
		}
		// A bullet or a 0:00–2:00 range is still a real time. A chapter or
		// quote with no time, or a time past the recording, is 时间不明.
		// A takeaway with no clock stays in 要点. A range is judged by its
		// later clock, including when a space sits around the dash. The
		// words kept after 时间不明 must not still carry that clock.
		stamp, normalized, prose, ok := fileCompanionCanonicalStampLine(trim)
		end, endProse, found, broken := fileCompanionRangeClock(trim)
		if broken {
			text := strings.TrimSpace(endProse)
			if text == "" {
				lines[i] = fileCompanionErrTimeUnknown
			} else {
				lines[i] = fileCompanionErrTimeUnknown + " " + text
			}
			uncertain = append(uncertain, lines[i])
			continue
		}
		if found {
			if !ok || end > stamp {
				stamp = end
			}
			prose = endProse
			if normalized == "" {
				normalized = trim
			}
			ok = true
		}
		if !ok {
			if section == "## 要点" {
				continue
			}
			lines[i] = fileCompanionErrTimeUnknown + " " + trim
			uncertain = append(uncertain, lines[i])
			continue
		}
		// duration < 0 means the length was not measured. Zero is an empty
		// recording, so a stamp past the slack is 时间不明.
		if duration >= 0 && stamp > duration+2*time.Second {
			text := strings.TrimSpace(prose)
			if text == "" && !found {
				text = trim
			}
			if text == "" {
				lines[i] = fileCompanionErrTimeUnknown
			} else {
				lines[i] = fileCompanionErrTimeUnknown + " " + text
			}
			uncertain = append(uncertain, lines[i])
			continue
		}
		lines[i] = normalized
	}
	if len(uncertain) == 0 {
		return strings.Join(lines, "\n")
	}
	return fileCompanionAppendUncertain(strings.Join(lines, "\n"), uncertain)
}

// fileCompanionFormulaBody is the remainder of a line that only looks like
// a stamp because a formula row starts with a time. A chapter sentence has
// Han text. A symbolic row such as "= t_0", or a TeX alignment, does not.
func fileCompanionFormulaBody(prose string) bool {
	if strings.ContainsAny(prose, `\&`) {
		return true
	}
	if strings.TrimSpace(prose) == "" {
		return false
	}
	for _, r := range prose {
		if unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return true
}

// fileCompanionDisplayMathWalk reports whether this line is part of a display
// formula. Call it once per line, in order. A chapter sentence ends an
// unclosed block and is not part of the formula.
func fileCompanionDisplayMathWalk(line string, inMath bool, mathClose string) (next bool, closeTok string, inside bool) {
	trim := strings.TrimSpace(line)
	if strings.HasPrefix(trim, "## ") {
		return false, "", false
	}
	mathLine := fileCompanionDisplayMathLine(trim)
	if inMath {
		if fileCompanionDisplayMathCloses(mathLine, mathClose) {
			return false, "", true
		}
		if _, _, prose, ok := fileCompanionCanonicalStampLine(trim); ok && !fileCompanionFormulaBody(prose) {
			return false, "", false
		}
		return true, mathClose, true
	}
	if fileCompanionDisplayMathWhole(mathLine) {
		return false, "", true
	}
	if tok, ok := fileCompanionDisplayMathOpens(mathLine); ok {
		return true, tok, true
	}
	return false, "", false
}

// fileCompanionDisplayMathLine drops a leading list marker. "- $$" and
// "* \[y\]" are formulas. A leading minus that is part of the text ("-1")
// stays, because that marker is not followed by a space.
func fileCompanionDisplayMathLine(trim string) string {
	i := 0
	for i < len(trim) {
		r, size := utf8.DecodeRuneInString(trim[i:])
		if r != '-' && r != '*' && r != '•' {
			break
		}
		i += size
	}
	if i == 0 || i >= len(trim) || (trim[i] != ' ' && trim[i] != '\t') {
		return trim
	}
	return strings.TrimSpace(trim[i:])
}

// fileCompanionDisplayMathWhole is one line that both opens and closes,
// such as $$E=mc^2$$ or \[E=mc^2\].
func fileCompanionDisplayMathWhole(trim string) bool {
	if strings.HasPrefix(trim, "$$") && strings.HasSuffix(trim, "$$") && len(trim) > 4 {
		return true
	}
	return strings.HasPrefix(trim, `\[`) && strings.HasSuffix(trim, `\]`) && len(trim) > 4
}

// fileCompanionDisplayMathOpens is a $$ or \[ that still needs its closer.
func fileCompanionDisplayMathOpens(trim string) (closeTok string, ok bool) {
	switch trim {
	case "$$":
		return "$$", true
	case `\[`:
		return `\]`, true
	}
	if strings.HasPrefix(trim, "$$") && len(trim) > 2 {
		return "$$", true
	}
	if strings.HasPrefix(trim, `\[`) && len(trim) > 2 {
		return `\]`, true
	}
	return "", false
}

func fileCompanionDisplayMathCloses(trim, closeTok string) bool {
	if closeTok == "" {
		return false
	}
	if trim == closeTok {
		return true
	}
	return len(trim) > len(closeTok) && strings.HasSuffix(trim, closeTok)
}

// fileCompanionUncertainPlaceholder is the model's "nothing to add" line.
// A later real doubt must not sit under that word.
func fileCompanionUncertainPlaceholder(trim string) bool {
	trim = strings.TrimLeft(trim, "-*• ")
	trim = strings.Trim(trim, "。．. ")
	return trim == "无"
}

// fileCompanionAppendUncertain adds host doubts and drops a lone 无.
// A doubt the section already states is not written twice.
func fileCompanionAppendUncertain(markdown string, uncertain []string) string {
	lines := strings.Split(markdown, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "## 不确定的地方" {
			start = i
			break
		}
	}
	if start < 0 {
		var b strings.Builder
		b.WriteString(strings.TrimRight(markdown, "\n"))
		b.WriteString("\n\n## 不确定的地方\n")
		for _, line := range uncertain {
			b.WriteString(strings.TrimSpace(line))
			b.WriteByte('\n')
		}
		return strings.TrimRight(b.String(), "\n")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	seen := map[string]struct{}{}
	var body []string
	for _, line := range lines[start+1 : end] {
		trim := strings.TrimSpace(line)
		if trim == "" || fileCompanionUncertainPlaceholder(trim) {
			continue
		}
		if _, ok := seen[trim]; ok {
			continue
		}
		seen[trim] = struct{}{}
		body = append(body, line)
	}
	for _, line := range uncertain {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if _, ok := seen[trim]; ok {
			continue
		}
		seen[trim] = struct{}{}
		body = append(body, trim)
	}
	var b strings.Builder
	b.WriteString(strings.Join(lines[:start+1], "\n"))
	b.WriteByte('\n')
	for _, line := range body {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	rest := strings.TrimLeft(strings.Join(lines[end:], "\n"), "\n")
	if rest != "" {
		if len(body) > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(rest)
	}
	return strings.TrimRight(b.String(), "\n")
}

// fileCompanionCanonicalStampLine drops a list marker and a false
// 时间不明 prefix when the line still begins with a stamp or a range.
func fileCompanionCanonicalStampLine(line string) (stamp time.Duration, normalized, prose string, ok bool) {
	trim := strings.TrimSpace(line)
	for strings.HasPrefix(trim, fileCompanionErrTimeUnknown) {
		trim = strings.TrimSpace(strings.TrimPrefix(trim, fileCompanionErrTimeUnknown))
	}
	trim = strings.TrimLeft(trim, "-*• ")
	if trim == "" || strings.HasPrefix(trim, "## ") || strings.HasPrefix(trim, "![") {
		return 0, "", "", false
	}
	if stamp, rest, ok := splitNoteStamp(trim); ok {
		rest = strings.TrimSpace(rest)
		normalized = formatNoteStamp(stamp)
		if rest != "" {
			normalized += " " + rest
		}
		return stamp, normalized, rest, true
	}
	token, rest, found := strings.Cut(trim, " ")
	if !found {
		return 0, "", "", false
	}
	start, ok := fileCompanionLeadingStamp(token)
	if !ok {
		return 0, "", "", false
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return 0, "", "", false
	}
	return start, token + " " + rest, rest, true
}

func splitNoteStamp(line string) (time.Duration, string, bool) {
	fields := strings.SplitN(line, " ", 2)
	if len(fields) == 0 {
		return 0, "", false
	}
	raw := fields[0]
	rest := ""
	if len(fields) == 2 {
		rest = fields[1]
	}
	parts := strings.Split(raw, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, "", false
	}
	nums := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return 0, "", false
		}
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0, "", false
			}
			n = n*10 + int(r-'0')
		}
		nums[i] = n
	}
	var h, m, s int
	if len(nums) == 2 {
		m, s = nums[0], nums[1]
	} else {
		h, m, s = nums[0], nums[1], nums[2]
	}
	if m > 59 || s > 59 {
		return 0, "", false
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second, rest, true
}

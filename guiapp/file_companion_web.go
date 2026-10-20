package guiapp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/wailsapp/wails/v2/pkg/maclawcapture"
	"github.com/wailsapp/wails/v2/pkg/maclawplayer"
)

// FileCompanionOpenResult is one open-field result. Web pages are not files.
type FileCompanionOpenResult struct {
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	URL       string `json:"url,omitempty"`
	FrameURL  string `json:"frameUrl,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Error     string `json:"error,omitempty"`
}

// FileCompanionWebNoteRequest starts a web note from the visible player
// rectangle. Fullscreen is only what the page reported. An empty rectangle
// does not start the note.
type FileCompanionWebNoteRequest struct {
	URL          string  `json:"url"`
	SessionID    string  `json:"sessionId"`
	Fullscreen   bool    `json:"fullscreen"`
	RequestID    string  `json:"requestId"`
	Text         string  `json:"text"`
	RectX        float64 `json:"rectX,omitempty"`
	RectY        float64 `json:"rectY,omitempty"`
	RectW        float64 `json:"rectW,omitempty"`
	RectH        float64 `json:"rectH,omitempty"`
	CaptureEpoch int64   `json:"captureEpoch,omitempty"`
}

type webNoteShot struct {
	At   time.Duration
	Gray []byte
	JPEG []byte
}

// fileCompanionWebCaptureHook replaces the WebView grab and process-audio
// recording in tests. A nil hook calls captureCompanionWebViewPNG and
// captureProcessAudio. Those functions refuse immediately when no WebView is
// bound, so a test that leaves the hook nil does not open a sound device.
var fileCompanionWebCaptureHook func() (shots []webNoteShot, audio []byte, refused bool)

// audclntStreamFlagsLoopback is AUDCLNT_STREAMFLAGS_LOOPBACK.
// The activation below leaves it unset. Opening the default render endpoint
// with this flag records every process. Initialize of the virtual device
// VAD\Process_Loopback must set it; that device is already limited to the
// target process tree. Zero Initialize flags fail with 0x88890021.
const audclntStreamFlagsLoopback = 0x00020000

// processAudioActivate is the Windows process-loopback activation.
// TargetPID is the WebView2 browser when this process has one, otherwise
// this process. IncludeProcessTree is required, and StreamFlags stays 0.
// The LOOPBACK bit is applied later, only to IAudioClient.Initialize.
// Include-tree does not hear a grandchild. The audio service that plays the
// page is a child of the browser, so the host PID records silence.
type processAudioActivate struct {
	TargetPID          uint32
	IncludeProcessTree bool
	StreamFlags        uint32
}

func companionProcessAudioActivate(pid uint32) processAudioActivate {
	return processAudioActivate{TargetPID: pid, IncludeProcessTree: true, StreamFlags: 0}
}

// FileCompanionOpenReference opens an arXiv paper or a web playback tab.
// A YouTube URL does not download the video and does not call FileCompanionOpen.
func (a *App) FileCompanionOpenReference(input string) (FileCompanionOpenResult, error) {
	raw := strings.TrimSpace(input)
	if id := arxivIDFromInput(raw); id != "" {
		path, err := a.FileCompanionOpenArxiv(raw)
		if err != nil {
			return FileCompanionOpenResult{Kind: "arxiv", Error: err.Error()}, err
		}
		return FileCompanionOpenResult{Kind: "arxiv", Path: path}, nil
	}
	if sentence := fileCompanionRejectWebInput(raw); sentence != "" {
		return FileCompanionOpenResult{Error: sentence}, errors.New(sentence)
	}
	frame, err := fileCompanionWebFrameURL(raw)
	if err != nil {
		return FileCompanionOpenResult{Error: err.Error()}, err
	}
	sessionID := fileCompanionSessionID(frame)
	if a != nil {
		st := a.companionRuntime()
		st.mu.Lock()
		if st.webSessions == nil {
			st.webSessions = map[string]string{}
		}
		st.webSessions[sessionID] = frame
		st.mu.Unlock()
	}
	return FileCompanionOpenResult{Kind: "web", URL: frame, FrameURL: frame, SessionID: sessionID}, nil
}

func fileCompanionRejectWebInput(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return fileCompanionErrOpenFileScheme
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Hostname())
	if scheme == "wails" || host == "wails.localhost" {
		return fileCompanionErrSelfPage
	}
	if scheme != "http" && scheme != "https" {
		return fileCompanionErrOpenFileScheme
	}
	return ""
}

func fileCompanionWebFrameURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return "", errors.New(fileCompanionErrOpenFileScheme)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	if (parsed.Scheme == "http" && parsed.Port() == "80") || (parsed.Scheme == "https" && parsed.Port() == "443") {
		parsed.Host = parsed.Hostname()
	}
	parsed.Fragment = ""
	host := parsed.Hostname()
	if host == "www.youtube.com" || host == "youtube.com" || host == "youtu.be" || host == "m.youtube.com" {
		q := parsed.Query()
		q.Del("t")
		parsed.RawQuery = q.Encode()
		// The watch page sends X-Frame-Options: SAMEORIGIN. An iframe then
		// shows 「拒绝连接」. The embed URL is the page YouTube allows others to frame.
		// The Windows top-level player does not load it. That player has no parent
		// page, so the embed endpoint answers 错误 153. FileCompanionPlayerPlace
		// opens the watch page instead.
		if embed := fileCompanionYouTubeEmbed(parsed); embed != "" {
			return embed, nil
		}
	}
	return parsed.String(), nil
}

func fileCompanionYouTubeEmbed(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	var id string
	if parsed.Hostname() == "youtu.be" {
		id = strings.Trim(parsed.EscapedPath(), "/")
	} else {
		path := strings.Trim(parsed.EscapedPath(), "/")
		switch {
		case path == "watch":
			id = parsed.Query().Get("v")
		case strings.HasPrefix(path, "embed/"):
			id = strings.TrimPrefix(path, "embed/")
		case strings.HasPrefix(path, "shorts/"):
			id = strings.TrimPrefix(path, "shorts/")
		case strings.HasPrefix(path, "live/"):
			id = strings.TrimPrefix(path, "live/")
		case strings.HasPrefix(path, "v/"):
			id = strings.TrimPrefix(path, "v/")
		}
	}
	if cut := strings.IndexAny(id, "/?"); cut >= 0 {
		id = id[:cut]
	}
	id = strings.TrimSpace(id)
	// videoseries and live_stream are embed endpoints, not video ids.
	// Treating them as an id drops list or channel. The iframe keeps this
	// URL. The Windows player opens the playlist or the channel live page.
	if id == "videoseries" || id == "live_stream" || !fileCompanionYouTubeID(id) {
		return ""
	}
	return "https://www.youtube.com/embed/" + id
}

func fileCompanionYouTubeID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// fileCompanionTopLevelPlaybackURL is the document the Windows player navigates to.
// The session and the iframe stay on the embed URL. The embed player, on
// youtube.com and on youtube-nocookie.com, requires an HTTP Referer from a
// parent page. A top-level Navigate starts at about:blank and sends none, so
// YouTube shows 「视频播放器配置错误」(错误 153). The watch page is the document
// YouTube serves for a visit, and this WebView is not framed. youtu.be and
// m.youtube.com are already rewritten to www.youtube.com/embed before this runs.
// embed/videoseries and embed/live_stream are not video ids. They open the
// playlist page or the channel live page. The iframe keeps the embed URL.
func fileCompanionTopLevelPlaybackURL(frame string) string {
	parsed, err := url.Parse(frame)
	if err != nil || parsed == nil {
		return frame
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "www.youtube.com", "youtube.com", "www.youtube-nocookie.com", "youtube-nocookie.com":
	default:
		return frame
	}
	path := strings.Trim(parsed.EscapedPath(), "/")
	const prefix = "embed/"
	if !strings.HasPrefix(path, prefix) {
		return frame
	}
	id := path[len(prefix):]
	if strings.ContainsAny(id, "/?") {
		return frame
	}
	switch id {
	case "videoseries":
		list := parsed.Query().Get("list")
		if !fileCompanionYouTubeID(list) {
			return frame
		}
		return "https://www.youtube.com/playlist?list=" + list
	case "live_stream":
		channel := parsed.Query().Get("channel")
		if !fileCompanionYouTubeID(channel) {
			return frame
		}
		return "https://www.youtube.com/channel/" + channel + "/live"
	}
	if !fileCompanionYouTubeID(id) {
		return frame
	}
	return "https://www.youtube.com/watch?v=" + id
}

// FileCompanionWebNote writes a note from the visible web player.
// CapturePreview photographs the whole WebView. The player rectangle is what
// crops that picture down to the video. An empty rectangle would keep the
// chat and the title bar, so the note does not start.
// It does not download the remote video and does not seek a media element.
func (a *App) FileCompanionWebNote(req FileCompanionWebNoteRequest) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	frame, err := fileCompanionWebFrameURL(req.URL)
	if err != nil {
		return err
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		sessionID = fileCompanionSessionID(frame)
	}
	if fileCompanionWebRect(req.RectX, req.RectY, req.RectW, req.RectH).Empty() {
		return errors.New(fileCompanionErrWebPlayerHidden)
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = "file-companion-web-note"
	}
	userID := "file-companion:" + sessionID
	if err := a.ensureFileCompanionHandler(userID, sessionID); err != nil {
		return err
	}
	st := a.companionRuntime()
	st.mu.Lock()
	if st.webSessions == nil {
		st.webSessions = map[string]string{}
	}
	st.webSessions[sessionID] = frame
	st.mu.Unlock()

	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	clearGen := a.fileCompanionCaptureChatGen(userID)
	noteCtx, noteCancel := a.fileCompanionBeginNote(sessionID, parent)
	// The shared transcriber reads these ids, as a local note does.
	// Without them it never replaces 正在记录声音, so the status stays on
	// recording after the pictures have already stopped.
	noteCtx = context.WithValue(noteCtx, noteTurnIDsKey{}, noteTurnIDs{RequestID: requestID, UserID: userID})
	defer a.fileCompanionEndNote(sessionID, noteCancel)
	// 停止 during capture or recognition ends capture only, so the note is
	// written. Recognition shows 正在识别语音 and is still that phase.
	// 停止 once the status is 正在整理笔记 cancels noteCtx.
	// Leaving the player still ends capture only.
	captureCtx, captureCancel := a.fileCompanionBeginWebCapture(sessionID, noteCtx, req.CaptureEpoch)
	defer a.fileCompanionEndWebCapture(sessionID, captureCancel)
	work := filepath.Join(a.GetDataDir(), "file-companion", "work", sessionID)
	_ = os.MkdirAll(work, 0o755)
	defer os.RemoveAll(work)

	a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionNoteProgressWatch)
	a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionNoteProgressListen)
	// playerWatch is the player this recording started on. Leaving that
	// player cancels it. Close and /clear cancel noteCtx. 停止 cancels
	// noteCtx only once the status is 正在整理笔记. Until then, including
	// 正在识别语音, it cancels captureCtx only, so a preview already
	// started is still this player.
	playerWatch := a.fileCompanionPlayerWatch()
	a.fileCompanionSetWebNotePhase(sessionID, webNotePhaseRecording)
	defer a.fileCompanionSetWebNotePhase(sessionID, webNotePhaseIdle)
	shots, audio, refused := fileCompanionCaptureWeb(captureCtx, fileCompanionWebRect(req.RectX, req.RectY, req.RectW, req.RectH), noteCtx, playerWatch)
	if noteCtx.Err() != nil {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
	}

	ev := videoNoteEvidence{Kind: "video"}
	var frames []videoNoteFrame
	for _, shot := range shots {
		gray := shot.Gray
		if grayFrameBlack(gray) {
			continue
		}
		frames = append(frames, videoNoteFrame{At: shot.At, Gray: append([]byte(nil), gray...), JPEG: append([]byte(nil), shot.JPEG...)})
	}
	frames = dedupVideoNoteFrames(frames)
	frames, frameDropped := capVideoNoteJPEGBytes(frames)
	if len(frames) == 0 {
		ev.Notes = append(ev.Notes, fileCompanionErrBlackFrame)
	}
	// Measured length, including a silent waveform. The log keeps this.
	// Chapter lines use fileCompanionNoteClock instead.
	ev.Duration = fileCompanionCaptureSpan(audio, frames)
	noAudio := refused || len(audio) == 0 || wavPCMSilent(audio)
	if noAudio {
		// A zero buffer is the device's silent flag, not a transcript.
		// Sending it to ASR lets the model invent the lecture from the frames.
		ev.Notes = append(ev.Notes, fileCompanionErrNoAudio)
	} else if !a.fileCompanionASRReady() {
		// Captured speech with no subtitle and no local ASR must fail the note.
		// Treating that as an empty transcript lets the model invent the lecture.
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrASRNotReady)
	} else {
		text, _ := a.fileCompanionTranscribeWebAudio(noteCtx, work, audio)
		if noteCtx.Err() != nil {
			return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
		}
		ev.Text = strings.TrimSpace(text)
	}
	// 口播很少 is captured speech that is still too thin for the frames.
	// No waveform is 没有录到声音, not a sparse narration.
	if !noAudio && fileCompanionSlideSpoken(ev.Text, ev.Duration, frames) {
		ev.Notes = append([]string{fileCompanionErrSlideSpoken}, ev.Notes...)
	}
	ev.Vision = fileCompanionNoteVision(a)
	ocrRan := false
	if !ev.Vision {
		a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionNoteProgressOCR)
		ocrRan = a.fileCompanionOCRFrames(noteCtx, frames)
	}
	if a.fileCompanionNoteStopped(noteCtx, userID, clearGen) {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
	}
	ev.Frames = frames
	if fileCompanionNoteContextTokens(a) < fileCompanionVideoNoteMinWindow {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrWindowTooSmall)
	}
	ev = fileCompanionFitNoteBudget(a, ev)
	frames = ev.Frames
	if fileCompanionNoteContextTokens(a) < fileCompanionVideoNoteMinWindow {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrWindowTooSmall)
	}
	// 正在整理笔记 is the stage a 停止 click cancels. Recognition before
	// this line is still the recording: 停止 there writes the note.
	a.fileCompanionSetWebNotePhase(sessionID, webNotePhaseWriting)
	a.fileCompanionEmitNoteProgress(requestID, userID, fileCompanionNoteProgressWrite)
	noteClock := fileCompanionNoteClock(noAudio, ev.Duration, frames)
	promptEv := ev
	promptEv.Duration = noteClock
	prompt := buildNoteModelPrompt(frame, "video", promptEv)
	msg := IMUserMessage{
		RequestID:              requestID,
		UserID:                 userID,
		Platform:               desktopLaunchFileCompanion,
		Text:                   prompt,
		NoWorkflowInterception: true,
		AssistantBinding:       &agent.AssistantBinding{Mode: "file-companion", InitialPrompt: prompt},
		CancelCtx:              noteCtx,
	}
	var sent []videoNoteFrame
	if ev.Vision {
		sent = frames
	}
	if a.fileCompanionNoteStopped(noteCtx, userID, clearGen) {
		return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
	}
	reply, askErr := a.fileCompanionAskNoteModel(noteCtx, msg, sent)
	if a.fileCompanionNoteStopped(noteCtx, userID, clearGen) {
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
	reply = fileCompanionRewriteNoteTimestamps(reply, noteClock)
	var embed []videoNoteFrame
	if !ev.Vision {
		embed = frames
	}
	pdf, err := fileCompanionWriteNoteFiles(a, frame, sessionID, userID, clearGen, "视频笔记", reply, embed, noteCtx)
	if err != nil {
		if err.Error() == fileCompanionErrNoteCancelled {
			return a.fileCompanionFailNote(requestID, userID, fileCompanionErrNoteCancelled)
		}
		return a.fileCompanionFailNote(requestID, userID, err.Error())
	}
	label := strings.TrimSpace(req.Text)
	if label == "" {
		label = "记笔记"
	}
	a.fileCompanionRememberNote(userID, label, frame)
	visible := reply
	if md := a.fileCompanionFindNoteMarkdown(sessionID); md != "" {
		if raw, readErr := os.ReadFile(md); readErr == nil {
			visible = fileCompanionNoteProse(string(raw))
		}
	}
	a.emitFileCompanionTurn(requestID, userID, &IMAgentResponse{
		Text:         visible,
		NoteCardPath: pdf,
		WebNote:      true,
	})
	a.fileCompanionLogFinishedNote(sessionID, ev.Duration, false, ev.Text, false, false, false, ocrRan, frameDropped, ev)
	return nil
}

// fileCompanionTranscribeWebAudio slices a process recording into the same
// 60-second pieces as a local track. It does not write the whole recording.
func (a *App) fileCompanionTranscribeWebAudio(ctx context.Context, work string, audio []byte) (string, error) {
	pieces, err := slicePCMWavForNative(audio)
	if err != nil || len(pieces) == 0 {
		pieces = [][]byte{audio}
	}
	dur := pieceDuration(audio)
	if dur <= 0 {
		dur = time.Duration(len(pieces)) * time.Second
	}
	text, _, asrErr := a.fileCompanionTranscribePieces(ctx, work, pieces, dur)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return text, asrErr
}

// FileCompanionOpenWebPage registers a cross-origin playback tab.
// An arXiv id stays on FileCompanionOpenArxiv. This function does not download.
func (a *App) FileCompanionOpenWebPage(raw string) (FileCompanionOpenResult, error) {
	text := strings.TrimSpace(raw)
	if arxivIDFromInput(text) != "" {
		return FileCompanionOpenResult{Kind: "arxiv", Error: "请填写视频页面地址"}, errors.New("请填写视频页面地址")
	}
	if sentence := fileCompanionRejectWebInput(text); sentence != "" {
		return FileCompanionOpenResult{Error: sentence}, errors.New(sentence)
	}
	frame, err := fileCompanionWebFrameURL(text)
	if err != nil {
		return FileCompanionOpenResult{Error: err.Error()}, err
	}
	sessionID := fileCompanionSessionID(frame)
	if a != nil {
		st := a.companionRuntime()
		st.mu.Lock()
		if st.webSessions == nil {
			st.webSessions = map[string]string{}
		}
		st.webSessions[sessionID] = frame
		st.mu.Unlock()
	}
	return FileCompanionOpenResult{Kind: "web", URL: frame, FrameURL: frame, SessionID: sessionID}, nil
}

// CancelFileCompanionTurn cancels the in-flight note and leaves the chat generation alone.
func (a *App) CancelFileCompanionTurn(path string) error {
	return a.FileCompanionCancelNote(path)
}

// FileCompanionWebTurnRequest is one turn on a web playback tab.
// It has no path, so it cannot enter fileCompanionGrant.
type FileCompanionWebTurnRequest struct {
	SessionID    string  `json:"sessionId"`
	Text         string  `json:"text"`
	Purpose      string  `json:"purpose,omitempty"`
	RequestID    string  `json:"requestId,omitempty"`
	Fullscreen   bool    `json:"fullscreen"`
	RectX        float64 `json:"rectX,omitempty"`
	RectY        float64 `json:"rectY,omitempty"`
	RectW        float64 `json:"rectW,omitempty"`
	RectH        float64 `json:"rectH,omitempty"`
	CaptureEpoch int64   `json:"captureEpoch,omitempty"`
}

func (a *App) fileCompanionWebFrame(sessionID string) string {
	if a == nil {
		return ""
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.webSessions == nil {
		return ""
	}
	return st.webSessions[sessionID]
}

// SendFileCompanionWebMessage runs a turn for a registered web tab.
// A note phrase records the visible player. It does not call FileCompanionOpen.
func (a *App) SendFileCompanionWebMessage(req FileCompanionWebTurnRequest) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	if !a.isFileCompanionProcess() {
		return errors.New("file companion is not running")
	}
	sessionID := strings.TrimSpace(req.SessionID)
	frame := a.fileCompanionWebFrame(sessionID)
	if frame == "" {
		return errors.New("web session is not open")
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return errors.New("text is required")
	}
	if fileCompanionIsClearCommand(text) {
		return a.ClearFileCompanionWebChat(sessionID)
	}
	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = fmt.Sprintf("file-companion-web-%d", time.Now().UnixNano())
	}
	if fileCompanionIsVideoNotePurpose(req.Purpose) || text == "记笔记" || fileCompanionVideoNotePhraseMatches(text) {
		return a.FileCompanionWebNote(FileCompanionWebNoteRequest{
			URL:          frame,
			SessionID:    sessionID,
			Fullscreen:   req.Fullscreen,
			RequestID:    requestID,
			Text:         text,
			RectX:        req.RectX,
			RectY:        req.RectY,
			RectW:        req.RectW,
			RectH:        req.RectH,
			CaptureEpoch: req.CaptureEpoch,
		})
	}
	userID := "file-companion:" + sessionID
	if err := a.ensureFileCompanionHandler(userID, sessionID); err != nil {
		return err
	}
	prompt := "你是文件伴读。当前打开的是网页：" + frame + "。不要使用工具。不要下载视频。用中文直接回答。不要声称已经生成了笔记或 PDF。如果用户想要笔记，告诉他们点「记笔记」。不要要求先全屏。"
	if excerpt := a.fileCompanionLoadSessionNoteExcerpt(sessionID); excerpt != "" {
		text = fileCompanionAppendNoteExcerpt(text, excerpt)
	}
	cancelCtx := a.ctx
	if cancelCtx == nil {
		cancelCtx = context.Background()
	}
	msg := IMUserMessage{
		RequestID:              requestID,
		UserID:                 userID,
		Platform:               desktopLaunchFileCompanion,
		Text:                   text,
		NoWorkflowInterception: true,
		AssistantBinding:       &agent.AssistantBinding{Mode: "file-companion", InitialPrompt: prompt},
		CancelCtx:              cancelCtx,
	}
	var resp *IMAgentResponse
	if fileCompanionTurnModelHook != nil {
		resp = fileCompanionTurnModelHook(msg)
	} else if a.imHandler != nil {
		resp = a.imHandler.HandleIMMessageWithProgressAndStream(msg, func(string) {}, func(string) {}, func() {}, func() {})
	}
	if resp == nil {
		return errors.New("empty response")
	}
	a.emitFileCompanionTurn(requestID, userID, resp)
	return nil
}

// ClearFileCompanionWebChat drops one web tab's transcript. It does not look up a file grant.
func (a *App) ClearFileCompanionWebChat(sessionID string) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	if !a.isFileCompanionProcess() {
		return errors.New("file companion is not running")
	}
	sessionID = strings.TrimSpace(sessionID)
	if a.fileCompanionWebFrame(sessionID) == "" {
		return errors.New("web session is not open")
	}
	userID := "file-companion:" + sessionID
	if err := a.ensureFileCompanionHandler(userID, sessionID); err != nil {
		return err
	}
	a.fileCompanionCancelNote(sessionID)
	if a.imHandler != nil {
		_, _ = a.imHandler.RequestCancelSessionForUser(userID)
	}
	side := filepath.Join(a.GetDataDir(), "file-companion", "sessions", sessionID+".json")
	if err := a.fileCompanionDropChat(userID, side); err != nil {
		return err
	}
	return nil
}

// FileCompanionWebPlayerLeft records that the visible player changed and
// ends capture. The watch is cancelled in this call, before the stop, so a
// wait that 停止 already started is woken and drops the raster. The note is
// still written. Leaving fullscreen uses CancelFileCompanionWebTurn.
// The title-bar 停止 uses StopFileCompanionWebNote.
func (a *App) FileCompanionWebPlayerLeft(sessionID string, epoch int64) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	a.retireFileCompanionPlayer()
	return a.CancelFileCompanionWebTurn(sessionID, epoch)
}

func (a *App) fileCompanionWebPlayerGen() uint64 {
	if a == nil {
		return 0
	}
	return a.companionRuntime().webPlayerGen.Load()
}

// fileCompanionPlayerWatch is the player currently on screen.
// A note copies it at start. Retire cancels that copy.
func (a *App) fileCompanionPlayerWatch() context.Context {
	if a == nil {
		return context.Background()
	}
	st := a.companionRuntime()
	st.noteMu.Lock()
	defer st.noteMu.Unlock()
	return st.currentPlayerWatchLocked()
}

// retireFileCompanionPlayer closes the watch the in-flight note is holding
// and installs the next player. Generation and watch move together.
func (a *App) retireFileCompanionPlayer() {
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.noteMu.Lock()
	defer st.noteMu.Unlock()
	if st.webPlayerWatchCancel != nil {
		st.webPlayerWatchCancel()
	}
	st.webPlayerWatch, st.webPlayerWatchCancel = context.WithCancel(context.Background())
	st.webPlayerGen.Add(1)
}

func (st *fileCompanionRuntime) currentPlayerWatchLocked() context.Context {
	if st.webPlayerWatch == nil {
		st.webPlayerWatch, st.webPlayerWatchCancel = context.WithCancel(context.Background())
	}
	return st.webPlayerWatch
}

// CancelFileCompanionWebTurn ends capture and lets the note be written.
// It does not cancel the note context and does not bump chatGen.
// Leaving the player and leaving fullscreen use this, including while the
// note is already being written. The title-bar 停止 uses StopFileCompanionWebNote.
// The player generation stays, so the preview already in flight is kept.
// Local cancel and /clear still discard the note.
// epoch is the attempt the page started. The same number must be passed
// on the note request. Zero stops only a capture that is already registered.
func (a *App) CancelFileCompanionWebTurn(sessionID string, epoch int64) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || a.fileCompanionWebFrame(sessionID) == "" {
		return nil
	}
	a.fileCompanionStopWebCapture(sessionID, epoch)
	return nil
}

const (
	webNotePhaseIdle = iota
	webNotePhaseRecording
	webNotePhaseWriting
)

// StopFileCompanionWebNote is the title-bar 停止.
// While the note is still recording or recognizing, this ends that
// recording and the note is written. Recognition shows 正在识别语音.
// Once the note is 正在整理笔记, this cancels it.
// Leaving the player stays on CancelFileCompanionWebTurn and still writes.
func (a *App) StopFileCompanionWebNote(sessionID string, epoch int64) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || a.fileCompanionWebFrame(sessionID) == "" {
		return nil
	}
	phase, matched := a.fileCompanionStopWebCapture(sessionID, epoch)
	if matched && phase == webNotePhaseWriting {
		a.fileCompanionCancelNote(sessionID)
	}
	return nil
}

func (a *App) fileCompanionSetWebNotePhase(sessionID string, phase int) {
	if a == nil || sessionID == "" {
		return
	}
	st := a.companionRuntime()
	st.noteMu.Lock()
	defer st.noteMu.Unlock()
	if st.webNotePhase == nil {
		st.webNotePhase = map[string]int{}
	}
	if phase == webNotePhaseIdle {
		delete(st.webNotePhase, sessionID)
		return
	}
	st.webNotePhase[sessionID] = phase
}

// FileCompanionDiscardWebNote cancels the in-flight web note when its tab closes.
// Stop and leaving fullscreen keep writing. Closing the tab does not.
// It does not bump chatGen and does not drop the transcript.
func (a *App) FileCompanionDiscardWebNote(sessionID string) error {
	if a == nil {
		return errors.New("app unavailable")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || a.fileCompanionWebFrame(sessionID) == "" {
		return nil
	}
	a.fileCompanionCancelNote(sessionID)
	return nil
}

// FileCompanionWebViewContainsFullscreen reads the Windows WebView flag.
// macOS and Linux stay on the page fullscreenElement signal, so this is false there.
func (a *App) FileCompanionWebViewContainsFullscreen() bool {
	// The video page is its own WebView. Fullscreen inside that document is
	// not visible on the companion page.
	if view := maclawplayer.View(); view != nil {
		return view.ContainsFullScreen()
	}
	view := maclawcapture.Current()
	if view == nil {
		return false
	}
	return view.ContainsFullScreen()
}

func (a *App) fileCompanionBeginWebCapture(sessionID string, parent context.Context, epoch int64) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	if a == nil {
		return ctx, cancel
	}
	st := a.companionRuntime()
	st.noteMu.Lock()
	if st.webStop == nil {
		st.webStop = map[string]context.CancelFunc{}
	}
	if st.webStopGen == nil {
		st.webStopGen = map[string]int64{}
	}
	if prev := st.webStop[sessionID]; prev != nil {
		prev()
	}
	stopped := epoch > 0 && st.webStopAt != nil && st.webStopAt[sessionID] >= epoch
	st.webStop[sessionID] = cancel
	st.webStopGen[sessionID] = epoch
	st.noteMu.Unlock()
	if stopped {
		cancel()
	}
	return ctx, cancel
}

func (a *App) fileCompanionEndWebCapture(sessionID string, cancel context.CancelFunc) {
	if cancel != nil {
		cancel()
	}
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.noteMu.Lock()
	if st.webStop != nil {
		delete(st.webStop, sessionID)
	}
	st.noteMu.Unlock()
}

// fileCompanionStopWebCapture cancels the recording for epoch when that
// attempt is the one registered. phase is what this session was doing.
// matched is false when epoch names a different attempt, so the caller
// must not cancel that other note. A registered cancel func is not itself
// proof that the capture loop is still running.
func (a *App) fileCompanionStopWebCapture(sessionID string, epoch int64) (phase int, matched bool) {
	if a == nil {
		return webNotePhaseIdle, false
	}
	st := a.companionRuntime()
	st.noteMu.Lock()
	if epoch > 0 {
		if st.webStopAt == nil {
			st.webStopAt = map[string]int64{}
		}
		if epoch > st.webStopAt[sessionID] {
			st.webStopAt[sessionID] = epoch
		}
	}
	cancel := st.webStop[sessionID]
	live := int64(0)
	if st.webStopGen != nil {
		live = st.webStopGen[sessionID]
	}
	if st.webNotePhase != nil {
		phase = st.webNotePhase[sessionID]
	}
	// A numbered stop belongs to one attempt. Zero is the unnumbered
	// capture used by tests that stop whatever is already registered.
	if epoch > 0 && live != epoch {
		cancel = nil
		matched = false
	} else {
		matched = cancel != nil || phase != webNotePhaseIdle
	}
	st.noteMu.Unlock()
	if cancel != nil {
		cancel()
	}
	return phase, matched
}

// FileCompanionWebHistory returns the web tab transcript. It does not call fileCompanionGrant.
func (a *App) FileCompanionWebHistory(sessionID string) ([]FileCompanionMessage, error) {
	if a == nil {
		return nil, errors.New("app unavailable")
	}
	sessionID = strings.TrimSpace(sessionID)
	if a.fileCompanionWebFrame(sessionID) == "" {
		return nil, errors.New("web session is not open")
	}
	return a.fileCompanionHistory(sessionID), nil
}

func grayFrameBlack(gray []byte) bool {
	if len(gray) == 0 {
		return true
	}
	sum := 0
	for _, b := range gray {
		sum += int(b)
	}
	return sum/len(gray) < 8
}

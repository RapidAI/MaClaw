package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

// Engine-injected post-recording choice buttons (option B).
// After a successful record_audio save, the host returns a deterministic
// response with Actions — no LLM call for the choice UI itself.
//
// Step 1 — click sends __record_post__ <action>:
//   - keep_only: host may short-circuit (MP3 delivery)
//   - minutes / transcribe: when diarization is enabled, offer step 2 (speaker confirm);
//     otherwise host ASR (transcribe) or agent context (minutes)
//
// Step 2 — click sends __record_post__ speakers <N|auto>:
//   pins known_speakers for CAM++ clustering, then continues minutes/transcribe.

const recordPostChoiceCommandPrefix = "__record_post__ "
const recordPostSpeakersCommandPrefix = "__record_post__ speakers "

type recordPostChoiceAction string

const (
	recordPostActionMinutes    recordPostChoiceAction = "minutes"
	recordPostActionTranscribe recordPostChoiceAction = "transcribe"
	recordPostActionKeepOnly   recordPostChoiceAction = "keep_only"
)

// pendingPostRecordingState holds metadata for a completed recording that is
// waiting for the user to pick a post-processing action via GUI buttons.
// Stored in IMMessageHandler.pendingPostRecording keyed by userID.
//
// Two-step flow when diarization is enabled and the user picks minutes/transcribe:
//  1. AwaitingSpeakerConfirm=false → choose minutes / transcribe / keep
//  2. AwaitingSpeakerConfirm=true  → confirm estimated speaker count, then process
type pendingPostRecordingState struct {
	Title       string
	Purpose     string
	Path        string
	MP3Path     string // sibling MP3 archive when auto-converted on save
	Format      string
	DurationSec string
	SizeBytes   string
	Report      string
	Lang        string    // UI language tag used when the choice card was shown
	CreatedAt   time.Time // absolute offer time; used for TTL (not refreshed on soft chat)

	// Speaker confirmation (step 2). Only set after minutes/transcribe is chosen
	// while CAM++ diarization is enabled.
	AwaitingSpeakerConfirm bool
	PendingAction          recordPostChoiceAction // minutes or transcribe
	SuggestedSpeakers      int                    // auto estimate; 0 = unknown/unavailable
	// KnownSpeakers is the user-confirmed pin passed to diarization (0 = auto).
	// Set only after the confirm step succeeds; used by host ASR / agent context.
	KnownSpeakers int
	WorkingState  *agent.WorkingState
}

func parseRecordPostChoiceCommand(text string) (recordPostChoiceAction, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, recordPostChoiceCommandPrefix) {
		return "", false
	}
	// Speaker-count confirm commands are handled separately.
	if strings.HasPrefix(trimmed, recordPostSpeakersCommandPrefix) {
		return "", false
	}
	action := recordPostChoiceAction(strings.TrimSpace(strings.TrimPrefix(trimmed, recordPostChoiceCommandPrefix)))
	switch action {
	case recordPostActionMinutes, recordPostActionTranscribe, recordPostActionKeepOnly:
		return action, true
	default:
		return "", false
	}
}

// parseRecordPostSpeakerCommand parses "__record_post__ speakers N|auto".
// ok=true when this is a speaker-confirm command. speakers=0 means automatic.
func parseRecordPostSpeakerCommand(text string) (speakers int, ok bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, recordPostSpeakersCommandPrefix) {
		return 0, false
	}
	arg := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, recordPostSpeakersCommandPrefix)))
	if arg == "" {
		return 0, false
	}
	if arg == "auto" || arg == "0" || arg == "automatic" {
		return 0, true
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 0 {
		return 0, false
	}
	if n > 15 {
		n = 15
	}
	return n, true
}

// speakerCountCN maps common Chinese number words used in short confirm replies.
var speakerCountCN = map[rune]int{
	'一': 1, '二': 2, '两': 2, '兩': 2, '三': 3, '四': 4, '五': 5,
	'六': 6, '七': 7, '八': 8, '九': 9, '十': 10,
}

// firstSpeakerCountInText finds the first plausible speaker count (1–15) in s,
// supporting arabic digits and single Chinese numerals (两人/3人/确认2人吧).
func firstSpeakerCountInText(s string) (int, bool) {
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r >= '0' && r <= '9' {
			j := i + 1
			for j < len(runes) && runes[j] >= '0' && runes[j] <= '9' {
				j++
			}
			n, err := strconv.Atoi(string(runes[i:j]))
			if err == nil && n >= 1 && n <= 15 {
				return n, true
			}
			i = j - 1
			continue
		}
		if n, ok := speakerCountCN[r]; ok {
			return n, true
		}
	}
	return 0, false
}

// matchRecordPostSpeakerFreeText maps plain replies like "2人"/"两人"/"自动" to a
// confirmed speaker count while the confirm step is pending.
// speakers=0 means "use automatic clustering".
func matchRecordPostSpeakerFreeText(text string) (speakers int, ok bool) {
	if n, ok := parseRecordPostSpeakerCommand(text); ok {
		return n, true
	}
	t := strings.TrimSpace(text)
	if t == "" {
		return 0, false
	}
	lower := strings.ToLower(t)
	// Auto / skip pin.
	switch lower {
	case "auto", "automatic", "自动", "自動", "auto mode", "自动估计", "自動估計", "自动识别", "自動識別":
		return 0, true
	}
	// Normalize UI button labels ("2 人（推荐）") then extract the first count.
	stripped := t
	for _, deco := range []string{
		"（推荐）", "（推薦）", "(recommended)", "(Recommended)", "（建议）", "（建議）",
	} {
		stripped = strings.ReplaceAll(stripped, deco, "")
	}
	if n, ok := firstSpeakerCountInText(stripped); ok {
		return n, true
	}
	// Bare "0" remains auto (same as button "auto").
	if strings.TrimSpace(strings.ReplaceAll(stripped, " ", "")) == "0" {
		return 0, true
	}
	return 0, false
}

// recordPostChoiceExactAliases is built once: i18n button labels (zh/en) plus
// conservative short aliases. Avoid broad tokens like "save"/"skip".
var recordPostChoiceExactAliases = sync.OnceValue(func() map[string]recordPostChoiceAction {
	out := map[string]recordPostChoiceAction{}
	add := func(s string, action recordPostChoiceAction) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			out[s] = action
		}
	}
	for _, lang := range []string{"zh", "en"} {
		add(i18n.T(i18n.MsgRecordPostBtnMinutes, lang), recordPostActionMinutes)
		add(i18n.T(i18n.MsgRecordPostBtnTranscribe, lang), recordPostActionTranscribe)
		add(i18n.T(i18n.MsgRecordPostBtnKeepOnly, lang), recordPostActionKeepOnly)
	}
	// Traditional Chinese labels (not in core i18n table yet).
	add("轉寫並生成會議紀要", recordPostActionMinutes)
	add("僅轉寫文字", recordPostActionTranscribe)
	add("不做處理", recordPostActionKeepOnly)

	for _, s := range []string{
		"1", "minutes", "纪要", "會議紀要", "会议纪要",
		"transcribe + meeting minutes", "transcribe and meeting minutes", "meeting minutes",
	} {
		add(s, recordPostActionMinutes)
	}
	for _, s := range []string{
		"2", "transcribe", "转写", "轉寫", "仅转写", "僅轉寫", "只转写", "只轉寫",
		"transcribe only", "transcription only",
	} {
		add(s, recordPostActionTranscribe)
	}
	for _, s := range []string{
		"3", "keep_only", "不做处理", "不做處理", "不处理", "不處理", "仅保存音频", "僅保存音訊",
		"keep audio only", "keep only", "no processing",
	} {
		add(s, recordPostActionKeepOnly)
	}
	return out
})

// matchRecordPostChoiceFreeText maps plain replies (option labels / 1-2-3) to
// a structured action while a post-recording choice is pending.
func matchRecordPostChoiceFreeText(text string) (recordPostChoiceAction, bool) {
	t := strings.TrimSpace(text)
	if t == "" {
		return "", false
	}
	if action, ok := parseRecordPostChoiceCommand(t); ok {
		return action, true
	}
	lower := strings.ToLower(t)
	if action, ok := recordPostChoiceExactAliases()[lower]; ok {
		return action, true
	}
	// Containment matches for slightly longer natural answers. Order matters:
	// minutes (纪要) before bare 转写; keep_only only with explicit phrasing.
	negatesMinutes := strings.Contains(t, "不要纪要") || strings.Contains(t, "不要紀要") ||
		strings.Contains(t, "不用纪要") || strings.Contains(t, "不用紀要") ||
		strings.Contains(lower, "no minutes") || strings.Contains(lower, "without minutes")
	switch {
	case !negatesMinutes && (strings.Contains(t, "会议纪要") || strings.Contains(t, "會議紀要") ||
		strings.Contains(t, "转写并生成") || strings.Contains(t, "轉寫並生成") ||
		(strings.Contains(t, "纪要") || strings.Contains(t, "紀要")) ||
		strings.Contains(lower, "meeting minutes") ||
		(strings.Contains(lower, "transcribe") && strings.Contains(lower, "minutes"))):
		return recordPostActionMinutes, true
	case strings.Contains(t, "仅转写") || strings.Contains(t, "僅轉寫") ||
		strings.Contains(t, "只转写") || strings.Contains(t, "只轉寫") ||
		strings.Contains(lower, "transcribe only") || strings.Contains(lower, "transcription only") ||
		((strings.Contains(t, "转写") || strings.Contains(t, "轉寫")) &&
			!strings.Contains(t, "纪要") && !strings.Contains(t, "紀要") && !strings.Contains(lower, "minutes")):
		return recordPostActionTranscribe, true
	case strings.Contains(t, "不做处理") || strings.Contains(t, "不做處理") ||
		strings.Contains(t, "不处理") || strings.Contains(t, "不處理") ||
		strings.Contains(t, "仅保存音频") || strings.Contains(t, "僅保存音訊") ||
		strings.Contains(lower, "keep audio") || strings.Contains(lower, "no process"):
		return recordPostActionKeepOnly, true
	}
	return "", false
}

// resolveRecordPostUILang maps app language tags to the three UI variants we ship:
// "en", "zh" (简体), "zh-Hant" (繁體).
func resolveRecordPostUILang(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "en"
	case appLanguageZhHant:
		return "zh-Hant"
	default:
		return "zh"
	}
}

// Traditional Chinese overlay for post-recording UI (core i18n is zh/en only).
var recordPostZhHant = map[string]string{
	i18n.MsgRecordPostSuccess:            "這次錄音成功！✅",
	i18n.MsgRecordPostSummaryHeading:     "**錄音摘要：**",
	i18n.MsgRecordPostLabelTitle:         "- 標題：%s",
	i18n.MsgRecordPostLabelDuration:      "- 時長：%s",
	i18n.MsgRecordPostLabelSize:          "- 大小：%s",
	i18n.MsgRecordPostLabelFormat:        "- 格式：%s",
	i18n.MsgRecordPostLabelPath:          "- 路徑（WAV）：`%s`",
	i18n.MsgRecordPostLabelMP3Path:       "- MP3 存檔：`%s`",
	i18n.MsgRecordPostChoosePrompt:       "請選擇後續處理（也可直接點下方按鈕）：",
	i18n.MsgRecordPostDefaultTitle:       "錄音",
	i18n.MsgRecordPostBtnMinutes:         "轉寫並生成會議紀要",
	i18n.MsgRecordPostBtnTranscribe:      "僅轉寫文字",
	i18n.MsgRecordPostBtnKeepOnly:        "不做處理",
	i18n.MsgRecordPostSizeBytes:          "%d B",
	i18n.MsgRecordPostSizeKB:             "約%d KB",
	i18n.MsgRecordPostSizeMB:             "約%.1f MB",
	i18n.MsgRecordPostSpeakersHeading:    "**確認說話人數**",
	i18n.MsgRecordPostSpeakersSuggested:  "系統估計約 **%d** 位說話人。",
	i18n.MsgRecordPostSpeakersUnknown:    "未能自動估計人數（模型未就緒或音訊過短）。",
	i18n.MsgRecordPostSpeakersPrompt:     "請確認人數後開始轉寫（點選下方按鈕，或直接回覆如「2人」「自動」）：",
	i18n.MsgRecordPostSpeakersBtnN:       "%d 人",
	i18n.MsgRecordPostSpeakersBtnNRec:    "%d 人（推薦）",
	i18n.MsgRecordPostSpeakersBtnAuto:    "自動",
	i18n.MsgRecordPostSpeakersEstimating: "正在估計說話人數…",
}

func recordPostT(key, lang string) string {
	ui := resolveRecordPostUILang(lang)
	if ui == "zh-Hant" {
		if s, ok := recordPostZhHant[key]; ok {
			return s
		}
	}
	if ui == "en" {
		return i18n.T(key, "en")
	}
	return i18n.T(key, "zh")
}

func recordPostTf(key, lang string, args ...interface{}) string {
	return fmt.Sprintf(recordPostT(key, lang), args...)
}

func recordPostChoiceLabel(action recordPostChoiceAction, lang string) string {
	switch action {
	case recordPostActionMinutes:
		return recordPostT(i18n.MsgRecordPostBtnMinutes, lang)
	case recordPostActionTranscribe:
		return recordPostT(i18n.MsgRecordPostBtnTranscribe, lang)
	case recordPostActionKeepOnly:
		return recordPostT(i18n.MsgRecordPostBtnKeepOnly, lang)
	default:
		return string(action)
	}
}

func isSuccessfulRecordingForChoice(report string) bool {
	status := strings.ToLower(extractRecordingFieldFromReport(report, "status"))
	path := extractRecordedPathFromReport(report)
	if path == "" {
		return false
	}
	switch status {
	case "stopped", "completed", "success", "saved", "":
		// Empty status still allowed when path is present (canonical payload always has both).
		return true
	default:
		return false
	}
}

func formatBytesApprox(sizeStr, lang string) string {
	sizeStr = strings.TrimSpace(sizeStr)
	if sizeStr == "" {
		return ""
	}
	n, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil || n < 0 {
		return sizeStr
	}
	if n < 1024 {
		return recordPostTf(i18n.MsgRecordPostSizeBytes, lang, n)
	}
	if n < 1024*1024 {
		return recordPostTf(i18n.MsgRecordPostSizeKB, lang, (n+512)/1024)
	}
	return recordPostTf(i18n.MsgRecordPostSizeMB, lang, float64(n)/(1024*1024))
}

func formatDurationFromSecField(secStr string) string {
	secStr = strings.TrimSpace(secStr)
	if secStr == "" {
		return ""
	}
	sec, err := strconv.ParseFloat(secStr, 64)
	if err != nil || sec < 0 {
		return secStr
	}
	total := int(sec + 0.5)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// suggestMP3ArchivePath returns a sibling .mp3 path for archival conversion.
func suggestMP3ArchivePath(audioPath string) string {
	audioPath = strings.TrimSpace(audioPath)
	if audioPath == "" {
		return ""
	}
	ext := filepath.Ext(audioPath)
	if strings.EqualFold(ext, ".mp3") {
		return audioPath
	}
	if ext != "" {
		return strings.TrimSuffix(audioPath, ext) + ".mp3"
	}
	return audioPath + ".mp3"
}

// suggestTranscriptMarkdownPath returns a sibling *_transcript.md next to audio.
// Prefer this over inventing a workspace-relative name so archives sit with the WAV/MP3.
func suggestTranscriptMarkdownPath(audioPath string) string {
	return asrTranscriptMarkdownPath(audioPath)
}

// appendRecordingDesktopDeliveryException counters desktopWorkflowDocOverride so the
// model does not skip write_file/generate_pdf/send_file for recording products.
func appendRecordingDesktopDeliveryException(b *strings.Builder) {
	if b == nil {
		return
	}
	b.WriteString("- DESKTOP EXCEPTION (priority over workflow-doc rules): desktopWorkflowDocOverride applies only to coding/product workflow phase docs (requirements/design/tasks). For THIS recording action: prefer host-written transcript_md/transcript_pdf when present; otherwise write_file .md, generate_pdf, and send_file md/pdf/mp3 — do not only paste text into chat.\n")
}

func formatPostRecordingChoiceText(title, report, lang string) string {
	defaultTitle := recordPostT(i18n.MsgRecordPostDefaultTitle, lang)
	title = strings.TrimSpace(title)
	if title == "" {
		title = extractRecordingFieldFromReport(report, "title")
	}
	if title == "" {
		title = defaultTitle
	}
	dur := formatDurationFromSecField(extractRecordingFieldFromReport(report, "duration_sec"))
	if dur == "" {
		dur = extractRecordingFieldFromReport(report, "duration")
	}
	size := formatBytesApprox(extractRecordingFieldFromReport(report, "size_bytes"), lang)
	format := extractRecordingFieldFromReport(report, "format")
	path := extractRecordedPathFromReport(report)

	var b strings.Builder
	b.WriteString(recordPostT(i18n.MsgRecordPostSuccess, lang))
	b.WriteString("\n\n")
	b.WriteString(recordPostT(i18n.MsgRecordPostSummaryHeading, lang))
	b.WriteString("\n")
	if title != "" {
		b.WriteString(recordPostTf(i18n.MsgRecordPostLabelTitle, lang, title))
		b.WriteString("\n")
	}
	if dur != "" {
		b.WriteString(recordPostTf(i18n.MsgRecordPostLabelDuration, lang, dur))
		b.WriteString("\n")
	}
	if size != "" {
		b.WriteString(recordPostTf(i18n.MsgRecordPostLabelSize, lang, size))
		b.WriteString("\n")
	}
	if format != "" {
		b.WriteString(recordPostTf(i18n.MsgRecordPostLabelFormat, lang, strings.ToUpper(format)))
		b.WriteString("\n")
	}
	if path != "" {
		b.WriteString(recordPostTf(i18n.MsgRecordPostLabelPath, lang, path))
		b.WriteString("\n")
	}
	if mp3 := extractRecordingFieldFromReport(report, "mp3_path"); mp3 != "" {
		b.WriteString(recordPostTf(i18n.MsgRecordPostLabelMP3Path, lang, mp3))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(recordPostT(i18n.MsgRecordPostChoosePrompt, lang))
	return b.String()
}

func postRecordingChoiceActions(lang string) []IMResponseAction {
	return []IMResponseAction{
		{Label: recordPostT(i18n.MsgRecordPostBtnMinutes, lang), Command: recordPostChoiceCommandPrefix + string(recordPostActionMinutes), Style: "primary"},
		{Label: recordPostT(i18n.MsgRecordPostBtnTranscribe, lang), Command: recordPostChoiceCommandPrefix + string(recordPostActionTranscribe), Style: "default"},
		{Label: recordPostT(i18n.MsgRecordPostBtnKeepOnly, lang), Command: recordPostChoiceCommandPrefix + string(recordPostActionKeepOnly), Style: "default"},
	}
}

// formatPostRecordingSpeakerConfirmText builds the step-2 confirm card body.
// suggested=0 means estimate unavailable yet (or failed). When estimating is
// true, prefer the "still estimating" copy over the hard-failure unknown text —
// the background pre-estimate may still land; users can pick a number anytime.
func formatPostRecordingSpeakerConfirmText(lang string, suggested int, action recordPostChoiceAction, estimating bool) string {
	var b strings.Builder
	b.WriteString(recordPostT(i18n.MsgRecordPostSpeakersHeading, lang))
	b.WriteString("\n\n")
	if label := recordPostChoiceLabel(action, lang); label != "" {
		b.WriteString("- ")
		b.WriteString(label)
		b.WriteString("\n\n")
	}
	switch {
	case suggested > 0:
		b.WriteString(recordPostTf(i18n.MsgRecordPostSpeakersSuggested, lang, suggested))
	case estimating:
		b.WriteString(recordPostT(i18n.MsgRecordPostSpeakersEstimating, lang))
	default:
		b.WriteString(recordPostT(i18n.MsgRecordPostSpeakersUnknown, lang))
	}
	b.WriteString("\n\n")
	b.WriteString(recordPostT(i18n.MsgRecordPostSpeakersPrompt, lang))
	return b.String()
}

// postRecordingSpeakerConfirmActions returns 1..5 person buttons (+ recommended
// highlight) and an Auto button. When the estimate is 6–15, an extra button for
// that count is inserted so the recommendation is one tap away. Free-text still
// accepts any 1–15.
func postRecordingSpeakerConfirmActions(lang string, suggested int) []IMResponseAction {
	maxFixed := 5
	actions := make([]IMResponseAction, 0, 8)
	for n := 1; n <= maxFixed; n++ {
		style := "default"
		label := recordPostTf(i18n.MsgRecordPostSpeakersBtnN, lang, n)
		if suggested == n {
			style = "primary"
			label = recordPostTf(i18n.MsgRecordPostSpeakersBtnNRec, lang, n)
		}
		actions = append(actions, IMResponseAction{
			Label:   label,
			Command: fmt.Sprintf("%s%d", recordPostSpeakersCommandPrefix, n),
			Style:   style,
		})
	}
	if suggested > maxFixed && suggested <= 15 {
		actions = append(actions, IMResponseAction{
			Label:   recordPostTf(i18n.MsgRecordPostSpeakersBtnNRec, lang, suggested),
			Command: fmt.Sprintf("%s%d", recordPostSpeakersCommandPrefix, suggested),
			Style:   "primary",
		})
	}
	autoStyle := "default"
	if suggested <= 0 {
		autoStyle = "primary"
	}
	actions = append(actions, IMResponseAction{
		Label:   recordPostT(i18n.MsgRecordPostSpeakersBtnAuto, lang),
		Command: recordPostSpeakersCommandPrefix + "auto",
		Style:   autoStyle,
	})
	return actions
}

// shouldOfferSpeakerConfirm is true when diarization can usefully pin a count.
// Skips the confirm card when CAM++ is disabled or the model file is missing so
// users are not stuck on a step that can only fall back to plain ASR.
func (h *IMMessageHandler) shouldOfferSpeakerConfirm() bool {
	if h == nil || h.app == nil {
		return false
	}
	if !h.app.GetDiarizationEnabled() {
		return false
	}
	info := h.app.CheckDiarizationModel()
	if info == nil {
		return false
	}
	exists, _ := info["exists"].(bool)
	return exists
}

// clearPendingPostRecording drops the choice state and any cached speaker estimate.
func (h *IMMessageHandler) clearPendingPostRecording(userID string) {
	if h == nil {
		return
	}
	h.pendingPostRecording.Delete(userID)
	h.pendingSpeakerEstimates.Delete(userID)
}

// cachedSpeakerEstimate returns a background-precomputed count when present.
func (h *IMMessageHandler) cachedSpeakerEstimate(userID string) int {
	if h == nil {
		return 0
	}
	raw, ok := h.pendingSpeakerEstimates.Load(userID)
	if !ok {
		return 0
	}
	switch n := raw.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}

// storeSpeakerEstimate caches a positive estimate for the open post-recording flow.
func (h *IMMessageHandler) storeSpeakerEstimate(userID string, n int) {
	if h == nil || n <= 0 {
		return
	}
	if n > 15 {
		n = 15
	}
	h.pendingSpeakerEstimates.Store(userID, n)
}

// preEstimateSpeakersForPending runs estimation in the background after the
// first choice card is shown so step-2 confirm is near-instant when the user
// picks minutes/transcribe. Writes only to pendingSpeakerEstimates (never
// mutates pendingPostRecording) to avoid races with the confirm state machine.
func (h *IMMessageHandler) preEstimateSpeakersForPending(userID string, path, format string) {
	if h == nil || h.app == nil || !h.app.GetDiarizationEnabled() {
		return
	}
	// Avoid spinning CAM++ work when weights are not on disk yet.
	if info := h.app.CheckDiarizationModel(); info != nil {
		if exists, _ := info["exists"].(bool); !exists {
			return
		}
	}
	// Already have a warm cache for this user — skip duplicate background work.
	if h.cachedSpeakerEstimate(userID) > 0 {
		return
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	go func() {
		// Bail if the user already dismissed the choice card.
		if _, ok := h.pendingPostRecording.Load(userID); !ok {
			return
		}
		wav, errMsg := prepareASRToolWAV(path, strings.TrimSpace(format))
		if errMsg != "" || len(wav) == 0 {
			return
		}
		n, err := h.app.EstimateSpeakerCountWAVBytes(wav)
		if err != nil || n <= 0 {
			return
		}
		// Still the same open recording?
		raw, ok := h.pendingPostRecording.Load(userID)
		if !ok {
			return
		}
		pending, ok := raw.(*pendingPostRecordingState)
		if !ok || pending == nil || strings.TrimSpace(pending.Path) != path {
			return
		}
		h.storeSpeakerEstimate(userID, n)
		if recordDetailEnabled() {
			log.Printf("[record-audio] pre-estimated speakers user=%s path=%q n=%d", userID, path, n)
		}
	}()
}

// offerPostRecordingSpeakerConfirm stores step-2 state and returns the confirm card.
// Intentionally non-blocking: uses cache / pending.SuggestedSpeakers only. A cold
// cache shows "unknown + Auto" immediately; background pre-estimate (started when
// the first choice card was offered) warms the cache for subsequent opens, and
// the user can always pick 1–5 without waiting for CAM++.
func (h *IMMessageHandler) offerPostRecordingSpeakerConfirm(
	userID string,
	pending *pendingPostRecordingState,
	action recordPostChoiceAction,
	priorHistory []agent.ConversationEntry,
) *IMAgentResponse {
	if h == nil || pending == nil {
		return nil
	}
	lang := pending.Lang
	if strings.TrimSpace(lang) == "" {
		lang = "zh"
	}
	suggested := pending.SuggestedSpeakers
	if suggested <= 0 {
		suggested = h.cachedSpeakerEstimate(userID)
	}
	// Kick a background estimate if still cold (e.g. user clicked before pre-estimate finished).
	estimating := false
	if suggested <= 0 && h.shouldOfferSpeakerConfirm() {
		h.preEstimateSpeakersForPending(userID, pending.Path, pending.Format)
		// Model is present; treat cold cache as "still estimating" rather than hard failure.
		estimating = true
	}
	pending.AwaitingSpeakerConfirm = true
	pending.PendingAction = action
	pending.SuggestedSpeakers = suggested
	pending.KnownSpeakers = 0
	// Refresh TTL anchor so the user has a full window for the second step.
	pending.CreatedAt = time.Now()
	h.pendingPostRecording.Store(userID, pending)

	text := formatPostRecordingSpeakerConfirmText(lang, suggested, action, estimating)
	resp := &IMAgentResponse{
		Text:           text,
		ResponseSource: imResponseSourceAskUser.String(),
		Actions:        postRecordingSpeakerConfirmActions(lang, suggested),
		SessionKey:     userID,
	}

	history := cloneConversationEntries(priorHistory)
	userLabel := recordPostChoiceLabel(action, lang)
	if strings.TrimSpace(userLabel) == "" {
		userLabel = string(action)
	}
	if !lastUserContentEquals(history, userLabel) {
		history = append(history, agent.ConversationEntry{Role: "user", Content: userLabel})
	}
	history = append(history, agent.ConversationEntry{Role: "assistant", Content: text})
	h.saveConversationHistoryTimed(userID, history, resp)

	if recordDetailEnabled() {
		log.Printf("[record-audio] offered speaker confirm user=%s action=%s suggested=%d path=%q",
			userID, action, suggested, pending.Path)
	}
	return resp
}

// offerPostRecordingChoice builds a deterministic ask_user-style response with
// engine-injected Actions, persists history, and stores pending state for the
// next user click/reply. lang is the UI language; empty falls back via app/i18n.
func (h *IMMessageHandler) offerPostRecordingChoice(userID, title, purpose, report, lang string, priorHistory []agent.ConversationEntry, workingState *agent.WorkingState) *IMAgentResponse {
	if h == nil {
		return nil
	}
	if strings.TrimSpace(lang) == "" {
		lang = h.imCommandResponseLang("")
	}
	// Preserve original tag (zh-Hans / zh-Hant / en) for UI + agent language hints.
	text := formatPostRecordingChoiceText(title, report, lang)
	resp := &IMAgentResponse{
		Text:           text,
		ResponseSource: imResponseSourceAskUser.String(),
		Actions:        postRecordingChoiceActions(lang),
	}

	history := cloneConversationEntries(priorHistory)
	history = append(history, agent.ConversationEntry{
		Role:    "user",
		Content: report,
	})
	history = append(history, agent.ConversationEntry{
		Role:    "assistant",
		Content: text,
	})
	h.saveConversationHistoryTimed(userID, history, resp)

	path := extractRecordedPathFromReport(report)
	mp3Path := extractRecordingFieldFromReport(report, "mp3_path")
	defaultTitle := recordPostT(i18n.MsgRecordPostDefaultTitle, lang)
	if title = strings.TrimSpace(title); title == "" {
		title = extractRecordingFieldFromReport(report, "title")
	}
	if title == "" {
		title = defaultTitle
	}
	now := time.Now()
	format := extractRecordingFieldFromReport(report, "format")
	h.pendingPostRecording.Store(userID, &pendingPostRecordingState{
		Title:        title,
		Purpose:      strings.TrimSpace(purpose),
		Path:         path,
		MP3Path:      mp3Path,
		Format:       format,
		DurationSec:  extractRecordingFieldFromReport(report, "duration_sec"),
		SizeBytes:    extractRecordingFieldFromReport(report, "size_bytes"),
		Report:       report,
		Lang:         lang,
		CreatedAt:    now,
		WorkingState: agent.CloneWorkingState(workingState),
	})
	// Warm speaker estimate so the confirm step is snappy.
	h.preEstimateSpeakersForPending(userID, path, format)
	if recordDetailEnabled() {
		log.Printf("[record-audio] offered post-recording choice user=%s title=%q path=%q mp3=%q lang=%s", userID, title, path, mp3Path, lang)
	}
	return resp
}

// pendingPostRecordingForCurrentHistory validates pending post-recording state.
// Binding is intentionally absolute-offer TTL + path based (not history prefix):
// users often chat before clicking a button. Soft chat must NOT extend TTL
// forever — CreatedAt is fixed at offer time.
func pendingPostRecordingForCurrentHistory(raw interface{}, _ []agent.ConversationEntry) (*pendingPostRecordingState, bool) {
	pending, ok := raw.(*pendingPostRecordingState)
	if !ok || pending == nil {
		return nil, false
	}
	// Backward-compat: older in-memory values may still have zero CreatedAt.
	anchor := pending.CreatedAt
	if anchor.IsZero() {
		return nil, false
	}
	if time.Since(anchor) >= pendingReplyTTL {
		return nil, false
	}
	if strings.TrimSpace(pending.Path) == "" {
		return nil, false
	}
	return pending, true
}

// resolveKnownMP3ArchivePath returns an engine-produced MP3 path only when the
// completion report / pending state actually recorded one. Does NOT invent a
// sibling path — that would falsely claim a pre-built product exists.
func resolveKnownMP3ArchivePath(pending *pendingPostRecordingState) string {
	if pending == nil {
		return ""
	}
	if p := strings.TrimSpace(pending.MP3Path); p != "" {
		return p
	}
	return extractRecordingFieldFromReport(pending.Report, "mp3_path")
}

// appendAudioArchiveMP3Instructions tells the model how to deliver the MP3 archive.
// knownMP3Path is only set when finalize already produced an on-disk MP3 product.
func appendAudioArchiveMP3Instructions(b *strings.Builder, audioPath, knownMP3Path string) {
	audioPath = strings.TrimSpace(audioPath)
	knownMP3Path = strings.TrimSpace(knownMP3Path)
	fallback := ""
	if audioPath != "" {
		fallback = suggestMP3ArchivePath(audioPath)
	}
	deliverPath := knownMP3Path
	if deliverPath == "" {
		deliverPath = fallback
	}

	b.WriteString("- Audio archive (required): deliver MP3 for long-term storage via send_file.\n")
	if knownMP3Path != "" {
		// Engine already converted on save — keep instructions short so the model
		// does not waste a turn re-running ffmpeg.
		b.WriteString(fmt.Sprintf("  Pre-built MP3 already exists at %q — do NOT re-encode; send_file(path=%q) only.\n", knownMP3Path, knownMP3Path))
		b.WriteString("  Only if that file is missing on disk: convert with bash+ffmpeg then send_file the new .mp3.\n")
		return
	}
	b.WriteString("  No pre-built MP3 — convert then send_file:\n")
	if audioPath != "" && deliverPath != "" {
		b.WriteString(fmt.Sprintf("    ffmpeg -y -i %q -codec:a libmp3lame -qscale:a 2 %q\n", audioPath, deliverPath))
		b.WriteString(fmt.Sprintf("  Then send_file(path=%q).\n", deliverPath))
	} else {
		b.WriteString("    ffmpeg -y -i \"<Audio path>\" -codec:a libmp3lame -qscale:a 2 \"<same stem>.mp3\"\n")
		b.WriteString("  Then send_file the resulting .mp3.\n")
	}
	b.WriteString("  If the source is already .mp3, skip conversion and send_file it directly.\n")
	b.WriteString("  If ffmpeg is unavailable, try another available converter; do not skip the MP3 archive when conversion is possible.\n")
	b.WriteString("  Optionally keep the original WAV; the user-facing archive link should be the MP3.\n")
}

func buildPostRecordingChoiceContext(pending *pendingPostRecordingState, action recordPostChoiceAction) string {
	if pending == nil {
		return ""
	}
	lang := pending.Lang
	if strings.TrimSpace(lang) == "" {
		lang = "zh"
	}
	label := recordPostChoiceLabel(action, lang)
	path := strings.TrimSpace(pending.Path)
	knownMP3 := resolveKnownMP3ArchivePath(pending)
	var b strings.Builder
	b.WriteString("[Context hint] The user selected a post-recording action via the engine-injected choice buttons (not a new request).\n")
	b.WriteString(fmt.Sprintf("Recording title: %s\n", pending.Title))
	if pending.Purpose != "" {
		b.WriteString(fmt.Sprintf("Recording purpose: %s\n", pending.Purpose))
	}
	if path != "" {
		b.WriteString(fmt.Sprintf("Audio path (WAV source / ASR input): %s\n", path))
	}
	if knownMP3 != "" {
		b.WriteString(fmt.Sprintf("MP3 archive path (pre-built on save): %s\n", knownMP3))
	} else if path != "" {
		if errMsg := extractRecordingFieldFromReport(pending.Report, "mp3_error"); errMsg != "" {
			b.WriteString(fmt.Sprintf("MP3 auto-archive failed on save (%s); must convert via ffmpeg before send_file.\n", errMsg))
		}
		if sug := suggestMP3ArchivePath(path); sug != "" && !strings.EqualFold(sug, path) {
			b.WriteString(fmt.Sprintf("Suggested MP3 archive path (convert if missing): %s\n", sug))
		}
	}
	if pending.DurationSec != "" {
		b.WriteString(fmt.Sprintf("duration_sec: %s\n", pending.DurationSec))
	}
	if pending.SizeBytes != "" {
		b.WriteString(fmt.Sprintf("size_bytes: %s\n", pending.SizeBytes))
	}
	if pending.Format != "" {
		b.WriteString(fmt.Sprintf("format: %s\n", pending.Format))
	}
	b.WriteString(fmt.Sprintf("UI language: %s\n", lang))
	b.WriteString(fmt.Sprintf("Chosen action: %s (%s)\n", action, label))
	b.WriteString("\nExecute NOW according to the chosen action. Do not re-ask with numbered text options.\n")
	b.WriteString("Write user-facing replies and document body language matching UI language above.\n")

	speakersArg := ""
	if pending.KnownSpeakers > 0 {
		speakersArg = fmt.Sprintf(", known_speakers=%d", pending.KnownSpeakers)
		b.WriteString(fmt.Sprintf("User-confirmed speaker count: %d — MUST pass known_speakers=%d to asr for accurate diarization.\n", pending.KnownSpeakers, pending.KnownSpeakers))
	}
	asrCall := "asr(path=<Audio path>" + speakersArg + ")"
	asrMinutesCall := "asr(path=<Audio path>, for_minutes=true" + speakersArg + ")"
	if path != "" {
		asrCall = fmt.Sprintf("asr(path=%q%s)", path, speakersArg)
		asrMinutesCall = fmt.Sprintf("asr(path=%q, for_minutes=true%s)", path, speakersArg)
	}

	mdPath := ""
	if path != "" {
		mdPath = suggestTranscriptMarkdownPath(path)
	}

	switch action {
	case recordPostActionMinutes:
		appendRecordingDesktopDeliveryException(&b)
		b.WriteString(fmt.Sprintf("- Call %s to transcribe the FULL original audio (do not skip ASR).\n", asrMinutesCall))
		b.WriteString("  for_minutes=true enables host LLM map-reduce into minutes_draft_file for long transcripts (slower but higher quality).\n")
		b.WriteString("- Produce meeting minutes in BOTH formats (both required) with the SAME body:\n")
		b.WriteString("  1) Markdown: write_file a structured .md minutes document.\n")
		b.WriteString("  2) PDF: call generate_pdf (or office action=generate_pdf) with the SAME markdown content and a clear title.\n")
		b.WriteString("- Minutes MUST include a dedicated full-transcript section with the complete ASR text (not a short paraphrase only).\n")
		b.WriteString("  Suggested structure: title/meta → summary → decisions/action items → **完整转写 / Full transcript** → attachments (mp3 path).\n")
		b.WriteString("  The transcript section is mandatory in BOTH the .md and the PDF.\n")
		b.WriteString("- LONG TRANSCRIPT (if asr returns [ASR long transcript] with transcript_file, or text is very long):\n")
		b.WriteString("  • Full text is only on disk at transcript_file — never paste the entire file into chat (context overflow).\n")
		b.WriteString("  • Prefer engine_minutes_draft / minutes_draft_file when present (host map-reduced). Lightly edit; do not invent facts.\n")
		b.WriteString("  • If no engine draft: map-reduce yourself — read_file/bash ~2k–4k token chunks, merge points. Do not invent middle content from preview only.\n")
		b.WriteString("  • Full-transcript section: assemble FROM transcript_file without rewriting (shell/copy/type/append or chunked read_file+write_file). Do not retype the whole transcript in the model.\n")
		b.WriteString("  • Generate PDF only after the .md on disk is complete. If generate_pdf rejects oversized content, convert the .md via bash (pandoc/wkhtmltopdf/weasyprint) or deliver the .md and report PDF failure.\n")
		appendAudioArchiveMP3Instructions(&b, path, knownMP3)
		b.WriteString("- MUST deliver clickable files to the AI panel:\n")
		b.WriteString("  • MP3 archive (send_file)\n")
		b.WriteString("  • Markdown minutes (send_file the .md path)\n")
		b.WriteString("  • PDF minutes (generate_pdf may auto-deliver; if not, ensure the PDF is delivered)\n")
		b.WriteString("- Final text must summarize duration/size and list all delivered paths (mp3, md, pdf; plus transcript_file if used).\n")
	case recordPostActionTranscribe:
		appendRecordingDesktopDeliveryException(&b)
		b.WriteString(fmt.Sprintf("- Call %s only (do not pass for_minutes).\n", asrCall))
		b.WriteString("- ALWAYS deliver the full transcript as BOTH formats (required, even for short audio):\n")
		if mdPath != "" {
			b.WriteString(fmt.Sprintf("  1) Markdown: prefer host-written transcript_md if asr returns it; else write_file to %q (title/meta + full ASR text).\n", mdPath))
			b.WriteString(fmt.Sprintf("  2) PDF: prefer host-written transcript_pdf if asr returns it; else generate_pdf from the markdown (suggested path %q).\n", asrTranscriptPDFPath(path)))
		} else {
			b.WriteString("  1) Markdown: write_file a .md next to the audio (prefer same stem *_transcript.md) with title/meta + full ASR text.\n")
			b.WriteString("  2) PDF: prefer host-written transcript_pdf if present; else generate_pdf from the markdown.\n")
		}
		b.WriteString("  Suggested structure: title/meta → **转写正文 / Transcript** (complete ASR text). No summary/decisions/action-items section.\n")
		b.WriteString("- Short ASR result: host often already wrote transcript_md and transcript_pdf — send_file them (do not only paste text). You may also show the full transcript in chat.\n")
		b.WriteString("- Long result ([ASR long transcript] / transcript_file / transcript_md): use on-disk md/txt without rewriting ASR text; generate PDF only if transcript_pdf is missing and only after .md is complete; short chat preview only.\n")
		b.WriteString("- If PDF is still missing after generate_pdf/host attempts: keep the .md, try bash conversion from the .md path, or deliver md+mp3 and clearly report PDF failure.\n")
		b.WriteString("- Do NOT write a full meeting-minutes document (no summary/decisions/TODOs) unless the user later asks.\n")
		appendAudioArchiveMP3Instructions(&b, path, knownMP3)
		b.WriteString("- MUST deliver clickable files to the AI panel:\n")
		b.WriteString("  • Markdown transcript (send_file transcript_md / .md path)\n")
		b.WriteString("  • PDF transcript (send_file transcript_pdf if present; else generate_pdf may auto-deliver)\n")
		b.WriteString("  • MP3 archive (send_file)\n")
		b.WriteString("- Final text must summarize duration/size and list delivered paths (md, pdf, mp3; plus transcript_file if used).\n")
	case recordPostActionKeepOnly:
		b.WriteString("- Do NOT call asr.\n")
		appendAudioArchiveMP3Instructions(&b, path, knownMP3)
		b.WriteString("- MUST send_file the MP3 archive and give a short duration/size/path summary.\n")
	}
	return b.String()
}

// hasActivePendingPostRecording reports whether the user still needs to pick a
// post-recording action (engine-injected buttons).
func (h *IMMessageHandler) hasActivePendingPostRecording(userID string, entries []agent.ConversationEntry) bool {
	if h == nil {
		return false
	}
	raw, ok := h.pendingPostRecording.Load(userID)
	if !ok {
		return false
	}
	_, fresh := pendingPostRecordingForCurrentHistory(raw, entries)
	return fresh
}

// consumePendingPostRecordingChoice resolves a button click or free-text reply
// while a post-recording choice is pending.
//
// Returns:
//   - ctx: agent context hint when the action still needs the LLM (or soft chat)
//   - hostResp: non-nil for fast host paths that already finished (e.g. keep_only)
//   - deferredHost: non-nil for host work that must run AFTER the session lock is released (ASR)
//   - ok: true when a post-recording pending existed (matched, host-handled, deferred, or soft)
//
// ok=true means callers should treat this as task continuation so the session is not archived.
func (h *IMMessageHandler) consumePendingPostRecordingChoice(userID, trimmed string, entries []agent.ConversationEntry) (ctx string, hostResp *IMAgentResponse, deferredHost func() *IMAgentResponse, ok bool) {
	if h == nil {
		return "", nil, nil, false
	}
	raw, ok := h.pendingPostRecording.Load(userID)
	if !ok {
		return "", nil, nil, false
	}
	pending, fresh := pendingPostRecordingForCurrentHistory(raw, entries)
	if !fresh {
		h.clearPendingPostRecording(userID)
		return "", nil, nil, false
	}

	// Step 2: speaker-count confirmation after minutes/transcribe was chosen.
	if pending.AwaitingSpeakerConfirm {
		return h.consumePendingSpeakerConfirm(userID, trimmed, pending, entries)
	}

	action, matched := parseRecordPostChoiceCommand(trimmed)
	if !matched {
		action, matched = matchRecordPostChoiceFreeText(trimmed)
	}
	if !matched {
		// Keep pending within the original offer TTL (CreatedAt). Soft chat does
		// not extend the window — that would pin choices indefinitely.
		if recordDetailEnabled() {
			log.Printf("[record-audio] post-choice pending, non-matching reply user=%s answer_len=%d age=%s",
				userID, len([]rune(trimmed)), time.Since(pending.CreatedAt).Round(time.Second))
		}
		soft := fmt.Sprintf(
			"[Context hint] A post-recording choice is still pending for audio path %q (title %q). "+
				"If the user's message is unrelated, answer briefly and remind them the three action buttons are still available. "+
				"If they are choosing processing, map it to minutes/transcribe/keep_only and execute. "+
				"Do not re-list numbered options as plain text — the UI already has buttons. "+
				"When executing: minutes need BOTH markdown and PDF and MUST embed the full ASR transcript; "+
				"transcribe-only also needs BOTH markdown and PDF of the full transcript (not meeting-minutes structure) plus MP3; "+
				"always deliver MP3 archive (prefer pre-built mp3_path if present). "+
				"If the user wants to start a new recording instead, call record_audio (that clears this pending choice).",
			pending.Path, pending.Title,
		)
		return soft, nil, nil, true
	}

	// When diarization is on, minutes/transcribe go through speaker confirm first.
	if (action == recordPostActionMinutes || action == recordPostActionTranscribe) && h.shouldOfferSpeakerConfirm() {
		if resp := h.offerPostRecordingSpeakerConfirm(userID, pending, action, entries); resp != nil {
			return "", resp, nil, true
		}
	}

	// Host-side deterministic paths (no LLM) when safe/cheap enough.
	// Transcribe is deferred (ASR can be slow); keep_only is immediate (stat + paths).
	switch action {
	case recordPostActionTranscribe:
		if deferred := h.deferHostPostRecordingTranscribe(userID, pending, entries); deferred != nil {
			h.clearPendingPostRecording(userID)
			if recordDetailEnabled() {
				log.Printf("[record-audio] post-choice deferred host transcribe user=%s path=%q", userID, pending.Path)
			}
			return "", nil, deferred, true
		}
	case recordPostActionKeepOnly:
		if resp := h.hostHandlePostRecordingKeepOnly(userID, pending, entries); resp != nil {
			h.clearPendingPostRecording(userID)
			if recordDetailEnabled() {
				log.Printf("[record-audio] post-choice host-handled keep_only user=%s path=%q files=%d",
					userID, pending.Path, len(resp.LocalFilePaths))
			}
			return "", resp, nil, true
		}
	}

	h.clearPendingPostRecording(userID)
	ctx = buildPostRecordingChoiceContext(pending, action)
	if recordDetailEnabled() {
		log.Printf("[record-audio] post-choice selected user=%s action=%s path=%q", userID, action, pending.Path)
	}
	return ctx, nil, nil, true
}

// consumePendingSpeakerConfirm handles step-2 replies (button or free text).
func (h *IMMessageHandler) consumePendingSpeakerConfirm(
	userID, trimmed string,
	pending *pendingPostRecordingState,
	entries []agent.ConversationEntry,
) (ctx string, hostResp *IMAgentResponse, deferredHost func() *IMAgentResponse, ok bool) {
	speakers, matched := matchRecordPostSpeakerFreeText(trimmed)
	if !matched {
		if recordDetailEnabled() {
			log.Printf("[record-audio] speaker-confirm pending, non-matching reply user=%s answer_len=%d",
				userID, len([]rune(trimmed)))
		}
		soft := fmt.Sprintf(
			"[Context hint] Speaker-count confirmation is still pending for audio path %q (action %s, suggested %d). "+
				"If unrelated, answer briefly and remind the user to confirm speaker count via the buttons (1–5 or Auto) or a short reply like \"2人\" / \"auto\". "+
				"Do not start transcription until they confirm. "+
				"If they want a new recording, call record_audio (clears this pending).",
			pending.Path, pending.PendingAction, pending.SuggestedSpeakers,
		)
		return soft, nil, nil, true
	}

	action := pending.PendingAction
	if action != recordPostActionMinutes && action != recordPostActionTranscribe {
		// Defensive: bad state — clear and soft-fail.
		h.clearPendingPostRecording(userID)
		return "", nil, nil, true
	}
	pending.KnownSpeakers = speakers
	pending.AwaitingSpeakerConfirm = false
	if recordDetailEnabled() {
		log.Printf("[record-audio] speaker confirmed user=%s action=%s known_speakers=%d path=%q",
			userID, action, speakers, pending.Path)
	}

	switch action {
	case recordPostActionTranscribe:
		if deferred := h.deferHostPostRecordingTranscribe(userID, pending, entries); deferred != nil {
			h.clearPendingPostRecording(userID)
			return "", nil, deferred, true
		}
	case recordPostActionMinutes:
		// Prefer host ASR (with confirmed speaker pin) + draft minutes so the
		// LLM cannot drop known_speakers on a re-asr. Falls back to agent ctx.
		if deferred := h.deferHostPostRecordingMinutes(userID, pending, entries); deferred != nil {
			h.clearPendingPostRecording(userID)
			return "", nil, deferred, true
		}
	}

	h.clearPendingPostRecording(userID)
	ctx = buildPostRecordingChoiceContext(pending, action)
	return ctx, nil, nil, true
}

// deferHostPostRecordingTranscribe returns a closure that runs host ASR + archive
// delivery after the session lock is released. Returns nil when host path is not eligible.
func (h *IMMessageHandler) deferHostPostRecordingTranscribe(
	userID string,
	pending *pendingPostRecordingState,
	priorHistory []agent.ConversationEntry,
) func() *IMAgentResponse {
	if h == nil || h.app == nil || pending == nil {
		return nil
	}
	if !shouldHostHandlePostRecordingTranscribe(pending) {
		return nil
	}
	if !h.app.GetASREnabled() || !h.app.IsASRReady() {
		return nil
	}
	// Snapshot state so the deferred call is independent of the sync.Map entry.
	snap := *pending
	historySnap := cloneConversationEntries(priorHistory)
	return func() *IMAgentResponse {
		resp := h.hostHandlePostRecordingTranscribe(userID, &snap, historySnap)
		if resp != nil {
			if recordDetailEnabled() {
				log.Printf("[record-audio] post-choice host-handled transcribe user=%s path=%q files=%d",
					userID, snap.Path, len(resp.LocalFilePaths))
			}
			return resp
		}
		// Pending already cleared; surface a clear failure instead of a silent empty reply.
		return hostPostRecordingTranscribeFailureResponse(userID, &snap)
	}
}

// deferHostPostRecordingMinutes runs host ASR (with confirmed speaker pin) then
// a minutes draft after the session lock is released. Returns nil when the
// recording is too long / ASR unavailable so the agent path can take over.
func (h *IMMessageHandler) deferHostPostRecordingMinutes(
	userID string,
	pending *pendingPostRecordingState,
	priorHistory []agent.ConversationEntry,
) func() *IMAgentResponse {
	if h == nil || h.app == nil || pending == nil {
		return nil
	}
	if !shouldHostHandlePostRecordingTranscribe(pending) {
		return nil
	}
	if !h.app.GetASREnabled() || !h.app.IsASRReady() {
		return nil
	}
	snap := *pending
	historySnap := cloneConversationEntries(priorHistory)
	return func() *IMAgentResponse {
		resp := h.hostHandlePostRecordingMinutes(userID, &snap, historySnap)
		if resp != nil {
			if recordDetailEnabled() {
				log.Printf("[record-audio] post-choice host-handled minutes user=%s path=%q known_speakers=%d files=%d",
					userID, snap.Path, snap.KnownSpeakers, len(resp.LocalFilePaths))
			}
			return resp
		}
		// Minutes assembly failed after eligibility checks: still deliver a pin-aware
		// transcript if ASR works, so the user is not left empty-handed.
		if tx := h.hostHandlePostRecordingTranscribe(userID, &snap, historySnap); tx != nil {
			if recordDetailEnabled() {
				log.Printf("[record-audio] minutes host failed; fell back to host transcribe user=%s path=%q",
					userID, snap.Path)
			}
			// Prefix a short note so the user knows minutes was incomplete.
			note := ""
			switch resolveRecordPostUILang(snap.Lang) {
			case "en":
				note = "Meeting minutes draft failed; delivering full transcript instead.\n\n"
			case "zh-Hant":
				note = "會議紀要草稿失敗，改為交付完整轉寫。\n\n"
			default:
				note = "会议纪要草稿失败，改为交付完整转写。\n\n"
			}
			tx.Text = note + tx.Text
			return tx
		}
		return hostPostRecordingMinutesFailureResponse(userID, &snap)
	}
}

func hostPostRecordingMinutesFailureResponse(userID string, pending *pendingPostRecordingState) *IMAgentResponse {
	lang := "zh"
	if pending != nil && strings.TrimSpace(pending.Lang) != "" {
		lang = pending.Lang
	}
	ui := resolveRecordPostUILang(lang)
	var text string
	switch ui {
	case "en":
		text = "Meeting minutes failed (ASR unavailable, empty result, or audio could not be decoded). Please retry, check ASR settings, or re-record."
	case "zh-Hant":
		text = "會議紀要失敗（語音識別不可用、結果為空或音訊無法解碼）。請重試、檢查 ASR 設定，或重新錄音。"
	default:
		text = "会议纪要失败（语音识别不可用、结果为空或音频无法解码）。请重试、检查 ASR 设置，或重新录音。"
	}
	if pending != nil && strings.TrimSpace(pending.Path) != "" {
		text += "\n\n`" + pending.Path + "`"
	}
	return &IMAgentResponse{
		Text:           text,
		SessionKey:     userID,
		ResponseSource: imResponseSourceAskUser.String(),
	}
}

// hostRunPostRecordingASR prepares WAV and transcribes with the pending speaker pin.
// Returns empty text when host ASR cannot run (caller decides fallback).
func (h *IMMessageHandler) hostRunPostRecordingASR(pending *pendingPostRecordingState) (path, text string, ok bool) {
	if h == nil || h.app == nil || pending == nil {
		return "", "", false
	}
	if !shouldHostHandlePostRecordingTranscribe(pending) {
		return "", "", false
	}
	path = strings.TrimSpace(pending.Path)
	if path == "" {
		return "", "", false
	}
	if !h.app.GetASREnabled() || !h.app.IsASRReady() {
		return "", "", false
	}
	wav, errMsg := prepareASRToolWAV(path, strings.TrimSpace(pending.Format))
	if errMsg != "" {
		log.Printf("[record-audio] host ASR prepare failed path=%q: %s", path, errMsg)
		return path, "", false
	}
	out, err := h.app.transcribeWAVBytesWithSpeakers(wav, pending.KnownSpeakers)
	if err != nil {
		log.Printf("[record-audio] host ASR failed path=%q known_speakers=%d: %v", path, pending.KnownSpeakers, err)
		return path, "", false
	}
	out = strings.TrimSpace(out)
	if out == "" {
		log.Printf("[record-audio] host ASR empty path=%q", path)
		return path, "", false
	}
	return path, out, true
}

func hostPostRecordingTranscribeFailureResponse(userID string, pending *pendingPostRecordingState) *IMAgentResponse {
	lang := "zh"
	if pending != nil && strings.TrimSpace(pending.Lang) != "" {
		lang = pending.Lang
	}
	ui := resolveRecordPostUILang(lang)
	var text string
	switch ui {
	case "en":
		text = "Transcription failed (ASR unavailable, empty result, or audio could not be decoded). Please retry, check ASR settings, or re-record."
	case "zh-Hant":
		text = "轉寫失敗（語音識別不可用、結果為空或音訊無法解碼）。請重試、檢查 ASR 設定，或重新錄音。"
	default:
		text = "转写失败（语音识别不可用、结果为空或音频无法解码）。请重试、检查 ASR 设置，或重新录音。"
	}
	if pending != nil && strings.TrimSpace(pending.Path) != "" {
		text += "\n\n`" + pending.Path + "`"
	}
	return &IMAgentResponse{
		Text:           text,
		SessionKey:     userID,
		ResponseSource: imResponseSourceAskUser.String(),
	}
}

// hostPostRecordingMaxDurationSec caps host-side ASR (runs AFTER session lock release).
// Longer recordings fall back to the agent loop so the user still sees tool progress.
// Not a lock-safety limit (deferred ASR unlocks first); this is a UX bound for
// synchronous host work without progress events.
const hostPostRecordingMaxDurationSec = 300 // 5 minutes of audio

// hostPostRecordingMaxBytes is used when duration_sec is unknown.
// ~16 kHz mono PCM16 ≈ 32 KiB/s → 300s ≈ 9.6 MiB; allow headroom for WAV containers.
const hostPostRecordingMaxBytes = 12 << 20 // 12 MiB

// shouldHostHandlePostRecordingTranscribe reports whether the host may run ASR
// for this recording (duration/size UX gate; lock is released before ASR).
func shouldHostHandlePostRecordingTranscribe(pending *pendingPostRecordingState) bool {
	if pending == nil {
		return false
	}
	path := strings.TrimSpace(pending.Path)
	if path == "" {
		return false
	}
	if secStr := strings.TrimSpace(pending.DurationSec); secStr != "" {
		sec, err := strconv.ParseFloat(secStr, 64)
		if err == nil {
			return sec >= 0 && sec <= float64(hostPostRecordingMaxDurationSec)
		}
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return info.Size() > 0 && info.Size() <= hostPostRecordingMaxBytes
	}
	// Unknown size/duration: do NOT host-handle — prefer agent path with progress.
	return false
}

// hostHandlePostRecordingTranscribe runs ASR and writes transcript archives without the LLM.
// Returns nil to fall back to agent-driven context when host handling is not possible.
func (h *IMMessageHandler) hostHandlePostRecordingTranscribe(
	userID string,
	pending *pendingPostRecordingState,
	priorHistory []agent.ConversationEntry,
) *IMAgentResponse {
	path, text, ok := h.hostRunPostRecordingASR(pending)
	if !ok {
		if recordDetailEnabled() && pending != nil {
			log.Printf("[record-audio] host transcribe skipped path=%q duration=%q",
				pending.Path, pending.DurationSec)
		}
		return nil
	}

	// Persist archives (md always; pdf for short transcripts; txt when spilling).
	spill := asrShouldSpillToFile(text)
	mdPath, pdfPath := writeASRTranscriptArchives(path, text, !spill)
	txtPath := ""
	if spill {
		txtPath = asrTranscriptSidecarPath(path)
		if werr := os.WriteFile(txtPath, []byte(text), 0o644); werr != nil {
			log.Printf("[record-audio] host transcribe write txt failed path=%q: %v", txtPath, werr)
			txtPath = ""
		}
	}

	paths := collectExistingPaths(mdPath, pdfPath, txtPath, resolvePostRecordingMP3Path(pending))
	resp := buildPostRecordingTranscribeHostResponse(pending, text, paths, spill)
	if resp == nil {
		return nil
	}
	resp.SessionKey = userID
	h.appendPostRecordingHostHistory(userID, pending, recordPostActionTranscribe, priorHistory, resp)
	return resp
}

// hostHandlePostRecordingMinutes runs pin-aware ASR then a minutes draft (LLM when
// configured, else extractive). Guarantees speaker labels use the user-confirmed
// count without relying on the agent to re-call asr(known_speakers=…).
func (h *IMMessageHandler) hostHandlePostRecordingMinutes(
	userID string,
	pending *pendingPostRecordingState,
	priorHistory []agent.ConversationEntry,
) *IMAgentResponse {
	path, text, ok := h.hostRunPostRecordingASR(pending)
	if !ok {
		return nil
	}

	spill := asrShouldSpillToFile(text)
	txMD, txPDF := writeASRTranscriptArchives(path, text, !spill)
	txTXT := ""
	if spill {
		txTXT = asrTranscriptSidecarPath(path)
		if werr := os.WriteFile(txTXT, []byte(text), 0o644); werr != nil {
			log.Printf("[record-audio] host minutes write txt failed path=%q: %v", txTXT, werr)
			txTXT = ""
		}
	}

	title := ""
	purpose := ""
	if pending != nil {
		title = strings.TrimSpace(pending.Title)
		purpose = strings.TrimSpace(pending.Purpose)
	}
	if title == "" {
		title = "会议纪要"
	}
	draft, usedLLM := buildMeetingMinutesDraft(context.Background(), h.app, title, purpose, text, true)

	// Assemble minutes markdown: draft body + mandatory full-transcript section.
	var body strings.Builder
	body.WriteString(strings.TrimSpace(draft))
	body.WriteString("\n\n## 完整转写 / Full transcript\n\n")
	body.WriteString(text)
	body.WriteByte('\n')
	// Dedicated minutes product next to the audio (not the intermediate draft sidecar).
	minutesMD := ""
	if ext := filepath.Ext(path); ext != "" {
		minutesMD = strings.TrimSuffix(path, ext) + "_minutes.md"
	} else if path != "" {
		minutesMD = path + "_minutes.md"
	}
	if werr := os.WriteFile(minutesMD, []byte(body.String()), 0o644); werr != nil {
		log.Printf("[record-audio] host minutes write md failed path=%q: %v", minutesMD, werr)
		minutesMD = ""
	}

	paths := collectExistingPaths(minutesMD, txMD, txPDF, txTXT, resolvePostRecordingMP3Path(pending))
	resp := buildPostRecordingMinutesHostResponse(pending, text, draft, paths, usedLLM, spill)
	if resp == nil {
		return nil
	}
	resp.SessionKey = userID
	h.appendPostRecordingHostHistory(userID, pending, recordPostActionMinutes, priorHistory, resp)
	return resp
}

// hostHandlePostRecordingKeepOnly delivers the audio archive without ASR when already on disk.
// Prefers MP3; falls back to source WAV. Returns nil only when nothing is on disk.
func (h *IMMessageHandler) hostHandlePostRecordingKeepOnly(
	userID string,
	pending *pendingPostRecordingState,
	priorHistory []agent.ConversationEntry,
) *IMAgentResponse {
	if h == nil || pending == nil {
		return nil
	}
	audio := resolvePostRecordingAudioArchivePath(pending)
	if audio == "" {
		return nil
	}
	paths := collectExistingPaths(audio)
	if len(paths) == 0 {
		return nil
	}
	resp := buildPostRecordingKeepOnlyHostResponse(pending, paths)
	if resp == nil {
		return nil
	}
	resp.SessionKey = userID
	h.appendPostRecordingHostHistory(userID, pending, recordPostActionKeepOnly, priorHistory, resp)
	return resp
}

func (h *IMMessageHandler) appendPostRecordingHostHistory(
	userID string,
	pending *pendingPostRecordingState,
	action recordPostChoiceAction,
	priorHistory []agent.ConversationEntry,
	resp *IMAgentResponse,
) {
	if h == nil || resp == nil {
		return
	}
	lang := ""
	if pending != nil {
		lang = pending.Lang
	}
	userLabel := recordPostChoiceLabel(action, lang)
	if strings.TrimSpace(userLabel) == "" {
		userLabel = string(action)
	}
	// Prefer live memory: deferred host work runs after the session lock is
	// released, so concurrent turns may have landed after priorHistory was snapshotted.
	var history []agent.ConversationEntry
	if h.memory != nil {
		history = cloneConversationEntries(h.memory.Load(userID))
	}
	if len(history) == 0 {
		history = cloneConversationEntries(priorHistory)
	}
	// Avoid duplicating the user label when the UI already stored the same turn.
	if !lastUserContentEquals(history, userLabel) {
		history = append(history, agent.ConversationEntry{
			Role:    "user",
			Content: userLabel,
		})
	}
	history = append(history, agent.ConversationEntry{
		Role:    "assistant",
		Content: resp.Text,
	})
	h.saveConversationHistoryTimed(userID, history, resp)
}

func lastUserContentEquals(history []agent.ConversationEntry, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" || len(history) == 0 {
		return false
	}
	last := history[len(history)-1]
	if last.Role != "user" {
		return false
	}
	switch c := last.Content.(type) {
	case string:
		return strings.TrimSpace(c) == want
	default:
		return false
	}
}

// resolvePostRecordingMP3Path returns a known or on-disk sibling MP3 path when available.
func resolvePostRecordingMP3Path(pending *pendingPostRecordingState) string {
	if pending == nil {
		return ""
	}
	if p := resolveKnownMP3ArchivePath(pending); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if sug := suggestMP3ArchivePath(pending.Path); sug != "" && !strings.EqualFold(sug, pending.Path) {
		if _, err := os.Stat(sug); err == nil {
			return sug
		}
	}
	return ""
}

// resolvePostRecordingAudioArchivePath prefers MP3; falls back to the source WAV/path
// so keep_only can still host-deliver when auto-MP3 failed.
func resolvePostRecordingAudioArchivePath(pending *pendingPostRecordingState) string {
	if p := resolvePostRecordingMP3Path(pending); p != "" {
		return p
	}
	if pending == nil {
		return ""
	}
	src := strings.TrimSpace(pending.Path)
	if src == "" {
		return ""
	}
	if _, err := os.Stat(src); err == nil {
		return src
	}
	return ""
}

func collectExistingPaths(paths ...string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err != nil {
			continue
		}
		key := strings.ToLower(filepath.Clean(p))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	return out
}

// buildPostRecordingMinutesHostResponse builds the host-side minutes response
// (ASR with speaker pin + draft). paths are delivered via LocalFilePaths.
func buildPostRecordingMinutesHostResponse(
	pending *pendingPostRecordingState,
	transcript, draft string,
	paths []string,
	usedLLM, longTranscript bool,
) *IMAgentResponse {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return nil
	}
	lang := "zh"
	if pending != nil && strings.TrimSpace(pending.Lang) != "" {
		lang = pending.Lang
	}
	ui := resolveRecordPostUILang(lang)
	title := ""
	if pending != nil {
		title = strings.TrimSpace(pending.Title)
	}
	var b strings.Builder
	dur := ""
	if pending != nil {
		dur = formatDurationFromSecField(pending.DurationSec)
	}
	switch ui {
	case "en":
		b.WriteString("Meeting minutes ready")
		if title != "" {
			b.WriteString(fmt.Sprintf(" (%s)", title))
		}
		b.WriteString(".\n")
		if dur != "" {
			b.WriteString(fmt.Sprintf("Duration: %s\n", dur))
		}
		if pending != nil && pending.KnownSpeakers > 0 {
			b.WriteString(fmt.Sprintf("Speakers: %d (confirmed)\n", pending.KnownSpeakers))
		}
		if usedLLM {
			b.WriteString("Draft: LLM map-reduce\n")
		} else {
			b.WriteString("Draft: extractive (review recommended)\n")
		}
		b.WriteString("\n**Minutes preview:**\n")
	case "zh-Hant":
		b.WriteString("會議紀要已生成")
		if title != "" {
			b.WriteString(fmt.Sprintf("（%s）", title))
		}
		b.WriteString("！\n")
		if dur != "" {
			b.WriteString(fmt.Sprintf("時長：%s\n", dur))
		}
		if pending != nil && pending.KnownSpeakers > 0 {
			b.WriteString(fmt.Sprintf("說話人：%d 人（已確認）\n", pending.KnownSpeakers))
		}
		if usedLLM {
			b.WriteString("草稿：LLM 彙總\n")
		} else {
			b.WriteString("草稿：抽取式（建議人工核對）\n")
		}
		b.WriteString("\n**紀要預覽：**\n")
	default:
		b.WriteString("会议纪要已生成")
		if title != "" {
			b.WriteString(fmt.Sprintf("（%s）", title))
		}
		b.WriteString("！\n")
		if dur != "" {
			b.WriteString(fmt.Sprintf("时长：%s\n", dur))
		}
		if pending != nil && pending.KnownSpeakers > 0 {
			b.WriteString(fmt.Sprintf("说话人：%d 人（已确认）\n", pending.KnownSpeakers))
		}
		if usedLLM {
			b.WriteString("草稿：LLM 汇总\n")
		} else {
			b.WriteString("草稿：抽取式（建议人工核对）\n")
		}
		b.WriteString("\n**纪要预览：**\n")
	}
	preview := strings.TrimSpace(draft)
	if preview == "" {
		preview = transcript
	}
	if longTranscript || utf8RuneCount(preview) > asrPreviewHeadRunes+asrPreviewTailRunes {
		head, tail, omitted := asrPreviewHeadTail(preview, asrPreviewHeadRunes, asrPreviewTailRunes)
		b.WriteString(head)
		if omitted > 0 {
			b.WriteString(fmt.Sprintf("\n\n… (%d chars omitted; see attached files) …\n\n", omitted))
			b.WriteString(tail)
		}
	} else {
		b.WriteString(preview)
	}
	b.WriteByte('\n')
	return &IMAgentResponse{
		Text:           b.String(),
		LocalFilePaths: paths,
		ResponseSource: imResponseSourceAskUser.String(),
	}
}

func utf8RuneCount(s string) int {
	return utf8.RuneCountInString(s)
}

// buildPostRecordingTranscribeHostResponse builds the deterministic UI response for
// host-side "transcribe only" (no LLM). paths are delivered via LocalFilePaths.
func buildPostRecordingTranscribeHostResponse(
	pending *pendingPostRecordingState,
	transcript string,
	paths []string,
	longTranscript bool,
) *IMAgentResponse {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return nil
	}
	lang := "zh"
	if pending != nil && strings.TrimSpace(pending.Lang) != "" {
		lang = pending.Lang
	}
	ui := resolveRecordPostUILang(lang)
	title := ""
	if pending != nil {
		title = strings.TrimSpace(pending.Title)
	}

	var b strings.Builder
	dur := ""
	if pending != nil {
		dur = formatDurationFromSecField(pending.DurationSec)
	}
	switch ui {
	case "en":
		b.WriteString("Transcription complete")
		if title != "" {
			b.WriteString(fmt.Sprintf(" (%s)", title))
		}
		b.WriteString(".\n")
		if dur != "" {
			b.WriteString(fmt.Sprintf("Duration: %s\n", dur))
		}
		b.WriteString("\n**Transcript:**\n")
	case "zh-Hant":
		b.WriteString("轉寫完成")
		if title != "" {
			b.WriteString(fmt.Sprintf("（%s）", title))
		}
		b.WriteString("！\n")
		if dur != "" {
			b.WriteString(fmt.Sprintf("時長：%s\n", dur))
		}
		b.WriteString("\n**轉寫內容：**\n")
	default:
		b.WriteString("转写完成")
		if title != "" {
			b.WriteString(fmt.Sprintf("（%s）", title))
		}
		b.WriteString("！\n")
		if dur != "" {
			b.WriteString(fmt.Sprintf("时长：%s\n", dur))
		}
		b.WriteString("\n**转写内容：**\n")
	}

	if longTranscript {
		head, tail, omitted := asrPreviewHeadTail(transcript, asrPreviewHeadRunes, asrPreviewTailRunes)
		b.WriteString(head)
		if omitted > 0 {
			switch ui {
			case "en":
				b.WriteString(fmt.Sprintf("\n\n… (%d characters omitted; see attached files for the full transcript) …\n\n", omitted))
			case "zh-Hant":
				b.WriteString(fmt.Sprintf("\n\n…（中間省略 %d 字，完整內容見附件）…\n\n", omitted))
			default:
				b.WriteString(fmt.Sprintf("\n\n…（中间省略 %d 字，完整内容见附件）…\n\n", omitted))
			}
			b.WriteString(tail)
		}
	} else {
		b.WriteString(transcript)
	}

	if len(paths) > 0 {
		switch ui {
		case "en":
			b.WriteString("\n\n**Saved files:**\n")
		case "zh-Hant":
			b.WriteString("\n\n**已保存檔案：**\n")
		default:
			b.WriteString("\n\n**已保存文件：**\n")
		}
		for _, p := range paths {
			b.WriteString(fmt.Sprintf("- `%s`\n", p))
		}
	}

	return finishPostRecordingHostResponse(strings.TrimSpace(b.String()), paths)
}

// buildPostRecordingKeepOnlyHostResponse delivers pre-built audio archives only.
func buildPostRecordingKeepOnlyHostResponse(pending *pendingPostRecordingState, paths []string) *IMAgentResponse {
	if len(paths) == 0 {
		return nil
	}
	lang := "zh"
	if pending != nil && strings.TrimSpace(pending.Lang) != "" {
		lang = pending.Lang
	}
	ui := resolveRecordPostUILang(lang)
	title := ""
	dur := ""
	if pending != nil {
		title = strings.TrimSpace(pending.Title)
		dur = formatDurationFromSecField(pending.DurationSec)
	}

	var b strings.Builder
	switch ui {
	case "en":
		b.WriteString("Recording kept")
		if title != "" {
			b.WriteString(fmt.Sprintf(" (%s)", title))
		}
		b.WriteString(".\n")
		if dur != "" {
			b.WriteString(fmt.Sprintf("Duration: %s\n", dur))
		}
		b.WriteString("\n**Audio archive:**\n")
	case "zh-Hant":
		b.WriteString("錄音已保留")
		if title != "" {
			b.WriteString(fmt.Sprintf("（%s）", title))
		}
		b.WriteString("。\n")
		if dur != "" {
			b.WriteString(fmt.Sprintf("時長：%s\n", dur))
		}
		b.WriteString("\n**音訊存檔：**\n")
	default:
		b.WriteString("录音已保留")
		if title != "" {
			b.WriteString(fmt.Sprintf("（%s）", title))
		}
		b.WriteString("。\n")
		if dur != "" {
			b.WriteString(fmt.Sprintf("时长：%s\n", dur))
		}
		b.WriteString("\n**音频存档：**\n")
	}
	for _, p := range paths {
		b.WriteString(fmt.Sprintf("- `%s`\n", p))
	}
	return finishPostRecordingHostResponse(strings.TrimSpace(b.String()), paths)
}

func finishPostRecordingHostResponse(text string, paths []string) *IMAgentResponse {
	resp := &IMAgentResponse{
		Text:           text,
		LocalFilePaths: append([]string(nil), paths...),
	}
	if len(paths) == 1 {
		resp.LocalFilePath = paths[0]
	}
	if len(paths) > 0 {
		resp.ResponseSource = imResponseSourceFileDelivery.String()
	} else {
		// Transcript-only text fallback (archive write failed).
		resp.ResponseSource = imResponseSourceAskUser.String()
	}
	return resp
}

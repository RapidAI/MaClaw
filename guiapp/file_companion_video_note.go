package guiapp

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/docgen"
)

const (
	fileCompanionPurposeVideoNote = "video-note"
	fileCompanionVideoNoteMarker  = "<!--vn-->"
	fileCompanionVideoNoteLabel   = "生成笔记"
	fileCompanionVideoNoteStored  = "已生成笔记"
	// fileCompanionNoteExcerptLabel marks host context on one question.
	// It is not part of the transcript. The note file is read again next time.
	fileCompanionNoteExcerptLabel = "已生成的笔记摘录（不是新的转写）："

	fileCompanionVideoNoteReserveTokens = 16_000
	fileCompanionVideoNoteImageTokens   = 1100
	fileCompanionVideoNoteFollowTokens  = 4_000
	fileCompanionVideoNoteMinWindow     = 24_000

	fileCompanionNoteMADThreshold = 12
	fileCompanionNoteMaxFrames    = 24
	fileCompanionNoteSegment      = 60 * time.Second
	fileCompanionNoteMaxDuration  = 10800 * time.Second
	fileCompanionNoteMaxBytes     = 8 << 30
)

const (
	fileCompanionErrNotPaper         = "这个文件不是论文，不能做论文解读"
	fileCompanionErrNoteMediaOnly    = "只有视频或音频可以生成笔记"
	fileCompanionErrVideoTooLong     = "视频超过 3 小时，暂不生成笔记"
	fileCompanionErrAudioTooLong     = "音频超过 3 小时，暂不生成笔记"
	fileCompanionErrVideoTooBig      = "视频超过 8 GB，暂不生成笔记"
	fileCompanionErrAudioTooBig      = "音频超过 8 GB，暂不生成笔记"
	fileCompanionErrASRNotReady      = "语音识别模型未就绪。请在主窗口设置里启用 ASR，并等待 SenseVoice 下载完成后再试。"
	fileCompanionErrNoSpeech         = "没有识别到语音。如果这是音乐或环境声，无法生成讲稿笔记。"
	fileCompanionErrNoTrack          = "这个视频没有音轨，也没有字幕，无法生成笔记。"
	fileCompanionErrVideoCorrupt     = "这个视频无法读取，可能已损坏。"
	fileCompanionErrAudioCorrupt     = "这个音频无法读取，可能已损坏。"
	fileCompanionErrEmptyFile        = "文件是空的"
	fileCompanionErrWindowTooSmall   = "当前模型的上下文窗口太小，无法整理这段笔记"
	fileCompanionErrNoteCancelled    = "已取消生成笔记"
	fileCompanionErrCannotOverwrite  = "不能覆盖当前文件"
	fileCompanionErrOpenFileScheme   = "本地文件请用「打开文档或导入项目」，这里不接受 file:// 和其他协议"
	fileCompanionErrSelfPage         = "这个地址是伴读自己的页面，不能当作视频网页打开"
	fileCompanionErrWebPlayerHidden  = "播放区域还没有显示"
	fileCompanionErrBlackFrame       = "画面无法采集"
	fileCompanionErrNoAudio          = "没有录到声音"
	fileCompanionErrFrameUnread      = "画面未识别"
	fileCompanionErrOCRUncertain     = "识别不确定"
	fileCompanionErrNoteEmpty        = "没有得到笔记内容"
	fileCompanionErrDecoderNoSubs    = "视频解码器没有安装成功，也没有同名字幕，无法生成笔记"
	fileCompanionErrDecoderAudio     = "视频解码器没有安装成功，无法读取这个音频"
	fileCompanionErrNativeLimit      = "这个音频超过了内置解码器的长度，视频解码器也没有安装成功"
	fileCompanionErrOggNotOpus       = "这个 Ogg 不是 Opus，内置解码器不能读，视频解码器也没有安装成功"
	fileCompanionErrAudioNoTrack     = "这个音频没有音轨，无法生成笔记。"
	fileCompanionErrSubsOnly         = "语音识别模型未就绪，本笔记只根据字幕"
	fileCompanionErrContainerUnread  = "容器没有读成，时间戳来自字幕。"
	fileCompanionErrSegmentMiss      = "（这一段没有识别到语音）"
	fileCompanionErrSegmentUnread    = "这一段没有识别"
	fileCompanionErrSlideSpoken      = "口播很少，下列内容主要来自画面"
	fileCompanionErrTimeUnknown      = "时间不明"
	fileCompanionErrSubsTruncated    = "字幕超过 2 MB，已截断。"
	fileCompanionNoteProgressInstall = "正在安装视频解码器"
	fileCompanionNoteProgressCheck   = "正在检查文件"
	fileCompanionNoteProgressSubs    = "正在读取字幕"
	fileCompanionNoteProgressAudio   = "正在提取音轨"
	fileCompanionNoteProgressFrames  = "正在抽取画面"
	fileCompanionNoteProgressOCR     = "正在识别画面文字"
	fileCompanionNoteProgressWrite   = "正在整理笔记"
	fileCompanionNoteProgressWatch   = "正在记录画面"
	fileCompanionNoteProgressListen  = "正在记录声音"
)

var fileCompanionVideoNotePhraseRe = regexp.MustCompile(`^(请|帮我)?(给(这个|该)?(视频|音频))?(生成|做|整理)(一下|一份)?(视频|音频)?笔记$`)

var fileCompanionNativeToWAV = audioconv.ToWAV

// fileCompanionTurnModelHook replaces the desktop model for tests.
// Production leaves it nil.
var fileCompanionTurnModelHook func(msg IMUserMessage) *IMAgentResponse

// fileCompanionNoteContextTokensHook overrides the context window in tests.
var fileCompanionNoteContextTokensHook func() int

type videoNoteFrame struct {
	At   time.Duration
	Gray []byte
	JPEG []byte
	OCR  string
}

type videoNoteEvidence struct {
	Kind             string
	Duration         time.Duration
	Text             string
	Frames           []videoNoteFrame
	Vision           bool
	Notes            []string
	Chapters         []videoNoteChapter
	ChaptersInferred bool
}

func fileCompanionIsVideoNotePurpose(purpose string) bool {
	return strings.EqualFold(strings.TrimSpace(purpose), fileCompanionPurposeVideoNote)
}

func fileCompanionIsMediaExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v", ".webm", ".mov", ".avi", ".mkv",
		".mp3", ".wav", ".ogg", ".oga", ".m4a", ".aac", ".flac", ".opus", ".silk":
		return true
	default:
		return false
	}
}

func fileCompanionMediaKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v", ".webm", ".mov", ".avi", ".mkv":
		return "video"
	case ".mp3", ".wav", ".ogg", ".oga", ".m4a", ".aac", ".flac", ".opus", ".silk":
		return "audio"
	default:
		return ""
	}
}

func fileCompanionVideoNotePhraseMatches(text string) bool {
	text = strings.TrimSpace(text)
	text = strings.TrimRight(text, "。.!！?？")
	if text == "" {
		return false
	}
	if strings.Contains(text, "不要") || strings.Contains(text, "别") || strings.Contains(text, "不用") {
		return false
	}
	if strings.ContainsAny(text, "，,") {
		return false
	}
	switch text {
	case "视频笔记", "音频笔记", "整理成笔记", "帮我记笔记":
		return true
	}
	if fileCompanionNoteAlias(text) {
		return true
	}
	return fileCompanionVideoNotePhraseRe.MatchString(text)
}

// fileCompanionCommandKey is the exact phrase after trim and trailing
// punctuation, compared case-insensitively. A longer sentence must not match.
func fileCompanionCommandKey(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimRight(text, "。.!！?？")
	return strings.ToLower(text)
}

func fileCompanionNoteAlias(text string) bool {
	switch fileCompanionCommandKey(text) {
	case "generate notes", "take notes", "make notes", "make a note", "create a note",
		"тэмдэглэл үүсгэх", "тэмдэглэл хийх", "тэмдэглэл бичих":
		return true
	default:
		return false
	}
}

func fileCompanionPurposeConflict(path string, req FileCompanionTurnRequest) string {
	kind := fileCompanionMediaKind(path)
	if fileCompanionIsPaperPurpose(req.Purpose) && kind != "" {
		return fileCompanionErrNotPaper
	}
	if fileCompanionIsVideoNotePurpose(req.Purpose) && kind == "" {
		return fileCompanionErrNoteMediaOnly
	}
	return ""
}

func fileCompanionWantsVideoNote(path string, req FileCompanionTurnRequest) bool {
	if fileCompanionIsVideoNotePurpose(req.Purpose) {
		return fileCompanionMediaKind(path) != ""
	}
	kind := fileCompanionMediaKind(path)
	return kind != "" && fileCompanionVideoNotePhraseMatches(req.Text)
}

func fileCompanionNoteStem(source string) string {
	base := strings.TrimSuffix(filepath.Base(strings.TrimSpace(source)), filepath.Ext(source))
	base = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, base)
	base = strings.Trim(base, " .")
	rs := []rune(base)
	if len(rs) > 60 {
		base = strings.TrimRight(string(rs[:60]), " .")
	}
	if strings.TrimSpace(base) == "" {
		base = "document"
	}
	return base
}

func fileCompanionNotePaths(dataDir, sessionID, source string) (md, pdf string, err error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, `/\`) {
		return "", "", errors.New("缺少会话")
	}
	dir := filepath.Join(dataDir, "file-companion", "results", sessionID)
	stem := fileCompanionNoteStem(source)
	return filepath.Join(dir, stem+"-笔记.md"), filepath.Join(dir, stem+"-笔记.pdf"), nil
}

func fileCompanionNoteMAD(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return fileCompanionNoteMADThreshold
	}
	var sum int
	for i := 0; i < n; i++ {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		sum += d
	}
	return sum / n
}

// dedupVideoNoteFrames keeps the last frame of each similar run, then thins
// to 24 runs. The last run stays. Call this once, after the candidates exist.
// Thinning on every new frame crowds a long recording toward its end.
func dedupVideoNoteFrames(frames []videoNoteFrame) []videoNoteFrame {
	return thinVideoNoteRuns(collapseVideoNoteRuns(frames), fileCompanionNoteMaxFrames)
}

// collapseVideoNoteRuns keeps the last frame of each similar run.
// Comparison is the 16×16 grayscale mean absolute difference against the
// previous candidate. MAD < 12 replaces the run; MAD >= 12 starts a new run.
func collapseVideoNoteRuns(frames []videoNoteFrame) []videoNoteFrame {
	if len(frames) == 0 {
		return nil
	}
	var runs []videoNoteFrame
	var prev []byte
	for _, frame := range frames {
		if prev == nil {
			runs = append(runs, frame)
			prev = append([]byte(nil), frame.Gray...)
			continue
		}
		if fileCompanionNoteMAD(prev, frame.Gray) < fileCompanionNoteMADThreshold {
			runs[len(runs)-1] = frame
		} else {
			runs = append(runs, frame)
		}
		prev = append([]byte(nil), frame.Gray...)
	}
	return runs
}

// fileCompanionKeepWebShot folds one captured frame into the open run.
// The 24-frame thin happens once when recording stops.
func fileCompanionKeepWebShot(frames []videoNoteFrame, shot videoNoteFrame) []videoNoteFrame {
	return collapseVideoNoteRuns(append(frames, shot))
}

func thinVideoNoteRuns(runs []videoNoteFrame, cap int) []videoNoteFrame {
	if cap < 1 || len(runs) <= cap {
		return runs
	}
	head := runs[:len(runs)-1]
	want := cap - 1
	picked := make([]videoNoteFrame, 0, cap)
	if want >= len(head) {
		picked = append(picked, head...)
	} else if want == 1 {
		picked = append(picked, head[0])
	} else {
		seen := map[int]bool{}
		for i := 0; i < want; i++ {
			idx := i * (len(head) - 1) / (want - 1)
			if seen[idx] {
				continue
			}
			seen[idx] = true
			picked = append(picked, head[idx])
		}
	}
	picked = append(picked, runs[len(runs)-1])
	return picked
}

// capVideoNoteJPEGBytes drops the earliest kept frames until the JPEGs fit in 4 MB.
// The last frame stays, matching the local extractor.
func capVideoNoteJPEGBytes(frames []videoNoteFrame) ([]videoNoteFrame, int) {
	total := 0
	for _, frame := range frames {
		total += len(frame.JPEG)
	}
	dropped := 0
	for total > 4<<20 && len(frames) > 1 {
		dropped++
		total -= len(frames[0].JPEG)
		frames = frames[1:]
	}
	return frames, dropped
}

func fileCompanionNoteContextTokens(a *App) int {
	if fileCompanionNoteContextTokensHook != nil {
		return fileCompanionNoteContextTokensHook()
	}
	if a != nil {
		if n := a.GetMaclawLLMConfig().EffectiveContextTokens(); n > 0 {
			return n
		}
	}
	return 128000
}

// fileCompanionNoteKeyframes lists the pictures embedded in a note so the
// chat can show them. The prose hides the file path; the strip shows the
// bytes. At most one turn's figure cap is registered.
func (a *App) fileCompanionNoteKeyframes(userID, markdown, frameDir string) []FileCompanionFigure {
	if a == nil || strings.TrimSpace(frameDir) == "" {
		return nil
	}
	matches := fileCompanionMarkdownImageRe.FindAllStringSubmatch(markdown, -1)
	var figures []FileCompanionFigure
	var listed []companionFigure
	for _, match := range matches {
		if len(figures) >= fileCompanionToolFigureCap {
			break
		}
		alt := strings.TrimSpace(match[1])
		path := strings.TrimSpace(match[2])
		if path == "" && len(match) > 3 {
			path = strings.TrimSpace(match[3])
		}
		// The model writes the note. Only frames this host just stored are shown.
		if path == "" || !strings.HasPrefix(alt, "板书") || !companionFigureInsideDir(frameDir, path) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() < paperFigureMinBytes {
			continue
		}
		figures = append(figures, FileCompanionFigure{
			Index:   len(figures) + 1,
			Label:   alt,
			Caption: "",
			Path:    path,
		})
		listed = append(listed, companionFigure{path: path, caption: alt, size: info.Size()})
	}
	if len(listed) == 0 {
		return nil
	}
	if _, err := a.appendFileCompanionTurnFigures(userID, listed); err != nil {
		return nil
	}
	return figures
}

// fileCompanionNoteProse is the note without picture lines. The file keeps
// those lines so the PDF can embed the frames. The chat and a later question
// show the sentences, not the frame path. The keyframe strip carries the pictures.
func fileCompanionNoteProse(markdown string) string {
	lines := strings.Split(markdown, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "![") && strings.Contains(trim, "](") && strings.HasSuffix(trim, ")") {
			continue
		}
		if trim == "" {
			if blank || len(out) == 0 {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func fileCompanionVideoNoteExcerpt(markdown string) string {
	markdown = fileCompanionNoteProse(markdown)
	if markdown == "" {
		return ""
	}
	rs := []rune(markdown)
	if len(rs) <= fileCompanionVideoNoteFollowTokens {
		return markdown
	}
	omission := "\n\n（中间已省略）\n\n"
	half := (fileCompanionVideoNoteFollowTokens - fileCompanionCountRunes(omission)) / 2
	if half < 1 {
		half = 1
	}
	return string(rs[:half]) + omission + string(rs[len(rs)-half:])
}

func fileCompanionAppendNoteExcerpt(base, excerpt string) string {
	excerpt = strings.TrimSpace(excerpt)
	if excerpt == "" {
		return base
	}
	return base + "\n\n" + fileCompanionNoteExcerptLabel + "\n" + excerpt
}

// fileCompanionStripNoteExcerptText removes the host note quote from one
// user utterance. A later question reads the note file again, so a regenerated
// note cannot stay behind in an older question.
func fileCompanionStripNoteExcerptText(text string) string {
	marker := "\n\n" + fileCompanionNoteExcerptLabel
	if idx := strings.Index(text, marker); idx >= 0 {
		return strings.TrimSpace(text[:idx])
	}
	return text
}

// fileCompanionStripNoteExcerptHistory drops the note quote from prior user
// turns before they are shown to the model or written back. Other owners are
// left unchanged. Assistant text is left unchanged.
func fileCompanionStripNoteExcerptHistory(userID string, history []agent.ConversationEntry) []agent.ConversationEntry {
	if !fileCompanionToolsDisabled(userID) || len(history) == 0 {
		return history
	}
	changed := false
	out := make([]agent.ConversationEntry, len(history))
	copy(out, history)
	for i := range out {
		if !strings.EqualFold(strings.TrimSpace(out[i].Role), "user") {
			continue
		}
		next, ok := fileCompanionStripNoteExcerptContent(out[i].Content)
		if !ok {
			continue
		}
		out[i].Content = next
		changed = true
	}
	if !changed {
		return history
	}
	return out
}

func fileCompanionStripNoteExcerptContent(content interface{}) (interface{}, bool) {
	switch v := content.(type) {
	case string:
		stripped := fileCompanionStripNoteExcerptText(v)
		if stripped == v {
			return content, false
		}
		return stripped, true
	case []interface{}:
		blocks := v
		copied := false
		for i, block := range v {
			bm, ok := block.(map[string]interface{})
			if !ok {
				continue
			}
			text, ok := bm["text"].(string)
			if !ok {
				continue
			}
			kind, _ := bm["type"].(string)
			if kind != "" && !normalizeIMContentBlockKind(kind).IsText() {
				continue
			}
			stripped := fileCompanionStripNoteExcerptText(text)
			if stripped == text {
				continue
			}
			if !copied {
				blocks = append([]interface{}(nil), v...)
				copied = true
			}
			clone := make(map[string]interface{}, len(bm))
			for key, val := range bm {
				clone[key] = val
			}
			clone["text"] = stripped
			blocks[i] = clone
		}
		if !copied {
			return content, false
		}
		return blocks, true
	default:
		return content, false
	}
}

type sidecarCue struct {
	Start time.Duration
	End   time.Duration
	Text  string
}

func fileCompanionReadSidecar(mediaPath string) (string, time.Duration, bool) {
	ext := filepath.Ext(mediaPath)
	stem := strings.TrimSuffix(mediaPath, ext)
	for _, cand := range []string{".srt", ".vtt", ".ass", ".ssa"} {
		path := stem + cand
		matches, _ := filepath.Glob(stem + cand)
		if len(matches) == 0 {
			// Windows is case-insensitive; Glob of the exact suffix is enough.
			if info, err := os.Stat(path); err != nil || info.IsDir() {
				continue
			}
			matches = []string{path}
		}
		body, err := os.ReadFile(matches[0])
		if err != nil || len(body) == 0 {
			continue
		}
		if len(body) > 2<<20 {
			body = body[:2<<20]
		}
		cues := parseSidecarCues(string(body))
		if len(cues) == 0 {
			continue
		}
		var b strings.Builder
		var end time.Duration
		for _, cue := range cues {
			if cue.End > end {
				end = cue.End
			}
			b.WriteString(formatNoteStamp(cue.Start))
			b.WriteByte(' ')
			b.WriteString(strings.TrimSpace(cue.Text))
			b.WriteByte('\n')
		}
		return b.String(), end, true
	}
	return "", 0, false
}

var sidecarStampRe = regexp.MustCompile(`(\d+):(\d{2}):(\d{2})[,.](\d{1,3})`)

func parseSidecarCues(body string) []sidecarCue {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var cues []sidecarCue
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.Contains(line, "-->") {
			continue
		}
		parts := strings.Split(line, "-->")
		if len(parts) < 2 {
			continue
		}
		start, ok1 := parseSidecarStamp(parts[0])
		end, ok2 := parseSidecarStamp(parts[1])
		if !ok1 || !ok2 {
			continue
		}
		var text []string
		for i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if next == "" || strings.Contains(next, "-->") {
				break
			}
			i++
			text = append(text, next)
		}
		cues = append(cues, sidecarCue{Start: start, End: end, Text: strings.Join(text, " ")})
	}
	return cues
}

func parseSidecarStamp(raw string) (time.Duration, bool) {
	m := sidecarStampRe.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return 0, false
	}
	h := atoiNote(m[1])
	min := atoiNote(m[2])
	sec := atoiNote(m[3])
	ms := atoiNote(m[4])
	if len(m[4]) == 1 {
		ms *= 100
	} else if len(m[4]) == 2 {
		ms *= 10
	}
	return time.Duration(h)*time.Hour + time.Duration(min)*time.Minute + time.Duration(sec)*time.Second + time.Duration(ms)*time.Millisecond, true
}

func atoiNote(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func formatNoteStamp(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d / time.Second)
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return sprintfStamp("%d:%02d:%02d", h, m, s)
	}
	return sprintfStamp("%02d:%02d", m, s)
}

func sprintfStamp(format string, args ...int) string {
	// Small local formatter so the note package stays free of fmt in hot stamps.
	var b strings.Builder
	arg := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			b.WriteByte(format[i])
			continue
		}
		if i+1 < len(format) && format[i+1] == '%' {
			b.WriteByte('%')
			i++
			continue
		}
		width := 0
		i++
		for i < len(format) && format[i] >= '0' && format[i] <= '9' {
			width = width*10 + int(format[i]-'0')
			i++
		}
		if i < len(format) && (format[i] == 'd') && arg < len(args) {
			writeIntWidth(&b, args[arg], width)
			arg++
		}
	}
	return b.String()
}

func writeIntWidth(b *strings.Builder, n, width int) {
	if n < 0 {
		b.WriteByte('-')
		n = -n
	}
	var buf [16]byte
	i := len(buf)
	if n == 0 {
		i--
		buf[i] = '0'
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	for len(buf)-i < width {
		i--
		buf[i] = '0'
	}
	b.Write(buf[i:])
}

func slicePCMWavForNative(data []byte) ([][]byte, error) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("not a wav")
	}
	format, rate, channels, bits, dataOff, dataLen, err := wavPCMLayout(data)
	if err != nil {
		return nil, err
	}
	if format != 1 {
		return nil, errors.New("non-pcm wav")
	}
	if channels <= 0 || bits < 8 || rate <= 0 {
		return nil, errors.New("bad wav")
	}
	block := channels * bits / 8
	if block <= 0 || dataOff < 0 || dataOff+dataLen > len(data) {
		return nil, errors.New("bad wav")
	}
	pcm := data[dataOff : dataOff+dataLen]
	bytesPerSec := rate * block
	maxByTime := bytesPerSec * 60
	maxBySize := int(audioconv.MaxNativeAudioInputBytes) - 44
	if maxBySize < block {
		maxBySize = block
	}
	chunk := maxByTime
	if chunk > maxBySize {
		chunk = maxBySize
	}
	chunk -= chunk % block
	if chunk <= 0 {
		return nil, errors.New("bad wav")
	}
	var out [][]byte
	for off := 0; off < len(pcm); {
		n := chunk
		if off+n > len(pcm) {
			n = len(pcm) - off
			n -= n % block
		}
		if n <= 0 {
			break
		}
		out = append(out, wavWrap(format, rate, channels, bits, pcm[off:off+n]))
		off += n
	}
	return out, nil
}

func wavPCMLayout(data []byte) (format, rate, channels, bits, dataOff, dataLen int, err error) {
	off := 12
	for off+8 <= len(data) {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		off += 8
		if size < 0 || off+size > len(data) {
			return 0, 0, 0, 0, 0, 0, errors.New("bad wav")
		}
		if id == "fmt " && size >= 16 {
			format = int(binary.LittleEndian.Uint16(data[off : off+2]))
			channels = int(binary.LittleEndian.Uint16(data[off+2 : off+4]))
			rate = int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
			bits = int(binary.LittleEndian.Uint16(data[off+14 : off+16]))
		}
		if id == "data" {
			return format, rate, channels, bits, off, size, nil
		}
		off += size
		if size%2 == 1 {
			off++
		}
	}
	return 0, 0, 0, 0, 0, 0, errors.New("bad wav")
}

// fileCompanionCaptureSpan is how long the capture ran: the later of the
// last kept frame and a parsed waveform, including silence. Callers drop
// black frames first. A waveform that does not parse does not invent a
// minute. Chapter lines use fileCompanionNoteClock, so this silence is
// not itself a timeline of events.
func fileCompanionCaptureSpan(audio []byte, frames []videoNoteFrame) time.Duration {
	var span time.Duration
	for _, frame := range frames {
		if frame.At > span {
			span = frame.At
		}
	}
	format, rate, channels, bits, _, dataLen, err := wavPCMLayout(audio)
	if err != nil || format != 1 || rate <= 0 || channels <= 0 || bits < 8 || dataLen <= 0 {
		return span
	}
	if d := pieceDuration(audio); d > span {
		span = d
	}
	return span
}

// fileCompanionNoteClock is the timeline chapter lines may use.
// Speech keeps the capture span, so a sentence can outlast the last
// picture. Silence has no such events: the only times are the kept
// frames. A silent tail after the last picture, including a black
// stretch already dropped before the span was measured, is not a chapter.
func fileCompanionNoteClock(noAudio bool, capture time.Duration, frames []videoNoteFrame) time.Duration {
	if !noAudio {
		return capture
	}
	var clock time.Duration
	for _, frame := range frames {
		if frame.At > clock {
			clock = frame.At
		}
	}
	return clock
}

// wavPCMSilent reports a 16-bit PCM recording whose samples are all zero.
// Process loopback writes that buffer when the device marks the packet silent.
// It is not speech: a quiet voice still moves the samples.
func wavPCMSilent(wav []byte) bool {
	format, _, channels, bits, dataOff, dataLen, err := wavPCMLayout(wav)
	if err != nil || format != 1 || channels <= 0 || bits != 16 || dataLen < 2 {
		return false
	}
	end := dataOff + dataLen
	if end > len(wav) {
		return false
	}
	for i := dataOff; i+1 < end; i += 2 {
		if wav[i] != 0 || wav[i+1] != 0 {
			return false
		}
	}
	return true
}

func wavWrap(format, rate, channels, bits int, pcm []byte) []byte {
	block := channels * bits / 8
	hdr := make([]byte, 44)
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(36+len(pcm)))
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], uint16(format))
	binary.LittleEndian.PutUint16(hdr[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(hdr[28:32], uint32(rate*block))
	binary.LittleEndian.PutUint16(hdr[32:34], uint16(block))
	binary.LittleEndian.PutUint16(hdr[34:36], uint16(bits))
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], uint32(len(pcm)))
	return append(hdr, pcm...)
}

func oggHasOpusHead(data []byte) bool {
	n := len(data)
	if n > 64<<10 {
		n = 64 << 10
	}
	return strings.Contains(string(data[:n]), "OpusHead")
}

func nativeAudioTooLarge(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "input audio too large") || strings.Contains(msg, "decoded PCM too large")
}

func buildNoteModelPrompt(name, kind string, ev videoNoteEvidence) string {
	var b strings.Builder
	b.WriteString("你是文件伴读。只根据下面的证据写笔记。不要使用工具。不要编造证据里没有的人名、数字和公式。\n")
	b.WriteString("文件：")
	b.WriteString(name)
	b.WriteString("\n用 Markdown，只写这五个二级标题，不要增删：\n\n## 概要\n## 章节\n## 要点\n## 金句\n## 不确定的地方\n\n")
	b.WriteString("章节的每一条以 mm:ss、h:mm:ss 或 0:00–2:00 这种范围开头，一段内容一行，不要为每个画面各写一行。金句以时间开头，用「」短引一句原话。要点只写几条结论，不要为结论标时间，也不要按分钟复述口播。口播是中文就用中文写，是英文就用英文写。不要声称已经生成了笔记或 PDF。\n")
	if sec := int(ev.Duration / time.Second); sec > 0 {
		// The rewrite can only drop a clock past this span. A progress bar
		// painted on the frame is a different clock and still looks legal
		// when that number happens to fall inside the span.
		b.WriteString("这次记录从 0:00 到 ")
		b.WriteString(formatNoteStamp(ev.Duration))
		b.WriteString("（")
		b.WriteString(itoaNote(sec))
		b.WriteString(" 秒）。章节和金句用这个钟。画面 t= 后面的数字是这个钟的秒。画面上另印的进度、总时长和时钟不是这个钟。\n")
	}
	b.WriteString("用自己的话整理。不要把转写或 OCR 原文抄进笔记。\n")
	if ev.Vision {
		b.WriteString("看得见的公式写成 $...$ 或独立成行的 $$...$$。板书写成普通文字。没有在画面里看见的式子放进「不确定的地方」。\n")
	} else {
		b.WriteString("看不到画面时，OCR 行只给你对照，不要把它写成公式，也不要写进笔记。式子以图为准。看到「画面未识别」或「画面无法采集」时不要描写画面。\n")
	}
	if len(ev.Chapters) > 0 {
		b.WriteString("章节边界：\n")
		for _, chapter := range ev.Chapters {
			b.WriteString(formatNoteStamp(chapter.Start))
			b.WriteByte(' ')
			b.WriteString(strings.TrimSpace(chapter.Title))
			b.WriteByte('\n')
		}
	}
	if ev.ChaptersInferred {
		b.WriteString("没有可靠的章节边界。请在「章节」里把推断出来的分段标成推断。\n")
	}
	for _, note := range ev.Notes {
		b.WriteString(note)
		b.WriteByte('\n')
	}
	b.WriteString("\n证据：\n")
	b.WriteString(ev.Text)
	for _, frame := range ev.Frames {
		b.WriteByte('\n')
		b.WriteString("画面 t=")
		b.WriteString(itoaNote(int(frame.At / time.Second)))
		b.WriteString("s")
		if ev.Vision {
			continue
		}
		b.WriteByte(' ')
		if strings.TrimSpace(frame.OCR) == "" {
			b.WriteString(fileCompanionErrFrameUnread)
			continue
		}
		b.WriteString(fileCompanionErrOCRUncertain)
		b.WriteString("：")
		b.WriteString(fileCompanionTruncateRunes(frame.OCR, 400))
	}
	return b.String()
}

func itoaNote(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func fileCompanionNoteSessionKey(sessionID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sessionID)))
	return hex.EncodeToString(sum[:8])
}

func fileCompanionStripMediaPaths(markdown, media string) string {
	if media == "" {
		return markdown
	}
	return strings.ReplaceAll(markdown, media, filepath.Base(media))
}

// fileCompanionFrameDirMark stands in for the frames directory until the note
// is bound. The PDF reads the staged directory. The saved note names the
// published directory. Both strings come from this one token.
const fileCompanionFrameDirMark = "\x00maclaw-frame-dir\x00"

// fileCompanionNotePublishHook runs after the PDF bytes exist and before they
// replace the previous note. Tests cancel the note there.
var fileCompanionNotePublishHook func()

func fileCompanionWriteNoteFiles(a *App, canonical, sessionID, userID string, clearGen uint64, title, markdown string, embed []videoNoteFrame, ctx context.Context) (string, error) {
	if a == nil {
		return "", errors.New("app unavailable")
	}
	mdPath, pdfPath, err := fileCompanionNotePaths(a.GetDataDir(), sessionID, canonical)
	if err != nil {
		return "", err
	}
	if sameFileCompanionExportPath(canonical, mdPath) || sameFileCompanionExportPath(canonical, pdfPath) {
		return "", errors.New(fileCompanionErrCannotOverwrite)
	}
	if a.fileCompanionNoteStopped(ctx, userID, clearGen) {
		return "", errors.New(fileCompanionErrNoteCancelled)
	}
	dir := filepath.Dir(mdPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	frameDir := filepath.Join(dir, "frames")
	stageDir := filepath.Join(dir, "frames.next")
	if abs, absErr := filepath.Abs(frameDir); absErr == nil {
		frameDir = abs
	}
	if abs, absErr := filepath.Abs(stageDir); absErr == nil {
		stageDir = abs
	}
	_ = os.RemoveAll(stageDir)
	plain := fileCompanionStripMediaPaths(strings.TrimSpace(markdown), canonical)
	staged := plain
	published := plain
	if len(embed) > 0 {
		marked := fileCompanionInsertNoteFramesInto(plain, stageDir, embed)
		staged = fileCompanionBindFrameDir(marked, stageDir)
		published = fileCompanionBindFrameDir(marked, frameDir)
	}
	if strings.TrimSpace(title) == "" {
		title = "视频笔记"
	}
	tmpMD := mdPath + ".tmp"
	tmpPDF := pdfPath + ".tmp"
	cleanup := func() {
		_ = os.Remove(tmpMD)
		_ = os.Remove(tmpPDF)
		_ = os.RemoveAll(stageDir)
	}
	if err := os.WriteFile(tmpMD, []byte(published+"\n"), 0o644); err != nil {
		cleanup()
		return "", err
	}
	footer := strings.TrimSpace(brand.Current().DisplayName) + " 伴读"
	if _, err := docgen.GenerateToFile(docgen.Spec{
		Title:          title,
		Subtitle:       filepath.Base(canonical),
		Content:        staged,
		FooterHint:     footer,
		Brand:          brand.Current().DisplayName,
		FileNamePrefix: "note",
		PaperSize:      "A4",
		Colorful:       false,
	}, tmpPDF); err != nil {
		cleanup()
		return "", err
	}
	if fileCompanionNotePublishHook != nil {
		fileCompanionNotePublishHook()
	}
	// The previous note stays in place until this attempt is still wanted.
	if a.fileCompanionNoteStopped(ctx, userID, clearGen) {
		cleanup()
		return "", errors.New(fileCompanionErrNoteCancelled)
	}
	restoreFrames, err := fileCompanionPromoteFrames(stageDir, frameDir)
	if err != nil {
		cleanup()
		return "", err
	}
	pdfBackup, err := fileCompanionReplaceFile(tmpPDF, pdfPath)
	if err != nil {
		restoreFrames()
		cleanup()
		return "", err
	}
	if _, err := fileCompanionReplaceFile(tmpMD, mdPath); err != nil {
		// The new PDF is already at pdfPath. Put the previous one back, or
		// remove the new one when this session had no note yet.
		if pdfBackup != "" {
			_ = os.Remove(pdfPath)
			_ = os.Rename(pdfBackup, pdfPath)
		} else {
			_ = os.Remove(pdfPath)
		}
		restoreFrames()
		cleanup()
		return "", err
	}
	if pdfBackup != "" {
		_ = os.Remove(pdfBackup)
	}
	_ = os.RemoveAll(frameDir + ".old")
	_ = os.WriteFile(filepath.Join(dir, "latest-video-note.txt"), []byte(pdfPath), 0o644)
	return pdfPath, nil
}

func fileCompanionPromoteFrames(stageDir, frameDir string) (func(), error) {
	noop := func() {}
	info, err := os.Stat(stageDir)
	if err != nil || !info.IsDir() {
		return noop, nil
	}
	entries, err := os.ReadDir(stageDir)
	if err != nil {
		return noop, err
	}
	if len(entries) == 0 {
		_ = os.RemoveAll(stageDir)
		return noop, nil
	}
	oldDir := frameDir + ".old"
	_ = os.RemoveAll(oldDir)
	movedOld := false
	if _, statErr := os.Stat(frameDir); statErr == nil {
		if err := os.Rename(frameDir, oldDir); err != nil {
			return noop, err
		}
		movedOld = true
	}
	if err := os.Rename(stageDir, frameDir); err != nil {
		if movedOld {
			_ = os.Rename(oldDir, frameDir)
		}
		return noop, err
	}
	return func() {
		_ = os.RemoveAll(frameDir)
		if movedOld {
			_ = os.Rename(oldDir, frameDir)
		}
	}, nil
}

func fileCompanionReplaceFile(src, dst string) (string, error) {
	backup := dst + ".bak"
	_ = os.Remove(backup)
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, backup); err != nil {
			return "", err
		}
	} else {
		backup = ""
	}
	if err := os.Rename(src, dst); err != nil {
		if backup != "" {
			_ = os.Rename(backup, dst)
		}
		return "", err
	}
	return backup, nil
}

// fileCompanionInsertNoteFrames writes kept JPEGs under results/<session>/frames
// and puts each picture on its own line so docgen's image regex matches it.
// OCR text is never wrapped in $...$.
func fileCompanionInsertNoteFrames(markdown, resultDir string, embed []videoNoteFrame) string {
	dir := filepath.Join(resultDir, "frames")
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return fileCompanionBindFrameDir(fileCompanionInsertNoteFramesInto(markdown, dir, embed), dir)
}

func fileCompanionBindFrameDir(markdown, dir string) string {
	return strings.ReplaceAll(markdown, fileCompanionFrameDirMark, dir)
}

func fileCompanionInsertNoteFramesInto(markdown, frameDir string, embed []videoNoteFrame) string {
	if len(embed) == 0 {
		return markdown
	}
	if err := os.MkdirAll(frameDir, 0o755); err != nil {
		return markdown
	}
	var blocks []noteFrameBlock
	for i, frame := range embed {
		sec := int(frame.At / time.Second)
		name := itoaNote(sec) + ".jpg"
		if i > 0 {
			name = itoaNote(sec) + "-" + itoaNote(i) + ".jpg"
		}
		dest := filepath.Join(frameDir, name)
		if len(frame.JPEG) == 0 {
			continue
		}
		if err := os.WriteFile(dest, frame.JPEG, 0o644); err != nil {
			continue
		}
		stamp := formatNoteStamp(frame.At)
		var image strings.Builder
		image.WriteString("![板书 ")
		image.WriteString(stamp)
		image.WriteString("](<")
		image.WriteString(fileCompanionFrameDirMark)
		image.WriteString(string(filepath.Separator))
		image.WriteString(name)
		image.WriteString(">)")
		// The chapter line already says this sentence. Repeating it under
		// every frame of that span is the same words again. Raw OCR is not
		// a caption. A picture that is not under that line still keeps it.
		caption := fileCompanionOrganizedCaption(markdown, frame.At)
		blocks = append(blocks, noteFrameBlock{
			at:      frame.At,
			image:   image.String(),
			caption: caption,
			unread:  caption == "" && strings.TrimSpace(frame.OCR) == "",
		})
	}
	return fileCompanionPlaceFrameBlocks(markdown, blocks)
}

type noteFrameBlock struct {
	at      time.Duration
	image   string
	caption string
	unread  bool
}

// fileCompanionFrameBlockText keeps the organized sentence when this picture
// is not already sitting under that same chapter line.
func fileCompanionFrameBlockText(block noteFrameBlock, anchorProse string) string {
	if block.caption != "" && block.caption == anchorProse {
		return block.image
	}
	if block.caption != "" {
		return block.image + "\n\n" + block.caption
	}
	if block.unread {
		return block.image + "\n\n" + fileCompanionErrFrameUnread
	}
	return block.image
}

// fileCompanionOrganizedCaption is the chapter sentence that covers at.
// It is the model's own wording. A line of raw OCR is not a caption.
func fileCompanionOrganizedCaption(markdown string, at time.Duration) string {
	lines := strings.Split(markdown, "\n")
	in := false
	inMath := false
	mathClose := ""
	var best string
	var bestStart time.Duration
	found := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "## 章节" {
			in = true
			inMath = false
			mathClose = ""
			continue
		}
		if in && strings.HasPrefix(trim, "## ") {
			break
		}
		if !in || strings.HasPrefix(trim, "![") {
			continue
		}
		var inside bool
		inMath, mathClose, inside = fileCompanionDisplayMathWalk(trim, inMath, mathClose)
		if inside {
			continue
		}
		start, prose, ok := fileCompanionChapterSpan(trim)
		if !ok || start > at {
			continue
		}
		if !found || start >= bestStart {
			best = prose
			bestStart = start
			found = true
		}
	}
	return best
}

func fileCompanionChapterSpan(line string) (time.Duration, string, bool) {
	trim := strings.TrimSpace(line)
	for strings.HasPrefix(trim, fileCompanionErrTimeUnknown) {
		trim = strings.TrimSpace(strings.TrimPrefix(trim, fileCompanionErrTimeUnknown))
	}
	trim = strings.TrimLeft(trim, "-–— ")
	if trim == "" || strings.Contains(trim, fileCompanionErrOCRUncertain) {
		return 0, "", false
	}
	token, prose, _ := strings.Cut(trim, " ")
	start, ok := fileCompanionLeadingStamp(token)
	if !ok {
		return 0, "", false
	}
	prose = strings.TrimSpace(prose)
	if prose == "" {
		return 0, "", false
	}
	return start, prose, true
}

func fileCompanionLeadingStamp(token string) (time.Duration, bool) {
	for _, sep := range []string{"–", "—", "~", "～", "-"} {
		head, tail, found := strings.Cut(token, sep)
		if !found || head == "" || tail == "" {
			continue
		}
		start, _, ok := splitNoteStamp(head)
		if ok {
			return start, true
		}
	}
	start, _, ok := splitNoteStamp(token)
	return start, ok
}

// fileCompanionRangeClock is the later clock of a chapter range, and the
// words after that clock. Frame order still uses the start. A line
// 0:00–13:32, 0:00 – 13:32, or 0:27–2:04，检查 is past a recording that
// ended before that end. An illegal end such as 99:99 is broken, and the
// words glued to it stay. A dash before ordinary words is prose, not a range.
func fileCompanionRangeClock(line string) (end time.Duration, prose string, found, broken bool) {
	trim := strings.TrimSpace(line)
	for strings.HasPrefix(trim, fileCompanionErrTimeUnknown) {
		trim = strings.TrimSpace(strings.TrimPrefix(trim, fileCompanionErrTimeUnknown))
	}
	trim = strings.TrimLeft(trim, "-*• ")
	if trim == "" {
		return 0, "", false, false
	}
	token, rest, _ := strings.Cut(trim, " ")
	for _, sep := range []string{"–", "—", "~", "～", "-"} {
		head, tail, ok := strings.Cut(token, sep)
		if !ok || head == "" || tail == "" {
			continue
		}
		if _, _, headOK := splitNoteStamp(head); !headOK {
			continue
		}
		end, prose, found, broken, isClock := fileCompanionClockTail(tail, rest)
		if !isClock {
			continue
		}
		return end, prose, found, broken
	}
	_, after, startOK := splitNoteStamp(trim)
	if !startOK {
		return 0, "", false, false
	}
	rest, ok := fileCompanionTrimRangeSep(after)
	if !ok {
		return 0, "", false, false
	}
	end, prose, found, broken, isClock := fileCompanionClockTail(rest, "")
	if !isClock {
		return 0, "", false, false
	}
	return end, prose, found, broken
}

// fileCompanionClockTail reads the end of a range. Words glued to that
// clock, including after a Chinese comma, are the sentence. A tail that
// is not a clock at all is left for the caller.
func fileCompanionClockTail(tail, outer string) (end time.Duration, prose string, found, broken, isClock bool) {
	if d, tailRest, ok := splitNoteStamp(tail); ok {
		return d, fileCompanionJoinClockProse(tailRest, outer), true, false, true
	}
	if d, glued, ok := noteStampPrefix(tail); ok {
		if more, sep := fileCompanionTrimRangeSep(glued); sep {
			d2, prose2, found2, broken2, clock2 := fileCompanionClockTail(more, outer)
			if clock2 {
				if broken2 {
					return 0, prose2, false, true, true
				}
				if found2 && d2 > d {
					d = d2
				}
				return d, prose2, true, false, true
			}
			return d, fileCompanionJoinClockProse(more, outer), true, false, true
		}
		return d, fileCompanionJoinClockProse(glued, outer), true, false, true
	}
	if left, bad := invalidClockPrefix(tail); bad {
		if more, sep := fileCompanionTrimRangeSep(left); sep {
			_, prose2, _, _, clock2 := fileCompanionClockTail(more, outer)
			if clock2 {
				return 0, prose2, false, true, true
			}
		}
		return 0, fileCompanionJoinClockProse(left, outer), false, true, true
	}
	return 0, "", false, false, false
}

func fileCompanionJoinClockProse(glued, outer string) string {
	glued = strings.TrimSpace(glued)
	outer = strings.TrimSpace(outer)
	switch {
	case glued == "" && outer == "":
		return ""
	case glued == "":
		return fileCompanionClockProse(outer)
	case outer == "":
		return fileCompanionClockProse(glued)
	default:
		return fileCompanionClockProse(glued + " " + outer)
	}
}

func fileCompanionClockProse(s string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(s), "，,、：:；;。．. "))
}

// noteStampPrefix reads a leading mm:ss or h:mm:ss. Digits stop at the
// first non-digit, so 2:04，检查 is 2:04 and the comma stays in the rest.
func noteStampPrefix(s string) (time.Duration, string, bool) {
	raw, rest := takeStampToken(s)
	if raw == "" {
		return 0, "", false
	}
	d, _, ok := splitNoteStamp(raw)
	if !ok {
		return 0, "", false
	}
	return d, rest, true
}

func invalidClockPrefix(s string) (string, bool) {
	raw, rest := takeStampToken(s)
	if raw == "" {
		return "", false
	}
	if _, _, ok := splitNoteStamp(raw); ok {
		return "", false
	}
	return rest, true
}

func takeStampToken(s string) (raw, rest string) {
	i := 0
	readDigits := func() bool {
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		return i > start
	}
	if !readDigits() || i >= len(s) || s[i] != ':' {
		return "", s
	}
	i++
	if !readDigits() {
		return "", s
	}
	if i < len(s) && s[i] == ':' {
		i++
		if !readDigits() {
			return "", s
		}
	}
	return s[:i], s[i:]
}

func fileCompanionTrimRangeSep(s string) (string, bool) {
	s = strings.TrimSpace(s)
	for _, sep := range []string{"–", "—", "~", "～", "-"} {
		if strings.HasPrefix(s, sep) {
			return strings.TrimSpace(strings.TrimPrefix(s, sep)), true
		}
	}
	return "", false
}

func fileCompanionPlaceFrameBlocks(markdown string, blocks []noteFrameBlock) string {
	if len(blocks) == 0 {
		return markdown
	}
	lines := strings.Split(markdown, "\n")
	start := -1
	end := len(lines)
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if start < 0 && trim == "## 章节" {
			start = i
			continue
		}
		if start >= 0 && i > start && strings.HasPrefix(trim, "## ") {
			end = i
			break
		}
	}
	if start < 0 {
		var b strings.Builder
		b.WriteString(strings.TrimRight(markdown, "\n"))
		b.WriteString("\n\n## 章节\n")
		for _, block := range blocks {
			b.WriteString(formatNoteStamp(block.at))
			b.WriteString("\n\n")
			b.WriteString(fileCompanionFrameBlockText(block, ""))
			b.WriteByte('\n')
		}
		return b.String()
	}
	// Anchors are the chapter lines before any picture is inserted. A
	// 0:00–2:00 range counts. Positions are measured on that original text,
	// then later frames go in first. A shared anchor is left unchanged so
	// the earlier frame stays above the later one, including when every
	// frame falls through to the end of the section.
	pos := make([]int, len(blocks))
	for n, block := range blocks {
		insertAt := end
		inMath := false
		mathClose := ""
		for i := start + 1; i < end && i < len(lines); i++ {
			var inside bool
			inMath, mathClose, inside = fileCompanionDisplayMathWalk(lines[i], inMath, mathClose)
			if inside {
				continue
			}
			stamp, ok := fileCompanionNoteLineStamp(lines[i])
			if ok && stamp <= block.at {
				insertAt = i + 1
			}
		}
		pos[n] = insertAt
	}
	for n := len(blocks) - 1; n >= 0; n-- {
		insertAt := pos[n]
		if insertAt < 0 {
			insertAt = 0
		}
		if insertAt > len(lines) {
			insertAt = len(lines)
		}
		anchor := ""
		if insertAt > 0 {
			if _, prose, ok := fileCompanionChapterSpan(lines[insertAt-1]); ok {
				anchor = prose
			}
		}
		piece := []string{"", fileCompanionFrameBlockText(blocks[n], anchor)}
		lines = append(lines[:insertAt], append(piece, lines[insertAt:]...)...)
		for m := 0; m < n; m++ {
			if pos[m] > insertAt {
				pos[m] += len(piece)
			}
		}
	}
	return strings.Join(lines, "\n")
}

// fileCompanionNoteLineStamp is the start of a chapter line. Bullets,
// a false 时间不明 prefix, and a range all still name a time.
func fileCompanionNoteLineStamp(line string) (time.Duration, bool) {
	stamp, _, _, ok := fileCompanionCanonicalStampLine(line)
	return stamp, ok
}

func fileCompanionTruncateRunes(s string, n int) string {
	if n <= 0 || fileCompanionCountRunes(s) <= n {
		return s
	}
	rs := []rune(s)
	return string(rs[:n])
}

func (a *App) fileCompanionRememberNote(userID, userText, canonical string) {
	if a == nil || a.imHandler == nil || a.imHandler.memory == nil {
		return
	}
	turn := fileCompanionVideoNoteLabel
	if strings.TrimSpace(userText) != "" {
		turn = strings.TrimSpace(userText)
	}
	body := turn + "\n\n" + fileCompanionVideoNoteMarker + "\n\n" + agent.FilePathPromptPrefix + "\n" + canonical + "\n"
	// Save replaces the session. The note pair has to carry the earlier
	// transcript, or generating a note deletes the questions before it.
	history := append([]agent.ConversationEntry(nil), a.imHandler.memory.Load(userID)...)
	history = append(history,
		agent.ConversationEntry{Role: "user", Content: body},
		agent.ConversationEntry{Role: "assistant", Content: fileCompanionVideoNoteStored},
	)
	a.imHandler.saveConversationHistoryTimed(userID, history, nil)
	// The note file is replaced on regenerate. The turn index is the saved
	// placeholder that owns that file, counted after save so a rejected save
	// cannot point at a row that was not written.
	sessionID := strings.TrimPrefix(strings.TrimSpace(userID), "file-companion:")
	a.fileCompanionRecordVideoNoteTurn(sessionID, fileCompanionCountStoredNotes(a.imHandler.memory.Load(userID)))
}

func fileCompanionCountStoredNotes(entries []agent.ConversationEntry) int {
	n := 0
	for _, entry := range entries {
		if !strings.EqualFold(strings.TrimSpace(entry.Role), "assistant") {
			continue
		}
		text, ok := entry.Content.(string)
		if !ok || strings.TrimSpace(text) != fileCompanionVideoNoteStored {
			continue
		}
		n++
	}
	return n
}

func (a *App) fileCompanionRecordVideoNoteTurn(sessionID string, n int) {
	if a == nil || n < 1 {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, `/\`) {
		return
	}
	dir := filepath.Join(a.GetDataDir(), "file-companion", "results", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "latest-video-note-turn.txt"), []byte(itoaNote(n)), 0o644)
}

func (a *App) fileCompanionVideoNoteTurnIndex(sessionID string) int {
	if a == nil {
		return 0
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, `/\`) {
		return 0
	}
	raw, err := os.ReadFile(filepath.Join(a.GetDataDir(), "file-companion", "results", sessionID, "latest-video-note-turn.txt"))
	if err != nil {
		return 0
	}
	n := 0
	for _, r := range strings.TrimSpace(string(raw)) {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	if n < 1 {
		return 0
	}
	return n
}

func (a *App) fileCompanionAttachVideoNoteBody(sessionID string, messages []FileCompanionMessage) []FileCompanionMessage {
	if a == nil || sessionID == "" {
		return messages
	}
	md := a.fileCompanionFindNoteMarkdown(sessionID)
	if md == "" {
		return messages
	}
	body, err := os.ReadFile(md)
	if err != nil {
		return messages
	}
	pdf := strings.TrimSuffix(md, ".md") + ".pdf"
	if _, statErr := os.Stat(pdf); statErr != nil {
		pdf = ""
	}
	// One note file is the latest generation. Only the turn that owns it
	// shows that body. A missing index means the file is still the latest one.
	index := a.fileCompanionVideoNoteTurnIndex(sessionID)
	chosen := -1
	last := -1
	seen := 0
	for i := range messages {
		if messages[i].Role != "assistant" || strings.TrimSpace(messages[i].Text) != fileCompanionVideoNoteStored {
			continue
		}
		seen++
		last = i
		if index > 0 && seen == index {
			chosen = i
			break
		}
	}
	if chosen < 0 {
		chosen = last
	}
	if chosen < 0 {
		return messages
	}
	messages[chosen].Text = fileCompanionNoteProse(string(body))
	messages[chosen].NoteCardPath = pdf
	messages[chosen].ResultPath = ""
	messages[chosen].VideoNote = true
	if a.fileCompanionWebFrame(sessionID) != "" {
		messages[chosen].WebNote = true
	}
	return messages
}

func (a *App) fileCompanionFindNoteMarkdown(sessionID string) string {
	dir := filepath.Join(a.GetDataDir(), "file-companion", "results", sessionID)
	matches, err := filepath.Glob(filepath.Join(dir, "*-笔记.md"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

func (a *App) fileCompanionLoadNoteExcerpt(canonical string) string {
	if a == nil {
		return ""
	}
	return a.fileCompanionLoadSessionNoteExcerpt(fileCompanionSessionID(canonical))
}

// fileCompanionLoadSessionNoteExcerpt reads the note already written for this
// tab. A later question on that tab, including a web page, quotes it. The
// note turn itself does not, so regenerating a note does not paste the old one.
func (a *App) fileCompanionLoadSessionNoteExcerpt(sessionID string) string {
	if a == nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	md := a.fileCompanionFindNoteMarkdown(sessionID)
	if md == "" {
		return ""
	}
	body, err := os.ReadFile(md)
	if err != nil {
		return ""
	}
	return fileCompanionVideoNoteExcerpt(string(body))
}

func fileCompanionCountRunes(s string) int {
	return utf8.RuneCountInString(s)
}

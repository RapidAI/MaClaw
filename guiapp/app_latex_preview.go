package guiapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/llm"
	"github.com/RapidAI/CodeClaw/corelib/tinytex"
)

const latexPreviewEvent = "latex-preview-progress"

var (
	latexPreviewMu     sync.Mutex
	latexPreviewURLSeq uint64
)

// latexPreviewFileURL keeps the reusable file token and adds a per-compile
// query so the preview iframe loads the PDF that was just written.
func latexPreviewFileURL(token string) string {
	n := atomic.AddUint64(&latexPreviewURLSeq, 1)
	return taskResultPreviewHTTPPath + "?t=" + token + "&v=" + strconv.FormatUint(n, 10)
}

// stageLatexPreviewPDF copies the compiled PDF aside. The preview reads the
// copy, so the next xelatex can replace the real PDF while the viewer still
// has the previous one open.
func stageLatexPreviewPDF(src string) (string, error) {
	dir := filepath.Join(os.TempDir(), "maclaw-latex-preview")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst, err := os.CreateTemp(dir, "preview-*.pdf")
	if err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		name := dst.Name()
		_ = dst.Close()
		_ = os.Remove(name)
		return "", err
	}
	_, copyErr := io.Copy(dst, in)
	_ = in.Close()
	closeErr := dst.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(dst.Name())
		if copyErr != nil {
			return "", copyErr
		}
		return "", closeErr
	}
	pruneLatexPreviewPDFs(dir, dst.Name())
	return dst.Name(), nil
}

func pruneLatexPreviewPDFs(dir, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) <= 16 {
		return
	}
	type stagedPDF struct {
		path string
		mod  time.Time
	}
	files := make([]stagedPDF, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".pdf") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		files = append(files, stagedPDF{path: filepath.Join(dir, entry.Name()), mod: info.ModTime()})
	}
	if len(files) <= 16 {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	extra := len(files) - 16
	for _, file := range files {
		if extra <= 0 {
			break
		}
		if file.path == keep {
			continue
		}
		if os.Remove(file.path) == nil {
			extra--
		}
	}
}

// CompileLatexPreview compiles a .tex file with the installed TinyTeX scheme-small,
// installs missing packages, repairs source errors, and returns a PDF preview.
func (a *App) CompileLatexPreview(filePath string) (map[string]interface{}, error) {
	cleaned, err := a.normalizeOpenPathForApp(filePath)
	if err != nil || strings.TrimSpace(cleaned) == "" {
		return nil, fmt.Errorf("文件路径无效")
	}
	if abs, absErr := filepath.Abs(cleaned); absErr == nil {
		cleaned = abs
	}
	ext := strings.ToLower(filepath.Ext(cleaned))
	if ext != ".tex" && ext != ".latex" && ext != ".ltx" {
		return nil, fmt.Errorf("不是 LaTeX 文件")
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("文件不存在")
		}
		return nil, fmt.Errorf("无法打开文件")
	}
	if info.IsDir() {
		return nil, fmt.Errorf("不是文件")
	}

	dist, distErr := tinytex.FindDistRoot(tinytex.DistDir(a.GetDataDir()))
	if distErr != nil || !tinytex.Verified(dist) {
		go a.ensureLatexTinyTeX()
		return map[string]interface{}{
			"ok":      false,
			"phase":   "error",
			"path":    cleaned,
			"error":   "TinyTeX 尚未就绪，正在后台安装 scheme-small",
			"message": "TinyTeX 尚未就绪，正在后台安装 scheme-small",
		}, nil
	}
	engine, err := tinytex.FindEngine(dist)
	if err != nil {
		return map[string]interface{}{
			"ok": false, "phase": "error", "path": cleaned, "error": "未找到 xelatex",
		}, nil
	}
	tlmgr, _ := tinytex.FindTlmgr(dist)
	bibtex, _ := tinytex.FindBin(dist, "bibtex.exe", "bibtex")
	biber, _ := tinytex.FindBin(dist, "biber.exe", "biber")

	latexPreviewMu.Lock()
	defer latexPreviewMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	a.emitLatexPreview(cleaned, "compiling", filepath.Base(cleaned))
	result, previewErr := tinytex.Preview(ctx, cleaned, tinytex.PreviewOptions{
		Engine: engine,
		Tlmgr:  tlmgr,
		Bibtex: bibtex,
		Biber:  biber,
		Run:    runLatexPreviewCommand,
		Install: func(installCtx context.Context, pkg string) error {
			if tlmgr == "" {
				return fmt.Errorf("tlmgr missing")
			}
			a.emitLatexPreview(cleaned, "installing", pkg)
			cctx, cancelInstall := context.WithTimeout(installCtx, 5*time.Minute)
			defer cancelInstall()
			out, code, runErr := tinytex.RunCommand(cctx, tlmgr, filepath.Dir(tlmgr), "install", pkg)
			if runErr != nil {
				return fmt.Errorf("%w: %s", runErr, clipLatexOutput([]byte(out)))
			}
			if code != 0 {
				return fmt.Errorf("exit status %d: %s", code, clipLatexOutput([]byte(out)))
			}
			return nil
		},
		Repair: func(repairCtx context.Context, path, source, excerpt string) (string, bool, error) {
			a.emitLatexPreview(cleaned, "repairing", filepath.Base(path))
			return a.repairLatexSource(repairCtx, path, source, scrubCloudCacheText(excerpt, ""))
		},
		OnPhase: func(phase, message string) {
			a.emitLatexPreview(cleaned, phase, message)
		},
	})
	if previewErr != nil || result.PDFPath == "" {
		shown := cleaned
		if omitCloudWorkspaceAbsPath(cleaned) == "" {
			shown = filepath.Base(cleaned)
		}
		log.Printf("[latex] preview failed file=%s err=%v", shown, previewErr)
		msg := strings.TrimSpace(result.Message)
		if msg == "" && previewErr != nil {
			msg = previewErr.Error()
		}
		a.emitLatexPreview(cleaned, "error", msg)
		return map[string]interface{}{
			"ok":       false,
			"phase":    "error",
			"path":     cleaned,
			"error":    msg,
			"log":      result.Log,
			"repaired": result.Repaired,
		}, nil
	}
	file, openErr := os.Open(result.PDFPath)
	if openErr != nil {
		return map[string]interface{}{"ok": false, "phase": "error", "path": cleaned, "error": "无法打开编译后的 PDF"}, nil
	}
	ok, peekErr := peekPDFHeader(file)
	_ = file.Close()
	if peekErr != nil || !ok {
		return map[string]interface{}{"ok": false, "phase": "error", "path": cleaned, "error": "编译没有生成有效 PDF"}, nil
	}
	tokenPath := result.PDFPath
	if staged, stageErr := stageLatexPreviewPDF(result.PDFPath); stageErr == nil && staged != "" {
		tokenPath = staged
	}
	token := issueTaskResultPreviewToken(tokenPath)
	if token == "" {
		return map[string]interface{}{"ok": false, "phase": "error", "path": cleaned, "error": "无法预览该文档"}, nil
	}
	a.emitLatexPreview(cleaned, "ready", filepath.Base(result.PDFPath))
	return map[string]interface{}{
		"ok":          true,
		"phase":       "ready",
		"path":        cleaned,
		"pdf_path":    result.PDFPath,
		"preview_url": latexPreviewFileURL(token),
		"repaired":    result.Repaired,
		"log":         result.Log,
	}, nil
}

// CompileLatexWorkbenchFile compiles a .tex file inside a local or cloud
// workspace cache. The cloud cache path stays on the backend.
func (a *App) CompileLatexWorkbenchFile(projectPath, relativePath string) (map[string]interface{}, error) {
	abs, root, err := a.latexWorkbenchLocation(projectPath, relativePath)
	if err != nil {
		return nil, err
	}
	if !latexWorkbenchTextExt(abs) {
		return nil, fmt.Errorf("不是 LaTeX 文件")
	}
	releasePush := a.holdCloudWorkspaceLatexPush(root)
	defer releasePush()
	result, err := a.CompileLatexPreview(abs)
	scrubCloudLatexLogFile(abs)
	result = scrubCloudLatexPreview(result, root)
	if err == nil && latexPreviewTouchedDisk(result) {
		a.noteCloudWorkspaceWrite(root)
	}
	return result, err
}

// holdCloudWorkspaceLatexPush keeps the cloud watcher from uploading a compile
// log before scrubCloudLatexLogFile removes the cache path. The returned
// function uploads once if a change was deferred.
func (a *App) holdCloudWorkspaceLatexPush(root string) func() {
	mount := cloudWorkspaceMountForWrite(root)
	if a == nil || mount == nil {
		return func() {}
	}
	mount.mu.Lock()
	mount.latexPushHold++
	if mount.latexPushHold == 1 && mount.pushTimer != nil {
		mount.pushTimer.Stop()
		mount.pushTimer = nil
		mount.latexPushDeferred = true
	}
	mount.mu.Unlock()
	return func() {
		mount.mu.Lock()
		if mount.latexPushHold > 0 {
			mount.latexPushHold--
		}
		deferred := mount.latexPushHold == 0 && mount.latexPushDeferred
		if mount.latexPushHold == 0 {
			mount.latexPushDeferred = false
		}
		mount.mu.Unlock()
		if deferred {
			a.scheduleCloudWorkspacePush(mount)
		}
	}
}

// SaveCodingWorkbenchTextFile writes a LaTeX source back into the workspace
// cache. A mounted cloud workspace then uploads that change.
func (a *App) SaveCodingWorkbenchTextFile(projectPath, relativePath, content string) error {
	abs, root, err := a.latexWorkbenchLocation(projectPath, relativePath)
	if err != nil {
		return err
	}
	if !latexWorkbenchTextExt(abs) {
		return fmt.Errorf("只能保存 LaTeX 源文件")
	}
	if strings.ContainsRune(content, 0) {
		return fmt.Errorf("不能保存二进制内容")
	}
	if utf8.RuneCountInString(content) > codingWorkbenchBrowserMaxRunes || len(content) > codingWorkbenchBrowserMaxReadBytes {
		return fmt.Errorf("文件过大，无法保存")
	}
	if err := writeLatexSourceFile(abs, content); err != nil {
		return err
	}
	a.noteCloudWorkspaceWrite(root)
	return nil
}

func (a *App) latexWorkbenchLocation(projectPath, relativePath string) (string, string, error) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if a == nil || projectPath == "" {
		return "", "", fmt.Errorf("project path is required")
	}
	if a.codingWorkbenchRejectsLocalOpen(projectPath) {
		return "", "", fmt.Errorf("云端工作区未挂载到本机，无法编辑")
	}
	root, err := codingWorkbenchBrowserLocalRoot(a, projectPath)
	if err != nil {
		return "", "", hideCloudPathErr(err)
	}
	if err := rejectReadOnlyCloudWorkspace(root); err != nil {
		return "", "", err
	}
	abs, err := codingWorkbenchLocalFileAbsPath(root, relativePath)
	if err != nil {
		return "", "", hideCloudPathErr(err)
	}
	return abs, root, nil
}

func hideCloudPathErr(err error) error {
	if err == nil {
		return nil
	}
	raw := err.Error()
	folded := strings.ToLower(strings.ReplaceAll(raw, "\\", "/"))
	if !cloudCacheText(folded) {
		return err
	}
	cleaned := scrubCloudCacheText(raw, "")
	if cleaned == "" {
		return fmt.Errorf("无法打开文件")
	}
	return errors.New(cleaned)
}

func writeLatexSourceFile(abs, content string) error {
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("无法打开文件")
	}
	if err := tinytex.WriteAtomic(abs, []byte(content)); err != nil {
		return fmt.Errorf("保存失败")
	}
	_ = os.Chmod(abs, info.Mode().Perm())
	return nil
}

func latexPreviewTouchedDisk(result map[string]interface{}) bool {
	if result == nil {
		return false
	}
	if result["ok"] == true {
		return true
	}
	if repaired, _ := result["repaired"].(bool); repaired {
		return true
	}
	logText, _ := result["log"].(string)
	return strings.TrimSpace(logText) != ""
}

func latexWorkbenchTextExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tex", ".latex", ".ltx":
		return true
	default:
		return false
	}
}

func scrubCloudLatexPreview(result map[string]interface{}, root string) map[string]interface{} {
	if result == nil {
		return result
	}
	for _, key := range []string{"path", "pdf_path"} {
		value, _ := result[key].(string)
		if value != "" && omitCloudWorkspaceAbsPath(value) == "" {
			delete(result, key)
		}
	}
	for _, key := range []string{"error", "message", "log"} {
		value, ok := result[key].(string)
		if !ok || value == "" {
			continue
		}
		result[key] = scrubCloudCacheText(value, root)
	}
	return result
}

func scrubCloudLatexLogFile(texPath string) {
	if omitCloudWorkspaceAbsPath(texPath) != "" {
		return
	}
	targets := []string{texPath}
	if main, err := tinytex.ResolveMain(texPath); err == nil && main != "" {
		targets = append(targets, main)
	}
	seen := map[string]struct{}{}
	for _, target := range targets {
		stem := strings.TrimSuffix(target, filepath.Ext(target))
		for _, ext := range []string{".log", ".blg", ".fls"} {
			path := stem + ext
			if _, ok := seen[path]; ok {
				continue
			}
			seen[path] = struct{}{}
			raw, err := os.ReadFile(path)
			if err != nil || len(raw) == 0 {
				continue
			}
			cleaned := scrubCloudCacheText(string(raw), "")
			if cleaned == string(raw) {
				continue
			}
			_ = tinytex.WriteAtomic(path, []byte(cleaned))
		}
	}
}

func scrubCloudCacheText(text, root string) string {
	if text == "" {
		return ""
	}
	normalized := stripPathPrefix(text, strings.ReplaceAll(root, "\\", "/"))
	var b strings.Builder
	b.Grow(len(normalized))
	rest := normalized
	for n := 0; n < 8192 && rest != ""; n++ {
		idx, marker := nextCloudCacheMarker(rest)
		if idx < 0 {
			b.WriteString(rest)
			return b.String()
		}
		start, end := cloudCacheSpan(rest, idx, marker)
		rel := cloudCacheTail(rest[idx+len(marker):end], marker)
		b.WriteString(rest[:start])
		rest = rest[end:]
		if _, again := nextCloudCacheMarker(rel); again != "" {
			rest = rel + rest
			continue
		}
		b.WriteString(rel)
	}
	b.WriteString(rest)
	return b.String()
}

func cloudCacheSpan(s string, idx int, marker string) (start, end int) {
	start = idx
	for start > 0 {
		if cloudPathDelimiter(s[start-1]) {
			break
		}
		start--
	}
	end = idx + len(marker)
	for end < len(s) {
		next := s[end]
		if cloudPathDelimiter(next) || next == ':' {
			break
		}
		end++
	}
	return start, end
}

func cloudCacheTail(rel, marker string) string {
	parts := strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' })
	drop := 2
	if strings.Contains(strings.ToLower(marker), "readonly") {
		drop = 3
	}
	if len(parts) > drop {
		return strings.Join(parts[drop:], "/")
	}
	if len(parts) == 0 {
		return "."
	}
	return strings.Join(parts, "/")
}

func cloudPathDelimiter(b byte) bool {
	switch b {
	case '\n', '\t', '"', '\'', '(', ')':
		return true
	default:
		return false
	}
}

func cloudCacheText(folded string) bool {
	return strings.Contains(folded, "/cloud-workspaces/") || strings.Contains(folded, "/cloud-workspaces-readonly/")
}

func nextCloudCacheMarker(s string) (int, string) {
	markers := [4]string{
		"/cloud-workspaces-readonly/",
		"\\cloud-workspaces-readonly\\",
		"/cloud-workspaces/",
		"\\cloud-workspaces\\",
	}
	best := -1
	found := ""
	for _, marker := range markers {
		idx := indexFold(s, marker)
		if idx >= 0 && (best < 0 || idx < best) {
			best = idx
			found = marker
		}
	}
	return best, found
}

func indexFold(s, needle string) int {
	if needle == "" || len(s) < len(needle) {
		return -1
	}
	last := len(s) - len(needle)
	for i := 0; i <= last; i++ {
		if strings.EqualFold(s[i:i+len(needle)], needle) {
			return i
		}
	}
	return -1
}

func stripPathPrefix(text, root string) string {
	root = strings.TrimRight(strings.ReplaceAll(root, "\\", "/"), "/")
	if text == "" || root == "" {
		return text
	}
	if len(strings.ToLower(text)) != len(text) || len(strings.ToLower(root)) != len(root) {
		return text
	}
	lowerText := strings.ToLower(text)
	lowerRoot := strings.ToLower(root)
	var b strings.Builder
	i := 0
	for i < len(text) {
		idx := strings.Index(lowerText[i:], lowerRoot)
		if idx < 0 {
			b.WriteString(text[i:])
			break
		}
		idx += i
		end := idx + len(lowerRoot)
		if end < len(text) && !pathPrefixBoundary(text[end]) {
			b.WriteString(text[i : idx+1])
			i = idx + 1
			continue
		}
		b.WriteString(text[i:idx])
		i = end
	}
	return b.String()
}

func pathPrefixBoundary(next byte) bool {
	switch next {
	case '/', ' ', '\n', '\t', '"', '\'', ')', ':':
		return true
	default:
		return false
	}
}

func cloudWorkspaceMountForWrite(root string) *cloudWorkspaceHeldMount {
	current := normalizeProjectSessionPath(root)
	for current != "" {
		if mount := lookupHeldCloudWorkspace(lookupCloudWorkspaceIDByLocalPath(current)); mount != nil {
			return mount
		}
		parent := normalizeProjectSessionPath(filepath.Dir(current))
		if parent == current {
			break
		}
		current = parent
	}
	return nil
}

func (a *App) noteCloudWorkspaceWrite(root string) {
	if a == nil {
		return
	}
	if mount := cloudWorkspaceMountForWrite(root); mount != nil {
		a.scheduleCloudWorkspacePush(mount)
	}
}

func rejectReadOnlyCloudWorkspace(root string) error {
	mount := cloudWorkspaceMountForWrite(root)
	if mount == nil {
		return nil
	}
	mount.mu.Lock()
	readOnly := mount.ReadOnly
	mount.mu.Unlock()
	if readOnly {
		return fmt.Errorf("云端工作区是只读的，无法修改或编译")
	}
	return nil
}

func latexPreviewPublicPath(path string) string {
	if omitCloudWorkspaceAbsPath(path) != "" {
		return path
	}
	cleaned := scrubCloudCacheText(path, "")
	folded := strings.ToLower(strings.ReplaceAll(cleaned, "\\", "/"))
	if cleaned == "" || cleaned == "." || cloudCacheText(folded) {
		return filepath.Base(path)
	}
	return cleaned
}

func (a *App) emitLatexPreview(path, phase, message string) {
	if a == nil {
		return
	}
	publicPath := latexPreviewPublicPath(path)
	a.emitEvent(latexPreviewEvent, map[string]interface{}{
		"path":    publicPath,
		"phase":   phase,
		"message": scrubCloudCacheText(message, filepath.Dir(path)),
	})
}

// latexRepairSlotBudget is how long one repair may occupy a foreground
// scheduler slot. It is not a deadline on the model body. Background LLM
// runs only while activeFG == 0, so holding the slot until the compile
// context ends freezes every background request. AwaitResponse releases the
// slot at this budget and keeps reading until that context ends. 75s is the
// slot hold meeting-minutes already passes to this helper. Releasing at
// once would drop the concurrency account and pile upstream calls on top
// of each other.
const latexRepairSlotBudget = 75 * time.Second

func latexRepairPrompt(path, source, excerpt string) (string, error) {
	line, ok := tinytex.ErrorLine(excerpt)
	if !ok {
		return "", fmt.Errorf("编译日志里没有文件行号")
	}
	_, _, window, ok := tinytex.FormatRepairWindow(source, line)
	if !ok {
		return "", fmt.Errorf("无法定位出错行")
	}
	return "你是 LaTeX 编译修复器。编译器在第 " + strconv.Itoa(line) + " 行失败。\n" +
		"下面每行以源文件真实行号开头。只返回一个替换块，不要解释，不要代码围栏：\n" +
		"@@@ 起始行 结束行\n" +
		"替换后的正文\n" +
		"@@@\n" +
		"起始行和结束行是闭区间，必须包含第 " + strconv.Itoa(line) + " 行，并且落在给出的行号里。正文不要行号。\n\n" +
		"文件: " + filepath.Base(path) + "\n\n编译错误:\n" + excerpt + "\n\n源码:\n" + window, nil
}

func (a *App) repairLatexSource(ctx context.Context, path, source, excerpt string) (string, bool, error) {
	if a == nil {
		return "", false, fmt.Errorf("app not available")
	}
	cfg := a.GetMaclawLLMConfig()
	if strings.TrimSpace(cfg.URL) == "" && strings.TrimSpace(cfg.Model) == "" {
		return "", false, fmt.Errorf("未配置大模型，无法自动修复")
	}
	prompt, err := latexRepairPrompt(path, source, excerpt)
	if err != nil {
		return "", false, err
	}
	messages := []interface{}{
		map[string]interface{}{
			"role":    "user",
			"content": prompt,
		},
	}
	// The client has no deadline. latexRepairSlotBudget only releases the
	// foreground slot; AwaitResponse waits for the body until the compile
	// context ends. Returning a budget error here would make
	// CompileLatexPreview cancel that context and abort the read.
	client := &http.Client{}
	reqCtx := llm.WithRequestTrace(ctx, llm.RequestTrace{Caller: "latex-preview", OwnerID: "latex-preview"})
	resp, err := doSimpleLLMRequestWithOptions(reqCtx, cfg, messages, client, latexRepairSlotBudget, simpleLLMRequestOptions{AwaitResponse: true})
	if err != nil {
		return "", false, err
	}
	// An empty or unusable reply consumes a repair slot and asks again.
	// Returning an error would skip the second slot. The block is read from
	// the provider fields before and after the chat display filter, because
	// that filter can delete the only copy.
	for _, candidate := range latexRepairCandidateTexts(resp) {
		body, ok := tinytex.ApplyRepairReply(source, excerpt, candidate)
		if ok {
			return body, true, nil
		}
	}
	return "", false, nil
}

// latexRepairCandidateTexts is the provider message in the order the edit
// protocol reads it. The stripped visible answer comes first. Raw content and
// reasoning follow when the visible answer has no usable block.
func latexRepairCandidateTexts(resp *llmSimpleResponse) []string {
	if resp == nil {
		return nil
	}
	var out []string
	add := func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		for _, prev := range out {
			if prev == text {
				return
			}
		}
		out = append(out, text)
	}
	if strings.TrimSpace(resp.RawContent) != "" {
		add(stripThinkTags(resp.RawContent))
		add(resp.RawContent)
	}
	if strings.TrimSpace(resp.ReasoningContent) != "" {
		add(stripThinkTags(resp.ReasoningContent))
		add(resp.ReasoningContent)
	}
	add(resp.Content)
	return out
}

func runLatexPreviewCommand(ctx context.Context, bin, dir string, args ...string) (string, int, error) {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return tinytex.RunCommand(cctx, bin, dir, args...)
}

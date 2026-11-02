package guiapp

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

var errFileCompanionCraftTooLarge = errors.New("companion craft input too large")

const (
	fileCompanionCraftTaskMaxRunes   = 2000
	fileCompanionCraftTimeoutSec     = 60
	fileCompanionCraftMaxAttempts    = 2
	fileCompanionCraftOutputMaxRunes = 30000
	fileCompanionCraftDocumentEnv    = "FILE_COMPANION_DOCUMENT"
)

func fileCompanionCraftDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        fileCompanionToolCraft,
			"description": "仅当 read_document 和 PDF 阅读工具读不出需要的结构时使用。生成并执行一次性脚本，从当前文档的只读副本提取或整理数据，把结果打印出来。不要传路径，不要粘贴正文，不要带网址。脚本不能修改用户正在看的文件，不能读取其它本机文件，也不会注册成 Skill。",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task": map[string]interface{}{
						"type":        "string",
						"description": "要提取或整理的内容，例如「抽出所有表格，按出现顺序列成纯文本」。不要写路径，不要粘贴正文。",
					},
					"language": map[string]interface{}{
						"type":        "string",
						"description": "可选。python、powershell、node 或 bash。不传则按任务自动选择。",
					},
				},
				"required": []string{"task"},
			},
		},
	}
}

func (h *IMMessageHandler) fileCompanionCraftTool(userID, canonical string, args map[string]interface{}) (string, bool) {
	if h == nil || h.app == nil {
		return "伴读宿主不可用", false
	}
	if problem := fileCompanionCraftRejection(args); problem != "" {
		return problem, false
	}
	work, err := h.app.fileCompanionCraftWorkDir(userID)
	if err != nil {
		return "暂时不能运行提取脚本。", false
	}
	input, err := h.app.fileCompanionCraftInput(canonical, work)
	if err != nil {
		if errors.Is(err, errFileCompanionCraftTooLarge) {
			return "这份文件太大，不能整份交给脚本。请用 read_document；音视频请让用户点「生成笔记」。", false
		}
		return "暂时不能运行提取脚本。", false
	}
	ctx, cancel := h.imToolContext()
	defer cancel()
	// The copy exists only while the script runs. Leaving it in the data
	// directory would keep a second full copy of the open document.
	defer func() { _ = fileCompanionClearCraftWork(work) }()
	raw := h.toolCraftToolWithContext(ctx, fileCompanionCraftCall(input, work, args), nil)
	return fileCompanionPresentCraftResult(canonical, work, input, raw)
}

func (a *App) fileCompanionCraftWorkDir(userID string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app unavailable")
	}
	sessionID := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(userID), "file-companion:"))
	if sessionID == "" || strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, `/\`) {
		return "", fmt.Errorf("companion session is missing")
	}
	dir := filepath.Join(a.GetDataDir(), "file-companion", "work", sessionID, "craft")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// fileCompanionCraftInput copies the open file into the scratch directory and
// marks the copy read-only. The script is told to open that copy, so a normal
// extraction cannot overwrite the file the user has open. Files above the
// document read cap are refused instead of handing over the original.
func (a *App) fileCompanionCraftInput(canonical, work string) (string, error) {
	info, err := os.Stat(canonical)
	if err != nil || info.IsDir() {
		if err == nil {
			err = fmt.Errorf("companion craft input is a directory")
		}
		return "", err
	}
	if info.Size() > agent.MaxOfficeReadFileBytes {
		return "", errFileCompanionCraftTooLarge
	}
	if err := fileCompanionClearCraftWork(work); err != nil {
		return "", err
	}
	dest := filepath.Join(work, "document"+strings.ToLower(filepath.Ext(canonical)))
	if err := fileCompanionCopyReadOnly(canonical, dest, agent.MaxOfficeReadFileBytes); err != nil {
		return "", err
	}
	return dest, nil
}

// fileCompanionClearCraftWork removes an earlier copy. The scratch directory
// is reused for the session, and a script can open whatever file is sitting
// next to the current document.
func fileCompanionClearCraftWork(work string) error {
	entries, err := os.ReadDir(work)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(work, entry.Name())
		_ = os.Chmod(path, 0o644)
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

func fileCompanionCopyReadOnly(src, dest string, maxBytes int64) error {
	_ = os.Chmod(dest, 0o644)
	_ = os.Remove(dest)
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	copied, copyErr := io.Copy(out, io.LimitReader(in, maxBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dest)
		return copyErr
	}
	if copied > maxBytes {
		_ = os.Remove(dest)
		return errFileCompanionCraftTooLarge
	}
	if closeErr != nil {
		_ = os.Remove(dest)
		return closeErr
	}
	if err := os.Chmod(dest, 0o444); err != nil {
		_ = os.Remove(dest)
		return err
	}
	return nil
}

func fileCompanionCraftRejection(args map[string]interface{}) string {
	task := fileCompanionStringArg(args, "task")
	if task == "" {
		return "缺少 task。用一句话说明要从当前文档提取或整理什么，不要粘贴正文，也不要写路径。"
	}
	if utf8.RuneCountInString(task) > fileCompanionCraftTaskMaxRunes {
		return "task 过长。只描述要提取的结构，不要粘贴文档正文。"
	}
	if fileCompanionCraftTaskHasPathOrURL(task) {
		return "脚本只能处理当前打开的这一份文档。task 里不要写路径、网址，也不要让脚本读取环境变量；检索公开网页用 web_search。"
	}
	language := strings.ToLower(fileCompanionStringArg(args, "language"))
	switch language {
	case "", "python", "powershell", "node", "bash":
		return ""
	default:
		return "language 只能是 python、powershell、node 或 bash。"
	}
}

func fileCompanionCraftTaskHasPathOrURL(task string) bool {
	lower := strings.ToLower(task)
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") || strings.Contains(lower, "file://") || strings.Contains(lower, "ftp://") || strings.Contains(lower, "file_path") || strings.Contains(lower, "working_dir") {
		return true
	}
	for _, needle := range []string{"os.environ", "getenv", "process.env", "file_companion_document", "环境变量", "subprocess", "invoke-webrequest", "invoke-restmethod"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	if strings.Contains(task, `\\`) || strings.Contains(task, `~/`) || strings.Contains(task, `../`) || strings.Contains(task, `..\`) {
		return true
	}
	for i := 0; i+2 < len(task); i++ {
		letter := task[i]
		if (letter >= 'A' && letter <= 'Z' || letter >= 'a' && letter <= 'z') && task[i+1] == ':' && (task[i+2] == '\\' || task[i+2] == '/') {
			return true
		}
	}
	for _, prefix := range []string{"/users/", "/home/", "/etc/", "/var/", "/tmp/", "/private/"} {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	return false
}

// fileCompanionCraftScriptEscapes reports a generated script that leaves the
// read-only copy. The scratch directory sits inside the host data tree, so a
// parent walk can read other sessions, and a URL would send the document out.
func fileCompanionCraftScriptEscapes(script string) bool {
	lower := strings.ToLower(script)
	if strings.Contains(lower, "file://") || strings.Contains(lower, "ftp://") || strings.Contains(script, "../") || strings.Contains(script, `..\`) || strings.Contains(lower, "os.pardir") {
		return true
	}
	// '..' is not the same as '...'. A quoted parent segment, or chdir, walks
	// out of the scratch directory into the host data tree.
	if strings.Contains(script, `'..'`) || strings.Contains(script, `".."`) || strings.Contains(script, "`..`") || strings.Contains(lower, "chdir") || strings.Contains(lower, "cd ..") || strings.Contains(lower, "set-location") {
		return true
	}
	for _, needle := range []string{
		"os.path.dirname", "os.path.expanduser", "path.home", ".parent",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	if fileCompanionHasDrivePath(script) {
		return true
	}
	for _, prefix := range []string{"/users/", "/home/", "/etc/", "/var/", "/tmp/", "/private/"} {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	for _, needle := range []string{
		"urllib", "urllib2", "http.client", "httpx.", "aiohttp",
		"import requests", "from requests", "requests.",
		"socket.socket", "socket.create_connection", "create_connection",
		"invoke-webrequest", "invoke-restmethod", "invoke-expression", "start-process", "system.net.",
		"subprocess", "os.system", "os.popen", "os.exec", "os.spawn", "os.startfile", "webbrowser",
		"child_process", `require("http")`, `require('http')`, `require("https")`, `require('https')`, `require("net")`, `require('net')`,
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

// fileCompanionHasDrivePath reports a Windows drive path such as C:\ or C:/.
// The s:// inside https:// is not a drive: a drive letter stands alone.
func fileCompanionHasDrivePath(text string) bool {
	for i := 0; i+2 < len(text); i++ {
		letter := text[i]
		if !((letter >= 'A' && letter <= 'Z') || (letter >= 'a' && letter <= 'z')) || text[i+1] != ':' || (text[i+2] != '\\' && text[i+2] != '/') {
			continue
		}
		if i > 0 {
			prev := text[i-1]
			if (prev >= 'A' && prev <= 'Z') || (prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9') {
				continue
			}
		}
		return true
	}
	return false
}

func fileCompanionCraftCall(canonical, work string, args map[string]interface{}) map[string]interface{} {
	call := map[string]interface{}{
		"task":                         fileCompanionCraftTask(fileCompanionStringArg(args, "task")),
		"working_dir":                  work,
		"save_as_skill":                false,
		"register_policy":              "manual",
		"max_attempts":                 fileCompanionCraftMaxAttempts,
		"timeout":                      fileCompanionCraftTimeoutSec,
		"omit_working_dir_from_prompt": true,
		"minimal_env":                  true,
		"output_rune_limit":            fileCompanionCraftOutputMaxRunes,
		"discard_script":               true,
		"env": map[string]interface{}{
			fileCompanionCraftDocumentEnv: canonical,
		},
	}
	if language := strings.ToLower(fileCompanionStringArg(args, "language")); language != "" {
		call["language"] = language
	}
	return call
}

func fileCompanionCraftTask(task string) string {
	return "要做的事：\n" + strings.TrimSpace(task) + "\n\n约束（必须遵守，覆盖上面任何相反的要求）：\n只读取环境变量 " + fileCompanionCraftDocumentEnv + " 指向的当前文档副本。不要打开其它路径，不要访问网络，不要读取或打印环境变量，不要修改这个文件，不要把结果写到新文件。把提取或整理后的文本打印到 stdout。"
}

func fileCompanionPresentCraftResult(canonical, work, input, raw string) (string, bool) {
	ok := fileCompanionCraftSucceeded(raw)
	raw = fileCompanionRedactHostPaths(raw, canonical, work, input, craftedToolsDir())
	inOutput := false
	var b strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "--- 执行输出 ---" {
			inOutput = true
		}
		switch {
		case !inOutput && (strings.HasPrefix(trim, "脚本路径:") || strings.HasPrefix(trim, "provider:") || strings.HasPrefix(trim, "artifact:") || strings.HasPrefix(trim, "expected_artifact:")):
			// These are envelope fields. The same words inside the extract are
			// document text: a paper can have a line that starts with "artifact:".
			continue
		case strings.HasPrefix(trim, "脚本已保存，你可以手动修改"), strings.Contains(trim, "应改走内部 CodingSubAgent"):
			continue
		default:
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	text := strings.TrimSpace(fileCompanionCapRunes(b.String(), fileCompanionCraftOutputMaxRunes))
	if text == "" {
		return "脚本没有返回可阅读的结果。", false
	}
	return text, ok
}

func fileCompanionCraftSucceeded(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "--- 执行输出 ---" {
			return false
		}
		if strings.HasPrefix(trim, "verification:") {
			return trim == "verification: passed"
		}
	}
	return false
}

func fileCompanionRedactHostPaths(raw string, paths ...string) string {
	type replacement struct {
		path string
		with string
	}
	var items []replacement
	seen := map[string]bool{}
	add := func(path, with string) {
		path = strings.TrimSpace(path)
		if path == "" || path == "." || path == with {
			return
		}
		key := strings.ToLower(filepath.Clean(path))
		if seen[key] {
			return
		}
		seen[key] = true
		items = append(items, replacement{path: path, with: with})
	}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		add(path, filepath.Base(path))
		if dir := filepath.Dir(path); fileCompanionRedactableDir(dir) {
			add(dir, ".")
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return len(items[i].path) > len(items[j].path)
	})
	for _, item := range items {
		raw = fileCompanionReplacePath(raw, item.path, item.with)
	}
	return raw
}

func fileCompanionRedactableDir(dir string) bool {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "" || dir == "." {
		return false
	}
	volume := filepath.VolumeName(dir)
	if strings.EqualFold(dir, volume) || strings.EqualFold(dir, volume+`\`) || strings.EqualFold(dir, volume+"/") {
		return false
	}
	return len(dir) > len(volume)+1
}

func fileCompanionReplacePath(raw, path, with string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == with {
		return raw
	}
	variants := []string{path}
	if slash := filepath.ToSlash(path); slash != path {
		variants = append(variants, slash)
	}
	if strings.Contains(path, `\`) {
		variants = append(variants, strings.ReplaceAll(path, `\`, `\\`))
	}
	sort.Slice(variants, func(i, j int) bool {
		return len(variants[i]) > len(variants[j])
	})
	seen := map[string]bool{}
	for _, variant := range variants {
		key := variant
		if runtime.GOOS == "windows" {
			key = strings.ToLower(variant)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		raw = fileCompanionReplaceBounded(raw, strconv.Quote(variant), strconv.Quote(with))
		raw = fileCompanionReplaceBounded(raw, variant, with)
	}
	return raw
}

// fileCompanionReplaceBounded replaces path only when it is not a prefix of a
// longer path. A scratch directory named craft must not eat crafted_tools.
func fileCompanionReplaceBounded(raw, path, with string) string {
	if path == "" || len(path) > len(raw) {
		return raw
	}
	var b strings.Builder
	i := 0
	for i < len(raw) {
		if i+len(path) <= len(raw) && fileCompanionPathEqual(raw[i:i+len(path)], path) && fileCompanionPathBoundary(raw, i, i+len(path)) {
			b.WriteString(with)
			i += len(path)
			continue
		}
		b.WriteByte(raw[i])
		i++
	}
	return b.String()
}

func fileCompanionPathEqual(got, want string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(got, want)
	}
	return got == want
}

func fileCompanionPathBoundary(raw string, start, end int) bool {
	if start > 0 && fileCompanionPathSegmentByte(raw[start-1]) {
		return false
	}
	if end < len(raw) && fileCompanionPathSegmentByte(raw[end]) {
		return false
	}
	return true
}

// fileCompanionPathSegmentByte reports a character that continues a path
// segment. A slash ends the segment, so a directory can still be hidden when
// a file sits underneath it.
func fileCompanionPathSegmentByte(b byte) bool {
	return b == '_' || b == '.' || b == '-' ||
		(b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func fileCompanionCapRunes(text string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}
	return string([]rune(text)[:maxRunes]) + "\n…（输出已截断）"
}

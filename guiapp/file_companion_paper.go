package guiapp

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/docgen"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

const (
	fileCompanionPurposePaper = "paper"
	fileCompanionPaperLabel   = "论文解读"
	// fileCompanionPaperMarker starts the button-turn comment. The comment holds
	// the page-score words and contains no ASCII word, so "paper" cannot outrank
	// a dataset page. A person typing the label does not have this comment.
	fileCompanionPaperMarker = "<!--§§"
	// fileCompanionPaperScoreQuery sits before the path marker, inside the comment.
	fileCompanionPaperScoreQuery   = "方法 实验 数据集 下载 开源 代码 dataset experiment github benchmark"
	fileCompanionPaperSearchBudget = 25 * time.Second
)

// paperUngivenURL matches an http(s) address a model wrote next to 「未给出」.
var paperUngivenURL = regexp.MustCompile(`https?://[^\s<>\]）)]+`)

// fileCompanionPaperLookupHook replaces the public search in tests.
var fileCompanionPaperLookupHook func(query string) (string, string)

func fileCompanionIsPaperPurpose(purpose string) bool {
	return strings.EqualFold(strings.TrimSpace(purpose), fileCompanionPurposePaper)
}

// fileCompanionOutgoing builds the model prompt for one companion turn.
// A paper turn never forwards the chat draft or the selection.
func fileCompanionOutgoing(canonical string, req FileCompanionTurnRequest) (prompt, turn, selection string, prefer int) {
	name := filepath.Base(canonical)
	if fileCompanionIsPaperPurpose(req.Purpose) {
		return fileCompanionPaperPrompt(name), fileCompanionPaperUserTurn(canonical), "", -1
	}
	prefer = -1
	selection = req.Selection
	if req.SelectionStart != nil && *req.SelectionStart >= 0 {
		prefer = *req.SelectionStart
	}
	return fileCompanionPrompt(name, filepath.Ext(canonical)), fileCompanionUserTurn(canonical, strings.TrimSpace(req.Text), selection), selection, prefer
}

func fileCompanionPaperPrompt(name string) string {
	return "你是文件伴读。只讨论当前打开的这一份文档：" + name + "。不要使用工具，不要编造文中没有的地址、仓库或指标。按用户消息里的解读要求输出正文。这篇解读的 PDF 由宿主根据正文生成。数据集和代码若文中没有 URL，写成「论文未给出」，并给出检索词。"
}

func fileCompanionPaperInstruction() string {
	return strings.TrimSpace(`
请只根据当前打开的这一份文档做论文解读。不要使用工具，不要编造文中没有的事实。
宿主会把你的正文做成 PDF，并在聊天里显示「已生成论文解读」。正文只写下面的小节，不要在末尾再写已经生成了 PDF。
文中没有的地址、仓库、指标写成「论文未给出」。
用 Markdown，按下面的二级标题顺序写，不要增删标题：

## 方法原理
用论文自己的说法说明方法如何工作。重要公式单独写成一行，放在 $$ 与 $$ 之间。

## 方法本质
用一两句人话说明这个方法到底在做什么。例如：使用 CNN 提取空间特征，再交给后面的模块做分类。不要堆术语。

## 主要原理图及说明
指出文中最主要的原理图，写出图号和图题。用文字说明图里有哪些模块、数据怎么流动、图注在说什么。不要声称已经把原图嵌进回答，原图由程序另附。

## 实验方法
说明实验怎么做、和谁对比、用什么指标。文中没有的写成「论文未给出」。

## 数据集
每个数据集单独写成：
- 名称：
- 论文中的下载地址：有则原样写出论文里的完整 URL；没有则只写「论文未给出」。不要写搜索引擎的结果页
- 检索词：只有地址是「论文未给出」时才写，用可搜索的短句，例如「CIFAR-10 dataset download」；地址已经给出时写「不需要」

## 是否开源
- 论文中的代码地址：有则原样写出论文里的完整 URL；没有则只写「论文未给出」。不要写搜索引擎的结果页
- 检索词：只有地址是「论文未给出」时才写，例如「论文英文名 github」；已经给出时写「不需要」

## 论文质量评价
从问题是否清楚、方法是否说得清、实验是否支撑结论来评价。缺证据就写缺什么。

不要输出 BASELINE_METRICS，不要再要求下载这份 PDF。
`)
}

func fileCompanionPaperUserTurn(path string) string {
	var b strings.Builder
	b.WriteString(fileCompanionPaperLabel)
	b.WriteString("\n\n")
	b.WriteString(fileCompanionPaperMarker)
	b.WriteString(" ")
	b.WriteString(fileCompanionPaperScoreQuery)
	b.WriteString("-->\n\n")
	b.WriteString(agent.FilePathPromptPrefix)
	b.WriteString("\n")
	b.WriteString(path)
	b.WriteString("\n\n")
	b.WriteString(fileCompanionPaperInstruction())
	return b.String()
}

func (a *App) fileCompanionDeliverPaper(canonical, sessionID, userID, requestID string, resp *IMAgentResponse) error {
	if resp == nil {
		logFileCompanion("turn end session=%s empty", sessionID)
		return errors.New("empty response")
	}
	if strings.TrimSpace(resp.Error) != "" {
		logFileCompanion("turn end session=%s error", sessionID)
		a.emitFileCompanionTurn(requestID, userID, resp)
		return errors.New(resp.Error)
	}
	reply := strings.TrimSpace(resp.Text)
	if reply == "" {
		resp.Error = "没有得到解读内容"
		logFileCompanion("turn end session=%s error", sessionID)
		a.emitFileCompanionTurn(requestID, userID, resp)
		return errors.New(resp.Error)
	}
	parent := context.Background()
	if a != nil && a.ctx != nil {
		parent = a.ctx
	}
	searchCtx, cancel := context.WithTimeout(parent, fileCompanionPaperSearchBudget)
	defer cancel()
	supplemented, missed := fileCompanionSupplementPaperText(reply, fileCompanionPaperSourceName(canonical), func(query string) (string, string) {
		return a.fileCompanionPaperLookupCtx(searchCtx, query)
	})
	if missed {
		logFileCompanion("paper search failed session=%s", sessionID)
	}
	resp.Text = supplemented
	pdfPath, err := a.fileCompanionWritePaperPDF(canonical, sessionID, supplemented)
	if err != nil {
		resp.Error = err.Error()
		resp.LocalFilePath = ""
		logFileCompanion("turn end session=%s paper-pdf-failed", sessionID)
		a.emitFileCompanionTurn(requestID, userID, resp)
		return err
	}
	resp.LocalFilePath = pdfPath
	a.emitFileCompanionTurn(requestID, userID, resp)
	logFileCompanion("turn end session=%s paper-pdf", sessionID)
	return nil
}

func (a *App) fileCompanionPaperLookupCtx(ctx context.Context, query string) (string, string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", ""
	}
	if fileCompanionPaperLookupHook != nil {
		return fileCompanionPaperLookupHook(query)
	}
	if a == nil || a.imHandler == nil || ctx == nil {
		return "", ""
	}
	response, err := websearch.SearchWithStrategyCtx(ctx, query, 5, a.imHandler.getWebSearchStrategy())
	if err != nil {
		return "", ""
	}
	return firstPaperSearchPage(response.Results)
}

func (a *App) fileCompanionWritePaperPDF(canonical, sessionID, markdown string) (string, error) {
	if a == nil {
		return "", errors.New("app unavailable")
	}
	output, err := fileCompanionPaperOutput(a.GetDataDir(), sessionID, canonical)
	if err != nil {
		return "", err
	}
	sourceAbs, _ := filepath.Abs(canonical)
	outAbs, _ := filepath.Abs(output)
	if sourceAbs != "" && strings.EqualFold(filepath.Clean(sourceAbs), filepath.Clean(outAbs)) {
		return "", errors.New("不能覆盖当前文件")
	}
	markdown = fileCompanionEmbedPaperFigures(canonical, filepath.Dir(output), markdown, sessionID)
	written, err := docgen.GenerateToFile(docgen.Spec{
		Title:          fileCompanionPaperLabel,
		Subtitle:       filepath.Base(canonical),
		Content:        markdown,
		FooterHint:     "由 MaClaw 伴读生成。网上核对的地址标有（可能），不是论文原文。",
		Brand:          "MaClaw",
		FileNamePrefix: "paper",
		PaperSize:      "A4",
		Colorful:       true,
	}, output)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(written), "latest.txt"), []byte(written), 0o644); err != nil {
		logFileCompanion("paper record failed session=%s", sessionID)
	} else {
		a.fileCompanionRecordPaperTurn(sessionID)
	}
	return written, nil
}

func fileCompanionPaperOutput(dataDir, sessionID, source string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, `/\`) {
		return "", errors.New("缺少会话")
	}
	return filepath.Join(dataDir, "file-companion", "results", sessionID, fileCompanionPaperFileName(source)), nil
}

func fileCompanionPaperSourceName(path string) string {
	base := strings.TrimSuffix(filepath.Base(strings.TrimSpace(path)), filepath.Ext(path))
	return strings.TrimSpace(base)
}

func fileCompanionPaperFileName(source string) string {
	base := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, fileCompanionPaperSourceName(source))
	base = strings.Trim(base, " .")
	rs := []rune(base)
	if len(rs) > 60 {
		base = strings.TrimRight(string(rs[:60]), " .")
	}
	if strings.TrimSpace(base) == "" {
		base = "document"
	}
	return base + "-论文解读.pdf"
}

type paperGap struct {
	kind  string
	name  string
	query string
}

// fileCompanionSupplementPaperText appends 网上核对 for addresses the paper
// itself did not give. lookup is called only for those gaps. A returned page
// is unwrapped to its target and marked （可能）. A search-engine page is not
// an address. A URL the paper already states is left unchanged.
func fileCompanionSupplementPaperText(markdown, sourceName string, lookup func(string) (string, string)) (string, bool) {
	markdown = fileCompanionRedactUngivenURLs(markdown)
	markdown = fileCompanionRedactSearchPageURLs(markdown)
	gaps := fileCompanionPaperGaps(markdown, sourceName)
	if len(gaps) == 0 || lookup == nil {
		return markdown, false
	}
	notes := make([]string, 0, len(gaps))
	seen := make(map[string]bool, len(gaps))
	missed := false
	for _, gap := range gaps {
		key := gap.kind + "\n" + gap.query
		if seen[key] {
			continue
		}
		seen[key] = true
		_, page := lookup(gap.query)
		page = paperTargetURL(page)
		if page == "" {
			missed = true
		}
		notes = append(notes, paperCheckLine(gap, page))
	}
	if len(notes) == 0 {
		return markdown, missed
	}
	return fileCompanionAppendWebCheck(markdown, notes), missed
}

func fileCompanionPaperGaps(markdown, sourceName string) []paperGap {
	var gaps []paperGap
	section := ""
	var block paperBlock
	var body strings.Builder
	sectionGaps := 0
	flushSection := func() {
		if added := flushPaperBlock(&gaps, section, sourceName, &block); added {
			sectionGaps++
		}
		if sectionGaps == 0 {
			if gap, ok := paperFallbackGap(section, body.String(), sourceName); ok {
				gaps = append(gaps, gap)
			}
		}
		body.Reset()
		sectionGaps = 0
		block = paperBlock{}
	}
	for _, line := range strings.Split(markdown, "\n") {
		line = strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#") {
			flushSection()
			section = paperSection(trim)
			continue
		}
		if section == "" {
			continue
		}
		body.WriteString(trim)
		body.WriteByte('\n')
		field, value, ok := paperField(trim)
		if !ok {
			continue
		}
		if field == "name" && section == "数据集" {
			// A later name starts the next dataset. The first name often
			// arrives after the address line, and must keep that address.
			if block.name != "" {
				if flushPaperBlock(&gaps, section, sourceName, &block) {
					sectionGaps++
				}
			}
			block.name = value
			continue
		}
		switch field {
		case "name":
			if block.name == "" {
				block.name = value
			}
		case "addr":
			block.addr = value
			block.addrSet = true
		case "query":
			block.query = value
		}
	}
	flushSection()
	if len(gaps) > 4 {
		return gaps[:4]
	}
	return gaps
}

type paperBlock struct {
	name    string
	addr    string
	query   string
	addrSet bool
}

func flushPaperBlock(gaps *[]paperGap, section, sourceName string, block *paperBlock) bool {
	gap, ok := block.gap(section, sourceName)
	*block = paperBlock{}
	if !ok {
		return false
	}
	*gaps = append(*gaps, gap)
	return true
}

func (b paperBlock) gap(section, sourceName string) (paperGap, bool) {
	if (section != "数据集" && section != "代码") || !b.addrSet || paperLineHasRealURL(b.addr) {
		return paperGap{}, false
	}
	query := cleanPaperQuery(b.query)
	if query == "" {
		query = paperFallbackQuery(section, b.name, sourceName)
	}
	name := strings.TrimSpace(b.name)
	if section == "代码" {
		name = "代码"
	}
	if strings.TrimSpace(query) == "" {
		return paperGap{}, false
	}
	return paperGap{kind: section, name: name, query: query}, true
}

func paperFallbackGap(section, body, sourceName string) (paperGap, bool) {
	if (section != "数据集" && section != "代码") || !strings.Contains(body, "论文未给出") || paperLineHasRealURL(body) {
		return paperGap{}, false
	}
	name := ""
	if section == "代码" {
		name = "代码"
	}
	return paperGap{kind: section, name: name, query: paperFallbackQuery(section, "", sourceName)}, true
}

func paperSection(title string) string {
	title = strings.TrimSpace(strings.TrimLeft(title, "#"))
	title = strings.TrimSpace(strings.TrimLeft(title, "0123456789.、．)）（( \t"))
	switch {
	case strings.HasPrefix(title, "数据集"):
		return "数据集"
	case strings.HasPrefix(title, "是否开源"), strings.HasPrefix(title, "开源"):
		return "代码"
	default:
		return ""
	}
}

func paperField(line string) (string, string, bool) {
	trim := strings.TrimSpace(line)
	trim = strings.TrimSpace(strings.TrimLeft(trim, "-*•·・ \t"))
	trim = stripPaperListIndex(trim)
	label, value, ok := splitPaperField(trim)
	if !ok {
		return "", "", false
	}
	switch {
	case strings.Contains(label, "名称"):
		return "name", value, true
	case strings.Contains(label, "下载地址"), strings.Contains(label, "代码地址"), strings.Contains(label, "仓库"):
		return "addr", value, true
	case strings.Contains(label, "检索词"):
		return "query", value, true
	default:
		return "", "", false
	}
}

func stripPaperListIndex(s string) string {
	rs := []rune(s)
	i := 0
	for i < len(rs) && rs[i] >= '0' && rs[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(rs) {
		return s
	}
	switch rs[i] {
	case '.', ')', '）', '、', '．':
		return strings.TrimSpace(string(rs[i+1:]))
	default:
		return s
	}
}

func splitPaperField(body string) (string, string, bool) {
	for _, sep := range []string{"：", ":"} {
		if i := strings.Index(body, sep); i >= 0 {
			return strings.TrimSpace(body[:i]), strings.TrimSpace(body[i+len(sep):]), true
		}
	}
	return "", "", false
}

func paperLineHasURL(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "https://") || strings.Contains(lower, "http://")
}

// paperLineHasRealURL reports whether text contains an http(s) address that is
// not a search-engine page. A Google result page is not the paper's address.
func paperLineHasRealURL(text string) bool {
	for _, raw := range paperUngivenURL.FindAllString(text, -1) {
		if paperSearchPageUsable(cleanPaperSearchURL(raw)) {
			return true
		}
	}
	return false
}

func cleanPaperQuery(query string) string {
	query = strings.TrimSpace(query)
	if query == "" || query == "不需要" || query == "无" || strings.Contains(query, "不需要") {
		return ""
	}
	rs := []rune(query)
	if len(rs) > 180 {
		query = string(rs[:180])
	}
	return strings.TrimSpace(query)
}

func paperFallbackQuery(section, name, sourceName string) string {
	name = strings.TrimSpace(name)
	source := strings.TrimSpace(sourceName)
	if section == "代码" {
		if source != "" {
			return source + " github source code"
		}
		return "github source code"
	}
	if name != "" {
		return name + " dataset download"
	}
	if source != "" {
		return source + " dataset download"
	}
	return "dataset download"
}

func paperCheckLine(gap paperGap, page string) string {
	label := gap.kind
	if gap.name != "" && gap.name != gap.kind {
		label = gap.kind + " " + gap.name
	}
	if page == "" {
		if gap.kind == "代码" {
			return "- " + label + "：网上未确认到代码地址"
		}
		return "- " + label + "：网上未确认到下载地址"
	}
	return "- " + label + "：" + page + "（可能）"
}

func fileCompanionAppendWebCheck(markdown string, notes []string) string {
	if len(notes) == 0 {
		return markdown
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(markdown, "\n"))
	b.WriteString("\n\n## 网上核对\n")
	b.WriteString("论文原文没有给出下列地址。下面是网上检索到的页面，需要自行核对。\n\n")
	for _, note := range notes {
		b.WriteString(note)
		b.WriteByte('\n')
	}
	return b.String()
}

func cleanPaperSearchURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), ".,);，。")
}

// fileCompanionRedactUngivenURLs drops http(s) addresses on lines that say the
// paper did not give them. A model URL next to 「未给出」 is not the paper's address.
func fileCompanionRedactUngivenURLs(markdown string) string {
	lines := strings.Split(markdown, "\n")
	changed := false
	for i, line := range lines {
		if !paperLineSaysUngiven(line) || !paperLineHasURL(line) {
			continue
		}
		cleaned := paperUngivenURL.ReplaceAllString(line, "")
		cleaned = strings.TrimRight(cleaned, " \t")
		for strings.Contains(cleaned, "  ") {
			cleaned = strings.ReplaceAll(cleaned, "  ", " ")
		}
		if cleaned != line {
			lines[i] = cleaned
			changed = true
		}
	}
	if !changed {
		return markdown
	}
	return strings.Join(lines, "\n")
}

// fileCompanionRedactSearchPageURLs removes search-engine addresses from the
// dataset and code sections. The host search then writes the target page.
func fileCompanionRedactSearchPageURLs(markdown string) string {
	lines := strings.Split(markdown, "\n")
	section := ""
	changed := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#") {
			section = paperSection(trim)
			continue
		}
		if (section != "数据集" && section != "代码") || !paperLineHasURL(line) {
			continue
		}
		cleaned := paperUngivenURL.ReplaceAllStringFunc(line, func(raw string) string {
			if paperSearchPageUsable(cleanPaperSearchURL(raw)) {
				return raw
			}
			return ""
		})
		cleaned = strings.TrimRight(cleaned, " \t")
		for strings.Contains(cleaned, "  ") {
			cleaned = strings.ReplaceAll(cleaned, "  ", " ")
		}
		if cleaned != line {
			lines[i] = cleaned
			changed = true
		}
	}
	if !changed {
		return markdown
	}
	return strings.Join(lines, "\n")
}

func paperLineSaysUngiven(line string) bool {
	return strings.Contains(line, "未给出") || strings.Contains(line, "未提供") || strings.Contains(line, "未标注")
}

func firstPaperSearchPage(results []websearch.SearchResult) (string, string) {
	for _, item := range results {
		page := paperTargetURL(item.URL)
		if page == "" {
			continue
		}
		return strings.TrimSpace(item.Title), page
	}
	return "", ""
}

// paperTargetURL unwraps a search redirect to the http(s) page it points at.
// A page that is still a search engine is dropped, so the PDF never keeps a
// Google, Bing, Baidu, DuckDuckGo, or Sogou results address.
func paperTargetURL(raw string) string {
	page := cleanPaperSearchURL(raw)
	seen := make(map[string]bool, 4)
	for i := 0; i < 4; i++ {
		if page == "" || seen[page] {
			break
		}
		seen[page] = true
		next := unwrapPaperRedirect(page)
		if next == "" {
			break
		}
		page = cleanPaperSearchURL(next)
	}
	if !paperSearchPageUsable(page) {
		return ""
	}
	return page
}

func unwrapPaperRedirect(page string) string {
	parsed, err := url.Parse(page)
	if err != nil {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return ""
	}
	path := strings.ToLower(parsed.EscapedPath())
	if path == "" {
		path = "/"
	}
	switch {
	case paperHostIsGoogle(host) && (path == "/url" || strings.HasPrefix(path, "/url/")):
		return firstHTTPParam(parsed, "q", "url")
	case paperHostIsGoogle(host) && (path == "/imgres" || strings.HasPrefix(path, "/imgres/")):
		return firstHTTPParam(parsed, "imgurl", "imgrefurl")
	case (host == "duckduckgo.com" || strings.HasSuffix(host, ".duckduckgo.com")) && (path == "/l" || strings.HasPrefix(path, "/l/")):
		return firstHTTPParam(parsed, "uddg")
	case (host == "bing.com" || strings.HasSuffix(host, ".bing.com")) && strings.HasPrefix(path, "/ck/"):
		return decodeBingClickTarget(rawQueryParam(parsed, "u"))
	case baiduSearchHost(host) && (path == "/link" || strings.HasPrefix(path, "/link") || strings.Contains(path, "baidu.php")):
		return firstHTTPParam(parsed, "url")
	case (host == "sogou.com" || strings.HasSuffix(host, ".sogou.com")) && strings.Contains(path, "link"):
		return firstHTTPParam(parsed, "url")
	default:
		return ""
	}
}

func firstHTTPParam(parsed *url.URL, keys ...string) string {
	for _, key := range keys {
		if target := httpPaperURL(rawQueryParam(parsed, key)); target != "" {
			return target
		}
	}
	return ""
}

func rawQueryParam(parsed *url.URL, key string) string {
	if parsed == nil {
		return ""
	}
	for _, part := range strings.Split(parsed.RawQuery, "&") {
		name, value, ok := strings.Cut(strings.TrimPrefix(part, "?"), "=")
		if !ok || name != key {
			continue
		}
		decoded, err := url.PathUnescape(value)
		if err != nil {
			return value
		}
		return decoded
	}
	return ""
}

func httpPaperURL(raw string) string {
	raw = strings.TrimSpace(raw)
	for i := 0; i < 2; i++ {
		lower := strings.ToLower(raw)
		if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
			return raw
		}
		if !strings.Contains(raw, "%") {
			return ""
		}
		decoded, err := url.PathUnescape(raw)
		if err != nil || decoded == raw {
			return ""
		}
		raw = decoded
	}
	return ""
}

func decodeBingClickTarget(raw string) string {
	raw = strings.TrimSpace(raw)
	if target := httpPaperURL(raw); target != "" {
		return target
	}
	payload := raw
	if strings.HasPrefix(payload, "a1") {
		payload = payload[2:]
	}
	payload = strings.TrimRight(payload, "=")
	if payload == "" {
		return ""
	}
	padded := payload
	if m := len(padded) % 4; m != 0 {
		padded += strings.Repeat("=", 4-m)
	}
	for _, candidate := range []string{payload, padded} {
		for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding, base64.StdEncoding, base64.URLEncoding} {
			decoded, err := enc.DecodeString(candidate)
			if err != nil {
				continue
			}
			if target := httpPaperURL(string(decoded)); target != "" {
				return target
			}
		}
	}
	return ""
}

func paperSearchPageUsable(page string) bool {
	page = strings.TrimSpace(page)
	lower := strings.ToLower(page)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	parsed, err := url.Parse(page)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	path := strings.ToLower(parsed.EscapedPath())
	if path == "" {
		path = "/"
	}
	return !paperHostIsSearchEngine(host, path)
}

func paperHostIsSearchEngine(host, path string) bool {
	switch {
	case host == "duckduckgo.com" || strings.HasSuffix(host, ".duckduckgo.com"):
		return true
	case host == "sogou.com" || strings.HasSuffix(host, ".sogou.com"):
		return true
	case host == "bing.com" || strings.HasSuffix(host, ".bing.com"):
		return true
	case paperHostIsGoogle(host):
		return true
	case baiduSearchHost(host):
		return true
	case (host == "github.com" || host == "www.github.com") && (path == "/search" || strings.HasPrefix(path, "/search/")):
		return true
	default:
		return false
	}
}

func paperHostIsGoogle(host string) bool {
	if host == "google.com" || strings.HasPrefix(host, "google.") {
		return true
	}
	return strings.Contains(host, ".google.") || strings.HasSuffix(host, ".google")
}

func baiduSearchHost(host string) bool {
	switch host {
	case "baidu.com", "www.baidu.com", "m.baidu.com", "wap.baidu.com":
		return true
	default:
		return false
	}
}

func (a *App) fileCompanionLatestPaper(sessionID string) string {
	if a == nil {
		return ""
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, `/\`) {
		return ""
	}
	dir := filepath.Join(a.GetDataDir(), "file-companion", "results", sessionID)
	raw, err := os.ReadFile(filepath.Join(dir, "latest.txt"))
	if err != nil {
		return ""
	}
	return fileCompanionPaperInside(dir, string(raw))
}

func fileCompanionPaperInside(dir, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	abs, err := filepath.Abs(raw)
	if err != nil {
		return ""
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return ""
	}
	return abs
}

func (a *App) fileCompanionRecordPaperTurn(sessionID string) {
	if a == nil || a.imHandler == nil || a.imHandler.memory == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, `/\`) {
		return
	}
	n := 0
	for _, message := range fileCompanionDisplayMessages(a.imHandler.memory.Load("file-companion:" + sessionID)) {
		if message.Paper {
			n++
		}
	}
	if n < 1 {
		return
	}
	path := filepath.Join(a.GetDataDir(), "file-companion", "results", sessionID, "latest-turn.txt")
	if err := os.WriteFile(path, []byte(strconv.Itoa(n)), 0o644); err != nil {
		logFileCompanion("paper record failed session=%s", sessionID)
	}
}

func (a *App) fileCompanionPaperTurnIndex(sessionID string) int {
	if a == nil {
		return 0
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, `/\`) {
		return 0
	}
	raw, err := os.ReadFile(filepath.Join(a.GetDataDir(), "file-companion", "results", sessionID, "latest-turn.txt"))
	if err != nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

func (a *App) fileCompanionAttachPaperResult(sessionID string, messages []FileCompanionMessage) []FileCompanionMessage {
	if a == nil || len(messages) == 0 {
		return messages
	}
	pdf := a.fileCompanionLatestPaper(sessionID)
	if pdf == "" {
		return messages
	}
	// latest-turn.txt is the button turn that wrote the PDF. A later press
	// that fails must not take the card. A missing index keeps the last card.
	index := a.fileCompanionPaperTurnIndex(sessionID)
	chosen := -1
	seen := 0
	for i := 0; i < len(messages)-1; i++ {
		if messages[i].Role != "user" || !messages[i].Paper || messages[i+1].Role != "assistant" {
			continue
		}
		seen++
		if index == 0 || seen == index {
			chosen = i + 1
			if index != 0 {
				break
			}
		}
	}
	if chosen < 0 {
		return messages
	}
	messages[chosen].ResultPath = pdf
	messages[chosen].Text = "已生成论文解读"
	return messages
}

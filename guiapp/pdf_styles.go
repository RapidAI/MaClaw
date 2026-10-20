package guiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// pdfStyleStoreMu serializes read-modify-write of the PDF style file.
var pdfStyleStoreMu sync.Mutex

// renamePDFStyleFile is the rename used by a catalog replacement. Tests point it
// at a failure to prove an unreadable file is put back.
var renamePDFStyleFile = os.Rename

const pdfStyleCorruptWarning = "风格文件无法读取，已暂时使用默认公报。原文件未改动。"

const pdfStyleGenerateTimeout = 60 * time.Second

type pdfStyleFile struct {
	Active string               `json:"active"`
	Styles []docgen.CustomStyle `json:"styles"`
}

func (a *App) pdfStylePath() string {
	return filepath.Join(a.GetDataDir(), "pdf_styles.json")
}

func (a *App) reloadPDFStyles() error {
	pdfStyleStoreMu.Lock()
	defer pdfStyleStoreMu.Unlock()
	return a.reloadPDFStylesUnlocked()
}

func (a *App) reloadPDFStylesUnlocked() error {
	file, err := readPDFStyleFile(a.pdfStylePath())
	if err != nil {
		return err
	}
	if err := docgen.SetCustomStyles(usablePDFStyles(file.Styles)); err != nil {
		return err
	}
	active := strings.TrimSpace(file.Active)
	if active == "" {
		return docgen.SetActiveStyle("")
	}
	if err := docgen.SetActiveStyle(active); err != nil {
		return docgen.SetActiveStyle("")
	}
	return nil
}

func usablePDFStyles(styles []docgen.CustomStyle) []docgen.CustomStyle {
	if len(styles) == 0 {
		return nil
	}
	out := make([]docgen.CustomStyle, 0, len(styles))
	seen := map[string]bool{}
	for _, style := range styles {
		if err := docgen.ValidateCustomStyle(style); err != nil {
			log.Printf("[pdf-style] skip %q: %v", style.ID, err)
			continue
		}
		id := strings.ToLower(strings.TrimSpace(style.ID))
		if seen[id] {
			log.Printf("[pdf-style] skip duplicate %q", style.ID)
			continue
		}
		seen[id] = true
		out = append(out, style)
	}
	return out
}

func readPDFStyleFile(path string) (pdfStyleFile, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// A crash during replace leaves the new bytes in .tmp and the
			// previous bytes in .bak, with no live file. Finish that replace.
			if file, ok := adoptPDFStyleSibling(path); ok {
				return file, nil
			}
			return pdfStyleFile{}, nil
		}
		return pdfStyleFile{}, err
	}
	return unmarshalPDFStyleFile(body)
}

func unmarshalPDFStyleFile(body []byte) (pdfStyleFile, error) {
	body = bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF})
	// A zero-length file is an empty catalog. It is not JSON, and treating it
	// as corrupt would quarantine nothing and then block the next save.
	if len(bytes.TrimSpace(body)) == 0 {
		return pdfStyleFile{}, nil
	}
	var file pdfStyleFile
	if err := json.Unmarshal(body, &file); err != nil {
		return pdfStyleFile{}, err
	}
	return file, nil
}

func adoptPDFStyleSibling(path string) (pdfStyleFile, bool) {
	for _, suffix := range []string{".tmp", ".bak"} {
		alt := path + suffix
		body, err := os.ReadFile(alt)
		if err != nil {
			continue
		}
		if len(bytes.TrimSpace(bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF}))) == 0 {
			continue
		}
		file, err := unmarshalPDFStyleFile(body)
		if err != nil {
			continue
		}
		if renErr := renamePDFStyleFile(alt, path); renErr != nil {
			log.Printf("[pdf-style] recover %s failed: %v", alt, renErr)
		}
		return file, true
	}
	return pdfStyleFile{}, false
}

func pdfStyleCorrupt(err error) bool {
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	return errors.As(err, &syntax) || errors.As(err, &typ)
}

// readPDFStyleCatalog reads the catalog for a change and does not quarantine it.
// A missing live file may be restored from .tmp or .bak. The bool is true when
// the bytes cannot be parsed. The caller then writes through commitPDFStyleFile,
// which is what moves those bytes to pdf_styles.json.bad.
func (a *App) readPDFStyleCatalog() (pdfStyleFile, bool, error) {
	file, err := readPDFStyleFile(a.pdfStylePath())
	if err == nil {
		return file, false, nil
	}
	if !pdfStyleCorrupt(err) {
		return pdfStyleFile{}, false, err
	}
	if _, statErr := os.Stat(a.pdfStylePath() + ".bad"); statErr == nil {
		return pdfStyleFile{}, false, fmt.Errorf("风格文件无法读取，已保留 pdf_styles.json.bad，请先移走该备份")
	}
	return pdfStyleFile{}, true, nil
}

func writePDFStyleFile(path string, file pdfStyleFile) error {
	return commitPDFStyleFile(path, file, false)
}

// commitPDFStyleFile replaces the catalog. preserveUnreadable moves a file that
// could not be parsed to pdf_styles.json.bad as part of the same replacement.
// A failed replacement puts that file back. The ordinary path still uses a
// temporary file, and its .bak is only a crash copy of a catalog that parsed.
func commitPDFStyleFile(path string, file pdfStyleFile, preserveUnreadable bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	if preserveUnreadable {
		bad := path + ".bad"
		if _, statErr := os.Stat(bad); statErr == nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("风格文件无法读取，已保留 pdf_styles.json.bad，请先移走该备份")
		}
		if renErr := renamePDFStyleFile(path, bad); renErr != nil {
			_ = os.Remove(tmp)
			log.Printf("[pdf-style] quarantine failed: %v", renErr)
			return fmt.Errorf("风格文件无法读取，未改动原文件")
		}
		if err := renamePDFStyleFile(tmp, path); err != nil {
			if backErr := renamePDFStyleFile(bad, path); backErr != nil {
				log.Printf("[pdf-style] restore unreadable catalog failed: %v", backErr)
			}
			_ = os.Remove(tmp)
			return err
		}
		log.Printf("[pdf-style] quarantined unreadable catalog to %s", bad)
		return nil
	}
	backup := path + ".bak"
	_ = os.Remove(backup)
	if err := renamePDFStyleFile(path, backup); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return err
	}
	if err := renamePDFStyleFile(tmp, path); err != nil {
		_ = renamePDFStyleFile(backup, path)
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func pdfStyleSelectable(id string, customs []docgen.CustomStyle) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return false
	}
	if style, ok := docgen.LookupStyle(id); ok && style.Builtin {
		return true
	}
	for _, style := range customs {
		if strings.EqualFold(strings.TrimSpace(style.ID), id) {
			return true
		}
	}
	return false
}

// PDFStyleList is the settings catalog. Warning is set when the saved file
// cannot be parsed. Built-in styles still load, and the file is left in place.
type PDFStyleList struct {
	Styles  []docgen.StyleInfo `json:"styles"`
	Warning string             `json:"warning,omitempty"`
}

// ListPDFStyles returns the built-in styles, then custom styles. The active flag
// marks the style used for every later PDF.
// A file that cannot be parsed still returns the five built-in styles. The reset
// stays under the same lock as a selection, so it cannot wipe a write that
// already moved the broken file aside.
func (a *App) ListPDFStyles() (PDFStyleList, error) {
	pdfStyleStoreMu.Lock()
	defer pdfStyleStoreMu.Unlock()
	if err := a.reloadPDFStylesUnlocked(); err != nil {
		if pdfStyleCorrupt(err) {
			_ = docgen.SetCustomStyles(nil)
			_ = docgen.SetActiveStyle("")
			log.Printf("[pdf-style] catalog unreadable: %v", err)
			return PDFStyleList{Styles: docgen.ListStyleInfo(), Warning: pdfStyleCorruptWarning}, nil
		}
		return PDFStyleList{}, err
	}
	return PDFStyleList{Styles: docgen.ListStyleInfo()}, nil
}

// SetPDFStyle makes one style the default for every PDF this app generates.
func (a *App) SetPDFStyle(id string) error {
	pdfStyleStoreMu.Lock()
	defer pdfStyleStoreMu.Unlock()
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return fmt.Errorf("缺少风格 id")
	}
	file, unreadable, err := a.readPDFStyleCatalog()
	if err != nil {
		return err
	}
	usable := usablePDFStyles(file.Styles)
	if !pdfStyleSelectable(id, usable) {
		return fmt.Errorf("没有这套风格")
	}
	// Selection changes only the active id. Records the catalog skips stay in
	// the file so a later edit can repair them. Memory changes after the file
	// is in place, so a rejected id leaves both the file and the process alone.
	file.Active = id
	if err := commitPDFStyleFile(a.pdfStylePath(), file, unreadable); err != nil {
		return err
	}
	if err := docgen.SetCustomStyles(usable); err != nil {
		_ = a.reloadPDFStylesUnlocked()
		return err
	}
	return docgen.SetActiveStyle(id)
}

// GeneratePDFStyle asks the configured model for a layout and palette, then saves it.
func (a *App) GeneratePDFStyle(request string) (docgen.StyleInfo, error) {
	request = strings.TrimSpace(request)
	if request == "" {
		return docgen.StyleInfo{}, fmt.Errorf("请先写明想要的风格")
	}
	raw, err := a.completePDFStyleJSON(request)
	if err != nil {
		return docgen.StyleInfo{}, err
	}
	style, err := parseGeneratedPDFStyle(raw)
	if err != nil {
		return docgen.StyleInfo{}, err
	}
	style, err = a.storeGeneratedPDFStyle(style)
	if err != nil {
		return docgen.StyleInfo{}, pdfStyleClientError(err)
	}
	for _, info := range docgen.ListStyleInfo() {
		if info.ID == style.ID {
			return info, nil
		}
	}
	return docgen.StyleInfo{}, fmt.Errorf("风格已保存，但列表里没有它")
}

// DeletePDFStyle removes a custom style. Built-in styles stay.
func (a *App) DeletePDFStyle(id string) error {
	pdfStyleStoreMu.Lock()
	defer pdfStyleStoreMu.Unlock()
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return fmt.Errorf("缺少风格 id")
	}
	for _, info := range docgen.ListStyleInfo() {
		if info.ID == id && info.Builtin {
			return fmt.Errorf("内置风格不能删除")
		}
	}
	file, err := readPDFStyleFile(a.pdfStylePath())
	if err != nil {
		if pdfStyleCorrupt(err) {
			return fmt.Errorf("风格文件无法读取，未改动原文件")
		}
		return err
	}
	next := make([]docgen.CustomStyle, 0, len(file.Styles))
	found := false
	for _, style := range file.Styles {
		if strings.EqualFold(strings.TrimSpace(style.ID), id) {
			found = true
			continue
		}
		next = append(next, style)
	}
	if !found {
		return fmt.Errorf("没有这套自定义风格")
	}
	if strings.EqualFold(file.Active, id) {
		file.Active = ""
	}
	file.Styles = next
	if err := writePDFStyleFile(a.pdfStylePath(), file); err != nil {
		return err
	}
	return a.reloadPDFStylesUnlocked()
}

func (a *App) storeGeneratedPDFStyle(style docgen.CustomStyle) (docgen.CustomStyle, error) {
	pdfStyleStoreMu.Lock()
	defer pdfStyleStoreMu.Unlock()
	file, unreadable, err := a.readPDFStyleCatalog()
	if err != nil {
		return docgen.CustomStyle{}, err
	}
	style.ID = uniquePDFStyleID(style.ID, file.Styles)
	_, style, err = mergeGeneratedPDFStyle(usablePDFStyles(file.Styles), style)
	if err != nil {
		return docgen.CustomStyle{}, err
	}
	next := append(append([]docgen.CustomStyle{}, file.Styles...), style)
	active := strings.TrimSpace(file.Active)
	if active != "" && !pdfStyleSelectable(active, usablePDFStyles(next)) {
		active = ""
	}
	file.Styles = next
	file.Active = active
	if err := commitPDFStyleFile(a.pdfStylePath(), file, unreadable); err != nil {
		return docgen.CustomStyle{}, err
	}
	if err := docgen.SetCustomStyles(usablePDFStyles(next)); err != nil {
		_ = a.reloadPDFStylesUnlocked()
		return docgen.CustomStyle{}, err
	}
	if err := docgen.SetActiveStyle(active); err != nil {
		_ = docgen.SetActiveStyle("")
	}
	return style, nil
}

func mergeGeneratedPDFStyle(existing []docgen.CustomStyle, style docgen.CustomStyle) ([]docgen.CustomStyle, docgen.CustomStyle, error) {
	style.ID = uniquePDFStyleID(style.ID, existing)
	if err := docgen.ValidateCustomStyle(style); err != nil {
		return nil, docgen.CustomStyle{}, err
	}
	normalized, err := style.Normalized()
	if err != nil {
		return nil, docgen.CustomStyle{}, err
	}
	return append(existing, normalized), normalized, nil
}

func uniquePDFStyleID(id string, existing []docgen.CustomStyle) string {
	taken := map[string]bool{}
	for _, style := range docgen.ListStyleInfo() {
		taken[strings.ToLower(style.ID)] = true
	}
	for _, style := range existing {
		taken[strings.ToLower(style.ID)] = true
	}
	base := strings.ToLower(strings.TrimSpace(id))
	if base == "" || !strings.ContainsAny(base, "abcdefghijklmnopqrstuvwxyz") {
		base = "custom-style"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	base = strings.Trim(b.String(), "-")
	if len(base) > 24 {
		base = strings.TrimRight(base[:24], "-")
	}
	if base == "" {
		base = "custom-style"
	}
	candidate := base
	for n := 2; taken[candidate] && n < 100; n++ {
		candidate = fmt.Sprintf("%s-%d", base, n)
	}
	if taken[candidate] {
		candidate = fmt.Sprintf("custom-%d", time.Now().Unix()%100000)
	}
	return candidate
}

func parseGeneratedPDFStyle(raw string) (docgen.CustomStyle, error) {
	body := extractJSONObject(raw)
	if body == "" {
		return docgen.CustomStyle{}, fmt.Errorf("模型没有返回可用的风格 JSON")
	}
	var style docgen.CustomStyle
	if err := json.Unmarshal([]byte(body), &style); err != nil {
		return docgen.CustomStyle{}, fmt.Errorf("风格 JSON 无法解析")
	}
	if err := docgen.ValidateCustomStyle(style); err != nil && (strings.Contains(err.Error(), "pdf_style_id_invalid") || strings.Contains(err.Error(), "pdf_style_id_taken")) {
		style.ID = "custom-style"
	}
	if err := docgen.ValidateCustomStyle(style); err != nil {
		return docgen.CustomStyle{}, pdfStyleClientError(err)
	}
	return style, nil
}

func pdfStyleClientError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case strings.Contains(err.Error(), "pdf_style_motif_invalid"):
		return fmt.Errorf("版式必须是顶栏、立柱、双框、色带、粗线之一")
	case strings.Contains(err.Error(), "pdf_style_id_invalid"), strings.Contains(err.Error(), "pdf_style_id_taken"):
		return fmt.Errorf("风格编号不可用")
	case strings.Contains(err.Error(), "pdf_style_label_invalid"):
		return fmt.Errorf("风格名称不合格")
	case strings.Contains(err.Error(), "pdf_style_summary_invalid"):
		return fmt.Errorf("风格说明不合格")
	case strings.Contains(err.Error(), "pdf_style_keywords_required"):
		return fmt.Errorf("至少需要一个两个字以上的关键词")
	case strings.Contains(err.Error(), "pdf_style_color_invalid"):
		return fmt.Errorf("颜色必须是 6 位十六进制")
	default:
		return err
	}
}

const pdfStyleSystemPrompt = `你是正式文档的版式设计师。根据用户需求设计一套可复用的 PDF 风格。只输出一个 JSON 对象，不要解释。
字段：
id：英文小写和短横线，2-32 字符，不要用 gazette、brief、dossier、atlas、ledger
label：不超过 8 个汉字
summary：不超过 28 个字，说明这套版式适合什么文书、主色是什么
keywords：2 到 6 个中文词，每个至少 2 个字
motif：版式，必须是 masthead、rail、frame、band、ledger 之一。masthead 是整页顶栏加朱红粗线，rail 是左侧色柱，frame 是金线双框和居中标题，band 是顶部细带加短色标，ledger 是粗线压题、标题更紧
colors：6 位十六进制，不要带 #。必须包含 ink、navy、accent、gold、meta、rule、quote、link、on_dark
页面底色保持白色。ink、navy、meta、quote、link 必须在白底上清楚，不要用浅灰。accent 和 rule 要饱和。on_dark 是深色顶栏上的浅色字。`

func (a *App) completePDFStyleJSON(request string) (string, error) {
	cfg := captionRequestConfig(a.withGlobalThinkingMode(a.GetMaclawLLMConfig()))
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return "", fmt.Errorf("请先在「大模型配置」里保存一个可用模型")
	}
	ctx, cancel := context.WithTimeout(context.Background(), pdfStyleGenerateTimeout)
	defer cancel()
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": pdfStyleSystemPrompt},
		map[string]interface{}{"role": "user", "content": request},
	}
	extra := map[string]interface{}{"max_tokens": 900}
	if captionUsesAnthropic(cfg) {
		resp, err := llm.DoAnthropicRequestWithOptions(ctx, cfg, messages, nil, pdfStyleHTTPClient, llm.AnthropicMessagesRequestOptions{MaxTokens: 900})
		text, err := captionTextFromResponse(resp, err)
		return pdfStyleModelText(text, err)
	}
	if cfg.IsResponsesAPI() {
		req, _, _, err := llm.NewResponsesAPIRequest(ctx, cfg, messages, llm.ResponsesAPIRequestOptions{Stream: false, ExtraBody: extra})
		if err != nil {
			return "", pdfStyleCallError(err)
		}
		return pdfStyleHTTPParse(ctx, req, llm.ParseNonStreamResponsesAPIBody)
	}
	req, _, _, err := llm.NewOpenAIChatRequest(ctx, cfg, messages, llm.OpenAIChatRequestOptions{Stream: false, ExtraBody: extra})
	if err != nil {
		return "", pdfStyleCallError(err)
	}
	return pdfStyleHTTPParse(ctx, req, llm.ParseNonStreamOpenAIResponseBody)
}

var pdfStyleHTTPClient = &http.Client{Timeout: pdfStyleGenerateTimeout}

func pdfStyleHTTPParse(ctx context.Context, req *http.Request, parse func([]byte) (*llm.Response, error)) (string, error) {
	req = req.WithContext(ctx)
	resp, err := pdfStyleHTTPClient.Do(req)
	if err != nil {
		return "", pdfStyleCallError(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return "", pdfStyleCallError(err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("模型请求失败（HTTP %d）", resp.StatusCode)
	}
	parsed, err := parse(body)
	text, err := captionTextFromResponse(parsed, err)
	return pdfStyleModelText(text, err)
}

// pdfStyleModelText is the text handed back to the settings panel.
// Transport failures stay out of that sentence: url.Error includes the endpoint.
func pdfStyleModelText(text string, err error) (string, error) {
	if err != nil {
		return "", pdfStyleCallError(err)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("模型没有返回内容")
	}
	return text, nil
}

func pdfStyleCallError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		log.Printf("[pdf-style] model call failed: %v", err)
		return fmt.Errorf("生成超时，请再试一次")
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		log.Printf("[pdf-style] model call failed: %v", err)
		if netErr.Timeout() {
			return fmt.Errorf("生成超时，请再试一次")
		}
		return fmt.Errorf("模型请求失败")
	}
	if err.Error() == "empty caption response" {
		return fmt.Errorf("模型没有返回内容")
	}
	return err
}

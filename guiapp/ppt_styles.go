package guiapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/pptx"
)

// pptStyleStoreMu serializes read-modify-write of the custom style file so a
// generate and a delete cannot each save a stale copy.
var pptStyleStoreMu sync.Mutex

const pptStyleGenerateTimeout = 60 * time.Second

// PPTStyleCard is one style in the settings gallery, with a cover preview.
type PPTStyleCard struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Summary    string   `json:"summary"`
	Keywords   []string `json:"keywords"`
	Builtin    bool     `json:"builtin"`
	CoverDark  bool     `json:"cover_dark"`
	PreviewURL string   `json:"preview_url"`
}

type pptStyleFile struct {
	Styles []pptx.CustomStyle `json:"styles"`
}

func (a *App) pptStylePath() string {
	return filepath.Join(a.GetDataDir(), "ppt_styles.json")
}

const pptStylePreviewRevision = "12"

func (a *App) pptStylePreviewPath(id, lang string) string {
	return filepath.Join(a.GetDataDir(), "ppt_style_previews", id+"-"+pptx.NormalizeStyleLang(lang)+"-"+pptStylePreviewRevision+".png")
}

func (a *App) removePPTStylePreviews(id string) {
	for _, lang := range []string{"zh-Hans", "zh-Hant", "en"} {
		_ = os.Remove(a.pptStylePreviewPath(id, lang))
	}
}

func (a *App) reloadPPTStyles() error {
	pptStyleStoreMu.Lock()
	defer pptStyleStoreMu.Unlock()
	return a.reloadPPTStylesUnlocked()
}

func (a *App) reloadPPTStylesUnlocked() error {
	styles, err := readPPTStyleFile(a.pptStylePath())
	if err != nil {
		return err
	}
	return pptx.SetCustomStyles(usableCustomStyles(styles))
}

// usableCustomStyles drops records that no longer validate so one bad palette
// cannot hide the built-in styles.
func usableCustomStyles(styles []pptx.CustomStyle) []pptx.CustomStyle {
	if len(styles) == 0 {
		return nil
	}
	out := make([]pptx.CustomStyle, 0, len(styles))
	for _, style := range styles {
		if err := pptx.ValidateCustomStyle(style); err != nil {
			log.Printf("[ppt-style] skip %q: %v", style.ID, err)
			continue
		}
		out = append(out, style)
	}
	return out
}

func readPPTStyleFile(path string) ([]pptx.CustomStyle, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var file pptStyleFile
	if err := json.Unmarshal(body, &file); err != nil {
		return nil, err
	}
	return file.Styles, nil
}

func writePPTStyleFile(path string, styles []pptx.CustomStyle) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(pptStyleFile{Styles: styles}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	backup := path + ".bak"
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Rename(backup, path)
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

// ListPPTStyleChoices returns style ids, labels, and keywords without rendering previews.
func (a *App) ListPPTStyleChoices() ([]pptx.StyleInfo, error) {
	if err := a.reloadPPTStyles(); err != nil {
		return nil, err
	}
	return pptx.ListStyleInfo(), nil
}

// ListPPTStyles returns built-in and custom styles with cover previews.
func (a *App) ListPPTStyles() ([]PPTStyleCard, error) {
	if err := a.reloadPPTStyles(); err != nil {
		return nil, err
	}
	infos := pptx.ListStyleInfo()
	out := make([]PPTStyleCard, 0, len(infos))
	for _, info := range infos {
		card := PPTStyleCard{
			ID: info.ID, Label: info.Label, Summary: info.Summary,
			Keywords: info.Keywords, Builtin: info.Builtin, CoverDark: info.CoverDark,
		}
		url, err := a.ensurePPTStylePreview(info.ID, "")
		if err != nil {
			card.Summary = strings.TrimSpace(card.Summary + "（预览暂不可用）")
		} else {
			card.PreviewURL = url
		}
		out = append(out, card)
	}
	return out, nil
}

func (a *App) ensurePPTStylePreview(id, lang string) (string, error) {
	path := a.pptStylePreviewPath(id, lang)
	body, err := os.ReadFile(path)
	if err != nil || !validPreviewPNG(body) {
		body, err = pptx.RenderStylePreviewPNGLang(id, lang)
		if err != nil {
			return "", err
		}
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr == nil {
			_ = os.WriteFile(path, body, 0o644)
		}
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(body), nil
}

func validPreviewPNG(body []byte) bool {
	return len(body) >= 8 && bytes.Equal(body[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
}

// GeneratePPTStyle asks the configured model for a palette and saves it.
func (a *App) GeneratePPTStyle(request, lang string) (PPTStyleCard, error) {
	request = strings.TrimSpace(request)
	if request == "" {
		return PPTStyleCard{}, fmt.Errorf("请先写明想要的风格")
	}
	raw, err := a.completePPTStyleJSON(request)
	if err != nil {
		return PPTStyleCard{}, err
	}
	style, err := parseGeneratedPPTStyle(raw)
	if err != nil {
		return PPTStyleCard{}, err
	}
	style, err = a.storeGeneratedPPTStyle(style)
	if err != nil {
		return PPTStyleCard{}, err
	}
	preview, _ := a.ensurePPTStylePreview(style.ID, lang)
	return PPTStyleCard{
		ID: style.ID, Label: style.Label, Summary: style.Summary,
		Keywords: style.Keywords, Builtin: false, CoverDark: style.CoverDark,
		PreviewURL: preview,
	}, nil
}

func (a *App) storeGeneratedPPTStyle(style pptx.CustomStyle) (pptx.CustomStyle, error) {
	pptStyleStoreMu.Lock()
	defer pptStyleStoreMu.Unlock()
	existing, err := readPPTStyleFile(a.pptStylePath())
	if err != nil {
		return pptx.CustomStyle{}, err
	}
	merged, style, err := mergeGeneratedStyle(existing, style)
	if err != nil {
		return pptx.CustomStyle{}, err
	}
	if err := pptx.SetCustomStyles(merged); err != nil {
		_ = a.reloadPPTStylesUnlocked()
		return pptx.CustomStyle{}, err
	}
	if err := writePPTStyleFile(a.pptStylePath(), merged); err != nil {
		_ = a.reloadPPTStylesUnlocked()
		return pptx.CustomStyle{}, err
	}
	a.removePPTStylePreviews(style.ID)
	invalidateExpertDefCache("builtin-pptx-maker")
	return style, nil
}

// DeletePPTStyle removes a custom style. Built-in styles stay.
func (a *App) DeletePPTStyle(id string) error {
	pptStyleStoreMu.Lock()
	defer pptStyleStoreMu.Unlock()
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return fmt.Errorf("缺少风格 id")
	}
	for _, info := range pptx.ListStyleInfo() {
		if info.ID == id && info.Builtin {
			return fmt.Errorf("内置风格不能删除")
		}
	}
	existing, err := readPPTStyleFile(a.pptStylePath())
	if err != nil {
		return err
	}
	next := make([]pptx.CustomStyle, 0, len(existing))
	found := false
	for _, style := range usableCustomStyles(existing) {
		if strings.EqualFold(style.ID, id) {
			found = true
			continue
		}
		next = append(next, style)
	}
	if !found {
		return fmt.Errorf("没有这套自定义风格")
	}
	if err := writePPTStyleFile(a.pptStylePath(), next); err != nil {
		return err
	}
	a.removePPTStylePreviews(id)
	invalidateExpertDefCache("builtin-pptx-maker")
	return a.reloadPPTStylesUnlocked()
}

// mergeGeneratedStyle keeps only palettes that still validate, then appends
// the new style. A broken record already on disk must not reject the save.
func mergeGeneratedStyle(existing []pptx.CustomStyle, style pptx.CustomStyle) ([]pptx.CustomStyle, pptx.CustomStyle, error) {
	clean := usableCustomStyles(existing)
	style.ID = uniquePPTStyleID(style.ID, clean)
	if err := pptx.ValidateCustomStyle(style); err != nil {
		return nil, pptx.CustomStyle{}, err
	}
	return append(clean, style), style, nil
}

func uniquePPTStyleID(id string, existing []pptx.CustomStyle) string {
	taken := map[string]bool{}
	for _, style := range existing {
		taken[strings.ToLower(style.ID)] = true
	}
	base := strings.ToLower(strings.TrimSpace(id))
	if base == "" {
		base = "custom-style"
	}
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

// PPTStylePreview renders one cover and returns a data URL. Missing covers are
// generated on demand so the settings list can appear before every image exists.
func (a *App) PPTStylePreview(id, lang string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("缺少风格 id")
	}
	if err := a.reloadPPTStyles(); err != nil {
		return "", err
	}
	if !knownPPTStyleID(id) {
		return "", fmt.Errorf("没有这套风格")
	}
	return a.ensurePPTStylePreview(id, lang)
}

func knownPPTStyleID(id string) bool {
	for _, info := range pptx.ListStyleInfo() {
		if info.ID == id {
			return true
		}
	}
	return false
}

func parseGeneratedPPTStyle(raw string) (pptx.CustomStyle, error) {
	var style pptx.CustomStyle
	err := walkStyleCandidates(raw, func(body string) error {
		parsed, decErr := decodeGeneratedPPTStyle(body)
		if decErr != nil {
			return decErr
		}
		style = parsed
		return nil
	})
	return style, err
}

func decodeGeneratedPPTStyle(body string) (pptx.CustomStyle, error) {
	var style pptx.CustomStyle
	if err := json.Unmarshal([]byte(styleJSONWithKeywordList(body)), &style); err != nil {
		return pptx.CustomStyle{}, fmt.Errorf("风格 JSON 无法解析")
	}
	if err := pptx.ValidateCustomStyle(style); err != nil && (strings.Contains(err.Error(), "ppt_style_id_invalid") || strings.Contains(err.Error(), "ppt_style_id_taken")) {
		style.ID = "custom-style"
	}
	if err := pptx.ValidateCustomStyle(style); err != nil {
		if strings.Contains(err.Error(), "ppt_style_motif_invalid") {
			return pptx.CustomStyle{}, fmt.Errorf("版式名称无效，必须是 report、split、ink、dashboard、poster、sketch、board、craft、defense、consulting、official、lesson 之一")
		}
		return pptx.CustomStyle{}, fmt.Errorf("这套配色不完整：%s", err.Error())
	}
	return style, nil
}

const pptStyleSystemPrompt = `你是演示文稿配色设计师。根据用户需求设计一套可复用的 PPT 风格。只输出一个 JSON 对象，不要解释。
字段：
id：英文小写和短横线，2-32 字符，不要用 clean、magazine、eink、dashboard、retro、sketch、whiteboard、handmade、research、mckinsey、official、course，也不要用它们的旧别名 business、consulting、academic、warm、launch、tech、education、minimal、modern
label：不超过 8 个汉字
summary：不超过 28 个字，说明适合什么场合、主色是什么
keywords：2 到 6 个中文词，用户以后做这类 PPT 时用来自动选中这套风格
motif：版式，必须是 report、split、ink、dashboard、poster、sketch、board、craft、defense、consulting、official、lesson 之一。report 是左色条和证据卡，split 是对半撞色，ink 是米纸大标题，dashboard 是指标卡，poster 是色块与描边卡，sketch 是细线开放要点，board 是便签，craft 是暖纸便签，defense 是学术蓝加红色结论，consulting 是浅底细线，official 是红金正式，lesson 是课头色带
cover_dark：封面主区域是否用深色底
colors：6 位十六进制，不要带 #。必须包含 paper、ink、navy、navy2、accent、gold、white、mute、slate、card、on_dark、on_dark_mute
navy 是结构色，accent 是强调色，paper 是内容页底色，ink 是正文色，on_dark 是深色区域上的标题色。文字和背景对比要够。`

func (a *App) completePPTStyleJSON(request string) (string, error) {
	cfg, maxTokens := prepareDocumentStyleModel(a.withGlobalThinkingMode(a.GetMaclawLLMConfig()))
	if strings.TrimSpace(cfg.URL) == "" || strings.TrimSpace(cfg.Model) == "" {
		return "", fmt.Errorf("请先在「大模型配置」里保存一个可用模型")
	}
	ctx, cancel := context.WithTimeout(context.Background(), pptStyleGenerateTimeout)
	defer cancel()
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": pptStyleSystemPrompt},
		map[string]interface{}{"role": "user", "content": request},
	}
	return completeDocumentStyleModel(ctx, cfg, pptStyleHTTPClient, messages, maxTokens)
}

var pptStyleHTTPClient = &http.Client{Timeout: pptStyleGenerateTimeout}

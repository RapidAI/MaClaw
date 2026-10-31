package guiapp

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// A companion project is a zip the reader can open again. It holds the
// document bytes, the highlight list, and the chat. The PDF inside is a copy.
// Highlights are not written into that PDF.

const (
	fileCompanionProjectKind         = "maclaw-file-companion"
	fileCompanionProjectVersion      = 1
	fileCompanionProjectManifestName = "maclaw-companion.json"
	fileCompanionProjectPaperEntry   = "sidecars/paper-result.pdf"
	fileCompanionProjectMaxMarks     = 8 << 20
	fileCompanionProjectMaxDoc       = 8 << 30
	fileCompanionProjectMaxSidecar   = 512 << 20
	// Keep these aligned with normalizePdfDrawings in pdfDrawings.ts.
	fileCompanionProjectMaxDrawings      = 200
	fileCompanionProjectMaxDrawingPoints = 1200
)

var errFileCompanionNotProject = errors.New("不是伴读项目")

type fileCompanionProjectManifest struct {
	Kind       string          `json:"kind"`
	Version    int             `json:"version"`
	Name       string          `json:"name"`
	Highlights json.RawMessage `json:"highlights"`
	// ChatMarks are the host's highlights on the chat answers. They are
	// indices into Messages, so they travel with the transcript they mark.
	// A legacy project omits the field; nil means "leave the reader's marks".
	ChatMarks json.RawMessage `json:"chatMarks,omitempty"`
	// Annotations are the host's placed notes on the paper. Each carries its
	// page/rects, text, annotation body, and optional card place and size.
	Annotations json.RawMessage `json:"annotations,omitempty"`
	// Drawings are the host's freehand strokes and boxes. Points are page
	// fractions so they keep their place at any zoom.
	Drawings json.RawMessage `json:"drawings,omitempty"`
	// Bookmarks are named places on the paper. Array order is the order they
	// were added. A legacy project omits the field and leaves a reader's own
	// bookmarks alone.
	Bookmarks   json.RawMessage        `json:"bookmarks,omitempty"`
	Messages    []FileCompanionMessage `json:"messages"`
	PaperResult string                 `json:"paperResult,omitempty"`
	PaperTurn   int                    `json:"paperTurn,omitempty"`
}

type fileCompanionProjectMark struct {
	ID    string                     `json:"id"`
	Page  int                        `json:"page"`
	Color string                     `json:"color"`
	Rects []fileCompanionProjectRect `json:"rects"`
	Text  string                     `json:"text,omitempty"`
}

type fileCompanionProjectRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type fileCompanionProjectEntry struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// Overridable so tests can pick a destination without a desktop dialog.
var fileCompanionSaveProjectDialog = func(a *App, defaultFilename string) (string, error) {
	if a == nil || a.ctx == nil {
		return "", nil
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出项目",
		DefaultFilename: defaultFilename,
		Filters: []runtime.FileFilter{
			{DisplayName: "伴读项目 (*.zip)", Pattern: "*.zip"},
		},
	})
}

// FileCompanionExportProject packs the open document, its highlights, and the
// chat into a zip. An empty dialog result is a cancel, not an error. The open
// file is only read.
func (a *App) FileCompanionExportProject(path, highlightsJSON string) (string, error) {
	if a == nil {
		return "", errors.New("应用不可用")
	}
	grant, canonical, err := a.fileCompanionGrant(path)
	if err != nil || grant == nil {
		return "", errFileCompanionNotGranted
	}
	source := strings.TrimSpace(grant.Canonical)
	if source == "" {
		source = canonical
	}
	info, err := os.Stat(source)
	if err != nil || info.IsDir() {
		return "", errors.New("文件无法读取")
	}
	highlights, chatMarks, annotations, drawings, bookmarks, err := fileCompanionSplitSharePayload(highlightsJSON)
	if err != nil {
		return "", err
	}
	messages := a.fileCompanionHistory(fileCompanionSessionID(source))
	paperPath, paperTurn := fileCompanionProjectPaper(messages)
	dest, err := fileCompanionSaveProjectDialog(a, fileCompanionProjectFileName(source))
	if err != nil {
		return "", err
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", nil
	}
	if abs, absErr := filepath.Abs(dest); absErr == nil {
		dest = abs
	}
	// The dialog can return the open document itself. Refuse that spelling
	// before a missing .zip suffix turns it into a different path.
	if sameFileCompanionExportPath(source, dest) {
		return "", errors.New("不能覆盖当前文件")
	}
	if !strings.EqualFold(filepath.Ext(dest), ".zip") {
		dest += ".zip"
	}
	if stat, statErr := os.Stat(dest); statErr == nil && stat.IsDir() {
		return "", errors.New("无法保存到该位置")
	}
	if sameFileCompanionExportPath(source, dest) {
		return "", errors.New("不能覆盖当前文件")
	}
	if err := writeFileCompanionProject(dest, source, highlights, chatMarks, annotations, drawings, bookmarks, messages, paperPath, paperTurn); err != nil {
		return "", err
	}
	logFileCompanion("export project")
	return dest, nil
}

func fileCompanionProjectFileName(source string) string {
	base := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == ".." {
		base = "document"
	}
	runes := []rune(base)
	if len(runes) > 60 {
		base = string(runes[:60])
	}
	return base + "-伴读项目.zip"
}

func fileCompanionCleanHighlights(raw string) (json.RawMessage, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return json.RawMessage("[]"), nil
	}
	if len(raw) > fileCompanionProjectMaxMarks {
		return nil, errors.New("高亮无法保存")
	}
	var incoming []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &incoming); err != nil {
		return nil, errors.New("高亮无法保存")
	}
	cleaned := make([]fileCompanionProjectMark, 0, len(incoming))
	for _, item := range incoming {
		if len(cleaned) >= 2000 {
			break
		}
		var mark fileCompanionProjectMark
		if err := json.Unmarshal(item, &mark); err != nil {
			continue
		}
		next, ok := fileCompanionCleanMark(mark)
		if ok {
			cleaned = append(cleaned, next)
		}
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return nil, errors.New("高亮无法保存")
	}
	return encoded, nil
}

// fileCompanionChatMark is one highlight on the agent's answer. Message and
// the character range refer to the transcript in Messages; Text is the quote
// that lets a re-rendered answer re-anchor it.
type fileCompanionChatMark struct {
	Message int    `json:"message"`
	Color   string `json:"color"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Text    string `json:"text"`
}

func fileCompanionCleanChatMarks(raw json.RawMessage) (json.RawMessage, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("[]"), nil
	}
	if len(raw) > fileCompanionProjectMaxMarks {
		return nil, errors.New("回答标注无法保存")
	}
	var incoming []fileCompanionChatMark
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return nil, errors.New("回答标注无法保存")
	}
	cleaned := make([]fileCompanionChatMark, 0, len(incoming))
	for _, item := range incoming {
		if len(cleaned) >= 2000 {
			break
		}
		switch item.Color {
		case "yellow", "green", "blue", "pink":
		default:
			continue
		}
		if item.Message < 0 || item.Message > 100000 || item.Start < 0 || item.End <= item.Start {
			continue
		}
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		if runes := []rune(text); len(runes) > 2000 {
			text = string(runes[:2000])
		}
		item.Text = text
		cleaned = append(cleaned, item)
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return nil, errors.New("回答标注无法保存")
	}
	return encoded, nil
}

// fileCompanionCleanAnnotations validates the host's placed notes before they
// travel with a project. Translations are never shared; only kind
// "annotation" survives, and each note must keep a usable page/rect anchor.
func fileCompanionCleanAnnotations(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage("[]"), nil
	}
	if len(raw) > fileCompanionProjectMaxMarks {
		return nil, errors.New("批注无法保存")
	}
	var incoming []fileCompanionAnnotation
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return nil, errors.New("批注无法保存")
	}
	cleaned := make([]fileCompanionAnnotation, 0, len(incoming))
	for _, item := range incoming {
		if len(cleaned) >= 200 {
			break
		}
		if next, ok := fileCompanionCleanAnnotation(item); ok {
			cleaned = append(cleaned, next)
		}
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return nil, errors.New("批注无法保存")
	}
	return encoded, nil
}

type fileCompanionAnnotationRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type fileCompanionAnnotationPart struct {
	Page  int                           `json:"page"`
	Rects []fileCompanionAnnotationRect `json:"rects"`
}

type fileCompanionAnnotationPlace struct {
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Above bool    `json:"above,omitempty"`
}

type fileCompanionAnnotationSize struct {
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type fileCompanionAnnotation struct {
	ID          string                        `json:"id"`
	Kind        string                        `json:"kind"`
	Text        string                        `json:"text"`
	Translation string                        `json:"translation"`
	Parts       []fileCompanionAnnotationPart `json:"parts"`
	Place       *fileCompanionAnnotationPlace `json:"place,omitempty"`
	Size        *fileCompanionAnnotationSize  `json:"size,omitempty"`
}

func fileCompanionCleanAnnotation(item fileCompanionAnnotation) (fileCompanionAnnotation, bool) {
	if item.Kind != "annotation" {
		return fileCompanionAnnotation{}, false
	}
	id := strings.TrimSpace(item.ID)
	if id == "" || len(id) > 80 {
		return fileCompanionAnnotation{}, false
	}
	text := strings.TrimSpace(item.Text)
	if text == "" {
		return fileCompanionAnnotation{}, false
	}
	if runes := []rune(text); len(runes) > 4000 {
		text = string(runes[:4000])
	}
	body := strings.TrimSpace(item.Translation)
	if body == "" {
		return fileCompanionAnnotation{}, false
	}
	if runes := []rune(body); len(runes) > 12000 {
		body = string(runes[:12000])
	}
	parts := make([]fileCompanionAnnotationPart, 0, len(item.Parts))
	for _, part := range item.Parts {
		if len(parts) >= 30 {
			break
		}
		if part.Page < 1 || part.Page > 100000 {
			continue
		}
		rects := make([]fileCompanionAnnotationRect, 0, len(part.Rects))
		for _, rect := range part.Rects {
			if len(rects) >= 160 {
				break
			}
			if !fileCompanionRectOK(rect.X) || !fileCompanionRectOK(rect.Y) || !fileCompanionRectOK(rect.W) || !fileCompanionRectOK(rect.H) {
				continue
			}
			if rect.W < 0.001 || rect.H < 0.001 || rect.X+rect.W > 1.001 || rect.Y+rect.H > 1.001 {
				continue
			}
			rects = append(rects, rect)
		}
		if len(rects) > 0 {
			parts = append(parts, fileCompanionAnnotationPart{Page: part.Page, Rects: rects})
		}
	}
	if len(parts) == 0 {
		return fileCompanionAnnotation{}, false
	}
	next := fileCompanionAnnotation{ID: id, Kind: "annotation", Text: text, Translation: body, Parts: parts}
	if item.Place != nil && fileCompanionFinite(item.Place.X) && fileCompanionFinite(item.Place.Y) {
		x := math.Min(4, math.Max(-4, item.Place.X))
		y := math.Min(4, math.Max(-4, item.Place.Y))
		if math.Abs(x) >= 0.0005 || math.Abs(y) >= 0.0005 || item.Place.Above {
			next.Place = &fileCompanionAnnotationPlace{X: x, Y: y, Above: item.Place.Above}
		}
	}
	if item.Size != nil && fileCompanionFinite(item.Size.W) && fileCompanionFinite(item.Size.H) && item.Size.W >= 0.02 && item.Size.H >= 0.02 {
		next.Size = &fileCompanionAnnotationSize{W: math.Min(1, item.Size.W), H: math.Min(1, item.Size.H)}
	}
	return next, true
}

func fileCompanionFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

type fileCompanionDrawingPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type fileCompanionDrawing struct {
	ID     string                      `json:"id"`
	Page   int                         `json:"page"`
	Color  string                      `json:"color"`
	Tool   string                      `json:"tool"`
	Points []fileCompanionDrawingPoint `json:"points"`
}

// fileCompanionCleanDrawings validates freehand strokes before they travel
// with a project. A box keeps two corners; a line keeps its sampled points.
func fileCompanionCleanDrawings(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage("[]"), nil
	}
	if len(raw) > fileCompanionProjectMaxMarks {
		return nil, errors.New("手绘无法保存")
	}
	var incoming []json.RawMessage
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return nil, errors.New("手绘无法保存")
	}
	cleaned := make([]fileCompanionDrawing, 0, len(incoming))
	for _, rawItem := range incoming {
		if len(cleaned) >= fileCompanionProjectMaxDrawings {
			break
		}
		var item fileCompanionDrawing
		if err := json.Unmarshal(rawItem, &item); err != nil {
			continue
		}
		if next, ok := fileCompanionCleanDrawing(item); ok {
			cleaned = append(cleaned, next)
		}
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return nil, errors.New("手绘无法保存")
	}
	return encoded, nil
}

func fileCompanionCleanDrawing(item fileCompanionDrawing) (fileCompanionDrawing, bool) {
	switch item.Color {
	case "yellow", "green", "blue", "pink":
	default:
		return fileCompanionDrawing{}, false
	}
	if item.Tool != "line" && item.Tool != "box" {
		return fileCompanionDrawing{}, false
	}
	if item.Page < 1 || item.Page > 100000 {
		return fileCompanionDrawing{}, false
	}
	id := strings.TrimSpace(item.ID)
	if id == "" || len(id) > 80 {
		return fileCompanionDrawing{}, false
	}
	points := make([]fileCompanionDrawingPoint, 0, len(item.Points))
	for _, point := range item.Points {
		if len(points) >= fileCompanionProjectMaxDrawingPoints {
			break
		}
		if !fileCompanionFinite(point.X) || !fileCompanionFinite(point.Y) {
			continue
		}
		if point.X < 0 || point.X > 1 || point.Y < 0 || point.Y > 1 {
			continue
		}
		points = append(points, point)
	}
	if len(points) < 2 {
		return fileCompanionDrawing{}, false
	}
	if item.Tool == "box" {
		start, end := points[0], points[len(points)-1]
		if math.Abs(end.X-start.X) < 0.002 && math.Abs(end.Y-start.Y) < 0.002 {
			return fileCompanionDrawing{}, false
		}
		points = []fileCompanionDrawingPoint{start, end}
	}
	return fileCompanionDrawing{ID: id, Page: item.Page, Color: item.Color, Tool: item.Tool, Points: points}, true
}

// fileCompanionSplitSharePayload reads the companion marks payload the window
// sends with a share or an export:
// {"highlights":[],"chatMarks":[],"annotations":[],"drawings":[],"bookmarks":[]}.
// A legacy caller still sends the highlights array alone; that keeps working
// and leaves the other fields nil, so an old project does not clear a reader's
// own answer highlights, placed notes, drawings, or bookmarks.
func fileCompanionSplitSharePayload(raw string) (highlights, chatMarks, annotations, drawings, bookmarks json.RawMessage, err error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "{") {
		var payload struct {
			Highlights  json.RawMessage `json:"highlights"`
			ChatMarks   json.RawMessage `json:"chatMarks"`
			Annotations json.RawMessage `json:"annotations"`
			Drawings    json.RawMessage `json:"drawings"`
			Bookmarks   json.RawMessage `json:"bookmarks"`
		}
		if json.Unmarshal([]byte(trimmed), &payload) == nil && len(payload.Highlights) > 0 {
			highlights, err = fileCompanionCleanHighlights(string(payload.Highlights))
			if err != nil {
				return nil, nil, nil, nil, nil, err
			}
			if len(payload.ChatMarks) > 0 {
				chatMarks, err = fileCompanionCleanChatMarks(payload.ChatMarks)
				if err != nil {
					return nil, nil, nil, nil, nil, err
				}
			}
			if len(payload.Annotations) > 0 {
				annotations, err = fileCompanionCleanAnnotations(payload.Annotations)
				if err != nil {
					return nil, nil, nil, nil, nil, err
				}
			}
			if len(payload.Drawings) > 0 {
				drawings, err = fileCompanionCleanDrawings(payload.Drawings)
				if err != nil {
					return nil, nil, nil, nil, nil, err
				}
			}
			if len(payload.Bookmarks) > 0 {
				bookmarks, err = fileCompanionCleanBookmarks(payload.Bookmarks)
				if err != nil {
					return nil, nil, nil, nil, nil, err
				}
			}
			return highlights, chatMarks, annotations, drawings, bookmarks, nil
		}
	}
	highlights, err = fileCompanionCleanHighlights(raw)
	return highlights, nil, nil, nil, nil, err
}

type fileCompanionProjectBookmark struct {
	ID    string  `json:"id"`
	Page  int     `json:"page"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Color string  `json:"color"`
	Name  string  `json:"name"`
}

func fileCompanionBookmarkColor(color string) bool {
	switch color {
	case "yellow", "orange", "red", "pink", "purple", "blue", "teal", "green":
		return true
	default:
		return false
	}
}

// fileCompanionCleanBookmarks validates named places before they travel with
// a project. Order is the order they were added. A repeated id keeps that
// place and takes the later fields. Past 200 valid bookmarks, the oldest drop.
// The project size cap already bounds the raw list. The walk has to see every
// row: cutting a raw tail would drop a real bookmark that sits in front of a
// long run of rejected rows, and stopping at the 2000th valid row would drop
// a newer one.
func fileCompanionCleanBookmarks(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return json.RawMessage("[]"), nil
	}
	if len(raw) > fileCompanionProjectMaxMarks {
		return nil, errors.New("书签无法保存")
	}
	var incoming []fileCompanionProjectBookmark
	if err := json.Unmarshal(raw, &incoming); err != nil {
		return nil, errors.New("书签无法保存")
	}
	cleaned := make([]fileCompanionProjectBookmark, 0, len(incoming))
	// A repeated id is one bookmark. The later fields win and the first place stays.
	seen := make(map[string]int, len(incoming))
	for _, item := range incoming {
		if !fileCompanionBookmarkColor(item.Color) {
			continue
		}
		if item.Page < 1 || item.Page > 100000 {
			continue
		}
		if !fileCompanionFinite(item.X) || !fileCompanionFinite(item.Y) || item.X < 0 || item.Y < 0 || item.X > 1 || item.Y > 1 {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" || len(id) > 80 {
			continue
		}
		name := strings.Join(strings.Fields(item.Name), " ")
		runes := []rune(name)
		if len(runes) == 0 {
			continue
		}
		if len(runes) > 40 {
			name = string(runes[:40])
		}
		row := fileCompanionProjectBookmark{
			ID: id, Page: item.Page, X: item.X, Y: item.Y, Color: item.Color, Name: name,
		}
		if at, ok := seen[id]; ok {
			cleaned[at] = row
			continue
		}
		seen[id] = len(cleaned)
		cleaned = append(cleaned, row)
	}
	if len(cleaned) > 200 {
		cleaned = cleaned[len(cleaned)-200:]
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return nil, errors.New("书签无法保存")
	}
	return encoded, nil
}

func fileCompanionCleanMark(mark fileCompanionProjectMark) (fileCompanionProjectMark, bool) {
	switch mark.Color {
	case "yellow", "green", "blue", "pink":
	default:
		return fileCompanionProjectMark{}, false
	}
	if mark.Page < 1 || mark.Page > 100000 {
		return fileCompanionProjectMark{}, false
	}
	id := strings.TrimSpace(mark.ID)
	if id == "" || len(id) > 80 {
		return fileCompanionProjectMark{}, false
	}
	rects := make([]fileCompanionProjectRect, 0, len(mark.Rects))
	for _, rect := range mark.Rects {
		if len(rects) >= 40 {
			break
		}
		if !fileCompanionRectOK(rect.X) || !fileCompanionRectOK(rect.Y) || !fileCompanionRectOK(rect.W) || !fileCompanionRectOK(rect.H) {
			continue
		}
		if rect.W < 0.001 || rect.H < 0.001 || rect.X+rect.W > 1.001 || rect.Y+rect.H > 1.001 {
			continue
		}
		rects = append(rects, rect)
	}
	if len(rects) == 0 {
		return fileCompanionProjectMark{}, false
	}
	text := strings.TrimSpace(mark.Text)
	if len([]rune(text)) > 500 {
		text = string([]rune(text)[:500])
	}
	return fileCompanionProjectMark{ID: id, Page: mark.Page, Color: mark.Color, Rects: rects, Text: text}, true
}

func fileCompanionRectOK(value float64) bool {
	return !((value != value) || value < 0 || value > 1)
}

func fileCompanionProjectPaper(messages []FileCompanionMessage) (string, int) {
	seen := 0
	path := ""
	turn := 0
	for i := 0; i < len(messages)-1; i++ {
		if messages[i].Role != "user" || !messages[i].Paper || messages[i+1].Role != "assistant" {
			continue
		}
		seen++
		candidate := strings.TrimSpace(messages[i+1].ResultPath)
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		path = candidate
		turn = seen
	}
	return path, turn
}

func fileCompanionProjectSafeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "document"
	}
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, name)
	runes := []rune(cleaned)
	if len(runes) > 180 {
		cleaned = string(runes[:180])
	}
	if cleaned == "" {
		return "document"
	}
	return cleaned
}

func writeFileCompanionProject(dest, source string, highlights, chatMarks, annotations, drawings, bookmarks json.RawMessage, messages []FileCompanionMessage, paperPath string, paperTurn int) error {
	src, err := os.Open(source)
	if err != nil {
		return errors.New("文件无法读取")
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil || info.IsDir() {
		return errors.New("文件无法读取")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return errors.New("无法保存到该位置")
	}
	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return errors.New("无法保存到该位置")
	}
	failed := true
	defer func() {
		if failed {
			_ = os.Remove(tmp)
		}
	}()
	zw := zip.NewWriter(out)
	manifest := fileCompanionProjectManifest{
		Kind:        fileCompanionProjectKind,
		Version:     fileCompanionProjectVersion,
		Name:        filepath.Base(source),
		Highlights:  highlights,
		ChatMarks:   chatMarks,
		Annotations: annotations,
		Drawings:    drawings,
		Bookmarks:   bookmarks,
		Messages:    messages,
	}
	if paperPath != "" && paperTurn > 0 {
		manifest.PaperResult = fileCompanionProjectPaperEntry
		manifest.PaperTurn = paperTurn
	}
	if err := writeProjectJSON(zw, fileCompanionProjectManifestName, manifest); err != nil {
		_ = zw.Close()
		_ = out.Close()
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		_ = zw.Close()
		_ = out.Close()
		return errors.New("无法保存项目")
	}
	header.Name = "document/" + fileCompanionProjectSafeName(filepath.Base(source))
	header.Method = zip.Store
	dw, err := zw.CreateHeader(header)
	if err != nil {
		_ = zw.Close()
		_ = out.Close()
		return errors.New("无法保存项目")
	}
	copied, err := io.Copy(dw, io.LimitReader(src, fileCompanionProjectMaxDoc+1))
	if err != nil || copied > fileCompanionProjectMaxDoc {
		_ = zw.Close()
		_ = out.Close()
		return errors.New("文件过大")
	}
	if manifest.PaperResult != "" {
		if err := copyProjectFile(zw, manifest.PaperResult, paperPath, fileCompanionProjectMaxSidecar); err != nil {
			_ = zw.Close()
			_ = out.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		_ = out.Close()
		return errors.New("无法保存项目")
	}
	if err := out.Close(); err != nil {
		return errors.New("无法保存项目")
	}
	if err := replaceFileCompanionProject(tmp, dest); err != nil {
		return err
	}
	failed = false
	return nil
}

func writeProjectJSON(zw *zip.Writer, name string, value any) error {
	w, err := zw.Create(name)
	if err != nil {
		return errors.New("无法保存项目")
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return errors.New("无法保存项目")
	}
	return nil
}

func copyProjectFile(zw *zip.Writer, name, source string, limit int64) error {
	file, err := os.Open(source)
	if err != nil {
		return errors.New("无法保存项目")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() > limit {
		return errors.New("文件过大")
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return errors.New("无法保存项目")
	}
	header.Name = name
	header.Method = zip.Store
	w, err := zw.CreateHeader(header)
	if err != nil {
		return errors.New("无法保存项目")
	}
	n, err := io.Copy(w, io.LimitReader(file, limit+1))
	if err != nil || n > limit {
		return errors.New("文件过大")
	}
	return nil
}

func replaceFileCompanionProject(tmp, dest string) error {
	if err := os.Rename(tmp, dest); err == nil {
		return nil
	}
	_ = os.Remove(dest)
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return errors.New("无法保存到该位置")
	}
	return nil
}

func (a *App) fileCompanionOpenProjectZip(path string) (FileCompanionDocument, error) {
	doc := FileCompanionDocument{Path: path, Name: filepath.Base(path)}
	imported, err := a.importFileCompanionProject(path)
	if err != nil {
		if errors.Is(err, errFileCompanionNotProject) {
			doc.Error = errFileCompanionNotProject.Error()
		} else {
			doc.Error = err.Error()
		}
		return doc, nil
	}
	opened, err := a.fileCompanionOpen(imported.document, false)
	if err != nil {
		return opened, err
	}
	if opened.Error != "" {
		return opened, nil
	}
	opened.Highlights = imported.highlights
	opened.ChatMarks = imported.chatMarks
	opened.Annotations = imported.annotations
	opened.Drawings = imported.drawings
	opened.Bookmarks = imported.bookmarks
	opened.Imported = true
	logFileCompanion("import project")
	return opened, nil
}

// fileCompanionProjectImport is the document extracted from a project zip
// plus the reader marks the manifest carried.
type fileCompanionProjectImport struct {
	document    string
	highlights  json.RawMessage
	chatMarks   json.RawMessage
	annotations json.RawMessage
	drawings    json.RawMessage
	bookmarks   json.RawMessage
}

func (a *App) importFileCompanionProject(path string) (fileCompanionProjectImport, error) {
	if a == nil {
		return fileCompanionProjectImport{}, errors.New("应用不可用")
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		return fileCompanionProjectImport{}, errFileCompanionNotProject
	}
	defer reader.Close()
	var manifestFile *zip.File
	var documentFile *zip.File
	var paperFile *zip.File
	for _, file := range reader.File {
		kind, ok := fileCompanionProjectEntryKind(file.Name)
		if !ok {
			if strings.HasSuffix(file.Name, "/") {
				continue
			}
			return fileCompanionProjectImport{}, errFileCompanionNotProject
		}
		switch kind {
		case "manifest":
			if manifestFile != nil {
				return fileCompanionProjectImport{}, errFileCompanionNotProject
			}
			manifestFile = file
		case "document":
			if documentFile != nil {
				return fileCompanionProjectImport{}, errFileCompanionNotProject
			}
			documentFile = file
		case "paper":
			paperFile = file
		default:
			return fileCompanionProjectImport{}, errFileCompanionNotProject
		}
	}
	if manifestFile == nil || documentFile == nil {
		return fileCompanionProjectImport{}, errFileCompanionNotProject
	}
	if manifestFile.UncompressedSize64 > fileCompanionProjectMaxMarks || documentFile.UncompressedSize64 > fileCompanionProjectMaxDoc {
		return fileCompanionProjectImport{}, errors.New("文件过大")
	}
	manifest, err := readProjectManifest(manifestFile)
	if err != nil {
		return fileCompanionProjectImport{}, err
	}
	if manifest.PaperResult != "" && (paperFile == nil || paperFile.Name != fileCompanionProjectPaperEntry) {
		return fileCompanionProjectImport{}, errFileCompanionNotProject
	}
	root := filepath.Join(a.GetDataDir(), "file-companion", "projects")
	tmpDir, err := os.MkdirTemp(root, "incoming-")
	if err != nil {
		if mkErr := os.MkdirAll(root, 0o755); mkErr != nil {
			return fileCompanionProjectImport{}, errors.New("无法打开项目")
		}
		tmpDir, err = os.MkdirTemp(root, "incoming-")
		if err != nil {
			return fileCompanionProjectImport{}, errors.New("无法打开项目")
		}
	}
	defer os.RemoveAll(tmpDir)
	safeName := fileCompanionProjectSafeName(manifest.Name)
	if safeName == "document" {
		safeName = fileCompanionProjectSafeName(strings.TrimPrefix(strings.ReplaceAll(documentFile.Name, "\\", "/"), "document/"))
	}
	tmpFile := filepath.Join(tmpDir, safeName)
	sum, err := extractProjectFile(documentFile, tmpFile, fileCompanionProjectMaxDoc)
	if err != nil {
		return fileCompanionProjectImport{}, err
	}
	finalDir := filepath.Join(root, hex.EncodeToString(sum[:16]))
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		return fileCompanionProjectImport{}, errors.New("无法打开项目")
	}
	finalPath := filepath.Join(finalDir, safeName)
	_ = os.Remove(finalPath)
	if err := os.Rename(tmpFile, finalPath); err != nil {
		if copyErr := copyPlainFile(tmpFile, finalPath); copyErr != nil {
			return fileCompanionProjectImport{}, errors.New("无法打开项目")
		}
	}
	identity, _, idErr := fileCompanionPathIdentity(finalPath)
	if idErr != nil {
		return fileCompanionProjectImport{}, errors.New("无法打开项目")
	}
	sessionID := fileCompanionSessionID(identity)
	if err := a.writeFileCompanionProjectSession(sessionID, manifest.Messages); err != nil {
		return fileCompanionProjectImport{}, err
	}
	if manifest.PaperResult != "" && paperFile != nil && manifest.PaperTurn > 0 {
		if err := a.installFileCompanionProjectPaper(sessionID, paperFile, manifest.PaperTurn); err != nil {
			return fileCompanionProjectImport{}, err
		}
	}
	highlights := manifest.Highlights
	if len(highlights) == 0 {
		highlights = json.RawMessage("[]")
	}
	return fileCompanionProjectImport{
		document:    identity,
		highlights:  highlights,
		chatMarks:   manifest.ChatMarks,
		annotations: manifest.Annotations,
		drawings:    manifest.Drawings,
		bookmarks:   manifest.Bookmarks,
	}, nil
}

func fileCompanionProjectEntryKind(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "../") {
		return "", false
	}
	if strings.HasSuffix(name, "/") {
		return "", false
	}
	if name == fileCompanionProjectManifestName {
		return "manifest", true
	}
	if name == fileCompanionProjectPaperEntry {
		return "paper", true
	}
	if strings.HasPrefix(name, "document/") {
		rest := strings.TrimPrefix(name, "document/")
		if rest == "" || strings.Contains(rest, "/") || rest == "." || rest == ".." {
			return "", false
		}
		return "document", true
	}
	return "", false
}

func readProjectManifest(file *zip.File) (fileCompanionProjectManifest, error) {
	var manifest fileCompanionProjectManifest
	rc, err := file.Open()
	if err != nil {
		return manifest, errFileCompanionNotProject
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, fileCompanionProjectMaxMarks+1))
	if err != nil || int64(len(raw)) > fileCompanionProjectMaxMarks {
		return manifest, errFileCompanionNotProject
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, errFileCompanionNotProject
	}
	if manifest.Kind != fileCompanionProjectKind || manifest.Version != fileCompanionProjectVersion {
		return manifest, errFileCompanionNotProject
	}
	if len(manifest.Highlights) == 0 {
		manifest.Highlights = json.RawMessage("[]")
	}
	cleaned, err := fileCompanionCleanHighlights(string(manifest.Highlights))
	if err != nil {
		return manifest, errFileCompanionNotProject
	}
	manifest.Highlights = cleaned
	if len(manifest.Bookmarks) > 0 {
		bookmarks, bookmarkErr := fileCompanionCleanBookmarks(manifest.Bookmarks)
		if bookmarkErr != nil {
			return manifest, errFileCompanionNotProject
		}
		manifest.Bookmarks = bookmarks
	}
	return manifest, nil
}

func extractProjectFile(file *zip.File, dest string, limit int64) ([32]byte, error) {
	var sum [32]byte
	if file.UncompressedSize64 > uint64(limit) {
		return sum, errors.New("文件过大")
	}
	rc, err := file.Open()
	if err != nil {
		return sum, errFileCompanionNotProject
	}
	defer rc.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return sum, errors.New("无法打开项目")
	}
	defer out.Close()
	hash := sha256.New()
	n, err := io.Copy(out, io.TeeReader(io.LimitReader(rc, limit+1), hash))
	if err != nil {
		return sum, errors.New("无法打开项目")
	}
	if n > limit {
		return sum, errors.New("文件过大")
	}
	copy(sum[:], hash.Sum(nil))
	return sum, nil
}

func copyPlainFile(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func (a *App) writeFileCompanionProjectSession(sessionID string, messages []FileCompanionMessage) error {
	sessionID = strings.TrimSpace(sessionID)
	if a == nil || sessionID == "" {
		return errors.New("无法打开项目")
	}
	userID := "file-companion:" + sessionID
	// History reads the in-memory copy first. Clear also removes the detached
	// record and unbinds it. The replacement bumps the save generation and writes
	// the sharer's transcript before it releases the lock, so a turn that
	// already loaded the old chat cannot write that chat back. Cancel comes after
	// the bump: a save from the turn being stopped then sees the new generation
	// and is dropped.
	if err := a.ensureFileCompanionHandler(userID, sessionID); err != nil {
		return errors.New("无法打开项目")
	}
	stored := fileCompanionProjectEntries(messages)
	entries := make([]agent.ConversationEntry, 0, len(stored))
	for _, entry := range stored {
		entries = append(entries, agent.ConversationEntry{
			Role:             entry.Role,
			Content:          entry.Content,
			ReasoningContent: entry.ReasoningContent,
		})
	}
	if !a.fileCompanionReplaceChat(userID, entries) {
		return errors.New("无法打开项目")
	}
	if a.imHandler != nil {
		_, _ = a.imHandler.RequestCancelSessionForUser(userID)
	}
	return nil
}

func fileCompanionProjectEntries(messages []FileCompanionMessage) []fileCompanionProjectEntry {
	if len(messages) == 0 {
		return []fileCompanionProjectEntry{}
	}
	out := make([]fileCompanionProjectEntry, 0, len(messages))
	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := message.Text
		if role == "user" && message.Paper && !strings.Contains(content, fileCompanionPaperMarker) {
			content = strings.TrimSpace(content) + "\n" + fileCompanionPaperMarker
		}
		reasoning := ""
		if role == "assistant" {
			reasoning = strings.TrimSpace(message.Reasoning)
		}
		if strings.TrimSpace(content) == "" && reasoning == "" {
			continue
		}
		out = append(out, fileCompanionProjectEntry{Role: role, Content: content, ReasoningContent: reasoning})
	}
	return out
}

func (a *App) installFileCompanionProjectPaper(sessionID string, file *zip.File, turn int) error {
	if file.UncompressedSize64 > fileCompanionProjectMaxSidecar {
		return errors.New("文件过大")
	}
	dir := filepath.Join(a.GetDataDir(), "file-companion", "results", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.New("无法打开项目")
	}
	pdfPath := filepath.Join(dir, "论文解读.pdf")
	if _, err := extractProjectFile(file, pdfPath, fileCompanionProjectMaxSidecar); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.txt"), []byte(pdfPath), 0o644); err != nil {
		return errors.New("无法打开项目")
	}
	if turn < 1 {
		turn = 1
	}
	if err := os.WriteFile(filepath.Join(dir, "latest-turn.txt"), []byte(itoaPositive(turn)), 0o644); err != nil {
		return errors.New("无法打开项目")
	}
	return nil
}

func itoaPositive(n int) string {
	if n < 1 {
		return "1"
	}
	digits := make([]byte, 0, 8)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

func marshalProjectJSON(value any) ([]byte, error) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(buf.String(), "\n")), nil
}

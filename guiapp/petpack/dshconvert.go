package petpack

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	dshMaxManifestBytes = 1 << 20
	dshMaxAtlasBytes    = 32 << 20
	dshMaxAtlasEdge     = 4096
	dshMaxAtlasPixels   = 8_000_000

	dshRejectLive2D  = "这是 Live2D / Cubism 或 frames2d 宠物，MaClaw 不能播放。请先自行导出精灵图集，再用本转换器。v1 不会在桌面里渲染 Live2D。"
	dshRejectVideo   = "视频和 GIF 播放不能转换。只接受 pet.json 加一张本地 spritesheet.png 或 webp。"
	dshRejectScript  = "JS 挂件和纯 SVG 宠物不能转换。"
	dshRejectNoJSON  = "目录里没有 pet.json，无法识别为精灵宠物。"
	dshRejectPath    = "图集路径必须是宠物目录内的相对路径，不能是绝对路径、URL 或包含 .. 。转换器不会下载文件。"
	dshRejectSize    = "图集超过允许的解码上限，已拒绝。"
	dshRejectGrid    = "spritesheet 尺寸与 frame 的宽×列、高×行不一致。"
	dshRejectNoFrame = "缺少 frame，且图集不是 1536×1872 或 1536×2288，无法安全切格。"
	dshRejectNoIdle  = "图集没有可用的不透明待机帧，无法生成宠物包。"
)

// DSHConvertRequest selects one local sprite folder and where to write the user pack.
type DSHConvertRequest struct {
	InputDir  string
	OutputDir string
	ZipPath   string
}

// DSHConvertReport is the machine-readable result printed by the CLI.
type DSHConvertReport struct {
	OK             bool              `json:"ok"`
	PackID         string            `json:"pack_id,omitempty"`
	OutputDir      string            `json:"output_dir,omitempty"`
	ZipPath        string            `json:"zip_path,omitempty"`
	RejectedReason string            `json:"rejected_reason,omitempty"`
	ValidatorError string            `json:"validator_error,omitempty"`
	Warnings       []string          `json:"warnings,omitempty"`
	Rows           []DSHRowReport    `json:"rows,omitempty"`
	KeptFrames     int               `json:"kept_frames,omitempty"`
	DroppedFrames  []DSHDroppedFrame `json:"dropped_frames,omitempty"`
	MirroredLeft   bool              `json:"mirrored_left,omitempty"`
	TextureScale   float64           `json:"texture_scale,omitempty"`
}

// DSHRowReport describes one source row after the frame budget.
type DSHRowReport struct {
	SourceRow int     `json:"source_row"`
	Name      string  `json:"name,omitempty"`
	Semantic  string  `json:"semantic"`
	Kept      int     `json:"kept"`
	Dropped   int     `json:"dropped"`
	Cue       string  `json:"cue,omitempty"`
	TravelX   float64 `json:"travel_x,omitempty"`
}

// DSHDroppedFrame records why one atlas cell was not kept.
type DSHDroppedFrame struct {
	Row    int    `json:"row"`
	Column int    `json:"column"`
	Reason string `json:"reason"`
}

// ConvertDSHPet reads a local DeepSeek Harness / Codex sprite folder and writes
// a MaClaw native-character user pack. It does not install the pack or open a window.
func ConvertDSHPet(req DSHConvertRequest) (DSHConvertReport, error) {
	var report DSHConvertReport
	root := strings.TrimSpace(req.InputDir)
	if root == "" {
		return report, errors.New("input directory is empty")
	}
	info, err := os.Stat(root)
	if err != nil {
		return report, err
	}
	if !info.IsDir() {
		return report, fmt.Errorf("input is not a directory")
	}
	parsed, err := readDSHPet(root)
	if err != nil {
		report.RejectedReason, _ = classifyDSHError(err)
		return report, err
	}
	built, err := buildDSHPack(parsed)
	if err != nil {
		if built != nil {
			report.Rows = built.rows
			report.DroppedFrames = built.dropped
			report.KeptFrames = built.kept
			report.MirroredLeft = built.mirrored
			report.PackID = built.report.PackID
		}
		report.RejectedReason, report.ValidatorError = classifyDSHError(err)
		return report, err
	}
	report = built.report
	if err := writeDSHPack(req, built); err != nil {
		report.OK = false
		report.OutputDir = ""
		report.ZipPath = ""
		// Disk failures stay IO (exit 1). They are not a family rejection and not a
		// rig validator failure — those are reported from buildDSHPack.
		report.RejectedReason, _ = classifyDSHError(err)
		return report, err
	}
	report.OK = true
	report.OutputDir = built.report.OutputDir
	report.ZipPath = built.report.ZipPath
	return report, nil
}

type dshReject struct{ Reason string }

func (e *dshReject) Error() string { return e.Reason }

func rejectDSH(reason string) error { return &dshReject{Reason: reason} }

// classifyDSHError splits a user-facing family rejection from a validator or IO error.
// Only a *dshReject fills RejectedReason. Everything else is ValidatorError when the
// caller is reporting a failed build; ConvertDSHPet leaves IO from readDSHPet as a bare error.
func classifyDSHError(err error) (rejected, validator string) {
	if err == nil {
		return "", ""
	}
	var rej *dshReject
	if errors.As(err, &rej) {
		return rej.Reason, ""
	}
	return "", err.Error()
}

type dshPetFile struct {
	ID              string          `json:"id"`
	DisplayName     string          `json:"displayName"`
	Description     string          `json:"description"`
	SpritesheetPath string          `json:"spritesheetPath"`
	Renderer        string          `json:"renderer"`
	Kind            string          `json:"kind"`
	Frame           *dshFrameSpec   `json:"frame"`
	Live2D          json.RawMessage `json:"live2d"`
	Animations      json.RawMessage `json:"animations"`
	Tracks          json.RawMessage `json:"tracks"`
	Sequences       json.RawMessage `json:"sequences"`
}

type dshFrameSpec struct {
	Width   int `json:"width"`
	Height  int `json:"height"`
	Columns int `json:"columns"`
	Rows    int `json:"rows"`
}

type dshParsed struct {
	id          string
	displayName string
	description string
	columns     int
	rows        int
	cellW       int
	cellH       int
	sheet       *image.NRGBA
	named       []dshNamedRow
	warnings    []string
}

type dshNamedRow struct {
	row      int
	semantic string
	columns  []int // nil means every column
}

func readDSHPet(root string) (*dshParsed, error) {
	manifestPath := filepath.Join(root, "pet.json")
	raw, err := readLimited(manifestPath, dshMaxManifestBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, rejectDSH(dshRejectNoJSON)
		}
		return nil, err
	}
	var doc dshPetFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse pet.json: %w", err)
	}
	if dshLive2D(doc) {
		return nil, rejectDSH(dshRejectLive2D)
	}
	switch strings.ToLower(strings.TrimSpace(doc.Renderer)) {
	case "", "spritesheet", "atlas", "codex", "sprite":
	default:
		return nil, fmt.Errorf("不认识的 renderer %q，只接受 spritesheet、atlas、codex 或 sprite", doc.Renderer)
	}
	sheetRel := strings.TrimSpace(doc.SpritesheetPath)
	if sheetRel == "" {
		if dshScriptish(string(raw)) {
			return nil, rejectDSH(dshRejectScript)
		}
		return nil, rejectDSH(dshRejectPath)
	}
	if videoExt(sheetRel) {
		return nil, rejectDSH(dshRejectVideo)
	}
	if scriptExt(sheetRel) {
		return nil, rejectDSH(dshRejectScript)
	}
	lower := strings.ToLower(sheetRel)
	if !strings.HasSuffix(lower, ".png") && !strings.HasSuffix(lower, ".webp") {
		return nil, rejectDSH(dshRejectPath)
	}
	full, err := dshSpriteFile(root, sheetRel)
	if err != nil {
		return nil, err
	}
	sheet, err := decodeDSHAtlas(full)
	if err != nil {
		return nil, err
	}
	cols, rows, cellW, cellH, err := resolveDSHGrid(doc.Frame, sheet.Bounds().Dx(), sheet.Bounds().Dy())
	if err != nil {
		return nil, err
	}
	named, warnings, err := resolveDSHRows(doc, rows, cols)
	if err != nil {
		return nil, err
	}
	return &dshParsed{
		id:          doc.ID,
		displayName: doc.DisplayName,
		description: doc.Description,
		columns:     cols,
		rows:        rows,
		cellW:       cellW,
		cellH:       cellH,
		sheet:       sheet,
		named:       named,
		warnings:    warnings,
	}, nil
}

func dshLive2D(doc dshPetFile) bool {
	kind := strings.ToLower(strings.TrimSpace(doc.Renderer))
	if kind == "" {
		kind = strings.ToLower(strings.TrimSpace(doc.Kind))
	}
	switch kind {
	case "live2d", "frames2d":
		return true
	}
	if len(bytes.TrimSpace(doc.Live2D)) > 0 && string(bytes.TrimSpace(doc.Live2D)) != "null" {
		return true
	}
	path := strings.ToLower(doc.SpritesheetPath)
	for _, ext := range []string{".model3.json", ".moc3", ".exp3.json", ".physics3.json"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

func dshScriptish(body string) bool {
	lower := strings.ToLower(body)
	for _, ext := range []string{".js", ".mjs", ".html", ".wasm", ".svg"} {
		if strings.Contains(lower, ext) {
			return true
		}
	}
	return false
}

func videoExt(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range []string{".mp4", ".webm", ".mov", ".gif", ".avi", ".mkv"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func scriptExt(path string) bool {
	lower := strings.ToLower(path)
	for _, ext := range []string{".js", ".mjs", ".html", ".wasm", ".svg"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func dshSpriteFile(root, rel string) (string, error) {
	clean := safeRel(rel)
	if clean == "" || strings.Contains(strings.ToLower(rel), "http:") || strings.Contains(strings.ToLower(rel), "https:") || strings.Contains(strings.ToLower(rel), "file:") {
		return "", rejectDSH(dshRejectPath)
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	fullReal, err := filepath.EvalSymlinks(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("spritesheet not found")
		}
		return "", err
	}
	if err := pathUnderRoot(rootReal, fullReal); err != nil {
		return "", rejectDSH(dshRejectPath)
	}
	return fullReal, nil
}

func readLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		if limit == dshMaxManifestBytes {
			return nil, fmt.Errorf("pet.json 超过 1 MiB，已拒绝")
		}
		return nil, rejectDSH(dshRejectSize)
	}
	return io.ReadAll(io.LimitReader(f, limit+1))
}

func decodeDSHAtlas(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > dshMaxAtlasBytes {
		return nil, rejectDSH(dshRejectSize)
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("decode spritesheet: %w", err)
	}
	if cfg.Width > dshMaxAtlasEdge || cfg.Height > dshMaxAtlasEdge || cfg.Width*cfg.Height > dshMaxAtlasPixels {
		return nil, rejectDSH(dshRejectSize)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(io.LimitReader(f, dshMaxAtlasBytes))
	if err != nil {
		return nil, fmt.Errorf("decode spritesheet: %w", err)
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	return out, nil
}

func resolveDSHGrid(frame *dshFrameSpec, width, height int) (cols, rows, cellW, cellH int, err error) {
	if frame == nil {
		switch {
		case width == 1536 && height == 1872:
			return 8, 9, 192, 208, nil
		case width == 1536 && height == 2288:
			return 8, 11, 192, 208, nil
		default:
			return 0, 0, 0, 0, rejectDSH(dshRejectNoFrame)
		}
	}
	if frame.Width < 1 || frame.Height < 1 || frame.Columns < 1 || frame.Rows < 1 {
		return 0, 0, 0, 0, rejectDSH(dshRejectGrid)
	}
	if frame.Width*frame.Columns != width || frame.Height*frame.Rows != height {
		return 0, 0, 0, 0, rejectDSH(dshRejectGrid)
	}
	return frame.Columns, frame.Rows, frame.Width, frame.Height, nil
}

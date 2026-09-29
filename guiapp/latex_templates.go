package guiapp

// LaTeX paper templates: the desktop half of the HubCenter template library.
//
// A template is always a .zip package so the same artifact moves in both
// directions: a user imports one from disk, the app stores it under
// <data>/latex-templates/, and "share" uploads the very same package to
// HubCenter where an administrator reviews it before other users can install it.
//
// The local index (index.json) is deliberately tiny metadata; the authoritative
// content is the extracted pack directory, which is also what a new document is
// materialised from.

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	latexTemplateManifestName     = "template.json"
	latexTemplateIndexFileName    = "index.json"
	latexTemplateMaxZipBytes      = 100 << 20
	latexTemplateMaxFiles         = 4000
	latexTemplateManifestMaxBytes = 256 << 10
	latexTemplateUnpackedMaxBytes = 256 << 20
	latexTemplateNameMaxRunes     = 80

	// latexTemplateSourceBlank marks the implicit "no template" choice. A new
	// LaTeX task starts from a bare article skeleton so the expert can build
	// the structure itself, and a template is opt-in.
	latexTemplateSourceBlank = "blank"
	latexTemplateSourceLocal = "local"
	latexTemplateSourceHub   = "hub"
)

// LatexTemplateCategory is a library section. The four product categories are
// seeded locally so the conference / journal / thesis / other grouping works
// before the first Hub sync, and administrator-added categories merge on top.
type LatexTemplateCategory struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	Builtin     bool   `json:"builtin"`
}

// LatexTemplate is one installed template pack.
type LatexTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CategoryID  string `json:"category_id"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	MainFile    string `json:"main_file"`
	FileCount   int    `json:"file_count"`
	SizeBytes   int64  `json:"size_bytes"`
	Source      string `json:"source"`
	RemoteID    string `json:"remote_id,omitempty"`
	// ShareStatus mirrors the Hub review state of a locally shared template:
	// "" (never shared), pending, approved, rejected.
	ShareStatus string `json:"share_status,omitempty"`
	// ShareNote is the moderation note left by the Hub administrator.
	ShareNote string `json:"share_note,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type latexTemplateIndex struct {
	Categories []LatexTemplateCategory `json:"categories"`
	Templates  []LatexTemplate         `json:"templates"`
}

type latexTemplatePackageManifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	MainFile    string `json:"main_file"`
}

var (
	latexTemplateMu sync.Mutex
	latexTemplateID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	// documentclassRe finds the entry point of a template: the .tex file that
	// declares the document class. It is the same marker tinytex.ResolveMain
	// uses, so a pack and a compiled project agree on the main file.
	documentclassRe = regexp.MustCompile(`\\documentclass`)
)

var builtinLatexTemplateCategories = []LatexTemplateCategory{
	{ID: "conference", Name: "会议", Description: "会议投稿与 camera-ready 模板", SortOrder: 10, Builtin: true},
	{ID: "journal", Name: "期刊", Description: "期刊投稿格式模板", SortOrder: 20, Builtin: true},
	{ID: "thesis", Name: "毕业论文", Description: "学位论文、开题与答辩模板", SortOrder: 30, Builtin: true},
	{ID: "other", Name: "其它", Description: "其它 LaTeX 文档模板", SortOrder: 40, Builtin: true},
}

// latexBlankTemplateSource is the default document skeleton. It is deliberately
// minimal: the LaTeX expert owns structure and content, and a template is an
// explicit user choice rather than an implicit starting point.
const latexBlankTemplateSource = `\documentclass[11pt,a4paper]{article}
\usepackage[margin=2.5cm]{geometry}
\usepackage{amsmath,amssymb,amsthm}
\usepackage{graphicx}
\usepackage{hyperref}

\title{Title}
\author{Author}
\date{\today}

\begin{document}
\maketitle

\section{Introduction}

\section{Related Work}

\section{Method}

\section{Experiments}

\section{Conclusion}

\bibliographystyle{plain}
% \bibliography{references}

\end{document}
`

func (a *App) latexTemplateRoot() string {
	return filepath.Join(a.GetDataDir(), "latex-templates")
}

func (a *App) latexTemplatePackDir(id string) string {
	return filepath.Join(a.latexTemplateRoot(), "packs", id)
}

func (a *App) latexTemplateZipDir() string {
	return filepath.Join(a.latexTemplateRoot(), "zips")
}

func (a *App) latexTemplateIndexPath() string {
	return filepath.Join(a.latexTemplateRoot(), latexTemplateIndexFileName)
}

// latexBlankTemplate is the implicit "no template" entry every library view
// lists first, so the default is always one click away.
func latexBlankTemplate() LatexTemplate {
	now := latexTemplateNow()
	return LatexTemplate{
		ID:          latexTemplateSourceBlank,
		Name:        "空白模板",
		Description: "不套用任何模板，从最小 article 骨架开始",
		CategoryID:  "other",
		Version:     "1.0.0",
		Author:      "MaClaw",
		MainFile:    "main.tex",
		FileCount:   1,
		Source:      latexTemplateSourceBlank,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func latexTemplateNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// readLatexTemplateIndex returns the effective index. Reading an absent or
// corrupt index file falls back to the seeded default rather than failing: the
// template library is a convenience surface and must never block startup.
func (a *App) readLatexTemplateIndex() latexTemplateIndex {
	raw, err := os.ReadFile(a.latexTemplateIndexPath())
	if err != nil {
		return a.defaultLatexTemplateIndex()
	}
	var stored latexTemplateIndex
	if err := json.Unmarshal(raw, &stored); err != nil {
		log.Printf("[latex_template] index unreadable, using defaults: %v", err)
		return a.defaultLatexTemplateIndex()
	}
	// Merge rather than replace: a builtin category introduced by a later
	// release must appear even in an index written by an earlier one.
	byID := make(map[string]LatexTemplateCategory, len(stored.Categories)+len(builtinLatexTemplateCategories))
	for _, category := range stored.Categories {
		if strings.TrimSpace(category.ID) == "" {
			continue
		}
		byID[category.ID] = category
	}
	for _, category := range builtinLatexTemplateCategories {
		existing, ok := byID[category.ID]
		if !ok {
			byID[category.ID] = category
			continue
		}
		// Ids and ordering are product-owned; only the display text is
		// administrator-editable, so reassert them on every read.
		existing.SortOrder = category.SortOrder
		existing.Builtin = true
		if strings.TrimSpace(existing.Name) == "" {
			existing.Name = category.Name
		}
		if strings.TrimSpace(existing.Description) == "" {
			existing.Description = category.Description
		}
		byID[category.ID] = existing
	}
	categories := make([]LatexTemplateCategory, 0, len(byID))
	for _, category := range byID {
		categories = append(categories, category)
	}
	sortLatexTemplateCategories(categories)

	templates := make([]LatexTemplate, 0, len(stored.Templates)+1)
	for _, item := range stored.Templates {
		if item.ID == latexTemplateSourceBlank || !latexTemplateID.MatchString(item.ID) {
			continue
		}
		templates = append(templates, item)
	}
	sortLatexTemplateTemplates(templates, categories)
	return latexTemplateIndex{
		Categories: categories,
		Templates:  append([]LatexTemplate{latexBlankTemplate()}, templates...),
	}
}

func (a *App) defaultLatexTemplateIndex() latexTemplateIndex {
	return latexTemplateIndex{
		Categories: append([]LatexTemplateCategory(nil), builtinLatexTemplateCategories...),
		Templates:  []LatexTemplate{latexBlankTemplate()},
	}
}

func sortLatexTemplateCategories(categories []LatexTemplateCategory) {
	rank := map[string]int{"conference": 0, "journal": 1, "thesis": 2, "other": 3}
	sort.SliceStable(categories, func(i, j int) bool {
		left, leftOK := rank[categories[i].ID]
		right, rightOK := rank[categories[j].ID]
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK && rightOK && left != right {
			return left < right
		}
		if categories[i].SortOrder != categories[j].SortOrder {
			return categories[i].SortOrder < categories[j].SortOrder
		}
		return categories[i].ID < categories[j].ID
	})
}

func sortLatexTemplateTemplates(templates []LatexTemplate, categories []LatexTemplateCategory) {
	order := make(map[string]int, len(categories))
	for position, category := range categories {
		order[category.ID] = position
	}
	sort.SliceStable(templates, func(i, j int) bool {
		left, leftOK := order[templates[i].CategoryID]
		right, rightOK := order[templates[j].CategoryID]
		if leftOK != rightOK {
			return leftOK
		}
		if leftOK && rightOK && left != right {
			return left < right
		}
		return templates[i].Name < templates[j].Name
	})
}

func (a *App) writeLatexTemplateIndex(index latexTemplateIndex) error {
	if err := os.MkdirAll(a.latexTemplateRoot(), 0o755); err != nil {
		return err
	}
	sortLatexTemplateCategories(index.Categories)
	sortLatexTemplateTemplates(index.Templates, index.Categories)
	raw, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	path := a.latexTemplateIndexPath()
	temp := path + ".tmp"
	if err := os.WriteFile(temp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func findLatexTemplate(index latexTemplateIndex, id string) (LatexTemplate, bool) {
	for _, item := range index.Templates {
		if item.ID == id {
			return item, true
		}
	}
	return LatexTemplate{}, false
}

func upsertLatexTemplate(index *latexTemplateIndex, item LatexTemplate) {
	for position := range index.Templates {
		if index.Templates[position].ID == item.ID {
			index.Templates[position] = item
			return
		}
	}
	index.Templates = append(index.Templates, item)
}

func latexTemplateCategoryKnown(index latexTemplateIndex, categoryID string) bool {
	if strings.TrimSpace(categoryID) == "" {
		return false
	}
	for _, category := range index.Categories {
		if category.ID == categoryID {
			return true
		}
	}
	return false
}

// --- Package inspection and extraction ---

// latexTemplateDirectoryEntry reports a zip directory. Official packages store
// the wrapper as its own entry ("elsarticle/"), and that name is not a file.
func latexTemplateDirectoryEntry(file *zip.File) bool {
	if file.FileInfo().IsDir() {
		return true
	}
	return strings.HasSuffix(strings.ReplaceAll(file.Name, "\\", "/"), "/")
}

// latexTemplateSafeEntryName rejects the archive shapes that would let a shared
// package escape its pack directory: absolute paths, drive letters, and parent
// traversal.
func latexTemplateSafeEntryName(name string) (string, error) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	if cleaned == "" {
		return "", errors.New("template package contains an empty file name")
	}
	if strings.HasPrefix(cleaned, "/") {
		return "", errors.New("template package contains an absolute path")
	}
	if len(cleaned) >= 2 && cleaned[1] == ':' {
		return "", errors.New("template package contains an absolute path")
	}
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == ".." {
			return "", errors.New("template package contains a path outside the template")
		}
	}
	trimmed := strings.TrimPrefix(cleaned, "./")
	if trimmed == "" || strings.HasSuffix(trimmed, "/") {
		return "", errors.New("template package contains an empty file name")
	}
	return filepath.FromSlash(trimmed), nil
}

// latexTemplateAllowedExt keeps a template to text sources, styles,
// bibliographies and figures. Executables and scripts have no place in a paper
// template and would only be a way to smuggle something onto a user's disk.
// .dtx/.ins/.drv are the documented sources publisher classes ship beside the
// .cls. They are TeX input, not programs.
var latexTemplateAllowedExt = map[string]bool{
	".tex": true, ".ltx": true, ".latex": true, ".sty": true, ".cls": true,
	".dtx": true, ".ins": true, ".drv": true,
	".bst": true, ".bbx": true, ".cbx": true, ".dbx": true, ".lbx": true, ".bib": true, ".bbl": true,
	".def": true, ".fd": true, ".clo": true, ".cfg": true, ".ldf": true, ".lco": true, ".rtx": true, ".ist": true,
	".lua": true,
	".txt": true, ".md": true, ".csv": true, ".tsv": true, ".json": true,
	".yml": true, ".yaml": true, ".toml": true, ".svg": true, ".eps": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".pdf": true,
	".tikz": true, ".mk": true, ".make": true,
}

// latexTemplateExtensionlessNames are the documentation and doc-build files a
// publisher zip ships without a suffix. Only these exact base names pass.
var latexTemplateExtensionlessNames = map[string]bool{
	"readme": true, "license": true, "licence": true, "copying": true,
	"changes": true, "changelog": true, "authors": true, "notice": true,
	"makefile": true, "gnumakefile": true,
}

func latexTemplateFileAllowed(name, ext string) bool {
	if latexTemplateAllowedExt[strings.ToLower(ext)] {
		return true
	}
	return ext == "" && latexTemplateExtensionlessNames[strings.ToLower(filepath.Base(name))]
}

// latexTemplateDocSource reports a file that lives in a package's manual
// directory. Publisher zips put the class manual next to the sample article,
// and both declare \documentclass.
func latexTemplateDocSource(name string) bool {
	for _, segment := range strings.Split(strings.ToLower(strings.ReplaceAll(name, "\\", "/")), "/") {
		switch segment {
		case "doc", "docs", "documentation", "docsrc":
			return true
		}
	}
	return false
}

// inspectLatexTemplatePackage validates a zip and returns its manifest plus the
// number of .tex sources it carries.
func inspectLatexTemplatePackage(data []byte, fallbackName string) (latexTemplatePackageManifest, int, error) {
	var out latexTemplatePackageManifest
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return out, 0, errors.New("不是有效的 zip 模板包")
	}
	if len(zr.File) == 0 {
		return out, 0, errors.New("模板包为空")
	}
	if len(zr.File) > latexTemplateMaxFiles {
		return out, 0, errors.New("模板包文件过多")
	}
	texFiles := make([]string, 0, 8)
	// Template archives are usually built with a single top-level folder. It is
	// stripped here as well as during extraction, so the manifest is found and the
	// recorded main_file is relative to the pack the user will actually get.
	root := latexTemplateArchiveRoot(zr)
	for _, file := range zr.File {
		if latexTemplateDirectoryEntry(file) {
			continue
		}
		name, err := latexTemplateSafeEntryName(file.Name)
		if err != nil {
			return out, 0, err
		}
		if latexTemplateJunkEntry(name) {
			continue
		}
		if !file.Mode().IsRegular() {
			return out, 0, fmt.Errorf("模板包包含不支持的文件类型：%s", name)
		}
		slashed, err := latexTemplateStripRoot(filepath.ToSlash(name), root)
		if err != nil {
			return out, 0, err
		}
		if slashed == "" {
			continue
		}
		if strings.EqualFold(slashed, latexTemplateManifestName) {
			rc, openErr := file.Open()
			if openErr != nil {
				return out, 0, errors.New("无法读取模板清单")
			}
			raw, readErr := io.ReadAll(io.LimitReader(rc, latexTemplateManifestMaxBytes))
			_ = rc.Close()
			if readErr != nil {
				return out, 0, errors.New("无法读取模板清单")
			}
			if jsonErr := json.Unmarshal(raw, &out); jsonErr != nil {
				return out, 0, errors.New("模板清单不是合法的 JSON")
			}
			continue
		}
		ext := strings.ToLower(filepath.Ext(slashed))
		if !latexTemplateFileAllowed(slashed, ext) {
			label := ext
			if label == "" {
				label = filepath.Base(slashed)
			}
			return out, 0, fmt.Errorf("模板包包含不支持的文件类型：%s", label)
		}
		if ext == ".tex" {
			texFiles = append(texFiles, slashed)
		}
	}
	if len(texFiles) == 0 {
		return out, 0, errors.New("模板包中没有 .tex 源文件")
	}
	if strings.TrimSpace(out.Name) == "" {
		out.Name = strings.TrimSuffix(strings.TrimSpace(fallbackName), filepath.Ext(fallbackName))
	}
	if strings.TrimSpace(out.Name) == "" {
		out.Name = "LaTeX 模板"
	}
	if utf8RuneCount(out.Name) > latexTemplateNameMaxRunes {
		return out, 0, fmt.Errorf("模板名称不能超过 %d 个字符", latexTemplateNameMaxRunes)
	}
	if strings.TrimSpace(out.Version) == "" {
		out.Version = "1.0.0"
	}
	if utf8RuneCount(out.Version) > 32 {
		return out, 0, errors.New("模板版本号过长")
	}
	mainFile := strings.TrimSpace(strings.ReplaceAll(out.MainFile, "\\", "/"))
	if mainFile != "" {
		mainFile = strings.TrimPrefix(mainFile, "./")
		if !latexTemplateHasEntry(texFiles, mainFile) {
			return out, 0, errors.New("清单中的 main_file 不是模板包内的 .tex 文件")
		}
		out.MainFile = mainFile
	}
	return out, len(texFiles), nil
}

func latexTemplateHasEntry(texFiles []string, mainFile string) bool {
	for _, candidate := range texFiles {
		if candidate == mainFile || strings.HasSuffix(candidate, "/"+mainFile) {
			return true
		}
	}
	return false
}

// latexTemplateJunkEntry reports archive noise that is not part of the paper:
// Finder and Explorer drop these beside the template, and AppleDouble files
// ride along inside __MACOSX. They must not reject the package, and they must
// not count as a second top-level folder or the real wrapper stays in place.
func latexTemplateJunkEntry(name string) bool {
	slashed := strings.TrimPrefix(strings.ToLower(strings.ReplaceAll(name, "\\", "/")), "./")
	slashed = strings.Trim(slashed, "/")
	if slashed == "" {
		return false
	}
	for _, segment := range strings.Split(slashed, "/") {
		if segment == "__macosx" {
			return true
		}
		switch segment {
		case ".ds_store", "thumbs.db", "desktop.ini":
			return true
		}
		if strings.HasPrefix(segment, "._") {
			return true
		}
	}
	return false
}

// latexTemplateArchiveRoot returns the single shared top-level directory of an
// archive, or "" when the archive has no wrapper. Template archives almost
// always have one, and stripping it keeps the materialised document free of a
// pointless folder level. Junk entries are ignored, and the decision does not
// depend on which entry the zip writer put first.
func latexTemplateArchiveRoot(zr *zip.Reader) string {
	root := ""
	seen := false
	for _, file := range zr.File {
		slashed := strings.TrimPrefix(filepath.ToSlash(file.Name), "./")
		slashed = strings.Trim(slashed, "/")
		if slashed == "" || latexTemplateJunkEntry(slashed) {
			continue
		}
		top, _, found := strings.Cut(slashed, "/")
		if !found {
			return ""
		}
		if !seen {
			root = top
			seen = true
			continue
		}
		if top != root {
			return ""
		}
	}
	if !seen {
		return ""
	}
	return root
}

// latexTemplateStripRoot removes the archive's shared top-level directory, so the
// inspector and the extractor agree on the pack layout. An entry that is the root
// directory itself collapses to "" and is skipped by the caller; an entry outside
// it means the archive is internally inconsistent and is rejected.
func latexTemplateStripRoot(slashed, root string) (string, error) {
	slashed = strings.TrimPrefix(slashed, "./")
	if root == "" {
		return slashed, nil
	}
	if slashed == root {
		return "", nil
	}
	if !strings.HasPrefix(slashed, root+"/") {
		return "", errors.New("模板包目录结构不一致")
	}
	return strings.TrimPrefix(slashed, root+"/"), nil
}

// extractLatexTemplatePackage writes a validated package into destDir. The main
// .tex file is resolved with the same \documentclass heuristic the compiler uses,
// so "compile preview" works on a freshly imported template with no extra
// bookkeeping in the frontend.
func extractLatexTemplatePackage(data []byte, destDir, preferredMain string) (string, int, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", 0, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", 0, errors.New("不是有效的 zip 模板包")
	}
	root := latexTemplateArchiveRoot(zr)
	var (
		totalBytes int64
		texFiles   []string
	)
	for _, file := range zr.File {
		if latexTemplateDirectoryEntry(file) {
			continue
		}
		name, err := latexTemplateSafeEntryName(file.Name)
		if err != nil {
			return "", 0, err
		}
		if latexTemplateJunkEntry(name) {
			continue
		}
		slashed, err := latexTemplateStripRoot(filepath.ToSlash(name), root)
		if err != nil {
			return "", 0, err
		}
		if slashed == "" {
			continue
		}
		target := filepath.Join(destDir, filepath.FromSlash(slashed))
		if !latexTemplatePathInsideRoot(destDir, target) {
			return "", 0, errors.New("模板包包含越界路径")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", 0, err
		}
		rc, err := file.Open()
		if err != nil {
			return "", 0, err
		}
		// 0600 while the entry streams in, so an interrupted import never leaves
		// a half-written template readable to other users of the machine.
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			_ = rc.Close()
			return "", 0, err
		}
		// One byte past the cap distinguishes a file that fits from one that
		// CopyN would otherwise truncate and leave in the pack.
		written, copyErr := io.CopyN(out, rc, latexTemplateMaxZipBytes+1)
		_ = rc.Close()
		closeErr := out.Close()
		if written > latexTemplateMaxZipBytes {
			_ = os.Remove(target)
			return "", 0, errors.New("模板包中的单个文件过大")
		}
		if copyErr != nil && !errors.Is(copyErr, io.EOF) {
			_ = os.Remove(target)
			return "", 0, copyErr
		}
		if closeErr != nil {
			_ = os.Remove(target)
			return "", 0, closeErr
		}
		totalBytes += written
		if totalBytes > latexTemplateUnpackedMaxBytes {
			_ = os.Remove(target)
			return "", 0, errors.New("模板解包后体积过大")
		}
		if strings.EqualFold(filepath.Ext(slashed), ".tex") {
			texFiles = append(texFiles, slashed)
		}
	}
	if len(texFiles) == 0 {
		return "", 0, errors.New("模板包中没有 .tex 源文件")
	}
	return latexTemplateResolveMain(destDir, texFiles, preferredMain), len(texFiles), nil
}

func latexTemplatePathInsideRoot(root, target string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func latexTemplateMainMatches(candidate, preferred string) bool {
	if candidate == preferred || strings.HasSuffix(candidate, "/"+preferred) {
		return true
	}
	return false
}

// latexTemplateWrappedMain reports the path under a single archive wrapper.
// "elsarticle/paper.tex" matches "paper.tex"; "doc/elsdoc.tex" must not match a
// different file that merely ends with the same name.
func latexTemplateWrappedMain(candidate, preferred string) bool {
	parent, rest, ok := strings.Cut(preferred, "/")
	return ok && parent != "" && !strings.Contains(parent, "/") && rest == candidate
}

func latexTemplateResolveMain(destDir string, texFiles []string, preferred string) string {
	if preferred != "" {
		var wrapped string
		for _, candidate := range texFiles {
			if latexTemplateMainMatches(candidate, preferred) {
				return candidate
			}
			if wrapped == "" && latexTemplateWrappedMain(candidate, preferred) {
				wrapped = candidate
			}
		}
		if wrapped != "" {
			return wrapped
		}
	}
	var manualMain string
	for _, candidate := range texFiles {
		if !strings.EqualFold(filepath.Base(candidate), "main.tex") {
			continue
		}
		if latexTemplateDocSource(candidate) {
			if manualMain == "" {
				manualMain = candidate
			}
			continue
		}
		return candidate
	}
	var manual, sample string
	for _, candidate := range texFiles {
		raw, err := os.ReadFile(filepath.Join(destDir, filepath.FromSlash(candidate)))
		if err != nil {
			continue
		}
		if !documentclassRe.Match(raw) {
			if sample == "" && !latexTemplateDocSource(candidate) {
				sample = candidate
			}
			continue
		}
		if latexTemplateDocSource(candidate) {
			if manual == "" {
				manual = candidate
			}
			continue
		}
		return candidate
	}
	if manualMain != "" {
		return manualMain
	}
	if manual != "" {
		return manual
	}
	if sample != "" {
		return sample
	}
	if len(texFiles) == 0 {
		return ""
	}
	return texFiles[0]
}

// copyLatexTemplateTree copies an installed pack into a new document directory.
// A file the user has edited is left alone. The one exception is the untouched
// blank skeleton: it is the placeholder written before a template is chosen,
// and a template entry of the same name replaces it.
func copyLatexTemplateTree(srcDir, destDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(srcDir, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return os.MkdirAll(destDir, 0o755)
		}
		target := filepath.Join(destDir, rel)
		if !latexTemplatePathInsideRoot(destDir, target) {
			return errors.New("template contains an out-of-tree path")
		}
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if existing, statErr := os.Stat(target); statErr == nil && existing.Size() > 0 && !latexTemplateReplaceableBlank(target, existing.Size()) {
			return nil
		}
		return copyLatexTemplateFile(path, target)
	})
}

// copyLatexTemplateFile streams one pack file into the workspace. The bytes
// land in a temporary file in the same directory and replace the destination
// only after the copy finishes, so a failed write cannot truncate the blank
// skeleton or leave a half-written .tex for the expert to open.
func copyLatexTemplateFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tpl-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, copyErr := io.Copy(tmp, in)
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return replaceLatexTemplateFile(tmpName, dst)
}

// replaceLatexTemplateFile moves a completed temp file onto dst. Unix replaces
// the destination in one rename. Windows refuses that, so the old file is
// moved aside first and put back if the new name cannot be installed.
func replaceLatexTemplateFile(tmpName, dst string) error {
	if err := os.Rename(tmpName, dst); err == nil {
		return nil
	}
	backup := tmpName + ".old"
	if err := os.Rename(dst, backup); err != nil {
		if os.IsNotExist(err) {
			if err := os.Rename(tmpName, dst); err != nil {
				_ = os.Remove(tmpName)
				return err
			}
			return nil
		}
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Rename(backup, dst)
		_ = os.Remove(tmpName)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

// --- Wails bindings ---

// ListLatexTemplates returns the whole local library (the blank option,
// installed packs and their categories) as JSON for the library page.
func (a *App) ListLatexTemplates() (string, error) {
	latexTemplateMu.Lock()
	defer latexTemplateMu.Unlock()
	// Read once: two reads could disagree if a background sync committed between
	// them, and the categories/templates pair must describe one index state.
	index := a.readLatexTemplateIndex()
	raw, err := json.Marshal(map[string]any{
		"categories": index.Categories,
		"templates":  index.Templates,
	})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ImportLatexTemplate opens a file picker and imports the chosen .zip package.
// It returns ("", nil) when the user dismisses the picker: cancelling a dialog
// is a decision, not a failure, and the caller must not report it as an error.
func (a *App) ImportLatexTemplate() (string, error) {
	selection, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择 LaTeX 模板（.zip）",
		Filters: []runtime.FileFilter{
			{DisplayName: "LaTeX 模板包 (*.zip)", Pattern: "*.zip"},
		},
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(selection) == "" {
		return "", nil
	}
	return a.ImportLatexTemplateFromPath(selection)
}

// ImportLatexTemplateFromPath imports a packaged template from a local path.
func (a *App) ImportLatexTemplateFromPath(zipPath string) (string, error) {
	zipPath = strings.TrimSpace(zipPath)
	if zipPath == "" {
		return "", errors.New("模板包路径无效")
	}
	info, err := os.Stat(zipPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("模板包文件不存在")
	}
	if info.Size() > latexTemplateMaxZipBytes {
		return "", errors.New("模板包不能超过 100 MB")
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		return "", err
	}
	manifest, texCount, err := inspectLatexTemplatePackage(data, filepath.Base(zipPath))
	if err != nil {
		return "", err
	}
	return a.installLatexTemplatePackage(data, manifest, texCount, latexTemplateSourceLocal, "")
}

func (a *App) installLatexTemplatePackage(data []byte, manifest latexTemplatePackageManifest, texCount int, source, remoteID string) (string, error) {
	latexTemplateMu.Lock()
	defer latexTemplateMu.Unlock()
	index := a.readLatexTemplateIndex()

	// Re-importing the same package must update in place rather than create a
	// duplicate, so an administrator's category change or a newer version is not
	// shadowed by an older local copy.
	id := ""
	for _, item := range index.Templates {
		if item.ID == latexTemplateSourceBlank {
			continue
		}
		if source == latexTemplateSourceHub && item.RemoteID != "" && item.RemoteID == remoteID {
			id = item.ID
			break
		}
		if source == latexTemplateSourceLocal && item.Source == latexTemplateSourceLocal && item.Name == manifest.Name && item.Version == manifest.Version {
			id = item.ID
			break
		}
	}
	sum := sha256.Sum256(data)
	if id == "" {
		id = fmt.Sprintf("%s-%s", source, hex.EncodeToString(sum[:])[:12])
	}
	packDir := a.latexTemplatePackDir(id)
	staging := packDir + ".staging"
	_ = os.RemoveAll(staging)
	mainFile, extracted, err := extractLatexTemplatePackage(data, staging, manifest.MainFile)
	if err != nil {
		_ = os.RemoveAll(staging)
		return "", err
	}
	if err := os.RemoveAll(packDir); err != nil {
		_ = os.RemoveAll(staging)
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(packDir), 0o755); err != nil {
		_ = os.RemoveAll(staging)
		return "", err
	}
	if err := os.Rename(staging, packDir); err != nil {
		_ = os.RemoveAll(staging)
		return "", err
	}
	if err := os.MkdirAll(a.latexTemplateZipDir(), 0o755); err != nil {
		return "", err
	}
	// Keep the exact uploaded bytes: sharing re-sends this file, so a
	// re-packaged directory could silently diverge from what the author shared.
	if err := os.WriteFile(filepath.Join(a.latexTemplateZipDir(), id+".zip"), data, 0o644); err != nil {
		return "", err
	}

	now := latexTemplateNow()
	item := LatexTemplate{
		ID:          id,
		Name:        strings.TrimSpace(manifest.Name),
		Description: strings.TrimSpace(manifest.Description),
		CategoryID:  strings.TrimSpace(manifest.Category),
		Version:     strings.TrimSpace(manifest.Version),
		Author:      strings.TrimSpace(manifest.Author),
		MainFile:    filepath.ToSlash(mainFile),
		FileCount:   extracted,
		SizeBytes:   int64(len(data)),
		Source:      source,
		RemoteID:    remoteID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if prior, ok := findLatexTemplate(index, id); ok {
		item.CreatedAt = prior.CreatedAt
		item.ShareStatus = prior.ShareStatus
		if strings.TrimSpace(item.CategoryID) == "" {
			item.CategoryID = prior.CategoryID
		}
	}
	if !latexTemplateCategoryKnown(index, item.CategoryID) {
		item.CategoryID = "other"
	}
	if item.FileCount == 0 {
		item.FileCount = texCount
	}
	upsertLatexTemplate(&index, item)
	if err := a.writeLatexTemplateIndex(index); err != nil {
		return "", err
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// DeleteLatexTemplate removes an installed pack. The blank option is not
// removable: it is the default a new LaTeX task starts from.
func (a *App) DeleteLatexTemplate(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("模板 ID 无效")
	}
	if id == latexTemplateSourceBlank {
		return errors.New("空白模板不可删除")
	}
	latexTemplateMu.Lock()
	defer latexTemplateMu.Unlock()
	index := a.readLatexTemplateIndex()
	filtered := make([]LatexTemplate, 0, len(index.Templates))
	found := false
	for _, item := range index.Templates {
		if item.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, item)
	}
	if !found {
		return errors.New("模板不存在")
	}
	index.Templates = filtered
	if err := a.writeLatexTemplateIndex(index); err != nil {
		return err
	}
	_ = os.RemoveAll(a.latexTemplatePackDir(id))
	_ = os.Remove(filepath.Join(a.latexTemplateZipDir(), id+".zip"))
	return nil
}

// --- HubCenter synchronisation ---

type latexTemplateCatalogResponse struct {
	Categories []LatexTemplateCategory `json:"categories"`
	Templates  []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		CategoryID  string `json:"category_id"`
		Version     string `json:"version"`
		Author      string `json:"author"`
		MainFile    string `json:"main_file"`
		FileCount   int    `json:"file_count"`
		SizeBytes   int64  `json:"size_bytes"`
	} `json:"templates"`
}

func (a *App) latexTemplateHTTPClient() *http.Client {
	return &http.Client{Timeout: 180 * time.Second}
}

// latexTemplateMaxSyncPerRun bounds one synchronisation pass. The public
// catalogue endpoint is unpaginated, so without a bound a growing library would
// turn "sync" into one very long download.
const latexTemplateMaxSyncPerRun = 50

// SyncLatexTemplatesFromHub downloads every approved template that is not
// installed yet. It never overwrites an installed pack: the local copy is
// already the user's working template, so a new version is an explicit import.
func (a *App) SyncLatexTemplatesFromHub() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	client := a.latexTemplateHTTPClient()
	var catalog latexTemplateCatalogResponse
	if _, _, err := a.getHubCenterJSON(ctx, client, "/api/v1/latex-templates", 8<<20, &catalog); err != nil {
		return "", fmt.Errorf("获取模板库失败：%w", err)
	}
	// Categories must land before any pack. installLatexTemplatePackage files a
	// template under its category only if that category is already in the index,
	// and persists the result — merging afterwards would leave every first-sync
	// template filed under 其它 and the mapping already written to disk.
	if err := a.mergeLatexTemplateCategories(catalog.Categories); err != nil {
		return "", err
	}
	added := 0
	failed := 0
	// Bounded per run. The catalogue has no pagination, so an unbounded sync would
	// download every package on a growing library in one 15-minute pass. The
	// remainder is reported so the caller can say so and the user can sync again.
	remaining := 0
	for index, remote := range catalog.Templates {
		if a.latexTemplateAlreadyInstalled(remote.ID) {
			continue
		}
		if added+failed >= latexTemplateMaxSyncPerRun {
			remaining = len(catalog.Templates) - index
			break
		}
		data, err := a.downloadLatexTemplatePackage(ctx, client, remote.ID)
		if err != nil {
			log.Printf("[latex_template] download %s failed: %v", remote.ID, err)
			failed++
			continue
		}
		manifest := latexTemplatePackageManifest{
			Name:        remote.Name,
			Description: remote.Description,
			Category:    remote.CategoryID,
			Version:     remote.Version,
			Author:      remote.Author,
			MainFile:    remote.MainFile,
		}
		if _, err := a.installLatexTemplatePackage(data, manifest, remote.FileCount, latexTemplateSourceHub, remote.ID); err != nil {
			log.Printf("[latex_template] install %s failed: %v", remote.ID, err)
			failed++
			continue
		}
		added++
	}
	raw, err := json.Marshal(map[string]any{"added": added, "failed": failed, "remaining": remaining})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (a *App) latexTemplateAlreadyInstalled(remoteID string) bool {
	remoteID = strings.TrimSpace(remoteID)
	if remoteID == "" {
		return false
	}
	latexTemplateMu.Lock()
	defer latexTemplateMu.Unlock()
	for _, item := range a.readLatexTemplateIndex().Templates {
		if item.RemoteID == remoteID {
			return true
		}
	}
	return false
}

func (a *App) mergeLatexTemplateCategories(remote []LatexTemplateCategory) error {
	if len(remote) == 0 {
		return nil
	}
	latexTemplateMu.Lock()
	defer latexTemplateMu.Unlock()
	index := a.readLatexTemplateIndex()
	known := make(map[string]bool, len(index.Categories))
	for _, category := range index.Categories {
		known[category.ID] = true
	}
	changed := false
	for _, category := range remote {
		id := strings.TrimSpace(category.ID)
		if id == "" || known[id] {
			continue
		}
		name := strings.TrimSpace(category.Name)
		if name == "" {
			name = id
		}
		index.Categories = append(index.Categories, LatexTemplateCategory{
			ID:          id,
			Name:        name,
			Description: strings.TrimSpace(category.Description),
			SortOrder:   category.SortOrder,
		})
		known[id] = true
		changed = true
	}
	if !changed {
		return nil
	}
	sortLatexTemplateCategories(index.Categories)
	return a.writeLatexTemplateIndex(index)
}

func (a *App) downloadLatexTemplatePackage(ctx context.Context, client *http.Client, remoteID string) ([]byte, error) {
	bases, err := a.resolveHubCenterCandidates(ctx, client)
	if err != nil {
		return nil, err
	}
	_, _, data, err := a.getHubCenterBytesFromCandidates(ctx, client, bases, "/api/v1/latex-templates/"+url.PathEscape(remoteID)+"/download", latexTemplateMaxZipBytes)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// ShareLatexTemplate uploads a locally imported template to HubCenter, where it
// waits for administrator review before other users can install it.
func (a *App) ShareLatexTemplate(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == latexTemplateSourceBlank {
		return "", errors.New("空白模板无需分享")
	}
	latexTemplateMu.Lock()
	item, ok := findLatexTemplate(a.readLatexTemplateIndex(), id)
	latexTemplateMu.Unlock()
	if !ok {
		return "", errors.New("模板不存在")
	}
	zipPath := filepath.Join(a.latexTemplateZipDir(), id+".zip")
	if _, err := os.Stat(zipPath); err != nil {
		return "", errors.New("本地模板包缺失，请重新导入后再分享")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client := a.latexTemplateHTTPClient()
	bases, err := a.resolveHubCenterSubmitCandidates(ctx, client)
	if err != nil {
		return "", fmt.Errorf("连接模板中心失败：%w", err)
	}
	body, contentType, err := a.buildLatexTemplateShareMultipart(zipPath, item)
	if err != nil {
		return "", err
	}
	var result map[string]any
	if err := a.postLatexTemplateShare(ctx, client, bases, body, contentType, &result); err != nil {
		return "", err
	}
	remoteID, _ := result["id"].(string)

	latexTemplateMu.Lock()
	defer latexTemplateMu.Unlock()
	index := a.readLatexTemplateIndex()
	if current, found := findLatexTemplate(index, id); found {
		current.ShareStatus = latexTemplateStatusFromHub(result)
		if remoteID != "" {
			current.RemoteID = remoteID
		}
		current.UpdatedAt = latexTemplateNow()
		upsertLatexTemplate(&index, current)
		if err := a.writeLatexTemplateIndex(index); err != nil {
			return "", err
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func latexTemplateStatusFromHub(result map[string]any) string {
	if status, ok := result["status"].(string); ok && strings.TrimSpace(status) != "" {
		return strings.TrimSpace(status)
	}
	return "pending"
}

// latexTemplateSubmission is the Hub's view of one of the caller's submissions.
type latexTemplateSubmission struct {
	RemoteID   string `json:"remote_id"`
	Status     string `json:"status"`
	ReviewNote string `json:"review_note"`
	UpdatedAt  string `json:"updated_at"`
}

// SyncLatexTemplateShares refreshes the review state of every template this
// machine has shared. Without it a submitted template would keep showing
// "pending review" forever, because installing new packages never revisits an
// entry that is already installed locally.
func (a *App) SyncLatexTemplateShares() error {
	latexTemplateMu.Lock()
	index := a.readLatexTemplateIndex()
	wanted := make(map[string]string, len(index.Templates))
	for _, item := range index.Templates {
		if remoteID := strings.TrimSpace(item.RemoteID); remoteID != "" {
			wanted[remoteID] = item.ID
		}
	}
	latexTemplateMu.Unlock()
	if len(wanted) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	client := a.latexTemplateHTTPClient()
	var payload struct {
		Submissions []latexTemplateSubmission `json:"submissions"`
	}
	path := "/api/v1/latex-templates/submissions"
	if email := strings.TrimSpace(a.latexTemplateShareEmail()); email != "" {
		path += "?email=" + url.QueryEscape(email)
	}
	if _, _, err := a.getHubCenterJSON(ctx, client, path, 1<<20, &payload); err != nil {
		return fmt.Errorf("同步模板审核状态失败：%w", err)
	}
	if len(payload.Submissions) == 0 {
		return nil
	}
	latexTemplateMu.Lock()
	defer latexTemplateMu.Unlock()
	index = a.readLatexTemplateIndex()
	changed := false
	for _, submission := range payload.Submissions {
		localID, ok := wanted[strings.TrimSpace(submission.RemoteID)]
		if !ok {
			continue
		}
		item, ok := findLatexTemplate(index, localID)
		if !ok {
			continue
		}
		status := strings.TrimSpace(submission.Status)
		note := strings.TrimSpace(submission.ReviewNote)
		if item.ShareStatus == status && item.ShareNote == note {
			continue
		}
		item.ShareStatus = status
		item.ShareNote = note
		upsertLatexTemplate(&index, item)
		changed = true
	}
	if !changed {
		return nil
	}
	return a.writeLatexTemplateIndex(index)
}

func (a *App) buildLatexTemplateShareMultipart(zipPath string, item LatexTemplate) ([]byte, string, error) {
	file, err := os.Open(zipPath)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("zip", filepath.Base(zipPath))
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, "", err
	}
	fields := map[string]string{
		"name":        item.Name,
		"description": item.Description,
		"category_id": item.CategoryID,
		"version":     item.Version,
		"author":      item.Author,
	}
	// The bearer token remains the primary credential; the email field is the
	// fallback for deployments still in email upload mode.
	if email := strings.TrimSpace(a.latexTemplateShareEmail()); email != "" {
		fields["email"] = email
	}
	for key, value := range fields {
		if value == "" {
			continue
		}
		if err := writer.WriteField(key, value); err != nil {
			return nil, "", err
		}
	}
	contentType := writer.FormDataContentType()
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), contentType, nil
}

func (a *App) latexTemplateShareEmail() string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.RemoteEmail)
}

func (a *App) postLatexTemplateShare(ctx context.Context, client *http.Client, bases []string, body []byte, contentType string, dest *map[string]any) error {
	if len(bases) == 0 {
		return errors.New("未找到可用的模板中心节点")
	}
	authHeader := a.latexTemplateAuthHeader()
	var lastErr error
	for _, base := range bases {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/latex-templates", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", contentType)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lastErr = err
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if dest != nil && len(raw) > 0 {
				_ = json.Unmarshal(raw, dest)
			}
			return nil
		}
		hubErr := latexTemplateHubError(resp.StatusCode, raw)
		// A rejected package (bad zip, missing .tex) is rejected by every node,
		// so surface that message instead of retrying the next candidate.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 &&
			resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden {
			return hubErr
		}
		lastErr = hubErr
	}
	if lastErr == nil {
		return errors.New("分享模板失败")
	}
	return fmt.Errorf("分享模板失败：%w", lastErr)
}

func latexTemplateHubError(status int, raw []byte) error {
	var payload struct {
		Message string `json:"message"`
		Error   string `json:"error"`
		Code    string `json:"code"`
	}
	_ = json.Unmarshal(raw, &payload)
	message := strings.TrimSpace(payload.Message)
	if message == "" {
		message = strings.TrimSpace(payload.Error)
	}
	if message == "" {
		message = strings.TrimSpace(string(raw))
	}
	if message == "" {
		message = fmt.Sprintf("HTTP %d", status)
	}
	return errors.New(message)
}

func (a *App) latexTemplateAuthHeader() string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return ""
	}
	if token := strings.TrimSpace(cfg.SkillMarketSessionToken); token != "" {
		return "Bearer " + token
	}
	if token := strings.TrimSpace(cfg.RemoteViewerToken); token != "" {
		return "Bearer " + token
	}
	return ""
}

// --- Document materialisation ---

// CreateLatexDocumentResult describes the document a new LaTeX task starts from.
type CreateLatexDocumentResult struct {
	ProjectPath  string `json:"project_path"`
	RelativePath string `json:"relative_path"`
	MainFile     string `json:"main_file"`
	TemplateID   string `json:"template_id"`
	TemplateName string `json:"template_name"`
	// WorkspacePath is the directory the unpacked template was written into.
	// It is also the directory the LaTeX expert's tools are pointed at, so a
	// relative path such as "elsarticle-template-num.tex" or "chapters/main.tex"
	// resolves for the agent.
	WorkspacePath string `json:"workspace_path"`
	// SourceFiles are workspace-relative template sources (.tex and the class,
	// style and bibliography files beside them). A zip may keep those files at
	// its root or under a subdirectory; these paths are relative to WorkspacePath
	// either way.
	SourceFiles []string `json:"source_files,omitempty"`
	// Created is false when an existing document was reused. The caller uses it
	// so the message it sends into the session does not claim it just created a
	// paper the user has been writing for a while.
	Created bool `json:"created"`
}

// CreateLatexDocument materialises a new LaTeX document inside a task
// workspace. An empty templateID (or the built-in blank id) writes a minimal
// article skeleton; any other id copies the installed pack and adopts its main
// .tex file.
//
// projectPath is a task directory, not a source directory: the document is
// written into the task's execution workspace, which is exactly where the
// LaTeX workbench reads and saves it.
//
// The call is idempotent and non-destructive. A LaTeX task owns one document,
// and a user who picks a template again — or clicks "blank" after writing a few
// pages — must never lose work. Existing files are therefore never overwritten;
// a repeated call re-opens whatever is already on disk.
func (a *App) CreateLatexDocument(projectPath, templateID, fileName string) (string, error) {
	projectPath = normalizeProjectSessionPath(projectPath)
	if projectPath == "" {
		return "", errors.New("任务目录无效")
	}
	templateID = strings.TrimSpace(templateID)
	if templateID == "" {
		templateID = latexTemplateSourceBlank
	}
	root, err := a.latexDocumentRoot(projectPath)
	if err != nil {
		return "", err
	}
	// Check writability after the root is resolved, because for a cloud workspace
	// the read-only marker lives on the mount, not on the task directory. Doing it
	// first would let a partial document be written into a read-only mount.
	if err := rejectReadOnlyCloudWorkspace(root); err != nil {
		return "", err
	}
	requestedName := latexTemplateDocumentName(fileName)
	result := CreateLatexDocumentResult{ProjectPath: projectPath, TemplateID: templateID}
	if templateID == latexTemplateSourceBlank {
		blank := latexBlankTemplate()
		result.TemplateName = blank.Name
		if err := os.MkdirAll(root, 0o755); err != nil {
			return "", err
		}
		// Reuse an existing document rather than replacing it: this endpoint is
		// reached on every "start a LaTeX paper" click, and the second click must
		// not discard the paper written after the first.
		if existing := latexTemplateExistingDocument(root, requestedName); existing != "" {
			result.RelativePath = existing
			result.MainFile = existing
			result.SourceFiles = []string{existing}
			return a.finishLatexDocument(projectPath, root, result, false)
		}
		if err := os.WriteFile(filepath.Join(root, requestedName), []byte(latexBlankTemplateSource), 0o644); err != nil {
			return "", err
		}
		result.RelativePath = requestedName
		result.MainFile = requestedName
		result.SourceFiles = []string{requestedName}
		result.Created = true
		a.noteCloudWorkspaceWrite(root)
		return a.finishLatexDocument(projectPath, root, result, true)
	}

	latexTemplateMu.Lock()
	item, ok := findLatexTemplate(a.readLatexTemplateIndex(), templateID)
	latexTemplateMu.Unlock()
	if !ok {
		return "", errors.New("模板不存在")
	}
	packDir := a.latexTemplatePackDir(item.ID)
	if info, statErr := os.Stat(packDir); statErr != nil || !info.IsDir() {
		return "", errors.New("本地模板文件缺失，请重新导入")
	}
	source := strings.TrimSpace(item.MainFile)
	if source == "" {
		source = "main.tex"
	}
	// A rename can only work once the file exists, so it has to follow the copy.
	// Renaming first would silently no-op on a fresh workspace and the requested
	// name would be ignored.
	wantsRename := fileName != "" && requestedName != "main.tex" && requestedName != source
	main := source
	// Only the template's own entry counts. A blank main.tex left by opening
	// the expert earlier must not make this first unpack look like a reuse.
	// Auxiliary files are still copied; an edited entry is not overwritten.
	reused := latexTemplateEntryPresent(root, source)
	if err := copyLatexTemplateTree(packDir, root); err != nil {
		return "", err
	}
	if wantsRename {
		if renamed := latexTemplateRenameMain(root, source, requestedName); renamed == requestedName {
			main = requestedName
		}
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(main))); statErr != nil {
		// The pack declared an entry point it does not actually contain; fall
		// back to whatever the copy produced rather than pointing at nothing.
		if fallback := latexTemplateExistingDocument(root, source); fallback != "" {
			main = fallback
		}
	} else if filepath.ToSlash(main) != "main.tex" {
		// The entry is on disk. Only then drop the placeholder main.tex, so a
		// failed unpack cannot leave the workspace with no paper at all.
		removeUntouchedBlankBesideTemplate(root, main)
	}
	result.RelativePath = filepath.ToSlash(main)
	result.MainFile = result.RelativePath
	result.TemplateName = item.Name
	result.SourceFiles = latexTemplatePackSources(packDir, source, result.RelativePath)
	result.Created = !reused
	if !reused {
		a.noteCloudWorkspaceWrite(root)
	}
	// A template click is an instruction to work on these files. Point the
	// expert at the directory they were unpacked into, even when an earlier
	// turn had left its tools on the desktop.
	return a.finishLatexDocument(projectPath, root, result, true)
}

// finishLatexDocument records where the files landed and, when pin is set,
// makes that directory the LaTeX expert's working directory.
func (a *App) finishLatexDocument(projectPath, root string, result CreateLatexDocumentResult, pin bool) (string, error) {
	result.WorkspacePath = root
	if pin {
		a.pinLatexExpertWorkspace(projectPath, root)
	}
	return latexTemplateDocumentJSON(result)
}

// pinLatexExpertWorkspace points the built-in LaTeX expert at the directory
// that now holds the unpacked template. Expert tools otherwise keep whatever
// directory the tab last used (often the desktop), so the model is told a
// relative path that does not exist in the directory it can actually read.
func (a *App) pinLatexExpertWorkspace(projectPath, dir string) {
	if a == nil {
		return
	}
	dir = normalizeProjectSessionPath(dir)
	if dir == "" {
		return
	}
	if a.isManagedRecentTaskWorkspacePath(projectPath) {
		if err := a.persistTaskWorkingDir(projectPath, dir); err != nil {
			log.Printf("[latex_template] persist task workspace project=%q dir=%q err=%v", projectPath, dir, err)
		}
	}
	owner := expertSessionUserID(builtinLatexExpertID)
	if normalizeProjectSessionPath(a.BoundWorkingDirForOwner(owner)) == dir {
		return
	}
	tabID := "expert-" + builtinLatexExpertID
	if err := a.SetTabWorkingDir(tabID, dir); err != nil {
		log.Printf("[latex_template] pin expert workspace dir=%q err=%v", dir, err)
	}
}

// latexTemplatePackSources lists the template sources a new paper should treat
// as its working set. Paths stay relative to the unpack directory, so a file
// that lived in a zip subdirectory (doc/elsdoc.tex) is not reported as if it
// sat at the workspace root. The entry file is listed first.
func latexTemplatePackSources(packDir, previousMain, entry string) []string {
	var found []string
	_ = filepath.Walk(packDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		rel, relErr := filepath.Rel(packDir, path)
		if relErr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		// The class manual is unpacked with the zip, but it is not part of the
		// paper the expert should read first. Skipping it keeps the capped list
		// for the files beside the entry.
		if info.IsDir() {
			if latexTemplateDocSource(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(rel)) {
		case ".tex", ".ltx", ".latex", ".cls", ".sty", ".bst", ".bib":
			if previousMain != "" && rel == filepath.ToSlash(previousMain) && entry != "" && entry != rel {
				rel = filepath.ToSlash(entry)
			}
			found = append(found, rel)
		}
		return nil
	})
	sort.Strings(found)
	entry = filepath.ToSlash(strings.TrimSpace(entry))
	if entry != "" {
		ordered := make([]string, 0, len(found)+1)
		ordered = append(ordered, entry)
		for _, item := range found {
			if item == entry {
				continue
			}
			ordered = append(ordered, item)
		}
		found = ordered
	}
	const maxSources = 24
	if len(found) > maxSources {
		// Chapter files sort before refs.bib. Keep class, style and
		// bibliography files when the cap would otherwise drop them.
		head := ""
		rest := found
		if entry != "" && len(found) > 0 && found[0] == entry {
			head = found[0]
			rest = found[1:]
		}
		support := make([]string, 0, 8)
		chapters := make([]string, 0, len(rest))
		for _, item := range rest {
			if latexTemplateSupportFile(item) {
				support = append(support, item)
				continue
			}
			chapters = append(chapters, item)
		}
		found = found[:0]
		if head != "" {
			found = append(found, head)
		}
		found = append(found, support...)
		found = append(found, chapters...)
		if len(found) > maxSources {
			found = found[:maxSources]
		}
	}
	return found
}

func latexTemplateSupportFile(rel string) bool {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".cls", ".sty", ".bst", ".bib":
		return true
	default:
		return false
	}
}

func latexTemplateDocumentJSON(result CreateLatexDocumentResult) (string, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// latexTemplateBlankSkeleton is the trimmed bytes of the auto-generated article.
var latexTemplateBlankSkeleton = bytes.TrimSpace([]byte(latexBlankTemplateSource))

// latexTemplateReplaceableBlank reports a destination that is still the
// auto-generated article. Only a small .tex is read; a PDF or an edited paper
// is rejected from the size and extension alone.
func latexTemplateReplaceableBlank(path string, size int64) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tex", ".ltx", ".latex":
	default:
		return false
	}
	// +64 covers a trailing newline and CRLF expansion of the same skeleton.
	if size <= 0 || size > int64(len(latexTemplateBlankSkeleton)+64) {
		return false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	return bytes.Equal(bytes.TrimSpace(raw), latexTemplateBlankSkeleton)
}

// removeUntouchedBlankBesideTemplate deletes the placeholder main.tex when the
// template's entry is a different file. An edited main.tex is kept.
func removeUntouchedBlankBesideTemplate(root, entry string) {
	if filepath.ToSlash(strings.TrimSpace(entry)) == "main.tex" {
		return
	}
	path := filepath.Join(root, "main.tex")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	if !latexTemplateReplaceableBlank(path, info.Size()) {
		return
	}
	if err := os.Remove(path); err != nil {
		log.Printf("[latex_template] remove blank skeleton: %v", err)
	}
}

// latexTemplateEntryPresent reports a template entry the user already has.
// The untouched blank skeleton does not count: applying a template should
// replace it.
func latexTemplateEntryPresent(root, rel string) bool {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return false
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return false
	}
	return !latexTemplateReplaceableBlank(path, info.Size())
}

// latexTemplateExistingDocument finds a LaTeX entry point already present in a
// workspace. It lets a repeated "start a LaTeX paper" call re-open the document
// the user already has instead of overwriting it. The requested name wins when
// it exists; otherwise the usual candidates are tried in order.
func latexTemplateExistingDocument(root, requestedName string) string {
	for _, candidate := range []string{requestedName, "main.tex", "paper.tex"} {
		if candidate == "" {
			continue
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(candidate)))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}
		return candidate
	}
	return ""
}

// latexTemplateDocumentName validates a caller-supplied entry document name.
func latexTemplateDocumentName(fileName string) string {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return "main.tex"
	}
	if strings.ContainsAny(fileName, "/\\") || strings.Contains(fileName, "..") {
		return "main.tex"
	}
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext != ".tex" && ext != ".ltx" && ext != ".latex" {
		fileName += ".tex"
	}
	return fileName
}

func latexTemplateRenameMain(root, current, requested string) string {
	source := filepath.Join(root, filepath.FromSlash(current))
	if _, err := os.Stat(source); err != nil {
		return current
	}
	target := filepath.Join(root, requested)
	if source == target {
		return requested
	}
	if err := os.Rename(source, target); err != nil {
		return current
	}
	// Rewrite \input/\include references so the renamed entry point does not
	// point at a file that no longer exists.
	raw, err := os.ReadFile(target)
	if err != nil {
		return requested
	}
	oldStem := strings.TrimSuffix(filepath.Base(filepath.FromSlash(current)), filepath.Ext(current))
	newStem := strings.TrimSuffix(requested, filepath.Ext(requested))
	if oldStem == newStem || oldStem == "" {
		return requested
	}
	pattern := regexp.MustCompile(`(\\input|\\include)\{` + regexp.QuoteMeta(oldStem) + `\}`)
	updated := pattern.ReplaceAll(raw, []byte("${1}{"+newStem+"}"))
	if !bytes.Equal(raw, updated) {
		if err := os.WriteFile(target, updated, 0o644); err != nil {
			log.Printf("[latex_template] rewrite entry references failed: %v", err)
		}
	}
	return requested
}

// latexDocumentRoot resolves the directory a new LaTeX document is written to.
// It reuses the coding-workbench root resolution so a task directory, a cloud
// workspace and a plain local folder all land in the same place the LaTeX
// workbench will later read from.
func (a *App) latexDocumentRoot(projectPath string) (string, error) {
	if strings.TrimSpace(projectPath) == "" {
		return "", errors.New("任务目录无效")
	}
	if cloudDir := strings.TrimSpace(a.cloudWorkspaceExecutionDir(projectPath)); cloudDir != "" {
		return cloudDir, nil
	}
	root, err := codingWorkbenchBrowserLocalRoot(a, projectPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return root, nil
}

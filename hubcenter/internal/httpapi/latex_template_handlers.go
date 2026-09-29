package httpapi

// LaTeX paper templates are a first-class HubCenter capability. A template is
// always shipped as a single .zip so a template can round-trip between the
// desktop app and the Hub without a bespoke import format.
//
// Lifecycle:
//   - An end user shares a locally imported template: POST /api/v1/latex-templates
//     stores the package as `pending` and attributes it to the uploader.
//   - An administrator approves it, optionally moving it to another category.
//     Only `approved` packages are visible to the public catalogue.
//   - Administrators may also upload a template directly; those land `approved`.
//   - Categories are seeded with 会议 / 期刊 / 毕业论文 / 其它 and remain
//     administrator-extensible, because publishers group templates differently
//     (workshops, posters, standards bodies, ...).

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	latexTemplateManifestName = "template.json"
	latexTemplateMaxZipBytes  = 100 << 20
	latexTemplateMaxFiles     = 4000
	latexTemplateManifestMax  = 256 << 10

	latexTemplateStatusPending  = "pending"
	latexTemplateStatusApproved = "approved"
	latexTemplateStatusRejected = "rejected"
)

type latexTemplateSchemaState struct {
	once sync.Once
	err  error
}

var latexTemplateSchemaByDB sync.Map // map[*sql.DB]*latexTemplateSchemaState

// LatexTemplateHandlers owns the LaTeX template control plane. It borrows the
// Capability Market database handle so it participates in the same WAL database
// and backup window, but keeps its own tables and blob directory.
type LatexTemplateHandlers struct {
	db      *sql.DB
	market  *SkillMarketHandlers
	dataDir string
	// Audit + status transitions are serialized in-process so two administrator
	// actions cannot interleave a review with a category reassignment.
	mu sync.Mutex
	// HA publication follows the skill-library snapshot: one coalesced dump
	// after a local change, applied by peers as a full catalogue replacement.
	syncMu      sync.Mutex
	sync        latexTemplateSyncRecorder
	syncRunning bool
	syncPending bool
}

func NewLatexTemplateHandlers(market *SkillMarketHandlers) *LatexTemplateHandlers {
	var (
		db      *sql.DB
		dataDir string
	)
	if market != nil {
		if market.store != nil {
			db = market.store.DB()
		}
		dataDir = market.dataDir
	}
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "data"
	}
	return &LatexTemplateHandlers{db: db, market: market, dataDir: dataDir}
}

func latexTemplateNow() string {
	// Fixed width so values sort in time order. RFC3339Nano drops trailing
	// zeros, and then "10:00:00.5Z" sorts before "10:00:00Z".
	return time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func latexTemplateParsedTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{"2006-01-02T15:04:05.000000000Z", time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func latexTemplateTimeOK(value string) bool {
	_, ok := latexTemplateParsedTime(value)
	return ok
}

// latexTemplateTimeAfter reports whether a is strictly later than b.
// Empty or unparseable values are not later than anything.
func latexTemplateTimeAfter(a, b string) bool {
	left, leftOK := latexTemplateParsedTime(a)
	right, rightOK := latexTemplateParsedTime(b)
	if !leftOK || !rightOK {
		return false
	}
	return left.After(right)
}

// LatexTemplateCategory is an administrator-managed grouping shown as a section
// heading in the desktop template library.
type LatexTemplateCategory struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
	Status      string `json:"status"`
	Builtin     bool   `json:"builtin"`
}

// LatexTemplate is the catalogue record for one packaged template.
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
	SHA256      string `json:"sha256"`
	Status      string `json:"status"`
	ReviewNote  string `json:"review_note,omitempty"`
	OwnerID     string `json:"owner_id,omitempty"`
	OwnerEmail  string `json:"owner_email,omitempty"`
	Source      string `json:"source"`
	DownloadURL string `json:"download_url"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// builtinLatexTemplateCategories are seeded on every schema initialization.
// Their ids are stable and are also used as the desktop-side ordering key, so
// the four sections the product promises (会议 / 期刊 / 毕业论文 / 其它) always
// exist even before an administrator touches the console.
var builtinLatexTemplateCategories = []LatexTemplateCategory{
	{ID: "conference", Name: "会议", Description: "会议投稿与_camera-ready_ 模板", SortOrder: 10, Status: "active", Builtin: true},
	{ID: "journal", Name: "期刊", Description: "期刊投稿格式模板", SortOrder: 20, Status: "active", Builtin: true},
	{ID: "thesis", Name: "毕业论文", Description: "学位论文、开题与答辩模板", SortOrder: 30, Status: "active", Builtin: true},
	{ID: "other", Name: "其它", Description: "其它 LaTeX 文档模板", SortOrder: 40, Status: "active", Builtin: true},
}

func (h *LatexTemplateHandlers) ensureSchema() error {
	if h == nil || h.db == nil {
		return errors.New("latex template library unavailable")
	}
	stateValue, _ := latexTemplateSchemaByDB.LoadOrStore(h.db, &latexTemplateSchemaState{})
	state := stateValue.(*latexTemplateSchemaState)
	state.once.Do(func() {
		for _, stmt := range []string{
			`CREATE TABLE IF NOT EXISTS latex_template_categories (id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', sort_order INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', builtin INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS latex_templates (id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '', category_id TEXT NOT NULL, version TEXT NOT NULL DEFAULT '1.0.0', author TEXT NOT NULL DEFAULT '', main_file TEXT NOT NULL DEFAULT 'main.tex', file_count INTEGER NOT NULL DEFAULT 0, size_bytes INTEGER NOT NULL DEFAULT 0, sha256 TEXT NOT NULL DEFAULT '', zip_path TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'pending', review_note TEXT NOT NULL DEFAULT '', owner_id TEXT NOT NULL DEFAULT '', owner_email TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT 'user', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS latex_template_events (id TEXT PRIMARY KEY, actor_id TEXT NOT NULL, action TEXT NOT NULL, target_id TEXT NOT NULL, reason TEXT NOT NULL DEFAULT '', before_json TEXT NOT NULL DEFAULT '', after_json TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS latex_template_tombstones (id TEXT PRIMARY KEY, deleted_at TEXT NOT NULL)`,
			`CREATE TABLE IF NOT EXISTS latex_template_category_tombstones (id TEXT PRIMARY KEY, deleted_at TEXT NOT NULL)`,
			`CREATE INDEX IF NOT EXISTS idx_latex_templates_status ON latex_templates(status,category_id,updated_at)`,
			`CREATE INDEX IF NOT EXISTS idx_latex_template_events_target ON latex_template_events(target_id,created_at)`,
		} {
			if _, err := h.db.Exec(stmt); err != nil {
				state.err = err
				return
			}
		}
		now := latexTemplateNow()
		// Reassert that the four product category ids exist and stay active.
		// Name, description, sort order and updated_at stay as an administrator
		// left them. Refreshing updated_at here would look like a newer edit
		// and overwrite that rename on the other HubCenter nodes.
		for _, category := range builtinLatexTemplateCategories {
			if _, err := h.db.Exec(`INSERT INTO latex_template_categories(id,name,description,sort_order,status,builtin,created_at,updated_at) VALUES(?,?,?,?,'active',1,?,?) ON CONFLICT(id) DO UPDATE SET status='active' WHERE latex_template_categories.status <> 'active'`, category.ID, category.Name, category.Description, category.SortOrder, now, now); err != nil {
				state.err = err
				return
			}
		}
	})
	return state.err
}

func (h *LatexTemplateHandlers) packageDir() string {
	return filepath.Join(h.dataDir, "latex-templates")
}

func (h *LatexTemplateHandlers) appendEvent(ctx context.Context, actor, action, targetID, reason string, before, after any) {
	if h == nil || h.db == nil {
		return
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	_, _ = h.db.ExecContext(ctx, `INSERT INTO latex_template_events(id,actor_id,action,target_id,reason,before_json,after_json,created_at) VALUES(?,?,?,?,?,?,?,?)`, uniqueID("latex_tpl_evt"), actor, action, targetID, strings.TrimSpace(reason), string(beforeJSON), string(afterJSON), latexTemplateNow())
}

func latexTemplateActor(r *http.Request) string {
	if admin := AdminFromContext(r.Context()); admin != nil {
		if name := strings.TrimSpace(firstNonEmptyLatexTemplate(admin.Username, admin.Email)); name != "" {
			return "admin:" + name
		}
		return "admin"
	}
	return strings.TrimSpace(r.Header.Get("X-Admin-User"))
}

func firstNonEmptyLatexTemplate(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

const latexTemplateSelectColumns = `id,name,description,category_id,version,author,main_file,file_count,size_bytes,sha256,status,review_note,owner_id,owner_email,source,created_at,updated_at`

type latexTemplateScanner interface {
	Scan(dest ...any) error
}

func scanLatexTemplate(row latexTemplateScanner) (LatexTemplate, error) {
	var item LatexTemplate
	err := row.Scan(newLatexTemplateScanTargets(&item, nil)...)
	if err != nil {
		return LatexTemplate{}, err
	}
	item.DownloadURL = "/api/v1/latex-templates/" + item.ID + "/download"
	return item, nil
}

// newLatexTemplateScanTargets keeps the column order in one place. The download
// handler appends the blob path to the same projection instead of issuing a
// second query that could observe a different row state.
func newLatexTemplateScanTargets(item *LatexTemplate, zipPath *string) []any {
	targets := []any{
		&item.ID, &item.Name, &item.Description, &item.CategoryID, &item.Version,
		&item.Author, &item.MainFile, &item.FileCount, &item.SizeBytes, &item.SHA256,
		&item.Status, &item.ReviewNote, &item.OwnerID, &item.OwnerEmail, &item.Source,
		&item.CreatedAt, &item.UpdatedAt,
	}
	if zipPath != nil {
		targets = append(targets, zipPath)
	}
	return targets
}

// ── Package inspection ───────────────────────────────────────────────────

type latexTemplatePackage struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	MainFile    string `json:"main_file"`
}

// latexTemplateAllowedExt keeps a shared package to paper sources, styles,
// bibliographies and figures. The upload endpoint is public, so the allowlist is
// enforced here as well as on the client: an executable or script has no place
// in a paper template and would only be a way to store or smuggle one.
// .dtx/.ins/.drv are the documented sources publisher classes (elsarticle,
// acmart, revtex) ship beside the .cls; they are TeX input, not programs.
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
// CTAN or publisher zip ships without a suffix (elsarticle's README and
// doc/makefile). Only these exact base names are accepted; any other file
// without an extension is still rejected.
var latexTemplateExtensionlessNames = map[string]bool{
	"readme": true, "license": true, "licence": true, "copying": true,
	"changes": true, "changelog": true, "authors": true, "notice": true,
	"makefile": true, "gnumakefile": true,
}

func latexTemplateFileAllowed(name, ext string) bool {
	if latexTemplateAllowedExt[strings.ToLower(ext)] {
		return true
	}
	return ext == "" && latexTemplateExtensionlessNames[strings.ToLower(path.Base(name))]
}

// latexTemplateDocSource reports a file that lives in a package's manual
// directory. Publisher zips (elsarticle's doc/elsdoc.tex) put the class manual
// next to the sample article, and both declare \documentclass.
func latexTemplateDocSource(name string) bool {
	for _, segment := range strings.Split(strings.ToLower(strings.ReplaceAll(name, "\\", "/")), "/") {
		switch segment {
		case "doc", "docs", "documentation", "docsrc":
			return true
		}
	}
	return false
}

// latexTemplateDocumentclassRe is the same entry-point marker the LaTeX
// compiler uses, so the catalogue's main_file agrees with what actually
// compiles on the desktop.
var latexTemplateDocumentclassRe = regexp.MustCompile(`\\documentclass`)

// inspectLatexTemplateZip validates an uploaded package and extracts its
// manifest. A package must be a readable zip whose entries are all paper-related
// and which carries at least one .tex file; the rest of the layout is left to the
// template author.
func inspectLatexTemplateZip(data []byte, fallbackName string) (latexTemplatePackage, int, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return latexTemplatePackage{}, 0, errors.New("not a valid zip package")
	}
	if len(zr.File) == 0 {
		return latexTemplatePackage{}, 0, errors.New("package is empty")
	}
	if len(zr.File) > latexTemplateMaxFiles {
		return latexTemplatePackage{}, 0, errors.New("package contains too many files")
	}
	out := latexTemplatePackage{}
	texFiles := make([]string, 0, 8)
	// Publisher zips wrap everything in one directory. The desktop strips that
	// directory when it installs the pack, so the catalogue main_file has to be
	// relative to the same root or the installed document opens the wrong file.
	root := latexTemplateArchiveRoot(zr)
	for _, file := range zr.File {
		name := strings.ReplaceAll(file.Name, "\\", "/")
		// Absolute POSIX paths and Windows drive prefixes are rejected outright.
		// The hub itself never extracts entries, but the desktop client unpacks
		// this package, so the package is the trust boundary.
		if strings.HasPrefix(name, "/") || (len(name) >= 2 && name[1] == ':') {
			return latexTemplatePackage{}, 0, errors.New("package contains an unsafe path")
		}
		// Reject only names whose dot-segments climb out of the archive root.
		// path.Clean clamps leading ".." only when a root is present, so clean
		// the RELATIVE name: "a/../../x" becomes "../x" and is caught here,
		// while "./main.tex" and "a/../b" stay inside and remain legal.
		if clean := path.Clean(name); clean == ".." || strings.HasPrefix(clean, "../") {
			return latexTemplatePackage{}, 0, errors.New("package contains an unsafe path")
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.Mode().IsRegular() {
			return latexTemplatePackage{}, 0, errors.New("package contains an unsupported file type")
		}
		stripped, stripErr := latexTemplateStripRoot(name, root)
		if stripErr != nil {
			return latexTemplatePackage{}, 0, stripErr
		}
		if stripped == "" {
			continue
		}
		lower := strings.ToLower(stripped)
		if lower == latexTemplateManifestName {
			rc, openErr := file.Open()
			if openErr != nil {
				return latexTemplatePackage{}, 0, errors.New("cannot read package manifest")
			}
			raw, readErr := io.ReadAll(io.LimitReader(rc, latexTemplateManifestMax))
			_ = rc.Close()
			if readErr != nil {
				return latexTemplatePackage{}, 0, errors.New("cannot read package manifest")
			}
			if jsonErr := json.Unmarshal(raw, &out); jsonErr != nil {
				return latexTemplatePackage{}, 0, errors.New("package manifest is not valid JSON")
			}
			continue
		}
		if ext := path.Ext(lower); !latexTemplateFileAllowed(lower, ext) {
			label := ext
			if label == "" {
				label = path.Base(lower)
			}
			return latexTemplatePackage{}, 0, fmt.Errorf("package contains an unsupported file type: %s", label)
		}
		if strings.HasSuffix(lower, ".tex") {
			texFiles = append(texFiles, stripped)
		}
	}
	if len(texFiles) == 0 {
		return latexTemplatePackage{}, 0, errors.New("package contains no .tex source file")
	}
	if strings.TrimSpace(out.Name) == "" {
		out.Name = strings.TrimSuffix(strings.TrimSpace(fallbackName), filepath.Ext(fallbackName))
	}
	if strings.TrimSpace(out.Name) == "" {
		out.Name = "LaTeX 模板"
	}
	if strings.TrimSpace(out.Version) == "" {
		out.Version = "1.0.0"
	}
	if len(out.Version) > 32 {
		return latexTemplatePackage{}, 0, errors.New("template version is too long")
	}
	mainFile := strings.TrimSpace(strings.ReplaceAll(out.MainFile, "\\", "/"))
	if mainFile != "" && !hasLatexTexEntry(texFiles, mainFile) {
		return latexTemplatePackage{}, 0, errors.New("manifest main_file is not a .tex file in the package")
	}
	if mainFile == "" {
		// Resolve an entry point so the catalogue always advertises a usable
		// main_file, instead of leaving it to every client to guess.
		mainFile = resolveLatexTemplateMain(zr, texFiles)
	}
	out.MainFile = mainFile
	return out, len(texFiles), nil
}

func latexTemplateArchiveRoot(zr *zip.Reader) string {
	if len(zr.File) == 0 {
		return ""
	}
	first := strings.TrimPrefix(strings.ReplaceAll(zr.File[0].Name, "\\", "/"), "./")
	root, _, found := strings.Cut(first, "/")
	if !found || root == "" || root == "." {
		return ""
	}
	for _, file := range zr.File {
		slashed := strings.TrimPrefix(strings.ReplaceAll(file.Name, "\\", "/"), "./")
		if slashed == root || strings.HasPrefix(slashed, root+"/") {
			continue
		}
		return ""
	}
	return root
}

func latexTemplateStripRoot(slashed, root string) (string, error) {
	slashed = strings.TrimPrefix(strings.ReplaceAll(slashed, "\\", "/"), "./")
	if root == "" {
		return slashed, nil
	}
	if slashed == root {
		return "", nil
	}
	if !strings.HasPrefix(slashed, root+"/") {
		return "", errors.New("package layout is inconsistent")
	}
	return strings.TrimPrefix(slashed, root+"/"), nil
}

func hasLatexTexEntry(texFiles []string, mainFile string) bool {
	for _, candidate := range texFiles {
		if candidate == mainFile || strings.HasSuffix(candidate, "/"+mainFile) {
			return true
		}
	}
	return false
}

// resolveLatexTemplateMain picks the entry document the same way the desktop
// importer does: main.tex, else the first sample that declares a document
// class, else the class manual, else any sample, else the first source.
// The manual lives under doc/ and must not win just because the zip listed it
// first.
func resolveLatexTemplateMain(zr *zip.Reader, texFiles []string) string {
	// A manual named doc/main.tex must not hide the sample article.
	var manualMain string
	for _, candidate := range texFiles {
		if !strings.EqualFold(path.Base(candidate), "main.tex") {
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
	root := latexTemplateArchiveRoot(zr)
	byName := make(map[string]*zip.File, len(zr.File))
	for _, file := range zr.File {
		name := strings.TrimPrefix(strings.ReplaceAll(file.Name, "\\", "/"), "./")
		stripped, err := latexTemplateStripRoot(name, root)
		if err != nil || stripped == "" {
			continue
		}
		byName[stripped] = file
	}
	var manual, sample string
	for _, candidate := range texFiles {
		file, ok := byName[candidate]
		if !ok {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(rc, latexTemplateManifestMax))
		_ = rc.Close()
		if readErr != nil {
			continue
		}
		if !latexTemplateDocumentclassRe.Match(raw) {
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

func (h *LatexTemplateHandlers) storeLatexTemplatePackage(id string, data []byte) (string, error) {
	dir := h.packageDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, id+".zip")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ── Categories ───────────────────────────────────────────────────────────

func (h *LatexTemplateHandlers) listCategories(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_LIST_FAILED", "latex template library unavailable")
		return
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,name,description,sort_order,status,builtin FROM latex_template_categories ORDER BY sort_order,name,id`)
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_LIST_FAILED", "internal error")
		return
	}
	defer rows.Close()
	out := []LatexTemplateCategory{}
	for rows.Next() {
		var item LatexTemplateCategory
		var builtin int
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.SortOrder, &item.Status, &builtin); err != nil {
			writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_LIST_FAILED", "internal error")
			return
		}
		item.Builtin = builtin != 0
		out = append(out, item)
	}
	writeJSON(w, 200, map[string]any{"categories": out})
}

func (h *LatexTemplateHandlers) createCategory(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_CREATE_FAILED", "latex template library unavailable")
		return
	}
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		SortOrder   *int   `json:"sort_order"`
	}
	if err := decodeLimitedJSON(w, r, &in, defaultJSONBodyLimit); err != nil {
		writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 40 {
		writeError(w, 400, "INVALID_LATEX_TEMPLATE_CATEGORY", "category name is required and must be at most 40 characters")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	id := latexTemplateSlugID(in.Name)
	if id == "" {
		writeError(w, 400, "INVALID_LATEX_TEMPLATE_CATEGORY", "category name must contain letters or digits")
		return
	}
	var existing int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM latex_template_categories WHERE id=?`, id).Scan(&existing); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_CREATE_FAILED", "internal error")
		return
	}
	if existing > 0 {
		writeError(w, 409, "LATEX_TEMPLATE_CATEGORY_EXISTS", "category already exists")
		return
	}
	sortOrder := 100
	if in.SortOrder != nil {
		sortOrder = *in.SortOrder
	}
	now := latexTemplateNow()
	item := LatexTemplateCategory{ID: id, Name: in.Name, Description: strings.TrimSpace(in.Description), SortOrder: sortOrder, Status: "active"}
	if _, err := h.db.ExecContext(r.Context(), `INSERT INTO latex_template_categories(id,name,description,sort_order,status,builtin,created_at,updated_at) VALUES(?,?,?,?,'active',0,?,?)`, item.ID, item.Name, item.Description, item.SortOrder, now, now); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_CREATE_FAILED", "internal error")
		return
	}
	h.appendEvent(r.Context(), latexTemplateActor(r), "category.created", item.ID, "", nil, item)
	h.emitSync(r.Context())
	writeJSON(w, 201, item)
}

func (h *LatexTemplateHandlers) patchCategory(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_UPDATE_FAILED", "latex template library unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var in struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		SortOrder   *int    `json:"sort_order"`
		Status      *string `json:"status"`
		Reason      string  `json:"reason"`
	}
	if err := decodeLimitedJSON(w, r, &in, defaultJSONBodyLimit); err != nil {
		writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var before LatexTemplateCategory
	var builtin int
	if err := h.db.QueryRowContext(r.Context(), `SELECT id,name,description,sort_order,status,builtin FROM latex_template_categories WHERE id=?`, id).Scan(&before.ID, &before.Name, &before.Description, &before.SortOrder, &before.Status, &builtin); errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "LATEX_TEMPLATE_CATEGORY_NOT_FOUND", "category not found")
		return
	} else if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_UPDATE_FAILED", "internal error")
		return
	}
	before.Builtin = builtin != 0
	after := before
	if in.Name != nil {
		after.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		after.Description = strings.TrimSpace(*in.Description)
	}
	if in.SortOrder != nil {
		after.SortOrder = *in.SortOrder
	}
	if in.Status != nil {
		if *in.Status != "active" && *in.Status != "disabled" {
			writeError(w, 400, "INVALID_LATEX_TEMPLATE_CATEGORY", "status must be active or disabled")
			return
		}
		if before.Builtin && *in.Status != "active" {
			writeError(w, 400, "LATEX_TEMPLATE_CATEGORY_MANAGED", "built-in categories must remain active")
			return
		}
		after.Status = *in.Status
	}
	if after.Name == "" {
		writeError(w, 400, "INVALID_LATEX_TEMPLATE_CATEGORY", "category name is required")
		return
	}
	if _, err := h.db.ExecContext(r.Context(), `UPDATE latex_template_categories SET name=?,description=?,sort_order=?,status=?,updated_at=? WHERE id=?`, after.Name, after.Description, after.SortOrder, after.Status, latexTemplateNow(), id); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_UPDATE_FAILED", "internal error")
		return
	}
	h.appendEvent(r.Context(), latexTemplateActor(r), "category.updated", id, in.Reason, before, after)
	h.emitSync(r.Context())
	writeJSON(w, 200, after)
}

func (h *LatexTemplateHandlers) deleteCategory(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_DELETE_FAILED", "latex template library unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	h.mu.Lock()
	defer h.mu.Unlock()
	var builtin int
	if err := h.db.QueryRowContext(r.Context(), `SELECT builtin FROM latex_template_categories WHERE id=?`, id).Scan(&builtin); errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "LATEX_TEMPLATE_CATEGORY_NOT_FOUND", "category not found")
		return
	} else if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_DELETE_FAILED", "internal error")
		return
	}
	if builtin != 0 {
		writeError(w, 400, "LATEX_TEMPLATE_CATEGORY_MANAGED", "built-in categories cannot be deleted")
		return
	}
	var used int
	if err := h.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM latex_templates WHERE category_id=?`, id).Scan(&used); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_DELETE_FAILED", "internal error")
		return
	}
	if used > 0 {
		writeError(w, 409, "LATEX_TEMPLATE_CATEGORY_IN_USE", "move the templates in this category before deleting it")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_DELETE_FAILED", "internal error")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO latex_template_category_tombstones(id, deleted_at) VALUES(?, ?) ON CONFLICT(id) DO UPDATE SET deleted_at=excluded.deleted_at`, id, latexTemplateNow()); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_DELETE_FAILED", "internal error")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM latex_template_categories WHERE id=?`, id); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_DELETE_FAILED", "internal error")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_CATEGORY_DELETE_FAILED", "internal error")
		return
	}
	h.appendEvent(r.Context(), latexTemplateActor(r), "category.deleted", id, r.URL.Query().Get("reason"), nil, nil)
	h.emitSync(r.Context())
	writeJSON(w, 200, map[string]any{"ok": true})
}

// latexTemplateSlugID derives a stable ascii id from a (possibly Chinese)
// category name. The four built-in ids are matched first so an administrator
// renaming "会议" lands back on the seeded row instead of creating a duplicate.
// A name with no ascii letters at all (a purely Chinese label) still has to be
// addressable, so it falls back to a short digest of the name itself.
func latexTemplateSlugID(name string) string {
	switch strings.TrimSpace(name) {
	case "会议", "会议模板", "conference":
		return "conference"
	case "期刊", "期刊模板", "journal":
		return "journal"
	case "毕业论文", "学位论文", "毕业论文模板", "thesis":
		return "thesis"
	case "其它", "其他", "其它模板", "other":
		return "other"
	}
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			builder.WriteRune('-')
		}
	}
	slug := strings.Trim(builder.String(), "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if len(slug) > 40 {
		slug = slug[:40]
	}
	if slug == "" {
		// No ascii content: derive a stable id from the name so the category stays
		// addressable by clients that key on the id.
		sum := sha256.Sum256([]byte(strings.TrimSpace(name)))
		slug = "cat-" + hex.EncodeToString(sum[:])[:10]
	}
	return slug
}

func (h *LatexTemplateHandlers) categoryExists(ctx context.Context, categoryID string) bool {
	var n int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM latex_template_categories WHERE id=? AND status='active'`, categoryID).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// ── Public catalogue ─────────────────────────────────────────────────────

func (h *LatexTemplateHandlers) listApprovedTemplates(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_LIST_FAILED", "latex template library unavailable")
		return
	}
	query := `SELECT ` + latexTemplateSelectColumns + ` FROM latex_templates WHERE status='approved'`
	args := []any{}
	if category := strings.TrimSpace(r.URL.Query().Get("category")); category != "" {
		query += ` AND category_id=?`
		args = append(args, category)
	}
	query += ` ORDER BY category_id,name,id`
	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_LIST_FAILED", "internal error")
		return
	}
	defer rows.Close()
	out := []LatexTemplate{}
	for rows.Next() {
		item, err := scanLatexTemplate(rows)
		if err != nil {
			writeError(w, 500, "LATEX_TEMPLATE_LIST_FAILED", "internal error")
			return
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_LIST_FAILED", "internal error")
		return
	}
	h.writeCatalogPayload(w, r, out)
}

// writeCatalogPayload returns templates and their categories in one response so
// the desktop library renders without a second round trip.
func (h *LatexTemplateHandlers) writeCatalogPayload(w http.ResponseWriter, r *http.Request, templates []LatexTemplate) {
	categoryRows, err := h.db.QueryContext(r.Context(), `SELECT id,name,description,sort_order,status,builtin FROM latex_template_categories WHERE status='active' ORDER BY sort_order,name,id`)
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_LIST_FAILED", "internal error")
		return
	}
	defer categoryRows.Close()
	categories := []LatexTemplateCategory{}
	for categoryRows.Next() {
		var item LatexTemplateCategory
		var builtin int
		if err := categoryRows.Scan(&item.ID, &item.Name, &item.Description, &item.SortOrder, &item.Status, &builtin); err != nil {
			writeError(w, 500, "LATEX_TEMPLATE_LIST_FAILED", "internal error")
			return
		}
		item.Builtin = builtin != 0
		categories = append(categories, item)
	}
	writeJSON(w, 200, map[string]any{"templates": templates, "categories": categories})
}

func (h *LatexTemplateHandlers) downloadTemplate(w http.ResponseWriter, r *http.Request) {
	h.serveTemplateZip(w, r, latexTemplateStatusApproved)
}

func (h *LatexTemplateHandlers) serveTemplateZip(w http.ResponseWriter, r *http.Request, requireStatus string) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_DOWNLOAD_FAILED", "latex template library unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var item LatexTemplate
	var zipPath string
	if err := h.db.QueryRowContext(r.Context(), `SELECT `+latexTemplateSelectColumns+`,zip_path FROM latex_templates WHERE id=?`, id).Scan(newLatexTemplateScanTargets(&item, &zipPath)...); errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "LATEX_TEMPLATE_NOT_FOUND", "template not found")
		return
	} else if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_DOWNLOAD_FAILED", "internal error")
		return
	}
	if requireStatus != "" && item.Status != requireStatus {
		writeError(w, 404, "LATEX_TEMPLATE_NOT_FOUND", "template not found")
		return
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		writeError(w, 410, "LATEX_TEMPLATE_UNAVAILABLE", "template package is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+latexTemplateDownloadName(item)+`.zip"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(200)
	_, _ = w.Write(data)
}

// latexTemplateDownloadName builds the Content-Disposition file name. The slug is
// already restricted to [a-z0-9-], but the value is interpolated into a response
// header, so it is re-filtered here rather than trusted.
func latexTemplateDownloadName(item LatexTemplate) string {
	slug := latexTemplateSlugID(item.Name)
	var safe strings.Builder
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			safe.WriteRune(r)
		default:
			safe.WriteRune('-')
		}
	}
	cleaned := strings.Trim(safe.String(), "-")
	if cleaned == "" {
		cleaned = "latex-template-" + latexTemplateSlugID(item.ID)
	}
	return cleaned
}

// ── End-user sharing ─────────────────────────────────────────────────────

// latexTemplateSubmission is the caller-visible view of one of their own
// submissions. It carries the moderation state and note so the desktop client can
// stop showing a stale "pending review" badge once a decision has been made.
type latexTemplateSubmission struct {
	RemoteID    string `json:"remote_id"`
	Name        string `json:"name"`
	CategoryID  string `json:"category_id"`
	Status      string `json:"status"`
	ReviewNote  string `json:"review_note,omitempty"`
	DownloadURL string `json:"download_url"`
	UpdatedAt   string `json:"updated_at"`
}

// listMySubmissions handles GET /api/v1/latex-templates/submissions. It reuses
// the share endpoint's identity rules, so an owner sees exactly what they
// submitted and nothing else.
func (h *LatexTemplateHandlers) listMySubmissions(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_SUBMISSION_LIST_FAILED", "latex template library unavailable")
		return
	}
	ownerID, ownerEmail, err := h.latexTemplateUploader(r)
	if err != nil {
		writeError(w, 401, "LATEX_TEMPLATE_UNAUTHORIZED", err.Error())
		return
	}
	// The owner is tracked as a user id when one is known and as the email
	// otherwise, so both are matched to cover the two identity paths.
	rows, err := h.db.QueryContext(r.Context(), `SELECT `+latexTemplateSelectColumns+` FROM latex_templates WHERE owner_id=? OR (owner_email<>'' AND owner_email=?) ORDER BY updated_at DESC,id`, ownerID, ownerEmail)
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_SUBMISSION_LIST_FAILED", "internal error")
		return
	}
	defer rows.Close()
	out := []latexTemplateSubmission{}
	for rows.Next() {
		item, err := scanLatexTemplate(rows)
		if err != nil {
			writeError(w, 500, "LATEX_TEMPLATE_SUBMISSION_LIST_FAILED", "internal error")
			return
		}
		out = append(out, latexTemplateSubmission{
			RemoteID:    item.ID,
			Name:        item.Name,
			CategoryID:  item.CategoryID,
			Status:      item.Status,
			ReviewNote:  item.ReviewNote,
			DownloadURL: item.DownloadURL,
			UpdatedAt:   item.UpdatedAt,
		})
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_SUBMISSION_LIST_FAILED", "internal error")
		return
	}
	writeJSON(w, 200, map[string]any{"submissions": out})
}

// latexTemplateUploader identifies the account credited with a shared package.
// It mirrors the Capability Market submit contract: a valid session token wins,
// and the email form field is only a fallback for deployments still in email
// upload mode.
func (h *LatexTemplateHandlers) latexTemplateUploader(r *http.Request) (string, string, error) {
	token := extractSessionToken(r)
	if token != "" && h.market != nil && h.market.authSvc != nil {
		session, err := h.market.authSvc.ValidateSession(r.Context(), token)
		if err != nil {
			return "", "", errors.New("session expired or invalid")
		}
		email := strings.TrimSpace(session.Email)
		ownerID := strings.TrimSpace(session.UserID)
		if h.market.userSvc != nil {
			user, err := h.market.userSvc.EnsureAccountWithVerifiedContact(r.Context(), session.UserID, email)
			if err == nil && user != nil {
				ownerID = user.ID
			}
		}
		if ownerID == "" {
			ownerID = email
		}
		return ownerID, email, nil
	}
	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		return "", "", errors.New("a signed-in account or an email address is required")
	}
	ownerID := email
	if h.market != nil && h.market.userSvc != nil {
		if user, err := h.market.userSvc.EnsureAccount(r.Context(), email); err == nil && user != nil {
			ownerID = user.ID
		}
	}
	return ownerID, email, nil
}

// shareTemplate handles POST /api/v1/latex-templates (multipart/form-data).
// A shared package always lands in `pending`: only an administrator can publish
// it to the other users.
func (h *LatexTemplateHandlers) shareTemplate(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_SHARE_FAILED", "latex template library unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, latexTemplateMaxZipBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, 400, "INVALID_MULTIPART", "invalid multipart form: "+err.Error())
		return
	}
	file, header, err := r.FormFile("zip")
	if err != nil {
		writeError(w, 400, "LATEX_TEMPLATE_ZIP_REQUIRED", "a .zip package is required")
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		writeError(w, 400, "LATEX_TEMPLATE_ZIP_REQUIRED", "the package must be a .zip file")
		return
	}
	ownerID, ownerEmail, err := h.latexTemplateUploader(r)
	if err != nil {
		writeError(w, 401, "LATEX_TEMPLATE_UNAUTHORIZED", err.Error())
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, latexTemplateMaxZipBytes+1))
	if err != nil {
		writeError(w, 400, "LATEX_TEMPLATE_ZIP_INVALID", "cannot read the uploaded package")
		return
	}
	if int64(len(data)) > latexTemplateMaxZipBytes {
		writeError(w, 413, "LATEX_TEMPLATE_TOO_LARGE", "the package exceeds the 100 MB limit")
		return
	}
	// The queue check and the insert are one transition. Checking first and
	// inserting afterwards would let two concurrent shares both observe room.
	// The lock is held across the package write too: that serialises shares
	// against each other, which is acceptable for a human-reviewed library and is
	// far cheaper than a queue limit that can be raced past.
	h.mu.Lock()
	defer h.mu.Unlock()
	pending, err := h.countOwnerPending(r.Context(), ownerID)
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_SHARE_FAILED", "internal error")
		return
	}
	if pending >= latexTemplateMaxPendingPerOwner {
		writeError(w, 429, "LATEX_TEMPLATE_QUEUE_FULL", "too many templates are awaiting review; wait for a decision before sharing more")
		return
	}
	h.insertTemplate(w, r, data, header.Filename, latexTemplateShareInput{
		OwnerID:    ownerID,
		OwnerEmail: ownerEmail,
		Source:     "user",
		Status:     latexTemplateStatusPending,
		ActorID:    "user:" + ownerEmail,
		// The desktop sends name/description/category/version/author alongside the
		// package so a locally imported template keeps the category the user filed
		// it under. Form values win over the packaged manifest, matching the
		// administrator upload path.
		RequestField: func(key string) string { return r.FormValue(key) },
	})
}

type latexTemplateShareInput struct {
	OwnerID    string
	OwnerEmail string
	Source     string
	Status     string
	ReviewNote string
	ActorID    string
	// Form-field overrides, read through this accessor so a user share and an
	// administrator upload share one persistence implementation.
	RequestField func(string) string
}

// latexTemplateMaxPendingPerOwner bounds the review queue a single uploader can
// create. A shared package is always reviewed by a human, so an unbounded queue
// is an easy way to make the queue useless; the cap is per owner, not global.
const latexTemplateMaxPendingPerOwner = 20

func (h *LatexTemplateHandlers) countOwnerPending(ctx context.Context, ownerID string) (int, error) {
	var pending int
	if err := h.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM latex_templates WHERE status=? AND owner_id=?`, latexTemplateStatusPending, ownerID).Scan(&pending); err != nil {
		return 0, err
	}
	return pending, nil
}

// insertTemplate validates, stores and records a template package. Form-field
// overrides (used by the administrator upload form) are read through
// RequestField so the user share path and the admin upload path share exactly
// one persistence implementation.
func (h *LatexTemplateHandlers) insertTemplate(w http.ResponseWriter, r *http.Request, data []byte, filename string, in latexTemplateShareInput) {
	manifest, texCount, err := inspectLatexTemplateZip(data, filename)
	if err != nil {
		writeError(w, 400, "LATEX_TEMPLATE_ZIP_INVALID", err.Error())
		return
	}
	formValue := func(key string) string {
		if in.RequestField == nil {
			return ""
		}
		return strings.TrimSpace(in.RequestField(key))
	}
	categoryID := firstNonEmptyLatexTemplate(formValue("category_id"), formValue("category"), manifest.Category)
	if !h.categoryExists(r.Context(), categoryID) {
		categoryID = "other"
	}
	name := firstNonEmptyLatexTemplate(formValue("name"), manifest.Name)
	if len([]rune(name)) > 80 {
		writeError(w, 400, "LATEX_TEMPLATE_NAME_INVALID", "template name must be at most 80 characters")
		return
	}
	sum := sha256.Sum256(data)
	id := uniqueID("latex_tpl")
	zipPath, err := h.storeLatexTemplatePackage(id, data)
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_SHARE_FAILED", "cannot store the uploaded package")
		return
	}
	now := latexTemplateNow()
	item := LatexTemplate{
		ID:          id,
		Name:        name,
		Description: firstNonEmptyLatexTemplate(formValue("description"), manifest.Description),
		CategoryID:  categoryID,
		Version:     firstNonEmptyLatexTemplate(formValue("version"), manifest.Version),
		Author:      firstNonEmptyLatexTemplate(formValue("author"), manifest.Author),
		MainFile:    manifest.MainFile,
		FileCount:   texCount,
		SizeBytes:   int64(len(data)),
		SHA256:      hex.EncodeToString(sum[:]),
		Status:      in.Status,
		ReviewNote:  in.ReviewNote,
		OwnerID:     in.OwnerID,
		OwnerEmail:  in.OwnerEmail,
		Source:      in.Source,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if _, err := h.db.ExecContext(r.Context(), `INSERT INTO latex_templates(id,name,description,category_id,version,author,main_file,file_count,size_bytes,sha256,zip_path,status,review_note,owner_id,owner_email,source,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.Name, item.Description, item.CategoryID, item.Version, item.Author, item.MainFile, item.FileCount, item.SizeBytes, item.SHA256, zipPath, item.Status, item.ReviewNote, item.OwnerID, item.OwnerEmail, item.Source, item.CreatedAt, item.UpdatedAt); err != nil {
		_ = os.Remove(zipPath)
		writeError(w, 500, "LATEX_TEMPLATE_SHARE_FAILED", "internal error")
		return
	}
	actor := in.ActorID
	if actor == "" {
		actor = "user:" + in.OwnerEmail
	}
	h.appendEvent(r.Context(), actor, "template."+in.Status, item.ID, in.ReviewNote, nil, item)
	h.emitSync(r.Context())
	item.DownloadURL = "/api/v1/latex-templates/" + item.ID + "/download"
	writeJSON(w, 201, item)
}

// ── Administration ───────────────────────────────────────────────────────

func (h *LatexTemplateHandlers) adminListTemplates(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_LIST_FAILED", "latex template library unavailable")
		return
	}
	query := `SELECT ` + latexTemplateSelectColumns + ` FROM latex_templates WHERE 1=1`
	args := []any{}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		query += ` AND status=?`
		args = append(args, status)
	}
	if category := strings.TrimSpace(r.URL.Query().Get("category")); category != "" {
		query += ` AND category_id=?`
		args = append(args, category)
	}
	if keyword := strings.TrimSpace(r.URL.Query().Get("keyword")); keyword != "" {
		query += ` AND (name LIKE ? OR description LIKE ? OR author LIKE ? OR owner_email LIKE ?)`
		like := "%" + keyword + "%"
		args = append(args, like, like, like, like)
	}
	query += ` ORDER BY CASE status WHEN 'pending' THEN 0 WHEN 'approved' THEN 1 ELSE 2 END, updated_at DESC, id`
	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_LIST_FAILED", "internal error")
		return
	}
	defer rows.Close()
	out := []LatexTemplate{}
	for rows.Next() {
		item, err := scanLatexTemplate(rows)
		if err != nil {
			writeError(w, 500, "LATEX_TEMPLATE_LIST_FAILED", "internal error")
			return
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_LIST_FAILED", "internal error")
		return
	}
	categoryRows, err := h.db.QueryContext(r.Context(), `SELECT id,name,description,sort_order,status,builtin FROM latex_template_categories ORDER BY sort_order,name,id`)
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_LIST_FAILED", "internal error")
		return
	}
	defer categoryRows.Close()
	categories := []LatexTemplateCategory{}
	for categoryRows.Next() {
		var item LatexTemplateCategory
		var builtin int
		if err := categoryRows.Scan(&item.ID, &item.Name, &item.Description, &item.SortOrder, &item.Status, &builtin); err != nil {
			writeError(w, 500, "LATEX_TEMPLATE_LIST_FAILED", "internal error")
			return
		}
		item.Builtin = builtin != 0
		categories = append(categories, item)
	}
	writeJSON(w, 200, map[string]any{"templates": out, "categories": categories, "total": len(out)})
}

// adminUploadTemplate handles POST /api/admin/latex-templates. Administrator
// uploads are published immediately: the operator is the reviewer.
func (h *LatexTemplateHandlers) adminUploadTemplate(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_UPLOAD_FAILED", "latex template library unavailable")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, latexTemplateMaxZipBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, 400, "INVALID_MULTIPART", "invalid multipart form: "+err.Error())
		return
	}
	file, header, err := r.FormFile("zip")
	if err != nil {
		writeError(w, 400, "LATEX_TEMPLATE_ZIP_REQUIRED", "a .zip package is required")
		return
	}
	defer file.Close()
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		writeError(w, 400, "LATEX_TEMPLATE_ZIP_REQUIRED", "the package must be a .zip file")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, latexTemplateMaxZipBytes+1))
	if err != nil {
		writeError(w, 400, "LATEX_TEMPLATE_ZIP_INVALID", "cannot read the uploaded package")
		return
	}
	if int64(len(data)) > latexTemplateMaxZipBytes {
		writeError(w, 413, "LATEX_TEMPLATE_TOO_LARGE", "the package exceeds the 100 MB limit")
		return
	}
	h.insertTemplate(w, r, data, header.Filename, latexTemplateShareInput{
		Source:     "admin",
		Status:     latexTemplateStatusApproved,
		ReviewNote: strings.TrimSpace(r.FormValue("review_note")),
		ActorID:    latexTemplateActor(r),
		// The administrator form is authoritative for name/category/version/author
		// when it supplies them, so an uploaded pack can be filed correctly
		// without editing the manifest first.
		RequestField: func(key string) string { return r.FormValue(key) },
	})
}

func (h *LatexTemplateHandlers) adminPatchTemplate(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_UPDATE_FAILED", "latex template library unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var in struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		CategoryID  *string `json:"category_id"`
		Version     *string `json:"version"`
		Author      *string `json:"author"`
		Reason      string  `json:"reason"`
	}
	if err := decodeLimitedJSON(w, r, &in, defaultJSONBodyLimit); err != nil {
		writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	before, err := scanLatexTemplate(h.db.QueryRowContext(r.Context(), `SELECT `+latexTemplateSelectColumns+` FROM latex_templates WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "LATEX_TEMPLATE_NOT_FOUND", "template not found")
		return
	}
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_UPDATE_FAILED", "internal error")
		return
	}
	after := before
	if in.Name != nil {
		after.Name = strings.TrimSpace(*in.Name)
	}
	if in.Description != nil {
		after.Description = strings.TrimSpace(*in.Description)
	}
	if in.Version != nil {
		after.Version = strings.TrimSpace(*in.Version)
	}
	if in.Author != nil {
		after.Author = strings.TrimSpace(*in.Author)
	}
	if in.CategoryID != nil {
		categoryID := strings.TrimSpace(*in.CategoryID)
		if !h.categoryExists(r.Context(), categoryID) {
			writeError(w, 400, "INVALID_LATEX_TEMPLATE_CATEGORY", "category not found")
			return
		}
		after.CategoryID = categoryID
	}
	if after.Name == "" {
		writeError(w, 400, "INVALID_LATEX_TEMPLATE_NAME", "template name is required")
		return
	}
	if after == before {
		// A patch that changes nothing should not bump updated_at: clients use it
		// to decide whether to re-download the package.
		writeJSON(w, 200, after)
		return
	}
	if _, err := h.db.ExecContext(r.Context(), `UPDATE latex_templates SET name=?,description=?,category_id=?,version=?,author=?,updated_at=? WHERE id=?`, after.Name, after.Description, after.CategoryID, after.Version, after.Author, latexTemplateNow(), id); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_UPDATE_FAILED", "internal error")
		return
	}
	h.appendEvent(r.Context(), latexTemplateActor(r), "template.updated", id, in.Reason, before, after)
	h.emitSync(r.Context())
	writeJSON(w, 200, after)
}

// adminReviewTemplate handles POST /api/admin/latex-templates/{id}/review.
// The action travels in the body so one route owns the whole review state
// machine (approve / reject / unlist) and cannot drift into an unknown
// PathValue.
func (h *LatexTemplateHandlers) adminReviewTemplate(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_REVIEW_FAILED", "latex template library unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var in struct {
		Action     string `json:"action"`
		Reason     string `json:"reason"`
		CategoryID string `json:"category_id"`
	}
	if err := decodeLimitedJSON(w, r, &in, defaultJSONBodyLimit); err != nil {
		writeJSONDecodeError(w, err, "INVALID_JSON", "Invalid request body")
		return
	}
	target := strings.ToLower(strings.TrimSpace(in.Action))
	status := map[string]string{
		"approve": latexTemplateStatusApproved,
		"reject":  latexTemplateStatusRejected,
		"unlist":  latexTemplateStatusRejected,
	}[target]
	if status == "" {
		writeError(w, 400, "LATEX_TEMPLATE_ACTION_UNKNOWN", "action must be approve, reject, or unlist")
		return
	}
	if strings.TrimSpace(in.Reason) == "" {
		writeError(w, 400, "LATEX_TEMPLATE_REASON_REQUIRED", "a moderation note is required")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	before, err := scanLatexTemplate(h.db.QueryRowContext(r.Context(), `SELECT `+latexTemplateSelectColumns+` FROM latex_templates WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "LATEX_TEMPLATE_NOT_FOUND", "template not found")
		return
	}
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_REVIEW_FAILED", "internal error")
		return
	}
	after := before
	after.Status = status
	after.ReviewNote = strings.TrimSpace(in.Reason)
	if categoryID := strings.TrimSpace(in.CategoryID); categoryID != "" {
		if !h.categoryExists(r.Context(), categoryID) {
			writeError(w, 400, "INVALID_LATEX_TEMPLATE_CATEGORY", "category not found")
			return
		}
		after.CategoryID = categoryID
	}
	if _, err := h.db.ExecContext(r.Context(), `UPDATE latex_templates SET status=?,review_note=?,category_id=?,updated_at=? WHERE id=?`, after.Status, after.ReviewNote, after.CategoryID, latexTemplateNow(), id); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_REVIEW_FAILED", "internal error")
		return
	}
	h.appendEvent(r.Context(), latexTemplateActor(r), "template."+target, id, in.Reason, before, after)
	h.emitSync(r.Context())
	writeJSON(w, 200, after)
}

func (h *LatexTemplateHandlers) adminDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_DELETE_FAILED", "latex template library unavailable")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	// The blob filename derives from this id; keep it a single clean path
	// segment so it can never traverse out of the package directory.
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id {
		writeError(w, 404, "LATEX_TEMPLATE_NOT_FOUND", "template not found")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	before, err := scanLatexTemplate(h.db.QueryRowContext(r.Context(), `SELECT `+latexTemplateSelectColumns+` FROM latex_templates WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "LATEX_TEMPLATE_NOT_FOUND", "template not found")
		return
	}
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_DELETE_FAILED", "internal error")
		return
	}
	tx, err := h.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_DELETE_FAILED", "internal error")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO latex_template_tombstones(id, deleted_at) VALUES(?, ?) ON CONFLICT(id) DO UPDATE SET deleted_at=excluded.deleted_at`, id, latexTemplateNow()); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_DELETE_FAILED", "internal error")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM latex_templates WHERE id=?`, id); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_DELETE_FAILED", "internal error")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_DELETE_FAILED", "internal error")
		return
	}
	_ = os.Remove(filepath.Join(h.packageDir(), id+".zip"))
	h.appendEvent(r.Context(), latexTemplateActor(r), "template.deleted", id, r.URL.Query().Get("reason"), before, nil)
	h.emitSync(r.Context())
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (h *LatexTemplateHandlers) adminListEvents(w http.ResponseWriter, r *http.Request) {
	if err := h.ensureSchema(); err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_EVENT_LIST_FAILED", "latex template library unavailable")
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	rows, err := h.db.QueryContext(r.Context(), `SELECT id,actor_id,action,target_id,reason,created_at FROM latex_template_events ORDER BY created_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		writeError(w, 500, "LATEX_TEMPLATE_EVENT_LIST_FAILED", "internal error")
		return
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var id, actor, action, targetID, reason, createdAt string
		if err := rows.Scan(&id, &actor, &action, &targetID, &reason, &createdAt); err != nil {
			writeError(w, 500, "LATEX_TEMPLATE_EVENT_LIST_FAILED", "internal error")
			return
		}
		out = append(out, map[string]string{"id": id, "actor_id": actor, "action": action, "target_id": targetID, "reason": reason, "created_at": createdAt})
	}
	writeJSON(w, 200, map[string]any{"events": out})
}

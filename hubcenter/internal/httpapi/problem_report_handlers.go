package httpapi

import (
	"archive/zip"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	_ "golang.org/x/image/webp"
)

var problemReportScreenshotNamePattern = regexp.MustCompile(`^screenshot-[0-9]{2}\.(png|jpg|webp)$`)

const (
	problemReportMaxUploadBytes      = 120 << 20
	problemReportMaxDiagnosticsBytes = 100 << 20
	problemReportMaxDescriptionBytes = 32 << 10
	problemReportMaxOSVersionBytes   = 512
	problemReportMaxScreenshotBytes  = 10 << 20
	problemReportMaxScreenshotPixels = 50_000_000
	problemReportAutoArchiveAge      = 100 * 24 * time.Hour
	problemReportMaxCompressionRatio = 200
	problemReportOriginLinkTTL       = time.Minute
	problemReportOriginClockSkew     = 30 * time.Second
)

type ProblemReportHandlers struct {
	store                  *skillmarket.Store
	auth                   *skillmarket.AuthService
	dataDir                string
	publicBaseURL          func(context.Context) (string, error)
	haPublicOrigin         func() string
	haSignPeerRequest      func(*http.Request) error
	haClusterSecret        func() string
	haInternalURLForOrigin func(string) string
	haOwnsPublicOrigin     func(string) bool
}

func NewProblemReportHandlers(store *skillmarket.Store, auth *skillmarket.AuthService, dataDir string) *ProblemReportHandlers {
	return &ProblemReportHandlers{store: store, auth: auth, dataDir: dataDir}
}

func (h *ProblemReportHandlers) SetOriginURLProvider(provider func(context.Context) (string, error)) {
	if h != nil {
		h.publicBaseURL = provider
	}
}

// SetHAPublicOriginProvider supplies this node's fixed client-facing URL.
// Unlike the ordinary public_base_url setting, it is not shared between HA
// nodes and is therefore safe to persist as an attachment origin.
func (h *ProblemReportHandlers) SetHAPublicOriginProvider(provider func() string) {
	if h != nil {
		h.haPublicOrigin = provider
	}
}

func (h *ProblemReportHandlers) SetHASigner(signer func(*http.Request) error) {
	if h != nil {
		h.haSignPeerRequest = signer
	}
}

func (h *ProblemReportHandlers) SetHAClusterSecretProvider(provider func() string) {
	if h != nil {
		h.haClusterSecret = provider
	}
}

// SetHAInternalURLResolver selects the authenticated HA transport endpoint for
// an attachment origin. Public URLs remain in report metadata for downloads,
// while administrative mutations use the peer's direct advertised endpoint.
func (h *ProblemReportHandlers) SetHAInternalURLResolver(resolver func(string) string) {
	if h != nil {
		h.haInternalURLForOrigin = resolver
	}
}

// SetHAOriginOwnershipResolver identifies whether a report's public origin is
// this HA node. It must use node-local configuration rather than the shared
// system public_base_url setting, which is replicated between HA nodes.
func (h *ProblemReportHandlers) SetHAOriginOwnershipResolver(resolver func(string) bool) {
	if h != nil {
		h.haOwnsPublicOrigin = resolver
	}
}

func (h *ProblemReportHandlers) originURL(ctx context.Context) string {
	if h != nil && h.haPublicOrigin != nil {
		if value := normalizeProblemReportOriginURL(h.haPublicOrigin()); value != "" {
			return value
		}
	}
	if h != nil && h.publicBaseURL != nil {
		if value, err := h.publicBaseURL(ctx); err == nil {
			return normalizeProblemReportOriginURL(value)
		}
	}
	return ""
}

func normalizeProblemReportOriginURL(raw string) string {
	uri, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (uri.Scheme != "http" && uri.Scheme != "https") || uri.Host == "" || uri.User != nil || uri.RawQuery != "" || uri.Fragment != "" {
		return ""
	}
	uri.Path = strings.TrimRight(uri.EscapedPath(), "/")
	uri.RawPath = ""
	return strings.TrimRight(uri.String(), "/")
}

// RunProblemReportArchiver archives pending reports older than 100 days. The
// application owns its lifecycle, so it stops cleanly with the HubCenter.
func RunProblemReportArchiver(ctx context.Context, store *skillmarket.Store) {
	if store == nil {
		return
	}
	archive := func() {
		_, _ = store.ArchiveUnprocessedProblemReports(ctx, time.Now().Add(-problemReportAutoArchiveAge))
	}
	archive()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			archive()
		}
	}
}

func (h *ProblemReportHandlers) authenticatedUser(r *http.Request) (*skillmarket.SkillMarketUser, bool) {
	if h == nil || h.auth == nil {
		return nil, false
	}
	user, err := h.auth.CurrentUser(r.Context(), extractSessionToken(r))
	return user, err == nil && user != nil
}

func newProblemReportID() string {
	raw := make([]byte, 6)
	_, _ = rand.Read(raw)
	return "BR-" + time.Now().UTC().Format("20060102") + "-" + strings.ToUpper(hex.EncodeToString(raw))
}

func problemReportStorageRoot(dataDir, reportID string) (string, bool) {
	reportID = strings.TrimSpace(reportID)
	if reportID == "" || len(reportID) > 128 || reportID == "." || reportID == ".." || strings.ContainsAny(reportID, `/\\`) || filepath.Base(reportID) != reportID {
		return "", false
	}
	for _, r := range reportID {
		if !(r == '-' || r == '_' || r == '.' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return "", false
		}
	}
	return filepath.Join(dataDir, "problem-reports", reportID), true
}

// stageProblemReportStorage keeps attachments recoverable until the metadata
// deletion (and its HA tombstone) has committed. Deleting files first could
// otherwise leave a live report pointing at irretrievably missing diagnostics.
func stageProblemReportStorage(root string) (string, error) {
	staged := root + ".deleting-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
	if err := os.Rename(root, staged); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return staged, nil
}

func restoreStagedProblemReportStorage(staged, root string) {
	if staged == "" {
		return
	}
	_ = os.Rename(staged, root)
}

func removeStagedProblemReportStorage(staged string) {
	if staged != "" {
		_ = os.RemoveAll(staged)
	}
}

func (h *ProblemReportHandlers) Create(w http.ResponseWriter, r *http.Request) {
	user, ok := h.authenticatedUser(r)
	if !ok {
		smError(w, http.StatusUnauthorized, "HubCenter sign-in required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, problemReportMaxUploadBytes)
	if err := r.ParseMultipartForm(problemReportMaxUploadBytes); err != nil {
		smError(w, http.StatusBadRequest, "invalid report form: "+err.Error())
		return
	}
	// Multipart files exceeding the in-memory threshold are backed by temporary
	// files. Remove them after this request so repeated large diagnostics uploads
	// cannot gradually fill the HubCenter host's temporary directory.
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	osVersion, description := strings.TrimSpace(r.FormValue("os_version")), strings.TrimSpace(r.FormValue("description"))
	guiVersion := strings.TrimSpace(r.FormValue("gui_version"))
	if osVersion == "" || description == "" {
		smError(w, http.StatusBadRequest, "os_version and description are required")
		return
	}
	if len(osVersion) > problemReportMaxOSVersionBytes || len(description) > problemReportMaxDescriptionBytes {
		smError(w, http.StatusBadRequest, "report text is too long")
		return
	}
	if len(guiVersion) > 256 {
		smError(w, http.StatusBadRequest, "GUI version is too long")
		return
	}
	diagnostics, header, err := r.FormFile("diagnostics")
	if err != nil {
		smError(w, http.StatusBadRequest, "diagnostics ZIP is required")
		return
	}
	defer diagnostics.Close()
	if header.Size <= 0 || header.Size > problemReportMaxDiagnosticsBytes {
		smError(w, http.StatusRequestEntityTooLarge, "diagnostics ZIP exceeds the 100 MB limit")
		return
	}
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		smError(w, http.StatusBadRequest, "diagnostics must use the .zip extension")
		return
	}
	id := newProblemReportID()
	root, safeRoot := problemReportStorageRoot(h.dataDir, id)
	if !safeRoot {
		smError(w, http.StatusInternalServerError, "generate report storage identifier")
		return
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o750); err != nil {
		smError(w, http.StatusInternalServerError, "create report storage: "+err.Error())
		return
	}
	// Never reuse an existing report directory. Apart from protecting against the
	// extremely unlikely random-ID collision, this prevents a stale or manually
	// created directory from being overwritten by a new submission.
	if err := os.Mkdir(root, 0o750); err != nil {
		if os.IsExist(err) {
			smError(w, http.StatusConflict, "report identifier already exists; please submit again")
			return
		}
		smError(w, http.StatusInternalServerError, "create report storage: "+err.Error())
		return
	}
	diagnosticsPath := filepath.Join(root, "diagnostics.zip")
	if err := copyUploadedFile(diagnosticsPath, diagnostics); err != nil {
		_ = os.RemoveAll(root)
		smError(w, http.StatusInternalServerError, "save diagnostics: "+err.Error())
		return
	}
	if err := validateProblemReportZIP(diagnosticsPath); err != nil {
		_ = os.RemoveAll(root)
		smError(w, http.StatusBadRequest, "invalid diagnostics ZIP: "+err.Error())
		return
	}
	screenshots := []string{}
	for _, fh := range r.MultipartForm.File["screenshots"] {
		if len(screenshots) >= 12 || fh.Size <= 0 || fh.Size > problemReportMaxScreenshotBytes {
			continue
		}
		format, err := validatedProblemScreenshotFormat(fh)
		if err != nil {
			continue
		}
		in, err := fh.Open()
		if err != nil {
			continue
		}
		name := fmt.Sprintf("screenshot-%02d.%s", len(screenshots)+1, format)
		path := filepath.Join(root, name)
		if err := copyUploadedFile(path, in); err == nil {
			screenshots = append(screenshots, name)
		}
		in.Close()
	}
	now := time.Now().UTC()
	originURL := h.originURL(r.Context())
	if originURL == "" {
		_ = os.RemoveAll(root)
		smError(w, http.StatusServiceUnavailable, "a valid HubCenter public base URL is required before submitting a problem report")
		return
	}
	report := &skillmarket.ProblemReport{ID: id, ReporterUserID: user.ID, ReporterContact: user.Email, OSVersion: osVersion, GUIVersion: guiVersion, Description: description, Status: "pending", DiagnosticsPath: "diagnostics.zip", ScreenshotPaths: screenshots, OriginURL: originURL, CreatedAt: now, UpdatedAt: now}
	if err := h.store.CreateProblemReport(r.Context(), report); err != nil {
		_ = os.RemoveAll(root)
		smError(w, http.StatusInternalServerError, "save report: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "status": report.Status, "created_at": now})
}

func validateProblemReportZIP(path string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("must be a readable ZIP")
	}
	defer archive.Close()
	if len(archive.File) == 0 || len(archive.File) > 10_000 {
		return fmt.Errorf("unexpected ZIP entry count")
	}
	var total uint64
	seenNames := make(map[string]bool, len(archive.File))
	for _, file := range archive.File {
		cleanName, ok := normalizedProblemReportZIPEntryName(file.Name)
		if !ok {
			return fmt.Errorf("contains an unsafe entry path")
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("contains a symbolic link")
		}
		isDirectory := file.FileInfo().IsDir()
		if !isDirectory && !file.Mode().IsRegular() {
			return fmt.Errorf("contains a non-regular file")
		}
		// Compare normalized paths case-insensitively. Reports can be inspected
		// on Windows or the default macOS filesystem, where case-only duplicates
		// would otherwise overwrite one another during extraction.
		comparisonName := strings.ToLower(cleanName)
		if _, exists := seenNames[comparisonName]; exists {
			return fmt.Errorf("contains duplicate entry paths")
		}
		for existingName, existingIsDirectory := range seenNames {
			if (existingName == comparisonName || strings.HasPrefix(existingName, comparisonName+"/")) && !isDirectory {
				return fmt.Errorf("contains file-directory path conflicts")
			}
			if strings.HasPrefix(comparisonName, existingName+"/") && !existingIsDirectory {
				return fmt.Errorf("contains file-directory path conflicts")
			}
		}
		seenNames[comparisonName] = isDirectory
		if file.UncompressedSize64 > 2<<30-total {
			return fmt.Errorf("uncompressed diagnostics exceed 2 GB")
		}
		if !isDirectory && file.UncompressedSize64 > 0 && (file.CompressedSize64 == 0 || file.UncompressedSize64/file.CompressedSize64 > problemReportMaxCompressionRatio) {
			return fmt.Errorf("diagnostics entry has an unsafe compression ratio")
		}
		total += file.UncompressedSize64
	}
	return nil
}

// ZIP paths always use forward slashes, but an untrusted archive can contain
// backslashes. Normalize both separators before validating so a report later
// unpacked on Windows cannot escape its intended diagnostics directory.
func isSafeProblemReportZIPEntryName(name string) bool {
	_, ok := normalizedProblemReportZIPEntryName(name)
	return ok
}

func normalizedProblemReportZIPEntryName(name string) (string, bool) {
	name = strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "//") {
		return "", false
	}
	cleanName := pathpkg.Clean(name)
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
		return "", false
	}
	// A colon in the first segment denotes a drive-qualified Windows path
	// (for example C:/temp). Diagnostics generated by MaClaw never need this.
	firstSegment, _, _ := strings.Cut(cleanName, "/")
	if strings.Contains(firstSegment, ":") {
		return "", false
	}
	return cleanName, true
}

func validatedProblemScreenshotFormat(header *multipart.FileHeader) (string, error) {
	in, err := header.Open()
	if err != nil {
		return "", err
	}
	defer in.Close()
	config, format, err := image.DecodeConfig(in)
	if err != nil {
		return "", err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > problemReportMaxScreenshotPixels {
		return "", fmt.Errorf("invalid image dimensions")
	}
	switch format {
	case "png":
		return "png", nil
	case "jpeg":
		return "jpg", nil
	case "webp":
		return "webp", nil
	default:
		return "", fmt.Errorf("unsupported image format")
	}
}

func copyUploadedFile(path string, source io.Reader) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, source)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (h *ProblemReportHandlers) ListMine(w http.ResponseWriter, r *http.Request) {
	user, ok := h.authenticatedUser(r)
	if !ok {
		smError(w, http.StatusUnauthorized, "HubCenter sign-in required")
		return
	}
	items, total, err := h.store.ListProblemReports(r.Context(), user.ID, strings.TrimSpace(r.URL.Query().Get("status")), 0, 100)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range items {
		items[i].ReporterContact = ""
		items[i].DiagnosticsPath = ""
		items[i].ScreenshotPaths = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (h *ProblemReportHandlers) AdminList(w http.ResponseWriter, r *http.Request) {
	_, _ = h.store.ArchiveUnprocessedProblemReports(r.Context(), time.Now().Add(-problemReportAutoArchiveAge))
	items, total, err := h.store.ListProblemReports(r.Context(), "", strings.TrimSpace(r.URL.Query().Get("status")), 0, 300)
	if err != nil {
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (h *ProblemReportHandlers) AdminUpdate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Status    string `json:"status"`
		AdminNote string `json:"admin_note"`
	}
	if !decodeSkillMarketJSON(w, r, &req, skillMarketAuthJSONBodyLimit) {
		return
	}
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if status != "pending" && status != "fixed" && status != "deferred" && status != "rejected" && status != "archived" {
		smError(w, http.StatusBadRequest, "invalid status")
		return
	}
	archived := time.Time{}
	if status == "archived" {
		archived = time.Now().UTC()
	}
	if len(req.AdminNote) > problemReportMaxDescriptionBytes {
		smError(w, http.StatusBadRequest, "administrator note is too long")
		return
	}
	if err := h.store.UpdateProblemReport(r.Context(), r.PathValue("id"), status, strings.TrimSpace(req.AdminNote), archived); err != nil {
		if err == skillmarket.ErrNotFound {
			smError(w, http.StatusNotFound, "report not found")
			return
		}
		smError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": status})
}

func (h *ProblemReportHandlers) AdminDelete(w http.ResponseWriter, r *http.Request) {
	report, err := h.store.GetProblemReport(r.Context(), r.PathValue("id"))
	if err != nil {
		smError(w, http.StatusNotFound, "report not found")
		return
	}
	if h.isRemoteOrigin(report) {
		if err := h.deleteFromOrigin(r.Context(), report); err != nil {
			smError(w, http.StatusBadGateway, "delete report from origin server: "+err.Error())
			return
		}
		// Attachments live only on the origin.  Once that node has acknowledged
		// their deletion, remove this replica's metadata too instead of waiting
		// for an asynchronous HA pull.  DeleteProblemReport also writes the
		// tombstone, preventing an older HA snapshot from reviving the report.
		if err := h.store.DeleteProblemReport(r.Context(), report.ID); err != nil && err != skillmarket.ErrNotFound {
			smError(w, http.StatusInternalServerError, "remove replicated report metadata: "+err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	root, safeRoot := problemReportStorageRoot(h.dataDir, report.ID)
	if !safeRoot {
		smError(w, http.StatusNotFound, "report not found")
		return
	}
	staged, err := stageProblemReportStorage(root)
	if err != nil {
		smError(w, http.StatusInternalServerError, "prepare attachment cleanup: "+err.Error())
		return
	}
	if err = h.store.DeleteProblemReport(r.Context(), report.ID); err != nil {
		restoreStagedProblemReportStorage(staged, root)
		smError(w, http.StatusInternalServerError, "report deletion failed: "+err.Error())
		return
	}
	removeStagedProblemReportStorage(staged)
	w.WriteHeader(http.StatusNoContent)
}

func (h *ProblemReportHandlers) AdminDownload(w http.ResponseWriter, r *http.Request) {
	report, err := h.store.GetProblemReport(r.Context(), r.PathValue("id"))
	if err != nil {
		smError(w, http.StatusNotFound, "report not found")
		return
	}
	requestedName := r.PathValue("file")
	name := filepath.Base(requestedName)
	if name != requestedName || name == "." || name == "" || !h.permittedProblemReportAttachment(report, name) {
		smError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if h.isRemoteOrigin(report) {
		h.redirectToOriginAttachment(w, r, report, name)
		return
	}
	h.serveProblemReportAttachment(w, r, report.ID, name)
}

// AdminAttachmentLink returns a short-lived, directly downloadable URL only
// when the report lives on a peer. Keeping local files on the authenticated
// download endpoint means an administrator's bearer token never appears in a
// URL, while peer files still open from their original upload server.
func (h *ProblemReportHandlers) AdminAttachmentLink(w http.ResponseWriter, r *http.Request) {
	report, err := h.store.GetProblemReport(r.Context(), r.PathValue("id"))
	if err != nil {
		smError(w, http.StatusNotFound, "report not found")
		return
	}
	requestedName := r.PathValue("file")
	name := filepath.Base(requestedName)
	if name != requestedName || name == "." || name == "" || !h.permittedProblemReportAttachment(report, name) {
		smError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if !h.isRemoteOrigin(report) {
		writeJSON(w, http.StatusOK, map[string]string{"url": ""})
		return
	}
	if h.haClusterSecret == nil {
		smError(w, http.StatusServiceUnavailable, "attachment origin signing is unavailable")
		return
	}
	location := h.originAttachmentURL(report, name)
	if location == "" {
		smError(w, http.StatusServiceUnavailable, "attachment origin signing is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": location})
}

// AdminAttachmentManifest obtains a remote report's attachment names on demand.
// Names are never persisted on the peer, keeping HA snapshots free of attachment
// metadata while still allowing administrators to inspect original screenshots.
func (h *ProblemReportHandlers) AdminAttachmentManifest(w http.ResponseWriter, r *http.Request) {
	report, err := h.store.GetProblemReport(r.Context(), r.PathValue("id"))
	if err != nil {
		smError(w, http.StatusNotFound, "report not found")
		return
	}
	if !h.isRemoteOrigin(report) {
		writeJSON(w, http.StatusOK, map[string][]string{"items": append([]string{"diagnostics.zip"}, report.ScreenshotPaths...)})
		return
	}
	items, err := h.attachmentManifestFromOrigin(r.Context(), report)
	if err != nil {
		smError(w, http.StatusBadGateway, "load attachments from origin server: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"items": items})
}

func (h *ProblemReportHandlers) isRemoteOrigin(report *skillmarket.ProblemReport) bool {
	if report == nil {
		return false
	}
	originURL := normalizeProblemReportOriginURL(report.OriginURL)
	if originURL == "" {
		return false
	}
	if h != nil && h.haOwnsPublicOrigin != nil {
		return !h.haOwnsPublicOrigin(originURL)
	}
	return !strings.EqualFold(originURL, h.originURL(context.Background()))
}

func (h *ProblemReportHandlers) permittedProblemReportAttachment(report *skillmarket.ProblemReport, name string) bool {
	if name == "diagnostics.zip" || containsProblemReportAttachment(report.ScreenshotPaths, name) {
		return true
	}
	// A peer intentionally does not sync screenshot names. Restrict the on-demand
	// request to the names the origin itself can own; the origin performs the final
	// exact manifest check before serving any bytes.
	return h.isRemoteOrigin(report) && problemReportScreenshotNamePattern.MatchString(name)
}

func (h *ProblemReportHandlers) originAttachmentURL(report *skillmarket.ProblemReport, name string) string {
	originURL := normalizeProblemReportOriginURL(report.OriginURL)
	if originURL == "" {
		return ""
	}
	base := originURL + "/api/v1/internal/ha/problem-reports/" + url.PathEscape(report.ID) + "/attachments/" + url.PathEscape(name)
	expiresAt := time.Now().UTC().Add(problemReportOriginLinkTTL).Unix()
	signature := h.originAttachmentSignature(report.ID, name, expiresAt)
	if signature == "" {
		return ""
	}
	return base + "?expires_at=" + strconv.FormatInt(expiresAt, 10) + "&signature=" + url.QueryEscape(signature)
}

func (h *ProblemReportHandlers) redirectToOriginAttachment(w http.ResponseWriter, r *http.Request, report *skillmarket.ProblemReport, name string) {
	if normalizeProblemReportOriginURL(report.OriginURL) == "" || h.haClusterSecret == nil {
		smError(w, http.StatusNotFound, "attachment origin is unavailable")
		return
	}
	location := h.originAttachmentURL(report, name)
	if location == "" {
		smError(w, http.StatusServiceUnavailable, "attachment origin signing is unavailable")
		return
	}
	// The link is intentionally short lived and must not be forwarded by the
	// browser when the downloaded ZIP or image is opened elsewhere.
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, location, http.StatusTemporaryRedirect)
}

func (h *ProblemReportHandlers) originAttachmentSignature(reportID, name string, expiresAt int64) string {
	if h == nil || h.haClusterSecret == nil || expiresAt <= 0 {
		return ""
	}
	secret := strings.TrimSpace(h.haClusterSecret())
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%s\n%s\n%d", reportID, name, expiresAt)
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *ProblemReportHandlers) validOriginAttachmentSignature(reportID, name string, expiresAt int64, signature string) bool {
	now := time.Now().UTC()
	// HA nodes may differ by a few seconds. Accept a bounded forward skew while
	// retaining a short, non-renewable download window.
	if expiresAt < now.Unix() || expiresAt > now.Add(problemReportOriginLinkTTL+problemReportOriginClockSkew).Unix() {
		return false
	}
	expected := h.originAttachmentSignature(reportID, name, expiresAt)
	provided, err := hex.DecodeString(strings.TrimSpace(signature))
	expectedBytes, expectedErr := hex.DecodeString(expected)
	return err == nil && expectedErr == nil && hmac.Equal(provided, expectedBytes)
}

// HAOriginDownload serves a short-lived, cluster-signed link generated by an
// authenticated administrator on another HA node. The attachment stays on and
// is downloaded from the original upload server; no diagnostics are replicated.
func (h *ProblemReportHandlers) HAOriginDownload(w http.ResponseWriter, r *http.Request) {
	report, err := h.store.GetProblemReport(r.Context(), r.PathValue("id"))
	if err != nil || h.isRemoteOrigin(report) {
		smError(w, http.StatusNotFound, "attachment not found")
		return
	}
	requestedName := r.PathValue("file")
	name := filepath.Base(requestedName)
	if name != requestedName || name == "." || name == "" {
		smError(w, http.StatusNotFound, "attachment not found")
		return
	}
	if name != "diagnostics.zip" && !containsProblemReportAttachment(report.ScreenshotPaths, name) {
		smError(w, http.StatusNotFound, "attachment not found")
		return
	}
	expiresAt, err := strconv.ParseInt(r.URL.Query().Get("expires_at"), 10, 64)
	if err != nil || !h.validOriginAttachmentSignature(report.ID, name, expiresAt, r.URL.Query().Get("signature")) {
		smError(w, http.StatusUnauthorized, "invalid or expired attachment link")
		return
	}
	h.serveProblemReportAttachment(w, r, report.ID, name)
}

// HAAttachmentManifest is only used for an authenticated peer's on-demand
// display. It deliberately exposes no file contents and is not HA-synced.
func (h *ProblemReportHandlers) HAAttachmentManifest(w http.ResponseWriter, r *http.Request) {
	report, err := h.store.GetProblemReport(r.Context(), r.PathValue("id"))
	if err != nil || h.isRemoteOrigin(report) {
		smError(w, http.StatusNotFound, "report not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string][]string{"items": append([]string{"diagnostics.zip"}, report.ScreenshotPaths...)})
}

func containsProblemReportAttachment(names []string, name string) bool {
	for _, item := range names {
		if name == item {
			return true
		}
	}
	return false
}

func (h *ProblemReportHandlers) serveProblemReportAttachment(w http.ResponseWriter, r *http.Request, reportID, name string) {
	root, safeRoot := problemReportStorageRoot(h.dataDir, reportID)
	if !safeRoot {
		smError(w, http.StatusNotFound, "attachment not found")
		return
	}
	path := filepath.Join(root, name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		smError(w, http.StatusNotFound, "attachment not found")
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(name))
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeFile(w, r, path)
}

func (h *ProblemReportHandlers) deleteFromOrigin(ctx context.Context, report *skillmarket.ProblemReport) error {
	originURL := normalizeProblemReportOriginURL(report.OriginURL)
	if originURL == "" {
		return fmt.Errorf("invalid origin server URL")
	}
	transportURL := originURL
	if h.haInternalURLForOrigin != nil {
		if internalURL := normalizeProblemReportOriginURL(h.haInternalURLForOrigin(originURL)); internalURL != "" {
			transportURL = internalURL
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, transportURL+"/api/v1/internal/ha/problem-reports/"+url.PathEscape(report.ID), nil)
	if err != nil {
		return err
	}
	if h.haSignPeerRequest != nil {
		if err := h.haSignPeerRequest(req); err != nil {
			return err
		}
	}
	if h.haClusterSecret != nil {
		if secret := strings.TrimSpace(h.haClusterSecret()); secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("origin returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	return nil
}

func (h *ProblemReportHandlers) attachmentManifestFromOrigin(ctx context.Context, report *skillmarket.ProblemReport) ([]string, error) {
	originURL := normalizeProblemReportOriginURL(report.OriginURL)
	if originURL == "" {
		return nil, fmt.Errorf("invalid origin server URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, originURL+"/api/v1/internal/ha/problem-reports/"+url.PathEscape(report.ID)+"/attachments", nil)
	if err != nil {
		return nil, err
	}
	if h.haSignPeerRequest != nil {
		if err := h.haSignPeerRequest(req); err != nil {
			return nil, err
		}
	}
	if h.haClusterSecret != nil {
		if secret := strings.TrimSpace(h.haClusterSecret()); secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return nil, fmt.Errorf("origin returned %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	var payload struct {
		Items []string `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&payload); err != nil {
		return nil, err
	}
	if len(payload.Items) == 0 || payload.Items[0] != "diagnostics.zip" || len(payload.Items) > 13 {
		return nil, fmt.Errorf("origin returned an invalid attachment manifest")
	}
	for _, name := range payload.Items {
		if filepath.Base(name) != name || name == "." || name == "" {
			return nil, fmt.Errorf("origin returned an invalid attachment manifest")
		}
	}
	return payload.Items, nil
}

// HADelete deletes the locally-owned report and attachments after peer
// authentication. It is never exposed through the normal administrator route.
func (h *ProblemReportHandlers) HADelete(w http.ResponseWriter, r *http.Request) {
	report, err := h.store.GetProblemReport(r.Context(), r.PathValue("id"))
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if h.isRemoteOrigin(report) {
		smError(w, http.StatusConflict, "report is not owned by this server")
		return
	}
	root, safeRoot := problemReportStorageRoot(h.dataDir, report.ID)
	if !safeRoot {
		smError(w, http.StatusNotFound, "report not found")
		return
	}
	staged, err := stageProblemReportStorage(root)
	if err != nil {
		smError(w, http.StatusInternalServerError, "prepare attachment cleanup: "+err.Error())
		return
	}
	if err := h.store.DeleteProblemReport(r.Context(), report.ID); err != nil {
		restoreStagedProblemReportStorage(staged, root)
		smError(w, http.StatusInternalServerError, "report deletion failed: "+err.Error())
		return
	}
	removeStagedProblemReportStorage(staged)
	w.WriteHeader(http.StatusNoContent)
}

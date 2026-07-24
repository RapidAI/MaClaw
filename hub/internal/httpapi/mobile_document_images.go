package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// mobileDocumentImageIDRe restricts image ids used in URLs / blob keys.
var mobileDocumentImageIDRe = regexp.MustCompile(`^img[0-9]{1,4}$`)

// Limits for embedded document images (DOCX media).
const (
	mobileDocumentMaxEmbeddedImages     = 16
	mobileDocumentMaxEmbeddedImageBytes = 4 << 20 // 4 MiB per image
)

// mobileDocumentDraftImage is a durable reference to an extracted illustration
// (e.g. from word/media). Bytes live on disk under SourcePath; never in state.json.
type mobileDocumentDraftImage struct {
	ID          string `json:"id"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	SourcePath  string `json:"source_path,omitempty"`
	SourceSize  int    `json:"source_size,omitempty"`
}

// mobileDocumentImageURL builds the authenticated relative download path for a draft image.
func mobileDocumentImageURL(draftID, imageID string) string {
	draftID = strings.TrimSpace(draftID)
	imageID = strings.TrimSpace(imageID)
	if draftID == "" || imageID == "" {
		return ""
	}
	return "/api/mobile/documents/drafts/" + draftID + "/images/" + imageID
}

// mobileDocumentImageMarkdown returns a markdown image line for preview clients.
func mobileDocumentImageMarkdown(alt, draftID, imageID string) string {
	url := mobileDocumentImageURL(draftID, imageID)
	if url == "" {
		return ""
	}
	alt = strings.TrimSpace(alt)
	if alt == "" {
		alt = imageID
	}
	// Escape alt brackets lightly.
	alt = strings.ReplaceAll(alt, "[", "(")
	alt = strings.ReplaceAll(alt, "]", ")")
	return "![" + alt + "](" + url + ")"
}

func mobileDraftDeleteImages(images []mobileDocumentDraftImage) {
	for _, img := range images {
		if p := strings.TrimSpace(img.SourcePath); p != "" {
			mobileDeleteDocumentBlob(p)
		}
	}
}

func mobileDraftImagesPayload(draftID string, images []mobileDocumentDraftImage) []map[string]any {
	if len(images) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(images))
	for _, img := range images {
		id := strings.TrimSpace(img.ID)
		if id == "" {
			continue
		}
		item := map[string]any{
			"id":       id,
			"filename": strings.TrimSpace(img.Filename),
			"size":     img.SourceSize,
			"url":      mobileDocumentImageURL(draftID, id),
		}
		if ct := strings.TrimSpace(img.ContentType); ct != "" {
			item["content_type"] = ct
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MobileDocumentDraftImageHandler streams one extracted illustration.
//
//	GET /api/mobile/documents/drafts/{draftId}/images/{imageId}
func MobileDocumentDraftImageHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		draftID := strings.TrimSpace(r.PathValue("draftId"))
		imageID := strings.TrimSpace(r.PathValue("imageId"))
		if draftID == "" || imageID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "draft id and image id are required")
			return
		}
		// Sanitize image id (path traversal + allow-list).
		imageID = filepath.Base(imageID)
		if !mobileDocumentImageIDRe.MatchString(imageID) {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "invalid image id")
			return
		}
		ownerID := mobilePrincipalOwnerID(principal)
		mobileDocuments.Lock()
		record, ok := mobileDocuments.drafts[draftID]
		var img *mobileDocumentDraftImage
		if ok && record.OwnerID == ownerID && mobileMeetingRecordingTenantMatches(principal.TenantID, record.TenantID) {
			for i := range record.Images {
				if strings.TrimSpace(record.Images[i].ID) == imageID {
					img = &record.Images[i]
					break
				}
			}
		}
		var blobPath string
		var contentType, filename string
		if img != nil {
			blobPath = img.SourcePath
			contentType = img.ContentType
			filename = img.Filename
		} else if ok && record.OwnerID == ownerID && mobileMeetingRecordingTenantMatches(principal.TenantID, record.TenantID) {
			// Fallback: conventional blob key used by extract (draftimg/{draftId}_{imageId}).
			blobPath = filepath.ToSlash(filepath.Join(filepath.Base(ownerID), "draftimg", filepath.Base(draftID)+"_"+imageID+".bin"))
			filename = imageID
		}
		mobileDocuments.Unlock()
		if !ok || record.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(principal.TenantID, record.TenantID) {
			writeError(w, http.StatusNotFound, "IMAGE_NOT_FOUND", "image not found")
			return
		}
		if filename == "" {
			filename = imageID
		}
		if contentType == "" || contentType == "application/octet-stream" {
			// Prefer extension; if missing/generic (fallback .bin keys), sniff magic.
			contentType = mobileContentTypeForImageFilename(filename)
			if contentType == "application/octet-stream" {
				if sniffed := mobileSniffImageContentTypeFromBlob(blobPath); sniffed != "" {
					contentType = sniffed
					if filepath.Ext(filename) == "" {
						filename = imageID + mobileExtForImageContentType(sniffed)
					}
				}
			}
		}
		// Inline so browsers / clients can display rather than force-download.
		if !mobileWriteOriginalHTTPDisp(w, contentType, filename, nil, blobPath, true) {
			writeError(w, http.StatusNotFound, "IMAGE_NOT_FOUND", "image blob missing")
		}
	}
}

// mobileSniffImageContentTypeFromBlob reads only a small header (not the full blob).
func mobileSniffImageContentTypeFromBlob(relPath string) string {
	f, _, err := mobileOpenDocumentBlob(relPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 16)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return ""
	}
	return mobileSniffImageContentType(buf[:n])
}

func mobileSniffImageContentType(raw []byte) string {
	if len(raw) >= 8 && bytes.Equal(raw[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "image/png"
	}
	if len(raw) >= 3 && raw[0] == 0xff && raw[1] == 0xd8 && raw[2] == 0xff {
		return "image/jpeg"
	}
	if len(raw) >= 6 && (bytes.Equal(raw[:6], []byte("GIF87a")) || bytes.Equal(raw[:6], []byte("GIF89a"))) {
		return "image/gif"
	}
	if len(raw) >= 12 && bytes.Equal(raw[:4], []byte("RIFF")) && bytes.Equal(raw[8:12], []byte("WEBP")) {
		return "image/webp"
	}
	if len(raw) >= 2 && raw[0] == 'B' && raw[1] == 'M' {
		return "image/bmp"
	}
	return ""
}

func mobileExtForImageContentType(ct string) string {
	switch strings.ToLower(strings.TrimSpace(ct)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	default:
		return ""
	}
}

// mobileDraftAppendImageMarkdown appends illustration links without wiping body text.
// If every image is already linked, the body is returned unchanged (no empty "## 附图").
func mobileDraftAppendImageMarkdown(body, draftID string, images []mobileDocumentDraftImage) string {
	body = strings.TrimSpace(body)
	if len(images) == 0 {
		if body == "" {
			return ""
		}
		return body + "\n"
	}
	var section strings.Builder
	added := 0
	for _, im := range images {
		id := strings.TrimSpace(im.ID)
		if id == "" {
			continue
		}
		// Skip duplicates already linked in the body.
		if strings.Contains(body, "/images/"+id) {
			continue
		}
		alt := strings.TrimSpace(im.Filename)
		if alt == "" {
			alt = id
		}
		section.WriteString(mobileDocumentImageMarkdown(alt, draftID, id))
		section.WriteString("\n\n")
		added++
	}
	if added == 0 {
		if body == "" {
			return ""
		}
		return body + "\n"
	}
	var b strings.Builder
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}
	if !strings.Contains(body, "## 附图") {
		b.WriteString("## 附图\n\n")
	}
	b.WriteString(section.String())
	return strings.TrimSpace(b.String()) + "\n"
}

func mobileContentTypeForImageFilename(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".tif", ".tiff":
		return "image/tiff"
	case ".svg":
		return "image/svg+xml"
	case ".emf", ".wmf":
		// Not browser-friendly; still label for completeness.
		return "application/octet-stream"
	default:
		return "application/octet-stream"
	}
}

func mobileIsBrowserPreviewableImage(filename, contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(ct, "image/png"),
		strings.HasPrefix(ct, "image/jpeg"),
		strings.HasPrefix(ct, "image/gif"),
		strings.HasPrefix(ct, "image/webp"),
		strings.HasPrefix(ct, "image/bmp"):
		return true
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return true
	default:
		return false
	}
}

// mobileDraftMarkdownFromDOCXEx extracts text + embedded images from a DOCX.
// When ownerID+draftID are set, images are persisted and markdown gets ![](url) links.
// Without IDs, images are skipped (text-only, for tests / dry extract).
func mobileDraftMarkdownFromDOCXEx(ownerID, draftID, filename string, raw []byte) (string, []mobileDocumentDraftImage, bool) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", nil, false
	}
	var documentXML []byte
	var relsXML []byte
	// Canonical media entries only (word/media/*). Alias lookup is separate.
	type mediaFile struct {
		zipPath string
		base    string
		data    []byte
	}
	var mediaList []mediaFile
	byAlias := make(map[string][]byte)
	mediaCount := 0
	for _, file := range zr.File {
		name := file.Name
		switch {
		case name == "word/document.xml":
			rc, err := file.Open()
			if err != nil {
				return "", nil, false
			}
			documentXML, err = io.ReadAll(io.LimitReader(rc, mobileDocumentUploadMaxBytes))
			_ = rc.Close()
			if err != nil {
				return "", nil, false
			}
		case name == "word/_rels/document.xml.rels":
			rc, err := file.Open()
			if err != nil {
				continue
			}
			relsXML, _ = io.ReadAll(io.LimitReader(rc, 2<<20))
			_ = rc.Close()
		case strings.HasPrefix(name, "word/media/"):
			if mediaCount >= mobileDocumentMaxEmbeddedImages*2 {
				// Soft cap on how many media parts we even load (includes non-previewable).
				continue
			}
			base := path.Base(name)
			if base == "" || base == "." || base == ".." {
				continue
			}
			if !mobileIsDOCXMediaImageName(base) {
				continue
			}
			rc, err := file.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(rc, mobileDocumentMaxEmbeddedImageBytes+1))
			_ = rc.Close()
			if err != nil || len(data) == 0 || len(data) > mobileDocumentMaxEmbeddedImageBytes {
				continue
			}
			mediaCount++
			mediaList = append(mediaList, mediaFile{zipPath: name, base: base, data: data})
			byAlias[name] = data
			byAlias["word/media/"+base] = data
			byAlias[base] = data
		}
	}
	if len(documentXML) == 0 {
		return "", nil, false
	}
	sort.SliceStable(mediaList, func(i, j int) bool {
		return mediaList[i].zipPath < mediaList[j].zipPath
	})

	ridToTarget := mobileDOCXParseRels(relsXML)
	blocks := mobileDOCXBlocksWithImages(documentXML)

	title := mobileUploadTitle(filename)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	var images []mobileDocumentDraftImage
	seenRels := make(map[string]bool)
	usedFiles := make(map[string]bool)
	canStore := strings.TrimSpace(ownerID) != "" && strings.TrimSpace(draftID) != ""

	storeImage := func(fname string, data []byte, alt string, linkInBody bool) {
		if !canStore || len(images) >= mobileDocumentMaxEmbeddedImages || len(data) == 0 {
			return
		}
		fname = path.Base(fname)
		if fname == "" || fname == "." {
			return
		}
		if usedFiles[fname] {
			return
		}
		if !mobileIsBrowserPreviewableImage(fname, mobileContentTypeForImageFilename(fname)) {
			if linkInBody {
				b.WriteString(fmt.Sprintf("_（附图 %s 格式暂不支持在线预览，请打开原件查看）_\n\n", fname))
			}
			usedFiles[fname] = true
			return
		}
		imageID := fmt.Sprintf("img%d", len(images)+1)
		blobID := draftID + "_" + imageID
		blobPath, size, _ := mobilePersistDocumentOriginal(ownerID, "draftimg", blobID, data)
		// Images must be on disk to be served later (no RAM cache for multi-MB art).
		if size == 0 || strings.TrimSpace(blobPath) == "" {
			return
		}
		ct := mobileContentTypeForImageFilename(fname)
		images = append(images, mobileDocumentDraftImage{
			ID:          imageID,
			Filename:    fname,
			ContentType: ct,
			SourcePath:  blobPath,
			SourceSize:  size,
		})
		usedFiles[fname] = true
		if linkInBody {
			if strings.TrimSpace(alt) == "" {
				alt = fmt.Sprintf("图%d", len(images))
			}
			b.WriteString(mobileDocumentImageMarkdown(alt, draftID, imageID))
			b.WriteString("\n\n")
		}
	}

	resolveMedia := func(rID string) (fname string, data []byte, ok bool) {
		rID = strings.TrimSpace(rID)
		if rID == "" {
			return "", nil, false
		}
		target := ridToTarget[rID]
		if target == "" {
			return "", nil, false
		}
		target = strings.TrimPrefix(strings.ReplaceAll(target, "\\", "/"), "/")
		candidates := []string{
			target,
			"word/" + strings.TrimPrefix(target, "/"),
			path.Clean("word/" + target),
			"word/media/" + path.Base(target),
			path.Base(target),
		}
		for _, c := range candidates {
			c = path.Clean(strings.ReplaceAll(c, "\\", "/"))
			if v, hit := byAlias[c]; hit {
				return path.Base(c), v, true
			}
		}
		return path.Base(target), nil, false
	}

	for _, block := range blocks {
		switch block.Kind {
		case "text":
			if t := strings.TrimSpace(block.Text); t != "" {
				b.WriteString(t)
				b.WriteString("\n\n")
			}
		case "image":
			if !canStore {
				continue
			}
			rID := strings.TrimSpace(block.RelID)
			if rID == "" || seenRels[rID] {
				continue
			}
			seenRels[rID] = true
			fname, data, ok := resolveMedia(rID)
			if !ok || len(data) == 0 {
				continue
			}
			storeImage(fname, data, fmt.Sprintf("图%d", len(images)+1), true)
		}
	}

	// Unreferenced media → appendix (deterministic order).
	if canStore {
		var appendixIDs []string
		for _, mf := range mediaList {
			if len(images) >= mobileDocumentMaxEmbeddedImages {
				break
			}
			if usedFiles[mf.base] || !mobileIsBrowserPreviewableImage(mf.base, "") {
				continue
			}
			before := len(images)
			storeImage(mf.base, mf.data, mf.base, false)
			if len(images) > before {
				appendixIDs = append(appendixIDs, images[len(images)-1].ID)
			}
		}
		if len(appendixIDs) > 0 {
			b.WriteString("## 附图\n\n")
			for _, id := range appendixIDs {
				var fname string
				for _, im := range images {
					if im.ID == id {
						fname = im.Filename
						break
					}
				}
				b.WriteString(mobileDocumentImageMarkdown(fname, draftID, id))
				b.WriteString("\n\n")
			}
		}
	}

	md := strings.TrimSpace(b.String()) + "\n"
	bodyOnly := strings.TrimSpace(strings.TrimPrefix(md, "# "+title))
	if bodyOnly == "" && len(images) == 0 {
		return "", nil, false
	}
	return md, images, true
}

func mobileIsDOCXMediaImageName(base string) bool {
	switch strings.ToLower(filepath.Ext(base)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".emf", ".wmf", ".svg":
		return true
	default:
		return false
	}
}

func mobileDOCXParseRels(raw []byte) map[string]string {
	out := make(map[string]string)
	if len(raw) == 0 {
		return out
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		var id, target, typ string
		for _, a := range start.Attr {
			switch a.Name.Local {
			case "Id":
				id = a.Value
			case "Target":
				target = a.Value
			case "Type":
				typ = a.Value
			}
		}
		if id == "" || target == "" {
			continue
		}
		// Prefer image relationships; still accept others pointing at media/.
		if strings.Contains(typ, "image") || strings.Contains(target, "media/") {
			out[id] = target
		}
	}
	return out
}

type mobileDOCXBlock struct {
	Kind  string // "text" | "image"
	Text  string
	RelID string
}

// mobileDOCXBlocksWithImages walks document.xml in order, emitting text paragraphs
// and image blip relationship ids where drawings appear.
func mobileDOCXBlocksWithImages(raw []byte) []mobileDOCXBlock {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var blocks []mobileDOCXBlock
	var current strings.Builder
	inParagraph := false
	flushText := func() {
		if t := strings.TrimSpace(current.String()); t != "" {
			blocks = append(blocks, mobileDOCXBlock{Kind: "text", Text: t})
		}
		current.Reset()
	}
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "p":
				inParagraph = true
				current.Reset()
			case "t":
				if inParagraph {
					var text string
					if err := decoder.DecodeElement(&text, &item); err == nil {
						current.WriteString(text)
					}
				}
			case "tab":
				if inParagraph {
					current.WriteString("\t")
				}
			case "br":
				if inParagraph {
					current.WriteString("\n")
				}
			case "blip":
				// Drawing may sit inside or between paragraphs.
				var embed string
				for _, a := range item.Attr {
					if a.Name.Local == "embed" {
						embed = a.Value
						break
					}
				}
				if embed != "" {
					// Flush pending paragraph text so image order matches the doc.
					flushText()
					blocks = append(blocks, mobileDOCXBlock{Kind: "image", RelID: embed})
				}
			}
		case xml.EndElement:
			if item.Name.Local == "p" && inParagraph {
				flushText()
				inParagraph = false
			}
		}
	}
	return blocks
}

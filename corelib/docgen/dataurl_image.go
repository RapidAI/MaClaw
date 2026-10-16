package docgen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"

	// Formats the desktop companion can produce and GoPDF cannot embed from
	// source bytes. Importing for side effects registers their decoders, which
	// is what lets re-encoding to PNG succeed.
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Images reach the renderer as Markdown targets, and not all of them are files.
// The desktop companion hands its figures over as data URLs, because that is
// what an <img> wants and what it already keeps for display. A filesystem-only
// renderer would drop those silently — the picture leaves no trace at all,
// which reads as "the export lost my figure" rather than a format error.
//
// These helpers decode a data URL into a real file the renderer can measure
// and embed, reusing the same temporary directory for the whole process so
// one figure does not become one stray file per pagination retry.

const dataURLPrefix = "data:"

// dataURLMediaType maps a declared media type to the file extension its bytes
// are stored under. The set is deliberately the formats GoPDF can place in a
// PDF without re-encoding: anything else is written as a PNG instead.
//
// "image/pjpeg" and "image/jpg" are spellings real browsers have emitted for
// JPEG; asking the decoder settles the rest, so nothing here has to be
// guesswork about a vendor's spelling.
var dataURLMediaType = map[string]string{
	"image/png":   ".png",
	"image/jpeg":  ".jpg",
	"image/jpg":   ".jpg",
	"image/pjpeg": ".jpg",
	"image/gif":   ".gif",
}

// dataURLFormatsToReencode lists media types whose bytes GoPDF cannot embed
// directly. It re-encodes them to PNG rather than dropping them: the desktop
// companion can hand over WebP or BMP figures, and those arriving as "图片未找到"
// reads as the export having lost the picture.
//
// Registering the decoder is what makes this work, so the imports above matter
// as much as this table.
var dataURLFormatsToReencode = map[string]bool{
	"image/webp":     true,
	"image/bmp":      true,
	"image/x-ms-bmp": true,
}

// dataURLScratch is the per-render home for decoded data-URL images.
//
// One document's images live together and are deleted when that render ends.
// A process-wide cache would be simpler to write and wrong to ship: nothing
// would ever remove the files, so every export leaves its figures behind in
// the system temporary directory for as long as the machine is up.
//
// The lifetime is the render, not the file: Markdown holds these paths only
// while its own document is being laid out, and nothing outside one Generate
// call may open them.
type dataURLScratch struct {
	once sync.Once
	dir  string
	err  error
	mu   sync.Mutex
	seen map[string]string
}

func newDataURLScratch() *dataURLScratch {
	return &dataURLScratch{seen: make(map[string]string)}
}

// scratchContextKey carries the per-render scratch through the Markdown layer.
// The renderer is reached through plain string helpers that predate this file,
// and threading another parameter through all of them would touch every call
// site; the context keeps the plumbing to the two entry points that own a
// render's lifetime.
type scratchContextKey struct{}

func withScratch(ctx context.Context, s *dataURLScratch) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, scratchContextKey{}, s)
}

// scratchFrom returns the render's scratch, or nil when called outside one.
// A nil scratch means there is nowhere to put decoded images, which callers
// treat exactly like an unusable path: an honest "图片未找到" rather than a
// panic.
func scratchFrom(ctx context.Context) *dataURLScratch {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(scratchContextKey{}).(*dataURLScratch)
	return s
}

// isDataURLImage reports whether raw is a data: image URL this package can decode.
func isDataURLImage(raw string) bool {
	return strings.HasPrefix(strings.TrimSpace(raw), dataURLPrefix)
}

// dirFor returns the scratch directory, creating it on first use.
func (s *dataURLScratch) dirFor() (string, error) {
	s.once.Do(func() {
		s.dir, s.err = os.MkdirTemp("", "docgen-images-")
	})
	return s.dir, s.err
}

// cleanup removes the directory and everything written into it. Safe to call
// on a scratch that never had anything decoded.
func (s *dataURLScratch) cleanup() {
	dir, err := s.dirFor()
	if err != nil || dir == "" {
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("[DocGen] 清理临时图片目录失败 %s: %v", dir, err)
	}
}

// resolveDataURLImage writes one data-URL image into the scratch directory and
// returns its path.
//
// Successful decoding is required before anything is written: a base64 blob
// that is not a picture would otherwise reach GoPDF as a file the embed step
// cannot use. Results are memoized by content hash because pagination
// re-renders the same Markdown several times over, and each pass would
// otherwise decode the same bytes again.
func (s *dataURLScratch) resolve(raw string) (string, bool) {
	payload := strings.TrimSpace(raw)
	ext, data, ok := splitDataURL(payload)
	if !ok {
		return "", false
	}
	sum := sha256.Sum256(data)
	key := hex.EncodeToString(sum[:]) + ext

	s.mu.Lock()
	cached, found := s.seen[key]
	s.mu.Unlock()
	if found {
		if info, err := os.Stat(cached); err == nil && !info.IsDir() {
			return cached, true
		}
	}

	dir, err := s.dirFor()
	if err != nil || dir == "" {
		return "", false
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return "", false
	}
	path := filepath.Join(dir, key)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", false
	}

	s.mu.Lock()
	s.seen[key] = path
	s.mu.Unlock()
	return path, true
}

// splitDataURL separates "data:image/png;base64,AAAA" into its extension and
// bytes. URL-encoded (non-base64) data URLs are rejected rather than guessed
// at: they are not how images are carried here, and percent-decoding them into
// an assumed image would turn text into a broken file.
func splitDataURL(payload string) (ext string, data []byte, ok bool) {
	if !strings.HasPrefix(payload, dataURLPrefix) {
		return "", nil, false
	}
	body, hasBody := dataURLBodyOf(payload)
	if !hasBody || body == "" {
		return "", nil, false
	}
	// metadata. Reading both from one stripped string drops whichever that
	// string omits.
	mediaType := dataURLMediaTypeOf(payload)
	ext, embeddedDirectly := dataURLMediaType[mediaType]
	if !embeddedDirectly {
		// Not a format that embeds directly. Re-encoding to PNG keeps formats
		// the companion legitimately produces instead of reporting them lost.
		if !dataURLFormatsToReencode[mediaType] {
			return "", nil, false
		}
		ext = ".png"
	}
	encoded := false
	for _, field := range strings.Split(dataURLMetaOf(payload), ";")[1:] {
		if strings.EqualFold(strings.TrimSpace(field), "base64") {
			encoded = true
			break
		}
	}
	if !encoded {
		return "", nil, false
	}
	// Cheap check before the expensive one: base64 carries 4 bytes of text for
	// every 3 of image, so refusing here avoids ever materialising them. This
	// has to sit before DecodeString, which was itself the allocation this
	// bound is meant to prevent.
	if approx := len(body) * 3 / 4; approx > maxReencodeSourceBytes {
		log.Printf("[DocGen] 跳过过大的图片 (%s): 约 %d 字节", mediaType, approx)
		return "", nil, false
	}
	decoded, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return "", nil, false
	}
	if len(decoded) == 0 {
		return "", nil, false
	}
	if !embeddedDirectly {
		// Check the dimensions before decoding: DecodeConfig reads the header
		// only, so an oversized picture costs almost nothing to refuse.
		if _, err := dataURLNeedsReencode(decoded); err != nil {
			log.Printf("[DocGen] 跳过无法处理的图片 (%s): %v", mediaType, err)
			return "", nil, false
		}
		// Re-encode here, after decoding rather than before: asking GoPDF to
		// read a WebP would fail, so the bytes are handed to it already PNG.
		pngBytes, err := decodeToPNG(decoded)
		if err != nil {
			log.Printf("[DocGen] 图片 re-encode 失败 (%s): %v", mediaType, err)
			return "", nil, false
		}
		return ext, pngBytes, true
	}
	return ext, decoded, true
}

// maxReencodePixels bounds how large an image may be before it is re-encoded.
//
// Re-encoding has to decode the whole picture into memory first, whereas the
// formats that embed directly never do. Uncompressed sources make the gap
// enormous: a 24MB BMP (which the companion permits) becomes ~48MB of RGBA,
// and on tall plots far worse. Anything past this is left out rather than
// risking a hundreds-of-megabytes spike inside a desktop app.
//
// 40 megapixels is well past anything meant for an A4 page, which is about
// 8.7 megapixels at 300dpi.
const maxReencodePixels = 40_000_000

// maxReencodeSourceBytes caps the encoded size of an image being converted.
//
// Base64 costs nothing to measure but everything to materialise: decoding it
// allocates the whole picture. Checking first means a pathologically large
// figure is turned away without the process ever holding its bytes. The bound
// is generous enough for anything the companion accepts (24MB) with room to
// spare, and applies only to the formats that must be re-encoded.
const maxReencodeSourceBytes = 48 << 20

// dataURLNeedsReencode reports whether these bytes must be converted to PNG
// before embedding, checking the pixel bound first so an oversized image is
// refused without being decoded.
func dataURLNeedsReencode(data []byte) (bool, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return false, err
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxReencodePixels {
		return false, fmt.Errorf("图片过大（%dx%d），无法转换后导出", cfg.Width, cfg.Height)
	}
	return true, nil
}

// decodeToPNG converts image bytes into PNG. GoPDF embeds JPEG directly and
// re-encodes anything else itself, but it asks the standard image decoders
// first, so a format they do not know has to arrive already converted.
//
// The size bound lives in the caller, which can read dimensions without
// paying for a full decode.
func decodeToPNG(data []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// dataURLMetaOf returns a data URL's metadata — everything before its first
// comma, parameters included: "data:image/png;base64,AAAA" -> "png;base64".
func dataURLMetaOf(payload string) string {
	rest := strings.TrimPrefix(strings.TrimSpace(payload), dataURLPrefix)
	if comma := strings.Index(rest, ","); comma >= 0 {
		rest = rest[:comma]
	}
	return strings.TrimSpace(rest)
}

// dataURLMediaTypeOf reads just the media type, stopping at the first
// parameter: "data:image/png;base64,AAAA" -> "image/png".
//
// Taking the whole metadata would keep ";base64" attached and match nothing,
// so the reported extension silently becomes "".
func dataURLMediaTypeOf(payload string) string {
	meta := dataURLMetaOf(payload)
	if semicolon := strings.Index(meta, ";"); semicolon >= 0 {
		meta = meta[:semicolon]
	}
	return strings.ToLower(strings.TrimSpace(meta))
}

// dataURLBodyOf returns everything after the comma, or false when there is
// none.
func dataURLBodyOf(payload string) (string, bool) {
	rest := strings.TrimPrefix(strings.TrimSpace(payload), dataURLPrefix)
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return "", false
	}
	return strings.TrimSpace(rest[comma+1:]), true
}

// dataURLExtension reports the file extension a data URL would get, which lets
// callers name the same image consistently before decoding it.
func dataURLExtension(payload string) string {
	return dataURLMediaType[dataURLMediaTypeOf(payload)]
}

// dataURLPlaceholder shortens a data URL for messages that must identify an
// image without carrying its bytes. It names the media type rather than the
// extension so the text still reads as a picture, not as ".png".
func dataURLPlaceholder(raw string) string {
	return fmt.Sprintf("data:%s;base64,<%d 字节>", dataURLMediaTypeOf(raw), len(strings.TrimSpace(raw)))
}

// dataURLViewForValidation collapses every data-URL image to its placeholder.
//
// Content limits exist to bound layout cost and to catch a pasted file. A
// data-URL image is neither: it contributes no wrapping, and its bytes are a
// picture rather than an accident. Counting them would reject a perfectly
// ordinary export for being one chart too long, and would trip the "suspicious
// base64 run" guard on the first figure — the very images this package now
// supports.
func dataURLViewForValidation(content string) string {
	if !strings.Contains(content, dataURLPrefix) {
		return content
	}
	var out strings.Builder
	out.Grow(len(content))
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		out.WriteString(collapseDataURLTarget(line))
		out.WriteString("\n")
	}
	return out.String()
}

// dataURLTargetRe finds an image target inside a line of text, wherever it
// sits. The renderer's own markdownImageRe anchors on the whole line, so a
// figure written mid-sentence ("看图 ![](…​) 结束") is invisible to it — and
// such a figure then trips the base64 guard on nothing but its size.
// The angle-bracket form is how Markdown spells a target that would otherwise
// be ambiguous, so it is accepted as well.
var dataURLTargetRe = regexp.MustCompile(`!\[[^\]]*\]\(\s*<?(data:[^)\s>]+)>?\s*\)`)

// htmlImgDataURLRe finds the src of an inline <img>. The renderer turns some
// Markdown into HTML before it is validated, and those tags must be shortened
// for exactly the same reason Markdown images are.
var htmlImgDataURLRe = regexp.MustCompile(`(?i)<img\b[^>]*\bsrc\s*=\s*"?(data:[^"\s>]+)`)

// collapseDataURLTarget rewrites the target of every data-URL image on one
// line, wherever it appears: alone, inside a sentence, in a list item, or in
// an <img> tag written by hand.
func collapseDataURLTarget(line string) string {
	out := line
	out = dataURLTargetRe.ReplaceAllStringFunc(out, func(match string) string {
		target := dataURLTargetRe.FindStringSubmatch(match)[1]
		return strings.Replace(match, target, dataURLPlaceholder(target), 1)
	})
	out = htmlImgDataURLRe.ReplaceAllStringFunc(out, func(match string) string {
		target := htmlImgDataURLRe.FindStringSubmatch(match)[1]
		return strings.Replace(match, target, dataURLPlaceholder(target), 1)
	})
	return out
}

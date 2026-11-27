package guiapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	runtime "github.com/RapidAI/CodeClaw/guiapp/wruntime"
)

// chatMediaMaxImageBytes bounds a picture placed on the clipboard or written
// from a data URL / remote fetch. Local "save as" of an existing file streams
// the original bytes and does not use this cap.
var chatMediaMaxImageBytes int64 = 12 * 1024 * 1024

const chatMediaMaxImagePixels int64 = 32_000_000

// chatMediaClipboard is what other programs need in order to paste.
// An image carries a bottom-up DIB (Paint, Word) and a PNG payload, and not a
// file drop: a file drop makes those programs paste a file instead of the picture.
// A non-image attachment carries only an HDROP listing so Explorer pastes the file.
type chatMediaClipboard struct {
	dib   []byte
	png   []byte
	hdrop []byte
}

// writeChatMediaClipboard places the payload on the system clipboard.
// Tests replace it; the platform file supplies the real writer.
var writeChatMediaClipboard = writeChatMediaClipboardPlatform

// chatMediaLookupIP and chatMediaProxyFromEnvironment are the network seams
// for a remote picture. Tests replace them.
var chatMediaLookupIP = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

var chatMediaProxyFromEnvironment = http.ProxyFromEnvironment

// chatMediaProxyDialKey marks the dial that reaches the configured proxy.
// That dial is not the picture host, so the address check must not reject it.
type chatMediaProxyDialKey struct{}

// chatMediaSaveDialog opens the native save browser. Tests replace it.
var chatMediaSaveDialog = func(a *App, title, defaultName string, filters []runtime.FileFilter) (string, error) {
	if a == nil || a.ctx == nil {
		return "", fmt.Errorf("save dialog is unavailable")
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:                title,
		DefaultFilename:      defaultName,
		Filters:              filters,
		CanCreateDirectories: true,
	})
}

// CopyChatMediaToClipboard puts a chat image or attachment on the system
// clipboard. kind is "image" or "file". source is a local absolute path, a
// data:image URL, or an http(s) image URL. Images are pasted as pictures;
// files are pasted as files.
func (a *App) CopyChatMediaToClipboard(kind, source string) error {
	payload, err := buildChatMediaClipboard(a, kind, source)
	if err != nil {
		return err
	}
	return writeChatMediaClipboard(payload)
}

// SaveChatMediaAs asks for a destination and writes the image or attachment
// there. An empty path means the user cancelled.
func (a *App) SaveChatMediaAs(kind, source, defaultName, title string) (string, error) {
	return saveChatMediaAs(a, kind, source, defaultName, title)
}

func buildChatMediaClipboard(a *App, kind, source string) (chatMediaClipboard, error) {
	kind = strings.TrimSpace(kind)
	source = strings.TrimSpace(source)
	if source == "" {
		return chatMediaClipboard{}, fmt.Errorf("nothing to copy")
	}
	if kind == "file" {
		path, err := cleanLocalMediaPath(source)
		if err != nil {
			return chatMediaClipboard{}, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return chatMediaClipboard{}, err
		}
		if info.IsDir() {
			return chatMediaClipboard{}, fmt.Errorf("cannot copy a folder")
		}
		return chatMediaClipboard{hdrop: hdropBytes([]string{path})}, nil
	}
	if kind != "image" {
		return chatMediaClipboard{}, fmt.Errorf("unsupported attachment kind")
	}
	if isDataImageURL(source) || isRemoteImageURL(source) {
		data, format, err := loadLooseChatImage(a, source)
		if err != nil {
			return chatMediaClipboard{}, err
		}
		return clipboardFromImageBytes(data, format)
	}
	path, err := cleanLocalMediaPath(source)
	if err != nil {
		return chatMediaClipboard{}, err
	}
	file, format, err := openAIAssistantAttachmentImage(path)
	if err != nil {
		return chatMediaClipboard{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, chatMediaMaxImageBytes+1))
	if err != nil {
		return chatMediaClipboard{}, err
	}
	if int64(len(data)) > chatMediaMaxImageBytes {
		return chatMediaClipboard{}, fmt.Errorf("image is larger than %d MB", chatMediaMaxImageBytes/1024/1024)
	}
	return clipboardFromImageBytes(data, format)
}

func clipboardFromImageBytes(data []byte, format string) (chatMediaClipboard, error) {
	img, decodedFormat, err := decodeChatImageBytes(data)
	if err != nil {
		return chatMediaClipboard{}, err
	}
	if format == "" {
		format = decodedFormat
	}
	payload := chatMediaClipboard{dib: dibFromImage(img)}
	if len(payload.dib) == 0 {
		return chatMediaClipboard{}, fmt.Errorf("image is empty")
	}
	if format == "png" {
		payload.png = data
	} else {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return chatMediaClipboard{}, err
		}
		payload.png = buf.Bytes()
	}
	return payload, nil
}

func saveChatMediaAs(a *App, kind, source, defaultName, title string) (string, error) {
	kind = strings.TrimSpace(kind)
	source = strings.TrimSpace(source)
	if source == "" {
		return "", fmt.Errorf("nothing to save")
	}
	if isDataImageURL(source) || isRemoteImageURL(source) {
		if kind == "file" {
			return "", fmt.Errorf("file path is required")
		}
		return saveLooseChatImage(a, source, defaultName, title)
	}
	path, err := cleanLocalMediaPath(source)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("cannot save a folder")
	}
	name := chatMediaFileName(defaultName, filepath.Base(path))
	if filepath.Ext(name) == "" {
		name = withExt(name, filepath.Ext(path))
	}
	dest, err := promptChatMediaSave(a, title, name, filepath.Ext(name))
	if err != nil || dest == "" {
		return "", err
	}
	if sameFilePath(dest, path) {
		return dest, nil
	}
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	if err := writeReaderAtomic(dest, in); err != nil {
		return "", err
	}
	return dest, nil
}

func saveLooseChatImage(a *App, source, defaultName, title string) (string, error) {
	// A remote picture with a known extension can ask for a path first.
	// Cancelling the dialog then does not download anything.
	if ext := remoteImageFileExt(source); ext != "" {
		name := withExt(chatMediaFileName(defaultName, "image"+ext), ext)
		dest, err := promptChatMediaSave(a, title, name, ext)
		if err != nil || dest == "" {
			return "", err
		}
		data, _, err := loadLooseChatImage(a, source)
		if err != nil {
			return "", err
		}
		if err := writeBytesAtomic(dest, data); err != nil {
			return "", err
		}
		return dest, nil
	}
	data, format, err := loadLooseChatImage(a, source)
	if err != nil {
		return "", err
	}
	ext := extForImageFormat(format)
	name := withExt(chatMediaFileName(defaultName, "image"+ext), ext)
	dest, err := promptChatMediaSave(a, title, name, ext)
	if err != nil || dest == "" {
		return "", err
	}
	if err := writeBytesAtomic(dest, data); err != nil {
		return "", err
	}
	return dest, nil
}

func remoteImageFileExt(raw string) string {
	if !isRemoteImageURL(raw) {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	switch strings.ToLower(filepath.Ext(u.Path)) {
	case ".jpg", ".jpeg":
		return ".jpg"
	case ".png", ".gif", ".webp", ".bmp":
		return strings.ToLower(filepath.Ext(u.Path))
	default:
		return ""
	}
}

func promptChatMediaSave(a *App, title, name, ext string) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Save"
	}
	var filters []runtime.FileFilter
	if ext != "" {
		label := strings.TrimPrefix(strings.ToUpper(ext), ".")
		filters = append(filters, runtime.FileFilter{DisplayName: label, Pattern: "*" + ext})
	}
	filters = append(filters, runtime.FileFilter{DisplayName: "All files", Pattern: "*.*"})
	dest, err := chatMediaSaveDialog(a, title, name, filters)
	if saveDialogCancelled(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(dest), nil
}

func saveDialogCancelled(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "cancel")
}

func loadLooseChatImage(a *App, source string) ([]byte, string, error) {
	if isDataImageURL(source) {
		data, err := decodeDataImageURL(source)
		if err != nil {
			return nil, "", err
		}
		format, err := inspectChatImageBytes(data)
		if err != nil {
			return nil, "", err
		}
		return data, format, nil
	}
	data, err := fetchRemoteChatImage(mediaContext(a), source)
	if err != nil {
		return nil, "", err
	}
	format, err := inspectChatImageBytes(data)
	if err != nil {
		return nil, "", err
	}
	return data, format, nil
}

func mediaContext(a *App) context.Context {
	if a != nil && a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func inspectChatImageBytes(data []byte) (string, error) {
	if int64(len(data)) > chatMediaMaxImageBytes {
		return "", fmt.Errorf("image is larger than %d MB", chatMediaMaxImageBytes/1024/1024)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("inspect image: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > chatMediaMaxImagePixels {
		return "", fmt.Errorf("image dimensions are not supported")
	}
	return format, nil
}

func decodeChatImageBytes(data []byte) (image.Image, string, error) {
	format, err := inspectChatImageBytes(data)
	if err != nil {
		return nil, "", err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("decode image: %w", err)
	}
	return img, format, nil
}

func isDataImageURL(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "data:image/")
}

func isRemoteImageURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func decodeDataImageURL(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	comma := strings.IndexByte(raw, ',')
	if comma < 0 || !isDataImageURL(raw) {
		return nil, fmt.Errorf("unsupported image data")
	}
	meta := strings.ToLower(raw[:comma])
	if !strings.Contains(meta, ";base64") {
		return nil, fmt.Errorf("unsupported image data")
	}
	payload := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, raw[comma+1:])
	if int64(len(payload)) > chatMediaMaxImageBytes*2 {
		return nil, fmt.Errorf("image is larger than %d MB", chatMediaMaxImageBytes/1024/1024)
	}
	// Markdown images are shown by the webview, which accepts unpadded base64.
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(payload)
	}
	if err != nil {
		return nil, fmt.Errorf("unsupported image data")
	}
	return data, nil
}

func fetchRemoteChatImage(ctx context.Context, raw string) ([]byte, error) {
	if !isRemoteImageURL(raw) {
		return nil, fmt.Errorf("unsupported image address")
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	originalHost := parsed.Hostname()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		// The picture host is checked in the proxy callback. DialContext also
		// carries the proxy connection, which is a local address on purpose.
		Proxy: chatMediaImageProxy(originalHost),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialChatMediaImage(ctx, dialer, originalHost, network, addr)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL == nil || req.URL.User != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
				return fmt.Errorf("unsupported image address")
			}
			return nil
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image download failed: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, chatMediaMaxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > chatMediaMaxImageBytes {
		return nil, fmt.Errorf("image is larger than %d MB", chatMediaMaxImageBytes/1024/1024)
	}
	return data, nil
}

func chatMediaImageProxy(originalHost string) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if req == nil || req.URL == nil {
			return nil, fmt.Errorf("unsupported image address")
		}
		// Check the picture host here. A proxy would otherwise dial a local
		// address and hide the real target from the dial filter.
		if err := assertChatMediaHostAllowed(req.Context(), originalHost, req.URL.Hostname()); err != nil {
			return nil, err
		}
		proxyURL, err := chatMediaProxyFromEnvironment(req)
		if err != nil || proxyURL == nil {
			return proxyURL, err
		}
		if addr := chatMediaProxyDialAddr(proxyURL); addr != "" {
			*req = *req.WithContext(context.WithValue(req.Context(), chatMediaProxyDialKey{}, addr))
		}
		return proxyURL, nil
	}
}

func chatMediaProxyDialAddr(u *url.URL) string {
	if u == nil {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "https":
			port = "443"
		case "socks5", "socks5h":
			port = "1080"
		default:
			port = "80"
		}
	}
	return net.JoinHostPort(host, port)
}

func assertChatMediaHostAllowed(ctx context.Context, originalHost, host string) error {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if host == "" {
		return fmt.Errorf("unsupported image address")
	}
	ips, err := chatMediaLookupIP(ctx, host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if chatMediaIPAllowed(originalHost, ip.IP) {
			return nil
		}
	}
	return fmt.Errorf("unsupported image address")
}

func dialChatMediaImage(ctx context.Context, dialer *net.Dialer, originalHost, network, addr string) (net.Conn, error) {
	if proxyAddr, _ := ctx.Value(chatMediaProxyDialKey{}).(string); proxyAddr != "" && strings.EqualFold(proxyAddr, addr) {
		return dialer.DialContext(ctx, network, addr)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := chatMediaLookupIP(ctx, host)
	if err != nil {
		return nil, err
	}
	var dialErr error
	allowed := false
	for _, ip := range ips {
		if !chatMediaIPAllowed(originalHost, ip.IP) {
			continue
		}
		allowed = true
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		dialErr = err
	}
	if !allowed {
		return nil, fmt.Errorf("unsupported image address")
	}
	if dialErr != nil {
		return nil, dialErr
	}
	return nil, fmt.Errorf("unsupported image address")
}

func chatMediaIPAllowed(originalHost string, ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	// Link-local is the cloud metadata range, not a picture source.
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if !ip.IsLoopback() && !ip.IsPrivate() {
		return true
	}
	orig := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(originalHost)), ".")
	if ip.IsLoopback() && orig == "localhost" {
		return true
	}
	parsed := net.ParseIP(orig)
	if parsed == nil {
		return false
	}
	if v4 := parsed.To4(); v4 != nil {
		parsed = v4
	}
	return parsed.Equal(ip)
}

func cleanLocalMediaPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" || isDataImageURL(path) || isRemoteImageURL(path) {
		return "", fmt.Errorf("file path is required")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("file path must be absolute")
	}
	return filepath.Clean(path), nil
}

func chatMediaFileName(name, fallback string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '|', '?', '*':
			return '_'
		}
		if r < 32 {
			return -1
		}
		return r
	}, name)
	name = strings.Trim(name, ". ")
	if name == "" || reservedWindowsFileName(name) {
		name = fallback
	}
	if len(name) > 180 {
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		for len(stem)+len(ext) > 180 && stem != "" {
			_, size := utf8.DecodeLastRuneInString(stem)
			if size <= 0 {
				break
			}
			stem = stem[:len(stem)-size]
		}
		name = strings.Trim(stem, ". ") + ext
	}
	if name == "" {
		return fallback
	}
	return name
}

func reservedWindowsFileName(name string) bool {
	base := name
	if ext := filepath.Ext(name); ext != "" {
		base = name[:len(name)-len(ext)]
	}
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

func extForImageFormat(format string) string {
	switch format {
	case "jpeg":
		return ".jpg"
	case "png", "gif", "webp", "bmp":
		return "." + format
	default:
		return ".png"
	}
}

func withExt(name, ext string) string {
	if ext == "" {
		return name
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.Trim(base, ". ")
	if base == "" {
		base = "image"
	}
	return base + ext
}

func sameFilePath(a, b string) bool {
	aa, err1 := filepath.Abs(filepath.Clean(a))
	bb, err2 := filepath.Abs(filepath.Clean(b))
	if err1 != nil || err2 != nil {
		return false
	}
	if goruntime.GOOS == "windows" {
		return strings.EqualFold(aa, bb)
	}
	return aa == bb
}

func writeBytesAtomic(dest string, data []byte) error {
	return writeReaderAtomic(dest, bytes.NewReader(data))
}

func writeReaderAtomic(dest string, r io.Reader) error {
	tmp := dest + ".maclawpart"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r)
	if copyErr == nil {
		copyErr = f.Sync()
	}
	closeErr := f.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	// Replace in one step. A failed replace must leave the previous file
	// where it is; deleting it first and then failing the rename loses it.
	if err := renameReplacing(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// dibFromImage builds a 32-bit bottom-up CF_DIB. The first pixel row in the
// buffer is the bottom of the image, which is what Windows paste expects.
// PNG pixels are straight alpha; premultiplying them makes Word paste a darker edge.
func dibFromImage(img image.Image) []byte {
	if img == nil {
		return nil
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	stride := w * 4
	buf := make([]byte, 40+stride*h)
	binary.LittleEndian.PutUint32(buf[0:], 40)
	binary.LittleEndian.PutUint32(buf[4:], uint32(w))
	binary.LittleEndian.PutUint32(buf[8:], uint32(h))
	binary.LittleEndian.PutUint16(buf[12:], 1)
	binary.LittleEndian.PutUint16(buf[14:], 32)
	binary.LittleEndian.PutUint32(buf[20:], uint32(stride*h))
	pix := buf[40:]
	switch src := img.(type) {
	case *image.NRGBA:
		if dibCopyChannels(pix, src.Pix, src.PixOffset(b.Min.X, b.Min.Y), src.Stride, w, h) {
			return buf
		}
	case *image.RGBA:
		if dibCopyChannels(pix, src.Pix, src.PixOffset(b.Min.X, b.Min.Y), src.Stride, w, h) {
			return buf
		}
	default:
		// JPEG and the other decoders land here. draw has tight paths for
		// them; walking At() per pixel is much slower on a multi-megapixel photo.
		drawn := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.Draw(drawn, drawn.Bounds(), img, b.Min, draw.Src)
		if dibCopyChannels(pix, drawn.Pix, 0, drawn.Stride, w, h) {
			return buf
		}
	}
	dibCopyAt(pix, img, b, w, h, stride)
	return buf
}

func dibCopyChannels(dst, src []byte, offset, srcStride, w, h int) bool {
	rowBytes := w * 4
	if w <= 0 || h <= 0 || srcStride < rowBytes || offset < 0 || len(dst) < rowBytes*h {
		return false
	}
	last := offset + (h-1)*srcStride + rowBytes
	if last < 0 || last > len(src) {
		return false
	}
	for y := 0; y < h; y++ {
		srow := src[offset+y*srcStride : offset+y*srcStride+rowBytes]
		drow := dst[(h-1-y)*rowBytes : (h-y)*rowBytes]
		for x := 0; x < rowBytes; x += 4 {
			drow[x] = srow[x+2]
			drow[x+1] = srow[x+1]
			drow[x+2] = srow[x]
			drow[x+3] = srow[x+3]
		}
	}
	return true
}

func dibCopyAt(pix []byte, img image.Image, bounds image.Rectangle, w, h, stride int) {
	for y := 0; y < h; y++ {
		row := pix[(h-1-y)*stride:]
		for x := 0; x < w; x++ {
			r, g, blue, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			i := x * 4
			row[i] = byte(blue >> 8)
			row[i+1] = byte(g >> 8)
			row[i+2] = byte(r >> 8)
			row[i+3] = byte(a >> 8)
		}
	}
}

// hdropBytes is a DROPFILES listing. pFiles points past the 20-byte header
// and fWide is set, so the path list is UTF-16 with an extra trailing NUL.
func hdropBytes(paths []string) []byte {
	var files []uint16
	for _, path := range paths {
		if path == "" {
			continue
		}
		encoded := utf16.Encode([]rune(path))
		files = append(files, encoded...)
		files = append(files, 0)
	}
	if len(files) == 0 {
		return nil
	}
	files = append(files, 0)
	buf := make([]byte, 20+len(files)*2)
	binary.LittleEndian.PutUint32(buf[0:], 20)
	binary.LittleEndian.PutUint32(buf[16:], 1)
	for i, c := range files {
		binary.LittleEndian.PutUint16(buf[20+i*2:], c)
	}
	return buf
}

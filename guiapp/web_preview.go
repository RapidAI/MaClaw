package guiapp

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Web preview is a loopback site, not a route on the assistant origin.
// The framed page keeps its own origin, so its scripts cannot reach the
// Wails bindings of the assistant window.

const (
	webPreviewRoutePrefix = "/web-preview/v1/"
	webPreviewBridgeName  = "__mc_bridge.js"
	webPreviewIdleTTL     = 30 * time.Minute
	webPreviewMaxSessions = 8
	webPreviewInjectLimit = 8 << 20
)

// WebPreviewInfo is the loopback site for one HTML file's directory.
type WebPreviewInfo struct {
	ID        string `json:"id"`
	Origin    string `json:"origin"`
	Token     string `json:"token"`
	Root      string `json:"root"`
	EntryURL  string `json:"entry_url"`
	EntryPath string `json:"entry_path"`
}

type webPreviewSession struct {
	root     string
	lastUsed time.Time
	refs     int
}

var (
	webPreviewMu       sync.Mutex
	webPreviewOrigin   string
	webPreviewListener net.Listener
	webPreviewSessions = map[string]*webPreviewSession{}
)

var webPreviewHeadRE = regexp.MustCompile(`(?i)<head\b[^>]*>`)

var webPreviewBridgeJS = []byte(`(function(){
  function locationPath(){
    var pathname=location.pathname||"";
    var mark="/web-preview/v1/";
    var at=pathname.indexOf(mark);
    var rest=pathname;
    if(at>=0){
      rest=pathname.slice(at+mark.length);
      var slash=rest.indexOf("/");
      rest=slash>=0?rest.slice(slash+1):"";
    }
    if(rest.charAt(0)==="/") rest=rest.slice(1);
    return rest+(location.search||"")+(location.hash||"");
  }
  function send(){
    try{parent.postMessage({source:"mc-web-preview",type:"location",path:locationPath()},"*");}catch(e){}
  }
  send();
  window.addEventListener("popstate",send);
  window.addEventListener("hashchange",send);
  var pushState=history.pushState;
  history.pushState=function(){var result=pushState.apply(this,arguments);send();return result;};
  var replaceState=history.replaceState;
  history.replaceState=function(){var result=replaceState.apply(this,arguments);send();return result;};
  document.addEventListener("click",function(){setTimeout(send,0);},true);
})();`)

// OpenWebPreview serves the directory that contains absPath on 127.0.0.1.
// HTML and SVG can be the entry page. Relative assets in that directory load with it.
// http(s) pages are loaded by the preview frame itself and are not served here.
func (a *App) OpenWebPreview(absPath string) (*WebPreviewInfo, error) {
	lowered := strings.ToLower(strings.TrimSpace(absPath))
	if strings.HasPrefix(lowered, "http://") || strings.HasPrefix(lowered, "https://") {
		return nil, fmt.Errorf("网络地址请直接在预览中打开")
	}
	cleaned, err := a.normalizeOpenPathForApp(absPath)
	if err != nil {
		return nil, fmt.Errorf("文件路径无效")
	}
	if abs, absErr := filepath.Abs(cleaned); absErr == nil {
		cleaned = abs
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("文件不存在")
		}
		return nil, fmt.Errorf("无法打开文件")
	}
	root := cleaned
	entry := "index.html"
	if info.IsDir() {
		index := filepath.Join(cleaned, "index.html")
		indexInfo, indexErr := os.Stat(index)
		if indexErr != nil || indexInfo.IsDir() {
			return nil, fmt.Errorf("该目录没有 index.html")
		}
		root = cleaned
	} else {
		if !isWebPreviewDocument(cleaned) {
			return nil, fmt.Errorf("只能预览 HTML 或 SVG")
		}
		root = filepath.Dir(cleaned)
		entry = filepath.Base(cleaned)
	}
	root = filepath.Clean(root)
	if resolved, resolveErr := filepath.EvalSymlinks(root); resolveErr == nil && resolved != "" {
		root = filepath.Clean(resolved)
	}
	entryFull := filepath.Join(root, filepath.FromSlash(entry))
	if _, _, entryErr := resolveWebPreviewFile(root, entryFull); entryErr != nil {
		return nil, fmt.Errorf("无法打开文件")
	}

	origin, err := ensureWebPreviewServer()
	if err != nil {
		return nil, fmt.Errorf("无法启动网页预览")
	}
	token := issueWebPreviewSession(root)
	if token == "" {
		return nil, fmt.Errorf("无法启动网页预览")
	}
	entry = filepath.ToSlash(entry)
	return &WebPreviewInfo{
		ID:        token,
		Origin:    origin,
		Token:     token,
		Root:      root,
		EntryURL:  webPreviewFileURL(origin, token, entry),
		EntryPath: entry,
	}, nil
}

// CloseWebPreview drops a preview session. The loopback listener stays up for the next page.
func (a *App) CloseWebPreview(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	webPreviewMu.Lock()
	sess := webPreviewSessions[id]
	if sess != nil {
		sess.refs--
		if sess.refs <= 0 {
			delete(webPreviewSessions, id)
		}
	}
	webPreviewMu.Unlock()
}

func isWebPreviewHTML(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html", ".htm", ".xhtml":
		return true
	default:
		return false
	}
}

func isWebPreviewDocument(name string) bool {
	return isWebPreviewHTML(name) || strings.EqualFold(filepath.Ext(name), ".svg")
}

func ensureWebPreviewServer() (string, error) {
	webPreviewMu.Lock()
	defer webPreviewMu.Unlock()
	if webPreviewOrigin != "" && webPreviewListener != nil {
		return webPreviewOrigin, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	srv := &http.Server{
		Handler:           http.HandlerFunc(handleWebPreviewHTTP),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = srv.Serve(ln)
	}()
	webPreviewListener = ln
	webPreviewOrigin = "http://" + ln.Addr().String()
	return webPreviewOrigin, nil
}

func issueWebPreviewSession(root string) string {
	now := time.Now()
	webPreviewMu.Lock()
	defer webPreviewMu.Unlock()
	evictIdleWebPreviewSessions(now)
	for id, sess := range webPreviewSessions {
		if sess != nil && sess.root == root {
			sess.refs++
			sess.lastUsed = now
			return id
		}
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	token := hex.EncodeToString(raw[:])
	for len(webPreviewSessions) >= webPreviewMaxSessions {
		var oldest string
		var oldestUsed time.Time
		for id, sess := range webPreviewSessions {
			if sess == nil {
				oldest = id
				break
			}
			// A page still on screen keeps its session. Only unused ones
			// make room; a live preview must not 404 to admit another.
			if sess.refs > 0 {
				continue
			}
			if oldest == "" || sess.lastUsed.Before(oldestUsed) {
				oldest = id
				oldestUsed = sess.lastUsed
			}
		}
		if oldest == "" {
			break
		}
		delete(webPreviewSessions, oldest)
	}
	webPreviewSessions[token] = &webPreviewSession{root: root, lastUsed: now, refs: 1}
	return token
}

func evictIdleWebPreviewSessions(now time.Time) {
	for id, sess := range webPreviewSessions {
		if sess == nil || (sess.refs <= 0 && now.Sub(sess.lastUsed) > webPreviewIdleTTL) {
			delete(webPreviewSessions, id)
		}
	}
}

func lookupWebPreviewRoot(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if len(token) != 32 {
		return "", false
	}
	if _, err := hex.DecodeString(token); err != nil {
		return "", false
	}
	now := time.Now()
	webPreviewMu.Lock()
	defer webPreviewMu.Unlock()
	sess, ok := webPreviewSessions[token]
	if !ok || sess == nil {
		return "", false
	}
	// A mounted preview keeps its session. Idle expiry is only for one the
	// assistant has already closed, so a page left on screen does not 404.
	if sess.refs <= 0 && now.Sub(sess.lastUsed) > webPreviewIdleTTL {
		delete(webPreviewSessions, token)
		return "", false
	}
	sess.lastUsed = now
	return sess.root, true
}

func handleWebPreviewHTTP(w http.ResponseWriter, r *http.Request) {
	if r == nil || r.URL == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !strings.HasPrefix(r.URL.Path, webPreviewRoutePrefix) {
		http.NotFound(w, r)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, webPreviewRoutePrefix)
	token, rel, _ := strings.Cut(rest, "/")
	root, ok := lookupWebPreviewRoot(token)
	if !ok {
		http.NotFound(w, r)
		return
	}
	rel = strings.TrimPrefix(rel, "/")
	if rel == webPreviewBridgeName {
		serveWebPreviewBridge(w, r)
		return
	}
	full, err := safeWebPreviewPath(root, rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	resolved, info, err := resolveWebPreviewFile(root, full)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if isWebPreviewHTML(resolved) && info.Size() <= webPreviewInjectLimit {
		body, readErr := os.ReadFile(resolved)
		if readErr != nil {
			http.NotFound(w, r)
			return
		}
		scriptURL := webPreviewRoutePrefix + token + "/" + webPreviewBridgeName
		body = injectWebPreviewBridge(body, scriptURL)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Zero modtime: a rewrite in the same second must not come back as 304.
		http.ServeContent(w, r, info.Name(), time.Time{}, bytes.NewReader(body))
		return
	}
	file, openErr := os.Open(resolved)
	if openErr != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	if ctype := webPreviewContentType(resolved); ctype != "" {
		w.Header().Set("Content-Type", ctype)
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, info.Name(), time.Time{}, file)
}

func serveWebPreviewBridge(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, webPreviewBridgeName, time.Time{}, bytes.NewReader(webPreviewBridgeJS))
}

func injectWebPreviewBridge(body []byte, scriptURL string) []byte {
	if scriptURL == "" || bytes.Contains(body, []byte(scriptURL)) {
		return body
	}
	tag := []byte(`<script src="` + scriptURL + `"></script>`)
	if loc := webPreviewHeadRE.FindIndex(body); loc != nil {
		out := make([]byte, 0, len(body)+len(tag))
		out = append(out, body[:loc[1]]...)
		out = append(out, tag...)
		out = append(out, body[loc[1]:]...)
		return out
	}
	out := make([]byte, 0, len(body)+len(tag))
	out = append(out, tag...)
	out = append(out, body...)
	return out
}

func webPreviewContentType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".html", ".htm", ".xhtml":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".wasm":
		return "application/wasm"
	}
	if ctype := mime.TypeByExtension(ext); ctype != "" {
		return ctype
	}
	return ""
}

func webPreviewFileURL(origin, token, rel string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(origin, "/"))
	b.WriteString(webPreviewRoutePrefix)
	b.WriteString(token)
	b.WriteByte('/')
	first := true
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == "" || seg == "." {
			continue
		}
		if !first {
			b.WriteByte('/')
		}
		first = false
		b.WriteString(url.PathEscape(seg))
	}
	if first {
		b.WriteString("index.html")
	}
	return b.String()
}

func safeWebPreviewPath(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	rel = strings.ReplaceAll(rel, "\\", "/")
	if strings.Contains(rel, "\x00") || strings.Contains(rel, ":") {
		return "", os.ErrPermission
	}
	if decoded, err := url.PathUnescape(rel); err == nil {
		rel = decoded
	}
	if strings.Contains(rel, "\\") || strings.Contains(rel, ":") || strings.Contains(rel, "\x00") {
		return "", os.ErrPermission
	}
	cleaned := path.Clean("/" + rel)
	if cleaned == "/" || cleaned == "." {
		cleaned = "/index.html"
	}
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "" || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", os.ErrPermission
	}
	full := filepath.Clean(filepath.Join(root, filepath.FromSlash(cleaned)))
	if !pathWithinRoot(root, full) {
		return "", os.ErrPermission
	}
	return full, nil
}

func resolveWebPreviewFile(root, full string) (string, os.FileInfo, error) {
	info, err := os.Lstat(full)
	if err != nil {
		return "", nil, err
	}
	if info.IsDir() {
		index := filepath.Join(full, "index.html")
		if !pathWithinRoot(root, index) {
			return "", nil, os.ErrPermission
		}
		full = index
		info, err = os.Lstat(full)
		if err != nil {
			return "", nil, err
		}
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil {
		resolvedRoot = root
	}
	resolved := full
	if info.Mode()&os.ModeSymlink != 0 {
		target, evalErr := filepath.EvalSymlinks(full)
		if evalErr != nil {
			return "", nil, evalErr
		}
		resolved = target
		info, err = os.Stat(resolved)
		if err != nil {
			return "", nil, err
		}
	} else if eval, evalErr := filepath.EvalSymlinks(full); evalErr == nil {
		resolved = eval
	}
	if !pathWithinRoot(resolvedRoot, resolved) || info.IsDir() {
		return "", nil, os.ErrPermission
	}
	return resolved, info, nil
}

func pathWithinRoot(root, full string) bool {
	root = filepath.Clean(root)
	full = filepath.Clean(full)
	if root == "" || full == "" {
		return false
	}
	if !strings.EqualFold(filepath.VolumeName(root), filepath.VolumeName(full)) && filepath.VolumeName(root) != "" {
		return false
	}
	sep := string(filepath.Separator)
	rootCmp := strings.ToLower(root)
	fullCmp := strings.ToLower(full)
	if fullCmp == rootCmp {
		return true
	}
	if !strings.HasSuffix(rootCmp, sep) {
		rootCmp += sep
	}
	return strings.HasPrefix(fullCmp, rootCmp)
}

package browser

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/pyenv"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
	xdraw "golang.org/x/image/draw"
)

// ocrMaxLongEdge caps the longest side of images sent to the RapidOCR sidecar.
// Full multi-monitor captures (e.g. 9840x3840) OOM/kill the Python process when
// fed at native resolution; downscale for OCR then map bboxes back.
const ocrMaxLongEdge = 2560

// ocrServerPy is the Python sidecar script embedded as a Go string.
const ocrServerPy = `#!/usr/bin/env python3
"""RapidOCR sidecar server - stdin/stdout JSON protocol."""
import sys, json, base64, signal

def main():
    from rapidocr_onnxruntime import RapidOCR
    engine = RapidOCR()
    signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            print(json.dumps({"error": "invalid json"}), flush=True)
            continue
        method = req.get("method", "")
        if method == "ocr":
            try:
                img_bytes = base64.b64decode(req["image_base64"])
                result, _ = engine(img_bytes)
                items = []
                if result:
                    for box, text, score in result:
                        x0, y0 = int(box[0][0]), int(box[0][1])
                        x1, y1 = int(box[2][0]), int(box[2][1])
                        items.append({
                            "text": text,
                            "confidence": round(float(score), 4),
                            "bbox": [x0, y0, x1 - x0, y1 - y0]
                        })
                print(json.dumps({"results": items}), flush=True)
            except Exception as e:
                print(json.dumps({"error": str(e)}), flush=True)
        elif method == "ping":
            print(json.dumps({"status": "ok"}), flush=True)
        else:
            print(json.dumps({"error": "unknown method: " + method}), flush=True)

if __name__ == "__main__":
    main()
`

// RapidOCRSidecar manages a Python RapidOCR process via stdin/stdout JSON.
type RapidOCRSidecar struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	scanner   *bufio.Scanner
	ready     bool
	idleTimer *time.Timer
	ocrDir    string // <MaclawBaseDir>/ocr/
	logger    func(string)
	statusC   chan string // optional: status messages for UI

	stderrLines []string   // recent sidecar stderr lines (diagnostics tail)
	stderrMu    sync.Mutex // guards stderrLines (separate from s.mu: drain must not block on Recognize/start)
}

// NewRapidOCRSidecar creates a sidecar manager.
func NewRapidOCRSidecar(logger func(string)) *RapidOCRSidecar {
	return &RapidOCRSidecar{
		ocrDir: filepath.Join(corelib.MaclawBaseDir(), "ocr"),
		logger: logger,
	}
}

// SetStatusChannel sets an optional channel for installation progress messages.
func (s *RapidOCRSidecar) SetStatusChannel(ch chan string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusC = ch
}

// Recognize implements OCRProvider.
// Large screenshots are downscaled before OCR; bounding boxes are scaled back
// to the original image coordinate space.
func (s *RapidOCRSidecar) Recognize(pngBase64 string) ([]OCRResult, error) {
	ocrB64, scaleX, scaleY, prepErr := prepareOCRImageBase64(pngBase64, ocrMaxLongEdge)
	if prepErr != nil {
		// Fail closed: never replay an unprepared payload. Falling back to the
		// original image reintroduces OOM/"pipe closed" on multi-monitor captures
		// (even when base64 is small due to high PNG compression).
		return nil, fmt.Errorf("OCR prepare: %w", prepErr)
	}
	if scaleX != 1 || scaleY != 1 {
		s.log("OCR image downscaled scale_x=%.3f scale_y=%.3f max_edge=%d", scaleX, scaleY, ocrMaxLongEdge)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReadyLocked(); err != nil {
		return nil, err
	}
	s.resetIdleTimer()

	// Send request
	req, _ := json.Marshal(map[string]string{
		"method":       "ocr",
		"image_base64": ocrB64,
	})
	if _, err := fmt.Fprintf(s.stdin, "%s\n", req); err != nil {
		s.stopLocked()
		return nil, fmt.Errorf("write to OCR sidecar: %w", err)
	}

	// Read response (with timeout to prevent indefinite blocking)
	type scanResult struct {
		line string
		ok   bool
	}
	scanCh := make(chan scanResult, 1)
	go func() {
		ok := s.scanner.Scan()
		scanCh <- scanResult{line: s.scanner.Text(), ok: ok}
	}()

	var line string
	select {
	case sr := <-scanCh:
		if !sr.ok {
			s.stopLocked()
			return nil, fmt.Errorf("OCR sidecar closed unexpectedly")
		}
		line = sr.line
	case <-time.After(60 * time.Second):
		s.stopLocked()
		return nil, fmt.Errorf("OCR sidecar response timeout (60s)")
	}

	var resp struct {
		Results []OCRResult `json:"results"`
		Error   string      `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		return nil, fmt.Errorf("parse OCR response: %w", err)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("OCR error: %s", resp.Error)
	}
	return scaleOCRResults(resp.Results, scaleX, scaleY), nil
}

// prepareOCRImageBase64 decodes a PNG/JPEG base64 image and, when the longest
// edge exceeds maxEdge, downscales it for OCR. Returns re-encoded PNG base64
// and scale factors (orig/new) so bboxes can be mapped back to original coords.
//
// Fast path: when the image is already under maxEdge, only DecodeConfig is used
// (no full pixel decode / re-encode).
func prepareOCRImageBase64(pngBase64 string, maxEdge int) (outB64 string, scaleX, scaleY float64, err error) {
	if maxEdge <= 0 {
		maxEdge = ocrMaxLongEdge
	}
	raw, err := decodeImageBytes(pngBase64)
	if err != nil {
		return "", 1, 1, err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return "", 1, 1, fmt.Errorf("decode image config: %w", err)
	}
	w, h := cfg.Width, cfg.Height
	if w <= 0 || h <= 0 {
		return "", 1, 1, fmt.Errorf("invalid image size %dx%d", w, h)
	}
	long := w
	if h > long {
		long = h
	}
	if long <= maxEdge {
		// Already small enough — keep pixels as-is, but always hand the sidecar
		// pure std base64 (never a data: URI prefix).
		return pureStdBase64(pngBase64, raw), 1, 1, nil
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", 1, 1, fmt.Errorf("decode image: %w", err)
	}
	bounds := img.Bounds()
	// Prefer actual pixel bounds if they differ from config (rare, but safer).
	if bw, bh := bounds.Dx(), bounds.Dy(); bw > 0 && bh > 0 {
		w, h = bw, bh
		long = w
		if h > long {
			long = h
		}
		if long <= maxEdge {
			return pureStdBase64(pngBase64, raw), 1, 1, nil
		}
	}
	newW := w * maxEdge / long
	newH := h * maxEdge / long
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, bounds, xdraw.Src, nil)
	var buf bytes.Buffer
	// Pre-size roughly: RGBA PNG often compresses well; avoid tiny buffer growth.
	buf.Grow(newW * newH / 4)
	if err := png.Encode(&buf, dst); err != nil {
		return "", 1, 1, fmt.Errorf("encode resized png: %w", err)
	}
	scaleX = float64(w) / float64(newW)
	scaleY = float64(h) / float64(newH)
	return base64.StdEncoding.EncodeToString(buf.Bytes()), scaleX, scaleY, nil
}

func decodeImageBytes(b64 string) ([]byte, error) {
	b64 = stripBase64Payload(b64)
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("base64 decode: %w", err)
		}
	}
	return raw, nil
}

// stripBase64Payload removes data-URI prefixes and surrounding whitespace.
func stripBase64Payload(b64 string) string {
	b64 = strings.TrimSpace(b64)
	if i := strings.Index(b64, ","); i >= 0 && strings.Contains(strings.ToLower(b64[:i]), "base64") {
		return strings.TrimSpace(b64[i+1:])
	}
	return b64
}

// pureStdBase64 returns a sidecar-safe std base64 string for raw image bytes.
// Strips data: URI prefixes. Reuses the cleaned payload when it is already
// std-padded base64 (matches EncodedLen) so the common screenshot path does
// not re-encode or re-decode multi-MB strings on every observe.
func pureStdBase64(orig string, raw []byte) string {
	cleaned := stripBase64Payload(orig)
	// decodeImageBytes already verified `cleaned` decodes (std or raw-std).
	// Std encoding with padding has a fixed length; reuse without another decode.
	if len(cleaned) == base64.StdEncoding.EncodedLen(len(raw)) {
		return cleaned
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func scaleOCRResults(results []OCRResult, scaleX, scaleY float64) []OCRResult {
	if len(results) == 0 || (scaleX == 1 && scaleY == 1) {
		return results
	}
	if scaleX <= 0 {
		scaleX = 1
	}
	if scaleY <= 0 {
		scaleY = 1
	}
	out := make([]OCRResult, len(results))
	for i, r := range results {
		bw := int(float64(r.BBox[2])*scaleX + 0.5)
		bh := int(float64(r.BBox[3])*scaleY + 0.5)
		if r.BBox[2] > 0 && bw < 1 {
			bw = 1
		}
		if r.BBox[3] > 0 && bh < 1 {
			bh = 1
		}
		out[i] = OCRResult{
			Text:       r.Text,
			Confidence: r.Confidence,
			BBox: [4]int{
				int(float64(r.BBox[0])*scaleX + 0.5),
				int(float64(r.BBox[1])*scaleY + 0.5),
				bw,
				bh,
			},
		}
	}
	return out
}

// IsAvailable implements OCRProvider.
func (s *RapidOCRSidecar) IsAvailable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ready {
		return true
	}
	// Check if script + lib already installed
	scriptPath := filepath.Join(s.ocrDir, "ocr_server.py")
	if _, err := os.Stat(scriptPath); err == nil {
		return true // installed but not running — can start on demand
	}
	// Check if Python available for auto-install
	st := pyenv.Detect()
	return st.Available
}

// Installed reports whether the OCR server script is on disk (pip target done).
func (s *RapidOCRSidecar) Installed() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	scriptPath := filepath.Join(s.ocrDir, "ocr_server.py")
	_, err := os.Stat(scriptPath)
	return err == nil
}

// Ready reports whether the long-lived OCR process is running.
func (s *RapidOCRSidecar) Ready() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// Warm starts the OCR sidecar if already installed. Does not run pip install
// (that stays on first Recognize). Used by Computer Use startup warmup.
func (s *RapidOCRSidecar) Warm() error {
	if s == nil {
		return fmt.Errorf("OCR sidecar nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ready {
		s.resetIdleTimer()
		return nil
	}
	scriptPath := filepath.Join(s.ocrDir, "ocr_server.py")
	if _, err := os.Stat(scriptPath); err != nil {
		return fmt.Errorf("OCR not installed yet (will install on first use)")
	}
	return s.startLocked()
}

// Close implements OCRProvider.
func (s *RapidOCRSidecar) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

// ── internal ──

func (s *RapidOCRSidecar) ensureReadyLocked() error {
	if s.ready {
		return nil
	}

	scriptPath := filepath.Join(s.ocrDir, "ocr_server.py")

	// Check if script exists; if not, install
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		if err := s.installLockedForce(false); err != nil {
			return fmt.Errorf("OCR install: %w", err)
		}
	}

	startErr := s.startLocked()
	if startErr == nil {
		return nil
	}
	// Previously installed but startup failed — commonly a stale lib whose
	// wheels target a different Python (ABI mismatch). Force one reinstall
	// with the current interpreter, then retry once.
	s.log("OCR sidecar start failed: %v; forcing one reinstall", startErr)
	if rerr := s.installLockedForce(true); rerr != nil {
		return fmt.Errorf("OCR sidecar start failed: %v (reinstall also failed: %v)", startErr, rerr)
	}
	if err2 := s.startLocked(); err2 != nil {
		return fmt.Errorf("OCR sidecar start failed after reinstall: %v", err2)
	}
	return nil
}

func (s *RapidOCRSidecar) installLocked() error {
	return s.installLockedForce(false)
}

// runOCRPython runs a short Python subcommand with output discarded.
func runOCRPython(pythonPath string, args ...string) error {
	cmd := exec.Command(pythonPath, args...)
	coretool.HideCommandWindow(cmd)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// installLockedForce installs RapidOCR into ~/.maclaw/ocr/lib.
// When force is true, clears the target lib dir first (repair ABI mismatch).
func (s *RapidOCRSidecar) installLockedForce(force bool) error {
	// Use pyenv's managed Python (private install or system fallback)
	st := pyenv.Detect()
	if !st.Available {
		return fmt.Errorf("Python 不可用，无法安装 RapidOCR。请先安装 Python 3.10+")
	}

	if force {
		s.emitStatus("正在重装 OCR 引擎…")
		s.log("force-reinstalling rapidocr-onnxruntime to %s", s.ocrDir)
	} else {
		s.emitStatus("正在安装 OCR 引擎（首次使用，约 30 秒）...")
		s.log("installing rapidocr-onnxruntime to %s", s.ocrDir)
	}

	libDir := filepath.Join(s.ocrDir, "lib")
	if force {
		// Wipe stale wheels so pip reinstalls for the current interpreter.
		if err := os.RemoveAll(libDir); err != nil {
			return fmt.Errorf("clear OCR lib dir: %w", err)
		}
	}
	if err := os.MkdirAll(libDir, 0755); err != nil {
		return fmt.Errorf("create OCR lib dir: %w", err)
	}

	// Prefer venv pip (if venv ready), otherwise use detected python -m pip
	pythonPath := st.PythonPath
	if st.VenvReady {
		if vp, err := pyenv.VenvPython(); err == nil {
			pythonPath = vp
		}
	}

	// uv-managed venvs ship without pip; bootstrap ensurepip so that
	// `python -m pip` works with this exact interpreter (ABI consistency —
	// wheels must be built for the interpreter that will run the sidecar).
	if err := runOCRPython(pythonPath, "-m", "pip", "--version"); err != nil {
		if berr := runOCRPython(pythonPath, "-m", "ensurepip", "--upgrade"); berr != nil {
			s.log("ensurepip bootstrap failed: %v (pip check: %v)", berr, err)
		}
	}

	// pip install --target=~/.maclaw/ocr/lib/ rapidocr-onnxruntime
	// This keeps all packages in our private directory, easy to manage and uninstall
	pipArgs := []string{"-m", "pip", "install",
		"--target", libDir,
		"--no-warn-script-location",
	}
	if force {
		pipArgs = append(pipArgs, "--upgrade", "--force-reinstall", "--no-cache-dir")
	}
	pipArgs = append(pipArgs, "rapidocr-onnxruntime")
	cmd := exec.Command(pythonPath, pipArgs...)
	coretool.HideCommandWindow(cmd)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		s.emitStatus("OCR 引擎安装失败，请手动执行: pip install --target=" + libDir + " rapidocr-onnxruntime")
		return fmt.Errorf("pip install: %w", err)
	}

	// Always rewrite ocr_server.py so embedded protocol stays in sync.
	scriptPath := filepath.Join(s.ocrDir, "ocr_server.py")
	if err := os.WriteFile(scriptPath, []byte(ocrServerPy), 0644); err != nil {
		return fmt.Errorf("write ocr_server.py: %w", err)
	}

	s.emitStatus("OCR 引擎安装完成")
	s.log("rapidocr installed to %s (force=%v)", libDir, force)
	return nil
}

func (s *RapidOCRSidecar) startLocked() error {
	scriptPath := filepath.Join(s.ocrDir, "ocr_server.py")
	libDir := filepath.Join(s.ocrDir, "lib")

	// Determine Python to use (prefer venv, then private, then system)
	pythonPath := ""
	st := pyenv.Detect()
	if st.VenvReady {
		if vp, err := pyenv.VenvPython(); err == nil {
			pythonPath = vp
		}
	}
	if pythonPath == "" && st.Available {
		pythonPath = st.PythonPath
	}
	if pythonPath == "" {
		return fmt.Errorf("Python not available, cannot start OCR sidecar")
	}

	cmd := exec.Command(pythonPath, scriptPath)
	coretool.HideCommandWindow(cmd)
	// Set PYTHONPATH so the sidecar can find rapidocr in our private lib dir
	cmd.Env = append(os.Environ(), "PYTHONPATH="+libDir)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("start OCR sidecar: %w", err)
	}

	// Drain stderr so we can diagnose OOM/crashes instead of only seeing "pipe closed".
	go func(r io.Reader) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line != "" {
				s.log("stderr: %s", line)
				s.stderrMu.Lock()
				s.stderrLines = append(s.stderrLines, line)
				if len(s.stderrLines) > 12 {
					s.stderrLines = s.stderrLines[len(s.stderrLines)-12:]
				}
				s.stderrMu.Unlock()
			}
		}
	}(stderr)

	s.cmd = cmd
	s.stdin = stdin
	s.scanner = bufio.NewScanner(stdout)
	// OCR JSON can be large on text-dense screens; keep a generous buffer.
	s.scanner.Buffer(make([]byte, 0, 4*1024*1024), 16*1024*1024)

	// Ping to verify. The response must be the JSON pong: sidecar startup may
	// print warning lines (e.g. OpenCV's numpy fallback notice) to stdout
	// right before dying on a broken environment. Accepting any line as pong
	// marks a doomed process "ready", and the first real write then fails
	// with a cryptic pipe error far from the root cause.
	pingReq, _ := json.Marshal(map[string]string{"method": "ping"})
	if _, err := fmt.Fprintf(stdin, "%s\n", pingReq); err != nil {
		s.stopLocked()
		return fmt.Errorf("ping OCR sidecar: %w", err)
	}
	if skipped, err := readOCRPong(s.scanner, ocrPingTimeout); err != nil {
		tail := s.stderrTailLocked()
		s.stopLocked()
		if tail != "" {
			return fmt.Errorf("OCR sidecar readiness check failed: %v (stderr: %s)", err, tail)
		}
		return fmt.Errorf("OCR sidecar readiness check failed: %v", err)
	} else if len(skipped) > 0 {
		s.log("pong received after %d non-JSON stdout line(s), first: %q", len(skipped), skipped[0])
	}

	s.stderrMu.Lock()
	s.stderrLines = nil
	s.stderrMu.Unlock()
	s.ready = true
	s.log("OCR sidecar started (pid=%d)", cmd.Process.Pid)
	s.resetIdleTimer()
	return nil
}

func (s *RapidOCRSidecar) stopLocked() {
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}
	if s.stdin != nil {
		s.stdin.Close()
		s.stdin = nil
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
		s.cmd = nil
	}
	s.scanner = nil
	s.ready = false
}

// ocrPingTimeout bounds the sidecar readiness (pong) wait. Cold starts load
// onnxruntime models before the loop reads stdin, which can take tens of
// seconds on slow machines.
const ocrPingTimeout = 45 * time.Second

// readOCRPong reads sidecar stdout until the JSON {"status":"ok"} pong
// arrives, skipping non-JSON warning lines (e.g. OpenCV's numpy notice) which
// are returned for diagnostics. EOF (process exit) and timeout are errors.
func readOCRPong(scanner *bufio.Scanner, timeout time.Duration) (skipped []string, err error) {
	type result struct {
		line string
		ok   bool
	}
	ch := make(chan result, 1)
	go func() {
		for scanner.Scan() {
			ch <- result{line: scanner.Text(), ok: true}
		}
		ch <- result{ok: false}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case r := <-ch:
			if !r.ok {
				return skipped, fmt.Errorf("sidecar exited without responding to ping")
			}
			line := strings.TrimSpace(r.line)
			var pong struct {
				Status string `json:"status"`
			}
			if json.Unmarshal([]byte(line), &pong) == nil && pong.Status == "ok" {
				return skipped, nil
			}
			if line != "" {
				skipped = append(skipped, line)
			}
		case <-timer.C:
			return skipped, fmt.Errorf("pong timeout after %s", timeout)
		}
	}
}

// stderrTailLocked returns a compact tail of recent sidecar stderr lines for
// error messages. Caller must hold s.mu; stderrLines itself uses stderrMu.
func (s *RapidOCRSidecar) stderrTailLocked() string {
	s.stderrMu.Lock()
	defer s.stderrMu.Unlock()
	if len(s.stderrLines) == 0 {
		return ""
	}
	tail := strings.Join(s.stderrLines, " | ")
	const max = 400
	if len(tail) > max {
		tail = tail[len(tail)-max:]
	}
	return tail
}

func (s *RapidOCRSidecar) resetIdleTimer() {
	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	s.idleTimer = time.AfterFunc(5*time.Minute, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.ready {
			s.log("OCR sidecar idle timeout, stopping")
			s.stopLocked()
		}
	})
}

func (s *RapidOCRSidecar) emitStatus(msg string) {
	if s.statusC != nil {
		select {
		case s.statusC <- msg:
		default:
		}
	}
}

func (s *RapidOCRSidecar) log(format string, args ...interface{}) {
	if s.logger != nil {
		s.logger(fmt.Sprintf("[ocr-sidecar] "+format, args...))
	}
}

package browser

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/pyenv"
)

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
	ocrDir    string // ~/.maclaw/ocr/
	logger    func(string)
	statusC   chan string // optional: status messages for UI
}

// NewRapidOCRSidecar creates a sidecar manager.
func NewRapidOCRSidecar(logger func(string)) *RapidOCRSidecar {
	home, _ := os.UserHomeDir()
	return &RapidOCRSidecar{
		ocrDir: filepath.Join(home, ".maclaw", "ocr"),
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
func (s *RapidOCRSidecar) Recognize(pngBase64 string) ([]OCRResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.ensureReadyLocked(); err != nil {
		return nil, err
	}
	s.resetIdleTimer()

	// Send request
	req, _ := json.Marshal(map[string]string{
		"method":       "ocr",
		"image_base64": pngBase64,
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
	return resp.Results, nil
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
		if err := s.installLocked(); err != nil {
			return fmt.Errorf("OCR install: %w", err)
		}
	}

	// Start sidecar
	return s.startLocked()
}

func (s *RapidOCRSidecar) installLocked() error {
	// Use pyenv's managed Python (private install or system fallback)
	st := pyenv.Detect()
	if !st.Available {
		return fmt.Errorf("Python 不可用，无法安装 RapidOCR。请先安装 Python 3.10+")
	}

	s.emitStatus("正在安装 OCR 引擎（首次使用，约 30 秒）...")
	s.log("installing rapidocr-onnxruntime to %s", s.ocrDir)

	// Create directories
	libDir := filepath.Join(s.ocrDir, "lib")
	if err := os.MkdirAll(libDir, 0755); err != nil {
		return fmt.Errorf("create OCR lib dir: %w", err)
	}

	// Determine which pip to use:
	// Prefer venv pip (if venv ready), otherwise use detected python -m pip
	pythonPath := st.PythonPath
	if st.VenvReady {
		if vp, err := pyenv.VenvPython(); err == nil {
			pythonPath = vp
		}
	}

	// pip install --target=~/.maclaw/ocr/lib/ rapidocr-onnxruntime
	// This keeps all packages in our private directory, easy to manage and uninstall
	cmd := exec.Command(pythonPath, "-m", "pip", "install",
		"--target", libDir,
		"--no-warn-script-location",
		"rapidocr-onnxruntime")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		s.emitStatus("OCR 引擎安装失败，请手动执行: pip install --target=" + libDir + " rapidocr-onnxruntime")
		return fmt.Errorf("pip install: %w", err)
	}

	// Write ocr_server.py
	scriptPath := filepath.Join(s.ocrDir, "ocr_server.py")
	if err := os.WriteFile(scriptPath, []byte(ocrServerPy), 0644); err != nil {
		return fmt.Errorf("write ocr_server.py: %w", err)
	}

	s.emitStatus("OCR 引擎安装完成")
	s.log("rapidocr installed to %s", libDir)
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

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("start OCR sidecar: %w", err)
	}

	s.cmd = cmd
	s.stdin = stdin
	s.scanner = bufio.NewScanner(stdout)
	s.scanner.Buffer(make([]byte, 0, 4*1024*1024), 4*1024*1024) // 4MB buffer for large OCR responses

	// Ping to verify
	pingReq, _ := json.Marshal(map[string]string{"method": "ping"})
	if _, err := fmt.Fprintf(stdin, "%s\n", pingReq); err != nil {
		s.stopLocked()
		return fmt.Errorf("ping OCR sidecar: %w", err)
	}
	if !s.scanner.Scan() {
		s.stopLocked()
		return fmt.Errorf("OCR sidecar did not respond to ping")
	}

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

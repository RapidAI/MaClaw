package browser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BrowserRecorder records user browser operations via CDP events.
type BrowserRecorder struct {
	mu        sync.Mutex
	recording bool
	startTime time.Time
	startURL  string
	steps     []RecordedStep
	flowDir   string // ~/.maclaw/browser_flows/
	sessionFn func() (*Session, error)
	logger    func(string)
}

// NewBrowserRecorder creates a recorder.
func NewBrowserRecorder(sessionFn func() (*Session, error), logger func(string)) *BrowserRecorder {
	home, _ := os.UserHomeDir()
	return &BrowserRecorder{
		flowDir:   filepath.Join(home, ".maclaw", "browser_flows"),
		sessionFn: sessionFn,
		logger:    logger,
	}
}

// Start begins recording browser operations.
// It captures the current page URL as the start point and enables CDP event listeners.
func (r *BrowserRecorder) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.recording {
		return fmt.Errorf("already recording")
	}

	sess, err := r.sessionFn()
	if err != nil {
		return fmt.Errorf("browser session: %w", err)
	}

	info, _ := sess.Info()
	r.startURL = ""
	if info != nil {
		r.startURL = info.URL
	}

	r.recording = true
	r.startTime = time.Now()
	r.steps = nil

	r.log("recording started from %s", r.startURL)
	return nil
}

// RecordStep manually adds a step to the recording.
// This is called by the agent or tool handlers when they perform browser actions.
func (r *BrowserRecorder) RecordStep(action, selector, text, url string, coords [2]int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return
	}

	step := RecordedStep{
		Action:    action,
		Selector:  selector,
		Text:      text,
		URL:       url,
		Coords:    coords,
		Timestamp: time.Since(r.startTime),
	}

	// Capture snapshot
	if sess, err := r.sessionFn(); err == nil {
		if info, err := sess.Info(); err == nil && info != nil {
			step.Snapshot = &RecordedSnapshot{URL: info.URL, Title: info.Title}
		}
	}

	r.steps = append(r.steps, step)
}

// Stop stops recording and saves the flow to disk.
func (r *BrowserRecorder) Stop(name, description string) (*RecordedFlow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.recording {
		return nil, fmt.Errorf("not recording")
	}

	r.recording = false

	flow := &RecordedFlow{
		Name:        name,
		Description: description,
		RecordedAt:  r.startTime,
		StartURL:    r.startURL,
		Steps:       r.steps,
	}

	// Save to disk
	if err := os.MkdirAll(r.flowDir, 0755); err != nil {
		return flow, fmt.Errorf("create flow dir: %w", err)
	}

	safeName := sanitizeFlowName(name)
	path := filepath.Join(r.flowDir, safeName+".json")
	data, err := json.MarshalIndent(flow, "", "  ")
	if err != nil {
		return flow, fmt.Errorf("marshal flow: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return flow, fmt.Errorf("write flow: %w", err)
	}

	r.log("recording saved: %s (%d steps)", path, len(r.steps))
	r.steps = nil
	return flow, nil
}

// IsRecording returns true if currently recording.
func (r *BrowserRecorder) IsRecording() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recording
}

// ListFlows returns all saved flows.
func (r *BrowserRecorder) ListFlows() ([]RecordedFlow, error) {
	entries, err := os.ReadDir(r.flowDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var flows []RecordedFlow
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		flow, err := r.loadFlowFile(filepath.Join(r.flowDir, e.Name()))
		if err != nil {
			continue
		}
		flows = append(flows, *flow)
	}
	return flows, nil
}

// LoadFlow loads a specific flow by name.
func (r *BrowserRecorder) LoadFlow(name string) (*RecordedFlow, error) {
	safeName := sanitizeFlowName(name)
	path := filepath.Join(r.flowDir, safeName+".json")
	return r.loadFlowFile(path)
}

func (r *BrowserRecorder) loadFlowFile(path string) (*RecordedFlow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var flow RecordedFlow
	if err := json.Unmarshal(data, &flow); err != nil {
		return nil, err
	}
	return &flow, nil
}

func (r *BrowserRecorder) log(format string, args ...interface{}) {
	if r.logger != nil {
		r.logger(fmt.Sprintf("[recorder] "+format, args...))
	}
}

func sanitizeFlowName(name string) string {
	// Replace unsafe chars with underscore
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
		" ", "_",
	)
	s := replacer.Replace(name)
	if s == "" {
		s = "unnamed"
	}
	return s
}

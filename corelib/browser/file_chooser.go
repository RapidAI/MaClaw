package browser

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// Native file chooser interception (generic, every site). While armed,
// Chrome does not show the OS dialog for any <input type=file> activation —
// script click(), showPicker(), a <label for=…> click or a real user click —
// and instead emits Page.fileChooserOpened with the input's backendNodeId;
// the pending files are set on exactly that input with DOM.setFileInputFiles.

type fileChooserState struct {
	mu      sync.Mutex
	files   []string
	handled chan error
	block   bool // armed without files: swallow the chooser
	blocked int  // choosers swallowed since the last FileChooserBlocked
}

// ErrFileChooserBlocked answers WaitFileChooser for a swallowed chooser.
var ErrFileChooserBlocked = fmt.Errorf("native file chooser blocked")

var fileChoosers sync.Map // *BrowserAgentSession -> *fileChooserState

func (s *BrowserAgentSession) chooserState() *fileChooserState {
	v, _ := fileChoosers.LoadOrStore(s, &fileChooserState{})
	return v.(*fileChooserState)
}

// ArmFileChooser intercepts the next file chooser and answers it with files.
func (s *BrowserAgentSession) ArmFileChooser(files []string) error {
	if err := validateUploadPolicy(s.Policy); err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("missing files")
	}
	client := s.cdpClient()
	if client == nil {
		return fmt.Errorf("no live browser session")
	}
	s.startEventPump()
	st := s.chooserState()
	st.mu.Lock()
	st.files = append([]string(nil), files...)
	st.block = false
	st.handled = make(chan error, 1)
	st.mu.Unlock()
	_, _ = client.Send("Page.enable", nil, 3*time.Second)
	if _, err := client.Send("Page.setInterceptFileChooserDialog", map[string]interface{}{"enabled": true}, DefaultCmdTimeout); err != nil {
		s.DisarmFileChooser()
		return fmt.Errorf("setInterceptFileChooserDialog: %w", err)
	}
	return nil
}

// ArmFileChooserBlock intercepts file choosers without files to give: the
// OS dialog does not open (a server desktop cannot use it) and the page's
// input stays empty. FileChooserBlocked reports whether one was swallowed,
// so the caller can point the model at the upload action instead.
func (s *BrowserAgentSession) ArmFileChooserBlock() error {
	client := s.cdpClient()
	if client == nil {
		return fmt.Errorf("no live browser session")
	}
	s.startEventPump()
	st := s.chooserState()
	st.mu.Lock()
	st.files = nil
	st.block = true
	st.handled = make(chan error, 1)
	st.mu.Unlock()
	_, _ = client.Send("Page.enable", nil, 3*time.Second)
	if _, err := client.Send("Page.setInterceptFileChooserDialog", map[string]interface{}{"enabled": true}, DefaultCmdTimeout); err != nil {
		s.DisarmFileChooser()
		return fmt.Errorf("setInterceptFileChooserDialog: %w", err)
	}
	return nil
}

// FileChooserBlocked returns and clears the count of swallowed choosers.
func (s *BrowserAgentSession) FileChooserBlocked() int {
	st := s.chooserState()
	st.mu.Lock()
	defer st.mu.Unlock()
	n := st.blocked
	st.blocked = 0
	return n
}

// WaitFileChooser reports whether an intercepted chooser was answered.
func (s *BrowserAgentSession) WaitFileChooser(timeout time.Duration) (bool, error) {
	st := s.chooserState()
	st.mu.Lock()
	ch := st.handled
	st.mu.Unlock()
	if ch == nil {
		return false, nil
	}
	select {
	case err := <-ch:
		return true, err
	case <-time.After(timeout):
		return false, nil
	}
}

// DisarmFileChooser restores normal chooser behaviour.
func (s *BrowserAgentSession) DisarmFileChooser() {
	st := s.chooserState()
	st.mu.Lock()
	st.files, st.handled, st.block = nil, nil, false
	st.mu.Unlock()
	if client := s.cdpClient(); client != nil {
		_, _ = client.Send("Page.setInterceptFileChooserDialog", map[string]interface{}{"enabled": false}, 3*time.Second)
	}
}

func parseFileChooserOpened(params json.RawMessage) (int, string) {
	var p struct {
		BackendNodeID int    `json:"backendNodeId"`
		Mode          string `json:"mode"`
	}
	_ = json.Unmarshal(params, &p)
	return p.BackendNodeID, p.Mode
}

func (s *BrowserAgentSession) onFileChooserOpened(params json.RawMessage) {
	st := s.chooserState()
	st.mu.Lock()
	files, ch, block := st.files, st.handled, st.block
	st.files = nil
	if ch != nil && block && len(files) == 0 {
		st.blocked++
	}
	st.mu.Unlock()
	node, mode := parseFileChooserOpened(params)
	if ch != nil && block && len(files) == 0 {
		log.Printf("[browser] native file chooser blocked session=%s node=%d (no files armed)", s.ID, node)
		select {
		case ch <- ErrFileChooserBlocked:
		default:
		}
		return
	}
	if ch == nil || len(files) == 0 {
		return // not ours; the dialog stays suppressed only while armed
	}
	if mode == "selectSingle" && len(files) > 1 {
		files = files[:1]
	}
	client := s.cdpClient()
	go func() {
		var err error
		if node == 0 || client == nil {
			err = fmt.Errorf("file chooser without input node")
		} else {
			_, err = client.Send("DOM.setFileInputFiles", map[string]interface{}{"backendNodeId": node, "files": files}, DefaultCmdTimeout)
		}
		log.Printf("[browser] file chooser intercepted session=%s node=%d files=%d err=%v", s.ID, node, len(files), err)
		select {
		case ch <- err:
		default:
		}
	}()
}

// EvalValue evaluates a page expression on the attached tab and returns its
// value as a string (JSON for objects).
func (s *BrowserAgentSession) EvalValue(expr string) (string, error) {
	client := s.cdpClient()
	if client == nil {
		return "", fmt.Errorf("no live browser session")
	}
	raw, err := client.Send("Runtime.evaluate", map[string]interface{}{"expression": expr, "returnByValue": true, "awaitPromise": true}, DefaultCmdTimeout)
	if err != nil {
		return "", err
	}
	var out struct {
		Result struct {
			Value interface{} `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if str, ok := out.Result.Value.(string); ok {
		return str, nil
	}
	b, _ := json.Marshal(out.Result.Value)
	return string(b), nil
}

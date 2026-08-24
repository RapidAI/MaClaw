package browser

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

func (s *Session) trackNetwork(requestID string, start bool) {
	if s == nil || requestID == "" {
		return
	}
	s.netMu.Lock()
	defer s.netMu.Unlock()
	if s.inflight == nil {
		s.inflight = map[string]struct{}{}
	}
	if start {
		s.inflight[requestID] = struct{}{}
		return
	}
	delete(s.inflight, requestID)
}

func (s *Session) inflightCount() int {
	if s == nil {
		return 0
	}
	s.netMu.Lock()
	defer s.netMu.Unlock()
	return len(s.inflight)
}

func (s *Session) noteAttachedTarget(sessionID, targetID, targetType, url, openerID string, waiting bool) {
	if s == nil || sessionID == "" {
		return
	}
	s.netMu.Lock()
	if s.attached == nil {
		s.attached = map[string]attachedFrame{}
	}
	s.attached[targetID] = attachedFrame{TargetID: targetID, SessionID: sessionID, URL: url, Type: targetType, OpenerID: openerID}
	client := s.client
	s.netMu.Unlock()
	if client == nil {
		return
	}
	go enableAttachedSession(client, sessionID, waiting)
}

func enableAttachedSession(client *CDPClient, sessionID string, waiting bool) {
	if client == nil || sessionID == "" {
		return
	}
	if waiting {
		_, _ = client.SendOn(sessionID, "Runtime.runIfWaitingForDebugger", nil, 3*time.Second)
	}
	_, _ = client.SendOn(sessionID, "Runtime.enable", nil, 3*time.Second)
	_, _ = client.SendOn(sessionID, "Page.enable", nil, 3*time.Second)
	_, _ = client.SendOn(sessionID, "Accessibility.enable", nil, 3*time.Second)
}

func (s *Session) noteDialog(message, dialogType, url string) {
	if s == nil {
		return
	}
	s.netMu.Lock()
	defer s.netMu.Unlock()
	s.pendingDialog = &jsDialogState{Message: message, Type: dialogType, URL: url}
}

func (s *Session) clearDialog() {
	if s == nil {
		return
	}
	s.netMu.Lock()
	defer s.netMu.Unlock()
	s.pendingDialog = nil
}

func (s *Session) hasPendingDialog() bool {
	if s == nil {
		return false
	}
	s.netMu.Lock()
	defer s.netMu.Unlock()
	return s.pendingDialog != nil
}

func (s *Session) pendingDialogCopy() *jsDialogState {
	if s == nil {
		return nil
	}
	s.netMu.Lock()
	defer s.netMu.Unlock()
	if s.pendingDialog == nil {
		return nil
	}
	cp := *s.pendingDialog
	return &cp
}

func (s *Session) frameSessionID(frameID string) string {
	if s == nil || frameID == "" || frameID == "main" {
		return ""
	}
	s.netMu.Lock()
	defer s.netMu.Unlock()
	for _, frame := range s.attached {
		if frame.TargetID == frameID || frame.SessionID == frameID {
			return frame.SessionID
		}
	}
	return ""
}

type frameScope struct {
	Name string
	URL  string
	Path []int
}

func (s *Session) scopeFor(frameID string) (frameScope, bool) {
	if frameID == "" || frameID == "main" {
		return frameScope{}, true
	}
	if s == nil {
		return frameScope{}, false
	}
	frames := s.frameTree()
	for _, frame := range frames {
		if frame.FrameID != frameID {
			continue
		}
		if frame.ParentFrameID == "" {
			return frameScope{}, true
		}
		scope := frameScope{
			Name: frame.Name,
			URL:  frame.URL,
			Path: frameChildPath(frames, frameID),
		}
		if len(scope.Path) == 0 && scope.Name == "" && scope.URL == "" {
			return frameScope{}, false
		}
		return scope, true
	}
	return frameScope{}, false
}

func frameChildPath(frames []BrowserFrameSnapshot, frameID string) []int {
	if frameID == "" || frameID == "main" || len(frames) == 0 {
		return nil
	}
	byID := make(map[string]BrowserFrameSnapshot, len(frames))
	for _, frame := range frames {
		if frame.FrameID != "" {
			byID[frame.FrameID] = frame
		}
	}
	target, ok := byID[frameID]
	if !ok {
		return nil
	}
	var path []int
	for target.ParentFrameID != "" {
		parent, ok := byID[target.ParentFrameID]
		if !ok {
			break
		}
		idx := 0
		found := false
		for _, frame := range frames {
			if frame.ParentFrameID != parent.FrameID {
				continue
			}
			if frame.FrameID == target.FrameID {
				path = append([]int{idx}, path...)
				found = true
				break
			}
			idx++
		}
		if !found {
			return nil
		}
		target = parent
	}
	return path
}

func scopedFindCall(selector string, scope frameScope) string {
	selJSON, _ := json.Marshal(selector)
	nameJSON, _ := json.Marshal(scope.Name)
	urlJSON, _ := json.Marshal(scope.URL)
	path := scope.Path
	if path == nil {
		path = []int{}
	}
	pathJSON, _ := json.Marshal(path)
	return fmt.Sprintf("findScoped(%s, %s, %s, %s)", string(selJSON), nameJSON, urlJSON, string(pathJSON))
}

func scopedLocatedCall(selector string, scope frameScope) string {
	selJSON, _ := json.Marshal(selector)
	nameJSON, _ := json.Marshal(scope.Name)
	urlJSON, _ := json.Marshal(scope.URL)
	path := scope.Path
	if path == nil {
		path = []int{}
	}
	pathJSON, _ := json.Marshal(path)
	return fmt.Sprintf("findScopedLocated(%s, %s, %s, %s)", string(selJSON), nameJSON, urlJSON, string(pathJSON))
}

func errFrameGone() error {
	return fmt.Errorf("browser frame is gone; run observe again")
}

func isFrameGoneErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "frame is gone")
}

func (s *Session) EvalOn(frameID, expression string) (string, error) {
	if s == nil {
		return "", fmt.Errorf("browser session not connected")
	}
	sessionID := s.frameSessionID(frameID)
	if sessionID == "" {
		return s.Eval(expression)
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return "", fmt.Errorf("browser session not connected")
	}
	result, err := client.SendOn(sessionID, "Runtime.evaluate", map[string]interface{}{
		"expression":    expression,
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return "", err
	}
	return extractStringValue(result), nil
}

func (s *Session) frameTree() []BrowserFrameSnapshot {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return nil
	}
	result, err := client.Send("Page.getFrameTree", nil, 5*time.Second)
	if err != nil {
		return nil
	}
	var payload struct {
		FrameTree cdpFrameTree `json:"frameTree"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil
	}
	return flattenFrameTree(payload.FrameTree, "")
}

type cdpFrameTree struct {
	Frame       cdpFrame       `json:"frame"`
	ChildFrames []cdpFrameTree `json:"childFrames"`
}

type cdpFrame struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	Name     string `json:"name"`
	URL      string `json:"url"`
}

func flattenFrameTree(tree cdpFrameTree, parent string) []BrowserFrameSnapshot {
	frameID := tree.Frame.ID
	if frameID == "" {
		return nil
	}
	parentID := tree.Frame.ParentID
	if parentID == "" {
		parentID = parent
	}
	out := []BrowserFrameSnapshot{{
		FrameID:       frameID,
		ParentFrameID: parentID,
		URL:           tree.Frame.URL,
		Name:          tree.Frame.Name,
	}}
	for _, child := range tree.ChildFrames {
		out = append(out, flattenFrameTree(child, frameID)...)
	}
	return out
}

func (s *Session) axInteractiveRefs() []BrowserElementRef {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return nil
	}
	result, err := client.Send("Accessibility.getFullAXTree", nil, 5*time.Second)
	if err != nil {
		return nil
	}
	var payload struct {
		Nodes []struct {
			Ignored          bool `json:"ignored"`
			BackendDOMNodeID int  `json:"backendDOMNodeId"`
			Role             struct {
				Value string `json:"value"`
			} `json:"role"`
			Name struct {
				Value string `json:"value"`
			} `json:"name"`
			Properties []struct {
				Name  string `json:"name"`
				Value struct {
					Value interface{} `json:"value"`
				} `json:"value"`
			} `json:"properties"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil
	}
	interactive := map[string]bool{
		"button": true, "link": true, "textbox": true, "searchbox": true,
		"combobox": true, "checkbox": true, "radio": true, "menuitem": true,
		"tab": true, "switch": true, "slider": true, "option": true,
	}
	out := []BrowserElementRef{}
	for _, node := range payload.Nodes {
		if node.Ignored || node.BackendDOMNodeID == 0 {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(node.Role.Value))
		if !interactive[role] {
			continue
		}
		name := strings.TrimSpace(node.Name.Value)
		disabled := false
		for _, prop := range node.Properties {
			if strings.EqualFold(prop.Name, "disabled") {
				if b, ok := prop.Value.Value.(bool); ok {
					disabled = b
				}
			}
		}
		out = append(out, BrowserElementRef{
			FrameID:       "main",
			Role:          role,
			Name:          name,
			Text:          name,
			Tag:           role,
			Disabled:      disabled,
			Visible:       true,
			BackendNodeID: node.BackendDOMNodeID,
			StableKey:     role + "|" + name,
		})
	}
	return out
}

func observableAttachedFrame(frame attachedFrame, activeTabID string) bool {
	if frame.Type != "iframe" || strings.TrimSpace(frame.SessionID) == "" {
		return false
	}
	if activeTabID != "" && frame.TargetID == activeTabID {
		return false
	}
	return true
}

func (s *Session) observeAttachedFrames(startIdx int) []BrowserElementRef {
	if s == nil {
		return nil
	}
	s.netMu.Lock()
	attached := make([]attachedFrame, 0, len(s.attached))
	for _, frame := range s.attached {
		if observableAttachedFrame(frame, s.activeTabID) {
			attached = append(attached, frame)
		}
	}
	s.netMu.Unlock()
	var out []BrowserElementRef
	idx := startIdx
	for _, frame := range attached {
		if frame.SessionID == "" {
			continue
		}
		raw, err := s.EvalOn(frame.TargetID, browserObserveScript)
		if err != nil || strings.TrimSpace(raw) == "" {
			continue
		}
		var payload observePayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			continue
		}
		for _, ref := range payload.Refs {
			ref.FrameID = frame.TargetID
			ref.Ref = fmt.Sprintf("@e%d", idx)
			idx++
			out = append(out, ref)
		}
	}
	return out
}

func (s *Session) visionExcerptOnce() string {
	ocr := getObserveOCR()
	if s == nil || ocr == nil || !ocr.IsAvailable() {
		return ""
	}
	s.netMu.Lock()
	if s.visionOnce {
		s.netMu.Unlock()
		return ""
	}
	s.visionOnce = true
	s.netMu.Unlock()
	png, err := s.Screenshot(false)
	if err != nil || png == "" {
		s.netMu.Lock()
		s.visionOnce = false
		s.netMu.Unlock()
		return ""
	}
	results, err := ocr.Recognize(png)
	if err != nil || len(results) == 0 {
		return ""
	}
	parts := make([]string, 0, len(results))
	for i, item := range results {
		if i >= 12 {
			break
		}
		text := strings.TrimSpace(item.Text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func (s *Session) ClickInFrame(frameID, selector string) error {
	if sessionID := s.frameSessionID(frameID); sessionID != "" {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.clickAtLockedOn(sessionID, selector, frameScope{})
	}
	scope, ok := s.scopeFor(frameID)
	if !ok {
		return errFrameGone()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clickAtLockedIn(selector, scope)
}

func (s *Session) jsClickOnSessionLocked(sessionID, selector string) error {
	js := fmt.Sprintf(`
		(function() {
			%s
			const el = findInFrames(document, %q);
			if (!el) return JSON.stringify({error: "element not found"});
			el.click();
			return JSON.stringify({ok: true});
		})()
	`, pierceFindJS, selector)
	result, err := s.client.SendOn(sessionID, "Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return err
	}
	str := extractStringValue(result)
	var r struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(str), &r) == nil && r.Error != "" {
		return fmt.Errorf("%s", r.Error)
	}
	return nil
}

func (s *Session) clickBackendNode(nodeID int) error {
	return s.clickBackendNodeOn("", nodeID)
}

func (s *Session) clickBackendNodeOn(sessionID string, nodeID int) error {
	if s == nil || s.client == nil || nodeID == 0 {
		return fmt.Errorf("missing backend node")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	send := func(method string, params map[string]interface{}) (json.RawMessage, error) {
		if sessionID == "" {
			return s.client.Send(method, params, DefaultCmdTimeout)
		}
		return s.client.SendOn(sessionID, method, params, DefaultCmdTimeout)
	}
	resolved, err := send("DOM.resolveNode", map[string]interface{}{"backendNodeId": nodeID})
	if err != nil {
		return fmt.Errorf("resolve backend node: %w", err)
	}
	var payload struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if err := json.Unmarshal(resolved, &payload); err != nil || payload.Object.ObjectID == "" {
		return fmt.Errorf("backend node not found")
	}
	_, err = send("Runtime.callFunctionOn", map[string]interface{}{
		"objectId":            payload.Object.ObjectID,
		"functionDeclaration": `function() { this.scrollIntoView({block:"center", behavior:"instant"}); this.click(); }`,
	})
	return err
}

func (s *Session) hoverBackendNodeOn(sessionID string, nodeID int) error {
	if s == nil || s.client == nil || nodeID == 0 {
		return fmt.Errorf("missing backend node")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	send := func(method string, params map[string]interface{}) (json.RawMessage, error) {
		if sessionID == "" {
			return s.client.Send(method, params, DefaultCmdTimeout)
		}
		return s.client.SendOn(sessionID, method, params, DefaultCmdTimeout)
	}
	resolved, err := send("DOM.resolveNode", map[string]interface{}{"backendNodeId": nodeID})
	if err != nil {
		return fmt.Errorf("resolve backend node: %w", err)
	}
	var payload struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if err := json.Unmarshal(resolved, &payload); err != nil || payload.Object.ObjectID == "" {
		return fmt.Errorf("backend node not found")
	}
	_, err = send("Runtime.callFunctionOn", map[string]interface{}{
		"objectId": payload.Object.ObjectID,
		"functionDeclaration": `function() {
			this.scrollIntoView({block:"center", behavior:"instant"});
			const opts = {bubbles: true, cancelable: true, view: window};
			this.dispatchEvent(new MouseEvent("pointerover", opts));
			this.dispatchEvent(new MouseEvent("mouseover", opts));
			this.dispatchEvent(new MouseEvent("mouseenter", {bubbles: false, cancelable: true, view: window}));
			this.dispatchEvent(new MouseEvent("mousemove", opts));
		}`,
	})
	return err
}

func (s *Session) typeBackendNodeOn(sessionID string, nodeID int, text, contentFormat string, appendText bool) error {
	if s == nil || s.client == nil || nodeID == 0 {
		return fmt.Errorf("missing backend node")
	}
	textJSON, _ := json.Marshal(text)
	htmlJSON := textJSON
	if normalizeBrowserContentFormat(contentFormat) == BrowserContentFormatMarkdown {
		if rich := browserMarkdownToHTML(text); strings.TrimSpace(rich) != "" {
			htmlJSON, _ = json.Marshal(rich)
		}
	}
	clearJS := "true"
	if appendText {
		clearJS = "false"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	send := func(method string, params map[string]interface{}) (json.RawMessage, error) {
		if sessionID == "" {
			return s.client.Send(method, params, DefaultCmdTimeout)
		}
		return s.client.SendOn(sessionID, method, params, DefaultCmdTimeout)
	}
	resolved, err := send("DOM.resolveNode", map[string]interface{}{"backendNodeId": nodeID})
	if err != nil {
		return fmt.Errorf("resolve backend node: %w", err)
	}
	var payload struct {
		Object struct {
			ObjectID string `json:"objectId"`
		} `json:"object"`
	}
	if err := json.Unmarshal(resolved, &payload); err != nil || payload.Object.ObjectID == "" {
		return fmt.Errorf("backend node not found")
	}
	decl := fmt.Sprintf(`function() {
		this.scrollIntoView({block:"center", behavior:"instant"});
		this.focus();
		const text = %s;
		const html = %s;
		const clear = %s;
		if (this.tagName === "INPUT" || this.tagName === "TEXTAREA") {
			if (clear) this.value = "";
			this.value = (this.value || "") + text;
		} else if (this.isContentEditable) {
			if (clear) { this.textContent = ""; this.innerHTML = ""; }
			if (html !== text) this.innerHTML = (this.innerHTML || "") + html;
			else this.textContent = (this.textContent || "") + text;
		} else {
			return JSON.stringify({error: "element is not editable"});
		}
		this.dispatchEvent(new Event("input", {bubbles: true}));
		this.dispatchEvent(new Event("change", {bubbles: true}));
		return JSON.stringify({ok: true});
	}`, string(textJSON), string(htmlJSON), clearJS)
	result, err := send("Runtime.callFunctionOn", map[string]interface{}{
		"objectId":            payload.Object.ObjectID,
		"functionDeclaration": decl,
		"returnByValue":       true,
	})
	if err != nil {
		return err
	}
	str := extractStringValue(result)
	var r struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(str), &r) == nil && r.Error != "" {
		return fmt.Errorf("%s", r.Error)
	}
	return nil
}

func (s *Session) TypeInFrame(frameID, selector, text, contentFormat string, appendText bool) error {
	if sessionID := s.frameSessionID(frameID); sessionID == "" {
		scope, ok := s.scopeFor(frameID)
		if !ok {
			return errFrameGone()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.typeContentLockedIn(selector, scope, text, contentFormat, appendText)
	}
	sessionID := s.frameSessionID(frameID)
	s.mu.Lock()
	defer s.mu.Unlock()
	prep := focusEditableJSIn(selector, frameScope{})
	if !appendText {
		prep = prepareEditableJSIn(selector, frameScope{})
	}
	if err := s.evalCheckOnLocked(sessionID, prep); err != nil {
		return err
	}
	if normalizeBrowserContentFormat(contentFormat) == BrowserContentFormatMarkdown {
		return s.insertMarkdownOnLocked(sessionID, text)
	}
	if _, err := s.client.SendOn(sessionID, "Input.insertText", map[string]interface{}{"text": text}, DefaultCmdTimeout); err == nil {
		if s.activeElementContainsTextOnLocked(sessionID, text) {
			return nil
		}
		log.Printf("[browser] OOPIF Input.insertText did not land (len=%d), trying JS fallback", len(text))
	}
	return s.evalCheckOnLocked(sessionID, insertTextViaJS(text))
}

func (s *Session) Hover(selector string) error {
	return s.HoverInFrame("", selector)
}

func (s *Session) HoverInFrame(frameID, selector string) error {
	if frameID != "" && frameID != "main" {
		if sessionID := s.frameSessionID(frameID); sessionID != "" {
			s.mu.Lock()
			defer s.mu.Unlock()
			coord, err := s.evalCoordsLockedOn(sessionID, selector, frameScope{})
			if err != nil {
				return s.jsHoverOnSessionLocked(sessionID, selector)
			}
			if _, err = s.client.SendOn(sessionID, "Input.dispatchMouseEvent", map[string]interface{}{
				"type": "mouseMoved", "x": coord.X, "y": coord.Y,
			}, DefaultCmdTimeout); err != nil {
				return s.jsHoverOnSessionLocked(sessionID, selector)
			}
			return nil
		}
	}
	scope, ok := s.scopeFor(frameID)
	if !ok {
		return errFrameGone()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	coord, err := s.evalCoordsLockedIn(selector, scope)
	if err != nil {
		return s.jsHoverLockedIn(selector, scope)
	}
	if _, err = s.client.Send("Input.dispatchMouseEvent", map[string]interface{}{
		"type": "mouseMoved", "x": coord.X, "y": coord.Y,
	}, DefaultCmdTimeout); err != nil {
		return s.jsHoverLockedIn(selector, scope)
	}
	return nil
}

func (s *Session) jsHoverLockedIn(selector string, scope frameScope) error {
	js := fmt.Sprintf(`
		(function() {
			%s
			const el = %s;
			if (!el) return JSON.stringify({error: "element not found"});
			el.scrollIntoView({block: "center", behavior: "instant"});
			const opts = {bubbles: true, cancelable: true, view: window};
			el.dispatchEvent(new MouseEvent("pointerover", opts));
			el.dispatchEvent(new MouseEvent("mouseover", opts));
			el.dispatchEvent(new MouseEvent("mouseenter", {bubbles: false, cancelable: true, view: window}));
			el.dispatchEvent(new MouseEvent("mousemove", opts));
			return JSON.stringify({ok: true});
		})()
	`, pierceFindJS, scopedFindCall(selector, scope))
	return s.evalCheck(js)
}

func (s *Session) jsHoverOnSessionLocked(sessionID, selector string) error {
	selJSON, _ := json.Marshal(selector)
	js := fmt.Sprintf(`
		(function() {
			%s
			const el = findInFrames(document, %s);
			if (!el) return JSON.stringify({error: "element not found"});
			el.scrollIntoView({block: "center", behavior: "instant"});
			const opts = {bubbles: true, cancelable: true, view: window};
			el.dispatchEvent(new MouseEvent("pointerover", opts));
			el.dispatchEvent(new MouseEvent("mouseover", opts));
			el.dispatchEvent(new MouseEvent("mouseenter", {bubbles: false, cancelable: true, view: window}));
			el.dispatchEvent(new MouseEvent("mousemove", opts));
			return JSON.stringify({ok: true});
		})()
	`, pierceFindJS, string(selJSON))
	result, err := s.client.SendOn(sessionID, "Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return err
	}
	str := extractStringValue(result)
	var r struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(str), &r) == nil && r.Error != "" {
		return fmt.Errorf("%s", r.Error)
	}
	return nil
}

func (s *Session) evalCheckOnLocked(sessionID, js string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("browser session not connected")
	}
	var (
		result json.RawMessage
		err    error
	)
	if sessionID == "" {
		result, err = s.client.Send("Runtime.evaluate", map[string]interface{}{
			"expression": js, "returnByValue": true,
		}, DefaultCmdTimeout)
	} else {
		result, err = s.client.SendOn(sessionID, "Runtime.evaluate", map[string]interface{}{
			"expression": js, "returnByValue": true,
		}, DefaultCmdTimeout)
	}
	if err != nil {
		return err
	}
	str := extractStringValue(result)
	var r struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(str), &r) == nil && r.Error != "" {
		return fmt.Errorf("%s", r.Error)
	}
	return nil
}

func (s *Session) SelectInFrame(frameID, selector, value string) error {
	sessionID := s.frameSessionID(frameID)
	if sessionID != "" {
		js := selectOptionJS(selector, value)
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.evalCheckOnLocked(sessionID, js)
	}
	scope, ok := s.scopeFor(frameID)
	if !ok {
		return errFrameGone()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.evalCheck(selectOptionJSIn(selector, value, scope))
}

func (s *Session) GetTextInFrame(frameID, selector string) (string, error) {
	if s == nil || s.client == nil {
		return "", fmt.Errorf("browser session not connected")
	}
	sessionID := s.frameSessionID(frameID)
	scope := frameScope{}
	if sessionID == "" {
		var ok bool
		scope, ok = s.scopeFor(frameID)
		if !ok {
			return "", errFrameGone()
		}
	}
	js := getTextJSIn(selector, scope)
	s.mu.Lock()
	defer s.mu.Unlock()
	if sessionID == "" {
		return s.evalString(js, "text")
	}
	result, err := s.client.SendOn(sessionID, "Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
	}, DefaultCmdTimeout)
	if err != nil {
		return "", err
	}
	str := extractStringValue(result)
	var r map[string]interface{}
	if err := json.Unmarshal([]byte(str), &r); err != nil {
		return str, nil
	}
	if e, ok := r["error"].(string); ok && e != "" {
		return "", fmt.Errorf("%s", e)
	}
	if v, ok := r["text"].(string); ok {
		return v, nil
	}
	return str, nil
}

func (s *Session) ScrollElementInFrame(frameID, selector string, deltaX, deltaY int) error {
	if strings.TrimSpace(selector) == "" {
		return s.Scroll(deltaX, deltaY)
	}
	sessionID := s.frameSessionID(frameID)
	scope := frameScope{}
	if sessionID == "" {
		var ok bool
		scope, ok = s.scopeFor(frameID)
		if !ok {
			return errFrameGone()
		}
	}
	js := fmt.Sprintf(`
		(function() {
			%s
			const el = %s;
			if (!el) return JSON.stringify({error: "element not found"});
			el.scrollIntoView({block: "center", behavior: "instant"});
			window.scrollBy(%d, %d);
			return JSON.stringify({ok: true});
		})()
	`, pierceFindJS, scopedFindCall(selector, scope), deltaX, deltaY)
	s.mu.Lock()
	defer s.mu.Unlock()
	if sessionID == "" {
		return s.evalCheck(js)
	}
	return s.evalCheckOnLocked(sessionID, js)
}

func (s *Session) SetFilesInFrame(frameID, selector string, files []string) error {
	sessionID := s.frameSessionID(frameID)
	if sessionID == "" {
		scope, ok := s.scopeFor(frameID)
		if !ok {
			return errFrameGone()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.setFilesLocked(selector, scope, files)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	send := func(method string, params map[string]interface{}) (json.RawMessage, error) {
		return s.client.SendOn(sessionID, method, params, DefaultCmdTimeout)
	}
	if _, err := send("DOM.enable", nil); err != nil {
		return err
	}
	selJSON, _ := json.Marshal(selector)
	js := fmt.Sprintf(`(function(){ %s return findInFrames(document, %s) || null; })()`, pierceFindJS, string(selJSON))
	result, err := send("Runtime.evaluate", map[string]interface{}{
		"expression":    js,
		"returnByValue": false,
	})
	if err != nil {
		return err
	}
	objectID := extractObjectID(result)
	if objectID == "" {
		return fmt.Errorf("element not found")
	}
	desc, err := send("DOM.describeNode", map[string]interface{}{"objectId": objectID})
	if err != nil {
		return err
	}
	var node struct {
		Node struct {
			BackendNodeID int `json:"backendNodeId"`
		} `json:"node"`
	}
	if json.Unmarshal(desc, &node) != nil || node.Node.BackendNodeID == 0 {
		return fmt.Errorf("element not found")
	}
	if _, err := send("DOM.setFileInputFiles", map[string]interface{}{
		"backendNodeId": node.Node.BackendNodeID,
		"files":         files,
	}); err != nil {
		return fmt.Errorf("setFileInputFiles: %w", err)
	}
	return nil
}

func (s *Session) Press(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("missing key")
	}
	spec := keyEventSpec(key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.client.Send("Input.dispatchKeyEvent", map[string]interface{}{
		"type":                  "keyDown",
		"key":                   spec.key,
		"code":                  spec.code,
		"windowsVirtualKeyCode": spec.vk,
	}, DefaultCmdTimeout); err != nil {
		return err
	}
	_, err := s.client.Send("Input.dispatchKeyEvent", map[string]interface{}{
		"type":                  "keyUp",
		"key":                   spec.key,
		"code":                  spec.code,
		"windowsVirtualKeyCode": spec.vk,
	}, DefaultCmdTimeout)
	return err
}

type keySpec struct {
	key  string
	code string
	vk   int
}

func keyEventSpec(raw string) keySpec {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "enter", "return":
		return keySpec{key: "Enter", code: "Enter", vk: 13}
	case "escape", "esc":
		return keySpec{key: "Escape", code: "Escape", vk: 27}
	case "tab":
		return keySpec{key: "Tab", code: "Tab", vk: 9}
	case "backspace":
		return keySpec{key: "Backspace", code: "Backspace", vk: 8}
	case "arrowdown", "down":
		return keySpec{key: "ArrowDown", code: "ArrowDown", vk: 40}
	case "arrowup", "up":
		return keySpec{key: "ArrowUp", code: "ArrowUp", vk: 38}
	case "arrowleft", "left":
		return keySpec{key: "ArrowLeft", code: "ArrowLeft", vk: 37}
	case "arrowright", "right":
		return keySpec{key: "ArrowRight", code: "ArrowRight", vk: 39}
	case "space", " ":
		return keySpec{key: " ", code: "Space", vk: 32}
	default:
		if len([]rune(raw)) == 1 {
			ch := []rune(raw)[0]
			return keySpec{key: string(ch), code: "Key" + strings.ToUpper(string(ch)), vk: int(ch)}
		}
		return keySpec{key: raw, code: raw, vk: 0}
	}
}

func (s *Session) HandleDialog(accept bool, promptText string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("browser session not connected")
	}
	if !s.hasPendingDialog() {
		return fmt.Errorf("no JavaScript dialog is open")
	}
	params := map[string]interface{}{"accept": accept}
	if strings.TrimSpace(promptText) != "" {
		params["promptText"] = promptText
	}
	_, err := s.client.Send("Page.handleJavaScriptDialog", params, DefaultCmdTimeout)
	if err == nil {
		s.clearDialog()
	}
	return err
}

func (s *Session) evalCoordsLocked(selector string) (*clickCoordResult, error) {
	return s.evalCoordsLockedIn(selector, frameScope{})
}

func (s *Session) evalCoordsLockedIn(selector string, scope frameScope) (*clickCoordResult, error) {
	return s.evalCoordsLockedOn("", selector, scope)
}

func (s *Session) evalCoordsLockedOn(sessionID, selector string, scope frameScope) (*clickCoordResult, error) {
	js := fmt.Sprintf(`
		(async function() {
			%s
			const found = %s;
			if (!found) return JSON.stringify({error: "element not found"});
			const el = found.el;
			el.scrollIntoView({block: "center", behavior: "instant"});
			if (document.visibilityState !== "hidden") {
				await new Promise(function(resolve) {
					let done = false;
					const finish = function() { if (!done) { done = true; resolve(); } };
					requestAnimationFrame(function() { requestAnimationFrame(finish); });
					setTimeout(finish, 250);
				});
			}
			const rect = el.getBoundingClientRect();
			let x = rect.x + rect.width / 2;
			let y = rect.y + rect.height / 2;
			for (const frame of found.chain) {
				const fr = frame.getBoundingClientRect();
				x += fr.x;
				y += fr.y;
			}
			return JSON.stringify({x: x, y: y, tag: el.tagName, vis: document.visibilityState});
		})()
	`, pierceFindJS, scopedLocatedCall(selector, scope))
	var (
		result json.RawMessage
		err    error
	)
	params := map[string]interface{}{
		"expression":    js,
		"returnByValue": true,
		"awaitPromise":  true,
	}
	if sessionID == "" {
		result, err = s.client.Send("Runtime.evaluate", params, DefaultCmdTimeout)
	} else {
		result, err = s.client.SendOn(sessionID, "Runtime.evaluate", params, DefaultCmdTimeout)
	}
	if err != nil {
		return nil, err
	}
	str := extractStringValue(result)
	var coord clickCoordResult
	if err := json.Unmarshal([]byte(str), &coord); err != nil {
		return nil, fmt.Errorf("parse coordinates: %w", err)
	}
	if coord.Error != "" {
		return nil, fmt.Errorf("%s", coord.Error)
	}
	return &coord, nil
}

func (s *Session) isPopupTarget(targetID string) bool {
	if s == nil || targetID == "" {
		return false
	}
	s.netMu.Lock()
	defer s.netMu.Unlock()
	frame, ok := s.attached[targetID]
	return ok && frame.OpenerID != ""
}

func (s *Session) notePopupTarget(targetID, openerID, targetType, url string) {
	if s == nil || targetID == "" || openerID == "" {
		return
	}
	s.netMu.Lock()
	defer s.netMu.Unlock()
	if s.attached == nil {
		s.attached = map[string]attachedFrame{}
	}
	prev := s.attached[targetID]
	prev.TargetID = targetID
	prev.OpenerID = openerID
	if targetType != "" {
		prev.Type = targetType
	}
	if url != "" {
		prev.URL = url
	}
	s.attached[targetID] = prev
}

func (s *Session) ScrollElement(selector string, deltaX, deltaY int) error {
	if strings.TrimSpace(selector) == "" {
		return s.Scroll(deltaX, deltaY)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	js := fmt.Sprintf(`
		(function() {
			%s
			const el = %s;
			if (!el) return JSON.stringify({error: "element not found"});
			el.scrollIntoView({block: "center", behavior: "instant"});
			if (%d !== 0 || %d !== 0) window.scrollBy(%d, %d);
			return JSON.stringify({ok: true});
		})()
	`, pierceFindJS, scopedFindCall(selector, frameScope{}), deltaX, deltaY, deltaX, deltaY)
	return s.evalCheck(js)
}

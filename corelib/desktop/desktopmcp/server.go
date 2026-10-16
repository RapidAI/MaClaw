// Package desktopmcp is the MCP (Model Context Protocol) server for a MaClaw
// cloud desktop. Hub mounts it at /mcp; cmd/maclaw-desktop-mcp runs it over
// stdio for local use.
package desktopmcp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktop/computeruse"
)

const serverName = "maclaw-desktop"
const serverVersion = "1.0.0"

var supportedProtocols = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// Server answers MCP JSON-RPC for one backend. HTTP clients authenticate
// with a bearer token that maps to an Identity; stdio uses one Identity.
type Server struct {
	backend  Backend
	auth     Authenticator
	takeover string   // "viewer" refuses control while someone watches in noVNC; "off" does not check
	frames   sync.Map // tenant/user -> *frameState
	settle   time.Duration
}

type frameState struct {
	mu   sync.Mutex
	shot desktop.Shot
	opts desktop.ShotOptions
}

func (s *Server) frame(id Identity) *frameState {
	value, _ := s.frames.LoadOrStore(id.TenantID+"\x00"+id.UserID, &frameState{shot: desktop.Shot{Scale: 1}})
	return value.(*frameState)
}

func (f *frameState) get() (desktop.Shot, desktop.ShotOptions) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shot, f.opts
}

func (f *frameState) set(shot desktop.Shot, opts desktop.ShotOptions) {
	shot.Data = nil
	opts.Region, opts.Marks = desktop.ShotRegion{}, false
	f.mu.Lock()
	f.shot, f.opts = shot, opts
	f.mu.Unlock()
}

// HashToken is how tokens are stored in the tokens file.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Authenticator maps an HTTP request (its bearer token) to an Identity.
type Authenticator func(r *http.Request) (Identity, bool)

// Options configure a Server.
type Options struct {
	// Auth authenticates HTTP clients. Without it every HTTP request is 401.
	Auth Authenticator
	// Takeover is "viewer" (default: refuse control while a person has the
	// desktop open in noVNC) or "off".
	Takeover string
	// Settle is the pause after input before the follow-up screenshot.
	Settle time.Duration
}

// New returns a server for backend.
func New(backend Backend, opts Options) *Server {
	if opts.Takeover != "off" {
		opts.Takeover = "viewer"
	}
	if opts.Settle <= 0 {
		opts.Settle = 500 * time.Millisecond
	}
	return &Server{backend: backend, auth: opts.Auth, takeover: opts.Takeover, settle: opts.Settle}
}

// BearerToken returns the request's bearer token, or "".
func BearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) < 7 || !strings.EqualFold(auth[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(auth[7:])
}

// StaticTokens authenticates against identities keyed by HashToken(token).
func StaticTokens(tokens map[string]Identity) Authenticator {
	return func(r *http.Request) (Identity, bool) {
		token := BearerToken(r)
		if token == "" {
			return Identity{}, false
		}
		hash := HashToken(token)
		for known, id := range tokens {
			if subtle.ConstantTimeCompare([]byte(known), []byte(hash)) == 1 {
				return id, true
			}
		}
		return Identity{}, false
	}
}

func (s *Server) identityFor(r *http.Request) (Identity, bool) {
	if s.auth == nil {
		return Identity{}, false
	}
	return s.auth(r)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// ServeHTTP is the Streamable HTTP transport: POST one JSON-RPC message (or
// a batch) and get application/json back. No server-initiated stream is
// offered, so GET answers 405 as the specification allows.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodDelete {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, ok := s.identityFor(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="maclaw-desktop-mcp"`)
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	out, hasReply := s.handleRaw(r.Context(), id, body)
	if !hasReply {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if session := r.Header.Get("Mcp-Session-Id"); session != "" {
		w.Header().Set("Mcp-Session-Id", session)
	}
	_, _ = w.Write(out)
}

// ServeStdio reads newline-delimited JSON-RPC from in and writes replies.
func (s *Server) ServeStdio(ctx context.Context, id Identity, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	var mu sync.Mutex
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		reply, ok := s.handleRaw(ctx, id, []byte(line))
		if !ok {
			continue
		}
		mu.Lock()
		_, err := out.Write(append(reply, '\n'))
		mu.Unlock()
		if err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *Server) handleRaw(ctx context.Context, id Identity, body []byte) ([]byte, bool) {
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		var batch []rpcRequest
		if err := json.Unmarshal(body, &batch); err != nil {
			raw, _ := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
			return raw, true
		}
		var replies []rpcResponse
		for _, req := range batch {
			if reply, ok := s.handle(ctx, id, req); ok {
				replies = append(replies, reply)
			}
		}
		if len(replies) == 0 {
			return nil, false
		}
		raw, _ := json.Marshal(replies)
		return raw, true
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		raw, _ := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error"}})
		return raw, true
	}
	reply, ok := s.handle(ctx, id, req)
	if !ok {
		return nil, false
	}
	raw, _ := json.Marshal(reply)
	return raw, true
}

func (s *Server) handle(ctx context.Context, id Identity, req rpcRequest) (rpcResponse, bool) {
	notification := len(req.ID) == 0 || string(req.ID) == "null"
	reply := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		version := supportedProtocols[0]
		for _, v := range supportedProtocols {
			if v == params.ProtocolVersion {
				version = v
			}
		}
		reply.Result = map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": serverName, "version": serverVersion},
			"instructions": "Tools for one MaClaw cloud desktop (Linux, XFCE, 1280x800 by default). Take desktop_screenshot first; coordinates in desktop_computer are pixels of the last full screenshot (scaled screenshots are converted back). " +
				"For non-browser apps desktop_app_probe lists accessible elements with ids that desktop_computer steps can target ({\"action\":\"click\",\"id\":N}). For web pages prefer the browser's CDP endpoint from desktop_browser_info.",
		}
	case "notifications/initialized", "notifications/cancelled":
		return reply, false
	case "ping":
		reply.Result = map[string]any{}
	case "tools/list":
		reply.Result = map[string]any{"tools": s.toolList(id)}
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			reply.Error = &rpcError{-32602, "invalid params"}
			break
		}
		if params.Arguments == nil {
			params.Arguments = map[string]any{}
		}
		tool, ok := toolByName(params.Name)
		if !ok {
			reply.Error = &rpcError{-32602, "unknown tool " + params.Name}
			break
		}
		reply.Result = s.call(ctx, id, tool, params.Arguments)
	default:
		if notification {
			return reply, false
		}
		reply.Error = &rpcError{-32601, "method not found: " + req.Method}
	}
	if notification {
		return reply, false
	}
	return reply, true
}

// tool describes one MCP tool and what it needs.
type tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Control     bool
	Needs       string // "", "bash", "install"
	ReadOnly    bool
}

func obj(props map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func prop(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

var shotProps = map[string]any{
	"max_width":  prop("integer", "Shrink the image to at most this width (keeps aspect). Later coordinates are pixels of this smaller image."),
	"max_height": prop("integer", "Shrink the image to at most this height."),
	"format":     map[string]any{"type": "string", "enum": []string{"png", "jpeg", "webp"}, "description": "Image format (default png)."},
	"quality":    prop("integer", "jpeg/webp quality 1-100 (default 80)."),
	"cursor":     prop("boolean", "Draw the mouse pointer."),
	"marks":      prop("boolean", "Draw the numbered boxes of the last desktop_app_probe."),
	"region":     map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "minItems": 4, "maxItems": 4, "description": "Zoom: [x0,y0,x1,y1] in pixels of the last full screenshot. A zoom does not change the coordinate frame."},
}

var tools = []tool{
	{Name: "desktop_screenshot", ReadOnly: true, Description: "Capture the desktop. Returns the image and its geometry (screen size, image size, scale). Coordinates you pass to other tools are pixels of the last full (non-zoom) screenshot.", Schema: obj(shotProps)},
	{Name: "desktop_computer", Control: true, Description: "Run mouse/keyboard actions in order; stops at the first failure and reports every step. Each action is Anthropic computer-use style ({action: left_click|right_click|middle_click|double_click|triple_click|mouse_move|left_click_drag|left_mouse_down|left_mouse_up|scroll|key|hold_key|type|cursor_position|wait|screenshot|zoom, coordinate:[x,y], start_coordinate, text, scroll_direction, scroll_amount, duration, region}), OpenAI computer-use style ({type: click|double_click|scroll|type|keypress|wait|screenshot|move|drag, x, y, button, scroll_x, scroll_y, keys, path}), or MaClaw style ({action: click|focus|set_text|paste, id, x, y, text}). id targets an element of the last desktop_app_probe. A screenshot is returned after input.",
		Schema: obj(map[string]any{"actions": map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "minItems": 1, "maxItems": computeruse.MaxSteps, "description": "Actions to run in order."}}, "actions")},
	{Name: "desktop_app_probe", ReadOnly: true, Description: "List the accessible UI elements (AT-SPI: role, name, states, bounds) of the focused window, a window whose title contains `window`, or all windows. Interactive elements first; ids are valid until the next probe. The browser is not included (use CDP).",
		Schema: obj(map[string]any{"window": prop("string", "focused (default), all, or part of a window title."), "max_nodes": prop("integer", "Most elements (default 120, max 400)."), "max_depth": prop("integer", "Deepest level (default 25)."), "interactive_only": prop("boolean", "Only buttons, menus, inputs, links, list items."), "marks": prop("boolean", "Also return a screenshot with the numbered boxes.")})},
	{Name: "desktop_clipboard_get", ReadOnly: true, Description: "Read the desktop clipboard (UTF-8 text, or a PNG image with kind=image).", Schema: obj(map[string]any{"kind": map[string]any{"type": "string", "enum": []string{"text", "image"}}})},
	{Name: "desktop_clipboard_set", Control: true, Description: "Put UTF-8 text (any language) on the desktop clipboard. Follow with a ctrl+v key (ctrl+shift+v in a terminal), or use a desktop_computer paste action, to insert long or Chinese text without the input method.", Schema: obj(map[string]any{"text": prop("string", "Text to copy.")}, "text")},
	{Name: "desktop_windows", ReadOnly: true, Description: "List top-level windows: id, title, class, pid, geometry (screen pixels), desktop, active/maximized/minimized.", Schema: obj(map[string]any{})},
	{Name: "desktop_window", Control: true, Description: "Activate, maximize, minimize, restore, move, resize, move_resize, or close a window chosen by id (from desktop_windows) or title. x, y, width, height are pixels of the last screenshot.",
		Schema: obj(map[string]any{"do": map[string]any{"type": "string", "enum": []string{"activate", "maximize", "minimize", "restore", "move", "resize", "move_resize", "close"}}, "window_id": prop("string", "Window id."), "title": prop("string", "Part of the title."), "x": prop("number", ""), "y": prop("number", ""), "width": prop("number", ""), "height": prop("number", "")}, "do")},
	{Name: "desktop_screen_info", ReadOnly: true, Description: "Screen resolution, pointer position, and the scale of the last screenshot.", Schema: obj(map[string]any{})},
	{Name: "desktop_wait", ReadOnly: true, Description: "Wait until the screen changes (for=change) or a window whose title contains `title` appears (for=window; gone=true waits for it to close). timeout seconds, max 60.",
		Schema: obj(map[string]any{"for": map[string]any{"type": "string", "enum": []string{"change", "window"}}, "title": prop("string", ""), "gone": prop("boolean", ""), "timeout": prop("number", "")})},
	{Name: "desktop_app_open", Control: true, Description: "Start a GUI program in a window (not a shell), e.g. mousepad, thunar, xfce4-terminal.", Schema: obj(map[string]any{"program": prop("string", "Program name or absolute path."), "args": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "program")},
	{Name: "desktop_file_read", ReadOnly: true, Description: "Read a text file in the desktop container (e.g. ~/Desktop/a.txt).", Schema: obj(map[string]any{"path": prop("string", "")}, "path")},
	{Name: "desktop_file_list", ReadOnly: true, Description: "List a directory in the desktop container.", Schema: obj(map[string]any{"path": prop("string", "")}, "path")},
	{Name: "desktop_file_write", Control: true, Description: "Write a text file in the desktop container.", Schema: obj(map[string]any{"path": prop("string", ""), "content": prop("string", "")}, "path", "content")},
	{Name: "desktop_file_download", ReadOnly: true, Description: "Download a file (any type) from the desktop container as base64 (an embedded resource).", Schema: obj(map[string]any{"path": prop("string", "")}, "path")},
	{Name: "desktop_file_upload", Control: true, Needs: "bash", Description: "Upload a file (any type, base64) into the desktop container.", Schema: obj(map[string]any{"path": prop("string", ""), "content_base64": prop("string", "")}, "path", "content_base64")},
	{Name: "desktop_bash", Control: true, Needs: "bash", Description: "Run one shell command in the desktop container and return its output (90 s budget).", Schema: obj(map[string]any{"command": prop("string", "")}, "command")},
	{Name: "desktop_install", Control: true, Needs: "install", Description: "apt-get install Debian packages in the desktop container.", Schema: obj(map[string]any{"packages": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "packages")},
	{Name: "desktop_browser_info", Control: true, Description: "Open (or reuse) the desktop and return the display and the Chromium DevTools (CDP) endpoint of its browser, for driving web pages with a CDP client.", Schema: obj(map[string]any{})},
}

func toolByName(name string) (tool, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return tool{}, false
}

func (s *Server) allowed(id Identity, t tool) error {
	if t.Control && !id.Control() {
		return fmt.Errorf("this token is read-only; %s needs the control scope", t.Name)
	}
	if t.Needs == "bash" && !id.AllowBash {
		return fmt.Errorf("%s is disabled for this token (allow_bash)", t.Name)
	}
	if t.Needs == "install" && !id.AllowInstall {
		return fmt.Errorf("%s is disabled for this token (allow_install)", t.Name)
	}
	return nil
}

func (s *Server) toolList(id Identity) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		if s.allowed(id, t) != nil {
			continue
		}
		schema := t.Schema
		if id.AnyUser {
			schema = withTarget(schema, id.AnyTenant)
		}
		out = append(out, map[string]any{
			"name": t.Name, "description": t.Description, "inputSchema": schema,
			"annotations": map[string]any{"readOnlyHint": t.ReadOnly, "destructiveHint": t.Control && (t.Needs != "" || t.Name == "desktop_window" || t.Name == "desktop_file_write"), "openWorldHint": false},
		})
	}
	return out
}

func textContent(text string) map[string]any { return map[string]any{"type": "text", "text": text} }

func toolError(err error) map[string]any {
	return map[string]any{"content": []any{textContent(err.Error())}, "isError": true}
}

// personControls refuses control while a person has the desktop open in
// noVNC (the takeover/handoff view), when the policy asks for it.
func (s *Server) personControls(ctx context.Context, id Identity) error {
	if s.takeover == "off" {
		return nil
	}
	out, err := s.backend.Ctl(ctx, id, map[string]any{"op": desktop.CtlStatus})
	if err != nil {
		return nil // an older desktop without ctl: nothing to check
	}
	if n, _ := computeruse.Number(out["viewers"]); n > 0 {
		return fmt.Errorf("a person is using this desktop right now (noVNC open); try again after they hand it back")
	}
	return nil
}

func (s *Server) call(ctx context.Context, id Identity, t tool, args map[string]any) map[string]any {
	id, err := id.target(args)
	if err != nil {
		return toolError(err)
	}
	if err := s.allowed(id, t); err != nil {
		return toolError(err)
	}
	if t.Control {
		if err := s.personControls(ctx, id); err != nil {
			return toolError(err)
		}
	}
	result, err := s.run(ctx, id, t.Name, args)
	if err != nil {
		if result != nil {
			result["isError"] = true
			result["content"] = append(result["content"].([]any), textContent(err.Error()))
			return result
		}
		return toolError(err)
	}
	return result
}

func (s *Server) shotContent(shot desktop.Shot, note string) []any {
	meta := map[string]any{
		"screen_width": shot.ScreenWidth, "screen_height": shot.ScreenHeight,
		"image_width": shot.ImageWidth, "image_height": shot.ImageHeight,
		"scale": shot.Scale, "region": shot.Region, "mime": shot.MIME,
	}
	if shot.HasCursor {
		x, y := shot.FromScreen(shot.CursorX, shot.CursorY)
		meta["cursor"] = []int{x, y}
	}
	raw, _ := json.Marshal(meta)
	return []any{
		map[string]any{"type": "image", "data": base64.StdEncoding.EncodeToString(shot.Data), "mimeType": shot.MIME},
		textContent(strings.TrimSpace(note + " " + string(raw))),
	}
}

func (s *Server) capture(ctx context.Context, id Identity, opts desktop.ShotOptions) (desktop.Shot, error) {
	shot, err := s.backend.Screenshot(ctx, id, opts)
	if err != nil {
		return shot, err
	}
	if opts.Region.Empty() {
		s.frame(id).set(shot, opts)
	}
	return shot, nil
}

func (s *Server) run(ctx context.Context, id Identity, name string, args map[string]any) (map[string]any, error) {
	ok := func(content ...any) (map[string]any, error) { return map[string]any{"content": content}, nil }
	frame := s.frame(id)
	shotFrame, base := frame.get()
	switch name {
	case "desktop_screenshot":
		opts, _, err := computeruse.ShotOptionsFromArgs(args, shotFrame, base)
		if err != nil {
			return nil, err
		}
		shot, err := s.capture(ctx, id, opts)
		if err != nil {
			return nil, err
		}
		note := "Full screenshot; coordinates for actions are pixels of this image."
		if !opts.Region.Empty() {
			note = "Zoom; coordinates for actions still use the last full screenshot."
		}
		return ok(s.shotContent(shot, note)...)
	case "desktop_computer":
		steps, err := computeruse.ParseSteps(args["actions"])
		if err != nil {
			return nil, err
		}
		var last *desktop.Shot
		env := computeruse.Env{
			Ctx:   ctx,
			Frame: frame.get,
			Exec: func(batch [][]string) (string, error) {
				out := ""
				for _, argv := range batch {
					if err := s.guardTyping(ctx, id, argv); err != nil {
						return out, err
					}
					text, err := s.backend.App(ctx, id, argv)
					if strings.TrimSpace(text) != "" {
						out = text
					}
					if err != nil {
						return out, err
					}
				}
				return out, nil
			},
			Ctl: func(request map[string]any) (map[string]any, error) { return s.backend.Ctl(ctx, id, request) },
			Capture: func(opts desktop.ShotOptions, _ bool) (string, error) {
				shot, err := s.capture(ctx, id, opts)
				if err != nil {
					return "", err
				}
				last = &shot
				return "screenshot", nil
			},
			Blocked: func() error { return s.personControls(ctx, id) },
			WindowClass: func() string {
				out, _ := s.backend.App(ctx, id, []string{"getactivewindow", "getwindowclassname"})
				return strings.TrimSpace(out)
			},
			Settle: func() { time.Sleep(s.settle) },
		}
		results, _, runErr := computeruse.Run(env, steps, true)
		content := []any{textContent(computeruse.ResultsText(steps, results, runErr))}
		if last != nil {
			content = append(content, s.shotContent(*last, "Screen after the actions.")...)
		}
		result := map[string]any{"content": content}
		return result, runErr
	case "desktop_app_probe", "desktop_clipboard_get", "desktop_clipboard_set", "desktop_windows", "desktop_window", "desktop_screen_info", "desktop_wait":
		action := map[string]string{
			"desktop_app_probe": "app_probe", "desktop_clipboard_get": "clipboard_get", "desktop_clipboard_set": "clipboard_set",
			"desktop_windows": "window_list", "desktop_window": "window", "desktop_screen_info": "screen_info", "desktop_wait": "wait",
		}[name]
		request, err := computeruse.CtlRequest(action, args, shotFrame)
		if err != nil {
			return nil, err
		}
		out, err := computeruse.CtlOK(s.backend.Ctl(ctx, id, request))
		if err != nil {
			return nil, err
		}
		switch action {
		case "app_probe":
			content := []any{textContent(computeruse.ProbeText(out, shotFrame))}
			if marks, _ := args["marks"].(bool); marks {
				opts := base
				opts.Marks = true
				if shot, err := s.backend.Screenshot(ctx, id, opts); err == nil {
					content = append(content, s.shotContent(shot, "Numbered boxes are the element ids.")...)
				}
			}
			return ok(content...)
		case "clipboard_get":
			if image, _ := out["image_base64"].(string); image != "" {
				return ok(map[string]any{"type": "image", "data": image, "mimeType": "image/png"})
			}
		case "screen_info":
			out["last_screenshot"] = map[string]any{"image_width": shotFrame.ImageWidth, "image_height": shotFrame.ImageHeight, "scale": shotFrame.Scale}
		}
		raw, _ := json.Marshal(out)
		return ok(textContent(string(raw)))
	case "desktop_app_open":
		program, _ := args["program"].(string)
		parsed, err := desktop.ProgramArgs(args["args"])
		if err != nil {
			return nil, err
		}
		argv, err := desktop.OpenArgv(program, parsed)
		if err != nil {
			return nil, err
		}
		text, err := s.backend.Open(ctx, id, argv[0], argv[1:])
		if err != nil {
			return nil, err
		}
		return ok(textContent(strings.TrimSpace("started " + argv[0] + "\n" + text)))
	case "desktop_file_read", "desktop_file_list", "desktop_file_write":
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		action := strings.TrimPrefix(name, "desktop_file_")
		text, err := s.backend.File(ctx, id, action, path, content)
		if err != nil {
			return nil, err
		}
		return ok(textContent(text))
	case "desktop_file_download":
		path, _ := args["path"].(string)
		text, err := s.backend.File(ctx, id, "bytes", path, "")
		if err != nil {
			return nil, err
		}
		data := strings.TrimSpace(text)
		if _, err := base64.StdEncoding.DecodeString(data); err != nil {
			return nil, fmt.Errorf("download did not return base64 data")
		}
		return ok(map[string]any{"type": "resource", "resource": map[string]any{"uri": "file://" + path, "mimeType": "application/octet-stream", "blob": data}})
	case "desktop_file_upload":
		path, _ := args["path"].(string)
		encoded, _ := args["content_base64"].(string)
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			return nil, fmt.Errorf("content_base64 is not base64")
		}
		if strings.ContainsAny(path, "'\n\x00") || strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("path is invalid")
		}
		staging := "/tmp/.maclaw-mcp-upload.b64"
		if _, err := s.backend.File(ctx, id, "write", staging, base64.StdEncoding.EncodeToString(raw)); err != nil {
			return nil, err
		}
		target := strings.Replace(path, "~/", "/home/desktop/", 1)
		out, err := s.backend.Bash(ctx, id, fmt.Sprintf("mkdir -p \"$(dirname '%s')\" && base64 -d %s > '%s' && rm -f %s && stat -c '%%s bytes' '%s'", target, staging, target, staging, target))
		if err != nil {
			return nil, err
		}
		return ok(textContent(strings.TrimSpace("uploaded " + target + ": " + out)))
	case "desktop_bash":
		command, _ := args["command"].(string)
		text, err := s.backend.Bash(ctx, id, command)
		if err != nil {
			return nil, err
		}
		return ok(textContent(text))
	case "desktop_install":
		names, err := desktop.PackageNames(args["packages"])
		if err != nil {
			return nil, err
		}
		text, err := s.backend.Install(ctx, id, names)
		if err != nil {
			return map[string]any{"content": []any{textContent(text)}}, err
		}
		return ok(textContent(text))
	case "desktop_browser_info":
		out, err := s.backend.Session(ctx, id)
		if err != nil {
			return nil, err
		}
		info := map[string]any{"display": out["display"], "cdp_url": out["cdp_url"]}
		raw, _ := json.Marshal(info)
		return ok(textContent(string(raw)))
	}
	return nil, fmt.Errorf("tool %s is not implemented", name)
}

// guardTyping keeps typed text out of the browser: this server cannot read
// the page's focused field, so a password field there cannot be excluded.
// Web pages go through the CDP endpoint instead.
func (s *Server) guardTyping(ctx context.Context, id Identity, argv []string) error {
	inserts := false
	if len(argv) > 0 && argv[0] == "type" {
		inserts = true
	}
	if len(argv) > 1 && argv[0] == "key" {
		for _, k := range argv[1:] {
			if !strings.HasPrefix(k, "--") && computeruse.KeyInserts(k) {
				inserts = true
			}
		}
	}
	if !inserts {
		return nil
	}
	out, err := s.backend.App(ctx, id, []string{"getactivewindow", "getwindowclassname"})
	if err != nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(out)) {
	case "chromium", "chromium-browser", "chrome", "google-chrome", "firefox", "firefox-esr", "navigator":
		return fmt.Errorf("typing into the browser window is refused here (a password field cannot be ruled out); use the CDP endpoint from desktop_browser_info for web pages")
	}
	return nil
}

// withTarget adds the admin-only user_id (and tenant_id) arguments.
func withTarget(schema map[string]any, tenant bool) map[string]any {
	out := map[string]any{}
	for k, v := range schema {
		out[k] = v
	}
	props := map[string]any{}
	if old, ok := schema["properties"].(map[string]any); ok {
		for k, v := range old {
			props[k] = v
		}
	}
	props["user_id"] = prop("string", "Admin only: the user whose desktop to use.")
	if tenant {
		props["tenant_id"] = prop("string", "Admin only: that user's tenant.")
	}
	out["properties"] = props
	return out
}

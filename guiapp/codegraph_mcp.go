package guiapp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// codegraphMCPSpec is one tools/call against the shared CodeGraph daemon.
// Symbol is set when the reply must still contain that symbol's definition.
// An indexed line range can slice the wrong part of the current file.
type codegraphMCPSpec struct {
	Tool   string
	Args   map[string]interface{}
	Symbol string
}

// codegraphMCPSpecs maps a code_navigation call onto the daemon's tools.
// An area question is one codegraph_explore. A single symbol is codegraph_node
// with its body. A file is the symbol map. Call edges use the callers and
// callees tools, which accept a file pin the CLI callers command does not.
func codegraphMCPSpecs(operation, query, question, file string, line int) []codegraphMCPSpec {
	pin := codegraphPinFile(file)
	switch operation {
	case "file_symbols":
		return []codegraphMCPSpec{{Tool: "codegraph_node", Args: codegraphMCPFileArgs(query, true)}}
	case "node", "definition":
		if codegraphTargetIsFile(query) {
			return []codegraphMCPSpec{{Tool: "codegraph_node", Args: codegraphMCPFileArgs(query, true)}}
		}
		return []codegraphMCPSpec{{Tool: "codegraph_node", Args: codegraphMCPSymbolArgs(query, pin, line), Symbol: query}}
	case "workspace_symbol":
		return []codegraphMCPSpec{{Tool: "codegraph_search", Args: map[string]interface{}{"query": query, "limit": 10}}}
	case "implementation":
		if codegraphTargetIsSymbol(query) {
			return []codegraphMCPSpec{{Tool: "codegraph_node", Args: codegraphMCPSymbolArgs(query, pin, line), Symbol: query}}
		}
		return []codegraphMCPSpec{{Tool: "codegraph_explore", Args: map[string]interface{}{"query": query, "maxFiles": 8}}}
	case "references", "callers":
		return []codegraphMCPSpec{{Tool: "codegraph_callers", Args: codegraphMCPEdgeArgs(query, pin)}}
	case "callees":
		return []codegraphMCPSpec{{Tool: "codegraph_callees", Args: codegraphMCPEdgeArgs(query, pin)}}
	default:
		target := strings.TrimSpace(question)
		if target == "" {
			target = strings.TrimSpace(query)
		}
		if codegraphTargetIsFile(target) {
			return []codegraphMCPSpec{{Tool: "codegraph_node", Args: codegraphMCPFileArgs(target, true)}}
		}
		if codegraphTargetIsSymbol(target) {
			return []codegraphMCPSpec{{Tool: "codegraph_node", Args: codegraphMCPSymbolArgs(target, pin, line), Symbol: target}}
		}
		return []codegraphMCPSpec{{Tool: "codegraph_explore", Args: map[string]interface{}{"query": target, "maxFiles": 8}}}
	}
}

func codegraphMCPFileArgs(file string, symbolsOnly bool) map[string]interface{} {
	args := map[string]interface{}{"file": file}
	if symbolsOnly {
		args["symbolsOnly"] = true
	}
	return args
}

func codegraphMCPSymbolArgs(symbol, pin string, line int) map[string]interface{} {
	args := map[string]interface{}{"symbol": symbol, "includeCode": true}
	if pin != "" {
		args["file"] = pin
	}
	if line > 0 {
		args["line"] = line
	}
	return args
}

func codegraphMCPEdgeArgs(symbol, pin string) map[string]interface{} {
	args := map[string]interface{}{"symbol": symbol, "limit": 20}
	if pin != "" {
		args["file"] = pin
	}
	return args
}

// codegraphExcerptDefines reports that text still contains a definition of
// symbol. Call sites and a stale slice of another function do not count.
func codegraphExcerptDefines(text, symbol string) bool {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" || strings.TrimSpace(text) == "" {
		return false
	}
	return codegraphDefinitionStart(strings.Split(text, "\n"), symbol) >= 0
}

// codegraphMCPRepair keeps text when it still defines symbol. Otherwise it
// reads the current file and returns that definition. An empty result means
// the caller should use the CLI fallback.
func codegraphMCPRepair(root, text, symbol, pin string) (string, bool) {
	if root != "" {
		text = codegraphMCPRefreshExplore(root, text)
	}
	symbol = strings.TrimSpace(symbol)
	if symbol == "" || !codegraphRemoteNodeSymbol(symbol) {
		return text, strings.TrimSpace(text) != ""
	}
	file := pin
	if file == "" {
		file = codegraphLocationFile(text)
	}
	// The indexed text can still contain name( after an edit. The daemon
	// names that file in its staleness banner. Only that file's function
	// body has to come from disk. A banner for some other file means this
	// definition is the fresh part of the response; dropping it made the
	// lookup miss and the agent read the whole project.
	if codegraphMCPStale(text) {
		_, kind, kindOK := codegraphMCPHeading(text)
		if (!kindOK || codegraphMCPKindHasBody(kind)) && file != "" && codegraphMCPStaleFile(text, file) {
			if root != "" && !navigationQueryEscapesRoot(file) {
				if body := strings.TrimSpace(codegraphSourceBody(root, file, symbol)); body != "" {
					return "Body for " + symbol + " in " + file + ".\n" + body, true
				}
			}
			return "", false
		}
	}
	if codegraphExcerptDefines(text, symbol) {
		return text, true
	}
	if root != "" && file != "" && !navigationQueryEscapesRoot(file) {
		if body := strings.TrimSpace(codegraphSourceBody(root, file, symbol)); body != "" {
			return "Body for " + symbol + " in " + file + ".\n" + body, true
		}
	}
	// A function body that does not contain the definition is the stale
	// indexed slice. Fall back so the current file can be read. A struct,
	// type, or variable has no brace body to recover, so the daemon text stays.
	name, kind, ok := codegraphMCPHeading(text)
	if ok && codegraphHeadingNameMatches(name, symbol) && !codegraphMCPKindHasBody(kind) {
		return text, true
	}
	return "", false
}

func codegraphMCPStale(text string) bool {
	return strings.Contains(text, "were edited since the last index sync")
}

// codegraphMCPStaleFile reports that the daemon's banner names this path.
// The match is the listed path, not a shorter name inside a longer one.
func codegraphMCPStaleFile(text, path string) bool {
	if !codegraphMCPStale(text) {
		return false
	}
	path = strings.ReplaceAll(strings.TrimSpace(path), `\`, "/")
	if path == "" {
		return false
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") || !strings.Contains(line, " (edited ") {
			continue
		}
		listed := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if i := strings.Index(listed, " (edited "); i >= 0 {
			listed = strings.TrimSpace(listed[:i])
		}
		if strings.ReplaceAll(listed, `\`, "/") == path {
			return true
		}
	}
	return false
}

// codegraphMCPRefreshExplore replaces an explore code block when the indexed
// line range no longer contains the named function. The block is read from
// the current file at those old lines, so it can be a different function.
func codegraphMCPRefreshExplore(root, text string) string {
	if root == "" || !strings.Contains(text, "#### ") {
		return text
	}
	lines := strings.Split(text, "\n")
	var out []string
	refreshed := map[string]bool{}
	for i := 0; i < len(lines); i++ {
		path, symbols := codegraphMCPExploreHeader(lines[i])
		if path == "" || len(symbols) == 0 {
			out = append(out, lines[i])
			continue
		}
		fence := i + 1
		if fence < len(lines) && strings.TrimSpace(lines[fence]) == "" {
			fence++
		}
		if fence >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[fence]), "```") {
			out = append(out, lines[i])
			continue
		}
		end := fence + 1
		for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "```") {
			end++
		}
		if end >= len(lines) {
			out = append(out, lines[i])
			continue
		}
		body := strings.Join(lines[fence+1:end], "\n")
		// The banner means this file's indexed ranges are already known to
		// be old. A slice can still contain the signature while ending at
		// the previous last line, so the body has to come from disk.
		stale := codegraphMCPStaleFile(text, path)
		var fresh []string
		kept := false
		sawBody := false
		missedDisk := false
		for _, sym := range symbols {
			if !codegraphMCPKindHasBody(sym.kind) || !codegraphRemoteNodeSymbol(sym.name) {
				continue
			}
			sawBody = true
			disk := strings.TrimSpace(codegraphSourceBody(root, path, sym.name))
			if stale && disk != "" {
				fresh = append(fresh, disk)
				continue
			}
			if stale {
				missedDisk = true
			}
			if codegraphExcerptDefines(body, sym.name) {
				kept = true
				continue
			}
			if !stale && disk != "" {
				fresh = append(fresh, disk)
			}
		}
		if stale && sawBody && !missedDisk {
			refreshed[strings.ReplaceAll(path, `\`, "/")] = true
		}
		out = append(out, lines[i])
		if fence > i+1 {
			out = append(out, lines[i+1:fence]...)
		}
		out = append(out, lines[fence])
		// A slice that still defines one of the named functions stays.
		// Replacing it would drop that function and keep only the ones
		// that had to be read from disk. A slice that defines none of
		// them is the wrong line range, so only the disk bodies remain.
		if len(fresh) == 0 || kept {
			out = append(out, lines[fence+1:end]...)
		}
		if len(fresh) > 0 {
			if kept {
				out = append(out, "")
			}
			out = append(out, strings.Join(fresh, "\n\n"))
		}
		out = append(out, lines[end])
		i = end
	}
	return codegraphStripRefreshedStaleBanner(strings.Join(out, "\n"), refreshed)
}

// codegraphStripRefreshedStaleBanner removes files whose bodies were just
// read from disk. The banner tells the model to Read those files; leaving it
// in place sends the agent back to the whole file after the body is current.
func codegraphStripRefreshedStaleBanner(text string, refreshed map[string]bool) string {
	if len(refreshed) == 0 || !codegraphMCPStale(text) {
		return text
	}
	lines := strings.Split(text, "\n")
	drop := map[int]bool{}
	header := -1
	instruction := -1
	remaining := 0
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if header < 0 && strings.Contains(trim, "were edited since the last index sync") {
			header = i
			continue
		}
		if header < 0 {
			continue
		}
		if instruction < 0 && strings.HasPrefix(trim, "- ") && strings.Contains(trim, " (edited ") {
			listed := strings.TrimSpace(strings.TrimPrefix(trim, "- "))
			if j := strings.Index(listed, " (edited "); j >= 0 {
				listed = strings.TrimSpace(listed[:j])
			}
			if refreshed[strings.ReplaceAll(listed, `\`, "/")] {
				drop[i] = true
			} else {
				remaining++
			}
			continue
		}
		if strings.Contains(trim, "Read them directly") {
			instruction = i
			break
		}
		if trim != "" {
			break
		}
	}
	if remaining == 0 && header >= 0 {
		drop[header] = true
		if instruction >= 0 {
			drop[instruction] = true
		}
	}
	if len(drop) == 0 {
		return text
	}
	kept := make([]string, 0, len(lines))
	for i, line := range lines {
		if !drop[i] {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

type codegraphMCPExploreSymbol struct {
	name string
	kind string
}

func codegraphMCPExploreHeader(line string) (string, []codegraphMCPExploreSymbol) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "#### ") {
		return "", nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "#### "))
	sep := " — "
	i := strings.Index(rest, sep)
	if i < 0 {
		sep = " - "
		i = strings.Index(rest, sep)
	}
	if i <= 0 {
		return "", nil
	}
	path := strings.TrimSpace(rest[:i])
	if path == "" || navigationQueryEscapesRoot(path) {
		return "", nil
	}
	tail := rest[i+len(sep):]
	if dot := strings.Index(tail, " · "); dot >= 0 {
		tail = tail[:dot]
	}
	var symbols []codegraphMCPExploreSymbol
	for _, part := range strings.Split(tail, ",") {
		part = strings.TrimSpace(part)
		if part == "" || strings.HasPrefix(part, "+") {
			continue
		}
		open := strings.LastIndex(part, "(")
		if open <= 0 || !strings.HasSuffix(part, ")") {
			continue
		}
		symbols = append(symbols, codegraphMCPExploreSymbol{
			name: strings.TrimSpace(part[:open]),
			kind: strings.TrimSpace(strings.TrimSuffix(part[open+1:], ")")),
		})
	}
	return path, symbols
}

func codegraphMCPHeading(text string) (name, kind string, ok bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "## "))
		open := strings.LastIndex(rest, " (")
		if open <= 0 || !strings.HasSuffix(rest, ")") {
			return rest, "", true
		}
		return strings.TrimSpace(rest[:open]), strings.TrimSuffix(strings.TrimSpace(rest[open+2:]), ")"), true
	}
	return "", "", false
}

func codegraphMCPKindHasBody(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "function", "method", "func", "constructor":
		return true
	default:
		return false
	}
}

func codegraphLocationFile(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, ":") {
			continue
		}
		if loc := codegraphLocationPath(line); loc != "" {
			return loc
		}
	}
	return ""
}

func codegraphLocationPath(line string) string {
	marked := false
	const marker = "**Location:**"
	if i := strings.Index(line, marker); i >= 0 {
		marked = true
		line = strings.TrimSpace(line[i+len(marker):])
	}
	line = strings.Trim(strings.TrimSpace(line), "`")
	line = codegraphStripLocationSuffix(line)
	path := strings.TrimSpace(line)
	if path == "" || strings.Contains(path, "\t") || (strings.Contains(path, " ") && !marked) {
		return ""
	}
	if !marked && !strings.Contains(path, ".") {
		return ""
	}
	if navigationQueryEscapesRoot(path) {
		return ""
	}
	return strings.ReplaceAll(path, `\`, "/")
}

// codegraphStripLocationSuffix removes :line and :line:column. A colon that
// belongs to the path, including a drive letter, stays.
func codegraphStripLocationSuffix(path string) string {
	for n := 0; n < 2; n++ {
		colon := strings.LastIndex(path, ":")
		if colon <= 0 {
			break
		}
		suffix := path[colon+1:]
		if suffix == "" {
			break
		}
		digits := true
		for _, r := range suffix {
			if r < '0' || r > '9' {
				digits = false
				break
			}
		}
		if !digits {
			break
		}
		path = path[:colon]
	}
	return path
}

type codegraphMCPPipe struct {
	write   func([]byte) error
	read    func() (string, error)
	close   func()
	waitEnd func()
	kill    func()
	stderr  func() string
}

type codegraphMCPSession struct {
	key      string
	root     string
	pipe     codegraphMCPPipe
	writeMu  sync.Mutex
	pendMu   sync.Mutex
	pending  map[string]chan codegraphMCPResponse
	nextID   atomic.Int64
	alive    atomic.Bool
	exited   chan struct{}
	lastLine string
}

type codegraphMCPResponse struct {
	result json.RawMessage
	err    error
}

type codegraphRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method,omitempty"`
	Params  interface{}     `json:"params,omitempty"`
}

type codegraphRPCReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   interface{}     `json:"error,omitempty"`
}

var (
	codegraphMCPMu   sync.Mutex
	codegraphMCPPool = map[string]*codegraphMCPSession{}
)

func codegraphMCPNavigate(root, bin, operation, query, question, file string, line int) (string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(bin) == "" {
		return "", fmt.Errorf("codegraph mcp unavailable")
	}
	return codegraphMCPInvoke(func(ctx context.Context) (*codegraphMCPSession, error) {
		return codegraphMCPLocal(ctx, root, bin)
	}, func(ctx context.Context, session *codegraphMCPSession) (string, error) {
		return session.navigate(ctx, operation, query, question, file, line, root)
	})
}

// codegraphMCPInvoke tries the proxy twice. The first budget is short because
// a proxy that loses the race with the daemon falls back in-process and then
// blocks on the database lock the daemon already holds. Closing that proxy
// and opening another attaches to the daemon that is already listening.
func codegraphMCPInvoke(open func(context.Context) (*codegraphMCPSession, error), run func(context.Context, *codegraphMCPSession) (string, error)) (string, error) {
	var last error
	for attempt, budget := range []time.Duration{20 * time.Second, 40 * time.Second} {
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		session, err := open(ctx)
		if err != nil {
			cancel()
			last = err
			if attempt == 1 || !errors.Is(err, context.DeadlineExceeded) {
				return "", err
			}
			continue
		}
		text, err := run(ctx, session)
		cancel()
		if err == nil || attempt == 1 || !errors.Is(err, context.DeadlineExceeded) {
			return text, err
		}
		last = err
	}
	return "", last
}

func (s *codegraphMCPSession) navigate(ctx context.Context, operation, query, question, file string, line int, repairRoot string) (string, error) {
	specs := codegraphMCPSpecs(operation, query, question, file, line)
	if len(specs) == 0 {
		return "", fmt.Errorf("codegraph mcp: no call")
	}
	pin := codegraphPinFile(file)
	var parts []string
	for _, spec := range specs {
		raw, err := s.callTool(ctx, spec.Tool, spec.Args)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				// The proxy may be stuck forwarding to a daemon that accepted
				// the socket and never sent hello. Drop it so the next call
				// opens a new proxy. stop does not kill the detached daemon.
				s.abandon()
			}
			return "", err
		}
		text, err := codegraphMCPResultText(raw)
		if err != nil {
			return "", err
		}
		// A miss is a successful tool result ("No callers found", "Symbol …
		// not found"). Returning it would skip the CLI and text-search
		// fallback. An empty result leaves the index usable.
		if !codegraphOutputUseful(text) {
			return "", nil
		}
		repaired, ok := codegraphMCPRepair(repairRoot, text, spec.Symbol, pin)
		if !ok {
			return "", fmt.Errorf("codegraph mcp: indexed span for %s does not define it", spec.Symbol)
		}
		parts = append(parts, repaired)
	}
	return strings.TrimSpace(strings.Join(parts, "\n")), nil
}

func codegraphMCPResultText(raw json.RawMessage) (string, error) {
	var body struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", err
	}
	if body.IsError {
		return "", fmt.Errorf("codegraph mcp tool error")
	}
	var b strings.Builder
	for _, part := range body.Content {
		if strings.TrimSpace(part.Text) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(part.Text)
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return "", fmt.Errorf("codegraph mcp empty result")
	}
	if codegraphMCPUnindexed(text) {
		return "", fmt.Errorf("codegraph mcp: %s", firstLine(text))
	}
	return text, nil
}

// codegraphMCPUnindexed reports CodeGraph's own not-indexed tool errors.
// The error is the first line of the tool result. A later source line that
// quotes the same sentence must not throw the definition away.
func codegraphMCPUnindexed(text string) bool {
	line := strings.TrimSpace(codegraphFirstLetterLine(text))
	if line == "" {
		return false
	}
	return strings.HasPrefix(line, "No CodeGraph project is loaded") ||
		strings.Contains(line, "isn't indexed with codegraph") ||
		strings.HasPrefix(line, "No indexed file matches")
}

func codegraphMCPLocal(ctx context.Context, root, bin string) (*codegraphMCPSession, error) {
	key := "local\x00" + filepath.Clean(root)
	return codegraphMCPGet(ctx, key, root, func() (codegraphMCPPipe, error) {
		return startLocalCodegraphProxy(root, bin)
	})
}

func startLocalCodegraphProxy(root, bin string) (codegraphMCPPipe, error) {
	command, args := codegraphServeArgs(bin, root)
	cmd := exec.Command(command, args...)
	cmd.Dir = root
	cmd.Env = os.Environ()
	hideCommandWindow(cmd)
	tool.PrepareCommandForTreeKill(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return codegraphMCPPipe{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return codegraphMCPPipe{}, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return codegraphMCPPipe{}, err
	}
	if err := cmd.Start(); err != nil {
		return codegraphMCPPipe{}, err
	}
	var errMu sync.Mutex
	var errBuf bytes.Buffer
	go func() {
		buf := make([]byte, 1024)
		for {
			n, readErr := stderr.Read(buf)
			if n > 0 {
				errMu.Lock()
				if errBuf.Len() < 4096 {
					errBuf.Write(buf[:n])
				}
				errMu.Unlock()
			}
			if readErr != nil {
				return
			}
		}
	}()
	reader := bufio.NewReaderSize(stdout, 1024*1024)
	return codegraphMCPPipe{
		write: func(p []byte) error {
			_, err := stdin.Write(p)
			return err
		},
		read: func() (string, error) { return reader.ReadString('\n') },
		close: func() {
			_ = stdin.Close()
		},
		waitEnd: func() { _ = cmd.Wait() },
		kill: func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		},
		stderr: func() string {
			errMu.Lock()
			defer errMu.Unlock()
			return strings.TrimSpace(errBuf.String())
		},
	}, nil
}

// codegraphServeArgs starts the bundled node directly. codegraph.cmd is a
// batch shim; its stdin pipe does not reach the process that speaks MCP.
func codegraphServeArgs(bin, root string) (string, []string) {
	serve := []string{"serve", "--mcp", "--path", root}
	if exe, entry, ok := codegraphBundledEntry(bin); ok {
		if entry != "" {
			return exe, append([]string{"--liftoff-only", entry}, serve...)
		}
		return exe, serve
	}
	return bin, serve
}

func codegraphBundledEntry(bin string) (string, string, bool) {
	pkg := "codegraph-" + codegraphPlatformOS() + "-" + codegraphPlatformArch()
	dir := filepath.Dir(bin)
	roots := []string{
		filepath.Join(dir, "node_modules", "@colbymchenry", pkg),
		filepath.Join(dir, "node_modules", "@colbymchenry", "codegraph", "node_modules", pkg),
	}
	for _, root := range roots {
		if runtime.GOOS == "windows" {
			nodeExe := filepath.Join(root, "node.exe")
			entry := filepath.Join(root, "lib", "dist", "bin", "codegraph.js")
			if regularFile(nodeExe) && regularFile(entry) {
				return nodeExe, entry, true
			}
			continue
		}
		launcher := filepath.Join(root, "bin", "codegraph")
		if regularFile(launcher) {
			return launcher, "", true
		}
	}
	return "", "", false
}

func codegraphPlatformOS() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS
}

func codegraphPlatformArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	default:
		return runtime.GOARCH
	}
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func codegraphMCPGet(ctx context.Context, key, root string, open func() (codegraphMCPPipe, error)) (*codegraphMCPSession, error) {
	codegraphMCPMu.Lock()
	if existing := codegraphMCPPool[key]; existing != nil && existing.alive.Load() {
		codegraphMCPMu.Unlock()
		return existing, nil
	}
	codegraphMCPMu.Unlock()

	pipe, err := open()
	if err != nil {
		return nil, err
	}
	session := &codegraphMCPSession{
		key:     key,
		root:    root,
		pipe:    pipe,
		pending: map[string]chan codegraphMCPResponse{},
		exited:  make(chan struct{}),
	}
	session.alive.Store(true)
	go session.readLoop()
	if pipe.waitEnd != nil {
		go func() {
			pipe.waitEnd()
			close(session.exited)
			session.fail(fmt.Errorf("codegraph mcp proxy exited"))
		}()
	} else {
		close(session.exited)
	}
	if err := session.initialize(ctx); err != nil {
		detail := ""
		if session.pipe.stderr != nil {
			detail = session.pipe.stderr()
		}
		session.stop()
		if detail != "" {
			return nil, fmt.Errorf("%w: %s", err, detail)
		}
		return nil, err
	}
	codegraphMCPMu.Lock()
	if existing := codegraphMCPPool[key]; existing != nil && existing.alive.Load() {
		codegraphMCPMu.Unlock()
		session.stop()
		return existing, nil
	}
	codegraphMCPPool[key] = session
	codegraphMCPMu.Unlock()
	return session, nil
}

func (s *codegraphMCPSession) initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "maclaw",
			"version": "1.0.0",
		},
	}
	if _, err := s.request(ctx, "initialize", params); err != nil {
		return err
	}
	note, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	if err != nil {
		return err
	}
	note = append(note, '\n')
	s.writeMu.Lock()
	err = s.pipe.write(note)
	s.writeMu.Unlock()
	return err
}

func (s *codegraphMCPSession) callTool(ctx context.Context, name string, args map[string]interface{}) (json.RawMessage, error) {
	if args == nil {
		args = map[string]interface{}{}
	}
	return s.request(ctx, "tools/call", map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
}

func (s *codegraphMCPSession) request(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !s.alive.Load() {
		return nil, fmt.Errorf("codegraph mcp proxy is not running")
	}
	id := strconv.FormatInt(s.nextID.Add(1), 10)
	payload, err := json.Marshal(codegraphRPCRequest{
		JSONRPC: "2.0",
		ID:      json.RawMessage(id),
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return nil, err
	}
	payload = append(payload, '\n')
	// Buffer two values. A tool response and the shutdown error can both be
	// sent after the caller has left. A select/default send is not safe here:
	// default is always ready, so Go may choose it and drop a response that
	// the buffer had room for.
	responseC := make(chan codegraphMCPResponse, 2)
	s.pendMu.Lock()
	s.pending[id] = responseC
	s.pendMu.Unlock()
	defer func() {
		s.pendMu.Lock()
		delete(s.pending, id)
		s.pendMu.Unlock()
	}()
	s.writeMu.Lock()
	err = s.pipe.write(payload)
	s.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	select {
	case resp := <-responseC:
		return resp.result, resp.err
	case <-ctx.Done():
		s.writeMu.Lock()
		last := s.lastLine
		s.writeMu.Unlock()
		note := ""
		if s.pipe.stderr != nil {
			note = s.pipe.stderr()
		}
		if last == "" && note == "" {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w last=%q stderr=%q", ctx.Err(), last, note)
	}
}

func (s *codegraphMCPSession) noteLine(line string) {
	if len(line) > 180 {
		line = line[:180]
	}
	s.writeMu.Lock()
	s.lastLine = line
	s.writeMu.Unlock()
}

func (s *codegraphMCPSession) readLoop() {
	for s.alive.Load() {
		line, err := s.pipe.read()
		if err != nil {
			s.fail(err)
			return
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			if line != "" {
				s.noteLine(line)
			}
			continue
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(line), &msg) != nil {
			s.noteLine(line)
			continue
		}
		id, hasID := codegraphRPCIDKey(msg.ID)
		if msg.Method != "" {
			if hasID {
				s.replyServer(msg.ID, msg.Method)
			}
			continue
		}
		if !hasID {
			s.noteLine("no-id " + line)
			continue
		}
		s.pendMu.Lock()
		ch := s.pending[id]
		s.pendMu.Unlock()
		if ch == nil {
			s.noteLine("unmatched " + id)
			continue
		}
		if msg.Error != nil {
			ch <- codegraphMCPResponse{err: fmt.Errorf("codegraph mcp %d: %s", msg.Error.Code, msg.Error.Message)}
			continue
		}
		ch <- codegraphMCPResponse{result: msg.Result}
	}
}

func (s *codegraphMCPSession) replyServer(id json.RawMessage, method string) {
	reply := codegraphRPCReply{JSONRPC: "2.0", ID: id}
	switch method {
	case "ping":
		reply.Result = map[string]interface{}{}
	case "roots/list":
		reply.Result = map[string]interface{}{"roots": []map[string]string{{
			"uri":  codegraphFileURI(s.root),
			"name": "project",
		}}}
	default:
		reply.Error = map[string]interface{}{"code": -32601, "message": "method not found"}
	}
	s.writeMsg(reply)
}

func (s *codegraphMCPSession) writeMsg(msg interface{}) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	payload = append(payload, '\n')
	s.writeMu.Lock()
	_ = s.pipe.write(payload)
	s.writeMu.Unlock()
}

func codegraphRPCIDKey(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return strconv.FormatInt(n, 10), true
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil && f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10), true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s, true
	}
	return "", false
}

func codegraphFileURI(path string) string {
	slash := filepath.ToSlash(path)
	if strings.HasPrefix(slash, "/") {
		return "file://" + slash
	}
	return "file:///" + slash
}

// abandon removes the proxy from the pool and closes it. The detached
// per-project daemon keeps running. Stop is synchronous so a retry does not
// open a second process while this one still holds the database.
func (s *codegraphMCPSession) abandon() {
	codegraphMCPMu.Lock()
	if codegraphMCPPool[s.key] == s {
		delete(codegraphMCPPool, s.key)
	}
	codegraphMCPMu.Unlock()
	s.stop()
}

func (s *codegraphMCPSession) fail(err error) {
	if err == nil {
		err = errors.New("codegraph mcp proxy stopped")
	}
	if !s.alive.CompareAndSwap(true, false) {
		return
	}
	codegraphMCPMu.Lock()
	if codegraphMCPPool[s.key] == s {
		delete(codegraphMCPPool, s.key)
	}
	codegraphMCPMu.Unlock()
	s.pendMu.Lock()
	pending := s.pending
	s.pending = map[string]chan codegraphMCPResponse{}
	s.pendMu.Unlock()
	for _, ch := range pending {
		ch <- codegraphMCPResponse{err: err}
	}
}

// stop closes the proxy stdin and, if it does not exit, kills that process
// only. The detached per-project daemon is not part of this kill.
func codegraphMCPStopAll() {
	codegraphMCPMu.Lock()
	sessions := make([]*codegraphMCPSession, 0, len(codegraphMCPPool))
	for _, session := range codegraphMCPPool {
		sessions = append(sessions, session)
	}
	codegraphMCPMu.Unlock()
	for _, session := range sessions {
		session.stop()
	}
}

func (s *codegraphMCPSession) stop() {
	if s.pipe.close != nil {
		s.pipe.close()
	}
	select {
	case <-s.exited:
	case <-time.After(2 * time.Second):
		if s.pipe.kill != nil {
			s.pipe.kill()
		}
		select {
		case <-s.exited:
		case <-time.After(2 * time.Second):
		}
	}
	s.fail(fmt.Errorf("codegraph mcp proxy stopped"))
}

func remoteCodegraphIndexProbeCommand() string {
	return remoteCodegraphDirAssignment() + remoteCodegraphHasDBFunc() + "if _cg_has_db; then echo READY; else echo NONE; fi"
}

func remoteCodegraphServeCommand(projectDir string) string {
	var b strings.Builder
	b.WriteString("cd " + remoteShellQuote(projectDir) + " || exit 1\n")
	b.WriteString("_cg_bin=$(command -v codegraph 2>/dev/null || true)\n")
	b.WriteString("if [ -z \"$_cg_bin\" ]; then _cg_bin=$(command -v codegraph.cmd 2>/dev/null || true); fi\n")
	b.WriteString("if [ -z \"$_cg_bin\" ]; then _cg_prefix=$(npm prefix -g 2>/dev/null || true); if [ -x \"$_cg_prefix/bin/codegraph\" ]; then _cg_bin=\"$_cg_prefix/bin/codegraph\"; elif [ -f \"$_cg_prefix/codegraph.cmd\" ]; then _cg_bin=\"$_cg_prefix/codegraph.cmd\"; fi; fi\n")
	b.WriteString("if [ -z \"$_cg_bin\" ]; then exit 127; fi\n")
	b.WriteString("exec \"$_cg_bin\" serve --mcp --path .\n")
	return "sh -lc " + remoteShellQuote(b.String())
}

var remoteCodegraphIndexReadyCache sync.Map

func remoteCodegraphRememberIndex(projectDir string) {
	if strings.TrimSpace(projectDir) != "" {
		remoteCodegraphIndexReadyCache.Store(projectDir, true)
	}
}

func remoteCodegraphForgetIndex(projectDir string) {
	remoteCodegraphIndexReadyCache.Delete(projectDir)
}

func remoteCodegraphProbeReady(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "READY" {
			return true
		}
	}
	return false
}

func (c *remoteCodingCallbacks) remoteCodegraphIndexCachedOrProbe() bool {
	if c == nil || c.agent == nil {
		return false
	}
	dir := c.agent.projectDir
	if _, ok := remoteCodegraphIndexReadyCache.Load(dir); ok {
		return true
	}
	out := c.sshBash(map[string]interface{}{
		"command":     remoteCodegraphIndexProbeCommand(),
		"working_dir": dir,
	})
	if remoteCodegraphProbeReady(out) {
		remoteCodegraphRememberIndex(dir)
		return true
	}
	return false
}

func (c *remoteCodingCallbacks) callRemoteCodegraphMCP(operation, query, question, file string, line int) (string, error) {
	if c == nil || c.agent == nil || c.agent.handler == nil || strings.TrimSpace(c.agent.projectDir) == "" {
		return "", fmt.Errorf("remote codegraph mcp unavailable")
	}
	key := "remote\x00" + c.agent.sessionID + "\x00" + c.agent.projectDir
	return codegraphMCPInvoke(func(ctx context.Context) (*codegraphMCPSession, error) {
		return codegraphMCPGet(ctx, key, c.agent.projectDir, func() (codegraphMCPPipe, error) {
			return c.openRemoteCodegraphPipe()
		})
	}, func(ctx context.Context, session *codegraphMCPSession) (string, error) {
		return session.navigate(ctx, operation, query, question, file, line, "")
	})
}

func (c *remoteCodingCallbacks) openRemoteCodegraphPipe() (codegraphMCPPipe, error) {
	h := c.agent.handler
	mgr := h.ensureSSHManager()
	if mgr == nil {
		return codegraphMCPPipe{}, fmt.Errorf("ssh manager unavailable")
	}
	session, ok := mgr.Get(c.agent.sessionID)
	if !ok || session == nil || session.Handle == nil {
		return codegraphMCPPipe{}, fmt.Errorf("ssh session unavailable")
	}
	if c.agent.runtimeAttempt != nil {
		expected := guiRemoteCodingTargetIdentity(h, c.agent.sessionID, c.agent.projectDir)
		if expected == "" || !guiRuntimeSSHSessionAlive(session) || guiRemoteCodingTargetIdentity(h, c.agent.sessionID, c.agent.projectDir) != expected {
			return codegraphMCPPipe{}, fmt.Errorf("verified remote session unavailable")
		}
	}
	client := session.Handle.Client()
	if client == nil {
		return codegraphMCPPipe{}, fmt.Errorf("ssh client unavailable")
	}
	stream, err := remote.OpenSSHCommand(client, remoteCodegraphServeCommand(c.agent.projectDir))
	if err != nil {
		return codegraphMCPPipe{}, err
	}
	return codegraphMCPPipe{
		write: func(p []byte) error {
			_, err := stream.Write(p)
			return err
		},
		read:    stream.ReadLine,
		close:   func() { _ = stream.Close() },
		waitEnd: func() { _ = stream.Wait() },
	}, nil
}

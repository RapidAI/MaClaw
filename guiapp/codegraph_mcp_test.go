package guiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodegraphMCPSpecsUsesExploreForAnArea(t *testing.T) {
	specs := codegraphMCPSpecs("explore", "how auth works", "how auth works", "", 0)
	if len(specs) != 1 || specs[0].Tool != "codegraph_explore" || specs[0].Symbol != "" {
		t.Fatalf("area specs = %#v", specs)
	}
	if specs[0].Args["query"] != "how auth works" || specs[0].Args["maxFiles"] != 8 {
		t.Fatalf("explore args = %#v", specs[0].Args)
	}
	symbol := codegraphMCPSpecs("explore", "AuthService", "AuthService", "", 0)
	if len(symbol) != 1 || symbol[0].Tool != "codegraph_node" || symbol[0].Symbol != "AuthService" || symbol[0].Args["includeCode"] != true {
		t.Fatalf("symbol specs = %#v", symbol)
	}
	file := codegraphMCPSpecs("file_symbols", "snake.cpp", "snake.cpp", "", 0)
	if file[0].Tool != "codegraph_node" || file[0].Args["symbolsOnly"] != true || file[0].Args["file"] != "snake.cpp" {
		t.Fatalf("file specs = %#v", file)
	}
	def := codegraphMCPSpecs("definition", "Foo", "Find the definition of Foo", "pkg/foo.go", 10)
	if def[0].Tool != "codegraph_node" || def[0].Args["file"] != "pkg/foo.go" || def[0].Args["line"] != 10 || def[0].Args["symbol"] != "Foo" {
		t.Fatalf("definition args = %#v", def[0].Args)
	}
	callers := codegraphMCPSpecs("callers", "Audio::playTone", "Find callers of Audio::playTone", "", 0)
	if callers[0].Tool != "codegraph_callers" || callers[0].Args["symbol"] != "Audio::playTone" || callers[0].Args["file"] != nil {
		t.Fatalf("qualified callers = %#v", callers[0].Args)
	}
	pinned := codegraphMCPSpecs("callers", "playTone", "Find callers of playTone", "snake.cpp", 0)
	if pinned[0].Args["file"] != "snake.cpp" {
		t.Fatalf("pinned callers = %#v", pinned[0].Args)
	}
	if outside := codegraphMCPSpecs("callers", "playTone", "Find callers of playTone", "../snake.cpp", 0); outside[0].Args["file"] != nil {
		t.Fatalf("escaped pin = %#v", outside[0].Args)
	}
	search := codegraphMCPSpecs("workspace_symbol", "playTone", "playTone", "", 0)
	if search[0].Tool != "codegraph_search" {
		t.Fatalf("search tool = %s", search[0].Tool)
	}
}

func TestCodegraphFileArgsDropDotSlash(t *testing.T) {
	specs := codegraphMCPSpecs("file_symbols", `./snake/src/Game.cpp`, `./snake/src/Game.cpp`, "", 0)
	if len(specs) != 1 || specs[0].Args["file"] != "snake/src/Game.cpp" || specs[0].Args["symbolsOnly"] != true {
		t.Fatalf("file args = %#v", specs)
	}
	pinned := codegraphMCPSpecs("callers", "update", "Find callers of update", `.\snake\src\Game.cpp`, 0)
	if pinned[0].Args["file"] != "snake/src/Game.cpp" {
		t.Fatalf("pin = %#v", pinned[0].Args)
	}
	rootPin := codegraphPinFile(".")
	if rootPin != "." {
		t.Fatalf("project root pin = %q", rootPin)
	}
	cmds := codegraphCommandList("file_symbols", `./snake/src/Game.cpp`, `./snake/src/Game.cpp`, "")
	if len(cmds) != 1 || cmds[0][len(cmds[0])-1] != "snake/src/Game.cpp" {
		t.Fatalf("cli = %#v", cmds)
	}
	outside := codegraphMCPSpecs("callers", "update", "Find callers of update", "../snake/src/Game.cpp", 0)
	if outside[0].Args["file"] != nil {
		t.Fatalf("escaped pin = %#v", outside[0].Args)
	}
}

func TestCodegraphExcerptDefinesIgnoresStaleSlice(t *testing.T) {
	stale := "1247\tif (step()) {\n1248\t    drawScene(mem);\n1249\t}\n"
	if codegraphExcerptDefines(stale, "drawScene") {
		t.Fatal("a call inside another function is not a definition")
	}
	real := "1565\tstatic void drawScene(HDC dc) {\n1566\t    fill(dc);\n1567\t}\n"
	if !codegraphExcerptDefines(real, "drawScene") {
		t.Fatal("numbered definition should count")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "snake.cpp"), []byte("static void drawScene(HDC dc) {\n    fill(dc);\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repaired, ok := codegraphMCPRepair(dir, stale, "drawScene", "snake.cpp")
	if !ok || !strings.Contains(repaired, "Body for drawScene in snake.cpp.") || !strings.Contains(repaired, "fill(dc)") {
		t.Fatalf("repair = %q ok=%v", repaired, ok)
	}
	if _, ok := codegraphMCPRepair("", stale, "drawScene", "snake.cpp"); ok {
		t.Fatal("remote repair cannot read a local file and must fall back")
	}
	staleFn := "## drawScene (function)\n\n**Location:** snake.cpp:1247\n\n```cpp\n1247\tdrawScene(mem);\n```\n"
	if _, ok := codegraphMCPRepair("", staleFn, "drawScene", "snake.cpp"); ok {
		t.Fatal("a function heading whose body is a call must fall back")
	}
	kind := "## Session (struct)\n\n**Location:** session.go:10\n\n```go\n10\ttype Session struct {\n11\t    ID string\n12\t}\n```\n"
	kept, ok := codegraphMCPRepair("", kind, "Session", "")
	if !ok || !strings.Contains(kept, "type Session struct") {
		t.Fatalf("struct result = %q ok=%v", kept, ok)
	}
	banner := "⚠️ Some files referenced below were edited since the last index sync — their codegraph entries may be stale:\n  - snake.cpp (edited 10ms ago, pending sync)\n\n## drawScene (function)\n\n**Location:** snake.cpp:1\n\n```cpp\n1\tstatic void drawScene(HDC dc) { old(); }\n```\n"
	refreshed, ok := codegraphMCPRepair(dir, banner, "drawScene", "snake.cpp")
	if !ok || strings.Contains(refreshed, "old()") || !strings.Contains(refreshed, "fill(dc)") {
		t.Fatalf("stale banner repair = %q ok=%v", refreshed, ok)
	}
	if _, ok := codegraphMCPRepair("", banner, "drawScene", "snake.cpp"); ok {
		t.Fatal("stale function body without the file must fall back")
	}
	otherFile := strings.Replace(banner, "- snake.cpp (edited", "- extra.go (edited", 1)
	keptOtherFile, ok := codegraphMCPRepair(dir, otherFile, "drawScene", "snake.cpp")
	if !ok || !strings.Contains(keptOtherFile, "old()") || strings.Contains(keptOtherFile, "fill(dc)") {
		t.Fatalf("banner for another file replaced this definition: %q ok=%v", keptOtherFile, ok)
	}
	if _, ok := codegraphMCPRepair("", otherFile, "drawScene", "snake.cpp"); !ok {
		t.Fatal("banner for another file must not drop a definition that cannot be re-read")
	}
	explore := "#### snake.cpp — drawScene(function)\n\n```cpp\n1\tif (step()) {\n2\t    drawScene(mem);\n3\t}\n```\n"
	area, ok := codegraphMCPRepair(dir, explore, "", "")
	if !ok || strings.Contains(area, "drawScene(mem)") || !strings.Contains(area, "fill(dc)") {
		t.Fatalf("explore slice = %q ok=%v", area, ok)
	}
	skeleton := "#### snake.cpp — drawScene · skeleton (signatures only)\n\n```cpp\n1\tvoid drawScene(HDC dc);\n```\n"
	keptSkel, _ := codegraphMCPRepair(dir, skeleton, "", "")
	if strings.Contains(keptSkel, "fill(dc)") {
		t.Fatalf("skeleton was expanded: %s", keptSkel)
	}
	mixed := "#### snake.cpp — step(function), drawScene(function)\n\n```cpp\n1\tstatic void step() {\n2\t    move();\n3\t}\n```\n"
	both, ok := codegraphMCPRepair(dir, mixed, "", "")
	if !ok || !strings.Contains(both, "static void step") || !strings.Contains(both, "fill(dc)") {
		t.Fatalf("mixed explore = %q ok=%v", both, ok)
	}
	short := "⚠️ Some files referenced below were edited since the last index sync — their codegraph entries may be stale:\n  - snake.cpp (edited 10ms ago, pending sync)\n\n#### snake.cpp — drawScene(function)\n\n```cpp\n1\tstatic void drawScene(HDC dc) { old(); }\n```\n"
	full, ok := codegraphMCPRepair(dir, short, "", "")
	if !ok || strings.Contains(full, "old()") || !strings.Contains(full, "fill(dc)") || strings.Contains(full, "were edited since the last index sync") || strings.Contains(full, "Read them directly") {
		t.Fatalf("stale explore slice = %q ok=%v", full, ok)
	}
	told := strings.Replace(short, "pending sync)\n", "pending sync)\nFor accurate content of those specific files, Read them directly. The rest of this response is fresh.\n", 1)
	toldFull, ok := codegraphMCPRepair(dir, told, "", "")
	if !ok || strings.Contains(toldFull, "Read them directly") || strings.Contains(toldFull, "were edited since the last index sync") || !strings.Contains(toldFull, "fill(dc)") {
		t.Fatalf("refreshed explore kept the read instruction: %q ok=%v", toldFull, ok)
	}
	dotted := strings.Replace(told, "- snake.cpp (edited", "- ./Snake.CPP (edited", 1)
	dottedFull, ok := codegraphMCPRepair(dir, dotted, "", "")
	if !ok || strings.Contains(dottedFull, "Read them directly") || strings.Contains(dottedFull, "were edited since the last index sync") || strings.Contains(dottedFull, "old()") || !strings.Contains(dottedFull, "fill(dc)") {
		t.Fatalf("dotted stale banner stayed after refresh: %q ok=%v", dottedFull, ok)
	}
	other := strings.Replace(short, "- snake.cpp (edited", "- extra.go (edited", 1)
	keptOther, _ := codegraphMCPRepair(dir, other, "", "")
	if !strings.Contains(keptOther, "old()") {
		t.Fatalf("unrelated banner replaced the slice: %s", keptOther)
	}
}

func TestCodegraphStaleFileMatchesCleanPath(t *testing.T) {
	banner := "⚠️ Some files referenced below were edited since the last index sync — their codegraph entries may be stale:\n  - ./snake/src/Old.cpp (edited 10ms ago, pending sync)\n"
	if !codegraphMCPStaleFile(banner, "snake/src/Old.cpp") || !codegraphMCPStaleFile(banner, `snake\src\Old.cpp`) || !codegraphMCPStaleFile(banner, "Snake/Src/Old.cpp") {
		t.Fatal("cleaned stale path did not match")
	}
	if codegraphMCPStaleFile(banner, "Old.cpp") || codegraphMCPStaleFile(banner, "snake/src/Old.cpp.bak") || codegraphMCPStaleFile(banner, "snake/src/Old") {
		t.Fatal("a shorter or longer path matched the stale file")
	}
	mixed := strings.Replace(banner, "./snake/src/Old.cpp", `.\Snake\Src\Old.cpp`, 1)
	if !codegraphMCPStaleFile(mixed, "snake/src/Old.cpp") {
		t.Fatal("backslash banner did not match")
	}
	if codegraphMCPStaleFile("no banner", "snake/src/Old.cpp") {
		t.Fatal("a result without the stale banner matched a path")
	}
}

func TestCodegraphRPCRequestIDStaysNumeric(t *testing.T) {
	payload, err := json.Marshal(codegraphRPCRequest{JSONRPC: "2.0", ID: json.RawMessage("7"), Method: "ping"})
	if err != nil || !strings.Contains(string(payload), `"id":7`) {
		t.Fatalf("payload = %s err=%v", payload, err)
	}
	key, ok := codegraphRPCIDKey(json.RawMessage(`"cg-srv-1"`))
	if !ok || key != "cg-srv-1" {
		t.Fatalf("string id = %q ok=%v", key, ok)
	}
	key, ok = codegraphRPCIDKey(json.RawMessage("7"))
	if !ok || key != "7" {
		t.Fatalf("numeric id = %q ok=%v", key, ok)
	}
}

func TestRemoteCodegraphMCPCommandsStayOutOfTheDaemonOptOut(t *testing.T) {
	probe := remoteCodegraphIndexProbeCommand()
	if msg := rejectDisallowedCodingBashCommand(probe); msg != "" {
		t.Fatal(msg)
	}
	if !strings.Contains(probe, "_cg_has_db") || !strings.Contains(probe, "echo READY") {
		t.Fatal(probe)
	}
	serve := remoteCodegraphServeCommand("/work/demo")
	if !strings.Contains(serve, "serve --mcp --path .") || strings.Contains(serve, "CODEGRAPH_NO_DAEMON") {
		t.Fatal(serve)
	}
	if !remoteCodegraphProbeReady("READY\nEXIT: 0\n") || remoteCodegraphProbeReady("NONE\nEXIT: 0\n") {
		t.Fatal("probe ready marker")
	}
}

func codegraphStopDaemonProcess(root string) {
	data, err := os.ReadFile(filepath.Join(root, ".codegraph", "daemon.pid"))
	if err != nil {
		return
	}
	var info struct {
		Pid int `json:"pid"`
	}
	if json.Unmarshal(data, &info) != nil || info.Pid <= 1 {
		return
	}
	proc, err := os.FindProcess(info.Pid)
	if err != nil {
		return
	}
	_ = proc.Kill()
	time.Sleep(200 * time.Millisecond)
}

func TestCodegraphServeArgsSkipsCmdShim(t *testing.T) {
	bin := lookCodegraphBinary()
	if bin == "" || !strings.HasSuffix(strings.ToLower(bin), ".cmd") {
		t.Skip("codegraph cmd shim is not the local binary")
	}
	command, args := codegraphServeArgs(bin, `C:\work\demo`)
	joined := strings.Join(args, " ")
	if strings.HasSuffix(strings.ToLower(command), ".cmd") || !strings.Contains(joined, "--liftoff-only") || !strings.Contains(joined, "serve") || !strings.Contains(joined, "--mcp") {
		t.Fatalf("command = %s args = %s", command, joined)
	}
}

func TestCodegraphMCPResultTextKeepsSourceThatMentionsIndexing(t *testing.T) {
	raw := []byte(`{"content":[{"type":"text","text":"the file isn't indexed in this comment"}]}`)
	text, err := codegraphMCPResultText(raw)
	if err != nil || !strings.Contains(text, "comment") {
		t.Fatalf("text=%q err=%v", text, err)
	}
	raw = []byte("{\"content\":[{\"type\":\"text\",\"text\":\"## drawScene (function)\\n\\nstatic void drawScene() {\\n  // the project isn't indexed with codegraph\\n}\\n\"}]}")
	text, err = codegraphMCPResultText(raw)
	if err != nil || !strings.Contains(text, "drawScene") {
		t.Fatalf("source quote was treated as an unindexed project: text=%q err=%v", text, err)
	}
	raw = []byte(`{"content":[{"type":"text","text":"snake.cpp isn't indexed with codegraph. Run codegraph init."}]}`)
	if _, err := codegraphMCPResultText(raw); err == nil {
		t.Fatal("expected unindexed error")
	}
	raw = []byte(`{"content":[{"type":"text","text":"No CodeGraph project is loaded for this session."}]}`)
	if _, err := codegraphMCPResultText(raw); err == nil {
		t.Fatal("expected unindexed error")
	}
	raw = []byte(`{"content":[{"type":"text","text":"No indexed file matches \"CMakeLists.txt\". Codegraph indexes source files; configs/docs it doesn't parse won't appear — Read those directly."}]}`)
	text, err = codegraphMCPResultText(raw)
	if err != nil || !codegraphMCPIndexedFileMiss(text) {
		t.Fatalf("file miss = %q err=%v", text, err)
	}
	raw = []byte(`{"content":[{"type":"text","text":"## drawScene (function)\n\nNo indexed file matches \"render.cpp\" inside a comment\n"}]}`)
	text, err = codegraphMCPResultText(raw)
	if err != nil || codegraphMCPIndexedFileMiss(text) {
		t.Fatalf("quoted file miss inside a hit = %q err=%v", text, err)
	}
}

func TestCodegraphMCPSessionRoutesToolResult(t *testing.T) {
	responses := make(chan string, 4)
	pipe := codegraphMCPPipe{
		write: func(p []byte) error {
			if bytes.Contains(p, []byte(`"initialize"`)) {
				responses <- "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\"}}\n"
			}
			if bytes.Contains(p, []byte(`"tools/call"`)) {
				responses <- "{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"## Answer (function)\\n\\nfunc Answer() int { return 7 }\"}]}}\n"
			}
			return nil
		},
		read: func() (string, error) {
			line, ok := <-responses
			if !ok {
				return "", io.EOF
			}
			return line, nil
		},
		stderr: func() string { return "" },
	}
	session := &codegraphMCPSession{
		pipe:    pipe,
		pending: map[string]chan codegraphMCPResponse{},
		exited:  make(chan struct{}),
	}
	session.alive.Store(true)
	go session.readLoop()
	t.Cleanup(func() { session.alive.Store(false); close(responses) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := session.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	text, err := session.navigate(ctx, "definition", "Answer", "Answer", "demo.go", 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "func Answer") || !strings.Contains(text, "return 7") {
		t.Fatalf("routed = %s", text)
	}
}

func TestCodegraphMCPMissIsNotAHit(t *testing.T) {
	responses := make(chan string, 4)
	pipe := codegraphMCPPipe{
		write: func(p []byte) error {
			if bytes.Contains(p, []byte(`"initialize"`)) {
				responses <- "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\"}}\n"
			}
			if bytes.Contains(p, []byte(`"tools/call"`)) {
				responses <- "{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"No callers found for \\\"missing\\\"\"}]}}\n"
			}
			return nil
		},
		read: func() (string, error) {
			line, ok := <-responses
			if !ok {
				return "", io.EOF
			}
			return line, nil
		},
		stderr: func() string { return "" },
	}
	session := &codegraphMCPSession{
		pipe:    pipe,
		pending: map[string]chan codegraphMCPResponse{},
		exited:  make(chan struct{}),
	}
	session.alive.Store(true)
	go session.readLoop()
	t.Cleanup(func() { session.alive.Store(false); close(responses) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := session.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	text, err := session.navigate(ctx, "callers", "missing", "missing", "", 0, "")
	if err != nil || strings.TrimSpace(text) != "" {
		t.Fatalf("miss = %q err=%v", text, err)
	}
}

func TestCodegraphMCPIndexedFileMissSkipsAnotherProcess(t *testing.T) {
	responses := make(chan string, 4)
	pipe := codegraphMCPPipe{
		write: func(p []byte) error {
			if bytes.Contains(p, []byte(`"initialize"`)) {
				responses <- "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\"}}\n"
			}
			if bytes.Contains(p, []byte(`"tools/call"`)) {
				responses <- "{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"No indexed file matches \\\"CMakeLists.txt\\\". Codegraph indexes source files.\"}]}}\n"
			}
			return nil
		},
		read: func() (string, error) {
			line, ok := <-responses
			if !ok {
				return "", io.EOF
			}
			return line, nil
		},
		stderr: func() string { return "" },
	}
	session := &codegraphMCPSession{
		pipe:    pipe,
		pending: map[string]chan codegraphMCPResponse{},
		exited:  make(chan struct{}),
	}
	session.alive.Store(true)
	go session.readLoop()
	t.Cleanup(func() { session.alive.Store(false); close(responses) })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := session.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	text, err := session.navigate(ctx, "file_symbols", "CMakeLists.txt", "CMakeLists.txt", "", 0, "")
	if err != nil || !codegraphMCPIndexedFileMiss(text) || codegraphOutputUseful(text) {
		t.Fatalf("file miss = %q err=%v", text, err)
	}
}

func TestCodegraphMCPAbandonDropsPooledSession(t *testing.T) {
	session := &codegraphMCPSession{
		key:     "local\x00abandon-test",
		pending: map[string]chan codegraphMCPResponse{},
		exited:  make(chan struct{}),
	}
	close(session.exited)
	session.alive.Store(true)
	codegraphMCPMu.Lock()
	codegraphMCPPool[session.key] = session
	codegraphMCPMu.Unlock()
	session.abandon()
	codegraphMCPMu.Lock()
	_, ok := codegraphMCPPool[session.key]
	codegraphMCPMu.Unlock()
	if ok {
		t.Fatal("abandoned session stayed in the pool")
	}
}

func TestCodegraphProxyDegradedLockAborts(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, codegraphDataDirName())
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "codegraph.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]int{"pid": os.Getpid()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "daemon.pid"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	session := &codegraphMCPSession{
		root:    root,
		pending: map[string]chan codegraphMCPResponse{},
		exited:  make(chan struct{}),
		pipe: codegraphMCPPipe{
			write: func([]byte) error { return nil },
			read: func() (string, error) {
				time.Sleep(time.Hour)
				return "", io.EOF
			},
			stderr: func() string {
				return "[CodeGraph MCP] Shared daemon unavailable; serving this session in-process (degraded).\n"
			},
		},
	}
	close(session.exited)
	session.alive.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	_, err = session.callTool(ctx, "codegraph_node", map[string]interface{}{"file": "render.cpp"})
	if !errors.Is(err, errCodegraphProxyDegraded) {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("lock wait = %s", elapsed)
	}
}

func TestCodegraphProxyDegradedLockKillsBlockedProxy(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, codegraphDataDirName())
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "codegraph.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]int{"pid": os.Getpid()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "daemon.pid"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	session := &codegraphMCPSession{
		root:    root,
		pending: map[string]chan codegraphMCPResponse{},
		exited:  exited,
		pipe: codegraphMCPPipe{
			write: func([]byte) error { return nil },
			read: func() (string, error) {
				time.Sleep(time.Hour)
				return "", io.EOF
			},
			close: func() {},
			kill: func() {
				select {
				case <-exited:
				default:
					close(exited)
				}
			},
			stderr: func() string {
				return "[CodeGraph MCP] Shared daemon unavailable; serving this session in-process (degraded).\n"
			},
		},
	}
	session.alive.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	_, err = session.callTool(ctx, "codegraph_node", map[string]interface{}{"file": "render.cpp"})
	if !errors.Is(err, errCodegraphProxyDegraded) {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("blocked proxy waited %s", elapsed)
	}
}

func TestCodegraphProxyDegradedChildPidIsNotTheParentLock(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "pkg")
	parentData := filepath.Join(parent, codegraphDataDirName())
	childData := filepath.Join(child, codegraphDataDirName())
	if err := os.MkdirAll(parentData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(childData, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parentData, "codegraph.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]int{"pid": os.Getpid()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childData, "daemon.pid"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	// The answer lands after the degraded line has already been noticed. A pid
	// check that treats the child lock as the parent database returns before
	// this line is read.
	reads := 0
	session := &codegraphMCPSession{
		root:    child,
		pending: map[string]chan codegraphMCPResponse{},
		exited:  make(chan struct{}),
		pipe: codegraphMCPPipe{
			write: func([]byte) error { return nil },
			read: func() (string, error) {
				reads++
				if reads > 1 {
					time.Sleep(time.Hour)
					return "", io.EOF
				}
				time.Sleep(300 * time.Millisecond)
				return "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"func Answer() int { return 7 }\"}]}}\n", nil
			},
			stderr: func() string {
				return "[CodeGraph MCP] Shared daemon unavailable; serving this session in-process (degraded).\n"
			},
		},
	}
	close(session.exited)
	session.alive.Store(true)
	go session.readLoop()
	t.Cleanup(func() { session.alive.Store(false) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := session.callTool(ctx, "codegraph_node", map[string]interface{}{"file": "demo.go"})
	if err != nil {
		t.Fatal(err)
	}
	text, err := codegraphMCPResultText(raw)
	if err != nil || !strings.Contains(text, "return 7") {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestCodegraphProxyDegradedLockFollowsParentIndex(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "pkg")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(parent, codegraphDataDirName(), "codegraph.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]int{"pid": os.Getpid()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dbPath), "daemon.pid"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	session := &codegraphMCPSession{
		root:    child,
		pending: map[string]chan codegraphMCPResponse{},
		exited:  make(chan struct{}),
		pipe: codegraphMCPPipe{
			write: func([]byte) error { return nil },
			read: func() (string, error) {
				time.Sleep(time.Hour)
				return "", io.EOF
			},
			stderr: func() string {
				return "[CodeGraph MCP] Shared daemon unavailable; serving this session in-process (degraded).\n"
			},
		},
	}
	close(session.exited)
	session.alive.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := time.Now()
	_, err = session.callTool(ctx, "codegraph_node", map[string]interface{}{"file": "render.cpp"})
	if !errors.Is(err, errCodegraphProxyDegraded) {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("lock wait = %s", elapsed)
	}
}

func TestCodegraphProxyDegradedStillReturnsAnswer(t *testing.T) {
	responses := make(chan string, 2)
	session := &codegraphMCPSession{
		pending: map[string]chan codegraphMCPResponse{},
		exited:  make(chan struct{}),
		pipe: codegraphMCPPipe{
			write: func(p []byte) error {
				if bytes.Contains(p, []byte(`"tools/call"`)) {
					responses <- "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"func Answer() int { return 7 }\"}]}}\n"
				}
				return nil
			},
			read: func() (string, error) {
				line, ok := <-responses
				if !ok {
					return "", io.EOF
				}
				return line, nil
			},
			stderr: func() string {
				return "[CodeGraph MCP] Shared daemon unavailable; serving this session in-process (degraded).\n"
			},
		},
	}
	close(session.exited)
	session.alive.Store(true)
	go session.readLoop()
	t.Cleanup(func() {
		session.alive.Store(false)
		close(responses)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw, err := session.callTool(ctx, "codegraph_node", map[string]interface{}{"file": "demo.go"})
	if err != nil {
		t.Fatal(err)
	}
	text, err := codegraphMCPResultText(raw)
	if err != nil || !strings.Contains(text, "return 7") {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestCodegraphMCPRoundTripReadsDefinition(t *testing.T) {
	bin := lookCodegraphBinary()
	if bin == "" {
		t.Skip("codegraph is not installed")
	}
	dir, err := os.MkdirTemp("", "codegraph-mcp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		codegraphMCPStopAll()
		codegraphStopDaemonProcess(dir)
		_ = os.RemoveAll(dir)
	})
	if err := os.WriteFile(filepath.Join(dir, "demo.go"), []byte("package demo\n\nfunc Answer() int {\n\treturn 7\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if _, err := runCodegraphCommand(ctx, bin, dir, "init", "."); err != nil {
		t.Fatal(err)
	}
	text, err := codegraphMCPNavigate(dir, bin, "definition", "Answer", "Find the definition of Answer", "demo.go", 2)
	if err != nil {
		logText, readErr := os.ReadFile(filepath.Join(dir, ".codegraph", "daemon.log"))
		if len(logText) > 800 {
			logText = logText[len(logText)-800:]
		}
		t.Fatalf("%v logErr=%v log=%s", err, readErr, logText)
	}
	if !strings.Contains(text, "func Answer") || !strings.Contains(text, "return 7") {
		t.Fatalf("mcp definition = %s", text)
	}
}

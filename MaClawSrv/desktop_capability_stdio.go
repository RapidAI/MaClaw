package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/botcapability"
	"github.com/RapidAI/CodeClaw/corelib/mcp"
)

// desktopMCPStdio runs one stdio MCP method in the account's cloud desktop.
// The command and env come from the saved document. Tests replace this.
var desktopMCPStdio = runDesktopMCPStdio

func desktopMCPStdioCall(ctx context.Context, scope agentruntime.Scope, server botcapability.Server, tool string, args map[string]any) (string, error) {
	raw, err := desktopMCPStdio(ctx, scope, server, "tools/call", map[string]any{
		"name":      tool,
		"arguments": args,
	})
	if err != nil {
		return "", err
	}
	return mcp.ToolCallContent(string(raw))
}

func desktopMCPStdioList(ctx context.Context, scope agentruntime.Scope, server botcapability.Server) (string, error) {
	raw, err := desktopMCPStdio(ctx, scope, server, "tools/list", map[string]any{})
	if err != nil {
		return "", err
	}
	return mcp.FormatListedResult(raw), nil
}

func runDesktopMCPStdio(ctx context.Context, scope agentruntime.Scope, server botcapability.Server, method string, params any) (json.RawMessage, error) {
	command, args := desktopMCPCloudArgv(server.Command, server.Args)
	if err := desktopBashConfined(scope, command+"\n"+strings.Join(args, "\n")); err != nil {
		return nil, err
	}
	jobBody := map[string]any{
		"command": command,
		"args":    args,
		"env":     server.Env,
		"method":  method,
		"params":  params,
	}
	work := desktopRunWorkDir(scope)
	if work != "" {
		jobBody["cwd"] = work
	}
	job, err := json.Marshal(jobBody)
	if err != nil {
		return nil, err
	}
	if len(job) > 180000 {
		return nil, fmt.Errorf("MCP %s env is too large", server.Name)
	}
	jobPath := desktopSecretEnvPath(scope)
	outPath := jobPath + ".out"
	if _, err := desktopContainerFile(ctx, scope, "file_write", map[string]any{
		"path": jobPath, "content": string(job),
	}); err != nil {
		return nil, err
	}
	errPath := outPath + ".err"
	defer func() {
		_, _ = desktopRunContainerBash(context.WithoutCancel(ctx), scope, "rm -f "+bashSingleQuote(jobPath)+" "+bashSingleQuote(outPath)+" "+bashSingleQuote(errPath))
	}()
	script := "chmod 0600 " + bashSingleQuote(jobPath) + "\npython3 - " + bashSingleQuote(jobPath) + " " + bashSingleQuote(outPath) + " <<'PY'\n" + desktopMCPStdioPy + "\nPY\n"
	if work != "" {
		script = "mkdir -p " + bashSingleQuote(work) + "\n" + script
	}
	bashText, err := desktopContainerBash(ctx, scope, script)
	if err != nil {
		return nil, fmt.Errorf("%s", scrubMCPEnv(err.Error(), server.Env))
	}
	if strings.Contains(bashText, "command timed out") {
		return nil, fmt.Errorf("MCP %s timed out", server.Name)
	}
	text, err := desktopContainerFile(ctx, scope, "file_read", map[string]any{"path": outPath})
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &wrapped); err != nil {
		return nil, fmt.Errorf("MCP %s returned %s", server.Name, trimCapabilityResult(text))
	}
	if strings.TrimSpace(wrapped.Error) != "" {
		return nil, fmt.Errorf("MCP %s: %s", server.Name, scrubMCPEnv(wrapped.Error, server.Env))
	}
	if len(wrapped.Result) == 0 {
		return nil, fmt.Errorf("MCP %s returned no result", server.Name)
	}
	return wrapped.Result, nil
}

func scrubMCPEnv(text string, env map[string]string) string {
	return desktopScrubSecrets(text, env)
}

// desktopMCPCloudArgv turns a Windows MCP command into one the cloud desktop
// can run. The desktop image has python3, npx, and node, not python.exe,
// py, or cmd. A saved npx.cmd, a cmd /c wrapper, or a PowerShell -Command
// wrapper becomes that program. Other commands stay as saved.
func desktopMCPCloudArgv(command string, args []string) (string, []string) {
	command = strings.TrimSpace(command)
	base := command
	if i := strings.LastIndexAny(command, `/\`); i >= 0 {
		base = command[i+1:]
	}
	switch strings.ToLower(base) {
	case "cmd", "cmd.exe":
		if cmd, cmdArgs, ok := unwrapCmd(args); ok {
			return desktopMCPCloudArgv(cmd, cmdArgs)
		}
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		if cmd, cmdArgs, ok := unwrapPowerShell(args); ok {
			return desktopMCPCloudArgv(cmd, cmdArgs)
		}
	case "npx.cmd", "npx.ps1", "npx.exe":
		command = "npx"
	case "npm.cmd", "npm.ps1", "npm.exe":
		command = "npm"
	case "node.exe":
		command = "node"
	case "python", "python.exe", "python3", "python3.exe", "py", "py.exe":
		if strings.EqualFold(base, "py") || strings.EqualFold(base, "py.exe") {
			args = stripPyLauncherFlag(args)
		}
		command = "python3"
	case "pip", "pip.exe", "pip3", "pip3.exe":
		command = "pip3"
	}
	return command, args
}

func unwrapCmd(args []string) (string, []string, bool) {
	rest := args
	for len(rest) > 0 {
		flag := strings.ToLower(strings.TrimSpace(rest[0]))
		switch flag {
		case "/d", "/s", "/q", "/a", "/u", "/e:on", "/e:off":
			rest = rest[1:]
			continue
		}
		break
	}
	if len(rest) == 0 || (!strings.EqualFold(rest[0], "/c") && !strings.EqualFold(rest[0], "/k")) {
		return "", nil, false
	}
	rest = rest[1:]
	if len(rest) == 1 {
		if cmd, cmdArgs, ok := desktopMCPSplitCmdLine(rest[0]); ok {
			return cmd, cmdArgs, true
		}
	}
	if len(rest) == 0 {
		return "", nil, false
	}
	return rest[0], rest[1:], true
}

func unwrapPowerShell(args []string) (string, []string, bool) {
	rest := args
	for len(rest) > 0 {
		flag := strings.ToLower(strings.TrimSpace(rest[0]))
		switch flag {
		case "-noprofile", "-nologo", "-noninteractive", "-noexit", "-nop":
			rest = rest[1:]
			continue
		case "-executionpolicy", "-windowstyle", "-inputformat", "-outputformat":
			if len(rest) < 2 {
				return "", nil, false
			}
			rest = rest[2:]
			continue
		case "-command", "-c":
			rest = rest[1:]
			if len(rest) == 0 || strings.HasPrefix(strings.TrimSpace(rest[0]), "{") {
				return "", nil, false
			}
			if len(rest) == 1 {
				return desktopMCPSplitCmdLine(rest[0])
			}
			return rest[0], rest[1:], true
		default:
			return "", nil, false
		}
	}
	return "", nil, false
}

// stripPyLauncherFlag drops the Windows py launcher's version switch.
// python3 rejects -3 and -3.11.
func stripPyLauncherFlag(args []string) []string {
	if len(args) == 0 || len(args[0]) < 2 || args[0][0] != '-' {
		return args
	}
	rest := args[0][1:]
	if rest == "0" || rest == "2" || rest == "3" {
		return args[1:]
	}
	if len(rest) > 2 && (rest[0] == '0' || rest[0] == '2' || rest[0] == '3') && rest[1] == '.' {
		for _, c := range rest[2:] {
			if c < '0' || c > '9' {
				return args
			}
		}
		return args[1:]
	}
	return args
}

// desktopMCPSplitCmdLine splits one "npx -y pkg" argument. Quotes keep a
// path with spaces as one word. An unquoted path stays one argument.
func desktopMCPSplitCmdLine(line string) (string, []string, bool) {
	fields, ok := splitCmdWords(line)
	if !ok || len(fields) < 2 {
		return "", nil, false
	}
	if !strings.ContainsAny(line, `"'`) && strings.ContainsAny(fields[0], `/\`) {
		return "", nil, false
	}
	return fields[0], fields[1:], true
}

func splitCmdWords(line string) ([]string, bool) {
	var words []string
	var b strings.Builder
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			b.WriteByte(c)
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case ' ', '\t':
			if b.Len() > 0 {
				words = append(words, b.String())
				b.Reset()
			}
		default:
			b.WriteByte(c)
		}
	}
	if quote != 0 {
		return nil, false
	}
	if b.Len() > 0 {
		words = append(words, b.String())
	}
	if len(words) == 0 {
		return nil, false
	}
	return words, true
}

const desktopMCPStdioPy = `
import json, os, signal, subprocess, sys, threading, time
job_path, out_path = sys.argv[1], sys.argv[2]
err_path = out_path + ".err"
with open(job_path, encoding="utf-8") as fh:
    job = json.load(fh)
os.remove(job_path)
proc = None
err_fh = open(err_path, "wb")

def ppid_of(raw):
    cut = raw.rfind(b")")
    if cut < 0:
        return None
    parts = raw[cut + 2:].split()
    if len(parts) < 2:
        return None
    return parts[1]

def child_pids(pid):
    kids = []
    if os.name != "posix":
        return kids
    want = str(pid).encode()
    try:
        names = os.listdir("/proc")
    except Exception:
        return kids
    for name in names:
        if not name.isdigit():
            continue
        try:
            raw = open("/proc/" + name + "/stat", "rb").read()
        except Exception:
            continue
        if ppid_of(raw) == want:
            child = int(name)
            if child > 1:
                kids.append(child)
    return kids

def descendant_pids(pid, seen=None):
    if seen is None:
        seen = set()
    out = []
    for child in child_pids(pid):
        if child in seen:
            continue
        seen.add(child)
        out.append(child)
        out.extend(descendant_pids(child, seen))
    return out

def stop():
    if proc is None:
        return
    extra = descendant_pids(proc.pid)
    try:
        os.killpg(os.getpgid(proc.pid), signal.SIGKILL)
    except Exception:
        try:
            proc.kill()
        except Exception:
            pass
    for pid in extra:
        try:
            os.kill(pid, signal.SIGKILL)
        except Exception:
            pass
    try:
        proc.wait(timeout=2)
    except Exception:
        pass

def stderr_tail():
    try:
        err_fh.flush()
        with open(err_path, "rb") as eh:
            eh.seek(0, os.SEEK_END)
            size = eh.tell()
            eh.seek(max(0, size - 2000))
            return eh.read(2000).decode("utf-8", "replace").strip()
    except Exception:
        return ""

def fail(msg):
    text = str(msg or "").strip()
    logged = stderr_tail()
    if text in ("", "mcp process ended"):
        text = logged
    elif logged and logged not in text:
        text = text + "\n" + logged
    if not text:
        text = "mcp process ended"
    with open(out_path, "w", encoding="utf-8") as fh:
        json.dump({"error": text[:4000]}, fh)
    stop()
    try:
        os.remove(err_path)
    except Exception:
        pass
    sys.exit(0)

env = os.environ.copy()
env.setdefault("PYTHONUNBUFFERED", "1")

def usable_env(key, value):
    low = str(key).lower()
    if low in ("pathext", "systemroot", "windir", "comspec"):
        return False
    text = str(value)
    if low == "path" and (";" in text or "\\" in text or (len(text) > 1 and text[1] == ":" and text[0].isalpha())):
        return False
    return True

for key, value in (job.get("env") or {}).items():
    if usable_env(key, value):
        env[str(key)] = str(value)
cwd = job.get("cwd") or None
if cwd and not os.path.isdir(cwd):
    cwd = None

def die_with_parent():
    try:
        import ctypes
        ctypes.CDLL("libc.so.6", use_errno=True).prctl(1, 9)
    except Exception:
        pass

restartable = False

class MCPRestart(Exception):
    pass

def launch():
    global proc, pending
    stop()
    pending = b""
    try:
        pop = {"stdin": subprocess.PIPE, "stdout": subprocess.PIPE, "stderr": err_fh, "env": env, "cwd": cwd, "bufsize": 0, "start_new_session": True}
        if os.name == "posix":
            pop["preexec_fn"] = die_with_parent
        proc = subprocess.Popen([job["command"], *(job.get("args") or [])], **pop)
    except Exception as exc:
        fail(exc)

def ended():
    if restartable:
        raise MCPRestart()
    fail("mcp process ended")

def parent_gone(pid):
    if os.name == "posix":
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            return True
        except OSError:
            return False
        return False
    try:
        import ctypes
        kernel = ctypes.windll.kernel32
        handle = kernel.OpenProcess(0x1000, 0, pid)
        if not handle:
            return True
        code = ctypes.c_ulong()
        ok = kernel.GetExitCodeProcess(handle, ctypes.byref(code))
        kernel.CloseHandle(handle)
        return (not ok) or code.value != 259
    except Exception:
        return False

def watch_parent(parent):
    while True:
        time.sleep(0.5)
        if os.getppid() == parent and not parent_gone(parent):
            continue
        stop()
        os._exit(0)

threading.Thread(target=watch_parent, args=(os.getppid(),), daemon=True).start()
launch()

def send(obj):
    proc.stdin.write(json.dumps(obj).encode() + b"\n")
    proc.stdin.flush()

def fill(need):
    global pending
    while len(pending) < need:
        chunk = proc.stdout.read(4096)
        if not chunk:
            return False
        pending += chunk
    return True

def same_id(got, want):
    return got == want or str(got) == str(want)

def server_request(msg):
    if not isinstance(msg, dict) or "method" not in msg or msg.get("id") is None:
        return False
    if "result" in msg or "error" in msg:
        return False
    method = msg.get("method")
    if method in ("initialize", "tools/list", "tools/call", "notifications/initialized"):
        return False
    if method == "ping":
        send({"jsonrpc": "2.0", "id": msg["id"], "result": {}})
    elif method == "roots/list":
        send({"jsonrpc": "2.0", "id": msg["id"], "result": {"roots": []}})
    else:
        send({"jsonrpc": "2.0", "id": msg["id"], "error": {"code": -32601, "message": "Method not found"}})
    return True

def matched(msg, want_id, state):
    if server_request(msg):
        state[0] += 1
        if state[0] > 32:
            fail("mcp server sent too many requests")
        return False
    return isinstance(msg, dict) and same_id(msg.get("id"), want_id) and ("result" in msg or "error" in msg)

def read_msg(want_id):
    global pending
    state = [0]
    while True:
        if not pending and not fill(1):
            ended()
        pending = pending.lstrip(b" \r\n\t")
        if not pending:
            continue
        head = pending[:20].lower()
        if head.startswith(b"content-length:") or head.startswith(b"content-type:"):
            while b"\r\n\r\n" not in pending:
                if not fill(len(pending) + 1):
                    ended()
                if len(pending) > 8192:
                    fail("mcp response is too large")
            mark = pending.find(b"\r\n\r\n")
            size = None
            for line in pending[:mark].split(b"\r\n"):
                if line.lower().startswith(b"content-length:"):
                    size = int(line.split(b":", 1)[1].strip())
                    break
            if size is None:
                fail("mcp response is missing content-length")
            if size < 0 or size > 1500000:
                fail("mcp response is too large")
            pending = pending[mark + 4:]
            while len(pending) < size:
                if not fill(size):
                    ended()
            body, pending = pending[:size], pending[size:]
            msg = json.loads(body)
            if matched(msg, want_id, state):
                return msg
            continue
        while b"\n" not in pending:
            if not fill(len(pending) + 1):
                ended()
        line, pending = pending.split(b"\n", 1)
        text = line.strip()
        if not text:
            continue
        try:
            msg = json.loads(text)
        except Exception:
            continue
        if matched(msg, want_id, state):
            return msg

def rpc(method, params, req_id):
    send({"jsonrpc": "2.0", "id": req_id, "method": method, "params": params})
    msg = read_msg(req_id)
    if msg.get("error"):
        err = msg["error"]
        fail(err.get("message") if isinstance(err, dict) else err)
    return msg.get("result")

def slim(value):
    if isinstance(value, dict):
        out = {}
        for key, item in value.items():
            if key == "data" and value.get("type") in ("image", "audio") and isinstance(item, str) and len(item) > 200:
                out[key] = ""
            else:
                out[key] = slim(item)
        return out
    if isinstance(value, list):
        return [slim(item) for item in value]
    return value

def initialize():
    global restartable
    restartable = True
    last = "mcp process ended"
    try:
        versions = ((5, "2025-06-18"), (1, "2025-03-26"), (3, "2024-11-05"))
        for attempt, (req_id, version) in enumerate(versions):
            try:
                send({"jsonrpc": "2.0", "id": req_id, "method": "initialize", "params": {"protocolVersion": version, "capabilities": {}, "clientInfo": {"name": "maclaw", "version": "1.0.0"}}})
                msg = read_msg(req_id)
            except (MCPRestart, BrokenPipeError, OSError):
                last = stderr_tail() or "mcp process ended"
                launch()
                continue
            if not msg.get("error"):
                send({"jsonrpc": "2.0", "method": "notifications/initialized"})
                return
            err = msg.get("error")
            last = err.get("message") if isinstance(err, dict) else err
            if attempt + 1 < len(versions):
                launch()
        fail(last)
    finally:
        restartable = False

def finish(result):
    payload = json.dumps({"result": result}, ensure_ascii=False)
    if len(payload.encode("utf-8")) > 160000:
        text = json.dumps(result, ensure_ascii=False)[:20000]
        payload = json.dumps({"result": {"content": [{"type": "text", "text": text + "\n…truncated"}]}}, ensure_ascii=False)
    with open(out_path, "w", encoding="utf-8") as fh:
        fh.write(payload)
    stop()
    try:
        os.remove(err_path)
    except Exception:
        pass

try:
    initialize()
    method = job.get("method") or ""
    params = job.get("params") or {}
    if method == "tools/list":
        tools, cursor, seen_cursor, seen_name = [], "", set(), set()
        for _ in range(5):
            if cursor and cursor in seen_cursor:
                break
            if cursor:
                seen_cursor.add(cursor)
            page = dict(params)
            if cursor:
                page["cursor"] = cursor
            result = rpc("tools/list", page, 2) or {}
            for tool in result.get("tools") or []:
                name = str((tool or {}).get("name") or "")
                if name and name not in seen_name:
                    seen_name.add(name)
                    tools.append(tool)
            nxt = str(result.get("nextCursor") or "").strip()
            if not nxt or nxt == cursor:
                break
            cursor = nxt
        result = {"tools": tools}
    else:
        result = rpc(method, params, 2)
    finish(slim(result))
except SystemExit:
    raise
except Exception as exc:
    fail(exc)
`

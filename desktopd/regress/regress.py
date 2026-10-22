#!/usr/bin/env python3
"""Offline computer-use regression set for a MaClaw *test* desktop container.

Runs inside the container (run.sh copies it in). It serves local fixtures on
127.0.0.1 (no internet), drives the owner Chromium over CDP and the X display
with xdotool/xclip, and checks each task with a programmatic verifier.
Prints one JSON report; exit status 0 only when every task passes.

Tasks:
  editor_dom_write_detected  a Draft.js-like editor ignores a DOM write and
                             the verify-after-act read-back reports not_applied
  editor_real_typing         real xdotool keystrokes reach the editor model
  editor_clipboard_paste     xclip + real Ctrl+V reaches the editor model
  form_fill                  CDP insertText + click fills and submits a form
  cross_origin_navigation    a second origin opens in its own tab
  nav_ready_and_login_wall   navigate readiness probe: a normal page is ready,
                             a sign-in form / 安全验证 page / blank page are
                             classified as walls (login_form, captcha, blank)
"""
import base64, http.server, json, os, socket, socketserver, struct, subprocess, sys, threading, time, urllib.request

CDP_PORT = int(os.environ.get("REGRESS_CDP_PORT", "18020"))
DISPLAY = os.environ.get("REGRESS_DISPLAY", ":20")
PORT_A, PORT_B = 0, 0  # picked free at start

EDITOR = """<!doctype html><meta charset=utf-8><title>editor</title>
<div id=ed contenteditable=true style="width:600px;height:200px;border:1px solid #888;font-size:20px"></div>
<script>
// Like Draft.js: the model is the truth, the DOM is redrawn from it. Only
// trusted input events change the model; any other DOM change is undone.
window.__model = "";
const ed = document.getElementById("ed");
let drawing = false;
function draw(){ drawing = true; ed.textContent = window.__model; drawing = false;
  const r = document.createRange(); r.selectNodeContents(ed); r.collapse(false);
  const s = getSelection(); if (document.activeElement === ed) { s.removeAllRanges(); s.addRange(r); } }
ed.addEventListener("beforeinput", e => {
  if (!e.isTrusted) { e.preventDefault(); return; }
  e.preventDefault();
  if (e.inputType === "insertText" || e.inputType === "insertFromPaste" || e.inputType === "insertReplacementText") {
    const t = e.data || (e.dataTransfer && e.dataTransfer.getData("text/plain")) || "";
    window.__model += t;
  } else if (e.inputType === "deleteContentBackward") { window.__model = window.__model.slice(0, -1); }
  draw();
});
ed.addEventListener("paste", e => { if (!e.isTrusted) { e.preventDefault(); return; }
  e.preventDefault(); window.__model += e.clipboardData.getData("text/plain"); draw(); });
new MutationObserver(() => { if (!drawing && ed.textContent !== window.__model) setTimeout(draw, 0); })
  .observe(ed, {childList: true, characterData: true, subtree: true});
</script>"""

FORM = """<!doctype html><meta charset=utf-8><title>form</title>
<form id=f onsubmit="event.preventDefault();document.getElementById('result').textContent='ok:'+n.value+':'+m.value">
<input id=n name=n placeholder=name><input id=m name=m placeholder=email><button id=go>提交</button></form>
<div id=result></div>"""

SIGNIN = """<!doctype html><meta charset=utf-8><title>登录 - 知乎</title>
<form><input name=u placeholder=手机号><input type=password name=p><button>登录</button></form>"""

CAPTCHA = """<!doctype html><meta charset=utf-8><title>知乎</title><div>系统检测到您的帐号或IP存在异常流量，请进行安全验证</div><button>验证</button>"""

BLANK = """<!doctype html><meta charset=utf-8><title>知乎 - 知乎</title><div id=root></div>"""

# Same expression as corelib/browser/navigate_ready.go pageReadyExpr.
PAGE_READY_JS = r"""(() => {
  const sel = 'a[href],button,input:not([type=hidden]),textarea,select,[contenteditable=""],[contenteditable=true],[role=button],[role=textbox],[role=link],[tabindex]:not([tabindex="-1"])';
  let n = 0;
  try {
    for (const el of document.querySelectorAll(sel)) {
      const r = el.getBoundingClientRect();
      if (r.width > 1 && r.height > 1) { n++; if (n >= 50) break; }
    }
  } catch (e) {}
  const body = document.body ? (document.body.innerText || '') : '';
  const head = body.slice(0, 4000);
  let wall = '';
  if (/安全验证|请完成验证|人机验证|滑动验证|拖动滑块|captcha|are you a robot|verify you are human/i.test(head)) wall = 'captcha';
  else if (document.querySelector('input[type=password]') && /登录|注册|sign ?in|log ?in/i.test(head + ' ' + document.title)) wall = 'login_form';
  return JSON.stringify({rs: document.readyState, n, url: location.href, title: document.title || '', tl: body.length, wall});
})()"""

OTHER = """<!doctype html><meta charset=utf-8><title>other-origin</title><h1 id=h>second origin</h1>"""

# Same read-back as corelib/browser/verify.go readbackJS(wide=false).
READBACK_JS = r"""(function(){const out=[];const cap=s=>(s||"").slice(0,20000);
let d=document,el=d.activeElement;
const textOf=e=>{if(!e)return"";if(e.tagName==="INPUT"||e.tagName==="TEXTAREA")return e.value||"";const r=e.closest&&e.closest("[contenteditable=''],[contenteditable=true],[contenteditable=plaintext-only]");return (r||e).innerText||(r||e).textContent||""};
if(el&&el!==d.body)out.push(cap(textOf(el)));return JSON.stringify(out)})()"""


def norm(s):
    return "".join(ch.lower() for ch in s if not ch.isspace() and ch not in "#*_`>~|[]()\u00a0\u200b")


def verdict(expected, contents):
    probe = norm(expected)[:24]
    if len(probe) < 2:
        return "unknown"
    return "applied" if any(probe in norm(c) for c in contents) else "not_applied"


class Handler(http.server.BaseHTTPRequestHandler):
    pages = {}

    def do_GET(self):
        body = self.pages.get(self.path.split("?")[0], "").encode()
        self.send_response(200 if body else 404)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *a):
        pass


def serve(port, pages):
    socketserver.ThreadingTCPServer.allow_reuse_address = True
    handler = type("H%d" % port, (Handler,), {"pages": pages})
    srv = socketserver.ThreadingTCPServer(("127.0.0.1", port), handler)
    srv.daemon_threads = True
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    return srv.server_address[1]


class CDP:
    """Minimal CDP client over a raw WebSocket (stdlib only)."""

    def __init__(self, ws_url):
        hostport, path = ws_url.split("://", 1)[1].split("/", 1)
        host, port = hostport.split(":")
        self.s = socket.create_connection((host, int(port)), timeout=15)
        key = base64.b64encode(os.urandom(16)).decode()
        self.s.sendall(("GET /%s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"
                        "Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n" % (path, hostport, key)).encode())
        head = b""
        while b"\r\n\r\n" not in head:
            head += self.s.recv(1)
        if b" 101 " not in head.split(b"\r\n")[0]:
            raise RuntimeError("cdp upgrade failed: %r" % head[:80])
        self.buf, self.n = b"", 0

    def _recv(self, n):
        while len(self.buf) < n:
            d = self.s.recv(65536)
            if not d:
                raise EOFError
            self.buf += d
        r, self.buf = self.buf[:n], self.buf[n:]
        return r

    def _frame(self):
        h = self._recv(2)
        n = h[1] & 127
        if n == 126:
            n = struct.unpack(">H", self._recv(2))[0]
        elif n == 127:
            n = struct.unpack(">Q", self._recv(8))[0]
        return h[0] & 15, self._recv(n)

    def call(self, method, **params):
        self.n += 1
        payload = json.dumps({"id": self.n, "method": method, "params": params}).encode()
        mask = os.urandom(4)
        n = len(payload)
        head = bytes([0x81]) + (bytes([0x80 | n]) if n < 126 else bytes([0xFE]) + struct.pack(">H", n) if n < 65536 else bytes([0xFF]) + struct.pack(">Q", n))
        self.s.sendall(head + mask + bytes(b ^ mask[i % 4] for i, b in enumerate(payload)))
        while True:
            op, data = self._frame()
            if op != 1:
                continue
            msg = json.loads(data)
            if msg.get("id") == self.n:
                if "error" in msg:
                    raise RuntimeError("%s: %s" % (method, msg["error"]))
                return msg.get("result", {})

    def eval(self, expr):
        r = self.call("Runtime.evaluate", expression=expr, returnByValue=True, awaitPromise=True)
        return r.get("result", {}).get("value")


def cdp_json(path, method="GET"):
    req = urllib.request.Request("http://127.0.0.1:%d%s" % (CDP_PORT, path), method=method)
    return json.loads(urllib.request.urlopen(req, timeout=10).read())


def new_tab(url):
    t = cdp_json("/json/new?" + url, method="PUT")
    c = CDP(t["webSocketDebuggerUrl"])
    c.call("Page.enable")
    c.call("Page.bringToFront")
    for _ in range(50):
        if c.eval("document.readyState") == "complete":
            break
        time.sleep(0.1)
    return t, c


def close_tab(t):
    try:
        cdp_json("/json/close/" + t["id"])
    except Exception:
        pass


def x(*args, stdin=None):
    env = dict(os.environ, DISPLAY=DISPLAY)
    return subprocess.run(args, input=stdin, env=env, capture_output=True, text=True, timeout=20)


def click_element(c, selector):
    box = c.eval("(()=>{const r=document.querySelector(%s).getBoundingClientRect();return [r.x+r.width/2,r.y+r.height/2]})()" % json.dumps(selector))
    for kind in ("mousePressed", "mouseReleased"):
        c.call("Input.dispatchMouseEvent", type=kind, x=box[0], y=box[1], button="left", clickCount=1)


def focus_browser_window():
    wid = x("xdotool", "search", "--onlyvisible", "--class", "chromium").stdout.split()
    if wid:
        x("xdotool", "windowactivate", "--sync", wid[-1])


TEXT = "hello regress 你好"
ASCII = "hello regress"


def task_editor_dom_write_detected():
    t, c = new_tab("http://127.0.0.1:%d/editor" % PORT_A)
    try:
        click_element(c, "#ed")
        c.eval("document.getElementById('ed').innerText=%s" % json.dumps(TEXT))
        time.sleep(0.3)
        got = verdict(TEXT, json.loads(c.eval(READBACK_JS)))
        model = c.eval("window.__model")
        return {"ok": got == "not_applied" and model == "", "readback": got, "model": model}
    finally:
        close_tab(t)


def task_editor_real_typing():
    t, c = new_tab("http://127.0.0.1:%d/editor" % PORT_A)
    try:
        focus_browser_window()
        click_element(c, "#ed")
        x("xdotool", "type", "--delay", "30", ASCII)
        time.sleep(0.5)
        model = c.eval("window.__model")
        got = verdict(ASCII, json.loads(c.eval(READBACK_JS)))
        return {"ok": model == ASCII and got == "applied", "model": model, "readback": got}
    finally:
        close_tab(t)


def task_editor_clipboard_paste():
    t, c = new_tab("http://127.0.0.1:%d/editor" % PORT_A)
    try:
        focus_browser_window()
        click_element(c, "#ed")
        # xclip stays behind to own the selection; no pipes, or run() waits.
        clip = subprocess.run(["xclip", "-selection", "clipboard"], input=TEXT.encode(), env=dict(os.environ, DISPLAY=DISPLAY),
                              stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=10)
        x("xdotool", "key", "--clearmodifiers", "ctrl+v")
        time.sleep(0.5)
        model = c.eval("window.__model")
        return {"ok": model == TEXT, "model": model, "xclip_rc": clip.returncode}
    finally:
        close_tab(t)


def task_form_fill():
    t, c = new_tab("http://127.0.0.1:%d/form" % PORT_A)
    try:
        click_element(c, "#n")
        c.call("Input.insertText", text="张三")
        click_element(c, "#m")
        c.call("Input.insertText", text="z@example.test")
        click_element(c, "#go")
        time.sleep(0.3)
        result = c.eval("document.getElementById('result').textContent")
        return {"ok": result == "ok:张三:z@example.test", "result": result}
    finally:
        close_tab(t)


def task_cross_origin_navigation():
    a, ca = new_tab("http://127.0.0.1:%d/form" % PORT_A)
    b = None
    try:
        b, cb = new_tab("http://localhost:%d/other" % PORT_B)
        title = cb.eval("document.title")
        tabs = [p.get("url", "") for p in cdp_json("/json/list") if p.get("type") == "page"]
        ok = title == "other-origin" and any(u.startswith("http://127.0.0.1:%d/" % PORT_A) for u in tabs) and any(u.startswith("http://localhost:%d/" % PORT_B) for u in tabs)
        return {"ok": ok, "title": title}
    finally:
        close_tab(a)
        if b:
            close_tab(b)


def task_nav_ready_and_login_wall():
    got = {}
    for path in ("/form", "/signin", "/captcha", "/blank"):
        t, c = new_tab("http://127.0.0.1:%d%s" % (PORT_A, path))
        try:
            time.sleep(0.5)
            st = json.loads(c.eval(PAGE_READY_JS))
            wall = st["wall"] or ("blank" if st["n"] == 0 and st["tl"] < 20 else "")
            got[path] = {"rs": st["rs"], "n": st["n"], "wall": wall}
        finally:
            close_tab(t)
    ok = (got["/form"]["wall"] == "" and got["/form"]["n"] > 0 and got["/form"]["rs"] == "complete"
          and got["/signin"]["wall"] == "login_form" and got["/captcha"]["wall"] == "captcha" and got["/blank"]["wall"] == "blank")
    return {"ok": ok, "pages": got}


TASKS = [task_editor_dom_write_detected, task_editor_real_typing, task_editor_clipboard_paste, task_form_fill, task_cross_origin_navigation,
         task_nav_ready_and_login_wall]


def main():
    global PORT_A, PORT_B
    PORT_A = serve(0, {"/editor": EDITOR, "/form": FORM, "/signin": SIGNIN, "/captcha": CAPTCHA, "/blank": BLANK})
    PORT_B = serve(0, {"/other": OTHER})
    cdp_json("/json/version")
    report, passed = [], 0
    only = set(sys.argv[1:])
    for task in TASKS:
        name = task.__name__[len("task_"):]
        if only and name not in only:
            continue
        started = time.time()
        try:
            res = task()
        except Exception as exc:
            res = {"ok": False, "error": "%s: %s" % (type(exc).__name__, exc)}
        res["task"], res["seconds"] = name, round(time.time() - started, 2)
        passed += bool(res.get("ok"))
        report.append(res)
    print(json.dumps({"passed": passed, "total": len(report), "tasks": report}, ensure_ascii=False, indent=1))
    return 0 if passed == len(report) else 1


if __name__ == "__main__":
    sys.exit(main())

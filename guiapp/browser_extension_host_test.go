package guiapp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

func TestCompanionPaperHostArgv(t *testing.T) {
	id := "chrome-extension://" + browserExtensionID + "/"
	if !isCompanionPaperHostArgv([]string{"MaClaw.exe", id, "--parent-window=0"}) {
		t.Fatal("chrome origin was not a host launch")
	}
	if !isCompanionPaperHostArgv([]string{"MaClaw.exe", companionPaperHostArg}) {
		t.Fatal("companion-paper-host was not a host launch")
	}
	if !isCompanionPaperHostArgv([]string{filepath.Join("C:\\Program Files\\MaClaw", "maclaw-companion-host.exe")}) {
		t.Fatal("helper filename was not a host launch")
	}
	if isCompanionPaperHostArgv([]string{"MaClaw.exe", "--parent-window=0"}) {
		t.Fatal("--parent-window=0 alone entered the host")
	}
	if isCompanionPaperHostArgv([]string{"MaClaw.exe", "open-file", "a.pdf"}) {
		t.Fatal("open-file entered the host")
	}
}

func TestSanitizeCompanionPaperTitle(t *testing.T) {
	got := sanitizeCompanionPaperTitle("A\nB\t" + strings.Repeat("文", 250))
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("controls kept: %q", got)
	}
	if n := len([]rune(got)); n != 200 {
		t.Fatalf("scalars=%d", n)
	}
}

func TestCompanionPaperHelloDoesNotDownload(t *testing.T) {
	dir := t.TempDir()
	prevDir := browserExtensionStateDirHook
	browserExtensionStateDirHook = func() (string, error) { return dir, nil }
	prevExec := companionPaperExecHook
	companionPaperExecHook = func(argv []string) error {
		t.Fatalf("exec %#v", argv)
		return nil
	}
	prevFetch := paperFetchHook
	paperFetchHook = func(context.Context, string, *websearch.FetchOptions) (*websearch.FetchResult, error) {
		t.Fatal("hello downloaded a paper")
		return nil, nil
	}
	t.Cleanup(func() {
		browserExtensionStateDirHook = prevDir
		companionPaperExecHook = prevExec
		paperFetchHook = prevFetch
	})
	edgeAt := "2026-01-02T03:04:05Z"
	if err := writeBrowserExtensionState(dir, browserExtensionStateFile{
		Edge: &browserExtensionBrowserRecord{Version: "0.9.0", HelloAt: edgeAt, RegisteredAt: "2026-01-01T00:00:00Z", BrowserRunningAtRegister: true},
	}); err != nil {
		t.Fatal(err)
	}
	reply := roundTripHost(t, map[string]any{"hello": true, "browser": "chrome", "version": browserExtensionVersion})
	if reply["ok"] != true || reply["code"] != nil {
		t.Fatalf("reply=%#v", reply)
	}
	state := readBrowserExtensionState(dir)
	if state.Chrome == nil || state.Chrome.Version != browserExtensionVersion || state.Chrome.HelloAt == "" {
		t.Fatalf("chrome=%#v", state.Chrome)
	}
	if state.Chrome.RegisteredAt != "" || state.Chrome.BrowserRunningAtRegister {
		t.Fatalf("hello wrote registration %#v", state.Chrome)
	}
	if state.Edge == nil || state.Edge.Version != "0.9.0" || state.Edge.HelloAt != edgeAt || state.Edge.RegisteredAt != "2026-01-01T00:00:00Z" || !state.Edge.BrowserRunningAtRegister {
		t.Fatalf("edge changed %#v", state.Edge)
	}
	if _, err := os.Stat(filepath.Join(dir, "browser-extension-state.json")); !os.IsNotExist(err) {
		t.Fatal("hello recreated the state json")
	}
}

func TestCompanionPaperHelloRejectsAndSafariWritesNothing(t *testing.T) {
	dir := t.TempDir()
	prevDir := browserExtensionStateDirHook
	browserExtensionStateDirHook = func() (string, error) { return dir, nil }
	prevExec := companionPaperExecHook
	companionPaperExecHook = func([]string) error {
		t.Fatal("exec")
		return nil
	}
	t.Cleanup(func() {
		browserExtensionStateDirHook = prevDir
		companionPaperExecHook = prevExec
	})
	cases := []map[string]any{
		{"hello": true, "browser": "chrome", "version": browserExtensionVersion, "url": "https://example.org"},
		{"hello": true, "browser": "chrome", "version": ""},
		{"hello": true, "browser": "chrome", "version": strings.Repeat("v", 33)},
		{"hello": true, "browser": "firefox", "version": "1"},
	}
	for _, msg := range cases {
		reply := roundTripHost(t, msg)
		if reply["ok"] != false || reply["code"] != companionPaperCodeSaveFailed {
			t.Fatalf("%#v reply=%#v", msg, reply)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "browser-extension-state.json")); !os.IsNotExist(err) {
		t.Fatal("rejected hello created the state file")
	}
	if _, err := os.Stat(filepath.Join(dir, "browser-extension-state.db")); !os.IsNotExist(err) {
		t.Fatal("rejected hello created the state database")
	}
	reply := roundTripHost(t, map[string]any{"hello": true, "browser": "safari", "version": browserExtensionVersion})
	if reply["ok"] != true {
		t.Fatalf("safari reply=%#v", reply)
	}
	if _, err := os.Stat(filepath.Join(dir, "browser-extension-state.json")); !os.IsNotExist(err) {
		t.Fatal("safari hello created the state file")
	}
	if _, err := os.Stat(filepath.Join(dir, "browser-extension-state.db")); !os.IsNotExist(err) {
		t.Fatal("safari hello created the state database")
	}
}

func TestCompanionPaperHostPanicWritesSaveFailed(t *testing.T) {
	prev := browserExtensionStateDirHook
	browserExtensionStateDirHook = func() (string, error) { panic("boom") }
	t.Cleanup(func() { browserExtensionStateDirHook = prev })
	body, _ := json.Marshal(map[string]any{"hello": true, "browser": "chrome", "version": "1.0.0"})
	var in bytes.Buffer
	_ = binary.Write(&in, binary.LittleEndian, uint32(len(body)))
	in.Write(body)
	var out bytes.Buffer
	if code := runCompanionPaperHostIO(&in, &out); code == 0 {
		t.Fatal("panic returned success")
	}
	var n uint32
	if err := binary.Read(&out, binary.LittleEndian, &n); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, n)
	if _, err := out.Read(buf); err != nil {
		t.Fatal(err)
	}
	var reply map[string]any
	if err := json.Unmarshal(buf, &reply); err != nil {
		t.Fatal(err)
	}
	if reply["ok"] != false || reply["code"] != companionPaperCodeSaveFailed {
		t.Fatalf("reply=%#v", reply)
	}
}

func TestCompanionPaperFrameDiscardsTitle(t *testing.T) {
	dir := t.TempDir()
	restorePaperHooks(t, dir, func(_ context.Context, rawURL string, opts *websearch.FetchOptions) (*websearch.FetchResult, error) {
		if strings.Contains(rawURL, "Secret Title") {
			t.Fatalf("title entered the URL %s", rawURL)
		}
		if err := os.WriteFile(opts.SavePath, []byte("%PDF-1.4 paper"), 0o644); err != nil {
			return nil, err
		}
		return &websearch.FetchResult{SavedTo: opts.SavePath, ContentType: "application/pdf"}, nil
	})
	prevExec := companionPaperExecHook
	var execs [][]string
	companionPaperExecHook = func(argv []string) error {
		execs = append(execs, append([]string(nil), argv...))
		return nil
	}
	t.Cleanup(func() { companionPaperExecHook = prevExec })
	reply := roundTripHost(t, map[string]any{
		"url":   "https://aclanthology.org/2024.acl-long.1.pdf",
		"title": "Secret Title\n" + strings.Repeat("名", 300),
	})
	if reply["ok"] != true {
		t.Fatalf("reply=%#v", reply)
	}
	name := companionPaperFileName(canonicalPaperURL("https://aclanthology.org/2024.acl-long.1.pdf"))
	if len(execs) != 1 || !strings.HasSuffix(execs[0][2], name) || strings.Contains(execs[0][2], "Secret") {
		t.Fatalf("exec=%#v", execs)
	}
}

func TestBrowserExtensionIDMatchesManifest(t *testing.T) {
	text := readRepoFile(t, filepath.Join("extensions", "companion-paper", "manifest.json"))
	var manifest struct {
		Version string `json:"version"`
		Key     string `json:"key"`
	}
	if err := json.Unmarshal([]byte(text), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != browserExtensionVersion {
		t.Fatalf("manifest version %s != %s", manifest.Version, browserExtensionVersion)
	}
	id, err := chromeExtensionIDFromManifestKey(manifest.Key)
	if err != nil {
		t.Fatal(err)
	}
	if id != browserExtensionID {
		t.Fatalf("id %s != %s", id, browserExtensionID)
	}
}

func roundTripHost(t *testing.T, payload any) map[string]any {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var in bytes.Buffer
	if err := binary.Write(&in, binary.LittleEndian, uint32(len(body))); err != nil {
		t.Fatal(err)
	}
	in.Write(body)
	var out bytes.Buffer
	if code := runCompanionPaperHostIO(&in, &out); code != 0 {
		t.Fatalf("host code=%d out=%q", code, out.Bytes())
	}
	var n uint32
	if err := binary.Read(&out, binary.LittleEndian, &n); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, n)
	if _, err := out.Read(buf); err != nil {
		t.Fatal(err)
	}
	var reply map[string]any
	if err := json.Unmarshal(buf, &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

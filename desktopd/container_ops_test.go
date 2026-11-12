package desktopd

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestContainerFileAndBashStayInTheUsersContainer(t *testing.T) {
	var commands [][]string
	var stdin string
	svc := &Service{
		Run: func(_ context.Context, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "inspect" {
				return "true", nil
			}
			return "", nil
		},
		RunCommand: func(_ context.Context, in io.Reader, args ...string) (string, int, error) {
			commands = append(commands, append([]string(nil), args...))
			if in != nil {
				raw, _ := io.ReadAll(in)
				stdin = string(raw)
			}
			joined := strings.Join(args, " ")
			switch {
			case strings.Contains(joined, " curl "):
				return "hello" + "\n__MACLAW_HTTP__" + "200 text/html", 0, nil
			case strings.Contains(joined, " /desktop_supervisor.py command "):
				return "hi\n", 0, nil
			case strings.Contains(joined, " file read "):
				return "int main(){}\n", 0, nil
			default:
				return "", 0, nil
			}
		},
	}
	text, err := svc.WriteFile(context.Background(), "tenant", "alice", "~/Desktop/a.c", "int main(){}\n")
	if err != nil || !strings.Contains(text, "/home/desktop/Desktop/a.c") || stdin != "int main(){}\n" {
		t.Fatalf("write=%q stdin=%q err=%v", text, stdin, err)
	}
	if !strings.Contains(strings.Join(commands[0], " "), " file write ") || commands[0][len(commands[0])-1] != "/home/desktop/Desktop/a.c" {
		t.Fatalf("write args=%v", commands[0])
	}
	text, err = svc.ReadFile(context.Background(), "tenant", "alice", "/root/Desktop/a.c")
	if err != nil || text != "int main(){}\n" {
		t.Fatalf("read=%q err=%v", text, err)
	}
	text, err = svc.Bash(context.Background(), "tenant", "alice", "echo hi")
	if err != nil || text != "hi" {
		t.Fatalf("bash=%q err=%v", text, err)
	}
	body, status, kind, err := svc.HTTPGet(context.Background(), "tenant", "alice", "https://example.com")
	if err != nil || body != "hello" || status != 200 || kind != "text/html" {
		t.Fatalf("http body=%q status=%d kind=%q err=%v", body, status, kind, err)
	}
	if _, err := svc.WriteFile(context.Background(), "tenant", "alice", `C:\Windows\a.c`, "x"); err == nil {
		t.Fatal("host path")
	}
	if len(commands) != 4 {
		t.Fatalf("docker calls=%d", len(commands))
	}
}

func TestHTTPGetDoesNotExecAStoppedContainer(t *testing.T) {
	called := false
	svc := &Service{
		Run: func(_ context.Context, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "inspect" {
				return "", errors.New("no such container")
			}
			return "", nil
		},
		RunCommand: func(context.Context, io.Reader, ...string) (string, int, error) {
			called = true
			return "should-not-run", 0, nil
		},
	}
	_, _, _, err := svc.HTTPGet(context.Background(), "tenant", "alice", "https://example.com")
	if !errors.Is(err, ErrDesktopAsleep) || called {
		t.Fatalf("err=%v exec=%v", err, called)
	}
}

func TestHTTPGetWakesWhenTheWorkspaceIsOff(t *testing.T) {
	running := false
	mounts := readyMounts(t, "tenant", "alice")
	var starts int
	svc := &Service{
		EnforceWakeReason: true,
		Run: func(_ context.Context, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "{{.State.Running}}") && !strings.Contains(joined, "maclaw.shm") {
				if running {
					return "true", nil
				}
				return "false", nil
			}
			if len(args) > 0 && args[0] == "inspect" {
				state := "false"
				if running {
					state = "true"
				}
				return state + "|1g|" + mounts + "|maclaw-gui:2|maclaw-gui:2|", nil
			}
			if len(args) > 0 && args[0] == "start" {
				starts++
				running = true
				return "", nil
			}
			return "", nil
		},
		RunCommand: func(_ context.Context, _ io.Reader, args ...string) (string, int, error) {
			if !running || !strings.Contains(strings.Join(args, " "), " curl ") {
				t.Fatalf("curl before the desktop was up: %v", args)
			}
			return "hello" + "\n__MACLAW_HTTP__" + "200 text/plain", 0, nil
		},
	}
	body, status, kind, err := svc.HTTPGet(context.Background(), "tenant", "alice", "https://example.com/papers")
	if err != nil || body != "hello" || status != 200 || kind != "text/plain" || starts != 1 {
		t.Fatalf("body=%q status=%d kind=%q starts=%d err=%v", body, status, kind, starts, err)
	}
}

func TestHTTPGetWarmStartStaysRetryable(t *testing.T) {
	t0 := time.Date(2026, 11, 12, 1, 0, 0, 0, time.UTC)
	var tick int
	curled := false
	mounts := readyMounts(t, "tenant", "alice")
	svc := &Service{
		EnforceWakeReason: true,
		Now: func() time.Time {
			tick++
			return t0.Add(time.Duration(tick) * 30 * time.Second)
		},
		Run: func(_ context.Context, args ...string) (string, error) {
			joined := strings.Join(args, " ")
			if strings.Contains(joined, "{{.State.Running}}") && !strings.Contains(joined, "maclaw.shm") {
				return "false", nil
			}
			if len(args) > 0 && args[0] == "inspect" {
				return "false|1g|" + mounts + "|maclaw-gui:2|maclaw-gui:2|", nil
			}
			return "", nil
		},
		RunCommand: func(context.Context, io.Reader, ...string) (string, int, error) {
			curled = true
			return "should-not-run", 0, nil
		},
	}
	_, _, _, err := svc.HTTPGet(context.Background(), "tenant", "alice", "https://example.com/papers")
	wake, ok := err.(WarmStartError)
	if !ok || !wake.Retryable() || curled {
		t.Fatalf("err=%v curled=%v", err, curled)
	}
}

func TestHTTPGetMachineRuntimeWakesInsteadOfAsleep(t *testing.T) {
	var reason string
	ensured := 0
	svc := &Service{
		ToolWorkspace: true,
		Machines: &wakeTape{ensure: func(_ context.Context, spec Spec) error {
			ensured++
			reason = spec.WakeReason
			return WarmStartError{}
		}},
		Run: func(_ context.Context, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "inspect" {
				return "", errors.New("no such container")
			}
			return "", nil
		},
		RunCommand: func(context.Context, io.Reader, ...string) (string, int, error) {
			t.Fatal("curl ran without a wake")
			return "", 0, nil
		},
	}
	_, _, _, err := svc.HTTPGet(context.Background(), "tenant", "alice", "https://example.com/papers")
	if _, ok := err.(WarmStartError); !ok || ensured != 1 || reason != WakeReasonBotDesktop {
		t.Fatalf("err=%v ensured=%d reason=%q", err, ensured, reason)
	}
}

func TestHTTPRouteReturnsWakingWhileTheDesktopStarts(t *testing.T) {
	svc := &Service{
		Machines: &wakeTape{ensure: func(context.Context, Spec) error { return WarmStartError{} }},
		Run: func(_ context.Context, args ...string) (string, error) {
			if len(args) > 0 && args[0] == "inspect" {
				return "", errors.New("no such container")
			}
			return "", nil
		},
	}
	h := Handler(svc, "tok", "", nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/desktops/http", strings.NewReader(`{"tenant_id":"tenant","user_id":"alice","url":"https://example.com/papers"}`))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"code":"DESKTOP_WAKING"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body["retryable"] != true {
		t.Fatalf("body=%s err=%v", rec.Body.String(), err)
	}
}

func TestReadBytesKeepsAPDFIntact(t *testing.T) {
	raw := append([]byte("%PDF-1.4\n"), 0xff, 0xfe)
	encoded := base64.StdEncoding.EncodeToString(raw)
	var commands [][]string
	svc := &Service{RunCommand: func(_ context.Context, _ io.Reader, args ...string) (string, int, error) {
		commands = append(commands, append([]string(nil), args...))
		return encoded, 0, nil
	}}
	got, err := svc.ReadBytes(context.Background(), "tenant", "alice", "~/Desktop/北京天气.pdf")
	if err != nil || got != encoded {
		t.Fatalf("bytes=%q err=%v", got, err)
	}
	joined := strings.Join(commands[0], " ")
	if !strings.Contains(joined, " file bytes ") || !strings.Contains(joined, "/home/desktop/Desktop/北京天气.pdf") || strings.Contains(joined, "base64") {
		t.Fatalf("args=%v", commands[0])
	}
	svc.RunCommand = func(context.Context, io.Reader, ...string) (string, int, error) {
		return base64.StdEncoding.EncodeToString(make([]byte, desktop.FileBytesMax+1)), 0, nil
	}
	if _, err := svc.ReadBytes(context.Background(), "tenant", "alice", "~/Desktop/北京天气.pdf"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("large err=%v", err)
	}
}

func TestBashFailureStaysInTheToolOutput(t *testing.T) {
	svc := &Service{RunCommand: func(context.Context, io.Reader, ...string) (string, int, error) {
		return "a.c:1: error: expected ';'\n", 1, nil
	}}
	text, err := svc.Bash(context.Background(), "tenant", "alice", "gcc a.c")
	if err != nil || !strings.Contains(text, "expected ';'") || !strings.Contains(text, "(exit 1)") {
		t.Fatalf("bash=%q err=%v", text, err)
	}
}

func TestContainerEditHoldsTheFileAcrossTheRewrite(t *testing.T) {
	var stdin string
	svc := &Service{RunCommand: func(_ context.Context, in io.Reader, args ...string) (string, int, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, " file read ") {
			return "int main(){}\n", 0, nil
		}
		if in != nil {
			raw, _ := io.ReadAll(in)
			stdin = string(raw)
		}
		return "", 0, nil
	}}
	text, err := svc.EditFile(context.Background(), "tenant", "alice", "~/Desktop/a.c", "int main(){}", "int main(void){}")
	if err != nil || !strings.Contains(text, "/home/desktop/Desktop/a.c") || stdin != "int main(void){}\n" {
		t.Fatalf("edit=%q stdin=%q err=%v", text, stdin, err)
	}
}

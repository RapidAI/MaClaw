package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHubInstallDocIsTheRunbook(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/docs/hub-install", nil)
	rec := httptest.NewRecorder()
	serveHubInstallDoc(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatal(rec.Header().Get("Content-Type"))
	}
	body := rec.Body.String()
	for _, phrase := range []string{
		"git clone",
		"go build",
		"/opt/maclaw-hub/maclaw-hub",
		"run-systemd.sh",
		"register_on_startup",
		"owner_email",
		"/api/admin/setup",
		"/healthz",
		"center_registration",
		"POST /api/hubs/register",
		"pending_confirmation",
		"不要打印 `hub_secret`",
		"不要把 systemd 的 `ExecStart` 指到 `start.sh`",
		"Wants=network-online.target",
		"-ge 90",
		"注册失败要重启 Hub 进程",
		"first_center",
		"9399/tcp",
		"ENABLED=yes",
		`"service":"maclaw-hub"`,
		"hub is still running",
		"already has a hub identity",
		"`installation_id` 在第一次尝试注册时就会写入",
		"recovery_secret:",
		"GOSUMDB=sum.golang.google.cn",
		"invalid public base url",
		"safe.directory=/opt/maclaw-src",
		"admin-setup.result",
		"rm -f /opt/maclaw-hub/data/admin-setup.result",
		"-buildvcs=false",
		"ADMIN_SETUP_MARK",
	} {
		if !strings.Contains(body, phrase) {
			t.Fatalf("runbook missing %q", phrase)
		}
	}
	if strings.Contains(body, "git pull --ff-only") {
		t.Fatal("a shallow clone must fetch and reset, not pull")
	}
	if strings.Contains(body, "firewall-cmd --reload") {
		t.Fatal("reloading firewalld drops the SSH session used for the install")
	}
	mkdirAt := strings.Index(body, "mkdir -p /opt/maclaw-hub")
	buildAt := strings.Index(body, "go build")
	if mkdirAt < 0 || buildAt < 0 || mkdirAt > buildAt {
		t.Fatal("the output directory must be created before go build")
	}
}

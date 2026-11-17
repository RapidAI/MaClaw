package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBotnodeDocIsTheRunbook(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/docs/botnode", nil)
	rec := httptest.NewRecorder()
	serveBotnodeDoc(rec, req)
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
		"BOTNODE_ENROLL_CODE",
		"HUB_EDGE_NODES",
		"/opt/maclaw-node/botnode",
		"DESKTOPD_INTRANET=1",
		"MACLAW_TOKEN_SECRET",
		"git -C /opt/maclaw-src fetch --depth 1 origin",
		"git -C /opt/maclaw-src reset --hard FETCH_HEAD",
		"GOPROXY=https://goproxy.cn,direct",
		"BOTNODE_SRV_ADDR=off",
		"只安装 desktopd",
		"生成内网注册码",
		"轮换密钥",
		"健康检查通过之后再发注册码",
		"DESKTOPD_HUB_LINK=off",
		"Bot 节点",
		"systemctl stop maclaw-botnode",
		"systemctl restart maclaw-srv",
		"宿主机不用安装 Python",
		"maclaw-gui:2",
	} {
		if !strings.Contains(body, phrase) {
			t.Fatalf("runbook missing %q", phrase)
		}
	}
	if strings.Contains(body, "git pull --ff-only") {
		t.Fatal("a shallow clone must fetch and reset, not pull")
	}
	mkdirAt := strings.Index(body, "mkdir -p /opt/maclaw-node")
	buildAt := strings.Index(body, "go build")
	if mkdirAt < 0 || buildAt < 0 || mkdirAt > buildAt {
		t.Fatal("the output directory must be created before go build")
	}
	if strings.Contains(body, "DESKTOPD_ADVERTISE_HOST=") || strings.Contains(body, "DESKTOPD_PUBLIC_URL=") {
		t.Fatal("an intranet desktop must not publish a public address")
	}
}

func TestSelfDesktopdDocIsTheInstallRunbook(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/docs/self-desktopd", nil)
	rec := httptest.NewRecorder()
	serveSelfDesktopdDoc(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	for _, phrase := range []string{
		"只安装 desktopd",
		"一并安装 desktopd 和 MaClawSrv",
		"DESKTOPD_INTRANET=1",
		"DESKTOPD_DEPLOY_DIR=/data/soft/maclaw_desktopd",
		"MACLAW_TOKEN_SECRET",
		"不要创建 .env",
		"/data/soft/maclaw_srv/bin/maclawsrv",
		"不要自己向 Hub 注册",
		"remote_deploy.sh",
		"DESKTOPD_IMAGE_FROM=build",
		"safe.directory=/opt/maclaw-src",
		"+refs/tags/${tag}:refs/tags/${tag}",
		"refs/tags/${tag}^{}",
		"checkout is not the release tag",
		"-buildvcs=false",
		"https://api.github.com/repos/RapidAI/MaClaw/releases/latest",
		"tag_name",
		`\1/p`,
		"url_effective",
		"宿主机不用安装 Python",
		"maclaw-gui:2",
		"cp -R desktopd/image/.",
		"theme/",
		"GOTOOLCHAIN=local",
		"GOSUMDB=sum.golang.google.cn",
	} {
		if !strings.Contains(body, phrase) {
			t.Fatalf("runbook missing %q", phrase)
		}
	}
	if strings.Contains(body, "DESKTOPD_ADVERTISE_HOST=") || strings.Contains(body, "DESKTOPD_PUBLIC_URL=") {
		t.Fatal("the self-install runbook must not assign a public address")
	}
	if strings.Contains(body, "--branch main") || strings.Contains(body, "origin main") || strings.Contains(body, "reset --hard FETCH_HEAD") || strings.Contains(body, "sort -V") {
		t.Fatal("intranet desktop install must use the latest release tag")
	}
	if strings.Contains(body, "desktopd/image/desktop_supervisor.py desktopd/image/Dockerfile") {
		t.Fatal("image context must be the whole desktopd/image tree")
	}
}

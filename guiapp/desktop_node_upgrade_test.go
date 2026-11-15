package guiapp

import (
	"fmt"
	"strings"
	"testing"
)

func TestDesktopNodeUpgradePromptRebuildsLatestAndKeepsToken(t *testing.T) {
	rec := DesktopNodeRecord{
		Host: "10.0.0.8", Port: 22, User: "deploy",
		DeployDir: "/data/soft/maclaw_desktopd", DesktopdPort: 18081,
	}
	stamp := "abc123stamp"
	prompt := desktopNodeUpgradePrompt(rec, "maclaw-desktopd-1", desktopNodePrivilege{sudo: true}, stamp)

	for _, want := range []string{
		"label=\"maclaw-desktopd-1\"",
		"不要传 host/user/password",
		"升级到最新代码",
		"git -C /opt/maclaw-src fetch --depth 1 origin",
		"git -C /opt/maclaw-src reset --hard FETCH_HEAD",
		"DESKTOPD_IMAGE_FROM=build",
		"DESKTOPD_INTRANET=1",
		"不要改 .env",
		"DESKTOPD_TOKEN",
		"/data/soft/maclaw_desktopd/state",
		"rm -f /data/soft/maclaw_desktopd/.upgrade-stamp",
		"printf '%s\\n' " + stamp,
		"DESKTOPD_DEPLOY: OK",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "password=") || strings.Contains(strings.ToLower(prompt), "secret-password") {
		t.Fatal("upgrade prompt must not carry a password")
	}
	if strings.Contains(prompt, "DESKTOPD_ADVERTISE_HOST="+rec.Host) {
		t.Fatal("an intranet desktop must not be advertised at its own address")
	}
	if !strings.Contains(prompt, "DESKTOPD_ADVERTISE_HOST=127.0.0.1") {
		t.Fatal("an older install script still requires a name; the only allowed fallback is loopback")
	}
	deploy := desktopNodeDeployPrompt(rec, "maclaw-desktopd-1", desktopNodePrivilege{sudo: true})
	if strings.Contains(deploy, "DESKTOPD_ADVERTISE_HOST="+rec.Host) || !strings.Contains(deploy, "DESKTOPD_INTRANET=1") {
		t.Fatal("deploy prompt must stay an intranet install")
	}
	if strings.Contains(prompt, "sed -i") || strings.Contains(deploy, "sed -i") {
		t.Fatal("the agent must not edit .env; the client removes the public name")
	}
}

func TestDesktopNodeIntranetEnvFixLeavesTheTokenAlone(t *testing.T) {
	if desktopNodeIntranetReady(map[string]string{"DESKTOPD_INTRANET": "true"}) != true {
		t.Fatal("true is intranet")
	}
	if desktopNodeIntranetReady(map[string]string{"DESKTOPD_INTRANET": "1", "DESKTOPD_ADVERTISE_HOST": "127.0.0.1"}) {
		t.Fatal("a leftover public name still has to be removed")
	}
	if desktopNodeIntranetReady(map[string]string{"DESKTOPD_INTRANET": "0"}) {
		t.Fatal("0 is not intranet")
	}
	cmd := intranetDesktopEnvCommand("/data/soft/maclaw_desktopd")
	if strings.Contains(cmd, "DESKTOPD_TOKEN") || !strings.Contains(cmd, "systemctl restart maclaw-desktopd") {
		t.Fatalf("command = %s", cmd)
	}
	if desktopNodeFatal(fmt.Errorf("desktopd 正在重启为内网桌面")) {
		t.Fatal("restart is retried by the next probe")
	}
	if desktopNodeFatal(fmt.Errorf("还没有 /data/soft/maclaw_desktopd/.env")) {
		t.Fatal("the env file appears only after the install script creates it")
	}
	if !desktopNodeFatal(fmt.Errorf("还没有连接 Hub")) {
		t.Fatal("a missing hub login is not cured by probing again")
	}
	if desktopNodeFatal(fmt.Errorf("desktopd 正在安装")) || desktopNodeFatal(fmt.Errorf("升级还未完成")) {
		t.Fatal("a probe while the install is still running must not stop the job")
	}
	cmdRestart := intranetDesktopEnvCommand("/data/soft/maclaw_desktopd")
	if !strings.HasPrefix(cmdRestart, "(") || !strings.Contains(cmdRestart, ") && chmod 600 ") {
		t.Fatalf("restart must wait until the env edit succeeds: %s", cmdRestart)
	}
}

func TestDesktopNodeJobDoesNotRecreateOrDoubleBook(t *testing.T) {
	a := &App{testHomeDir: t.TempDir()}
	if _, err := a.updateDesktopNode("gone", func(rec *DesktopNodeRecord) { rec.Status = desktopNodeStatusReady }); err == nil {
		t.Fatal("update recreated a deleted desktop")
	}
	rec, err := a.insertDesktopNode(DesktopNodeRecord{
		ID: "dn_lab", Name: "lab", Mode: "ssh", Host: "10.0.0.8", User: "deploy", Status: desktopNodeStatusReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteDesktopNode(rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.updateDesktopNode(rec.ID, func(r *DesktopNodeRecord) {
		r.Status = desktopNodeStatusUpgrading
		r.Message = "agent 正在用最新代码升级 desktopd"
	}); err == nil {
		t.Fatal("a finished job wrote the deleted desktop back")
	}
	if !claimDesktopNodeJob("dn_live") {
		t.Fatal("first claim should succeed")
	}
	defer releaseDesktopNodeJob("dn_live")
	if claimDesktopNodeJob("dn_live") {
		t.Fatal("second claim should fail while the job is running")
	}
	releaseDesktopNodeJob("dn_live")
	if !claimDesktopNodeJob("dn_live") {
		t.Fatal("claim should succeed after release")
	}
	releaseDesktopNodeJob("dn_live")
}

func TestSettleDesktopNodeJobKeepsALiveUpgrade(t *testing.T) {
	records := []DesktopNodeRecord{
		{ID: "dn_dead", Status: desktopNodeStatusUpgrading, Host: "10.0.0.8"},
		{ID: "dn_live", Status: desktopNodeStatusUpgrading, Host: "10.0.0.9"},
		{ID: "dn_ok", Status: desktopNodeStatusReady, Host: "10.0.0.10"},
	}
	if !claimDesktopNodeJob("dn_live") {
		t.Fatal("claim")
	}
	defer releaseDesktopNodeJob("dn_live")
	settled, changed := settleDesktopNodeJobs(records)
	if !changed {
		t.Fatal("expected the dead job to be settled")
	}
	if settled[0].Status != desktopNodeStatusFailed || settled[0].Message == "" {
		t.Fatalf("dead job: %+v", settled[0])
	}
	if settled[1].Status != desktopNodeStatusUpgrading {
		t.Fatalf("live job was settled: %+v", settled[1])
	}
	if settled[2].Status != desktopNodeStatusReady {
		t.Fatalf("ready desktop changed: %+v", settled[2])
	}
}

func TestDesktopNodeFatalLeavesABlipRetryable(t *testing.T) {
	if desktopNodeFatal(fmt.Errorf("desktopd 不可达")) {
		t.Fatal("a dropped connection should be tried again")
	}
	if desktopNodeFatal(fmt.Errorf("desktopd 已升级，正在重新连接 Hub")) {
		t.Fatal("a reconnecting hub link should be tried again")
	}
	if !desktopNodeFatal(fmt.Errorf("Hub 拒绝了桌面注册")) {
		t.Fatal("a hub rejection should stop the job")
	}
	if !desktopNodeFatal(desktopNodeRegisterError{msg: "desktopd 拒绝了访问令牌"}) {
		t.Fatal("a rejected token should stop the job")
	}
}

package guiapp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestDesktopNodeUpgradePromptRebuildsLatestAndKeepsToken(t *testing.T) {
	rec := DesktopNodeRecord{
		Host: "10.0.0.8", Port: 22, User: "deploy",
		HubURL:    "https://hub.example.test",
		DeployDir: "/data/soft/maclaw_desktopd", DesktopdPort: 18081,
	}
	stamp := "abc123stamp"
	doc := "https://hub.example.test/docs/self-desktopd"
	prompt := desktopNodeUpgradePrompt(rec, "maclaw-desktopd-1", desktopNodePrivilege{sudo: true}, stamp)

	for _, want := range []string{
		"label=\"maclaw-desktopd-1\"",
		"不要传 host/user/password",
		doc,
		"更新步骤按文档第 4 节",
		"不要改 .env",
		"DESKTOPD_TOKEN",
		"rm -f /data/soft/maclaw_desktopd/.upgrade-stamp",
		"printf '%s\\n' " + stamp,
		"DESKTOPD_DEPLOY: OK",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "password=") || strings.Contains(strings.ToLower(prompt), "secret-password") || strings.Contains(prompt, "hub.mypapers.top") {
		t.Fatal("upgrade prompt must not carry a password or a hardcoded hub")
	}
	if strings.Contains(prompt, "DESKTOPD_ADVERTISE_HOST="+rec.Host) {
		t.Fatal("an intranet desktop must not be advertised at its own address")
	}
	deploy := desktopNodeDeployPrompt(rec, "maclaw-desktopd-1", desktopNodePrivilege{sudo: true})
	if !strings.Contains(deploy, doc) || !strings.Contains(deploy, "这次的安装选择：只安装 desktopd") {
		t.Fatal("deploy prompt must point at the self-desktopd runbook and name the desktop-only choice")
	}
	if strings.Contains(deploy, "DESKTOPD_ADVERTISE_HOST="+rec.Host) || strings.Contains(deploy, "hub.mypapers.top") {
		t.Fatal("deploy prompt must stay on the logged-in hub and must not advertise this machine")
	}
	if strings.Contains(prompt, "sed -i") || strings.Contains(deploy, "sed -i") {
		t.Fatal("the agent must not edit .env; the client removes the public name")
	}
	if strings.Contains(deploy, "maclawsrv") {
		t.Fatal("desktop-only deploy must not install MaClawSrv")
	}
	rec.WithSrv = true
	withSrv := desktopNodeDeployPrompt(rec, "maclaw-desktopd-1", desktopNodePrivilege{sudo: true})
	if !strings.Contains(withSrv, "一并安装 desktopd 和 MaClawSrv") || !strings.Contains(withSrv, "不要创建 .env") {
		t.Fatal("srv install must leave secrets to the desktop page")
	}
	cmd := srvHubTokenWriteCommand("/data/soft/maclaw_srv", "hub-token-secret-value")
	if strings.Contains(cmd, "hub-token-secret-value") || !strings.Contains(cmd, "MACLAW_DESKTOP_API_TOKEN") {
		t.Fatal("the token write must not carry the raw hub token")
	}
	envCmd := srvEnvWriteCommand("/data/soft/maclaw_srv", "admin-secret-value", "token-secret-value", "https://hub.example")
	if strings.Contains(envCmd, "admin-secret-value") || strings.Contains(envCmd, "token-secret-value") {
		t.Fatal("the admin secret must not appear in the remote command")
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
	if desktopNodeFatal(fmt.Errorf("desktopd 正在安装")) || desktopNodeFatal(fmt.Errorf("升级还未完成")) || desktopNodeFatal(fmt.Errorf("升级还未完成: 标记还不是这次升级")) {
		t.Fatal("a probe while the install is still running must not stop the job")
	}
	if desktopNodeFatal(fmt.Errorf("还没有 /data/soft/maclaw_desktopd/.env: /data/soft/maclaw_desktopd/.env: 文件是空的")) {
		t.Fatal("an empty env file is still the install catching up")
	}
	if desktopNodeFatal(fmt.Errorf("maclawsrv 正在安装: exit status 1")) {
		t.Fatal("a missing maclawsrv binary is still the install catching up")
	}
	cmdRestart := intranetDesktopEnvCommand("/data/soft/maclaw_desktopd")
	if !strings.HasPrefix(cmdRestart, "(") || !strings.Contains(cmdRestart, ") && chmod 600 ") {
		t.Fatalf("restart must wait until the env edit succeeds: %s", cmdRestart)
	}
}

func TestLocalDesktopJobsStayOnTheMonitorUntilFinished(t *testing.T) {
	a := &App{testHomeDir: t.TempDir()}
	if !claimDesktopNodeJob("dn_lab") {
		t.Fatal("could not claim the live install")
	}
	defer releaseDesktopNodeJob("dn_lab")
	_, err := a.insertDesktopNode(DesktopNodeRecord{
		ID: "dn_lab", Name: "lab", Mode: "ssh", Host: "192.168.1.49", User: "znsoft",
		Status: desktopNodeStatusDeploying, WithSrv: true, Token: "super-secret-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.insertDesktopNode(DesktopNodeRecord{
		ID: "dn_idle", Name: "idle", Mode: "ssh", Status: desktopNodeStatusReady, Token: "other-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.insertDesktopNode(DesktopNodeRecord{
		ID: "dn_orphan", Name: "orphan", Mode: "ssh", Host: "10.0.0.9", User: "old",
		Status: desktopNodeStatusDeploying,
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := a.ListLocalDesktopJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != "dn_lab" || jobs[0].Status != desktopNodeStatusDeploying {
		t.Fatalf("jobs = %+v", jobs)
	}
	orphan, err := a.findDesktopNode("dn_orphan")
	if err != nil {
		t.Fatal(err)
	}
	if orphan.Status != desktopNodeStatusFailed || orphan.JobStartedAt != "" || !strings.Contains(orphan.Message, "已中断") {
		t.Fatalf("orphan = %+v", orphan)
	}
	if jobs[0].Title != "部署 desktopd 和 MaClawSrv · znsoft@192.168.1.49" || jobs[0].StartedAt == "" {
		t.Fatalf("title = %+v", jobs[0])
	}
	raw, err := json.Marshal(jobs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("job list leaked a token: %s", raw)
	}
	a.setDesktopNodeStatus("dn_lab", desktopNodeStatusUpgrading, "agent 正在用最新代码升级 desktopd")
	jobs, err = a.ListLocalDesktopJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Title != "更新 desktopd 和 MaClawSrv · znsoft@192.168.1.49" {
		t.Fatalf("upgrade job = %+v", jobs)
	}
	a.setDesktopNodeStatus("dn_lab", desktopNodeStatusReady, "")
	jobs, err = a.ListLocalDesktopJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("finished job stayed listed: %+v", jobs)
	}
}

func TestSrvTokenWriteOnAnUpgradeStaysAnUpgrade(t *testing.T) {
	a := &App{testHomeDir: t.TempDir()}
	_, err := a.insertDesktopNode(DesktopNodeRecord{
		ID: "dn_up", Name: "lab", Mode: "ssh", Host: "10.0.0.8", User: "deploy",
		Status: desktopNodeStatusUpgrading, WithSrv: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.updateDesktopNode("dn_up", func(r *DesktopNodeRecord) {
		kind := desktopNodeOutcomeKind(r.Status)
		r.SrvTokenSet = true
		r.Status = desktopNodeStatusReady
		r.Message = ""
		recordDesktopNodeResult(r, kind, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.findDesktopNode("dn_up")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastKind != "upgrade" || !strings.Contains(got.LastResult, "更新完成") {
		t.Fatalf("result = %+v", got)
	}
	if desktopNodeOutcomeKind(desktopNodeStatusRegistering) != "deploy" {
		t.Fatal("a first registration is a deploy")
	}
}

func TestDesktopNodeResultStaysUntilCleared(t *testing.T) {
	a := &App{testHomeDir: t.TempDir()}
	rec, err := a.insertDesktopNode(DesktopNodeRecord{
		ID: "dn_lab", Name: "lab", Mode: "ssh", Host: "10.0.0.8", User: "deploy", Status: desktopNodeStatusDeploying,
	})
	if err != nil {
		t.Fatal(err)
	}
	a.setDesktopNodeStatus(rec.ID, desktopNodeStatusReady, "")
	got, err := a.findDesktopNode(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != desktopNodeStatusReady || got.LastKind != "deploy" || got.LastResult == "" || got.Message != "" {
		t.Fatalf("result = %+v", got)
	}
	if strings.Contains(got.LastResult, "token") {
		t.Fatalf("result leaked a token word: %s", got.LastResult)
	}
	if err := a.ClearDesktopNodeResult(rec.ID); err != nil {
		t.Fatal(err)
	}
	got, err = a.findDesktopNode(rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastResult != "" || got.LastKind != "" || got.Status != desktopNodeStatusReady {
		t.Fatalf("cleared = %+v", got)
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

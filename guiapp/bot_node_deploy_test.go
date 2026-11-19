package guiapp

import (
	"strings"
	"testing"
)

func TestBotNodePromptRegistersToTheConnectedHub(t *testing.T) {
	rec := BotNodeRecord{
		ID:      "bn_prompt",
		Host:    "203.0.113.20",
		Port:    22,
		User:    "ubuntu",
		WithSrv: true,
		HubURL:  "https://hub.example.test",
	}
	code := "AABBCCDDEEFF0011"
	prompt := botNodePrompt(rec, "ssh-secret", desktopNodePrivilege{sudo: true}, code, false)
	for _, phrase := range []string{
		"https://hub.example.test/docs/botnode",
		"HUB_URL=https://hub.example.test",
		"WITH_SRV=1",
		"ENROLL_CODE=" + code,
		"node_id",
		"不要向 Hub Center 注册",
		"不要写 BOTNODE_ENROLL_CODE",
		"不要把 BOTNODE_SRV_ADDR 写成 off",
		"MACLAW_DESKTOP_API_TOKEN",
		"BOTNODE_INSTALL: OK",
		"ssh-secret",
	} {
		if !strings.Contains(prompt, phrase) {
			t.Fatalf("prompt missing %q", phrase)
		}
	}
	if strings.Contains(prompt, "DESKTOPD_ADVERTISE_HOST=") || strings.Contains(prompt, "DESKTOPD_PUBLIC_URL=") || strings.Contains(prompt, "BOTNODE_SRV_ADDR=off") {
		t.Fatal("prompt tells the agent to set a public desktop URL or to turn MaClawSrv off")
	}
	display := botNodeDisplay(rec.User, rec.Host)
	if strings.Contains(display, "ssh-secret") || strings.Contains(display, code) {
		t.Fatal("the visible row contains a secret")
	}
	again := botNodePrompt(rec, "ssh-secret", desktopNodePrivilege{root: true}, "0011223344556677", true)
	if !strings.Contains(again, "不要用旧码重试") || !strings.Contains(again, "ENROLL_CODE=0011223344556677") {
		t.Fatal(again)
	}
}

func TestBotNodeProbeWaitsForHubRegistration(t *testing.T) {
	healthy := `{"desktopd":true,"node_id":"","botnode":false,"enroll_left":false,"srv":false,"srv_token":false}`
	if _, _, err := botNodeProbeDecision(healthy, false, "https://hub.example.test", false); err == nil || !strings.Contains(err.Error(), "正在注册") {
		t.Fatal(err)
	}
	if _, _, err := botNodeProbeDecision(healthy, false, "https://hub.example.test", true); err == nil || !strings.Contains(err.Error(), "注册失败") || strings.Contains(err.Error(), "正在") {
		t.Fatal(err)
	}
	registered := `{"desktopd":true,"node_id":"node-1","hub_url":"https://hub.example.test","botnode":true,"enroll_left":true,"srv":false,"srv_token":false}`
	if _, _, err := botNodeProbeDecision(registered, false, "https://hub.example.test", false); err == nil || !strings.Contains(err.Error(), "正在注册") {
		t.Fatal(err)
	}
	id, left, err := botNodeProbeDecision(registered, false, "https://hub.example.test", true)
	if err != nil || id != "node-1" || !left {
		t.Fatalf("id=%s left=%v err=%v", id, left, err)
	}
	done := `{"desktopd":true,"node_id":"node-1","hub_url":"https://hub.example.test/","botnode":true,"enroll_left":false,"srv":true,"srv_token":true}`
	if _, _, err := botNodeProbeDecision(done, true, "https://hub.example.test", false); err == nil || !strings.Contains(err.Error(), "正在收尾") {
		t.Fatal(err)
	}
	id, left, err = botNodeProbeDecision(done, true, "https://hub.example.test", true)
	if err != nil || id != "node-1" || left {
		t.Fatalf("srv id=%s left=%v err=%v", id, left, err)
	}
	if _, _, err := botNodeProbeDecision(registered, true, "https://hub.example.test", true); err == nil || !strings.Contains(err.Error(), "正在收尾") {
		t.Fatal(err)
	}
	restarting := `{"desktopd":true,"node_id":"node-1","hub_url":"https://hub.example.test","botnode":false,"enroll_left":false}`
	if _, _, err := botNodeProbeDecision(restarting, false, "https://hub.example.test", true); err == nil || !strings.Contains(err.Error(), "正在") {
		t.Fatal(err)
	}
	wrong := `{"desktopd":true,"node_id":"node-1","hub_url":"https://other.example","botnode":true,"enroll_left":false}`
	if _, _, err := botNodeProbeDecision(wrong, false, "https://hub.example.test", true); err == nil || !strings.Contains(err.Error(), "不是当前 Hub") {
		t.Fatal(err)
	}
	if _, _, err := botNodeProbeDecision(`{"desktopd":false}`, false, "https://hub.example.test", true); err == nil || !strings.Contains(err.Error(), "正在安装") {
		t.Fatal(err)
	}
}

func TestBotNodeProbeDoesNotNeedHostPython(t *testing.T) {
	if strings.Contains(botNodeProbeScript, "python3") || strings.Contains(botNodeProbeCommand(), "python3") {
		t.Fatal("probe still calls python on the host")
	}
	for _, phrase := range []string{`"desktopd"`, `"node_id"`, `"hub_url"`, `"srv_token"`} {
		if !strings.Contains(botNodeProbeScript, phrase) {
			t.Fatalf("probe script missing %s", phrase)
		}
	}
}

func TestBotNodeRejectsABadCode(t *testing.T) {
	a := &App{testHomeDir: t.TempDir()}
	if _, err := a.DeployBotNode(BotNodeDeployRequest{Host: "203.0.113.20", User: "root", Password: "pw", EnrollCode: "short"}); err == nil || !strings.Contains(err.Error(), "注册码无效") {
		t.Fatal(err)
	}
	code, err := botNodeEnrollCode("  aabbccddeeff0011 ")
	if err != nil || code != "AABBCCDDEEFF0011" {
		t.Fatalf("code=%s err=%v", code, err)
	}
}

func TestBotNodeOrphanJobIsClosed(t *testing.T) {
	a := &App{testHomeDir: t.TempDir()}
	if !claimBotNodeJob("bn_live") {
		t.Fatal("could not claim the live install")
	}
	defer releaseBotNodeJob("bn_live")
	if err := a.saveBotNodes([]BotNodeRecord{
		{ID: "bn_live", Host: "10.0.0.1", Port: 22, User: "root", Status: botNodeStatusDeploying, JobStartedAt: "2026-11-16T00:00:00Z"},
		{ID: "bn_orphan", Host: "10.0.0.2", Port: 22, User: "root", Status: botNodeStatusRegistering, JobStartedAt: "2026-11-16T00:00:00Z"},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := a.ListBotNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	for _, row := range rows {
		switch row.ID {
		case "bn_live":
			if row.Status != botNodeStatusDeploying || row.JobStartedAt == "" {
				t.Fatalf("live = %+v", row)
			}
		case "bn_orphan":
			if row.Status != botNodeStatusFailed || row.JobStartedAt != "" || !strings.Contains(row.Message, "已中断") {
				t.Fatalf("orphan = %+v", row)
			}
		default:
			t.Fatalf("unexpected row %s", row.ID)
		}
	}
	jobs := a.listLocalBotNodeJobs()
	if len(jobs) != 1 || jobs[0].ID != "bn_live" || !strings.Contains(jobs[0].Title, "10.0.0.1") {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestBotNodeHostBusyIgnoresItsOwnRecord(t *testing.T) {
	records := []BotNodeRecord{
		{ID: "bn_a", Host: "10.0.0.1", Port: 22, Status: botNodeStatusDeploying},
		{ID: "bn_b", Host: "10.0.0.1", Port: 22, Status: botNodeStatusFailed},
	}
	if botNodeHostBusy(records, "bn_a", "10.0.0.1", 22) {
		t.Fatal("the running install blocked itself")
	}
	if !botNodeHostBusy(records, "bn_b", "10.0.0.1", 22) {
		t.Fatal("a second install was allowed onto a live host")
	}
}

func TestBotNodePromptOmitsTheSrvSectionsWhenDesktopOnly(t *testing.T) {
	rec := BotNodeRecord{Host: "203.0.113.20", Port: 22, User: "root", HubURL: "https://hub.example.test"}
	prompt := botNodePrompt(rec, "pw", desktopNodePrivilege{root: true}, "AABBCCDDEEFF0011", false)
	if !strings.Contains(prompt, "WITH_SRV=0") || !strings.Contains(prompt, "跳过第 7 节和第 9 节") {
		t.Fatal(prompt)
	}
}

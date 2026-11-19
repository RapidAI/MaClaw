package guiapp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestHubDeployPromptNamesTheRunbookAndHidesThePasswordFromTheTitle(t *testing.T) {
	rec := HubDeployRecord{
		ID:            "hd_prompt",
		Host:          "203.0.113.10",
		Port:          22,
		User:          "ubuntu",
		Email:         "owner@example.com",
		PublicURL:     "http://203.0.113.10:9399",
		CenterURLs:    "https://hubs.mypapers.top",
		AdminUser:     "admin",
		AdminPassword: "0123456789abcdef0123456789abcdef",
	}
	prompt := hubDeployPrompt(rec, "ssh-secret", desktopNodePrivilege{root: true}, "https://hub.example.test")
	for _, phrase := range []string{
		"https://hub.example.test/docs/hub-install",
		"HUB_PUBLIC_URL=http://203.0.113.10:9399",
		"OWNER_EMAIL=owner@example.com",
		"ADMIN_PASSWORD=0123456789abcdef0123456789abcdef",
		"ADMIN_SETUP_MARK=hd_prompt",
		"https://hubs.mypapers.top",
		"HUB_INSTALL: OK",
		"ssh-secret",
	} {
		if !strings.Contains(prompt, phrase) {
			t.Fatalf("prompt missing %q", phrase)
		}
	}
	display := "〔Hub 部署〕在 ubuntu@203.0.113.10 上安装 Hub 并注册"
	if strings.Contains(display, "ssh-secret") || strings.Contains(display, rec.AdminPassword) {
		t.Fatal("the visible row contains a secret")
	}
}

func TestHubDeployRegistrationResult(t *testing.T) {
	id, pending, err := hubDeployRegistrationResult(`{"registered":true,"pending":false,"hub_id":"hub_1"}`)
	if err != nil || id != "hub_1" || pending {
		t.Fatalf("registered: id=%s pending=%v err=%v", id, pending, err)
	}
	id, pending, err = hubDeployRegistrationResult(`{"registered":false,"pending":true,"hub_id":"hub_2"}`)
	if err != nil || id != "hub_2" || !pending {
		t.Fatalf("pending: id=%s pending=%v err=%v", id, pending, err)
	}
	_, _, err = hubDeployRegistrationResult(`{"disabled":true,"hub_id":"hub_3","last_error":"locked"}`)
	if err == nil || !strings.Contains(err.Error(), "已被停用") {
		t.Fatal(err)
	}
	_, _, err = hubDeployRegistrationResult(`{"last_error":"hub center base url is required"}`)
	if err == nil || !strings.Contains(err.Error(), "注册失败") || strings.Contains(err.Error(), "正在") {
		t.Fatal(err)
	}
	_, _, err = hubDeployRegistrationResult("")
	if err == nil || !strings.Contains(err.Error(), "正在") {
		t.Fatal(err)
	}
}

func TestHubDeployPublicCenterURLsDropsLoopback(t *testing.T) {
	got := hubDeployPublicCenterURLs([]string{
		"https://center.example",
		"http://127.0.0.1:9399",
		"https://center.example",
		"http://[::1]:9399",
	})
	if len(got) != 1 || got[0] != "https://center.example" {
		t.Fatalf("centers = %#v", got)
	}
	if len(hubDeployPublicCenterURLs([]string{"http://localhost:9399"})) != 0 {
		t.Fatal("localhost was kept")
	}
}

func TestHubDeployAdminResultKeepsThePasswordOnlyWhenCreated(t *testing.T) {
	started := time.Unix(1_700_000_000, 0).UTC()
	if hubDeployAdminCodeFresh("1700000100 200", "hd_new", started) != "200" || hubDeployAdminCodeFresh("1699990000 409 hd_new", "hd_new", started) != "409" {
		t.Fatal("a result written during this install was ignored")
	}
	if hubDeployAdminCodeFresh("1699990000 200", "hd_new", started) != "" || hubDeployAdminCodeFresh("1699990000 200 hd_old", "hd_new", started) != "" || hubDeployAdminCodeFresh("0 missing", "hd_new", started) != "" || hubDeployAdminCodeFresh("1700000100 0123456789abcdef", "hd_new", started) != "" {
		t.Fatal("a stale or non-status result was accepted")
	}
	created := hubDeployReadyMessage(false, "pw")
	if created != "Hub 已安装并完成注册" {
		t.Fatalf("created = %q", created)
	}
	exists := hubDeployReadyMessage(true, "")
	if !strings.Contains(exists, "等待确认") || !strings.Contains(exists, "已经创建") {
		t.Fatalf("exists = %q", exists)
	}
}

func TestHubDeployOrphanJobIsClosed(t *testing.T) {
	a := &App{testHomeDir: t.TempDir()}
	if !claimHubDeployJob("hd_live") {
		t.Fatal("could not claim the live install")
	}
	defer releaseHubDeployJob("hd_live")
	if err := a.saveHubDeploys([]HubDeployRecord{
		{ID: "hd_live", Host: "10.0.0.1", Port: 22, User: "root", Status: hubDeployStatusDeploying, JobStartedAt: "2026-11-16T00:00:00Z", AdminPassword: "live-admin-secret"},
		{ID: "hd_orphan", Host: "10.0.0.2", Port: 22, User: "root", Status: hubDeployStatusRegistering, JobStartedAt: "2026-11-16T00:00:00Z", AdminPassword: "orphan-admin-secret"},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := a.ListHubDeploys()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	for _, row := range rows {
		switch row.ID {
		case "hd_live":
			if row.Status != hubDeployStatusDeploying || row.JobStartedAt == "" {
				t.Fatalf("live = %+v", row)
			}
		case "hd_orphan":
			if row.Status != hubDeployStatusFailed || row.JobStartedAt != "" || !strings.Contains(row.Message, "已中断") {
				t.Fatalf("orphan = %+v", row)
			}
			if row.AdminPassword != "orphan-admin-secret" {
				t.Fatal("the interrupted install dropped the console password")
			}
		default:
			t.Fatalf("unexpected row %s", row.ID)
		}
	}
	jobs := a.listLocalHubDeployJobs()
	if len(jobs) != 1 || jobs[0].ID != "hd_live" {
		t.Fatalf("jobs = %+v", jobs)
	}
	raw, err := json.Marshal(jobs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("job list leaked a password: %s", raw)
	}
}

func TestHubDeployRejectsABadEmail(t *testing.T) {
	if hubDeployEmail.MatchString("not-an-email") {
		t.Fatal("accepted an email without a domain")
	}
	if !hubDeployEmail.MatchString("owner@example.com") {
		t.Fatal("rejected a normal email")
	}
}

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestMobileSSHQuickConnectRegistersProfileAndVault(t *testing.T) {
	identity, _, _ := newHTTPAPITestServices(t)
	token, _ := issueViewerToken(t, identity, "quick-ssh@example.com")

	mobileServerProfiles.Lock()
	prevP := mobileServerProfiles.profiles
	mobileServerProfiles.profiles = make(map[string]mobileServerProfileRecord)
	mobileServerProfiles.Unlock()
	mobileSSHVault.Lock()
	prevV := mobileSSHVault.secrets
	mobileSSHVault.secrets = make(map[string]mobileSSHVaultRecord)
	mobileSSHVault.Unlock()
	t.Cleanup(func() {
		mobileServerProfiles.Lock()
		mobileServerProfiles.profiles = prevP
		mobileServerProfiles.Unlock()
		mobileSSHVault.Lock()
		mobileSSHVault.secrets = prevV
		mobileSSHVault.Unlock()
	})

	body := `{"host":"10.0.0.9","username":"root","password":"s3cret","port":22}`
	req := httptest.NewRequest(http.MethodPost, "/api/mobile/ssh/quick-connect", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	MobileSSHQuickConnectHandler(identity).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "s3cret") {
		t.Fatal("password must not leak in response")
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["label"] != "root@10.0.0.9" {
		t.Fatalf("label=%v", resp["label"])
	}
	if resp["has_secret"] != true {
		t.Fatalf("body=%#v", resp)
	}
	profileID, _ := resp["profile_id"].(string)
	if profileID == "" || !strings.HasPrefix(profileID, "q") {
		t.Fatalf("profile_id=%v", profileID)
	}

	// Inject hosts for this viewer.
	principal, err := identity.AuthenticateViewer(req.Context(), token)
	if err != nil {
		// Token format may require raw without Bearer prefix.
		principal, err = identity.AuthenticateViewer(req.Context(), strings.TrimPrefix(token, ""))
	}
	if err != nil {
		// Fall back: scan maps without principal injection.
		mobileSSHVault.Lock()
		nVault := len(mobileSSHVault.secrets)
		mobileSSHVault.Unlock()
		mobileServerProfiles.Lock()
		nProf := len(mobileServerProfiles.profiles)
		mobileServerProfiles.Unlock()
		if nVault != 1 || nProf != 1 {
			t.Fatalf("vault=%d profiles=%d authErr=%v", nVault, nProf, err)
		}
		return
	}
	hosts := mobileAgentSSHHosts(principal, t.TempDir())
	if len(hosts) != 1 {
		t.Fatalf("hosts=%#v principal=%#v", hosts, principal)
	}
	if hosts[0].Password != "s3cret" || hosts[0].Host != "10.0.0.9" {
		t.Fatalf("host entry=%#v", hosts[0])
	}
	_ = auth.ViewerPrincipal{} // keep import if needed for older go
}

func TestMobileSSHQuickProfileIDStable(t *testing.T) {
	a := mobileSSHQuickProfileID("10.0.0.1", 22, "root")
	b := mobileSSHQuickProfileID("10.0.0.1", 22, "root")
	c := mobileSSHQuickProfileID("10.0.0.1", 22, "ubuntu")
	if a != b || a == c || !strings.HasPrefix(a, "q") {
		t.Fatalf("ids a=%s b=%s c=%s", a, b, c)
	}
}

func TestMobileParseQuickSSHFromText(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		wantHost   string
		wantUser   string
		wantPass   string
		wantPort   int
		wantOK     bool
	}{
		{
			name: "chinese freeform host user pass",
			text: "查一下服务器状态 www.example.com root MyPass123",
			wantHost: "www.example.com", wantUser: "root", wantPass: "MyPass123", wantPort: 22, wantOK: true,
		},
		{
			name: "user@host password",
			text: "root@10.0.0.9:2222 s3cretPass",
			wantHost: "10.0.0.9", wantUser: "root", wantPass: "s3cretPass", wantPort: 2222, wantOK: true,
		},
		{
			name: "labeled chinese",
			text: "主机 10.1.2.3 用户 ubuntu 密码 SuperSecret9",
			wantHost: "10.1.2.3", wantUser: "ubuntu", wantPass: "SuperSecret9", wantPort: 22, wantOK: true,
		},
		{
			name: "labeled with colons",
			text: "host: edge.internal user: deploy password: Abcd1234",
			wantHost: "edge.internal", wantUser: "deploy", wantPass: "Abcd1234", wantPort: 22, wantOK: true,
		},
		{
			name: "no credentials",
			text: "帮我看看天气怎么样",
			wantOK: false,
		},
		{
			name: "host only no password",
			text: "连接 10.0.0.1 root",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, u, p, port, ok := mobileParseQuickSSHFromText(tc.text)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want=%v got host=%q user=%q pass=%q", ok, tc.wantOK, h, u, p)
			}
			if !tc.wantOK {
				return
			}
			if h != tc.wantHost || u != tc.wantUser || p != tc.wantPass || port != tc.wantPort {
				t.Fatalf("got host=%q user=%q pass=%q port=%d", h, u, p, port)
			}
		})
	}
}

func TestMobileMaybeAutoRegisterSSHFromUserText(t *testing.T) {
	mobileServerProfiles.Lock()
	prevP := mobileServerProfiles.profiles
	mobileServerProfiles.profiles = make(map[string]mobileServerProfileRecord)
	mobileServerProfiles.Unlock()
	mobileSSHVault.Lock()
	prevV := mobileSSHVault.secrets
	mobileSSHVault.secrets = make(map[string]mobileSSHVaultRecord)
	mobileSSHVault.Unlock()
	t.Cleanup(func() {
		mobileServerProfiles.Lock()
		mobileServerProfiles.profiles = prevP
		mobileServerProfiles.Unlock()
		mobileSSHVault.Lock()
		mobileSSHVault.secrets = prevV
		mobileSSHVault.Unlock()
	})

	principal := &auth.ViewerPrincipal{
		UserID:   "auto-ssh-user",
		TenantID: "tenant-auto",
		Email:    "auto@example.com",
	}
	text := "查状态 203.0.113.10 root SecretPass99 并告诉我 uptime"
	redacted, label, pass, ok := mobileMaybeAutoRegisterSSHFromUserText(principal, text)
	if !ok {
		t.Fatal("expected auto-register success")
	}
	if label != "root@203.0.113.10" {
		t.Fatalf("label=%q", label)
	}
	if pass != "SecretPass99" {
		t.Fatalf("pass=%q", pass)
	}
	if strings.Contains(redacted, "SecretPass99") {
		t.Fatalf("password leaked into redacted text: %q", redacted)
	}
	if !strings.Contains(redacted, "***") {
		t.Fatalf("expected redaction marker in %q", redacted)
	}
	if !strings.Contains(redacted, "ssh tool") {
		t.Fatalf("expected system nudge in %q", redacted)
	}

	hosts := mobileAgentSSHHosts(principal, t.TempDir())
	if len(hosts) != 1 {
		t.Fatalf("hosts=%#v", hosts)
	}
	if hosts[0].Password != "SecretPass99" || hosts[0].Host != "203.0.113.10" {
		t.Fatalf("host entry=%#v", hosts[0])
	}
	// Re-register same host should succeed (upsert) and keep tool enabled.
	_, _, _, ok2 := mobileMaybeAutoRegisterSSHFromUserText(principal, "root@203.0.113.10 SecretPass99")
	if !ok2 {
		t.Fatal("re-register should succeed")
	}
}

func TestMobileRedactPasswordInText(t *testing.T) {
	got := mobileRedactPasswordInText("use pass SecretX on 1.2.3.4", "SecretX")
	if strings.Contains(got, "SecretX") || !strings.Contains(got, "***") {
		t.Fatalf("got %q", got)
	}
}

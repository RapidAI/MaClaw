package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

func TestCloudWorkspaceShareHTTPAcceptAndRevoke(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	created := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "分享课题"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var ws struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &ws); err != nil || ws.ID == "" {
		t.Fatalf("create body=%s err=%v", created.Body.String(), err)
	}

	shareRec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+ws.ID+"/share", "m1", "secret", map[string]any{"permission": "read"})
	if shareRec.Code != http.StatusOK {
		t.Fatalf("share=%d %s", shareRec.Code, shareRec.Body.String())
	}
	var share struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(shareRec.Body.Bytes(), &share); err != nil || share.Token == "" {
		t.Fatalf("share body=%s err=%v", shareRec.Body.String(), err)
	}

	page := doCloudWorkspaceRequest(t, h, http.MethodGet, "/hub/cloud-workspaces/shares/"+share.Token, "", "", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "maclaw://cloud-workspace-share?") || !strings.Contains(page.Body.String(), "token=") {
		t.Fatalf("public page=%d %s", page.Code, page.Body.String())
	}

	accept := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspace-shares/"+share.Token+"/accept", "m2", "secret", nil)
	if accept.Code != http.StatusOK {
		t.Fatalf("accept=%d %s", accept.Code, accept.Body.String())
	}
	var accepted struct {
		OwnerEmail string `json:"owner_email"`
	}
	if err := json.Unmarshal(accept.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if accepted.OwnerEmail != "u1@x.com" {
		t.Fatalf("owner_email=%q body=%s", accepted.OwnerEmail, accept.Body.String())
	}

	ent := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/entitlement", "m2", "secret", nil)
	if ent.Code != http.StatusOK {
		t.Fatalf("entitlement=%d %s", ent.Code, ent.Body.String())
	}
	var payload struct {
		Shared []struct {
			ID              string `json:"id"`
			SharePermission string `json:"share_permission"`
		} `json:"shared"`
	}
	if err := json.Unmarshal(ent.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Shared) != 1 || payload.Shared[0].ID != ws.ID || payload.Shared[0].SharePermission != "read" {
		t.Fatalf("shared=%+v", payload.Shared)
	}

	manifest := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/"+ws.ID+"/manifest", "m2", "secret", nil)
	if manifest.Code != http.StatusOK {
		t.Fatalf("reader manifest=%d %s", manifest.Code, manifest.Body.String())
	}

	put := doCloudWorkspaceRequest(t, h, http.MethodPut, "/api/v1/cloud-workspaces/"+ws.ID+"/manifest", "m2", "secret", map[string]any{"entries": []any{}})
	if put.Code != http.StatusForbidden || cloudWorkspaceErrCode(t, put) != "CLOUD_WORKSPACE_SHARE_READ_ONLY" {
		t.Fatalf("reader put=%d %s", put.Code, put.Body.String())
	}

	revoke := doCloudWorkspaceRequest(t, h, http.MethodDelete, "/api/v1/cloud-workspaces/"+ws.ID+"/share", "m1", "secret", nil)
	if revoke.Code != http.StatusOK {
		t.Fatalf("revoke=%d %s", revoke.Code, revoke.Body.String())
	}
	ent2 := doCloudWorkspaceRequest(t, h, http.MethodGet, "/api/v1/cloud-workspaces/entitlement", "m2", "secret", nil)
	if err := json.Unmarshal(ent2.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Shared) != 0 {
		t.Fatalf("shared after revoke=%+v", payload.Shared)
	}
}

func TestCloudWorkspaceShareAcceptDoesNotRequireInstanceSession(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	created := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "分享课题"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var ws struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &ws); err != nil || ws.ID == "" {
		t.Fatalf("create body=%s err=%v", created.Body.String(), err)
	}
	shareRec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+ws.ID+"/share", "m1", "secret", map[string]any{"permission": "read"})
	if shareRec.Code != http.StatusOK {
		t.Fatalf("share=%d %s", shareRec.Code, shareRec.Body.String())
	}
	var share struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(shareRec.Body.Bytes(), &share); err != nil || share.Token == "" {
		t.Fatalf("share body=%s err=%v", shareRec.Body.String(), err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-workspace-shares/"+share.Token+"/accept", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Machine-ID", "m2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept without instance session=%d %s", rec.Code, rec.Body.String())
	}
}

func TestCloudWorkspaceShareClaimAcrossHubs(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	created := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "联邦课题"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var ws struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &ws); err != nil {
		t.Fatal(err)
	}
	shareRec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+ws.ID+"/share", "m1", "secret", map[string]any{"permission": "read"})
	if shareRec.Code != http.StatusOK {
		t.Fatalf("share=%d %s", shareRec.Code, shareRec.Body.String())
	}
	var share struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(shareRec.Body.Bytes(), &share); err != nil || share.Token == "" {
		t.Fatalf("share body=%s err=%v", shareRec.Body.String(), err)
	}

	claimReq := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-workspace-shares/"+share.Token+"/accept", strings.NewReader(`{"home_hub":"https://other.hub","home_user_id":"u-b","display_name":"b@x.com"}`))
	claimReq.Header.Set("Content-Type", "application/json")
	claimRec := httptest.NewRecorder()
	h.ServeHTTP(claimRec, claimReq)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim=%d %s", claimRec.Code, claimRec.Body.String())
	}
	var claimed struct {
		AccessToken string `json:"access_token"`
		OwnerEmail  string `json:"owner_email"`
	}
	if err := json.Unmarshal(claimRec.Body.Bytes(), &claimed); err != nil || !strings.HasPrefix(claimed.AccessToken, cloudworkspace.ShareAccessTokenPrefix) {
		t.Fatalf("claimed=%+v err=%v body=%s", claimed, err, claimRec.Body.String())
	}

	manReq := httptest.NewRequest(http.MethodGet, "/api/v1/cloud-workspaces/"+ws.ID+"/manifest", nil)
	manReq.Header.Set("Authorization", "Bearer "+claimed.AccessToken)
	manReq.Header.Set("X-Cloud-Workspace-Protocol", cloudworkspace.CloudWorkspaceProtocol)
	manRec := httptest.NewRecorder()
	h.ServeHTTP(manRec, manReq)
	if manRec.Code != http.StatusOK {
		t.Fatalf("share-bearer manifest=%d %s", manRec.Code, manRec.Body.String())
	}
}

func TestCloudWorkspaceShareHTTPPassword(t *testing.T) {
	_, h, _ := newCloudWorkspaceUserEnv(t, cloudworkspace.ModeAllUsers, 5, nil)
	created := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces", "m1", "secret", map[string]any{"name": "密码课题"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var ws struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &ws); err != nil {
		t.Fatal(err)
	}
	shareRec := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspaces/"+ws.ID+"/share", "m1", "secret", map[string]any{
		"permission": "read",
		"password":   "secret",
		"ttl":        "7d",
	})
	if shareRec.Code != http.StatusOK {
		t.Fatalf("share=%d %s", shareRec.Code, shareRec.Body.String())
	}
	var share struct {
		Token       string `json:"token"`
		PasswordSet bool   `json:"password_set"`
		ExpiresAt   string `json:"expires_at"`
	}
	if err := json.Unmarshal(shareRec.Body.Bytes(), &share); err != nil || !share.PasswordSet || share.Token == "" || share.ExpiresAt == "" {
		t.Fatalf("share body=%s err=%v", shareRec.Body.String(), err)
	}

	page := doCloudWorkspaceRequest(t, h, http.MethodGet, "/hub/cloud-workspaces/shares/"+share.Token, "", "", nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "设有密码") {
		t.Fatalf("public page=%d %s", page.Code, page.Body.String())
	}

	missing := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspace-shares/"+share.Token+"/accept", "m2", "secret", nil)
	if missing.Code != http.StatusForbidden || cloudWorkspaceErrCode(t, missing) != "CLOUD_WORKSPACE_SHARE_PASSWORD_REQUIRED" {
		t.Fatalf("missing password=%d %s", missing.Code, missing.Body.String())
	}
	wrong := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspace-shares/"+share.Token+"/accept", "m2", "secret", map[string]any{"password": "nope"})
	if wrong.Code != http.StatusForbidden || cloudWorkspaceErrCode(t, wrong) != "CLOUD_WORKSPACE_SHARE_PASSWORD_INVALID" {
		t.Fatalf("wrong password=%d %s", wrong.Code, wrong.Body.String())
	}
	ok := doCloudWorkspaceRequest(t, h, http.MethodPost, "/api/v1/cloud-workspace-shares/"+share.Token+"/accept", "m2", "secret", map[string]any{"password": "secret"})
	if ok.Code != http.StatusOK {
		t.Fatalf("accept=%d %s", ok.Code, ok.Body.String())
	}
}

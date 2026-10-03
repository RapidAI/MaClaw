package llmservice

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAdminAPIKeyCreateListAuthRevoke(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	created, err := svc.CreateAdminAPIKey(ctx, "batch")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(created.APIKey, "hck_") || created.ID == "" || created.Prefix == "" {
		t.Fatalf("created = %+v", created)
	}
	if err := svc.AuthenticateAdminAPIKey(ctx, created.APIKey); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if err := svc.AuthenticateAdminAPIKey(ctx, "hck_not-a-real-key"); err == nil {
		t.Fatal("bad key authenticated")
	}
	keys, err := svc.ListAdminAPIKeys(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 || keys[0].Hash != "" || keys[0].APIKey != created.APIKey || keys[0].ID != created.ID {
		t.Fatalf("list = %+v", keys)
	}
	raw, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"hash"`) || !strings.Contains(string(raw), created.APIKey) {
		t.Fatalf("list json = %s", raw)
	}
	stored := svc.system.(*mockSystemSettings).data[AdminAPIKeySettingKey]
	if !strings.Contains(stored, `"hash":"`) || !strings.Contains(stored, created.APIKey) {
		t.Fatalf("stored keys dropped the hash or secret: %s", stored)
	}
	authed, err := svc.AuthorizeAdminAPIKey(ctx, created.APIKey, "")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if authed.APIKey != "" || authed.Hash != "" {
		t.Fatalf("authorize copy kept secret material: %+v", authed)
	}
	stored = svc.system.(*mockSystemSettings).data[AdminAPIKeySettingKey]
	if !strings.Contains(stored, created.APIKey) {
		t.Fatalf("authorize dropped the saved secret: %s", stored)
	}
	if err := svc.RevokeAdminAPIKey(ctx, created.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	stored = svc.system.(*mockSystemSettings).data[AdminAPIKeySettingKey]
	if strings.Contains(stored, created.APIKey) {
		t.Fatalf("revoked key kept the secret: %s", stored)
	}
	if err := svc.RevokeAdminAPIKey(ctx, created.ID); err != nil {
		t.Fatalf("second revoke: %v", err)
	}
	longName := strings.Repeat("密", 80)
	if _, err := svc.CreateAdminAPIKey(ctx, longName); err != nil {
		t.Fatalf("80-rune name: %v", err)
	}
	if _, err := svc.CreateAdminAPIKey(ctx, longName+"钥"); err == nil {
		t.Fatal("81-rune name accepted")
	}
	if err := svc.AuthenticateAdminAPIKey(ctx, created.APIKey); err == nil {
		t.Fatal("revoked key still authenticated")
	}
	if _, err := svc.CreateAdminAPIKey(ctx, "  "); err == nil {
		t.Fatal("blank name accepted")
	}
	if err := svc.RevokeAdminAPIKey(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "api key not found") {
		t.Fatalf("missing revoke error = %v", err)
	}
	capped := NewService(&mockSystemSettings{})
	for i := 0; i < maxActiveAdminAPIKeys; i++ {
		if _, err := capped.CreateAdminAPIKey(ctx, "k"); err != nil {
			t.Fatalf("seed key %d: %v", i, err)
		}
	}
	if _, err := capped.CreateAdminAPIKey(ctx, "overflow"); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("cap error = %v", err)
	}
}

func TestAdminAPIKeyLegacyRecordStillAuthenticates(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	created, err := svc.CreateAdminAPIKey(ctx, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	var file adminAPIKeyFile
	if err := json.Unmarshal([]byte(svc.system.(*mockSystemSettings).data[AdminAPIKeySettingKey]), &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Keys) != 1 {
		t.Fatalf("stored = %+v", file.Keys)
	}
	file.Keys[0].APIKey = ""
	encoded, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	svc.system.(*mockSystemSettings).data[AdminAPIKeySettingKey] = string(encoded)
	keys, err := svc.ListAdminAPIKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].APIKey != "" || keys[0].Hash != "" || keys[0].Prefix == "" || strings.Contains(string(listed), created.APIKey) {
		t.Fatalf("legacy list = %s", listed)
	}
	if err := svc.AuthenticateAdminAPIKey(ctx, created.APIKey); err != nil {
		t.Fatalf("legacy auth: %v", err)
	}
}

func TestAuthorizeKeepsSecretFileAndRecordsUse(t *testing.T) {
	svc := NewService(&mockSystemSettings{})
	ctx := context.Background()
	created, err := svc.CreateAdminAPIKey(ctx, "used")
	if err != nil {
		t.Fatal(err)
	}
	before := svc.system.(*mockSystemSettings).data[AdminAPIKeySettingKey]
	if _, err := svc.AuthorizeAdminAPIKey(ctx, created.APIKey, AdminAPIScopeRead); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthorizeAdminAPIKey(ctx, created.APIKey, AdminAPIScopeRead); err != nil {
		t.Fatal(err)
	}
	after := svc.system.(*mockSystemSettings).data[AdminAPIKeySettingKey]
	if after != before {
		t.Fatalf("authorize rewrote the secret file\nbefore %s\nafter %s", before, after)
	}
	usage := svc.system.(*mockSystemSettings).data[adminAPIKeyLastUsedSettingKey]
	if !strings.Contains(usage, created.ID) || strings.Contains(usage, created.APIKey) {
		t.Fatalf("usage = %s", usage)
	}
	keys, err := svc.ListAdminAPIKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].LastUsedAt.IsZero() || keys[0].APIKey != created.APIKey {
		t.Fatalf("list = %+v", keys)
	}
	if err := svc.RevokeAdminAPIKey(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	usage = svc.system.(*mockSystemSettings).data[adminAPIKeyLastUsedSettingKey]
	if strings.Contains(usage, created.ID) || strings.Contains(usage, created.APIKey) {
		t.Fatalf("revoked usage = %s", usage)
	}
}

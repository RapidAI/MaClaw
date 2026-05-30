package skill

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

type sourceControlMemoryKV map[string]string

func (m sourceControlMemoryKV) Set(_ context.Context, key, value string) error {
	m[key] = value
	return nil
}

func (m sourceControlMemoryKV) Get(_ context.Context, key string) (string, error) {
	return m[key], nil
}

func TestValidateSourceNamesAcceptsAliasesAndEnterpriseHub(t *testing.T) {
	if err := ValidateSourceNames([]string{"hubcenter", "git_hub", "enterprise", "local", "zip", "local_upload"}); err != nil {
		t.Fatalf("ValidateSourceNames() error = %v", err)
	}
	if err := ValidateSourceNames([]string{"unknown"}); err == nil {
		t.Fatal("ValidateSourceNames() should reject unknown source")
	}
}

func TestIntersectSourcesNormalizesAliases(t *testing.T) {
	got := IntersectSources([]string{"hubcenter", "git_hub", "enterprise", "zip", "local_upload"}, []string{"skillhub", "github", "enterprise_hub", "local"})
	want := []string{"skillhub", "github", "enterprise_hub", "local"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IntersectSources() = %#v, want %#v", got, want)
	}
}

func TestFormatSourcePolicyDeniedLocalizesAndNormalizesAliases(t *testing.T) {
	msg := FormatSourcePolicyDenied("hubcenter", []string{"enterprise", "git_hub", "enterprise_hub"})
	for _, want := range []string{"当前企业策略不允许", "Your organization policy does not allow", "skill source: skillhub", "allowed sources: enterprise_hub, github"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("FormatSourcePolicyDenied() = %q, want substring %q", msg, want)
		}
	}
}

func TestTenantUserPolicyDoesNotCollideWithGlobalUserPolicy(t *testing.T) {
	ctx := context.Background()
	svc := NewSourceControlService(sourceControlMemoryKV{})
	if err := svc.SetUser(ctx, "tenant-a:user-a", &SourceControlConfig{Enabled: true, AllowedSources: []string{"github"}}); err != nil {
		t.Fatalf("SetUser: %v", err)
	}
	if err := svc.SetTenantUser(ctx, "tenant-a", "user-a", &SourceControlConfig{Enabled: true, AllowedSources: []string{"local"}}); err != nil {
		t.Fatalf("SetTenantUser: %v", err)
	}

	if got := svc.ResolveForUser(ctx, "user-a", "tenant-a"); !reflect.DeepEqual(got, []string{"local"}) {
		t.Fatalf("tenant user ResolveForUser() = %#v, want local", got)
	}
	if got := svc.ResolveForUser(ctx, "tenant-a:user-a", ""); !reflect.DeepEqual(got, []string{"github"}) {
		t.Fatalf("global user ResolveForUser() = %#v, want github", got)
	}
}

func TestEnabledEmptyPolicyBlocksAllSources(t *testing.T) {
	ctx := context.Background()
	svc := NewSourceControlService(sourceControlMemoryKV{})
	if got := svc.ResolveForUser(ctx, "user-a", "tenant-a"); got != nil {
		t.Fatalf("unset ResolveForUser() = %#v, want nil all-allowed sentinel", got)
	}
	if err := svc.SetTenantUser(ctx, "tenant-a", "user-a", &SourceControlConfig{Enabled: true}); err != nil {
		t.Fatalf("SetTenantUser: %v", err)
	}
	got := svc.ResolveForUser(ctx, "user-a", "tenant-a")
	if got == nil || len(got) != 0 {
		t.Fatalf("enabled empty ResolveForUser() = %#v, want non-nil empty block-all list", got)
	}
}

func TestSourceControlConfigsAreDefensivelyCopied(t *testing.T) {
	ctx := context.Background()
	svc := NewSourceControlService(sourceControlMemoryKV{})
	cfg := &SourceControlConfig{Enabled: true, AllowedSources: []string{"github"}}
	if err := svc.SetTenantUser(ctx, "tenant-a", "user-a", cfg); err != nil {
		t.Fatalf("SetTenantUser: %v", err)
	}
	cfg.AllowedSources[0] = "local"
	if got := svc.ResolveForUser(ctx, "user-a", "tenant-a"); !reflect.DeepEqual(got, []string{"github"}) {
		t.Fatalf("ResolveForUser after caller mutation = %#v, want github", got)
	}

	gotCfg, err := svc.GetTenantUser(ctx, "tenant-a", "user-a")
	if err != nil {
		t.Fatalf("GetTenantUser: %v", err)
	}
	gotCfg.AllowedSources[0] = "local"
	if got := svc.ResolveForUser(ctx, "user-a", "tenant-a"); !reflect.DeepEqual(got, []string{"github"}) {
		t.Fatalf("ResolveForUser after returned config mutation = %#v, want github", got)
	}
}

func TestSetSourceControlRejectsNilConfig(t *testing.T) {
	ctx := context.Background()
	svc := NewSourceControlService(sourceControlMemoryKV{})
	if err := svc.SetGlobal(ctx, nil); err == nil || !strings.Contains(err.Error(), "config is required") {
		t.Fatalf("SetGlobal(nil) error = %v, want required config", err)
	}
	if err := svc.SetTenant(ctx, "tenant-a", nil); err == nil || !strings.Contains(err.Error(), "config is required") {
		t.Fatalf("SetTenant(nil) error = %v, want required config", err)
	}
	if err := svc.SetUser(ctx, "user-a", nil); err == nil || !strings.Contains(err.Error(), "config is required") {
		t.Fatalf("SetUser(nil) error = %v, want required config", err)
	}
	if err := svc.SetTenantUser(ctx, "tenant-a", "user-a", nil); err == nil || !strings.Contains(err.Error(), "config is required") {
		t.Fatalf("SetTenantUser(nil) error = %v, want required config", err)
	}
}

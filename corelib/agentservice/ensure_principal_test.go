package agentservice

import (
	"context"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestEnsurePrincipalCreatesTenantUserAndIsIdempotent(t *testing.T) {
	t.Parallel()
	svc, err := NewService(Config{
		DataRoot:    t.TempDir(),
		TokenSecret: "test-token-secret-0123456789012345",
		TokenTTL:    time.Hour,
	}, NewMemoryStore(), EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	p := Principal{TenantID: "tenant_mobile_1", UserID: "user_mobile_1"}
	if err := svc.EnsurePrincipal(context.Background(), p, "phone:19900001111", "Mobile User"); err != nil {
		t.Fatalf("EnsurePrincipal: %v", err)
	}
	user, err := svc.store.GetUser(p.TenantID, p.UserID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.ID != p.UserID || user.TenantID != p.TenantID {
		t.Fatalf("user = %#v", user)
	}
	if err := svc.EnsurePrincipal(context.Background(), p, "phone:19900001111", "Mobile User"); err != nil {
		t.Fatalf("EnsurePrincipal second: %v", err)
	}
	skills, err := svc.ListSkills(context.Background(), p)
	if err != nil {
		t.Fatalf("ListSkills after ensure: %v", err)
	}
	if skills == nil {
		skills = []corelib.NLSkillEntry{}
	}
	_ = skills
}

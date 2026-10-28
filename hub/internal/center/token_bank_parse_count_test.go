package center

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// countingSettings counts reads of the registry row. Each read is one full
// JSON parse in LoadRegistry.
type countingSettings struct {
	*fakeSettingsRepo
	registryReads atomic.Int64
}

func (c *countingSettings) Get(ctx context.Context, key string) (string, error) {
	if key == llmservice.RegistryKey {
		c.registryReads.Add(1)
	}
	return c.fakeSettingsRepo.Get(ctx, key)
}

// A pass with no confirm and no debit must parse the registry once in total,
// not once or twice per user (papers: 208 users x 2.3 MB kept the hub at 100%).
func TestRunTokenBankAutoOnceParsesRegistryOncePerQuietPass(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()
	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	base := newFakeSettingsRepo()
	ctx := context.Background()
	if err := base.Set(ctx, systemKeyCenterRegistration, mustJSON(registrationRecord{Registered: true, HubID: "hub-auto", HubSecret: "secret"})); err != nil {
		t.Fatal(err)
	}
	if err := llmservice.SaveRegistry(ctx, base, &llmservice.Registry{
		// An empty access policy is a free route: these users need no top-up.
		ModelServiceGroups:          []llmservice.ModelServiceGroup{{ID: "paid", Name: "Paid"}},
		SystemDefaultServiceGroupID: "paid",
		DefaultNewUserServiceGroups: []string{"paid"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := base.Set(ctx, "token_bank_auto_withdraw", `{"enabled":true,"service_group_id":"paid"}`); err != nil {
		t.Fatal(err)
	}
	settings := &countingSettings{fakeSettingsRepo: base}
	svc := NewService(cfg, settings)
	users := make([]*store.User, 0, 50)
	for i := 0; i < 50; i++ {
		users = append(users, &store.User{ID: fmt.Sprintf("user-%d", i), Email: fmt.Sprintf("u%d@example.com", i)})
	}
	svc.users = tokenBankUserList{users: users}
	_ = svc.RunTokenBankAutoOnce(ctx)
	got := settings.registryReads.Load()
	t.Logf("registry reads for 50 users: %d", got)
	if got > 5 {
		t.Fatalf("registry reads per pass = %d for 50 users, want O(1) when nothing is confirmed", got)
	}
}

package agentruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNewJobIdempotencyIdentityIsScopedStableAndNonReversible(t *testing.T) {
	requestA := map[string]any{"server_id": "mcp_1", "options": map[string]any{"deep": true}}
	requestB := map[string]any{"options": map[string]any{"deep": true}, "server_id": "mcp_1"}
	first, err := NewJobIdempotencyIdentity("tenant", "user", "mcp.health_check", "private-client-key", requestA)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewJobIdempotencyIdentity("tenant", "user", "mcp.health_check", "private-client-key", requestB)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("stable request produced different identity: %#v %#v", first, second)
	}
	if strings.Contains(first.Digest, "private") || strings.Contains(first.RequestDigest, "mcp_1") {
		t.Fatalf("identity exposed raw input: %#v", first)
	}
	otherUser, _ := NewJobIdempotencyIdentity("tenant", "other", "mcp.health_check", "private-client-key", requestA)
	if first.Digest == otherUser.Digest {
		t.Fatal("idempotency digest was not principal scoped")
	}
}

func TestJobIdempotencyIdentityDetectsRequestConflict(t *testing.T) {
	first, _ := NewJobIdempotencyIdentity("tenant", "user", "demo", "same-key", map[string]int{"value": 1})
	second, _ := NewJobIdempotencyIdentity("tenant", "user", "demo", "same-key", map[string]int{"value": 2})
	if first.Digest != second.Digest || first.RequestDigest == second.RequestDigest {
		t.Fatalf("unexpected conflict identities: %#v %#v", first, second)
	}
}

func TestValidateJobIdempotencyStateRejectsPartialOrResponseState(t *testing.T) {
	identity, _ := NewJobIdempotencyIdentity("tenant", "user", "demo", "key", nil)
	for _, test := range []struct {
		digest, requestDigest string
		replay                bool
	}{
		{digest: identity.Digest},
		{requestDigest: identity.RequestDigest},
		{digest: identity.Digest, requestDigest: identity.RequestDigest, replay: true},
		{replay: true},
	} {
		if err := ValidateJobIdempotencyState(test.digest, test.requestDigest, test.replay); !errors.Is(err, ErrInvalidJobIdempotency) {
			t.Fatalf("ValidateJobIdempotencyState(%#v) = %v", test, err)
		}
	}
}

func TestJobIdempotencyIdentityContextContainsOnlyDigests(t *testing.T) {
	identity, _ := NewJobIdempotencyIdentity("tenant", "user", "demo", "key", map[string]bool{"run": true})
	ctx := WithJobIdempotencyIdentity(context.Background(), identity)
	got, ok := JobIdempotencyIdentityFromContext(ctx)
	if !ok || got != identity {
		t.Fatalf("context identity = %#v, %v", got, ok)
	}
}

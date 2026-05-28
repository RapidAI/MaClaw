package im

import (
	"context"
	"testing"
)

type fakeSmartRouteUsers struct {
	enabled bool
}

func (f fakeSmartRouteUsers) GetSmartRouteByUserID(context.Context, string) (bool, error) {
	return f.enabled, nil
}

type fakeSmartRouteSettings struct {
	values map[string]string
}

func (f fakeSmartRouteSettings) Get(_ context.Context, key string) (string, error) {
	return f.values[key], nil
}

type fakeSmartRoutePolicy struct {
	allowed    bool
	controlled bool
}

func (f fakeSmartRoutePolicy) IsSmartRouteAllowedBySecurityPolicy(context.Context, string) (bool, bool) {
	return f.allowed, f.controlled
}

func TestSmartRouteCheckerSecurityPolicyDisablesSmartRoute(t *testing.T) {
	checker := NewDBSmartRouteChecker(
		fakeSmartRouteUsers{enabled: true},
		fakeSmartRouteSettings{values: map[string]string{"smart_route_all": "true"}},
	)
	checker.SetSecurityPolicyProvider(fakeSmartRoutePolicy{allowed: false, controlled: true})

	if checker.IsSmartRouteEnabled(context.Background(), "user-1") {
		t.Fatal("smart route should be disabled by centralized security policy")
	}
}

func TestSmartRouteCheckerSecurityPolicyAllowsExistingPermission(t *testing.T) {
	checker := NewDBSmartRouteChecker(
		fakeSmartRouteUsers{enabled: true},
		fakeSmartRouteSettings{values: map[string]string{"smart_route_all": "false"}},
	)
	checker.SetSecurityPolicyProvider(fakeSmartRoutePolicy{allowed: true, controlled: true})

	if !checker.IsSmartRouteEnabled(context.Background(), "user-1") {
		t.Fatal("smart route should follow existing user permission when security policy allows it")
	}
}

func TestSmartRouteCheckerSecurityPolicyDoesNotGrantExistingPermission(t *testing.T) {
	checker := NewDBSmartRouteChecker(
		fakeSmartRouteUsers{enabled: false},
		fakeSmartRouteSettings{values: map[string]string{"smart_route_all": "false"}},
	)
	checker.SetSecurityPolicyProvider(fakeSmartRoutePolicy{allowed: true, controlled: true})

	if checker.IsSmartRouteEnabled(context.Background(), "user-1") {
		t.Fatal("security policy allow must not grant smart route when existing permission is off")
	}
}

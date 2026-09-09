package main

import "testing"

func TestParseRuntimeRateLimitConfig(t *testing.T) {
	t.Setenv(runtimeRateLimitRateEnv, "2.5")
	t.Setenv(runtimeRateLimitBurstEnv, "4")
	t.Setenv(runtimeRateLimitTenantEnv, "8")
	rate, err := parseRuntimeRateLimitRate()
	if err != nil || rate != 2.5 {
		t.Fatalf("rate=%v err=%v", rate, err)
	}
	burst, err := parseRuntimeRateLimitInt(runtimeRateLimitBurstEnv, 0, 1_000_000)
	if err != nil || burst != 4 {
		t.Fatalf("burst=%v err=%v", burst, err)
	}
	tenantLimit, err := parseRuntimeRateLimitInt(runtimeRateLimitTenantEnv, 0, 1_000_000)
	if err != nil || tenantLimit != 8 {
		t.Fatalf("tenant limit=%v err=%v", tenantLimit, err)
	}
	t.Setenv(runtimeRateLimitSharedEnv, "true")
	shared, err := parseRuntimeRateLimitShared()
	if err != nil || !shared {
		t.Fatalf("shared=%v err=%v", shared, err)
	}
}

func TestParseRuntimeRateLimitConfigRejectsMalformedValues(t *testing.T) {
	t.Setenv(runtimeRateLimitRateEnv, "not-a-number")
	if _, err := parseRuntimeRateLimitRate(); err == nil {
		t.Fatal("malformed rate should fail closed")
	}
	t.Setenv(runtimeRateLimitRateEnv, "0")
	t.Setenv(runtimeRateLimitBurstEnv, "-1")
	if _, err := parseRuntimeRateLimitInt(runtimeRateLimitBurstEnv, 0, 1_000_000); err == nil {
		t.Fatal("negative burst should fail closed")
	}
	t.Setenv(runtimeRateLimitSharedEnv, "maybe")
	if _, err := parseRuntimeRateLimitShared(); err == nil {
		t.Fatal("malformed shared flag should fail closed")
	}
}

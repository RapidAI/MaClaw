package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

const (
	runtimeRateLimitRateEnv   = "MACLAW_RUNTIME_RATE_LIMIT_RATE"
	runtimeRateLimitBurstEnv  = "MACLAW_RUNTIME_RATE_LIMIT_BURST"
	runtimeRateLimitTenantEnv = "MACLAW_RUNTIME_RATE_LIMIT_TENANT_LIMIT"
	runtimeRateLimitSharedEnv = "MACLAW_RUNTIME_RATE_LIMIT_SHARED"
)

// parseRuntimeRateLimitRate keeps rate-limit configuration fail-closed: a
// malformed value must not silently disable protection. An absent or blank
// value intentionally means unlimited for backwards compatibility.
func parseRuntimeRateLimitRate() (float64, error) {
	raw := strings.TrimSpace(os.Getenv(runtimeRateLimitRateEnv))
	if raw == "" {
		return 0, nil
	}
	rate, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate > 1_000_000 {
		return 0, fmt.Errorf("invalid %s: want a number in [0,1000000]", runtimeRateLimitRateEnv)
	}
	return rate, nil
}

func parseRuntimeRateLimitShared() (bool, error) {
	raw := strings.TrimSpace(os.Getenv(runtimeRateLimitSharedEnv))
	if raw == "" {
		return false, nil
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s: want true/false", runtimeRateLimitSharedEnv)
	}
}

func parseRuntimeRateLimitInt(name string, fallback, maximum int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 || value > maximum {
		return 0, fmt.Errorf("invalid %s: want an integer in [%d,%d]", name, 0, maximum)
	}
	return value, nil
}

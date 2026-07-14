package moa

import (
	"os"
	"strings"
)

// Env value semantics for MACLAW_MOA:
//
//	on / true / 1 / yes  → force allow (still needs config.Enabled for EffectiveEnabled)
//	off / false / 0 / no → force deny (kill switch)
//	unset / empty        → allow when config.Enabled (UI enable is enough)
func envRaw() string {
	return strings.TrimSpace(strings.ToLower(os.Getenv("MACLAW_MOA")))
}

// EnvForcedOff is true when MACLAW_MOA explicitly disables MoA.
func EnvForcedOff() bool {
	switch envRaw() {
	case "0", "false", "off", "no":
		return true
	default:
		return false
	}
}

// EnvForcedOn is true when MACLAW_MOA explicitly enables the kill-switch open.
func EnvForcedOn() bool {
	switch envRaw() {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// EnvAllows reports whether the environment permits MoA.
// Forced off → false; forced on or unset → true (config still required).
func EnvAllows() bool {
	if EnvForcedOff() {
		return false
	}
	return true
}

// EffectiveEnabled is true when MoA may run: not env-forced-off AND config enabled.
func EffectiveEnabled(configEnabled bool) bool {
	return EnvAllows() && configEnabled
}

// EnvStatusLabel is a short doctor/UI label for the env gate.
func EnvStatusLabel() string {
	if EnvForcedOff() {
		return "off"
	}
	if EnvForcedOn() {
		return "on"
	}
	return "auto" // unset → follow config
}

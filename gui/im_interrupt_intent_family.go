package main

import "strings"

type interruptIntentFamilyKind int

const (
	interruptIntentFamilyUnknown interruptIntentFamilyKind = iota
	interruptIntentFamilyCoding
)

func classifyInterruptIntentFamily(intent string) interruptIntentFamilyKind {
	switch strings.ToLower(strings.TrimSpace(intent)) {
	case intentCoding.String(), "bug_fix", "maintenance":
		return interruptIntentFamilyCoding
	default:
		return interruptIntentFamilyUnknown
	}
}

func sameInterruptIntentFamily(a, b string) bool {
	familyA := classifyInterruptIntentFamily(a)
	return familyA != interruptIntentFamilyUnknown && familyA == classifyInterruptIntentFamily(b)
}

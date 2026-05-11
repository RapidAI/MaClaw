package main

import "strings"

type imMemoryRecallMode string

const (
	imMemoryRecallModeDynamic  imMemoryRecallMode = "dynamic"
	imMemoryRecallModeHybrid   imMemoryRecallMode = "hybrid"
	imMemoryRecallModeAuto     imMemoryRecallMode = "auto"
	imMemoryRecallModeAdaptive imMemoryRecallMode = "adaptive"
	imMemoryRecallModeInvalid  imMemoryRecallMode = "invalid"
)

func normalizeIMMemoryRecallMode(value string) imMemoryRecallMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "dynamic":
		return imMemoryRecallModeDynamic
	case "hybrid", "recall":
		return imMemoryRecallModeHybrid
	case "auto":
		return imMemoryRecallModeAuto
	case "adaptive", "hier", "adaptive_hier":
		return imMemoryRecallModeAdaptive
	default:
		return imMemoryRecallModeInvalid
	}
}

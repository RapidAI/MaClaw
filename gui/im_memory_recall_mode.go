package main

import "strings"

type imMemoryRecallMode string

const (
	imMemoryRecallModeDynamic  imMemoryRecallMode = "dynamic"
	imMemoryRecallModeHybrid   imMemoryRecallMode = "hybrid"
	imMemoryRecallModeLightMem imMemoryRecallMode = "lightmem"
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
	case "lightmem", "light_mem", "planned":
		return imMemoryRecallModeLightMem
	case "auto":
		return imMemoryRecallModeAuto
	case "adaptive", "hier", "adaptive_hier":
		return imMemoryRecallModeAdaptive
	default:
		return imMemoryRecallModeInvalid
	}
}

package main

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestFormatTUIStartupLineFitsNarrowTerminals(t *testing.T) {
	line := formatTUIStartupLine("AICoder", "|", 42, "加载一个很长很长的启动阶段名称", 32)
	if got := runewidth.StringWidth(line); got > 32 {
		t.Fatalf("line width = %d, want <= 32: %q", got, line)
	}
	if strings.Contains(line, "\n") || strings.Contains(line, "\r") {
		t.Fatalf("line should stay on one terminal row: %q", line)
	}
}

func TestFormatTUIStartupLineClampsPercent(t *testing.T) {
	line := formatTUIStartupLine("AICoder", "|", 150, "ready", 80)
	if !strings.Contains(line, " 99%") {
		t.Fatalf("line = %q, want clamped 99%%", line)
	}

	line = formatTUIStartupLine("AICoder", "|", -10, "ready", 80)
	if !strings.Contains(line, "  0%") {
		t.Fatalf("line = %q, want clamped 0%%", line)
	}
}

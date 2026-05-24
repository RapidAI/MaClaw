package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/mattn/go-isatty"
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

type tuiStartupIndicator struct {
	enabled bool
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
	mu      sync.Mutex
	percent int
	label   string
	frame   int
	width   int
}

func startTUIStartupIndicator() *tuiStartupIndicator {
	ind := &tuiStartupIndicator{}
	if !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		return ind
	}
	ind.enabled = true
	ind.done = make(chan struct{})
	ind.stopped = make(chan struct{})
	ind.percent = 8
	ind.label = "启动中"
	ind.width = terminalWidth(os.Stdout.Fd())
	fmt.Fprint(os.Stdout, "\x1b[?25l")
	ind.render()
	go ind.loop()
	return ind
}

func (i *tuiStartupIndicator) Stage(percent int, label string) {
	if i == nil || !i.enabled {
		return
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 99 {
		percent = 99
	}
	i.mu.Lock()
	i.percent = percent
	if strings.TrimSpace(label) != "" {
		i.label = label
	}
	i.mu.Unlock()
	i.render()
}

func (i *tuiStartupIndicator) Stop() {
	if i == nil || !i.enabled {
		return
	}
	i.once.Do(func() {
		close(i.done)
		<-i.stopped
		fmt.Fprint(os.Stdout, "\r\x1b[2K\x1b[?25h")
	})
}

func (i *tuiStartupIndicator) loop() {
	ticker := time.NewTicker(90 * time.Millisecond)
	defer ticker.Stop()
	defer close(i.stopped)
	for {
		select {
		case <-i.done:
			return
		case <-ticker.C:
			i.mu.Lock()
			i.frame++
			i.mu.Unlock()
			i.render()
		}
	}
}

func (i *tuiStartupIndicator) render() {
	i.mu.Lock()
	defer i.mu.Unlock()
	percent := i.percent
	label := i.label
	frame := i.frame
	maxWidth := i.width
	if maxWidth <= 0 {
		maxWidth = terminalWidth(os.Stdout.Fd())
	}

	spinner := []string{"|", "/", "-", "\\"}[frame%4]
	name := brand.Current().DisplayName
	fmt.Fprint(os.Stdout, "\r\x1b[2K"+formatTUIStartupLine(name, spinner, percent, label, maxWidth))
}

func terminalWidth(fd uintptr) int {
	width, _, err := term.GetSize(int(fd))
	if err != nil || width <= 0 {
		return 80
	}
	return width
}

func formatTUIStartupLine(name, spinner string, percent int, label string, maxWidth int) string {
	if maxWidth <= 0 {
		maxWidth = 80
	}
	barWidth := 24
	if maxWidth < 64 {
		barWidth = 12
	}
	if maxWidth < 42 {
		barWidth = 8
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 99 {
		percent = 99
	}
	filled := percent * barWidth / 100
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
	line := fmt.Sprintf("%s TUI %s [%s] %3d%%  %s", name, spinner, bar, percent, label)
	if runewidth.StringWidth(line) <= maxWidth {
		return line
	}
	if maxWidth <= 0 {
		return ""
	}
	return runewidth.Truncate(line, maxWidth, "")
}

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

const browserAutomationSkillRejectionGuidance = "use the stable browser tool/session mechanism instead of shell Playwright/CDP/screenshot scripts"

// Browser automation must go through the managed browser session/tool path.
// Shell/CDP/Playwright skills create separate tabs/process lifecycles and bypass
// the shared state model, so they are rejected at discovery and execution.
func isShellBrowserAutomationSkill(s NLSkillDefinition) bool {
	return isShellBrowserAutomationSkillEntry(corelib.NLSkillEntry{
		Name:                s.Name,
		Description:         s.Description,
		Triggers:            s.Triggers,
		Status:              s.Status,
		Steps:               s.Steps,
		RequiresGUI:         s.RequiresGUI,
		RequiresTools:       s.RequiresTools,
		FallbackForTools:    s.FallbackForTools,
		RequiresToolsets:    s.RequiresToolsets,
		FallbackForToolsets: s.FallbackForToolsets,
		SkillDir:            s.SkillDir,
	})
}

func isShellBrowserAutomationSkillEntry(s corelib.NLSkillEntry) bool {
	if skillDeclaresBrowserSurface(s) {
		return true
	}
	if skillDirHasBrowserAutomationMarkers(s.SkillDir) {
		return true
	}
	if skillStepsHaveBrowserAutomationMarkers(s.Steps) {
		return true
	}
	if !s.RequiresGUI {
		return false
	}
	skillText := strings.ToLower(strings.Join(append([]string{s.Name, s.Description}, s.Triggers...), " "))
	for _, marker := range []string{"playwright", "puppeteer", "selenium", "pyppeteer", "connect_over_cdp", "remote-debugging", "chromium.launch", "chrome --remote-debugging", "cdp", "browser automation"} {
		if strings.Contains(skillText, marker) {
			return true
		}
	}
	return false
}

func skillStepsHaveBrowserAutomationMarkers(steps []corelib.NLSkillStep) bool {
	for _, step := range steps {
		if !strings.EqualFold(strings.TrimSpace(step.Action), "bash") {
			continue
		}
		cmd := strings.ToLower(strings.TrimSpace(browserSkillStringParam(step.Params, "command")))
		if cmd == "" {
			continue
		}
		for _, marker := range []string{"playwright", "puppeteer", "selenium", "pyppeteer", "connect_over_cdp", "remote-debugging", "--remote-debugging-port", "chromium.launch", "chrome.exe", "chrome --", "chromium --", "cdp", "--screenshot"} {
			if strings.Contains(cmd, marker) {
				return true
			}
		}
	}
	return false
}

func skillDirHasBrowserAutomationMarkers(skillDir string) bool {
	skillDir = strings.TrimSpace(skillDir)
	if skillDir == "" {
		return false
	}
	info, err := os.Stat(skillDir)
	if err != nil || !info.IsDir() {
		return false
	}
	filesScanned := 0
	const maxFiles = 24
	const maxBytes = 512 * 1024
	walkErr := filepath.WalkDir(skillDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			switch strings.ToLower(d.Name()) {
			case ".git", "node_modules", ".venv", "venv", "__pycache__", "dist", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if filesScanned >= maxFiles || !isBrowserAutomationScanFile(path) {
			return nil
		}
		filesScanned++
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if len(data) > maxBytes {
			data = data[:maxBytes]
		}
		if textHasBrowserAutomationMarker(strings.ToLower(string(data))) {
			return errSkillDirBrowserAutomationFound
		}
		return nil
	})
	return errors.Is(walkErr, errSkillDirBrowserAutomationFound)
}

var errSkillDirBrowserAutomationFound = fmt.Errorf("browser automation marker found")

func isBrowserAutomationScanFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".sh", ".ps1", ".yaml", ".yml", ".md":
		return true
	default:
		return false
	}
}

func textHasBrowserAutomationMarker(text string) bool {
	for _, marker := range []string{
		"connect_over_cdp", "remote-debugging-port", "chromium.launch", "firefox.launch", "webkit.launch",
		"async_playwright", "sync_playwright", "from playwright", "require('playwright", "require(\"playwright",
		"require('puppeteer", "require(\"puppeteer", "from selenium", "import selenium", "webdriver.chrome",
		"ctx.new_page", "context.new_page", ".new_page()", ".newpage()",
		"page.screenshot", ".screenshot(", "browser.close()", "127.0.0.1:3888",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func browserAutomationSkillRejectedError(skillName string) error {
	return fmt.Errorf("skill %q is disabled for browser automation: %s", skillName, browserAutomationSkillRejectionGuidance)
}

func skillDeclaresBrowserSurface(s corelib.NLSkillEntry) bool {
	for _, values := range [][]string{s.RequiresTools, s.FallbackForTools, s.RequiresToolsets, s.FallbackForToolsets} {
		for _, value := range values {
			if strings.EqualFold(strings.TrimSpace(value), "browser") {
				return true
			}
		}
	}
	return false
}

func browserSkillStringParam(params map[string]interface{}, key string) string {
	if params == nil {
		return ""
	}
	if s, ok := params[key].(string); ok {
		return s
	}
	return ""
}

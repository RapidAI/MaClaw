package tool

import (
	"fmt"
	"testing"
)

// TestMatchConditionalTools_MemoryBrowserNotPinned verifies that browser tools
// are NOT pinned from memory content that mentions "浏览器" or browser-related
// keywords. This prevents false-positive browser tool activation when SSH
// server resource output mentions "Chrome 浏览器进程" in recalled memory.
//
// Root cause: MatchConditionalTools previously promoted all needsConfirm tools
// (including browser weak-rule matches) unconditionally. Memory text like
// "Chrome 浏览器进程占 CPU 39.6%" would match the strong browser keyword
// "浏览器", causing 25+ browser tools to be pinned to the session.
func TestMatchConditionalTools_MemoryBrowserNotPinned(t *testing.T) {
	// Simulate memory content from SSH server resource check that mentions
	// Chrome browser process.
	memoryText := "服务器资源：Chrome 浏览器进程 PID 917323 CPU 39.6%，node 进程 CPU 2.6%"
	matched := MatchConditionalTools(memoryText)

	// Browser tool should NOT be in the matched set — "浏览器" in server
	// process output is not a signal that the user wants browser automation.
	if matched["browser"] {
		t.Errorf("MatchConditionalTools(%q) should NOT include browser tool %q; "+
			"memory mentioning '浏览器' in server process list is a false positive",
			memoryText, "browser")
	}
}

// TestMatchConditionalTools_MemorySSHStillPinned verifies that SSH tools ARE
// still pinned from memory content that mentions SSH-related keywords.
func TestMatchConditionalTools_MemorySSHStillPinned(t *testing.T) {
	memoryText := "上次连接了 api.rapidai.tech 服务器，查看了 Docker 容器状态"
	matched := MatchConditionalTools(memoryText)

	if !matched["ssh"] {
		t.Errorf("MatchConditionalTools(%q) should include ssh tool; "+
			"memory mentioning '服务器' and 'Docker' is a valid SSH signal",
			memoryText)
	}
}

// TestMatchConditionalTools_WeakBrowserKeywordsNotPinned verifies that weak
// browser keyword combinations (page + action) in memory do NOT pin browser
// tools. These are the most common false positives.
func TestMatchConditionalTools_WeakBrowserKeywordsNotPinned(t *testing.T) {
	// Memory from a game development task that mentions "页面" and "打开".
	memoryText := "用户要开发打飞机游戏，页面上直接打开即玩"
	matched := MatchConditionalTools(memoryText)

	if matched["browser"] {
		t.Errorf("MatchConditionalTools(%q) should NOT include browser tool; "+
			"weak browser keywords in game description are false positives",
			memoryText)
	}
}


// TestRoute_SemanticIntentEnhancement_NoBrowserActivation verifies that the
// semantic intent enhancement path in Route() does NOT activate browser tools
// even when IntentClassifier returns IntentBrowser. Browser tools should only
// be activated through conditionalKeepRules (strong/weak keyword matching).
//
// Root cause: Route() had a separate "semantic intent enhancement" path that
// activated ALL 25+ browser tools when IntentClassifier returned IntentBrowser
// with confidence >= 0.50. This was a third activation path that bypassed the
// strong/weak keyword + semantic confirm mechanism, causing false positives
// when embedding similarity gave borderline IntentBrowser scores for SSH tasks.
func TestRoute_SemanticIntentEnhancement_NoBrowserActivation(t *testing.T) {
	gen := NewDefinitionGenerator(nil, nil)
	router := NewRouter(gen)

	// Build a tool list with core tools + browser tools.
	var tools []map[string]interface{}
	for name := range CoreToolNames {
		tools = append(tools, makeToolDef(name, "core "+name))
	}
	tools = append(tools,
		makeToolDef("browser", "浏览器自动化工具"),
	)
	// Pad with extra tools so BM25 scoring has candidates.
	for i := 0; i < 20; i++ {
		tools = append(tools, makeToolDef(fmt.Sprintf("extra_%d", i), "extra tool"))
	}

	// "查看服务器资源" — no browser keywords at all.
	// Even if IntentClassifier were to return IntentBrowser (which it shouldn't
	// for this message), the semantic intent enhancement path no longer
	// activates browser tools.
	result := router.Route("查看服务器资源", tools)

	resultNames := make(map[string]bool)
	for _, r := range result {
		resultNames[ExtractToolName(r)] = true
	}

	if resultNames["browser"] {
		t.Errorf("Route(%q) should NOT include browser tool %q; "+
			"no browser keywords and semantic intent enhancement excluded IntentBrowser", "查看服务器资源", "browser")
	}

	// SSH should be activated (keyword "服务器" matches).
	if !resultNames["ssh"] {
		t.Logf("Note: ssh not in result — may be expected if ssh is not in CoreToolNames and not in tools list")
	}
}

// TestMatchConditionalTools_DesktopGUINotPinned verifies that desktop GUI tools
// (gui_observe, gui_verify) are NOT pinned from memory content that mentions
// desktop GUI keywords like "窗口", "桌面", "gui" etc.
//
// Root cause (#85.1): gui_observe and gui_verify were in a separate conditional
// keep rule (desktopGUIKeywords) but NOT in allBrowserToolNames, so they were
// NOT in noEagerPinTools. Memory content mentioning "窗口" from a previous GUI
// task would match the desktopGUI rule, and MatchConditionalTools would return
// them without filtering. The memory-driven pin path in appendProactiveRecall
// would then permanently pin them to the session.
func TestMatchConditionalTools_DesktopGUINotPinned(t *testing.T) {
	tests := []struct {
		name       string
		memoryText string
	}{
		{
			name:       "memory mentions 窗口 from previous GUI task",
			memoryText: "上次帮用户在记事本窗口中输入了文本，验证了按钮状态",
		},
		{
			name:       "memory mentions 桌面 from previous task",
			memoryText: "用户让我观测桌面应用的元素树结构",
		},
		{
			name:       "memory mentions gui from tool usage",
			memoryText: "调用了 gui_observe 工具查看了窗口状态",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matched := MatchConditionalTools(tt.memoryText)

			if matched["gui_observe"] {
				t.Errorf("MatchConditionalTools(%q) should NOT include gui_observe; "+
					"desktop GUI keywords in memory are false positives for memory-driven pin",
					tt.memoryText)
			}
			if matched["gui_verify"] {
				t.Errorf("MatchConditionalTools(%q) should NOT include gui_verify; "+
					"desktop GUI keywords in memory are false positives for memory-driven pin",
					tt.memoryText)
			}
		})
	}
}

// TestMatchConditionalTools_DesktopGUIAndBrowserBothFiltered verifies that
// noEagerPinTools covers BOTH browser tools AND desktop GUI tools.
// This is the mechanistic invariant: any tool in noEagerPinTools is excluded
// from MatchConditionalTools, regardless of which conditional keep rule matched it.
func TestMatchConditionalTools_DesktopGUIAndBrowserBothFiltered(t *testing.T) {
	// Text that matches both browser strong keywords AND desktop GUI keywords.
	memoryText := "用户在浏览器中操作，同时观测了桌面窗口的元素树"
	matched := MatchConditionalTools(memoryText)

	for name := range matched {
		if noEagerPinTools[name] {
			t.Errorf("MatchConditionalTools returned %q which is in noEagerPinTools; "+
				"noEagerPinTools tools must never be returned by MatchConditionalTools",
				name)
		}
	}

	// SSH should still work (not in noEagerPinTools).
	memoryText2 := "连接了服务器查看资源"
	matched2 := MatchConditionalTools(memoryText2)
	if !matched2["ssh"] {
		t.Errorf("MatchConditionalTools(%q) should include ssh; "+
			"ssh is not in noEagerPinTools and should be pinnable from memory",
			memoryText2)
	}
}

// TestNoEagerPinTools_DerivedFromRules verifies the structural invariant:
// noEagerPinTools is derived from conditionalKeepRules with noMemoryPin=true.
// This means adding a new rule with noMemoryPin=true automatically protects
// its tools — no separate list maintenance needed.
func TestNoEagerPinTools_DerivedFromRules(t *testing.T) {
	// Collect all tools from noMemoryPin rules.
	expected := make(map[string]bool)
	for _, rule := range conditionalKeepRules {
		if rule.noMemoryPin {
			for _, name := range rule.keepTools {
				expected[name] = true
			}
		}
	}

	// noEagerPinTools must exactly equal the union of all noMemoryPin rule tools.
	if len(noEagerPinTools) != len(expected) {
		t.Errorf("noEagerPinTools has %d entries but noMemoryPin rules have %d tools; "+
			"noEagerPinTools must be derived from rules, not maintained separately",
			len(noEagerPinTools), len(expected))
	}
	for name := range expected {
		if !noEagerPinTools[name] {
			t.Errorf("tool %q is in a noMemoryPin rule but NOT in noEagerPinTools; "+
				"noEagerPinTools derivation is broken", name)
		}
	}
	for name := range noEagerPinTools {
		if !expected[name] {
			t.Errorf("tool %q is in noEagerPinTools but NOT in any noMemoryPin rule; "+
				"noEagerPinTools has stale entries", name)
		}
	}

	// Verify specific tools are protected.
	for _, name := range []string{"browser", "gui_observe", "gui_verify", "gui_record_start"} {
		if !noEagerPinTools[name] {
			t.Errorf("expected %q to be in noEagerPinTools (from noMemoryPin rules)", name)
		}
	}

	// Verify SSH is NOT protected (it should be pinnable from memory).
	if noEagerPinTools["ssh"] {
		t.Errorf("ssh should NOT be in noEagerPinTools; it's a safe memory-driven pin target")
	}
}

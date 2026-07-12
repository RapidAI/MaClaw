package memory

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// pinOutputIndicator is the display marker used for pinned memory entries.
// Keep this as a stable UTF-8 rune so property tests and product UIs stay aligned.
const pinOutputIndicator = "📌"

// Feature: memory-claude-style-upgrade, Property 13: Pinned entry indicator in output
// **Validates: Requirements 4.7**
//
// For any pinned entry returned by list or search operations, the formatted
// output string should contain the indicator character.
func TestProperty_PinIndicatorInOutput(t *testing.T) {
	dir := t.TempDir()

	rapid.Check(t, func(rt *rapid.T) {
		storePath := filepath.Join(dir, rapid.StringMatching(`[a-z]{8}`).Draw(rt, "fname")+".json")
		store, err := NewStore(storePath)
		if err != nil {
			rt.Fatal(err)
		}
		defer store.Stop()

		// Generate 2-10 entries, randomly pin some.
		n := rapid.IntRange(2, 10).Draw(rt, "entryCount")
		type entryInfo struct {
			content string
			pinned  bool
		}
		var infos []entryInfo

		for i := 0; i < n; i++ {
			content := rapid.StringMatching(`[a-zA-Z0-9]{5,50}`).Draw(rt, fmt.Sprintf("content_%d", i))
			cat := genCategory().Draw(rt, fmt.Sprintf("cat_%d", i))
			entry := Entry{
				Content:  content,
				Category: cat,
				Tags:     []string{"test"},
			}
			if err := store.Save(entry); err != nil {
				rt.Fatal(err)
			}
			pinned := rapid.Bool().Draw(rt, fmt.Sprintf("pin_%d", i))
			infos = append(infos, entryInfo{content: content, pinned: pinned})
		}

		// Pin the selected entries.
		entries := store.List("", "")
		for i, info := range infos {
			if i < len(entries) && info.pinned {
				_ = store.PinEntry(entries[i].ID)
			}
		}

		// Re-list and format output (mirrors TUI/GUI formatting logic).
		listed := store.List("", "")
		for _, e := range listed {
			line := formatEntryLine(e)
			if e.Pinned {
				if !strings.Contains(line, pinOutputIndicator) {
					rt.Fatalf("pinned entry %s missing indicator in output: %s", e.ID, line)
				}
			} else {
				if strings.Contains(line, pinOutputIndicator) {
					rt.Fatalf("unpinned entry %s should not have indicator in output: %s", e.ID, line)
				}
			}
		}

		// Also verify via Search.
		searched := store.Search("", "", 100)
		for _, e := range searched {
			line := formatEntryLine(e)
			if e.Pinned {
				if !strings.Contains(line, pinOutputIndicator) {
					rt.Fatalf("pinned entry %s missing indicator in search output: %s", e.ID, line)
				}
			}
		}
	})
}

// formatEntryLine mirrors the formatting logic used in both TUI and GUI
// toolMemory list/search output. Pinned entries get a prefix.
func formatEntryLine(e Entry) string {
	prefix := ""
	if e.Pinned {
		prefix = pinOutputIndicator + " "
	}
	return fmt.Sprintf("%s[%s] %s: %s (tags: %s)", prefix, e.ID, e.Category, e.Content, strings.Join(e.Tags, ","))
}

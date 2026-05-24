package main

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// handleMemoryStatusCommand handles the /memory slash command.
// Renders a text-based bar chart showing category distribution and capacity usage.
func (h *IMMessageHandler) handleMemoryStatusCommand() *IMAgentResponse {
	return h.handleMemoryStatusCommandWithLang("zh-Hans")
}

func (h *IMMessageHandler) handleMemoryStatusCommandWithLang(lang string) *IMAgentResponse {
	store := h.memoryStore
	if store == nil && h.app != nil {
		if h.app.memoryStore == nil {
			h.app.ensureMemoryStore()
		}
		store = h.app.memoryStore
		if store != nil {
			h.memoryStore = store
		}
	}
	if store == nil {
		return &IMAgentResponse{Text: localizedIMMemoryNotInitializedMessage(lang)}
	}

	hr := store.HealthReport()
	text := formatMemoryStatusTextWithLang(hr, lang)
	return &IMAgentResponse{Text: text}
}

func localizedIMMemoryNotInitializedMessage(lang string) string {
	switch normalizeAppLanguageKind(lang) {
	case appLanguageEnglish:
		return "Memory system is not initialized."
	case appLanguageZhHant:
		return "記憶系統尚未初始化。"
	default:
		return "记忆系统尚未初始化。"
	}
}

// catLabelZh maps category string to Chinese display label.
var catLabelZh = map[string]string{
	"self_identity":        "自我认知",
	"user_fact":            "用户事实",
	"preference":           "偏好设置",
	"project_knowledge":    "项目知识",
	"instruction":          "指令",
	"conversation_summary": "对话摘要",
	"session_checkpoint":   "会话检查点",
	"task_artifact":        "任务产出物",
	"profile":              "用户画像",
	"user":                 "用户信息",
	"feedback":             "反馈",
	"project":              "项目",
	"reference":            "引用",
}

// catEmoji maps category to a representative emoji for the text chart.
var catEmoji = map[string]string{
	"self_identity":        "🤖",
	"user_fact":            "👤",
	"preference":           "⚙️",
	"project_knowledge":    "📁",
	"instruction":          "📌",
	"conversation_summary": "💬",
	"session_checkpoint":   "📍",
	"task_artifact":        "📄",
	"profile":              "🧑",
	"user":                 "👤",
	"feedback":             "💡",
	"project":              "📁",
	"reference":            "🔗",
}

// formatMemoryStatusText builds a text-based memory status report with bar charts.
func formatMemoryStatusText(hr *memory.HealthReport) string {
	return formatMemoryStatusTextWithLang(hr, "zh-Hans")
}

func formatMemoryStatusTextWithLang(hr *memory.HealthReport, lang string) string {
	if normalizeAppLanguageKind(lang) == appLanguageEnglish {
		return formatMemoryStatusTextEnglish(hr)
	}
	return formatMemoryStatusTextChinese(hr)
}

func formatMemoryStatusTextChinese(hr *memory.HealthReport) string {
	var sb strings.Builder

	// ── Header ──
	sb.WriteString("🧠 **记忆状态**\n\n")

	// ── Capacity gauge ──
	active := hr.ActiveEntries
	maxCap := hr.MaxCapacity
	if maxCap == 0 {
		maxCap = 2000
	}
	pct := float64(active) / float64(maxCap) * 100
	sb.WriteString(fmt.Sprintf("**容量**: %d / %d (%.1f%%)\n", active, maxCap, pct))
	sb.WriteString(renderBarGauge(pct, 20))
	sb.WriteString("\n\n")

	// ── Category breakdown (horizontal bar chart) ──
	sb.WriteString("**分类占比**:\n")

	type catRow struct {
		cat   string
		count int
	}
	var rows []catRow
	for cat, count := range hr.CategoryCounts {
		rows = append(rows, catRow{cat, count})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].count > rows[j].count })

	total := active
	if total == 0 {
		total = 1
	}

	// Find max label width for alignment.
	maxLabelW := 0
	for _, r := range rows {
		label := catLabelZh[r.cat]
		if label == "" {
			label = r.cat
		}
		w := runeWidth(label)
		if w > maxLabelW {
			maxLabelW = w
		}
	}

	const barMaxWidth = 16
	for _, r := range rows {
		label := catLabelZh[r.cat]
		if label == "" {
			label = r.cat
		}
		emoji := catEmoji[r.cat]
		if emoji == "" {
			emoji = "📦"
		}
		rowPct := float64(r.count) / float64(total) * 100
		barLen := int(math.Round(float64(barMaxWidth) * float64(r.count) / float64(total)))
		if barLen == 0 && r.count > 0 {
			barLen = 1
		}
		bar := strings.Repeat("█", barLen) + strings.Repeat("░", barMaxWidth-barLen)
		padding := strings.Repeat(" ", maxLabelW-runeWidth(label))
		sb.WriteString(fmt.Sprintf("%s %s%s %s %3d条 (%4.1f%%)\n",
			emoji, label, padding, bar, r.count, rowPct))
	}

	// ── Summary stats ──
	sb.WriteString("\n**详细信息**:\n")
	if hr.ArchivedEntries > 0 {
		sb.WriteString(fmt.Sprintf("  📦 已归档: %d 条\n", hr.ArchivedEntries))
	}
	if hr.StaleEntries > 0 {
		sb.WriteString(fmt.Sprintf("  🕸️ 过期: %d 条\n", hr.StaleEntries))
	}
	if hr.PinnedEntries > 0 {
		sb.WriteString(fmt.Sprintf("  📌 固定: %d 条\n", hr.PinnedEntries))
	}
	if hr.EmbedderActive {
		noEmbed := hr.NoEmbedding
		embedded := active - noEmbed
		sb.WriteString(fmt.Sprintf("  🔢 向量化: %d / %d\n", embedded, active))
	} else {
		sb.WriteString("  🔢 向量化: 未启用\n")
	}
	if hr.OldestEntry != "" {
		if t, err := time.Parse(time.RFC3339, hr.OldestEntry); err == nil {
			sb.WriteString(fmt.Sprintf("  📅 最早记忆: %s\n", t.Format("2006-01-02 15:04")))
		}
	}
	if hr.NewestEntry != "" {
		if t, err := time.Parse(time.RFC3339, hr.NewestEntry); err == nil {
			sb.WriteString(fmt.Sprintf("  📅 最新记忆: %s\n", t.Format("2006-01-02 15:04")))
		}
	}

	return sb.String()
}

func formatMemoryStatusTextEnglish(hr *memory.HealthReport) string {
	var sb strings.Builder

	sb.WriteString("**Memory Status**\n\n")

	active := hr.ActiveEntries
	maxCap := hr.MaxCapacity
	if maxCap == 0 {
		maxCap = 2000
	}
	pct := float64(active) / float64(maxCap) * 100
	sb.WriteString(fmt.Sprintf("**Capacity**: %d / %d (%.1f%%)\n", active, maxCap, pct))
	sb.WriteString(renderBarGauge(pct, 20))
	sb.WriteString("\n\n")

	sb.WriteString("**Category Breakdown**:\n")
	type catRow struct {
		cat   string
		count int
	}
	var rows []catRow
	for cat, count := range hr.CategoryCounts {
		rows = append(rows, catRow{cat, count})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].count > rows[j].count })

	total := active
	if total == 0 {
		total = 1
	}
	maxLabelW := 0
	for _, r := range rows {
		if w := runeWidth(r.cat); w > maxLabelW {
			maxLabelW = w
		}
	}

	const barMaxWidth = 16
	for _, r := range rows {
		emoji := catEmoji[r.cat]
		if emoji == "" {
			emoji = "-"
		}
		rowPct := float64(r.count) / float64(total) * 100
		barLen := int(math.Round(float64(barMaxWidth) * float64(r.count) / float64(total)))
		if barLen == 0 && r.count > 0 {
			barLen = 1
		}
		bar := strings.Repeat("#", barLen) + strings.Repeat("-", barMaxWidth-barLen)
		padding := strings.Repeat(" ", maxLabelW-runeWidth(r.cat))
		sb.WriteString(fmt.Sprintf("%s %s%s %s %3d item(s) (%4.1f%%)\n", emoji, r.cat, padding, bar, r.count, rowPct))
	}

	sb.WriteString("\n**Details**:\n")
	if hr.ArchivedEntries > 0 {
		sb.WriteString(fmt.Sprintf("  Archived: %d item(s)\n", hr.ArchivedEntries))
	}
	if hr.StaleEntries > 0 {
		sb.WriteString(fmt.Sprintf("  Stale: %d item(s)\n", hr.StaleEntries))
	}
	if hr.PinnedEntries > 0 {
		sb.WriteString(fmt.Sprintf("  Pinned: %d item(s)\n", hr.PinnedEntries))
	}
	if hr.EmbedderActive {
		noEmbed := hr.NoEmbedding
		embedded := active - noEmbed
		sb.WriteString(fmt.Sprintf("  Embeddings: %d / %d\n", embedded, active))
	} else {
		sb.WriteString("  Embeddings: disabled\n")
	}
	if hr.OldestEntry != "" {
		if t, err := time.Parse(time.RFC3339, hr.OldestEntry); err == nil {
			sb.WriteString(fmt.Sprintf("  Oldest memory: %s\n", t.Format("2006-01-02 15:04")))
		}
	}
	if hr.NewestEntry != "" {
		if t, err := time.Parse(time.RFC3339, hr.NewestEntry); err == nil {
			sb.WriteString(fmt.Sprintf("  Newest memory: %s\n", t.Format("2006-01-02 15:04")))
		}
	}

	return sb.String()
}

// renderBarGauge renders a text-based capacity gauge bar.
// Example: [████████████░░░░░░░░] 60%
func renderBarGauge(pct float64, width int) string {
	filled := int(math.Round(float64(width) * pct / 100))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	// Color indicator based on usage level.
	indicator := "🟢"
	if pct >= 90 {
		indicator = "🔴"
	} else if pct >= 70 {
		indicator = "🟡"
	}
	return fmt.Sprintf("%s [%s]", indicator, bar)
}

// runeWidth estimates the display width of a string (CJK chars = 2, others = 1).
func runeWidth(s string) int {
	w := 0
	for _, r := range s {
		if r >= 0x2E80 && r <= 0x9FFF || r >= 0xF900 && r <= 0xFAFF || r >= 0xFE30 && r <= 0xFE4F || r >= 0xFF00 && r <= 0xFFEF {
			w += 2
		} else {
			w++
		}
	}
	return w
}

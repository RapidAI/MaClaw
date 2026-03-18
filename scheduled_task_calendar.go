package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"
)

// isRecurringTask returns true if the task is not a one-time task.
// A one-time task has StartDate == EndDate (both set to the same date).
func isRecurringTask(t *ScheduledTask) bool {
	if t.StartDate == "" && t.EndDate == "" {
		// No date range → recurring (every day / every week etc.)
		return true
	}
	if t.StartDate != "" && t.EndDate != "" && t.StartDate == t.EndDate {
		// Same start and end date → one-time
		return false
	}
	return true
}

// SyncTaskToSystemCalendar creates an ICS file for a recurring scheduled task
// and opens it with the default calendar application. One-time tasks are skipped.
func SyncTaskToSystemCalendar(task *ScheduledTask) error {
	if !isRecurringTask(task) {
		return nil // 一次性任务不同步到日历
	}

	ics := buildICSEvent(task)
	if ics == "" {
		return fmt.Errorf("failed to build ICS event")
	}

	// Write to temp file and schedule cleanup
	tmpDir := os.TempDir()
	icsPath := filepath.Join(tmpDir, fmt.Sprintf("maclaw_task_%s.ics", task.ID))
	if err := os.WriteFile(icsPath, []byte(ics), 0644); err != nil {
		return fmt.Errorf("write ICS file: %w", err)
	}
	// Clean up temp file after 60s (enough time for the calendar app to import)
	go func() {
		time.Sleep(60 * time.Second)
		_ = os.Remove(icsPath)
	}()

	return openFile(icsPath)
}

// buildICSEvent generates an ICS (iCalendar) event string for a recurring task.
// Uses floating time (no timezone suffix) so the calendar app interprets it
// in the user's local timezone, which matches the scheduled task's intent.
func buildICSEvent(t *ScheduledTask) string {
	now := time.Now()
	uid := fmt.Sprintf("maclaw-task-%s@maclaw.local", t.ID)

	// Determine DTSTART in local time
	var dtStart time.Time
	if t.StartDate != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", t.StartDate, time.Local); err == nil {
			dtStart = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), t.Hour, t.Minute, 0, 0, time.Local)
		}
	}
	if dtStart.IsZero() {
		dtStart = time.Date(now.Year(), now.Month(), now.Day(), t.Hour, t.Minute, 0, 0, time.Local)
		if dtStart.Before(now) {
			dtStart = dtStart.AddDate(0, 0, 1)
		}
	}

	rrule := buildRRule(t)

	// UNTIL in UTC (RFC 5545 requires UTC when DTSTART is floating or has TZID)
	var untilStr string
	if t.EndDate != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", t.EndDate, time.Local); err == nil {
			until := time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 0, time.Local).UTC()
			untilStr = fmt.Sprintf(";UNTIL=%s", until.Format("20060102T150405Z"))
		}
	}

	// Use floating time format (no Z suffix) so the event is interpreted
	// in the user's local timezone by the calendar application.
	dtFmt := "20060102T150405"

	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//MaClaw//ScheduledTask//CN\r\n")
	b.WriteString("BEGIN:VEVENT\r\n")
	b.WriteString(fmt.Sprintf("UID:%s\r\n", uid))
	b.WriteString(fmt.Sprintf("DTSTAMP:%s\r\n", now.UTC().Format("20060102T150405Z")))
	b.WriteString(fmt.Sprintf("DTSTART:%s\r\n", dtStart.Format(dtFmt)))
	b.WriteString(fmt.Sprintf("DTEND:%s\r\n", dtStart.Add(30*time.Minute).Format(dtFmt)))
	writeICSFolded(&b, "SUMMARY", escapeICS(t.Name))
	writeICSFolded(&b, "DESCRIPTION", escapeICS(t.Action))
	if rrule != "" {
		b.WriteString(fmt.Sprintf("RRULE:%s%s\r\n", rrule, untilStr))
	}
	b.WriteString("BEGIN:VALARM\r\n")
	b.WriteString("TRIGGER:PT0M\r\n")
	b.WriteString("ACTION:DISPLAY\r\n")
	writeICSFolded(&b, "DESCRIPTION", escapeICS(t.Name))
	b.WriteString("END:VALARM\r\n")
	b.WriteString("END:VEVENT\r\n")
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

// writeICSFolded writes a property with RFC 5545 line folding (max 75 octets per line).
func writeICSFolded(b *strings.Builder, key, value string) {
	line := key + ":" + value
	const maxLen = 75
	for len(line) > maxLen {
		b.WriteString(line[:maxLen])
		b.WriteString("\r\n ")
		line = line[maxLen:]
	}
	b.WriteString(line)
	b.WriteString("\r\n")
}

// buildRRule generates the RRULE string for a scheduled task.
func buildRRule(t *ScheduledTask) string {
	if t.DayOfMonth > 0 {
		return fmt.Sprintf("FREQ=MONTHLY;BYMONTHDAY=%d", t.DayOfMonth)
	}
	if t.DayOfWeek >= 0 && t.DayOfWeek <= 6 {
		days := []string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}
		return fmt.Sprintf("FREQ=WEEKLY;BYDAY=%s", days[t.DayOfWeek])
	}
	return "FREQ=DAILY"
}

// escapeICS escapes special characters for ICS format.
func escapeICS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// openFile opens a file with the default system application.
func openFile(path string) error {
	switch goruntime.GOOS {
	case "windows":
		// Use rundll32 to avoid issues with spaces in path (cmd /c start has quoting problems).
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

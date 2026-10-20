package scheduler

import "testing"

func TestFormatScheduleReportsTheCadence(t *testing.T) {
	everyDay := -1
	if got := FormatSchedule(ScheduledTask{Hour: 9, Minute: 30, DayOfWeek: everyDay}); got != "09:30" {
		t.Fatalf("clock=%q", got)
	}
	if got := FormatSchedule(ScheduledTask{Hour: 0, Minute: 0, IntervalMinutes: 60, DayOfWeek: everyDay}); got != "每1小时" {
		t.Fatalf("hourly=%q", got)
	}
	if got := FormatSchedule(ScheduledTask{IntervalMinutes: 90, DayOfWeek: everyDay}); got != "每1.5小时" {
		t.Fatalf("ninety=%q", got)
	}
	if got := FormatSchedule(ScheduledTask{Hour: 9, Minute: 0, IntervalMinutes: 60, DayOfWeek: everyDay}); got != "每1小时" {
		t.Fatalf("hourly phase hour=%q", got)
	}
	if got := FormatSchedule(ScheduledTask{Hour: 9, Minute: 0, IntervalMinutes: 1440, DayOfWeek: everyDay}); got != "每1天 09:00" {
		t.Fatalf("daily anchor=%q", got)
	}
	if got := FormatSchedule(ScheduledTask{Hour: 0, Minute: 0, IntervalMinutes: 1440, DayOfWeek: everyDay}); got != "每1天 00:00" {
		t.Fatalf("daily midnight=%q", got)
	}
	if got := FormatSchedule(ScheduledTask{Hour: 0, Minute: 30, IntervalMinutes: 60, DayOfWeek: everyDay}); got != "每1小时（从 00:30 起）" {
		t.Fatalf("half-hour grid=%q", got)
	}
	if got := FormatSchedule(ScheduledTask{Hour: 1, Minute: 0, IntervalMinutes: 90, DayOfWeek: everyDay}); got != "每1.5小时（从 01:00 起）" {
		t.Fatalf("shifted ninety=%q", got)
	}
	if got := FormatSchedule(ScheduledTask{Hour: 9, Minute: 0, DayOfWeek: 1}); got != "每周一 09:00" {
		t.Fatalf("weekday clock=%q", got)
	}
	if got := FormatSchedule(ScheduledTask{Hour: 0, Minute: 0, IntervalMinutes: 60, DayOfWeek: 1}); got != "每1小时 周一" {
		t.Fatalf("weekday hourly=%q", got)
	}
	if got := FormatSchedule(ScheduledTask{Hour: 9, Minute: 0, DayOfMonth: 15, DayOfWeek: everyDay}); got != "每月15号 09:00" {
		t.Fatalf("month clock=%q", got)
	}
	if got := FormatSchedule(ScheduledTask{Hour: 9, Minute: 0, DayOfWeek: everyDay, StartDate: "2026-10-01", EndDate: "2026-10-31"}); got != "09:00 2026-10-01~2026-10-31" {
		t.Fatalf("date window=%q", got)
	}
}

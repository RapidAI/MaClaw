package guiapp

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// maclawAppAutomationSchedule mirrors binding.automation.schedule in the app
// manifest. It deliberately reuses the corelib/scheduler fixed-time / interval
// semantics; cron expressions are not supported.
type maclawAppAutomationSchedule struct {
	Hour            int
	Minute          int
	DayOfWeek       int // -1 = every day, 0 = Sunday .. 6 = Saturday
	DayOfMonth      int // -1 = any, 1-31
	IntervalMinutes int
	StartDate       string // "2006-01-02", empty = no limit
	EndDate         string // "2006-01-02", empty = no limit
	TaskType        string // "" / "reminder" (default) or "process"
}

// maclawAppAutomationBinding is the parsed binding.automation block of an
// automation_app manifest: which skill runs, with which static params, on
// which schedule.
type maclawAppAutomationBinding struct {
	ID        string
	Skill     string
	Params    map[string]any
	OutputDir string
	Schedule  maclawAppAutomationSchedule
}

// parseMaclawAppAutomationBinding extracts binding.automation from an app
// manifest map (the "app" object, not the whole document). It returns
// (nil, nil) when the block is absent — an automation_app without a binding
// stays installable but has no schedule to run. A present-but-malformed block
// is a hard validation error.
func parseMaclawAppAutomationBinding(appMap map[string]any, path string) (*maclawAppAutomationBinding, error) {
	if appMap == nil {
		return nil, nil
	}
	binding := anyMap(appMap["binding"])
	if binding == nil {
		return nil, nil
	}
	raw, present := binding["automation"]
	if !present || raw == nil {
		return nil, nil
	}
	automation := anyMap(raw)
	if automation == nil {
		return nil, fmt.Errorf("%s.binding.automation must be an object", path)
	}
	automationPath := path + ".binding.automation"
	b := &maclawAppAutomationBinding{
		ID:        maclawAppStringValue(automation, "id"),
		Skill:     maclawAppStringValue(automation, "skill"),
		OutputDir: maclawAppStringValue(automation, "outputDir", "output_dir"),
	}
	if b.Skill == "" {
		return nil, fmt.Errorf("%s.skill is required", automationPath)
	}
	if params := anyMap(automation["params"]); params != nil {
		b.Params = cloneMapAny(params)
	} else if automation["params"] != nil {
		return nil, fmt.Errorf("%s.params must be an object", automationPath)
	}
	schedule, err := parseMaclawAppAutomationSchedule(automation["schedule"], automationPath+".schedule")
	if err != nil {
		return nil, err
	}
	b.Schedule = schedule
	return b, nil
}

// parseMaclawAppAutomationBindingForEntry adapts the manifest parser to the
// parsedMaclawAppEntry shape used by package validation.
func parseMaclawAppAutomationBindingForEntry(entry parsedMaclawAppEntry, path string) (*maclawAppAutomationBinding, error) {
	return parseMaclawAppAutomationBinding(entry.App, path)
}

func parseMaclawAppAutomationSchedule(raw any, path string) (maclawAppAutomationSchedule, error) {
	schedule := maclawAppAutomationSchedule{Hour: 9, Minute: 0, DayOfWeek: -1, DayOfMonth: -1}
	if raw == nil {
		return schedule, fmt.Errorf("%s is required", path)
	}
	m := anyMap(raw)
	if m == nil {
		return schedule, fmt.Errorf("%s must be an object", path)
	}
	var err error
	if schedule.Hour, err = maclawAppAutomationIntField(m, schedule.Hour, 0, 23, path, "hour"); err != nil {
		return schedule, err
	}
	if schedule.Minute, err = maclawAppAutomationIntField(m, schedule.Minute, 0, 59, path, "minute"); err != nil {
		return schedule, err
	}
	if schedule.DayOfWeek, err = maclawAppAutomationIntField(m, schedule.DayOfWeek, -1, 6, path, "dayOfWeek", "day_of_week"); err != nil {
		return schedule, err
	}
	if schedule.DayOfMonth, err = maclawAppAutomationIntField(m, schedule.DayOfMonth, -1, 31, path, "dayOfMonth", "day_of_month"); err != nil {
		return schedule, err
	}
	if schedule.IntervalMinutes, err = maclawAppAutomationIntField(m, 0, 0, math.MaxInt32, path, "intervalMinutes", "interval_minutes"); err != nil {
		return schedule, err
	}
	schedule.StartDate, err = maclawAppAutomationDateField(m, path, "startDate", "start_date")
	if err != nil {
		return schedule, err
	}
	schedule.EndDate, err = maclawAppAutomationDateField(m, path, "endDate", "end_date")
	if err != nil {
		return schedule, err
	}
	schedule.TaskType = strings.TrimSpace(maclawAppStringValue(m, "taskType", "task_type"))
	switch schedule.TaskType {
	case "", scheduler.TaskTypeReminder, scheduler.TaskTypeProcess:
	default:
		return schedule, fmt.Errorf("%s.taskType must be %q or %q", path, scheduler.TaskTypeReminder, scheduler.TaskTypeProcess)
	}
	return schedule, nil
}

// maclawAppAutomationIntField reads an integer schedule field (first present
// key wins), applying the default when absent and rejecting out-of-range or
// fractional values.
func maclawAppAutomationIntField(m map[string]any, def, min, max int, path string, keys ...string) (int, error) {
	for _, key := range keys {
		raw, present := m[key]
		if !present || raw == nil {
			continue
		}
		n, ok := maclawAppNumberFromAny(raw)
		if !ok || math.Trunc(n) != n {
			return def, fmt.Errorf("%s.%s must be an integer", path, key)
		}
		v := int(n)
		if v < min || v > max {
			return def, fmt.Errorf("%s.%s must be between %d and %d", path, key, min, max)
		}
		return v, nil
	}
	return def, nil
}

// maclawAppAutomationDateField reads a YYYY-MM-DD schedule boundary,
// accepting both camelCase and snake_case keys.
func maclawAppAutomationDateField(m map[string]any, path string, keys ...string) (string, error) {
	value := maclawAppStringValue(m, keys...)
	if value == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return "", fmt.Errorf("%s.%s must be a YYYY-MM-DD date", path, keys[0])
	}
	return value, nil
}

// scheduledTaskSpec maps the binding onto the corelib/scheduler task model.
// The returned task carries no ID — scheduler.Manager.Add assigns one; the
// app linkage is tracked through the Action marker (see the runtime file).
func (b *maclawAppAutomationBinding) scheduledTaskSpec(appID, appName string) scheduler.ScheduledTask {
	name := strings.TrimSpace(appName)
	if name == "" {
		name = firstNonEmptyMaclawAppString(b.ID, b.Skill, "automation")
	}
	return scheduler.ScheduledTask{
		Name:            fmt.Sprintf("[appauto] %s", name),
		Action:          maclawAppAutomationActionMarker(appID),
		Hour:            b.Schedule.Hour,
		Minute:          b.Schedule.Minute,
		DayOfWeek:       b.Schedule.DayOfWeek,
		DayOfMonth:      b.Schedule.DayOfMonth,
		IntervalMinutes: b.Schedule.IntervalMinutes,
		StartDate:       b.Schedule.StartDate,
		EndDate:         b.Schedule.EndDate,
		TaskType:        b.Schedule.TaskType,
	}
}

// scheduleSummary renders a short human-readable description of the schedule
// for status display.
func (b *maclawAppAutomationBinding) scheduleSummary() string {
	s := b.Schedule
	var base string
	if s.IntervalMinutes > 0 {
		base = fmt.Sprintf("every %s", scheduler.FormatInterval(s.IntervalMinutes))
	} else {
		base = fmt.Sprintf("daily at %02d:%02d", s.Hour, s.Minute)
	}
	switch {
	case s.DayOfMonth > 0:
		base += fmt.Sprintf(" on day %d", s.DayOfMonth)
	case s.DayOfWeek >= 0:
		base += fmt.Sprintf(" on weekday %d", s.DayOfWeek)
	}
	if s.StartDate != "" {
		base += " from " + s.StartDate
	}
	if s.EndDate != "" {
		base += " until " + s.EndDate
	}
	return base
}

// updateArgs returns the scheduler.Manager.Update argument map that aligns a
// persisted task with this binding's schedule and display name.
func (b *maclawAppAutomationBinding) updateArgs(appID, appName string) map[string]interface{} {
	spec := b.scheduledTaskSpec(appID, appName)
	return map[string]interface{}{
		"name":             spec.Name,
		"action":           spec.Action,
		"hour":             spec.Hour,
		"minute":           spec.Minute,
		"day_of_week":      spec.DayOfWeek,
		"day_of_month":     spec.DayOfMonth,
		"interval_minutes": spec.IntervalMinutes,
		"start_date":       spec.StartDate,
		"end_date":         spec.EndDate,
		"task_type":        spec.TaskType,
	}
}

// matchesTask reports whether an existing scheduler task already reflects
// this binding (schedule + display fields). Skill/params changes do not
// require a task update because the executor re-reads the install record on
// every fire.
func (b *maclawAppAutomationBinding) matchesTask(task *scheduler.ScheduledTask, appID, appName string) bool {
	if task == nil {
		return false
	}
	spec := b.scheduledTaskSpec(appID, appName)
	return task.Name == spec.Name &&
		task.Action == spec.Action &&
		task.Hour == spec.Hour &&
		task.Minute == spec.Minute &&
		task.DayOfWeek == spec.DayOfWeek &&
		task.DayOfMonth == spec.DayOfMonth &&
		task.IntervalMinutes == spec.IntervalMinutes &&
		task.StartDate == spec.StartDate &&
		task.EndDate == spec.EndDate &&
		task.TaskType == spec.TaskType
}

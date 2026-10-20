package scheduler

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AdmitFields is the field-presence record both schedule.administer.local
// hosts parse into. The operation and the row Add stores are decided here,
// so a record attribute cannot grow a second grammar on one host.
type AdmitFields struct {
	Name            string
	TaskAction      string
	ID              string
	Status          string
	StartDate       string
	EndDate         string
	Hour            int
	Minute          int
	DayOfWeek       int
	DayOfMonth      int
	IntervalMinutes int
	HasHour         bool
	HasMinute       bool
	HasDayOfWeek    bool
	HasDayOfMonth   bool
	HasInterval     bool
}

// HasSchedule is the record's when. A clock hour is a calendar run. A
// positive interval repeats every N minutes from an anchor of Hour:Minute.
// An omitted hour is stored as 0, so the grid starts at midnight and every
// 60 minutes is each hour. Zero is not a schedule.
func (a AdmitFields) HasSchedule() bool {
	return a.HasHour || (a.HasInterval && a.IntervalMinutes > 0)
}

func (a AdmitFields) hasCreateFields() bool {
	return a.Name != "" && a.TaskAction != "" && a.HasSchedule()
}

// HasUpdateFields reports a present record attribute. Status is one of those
// attributes. It does not, by itself, select create, update, or list.
func (a AdmitFields) HasUpdateFields() bool {
	return a.Name != "" || a.TaskAction != "" || a.HasHour || a.HasMinute || a.HasDayOfWeek || a.HasDayOfMonth || a.HasInterval || a.Status != "" || a.StartDate != "" || a.EndDate != ""
}

// AdmitOperation returns create, update, delete, or list.
// Create is the name, the task action, and a schedule. Status, minute,
// weekday, day of month, and the date window ride along on that create;
// they are the record, not another operation. id alone deletes. id plus any
// attribute updates. No attributes lists.
func AdmitOperation(a AdmitFields) (string, bool) {
	if a.ID == "" {
		if a.hasCreateFields() {
			return "create", true
		}
		if !a.HasUpdateFields() {
			return "list", true
		}
		return "", false
	}
	if a.HasUpdateFields() {
		return "update", true
	}
	return "delete", true
}

// AdmitScreen applies the numeric range and then selects the operation.
// An illegal number is returned in field with ok false, and the operation is
// left unset, so a caller reports the number instead of a missing schedule.
func AdmitScreen(a AdmitFields) (op, field string, ok bool) {
	if field = AdmitRange(a); field != "" {
		return "", field, false
	}
	op, ok = AdmitOperation(a)
	return op, "", ok
}

// AdmitRange returns the first present field that cannot be stored.
// The name is the suffix both hosts use in their rejection. An absent field
// is not a range error. Zero interval is in range; without a clock hour it
// is simply not a schedule. Dates are YYYY-MM-DD because the window compare
// is lexical. A start after the end is date_range.
func AdmitRange(a AdmitFields) string {
	start := strings.TrimSpace(a.StartDate)
	end := strings.TrimSpace(a.EndDate)
	switch {
	case a.HasHour && (a.Hour < 0 || a.Hour > 23):
		return "hour"
	case a.HasMinute && (a.Minute < 0 || a.Minute > 59):
		return "minute"
	case a.HasDayOfWeek && (a.DayOfWeek < -1 || a.DayOfWeek > 6):
		return "day_of_week"
	case a.HasDayOfMonth && a.DayOfMonth != -1 && (a.DayOfMonth < 1 || a.DayOfMonth > 31):
		return "day_of_month"
	case a.HasInterval && a.IntervalMinutes < 0:
		return "interval"
	case start != "" && !scheduleDateOK(start):
		return "start_date"
	case end != "" && !scheduleDateOK(end):
		return "end_date"
	case start != "" && end != "" && start > end:
		return "date_range"
	default:
		return ""
	}
}

// scheduleDateOK reports that value is empty or a canonical YYYY-MM-DD.
// The window compare is a string compare, so 2026-1-5 and 2026/10/20 are
// not dates it can read. The end date itself is still a run day.
func scheduleDateOK(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return false
	}
	return parsed.Format("2006-01-02") == value
}

// AdmitPatch is the field map UpdateState stores for an existing row.
// Status is not in the map: it is the separate state argument, so a legacy
// tool map that happens to carry a status key cannot change the record's
// state through Update.
func AdmitPatch(a AdmitFields) map[string]interface{} {
	patch := map[string]interface{}{}
	if a.Name != "" {
		patch["name"] = a.Name
	}
	if a.TaskAction != "" {
		patch["action"] = a.TaskAction
	}
	if a.HasHour {
		patch["hour"] = a.Hour
	}
	if a.HasMinute {
		patch["minute"] = a.Minute
	}
	if a.HasDayOfWeek {
		patch["day_of_week"] = a.DayOfWeek
	}
	if a.HasDayOfMonth {
		patch["day_of_month"] = a.DayOfMonth
	}
	if a.HasInterval {
		patch["interval_minutes"] = a.IntervalMinutes
	}
	if a.StartDate != "" {
		patch["start_date"] = a.StartDate
	}
	if a.EndDate != "" {
		patch["end_date"] = a.EndDate
	}
	return patch
}

// ParseAdmit reads one schedule.administer.local argument map into the shared
// record. It checks types and presence only. Numeric range and which
// operation the record is belong to the caller, so a bad number is not
// reported as a missing schedule. An unknown key or a value of the wrong
// type is rejected.
func ParseAdmit(args map[string]interface{}) (AdmitFields, error) {
	var parsed AdmitFields
	for key, raw := range args {
		switch key {
		case "name", "task_action", "id", "status", "start_date", "end_date":
			value, ok := raw.(string)
			if !ok {
				return AdmitFields{}, fmt.Errorf("scheduler: schedule arguments rejected")
			}
			value = strings.TrimSpace(value)
			switch key {
			case "name":
				parsed.Name = value
			case "task_action":
				parsed.TaskAction = value
			case "id":
				parsed.ID = value
			case "status":
				parsed.Status = value
			case "start_date":
				parsed.StartDate = value
			case "end_date":
				parsed.EndDate = value
			}
		case "hour", "minute", "day_of_week", "day_of_month", "interval_minutes":
			n, ok := parseAdmitInt(raw)
			if !ok {
				return AdmitFields{}, fmt.Errorf("scheduler: schedule arguments rejected")
			}
			switch key {
			case "hour":
				parsed.Hour, parsed.HasHour = n, true
			case "minute":
				parsed.Minute, parsed.HasMinute = n, true
			case "day_of_week":
				parsed.DayOfWeek, parsed.HasDayOfWeek = n, true
			case "day_of_month":
				parsed.DayOfMonth, parsed.HasDayOfMonth = n, true
			case "interval_minutes":
				parsed.IntervalMinutes, parsed.HasInterval = n, true
			}
		default:
			return AdmitFields{}, fmt.Errorf("scheduler: schedule arguments rejected")
		}
	}
	return parsed, nil
}

func parseAdmitInt(raw interface{}) (int, bool) {
	switch n := raw.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

// AdmitTask is the row a create hands to Add. An omitted hour is 0. Omitted
// weekday and day of month are -1, every day and any day. Status is copied
// for the caller to replace with the canonical value before Add. Add then
// writes active or paused in that same save.
func AdmitTask(a AdmitFields) ScheduledTask {
	hour := a.Hour
	if !a.HasHour {
		hour = 0
	}
	task := ScheduledTask{
		Name:            a.Name,
		Action:          a.TaskAction,
		Hour:            hour,
		Minute:          0,
		DayOfWeek:       -1,
		DayOfMonth:      -1,
		IntervalMinutes: 0,
		StartDate:       a.StartDate,
		EndDate:         a.EndDate,
		Status:          a.Status,
	}
	if a.HasMinute {
		task.Minute = a.Minute
	}
	if a.HasDayOfWeek {
		task.DayOfWeek = a.DayOfWeek
	}
	if a.HasDayOfMonth {
		task.DayOfMonth = a.DayOfMonth
	}
	if a.HasInterval {
		task.IntervalMinutes = a.IntervalMinutes
	}
	return task
}

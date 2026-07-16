package main

import "strconv"

// computerUseTrayLabels builds localized Computer Use tray submenu strings and
// enable flags. Shared by Windows systray and macOS Tahoe tray.
func computerUseTrayLabels(app *App) (
	menuTitle, status, pause, resume, stop, reset string,
	pauseOn, resumeOn, stopOn, resetOn bool,
) {
	lang := "en"
	if app != nil && app.CurrentLanguage != "" {
		lang = app.CurrentLanguage
	}
	tr := trayTranslations()
	t, ok := tr[lang]
	if !ok {
		t = tr["en"]
	}
	menuTitle = t["cu_menu"]
	pause = t["cu_pause"]
	resume = t["cu_resume"]
	stop = t["cu_stop"]
	reset = t["cu_reset"]
	status = t["cu_idle"]

	if app == nil {
		return
	}
	st := app.GetComputerUseStatus()
	paused, _ := st["paused"].(bool)
	stopped, _ := st["stopped"].(bool)
	active, _ := st["session_active"].(bool)
	steps := 0
	switch v := st["step_count"].(type) {
	case int:
		steps = v
	case float64:
		steps = int(v)
	case int64:
		steps = int(v)
	}
	// ControlState: stopped implies not "soft paused" for enable matrix.
	switch {
	case stopped:
		status = t["cu_stopped"]
		resetOn = true
	case paused:
		status = t["cu_paused"]
		resumeOn = true
		stopOn = true
		resetOn = true
	case active || steps > 0:
		status = t["cu_active"]
		pauseOn = true
		stopOn = true
	}
	if steps > 0 {
		status = status + " · " + strconv.Itoa(steps)
	}
	return
}

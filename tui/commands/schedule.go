package commands

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// RunSchedule 执行 schedule 子命令。
func RunSchedule(args []string, dataDir string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui schedule <list|create|delete|pause|resume|trigger>")
	}
	mgr, err := openScheduleManager(dataDir)
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		return scheduleList(mgr, args[1:])
	case "create":
		return scheduleCreate(mgr, args[1:])
	case "delete":
		return scheduleDelete(mgr, args[1:])
	case "pause":
		return schedulePause(mgr, args[1:])
	case "resume":
		return scheduleResume(mgr, args[1:])
	case "trigger":
		return scheduleTrigger(mgr, args[1:])
	default:
		return NewUsageError("unknown schedule action: %s", args[0])
	}
}

func openScheduleManager(dataDir string) (*scheduler.Manager, error) {
	path := filepath.Join(dataDir, "scheduled_tasks.json")
	return scheduler.NewManager(path)
}

func scheduleList(mgr *scheduler.Manager, args []string) error {
	fs := flag.NewFlagSet("schedule list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	tasks := mgr.List()
	if *jsonOut {
		return PrintJSON(tasks)
	}
	if len(tasks) == 0 {
		fmt.Println("No scheduled tasks.")
		return nil
	}
	fmt.Printf("%-20s %-20s %-8s %-8s %-10s %-6s %s\n", "ID", "NAME", "TYPE", "STATUS", "SCHEDULE", "RUNS", "ACTION")
	fmt.Println(strings.Repeat("-", 105))
	for _, t := range tasks {
		action := TruncateDisplay(t.Action, 30)
		var schedStr string
		if t.IntervalMinutes > 0 {
			schedStr = "每" + scheduler.FormatInterval(t.IntervalMinutes)
		} else {
			schedStr = fmt.Sprintf("%02d:%02d", t.Hour, t.Minute)
		}
		taskType := t.TaskType
		if taskType == "" {
			taskType = "reminder"
		}
		fmt.Printf("%-20s %-20s %-8s %-8s %-10s %-6d %s\n",
			TruncateDisplay(t.ID, 20), TruncateDisplay(t.Name, 20), taskType, t.Status, schedStr, t.RunCount, action)
	}
	return nil
}

func scheduleCreate(mgr *scheduler.Manager, args []string) error {
	fs := flag.NewFlagSet("schedule create", flag.ExitOnError)
	name := fs.String("name", "", "任务名称（必填）")
	action := fs.String("action", "", "任务动作（必填，自然语言描述）")
	hour := fs.Int("hour", 0, "执行小时 0-23")
	minute := fs.Int("minute", 0, "执行分钟 0-59")
	dow := fs.Int("day-of-week", -1, "星期几 0=Sun..6=Sat, -1=每天")
	dom := fs.Int("day-of-month", -1, "每月几号 1-31, -1=不限")
	intervalMin := fs.Int("interval", 0, "重复间隔（分钟），>0 时启用间隔模式")
	startDate := fs.String("start-date", "", "开始日期 YYYY-MM-DD")
	endDate := fs.String("end-date", "", "结束日期 YYYY-MM-DD")
	taskType := fs.String("type", "", "任务类型: reminder(提醒,默认) 或 process(处理)")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if *name == "" || *action == "" {
		return NewUsageError("usage: schedule create --name <name> --action <text> [--hour H] [--minute M] [--interval N] [--type reminder|process]")
	}

	task := scheduler.ScheduledTask{
		Name:            *name,
		Action:          *action,
		Hour:            *hour,
		Minute:          *minute,
		DayOfWeek:       *dow,
		DayOfMonth:      *dom,
		IntervalMinutes: *intervalMin,
		StartDate:       *startDate,
		EndDate:         *endDate,
		TaskType:        *taskType,
	}
	id, err := mgr.Add(task)
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"id": id, "status": "created"})
	}
	fmt.Printf("Scheduled task created: %s\n", id)
	return nil
}

func scheduleDelete(mgr *scheduler.Manager, args []string) error {
	fs := flag.NewFlagSet("schedule delete", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: schedule delete <id>")
	}
	id := fs.Arg(0)
	if err := mgr.Delete(id); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"id": id, "status": "deleted"})
	}
	fmt.Printf("Scheduled task %s deleted.\n", id)
	return nil
}

func schedulePause(mgr *scheduler.Manager, args []string) error {
	fs := flag.NewFlagSet("schedule pause", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: schedule pause <id>")
	}
	id := fs.Arg(0)
	if err := mgr.Pause(id); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"id": id, "status": "paused"})
	}
	fmt.Printf("Scheduled task %s paused.\n", id)
	return nil
}

func scheduleResume(mgr *scheduler.Manager, args []string) error {
	fs := flag.NewFlagSet("schedule resume", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: schedule resume <id>")
	}
	id := fs.Arg(0)
	if err := mgr.Resume(id); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"id": id, "status": "resumed"})
	}
	fmt.Printf("Scheduled task %s resumed.\n", id)
	return nil
}

func scheduleTrigger(mgr *scheduler.Manager, args []string) error {
	fs := flag.NewFlagSet("schedule trigger", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: schedule trigger <id>")
	}
	id := fs.Arg(0)
	if err := mgr.TriggerNow(id); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"id": id, "status": "triggered"})
	}
	fmt.Printf("Scheduled task %s triggered.\n", id)
	return nil
}



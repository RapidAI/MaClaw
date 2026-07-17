package commands

import (
	"flag"
	"path/filepath"
	"strings"
	"time"
	"github.com/RapidAI/CodeClaw/corelib/security")

// RunAudit 执行 audit 子命令。
func RunAudit(args []string, dataDir string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui audit <list>")
	}
	switch args[0] {
	case "list":
		return auditList(dataDir, args[1:])
	default:
		return NewUsageError("unknown audit action: %s", args[0])
	}
}

func auditList(dataDir string, args []string) error {
	fs := flag.NewFlagSet("audit list", flag.ExitOnError)
	action := fs.String("action", "", "按动作过滤")
	tool := fs.String("tool", "", "按工具名过滤")
	riskLevel := fs.String("risk-level", "", "按风险等级过滤（逗号分隔: low,medium,high,critical）")
	start := fs.String("start", "", "开始时间 YYYY-MM-DD")
	end := fs.String("end", "", "结束时间 YYYY-MM-DD")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	auditDir := filepath.Join(dataDir, "audit_logs")
	log, err := security.NewAuditLog(auditDir)
	if err != nil {
		return err
	}
	defer log.Close()

	filter := security.AuditFilter{
		Action:   security.AuditAction(*action),
		ToolName: *tool,
	}
	if *riskLevel != "" {
		for _, rl := range strings.Split(*riskLevel, ",") {
			filter.RiskLevels = append(filter.RiskLevels, security.RiskLevel(strings.TrimSpace(rl)))
		}
	}
	if *start != "" {
		t, err := time.Parse("2006-01-02", *start)
		if err != nil {
			return NewUsageError("invalid --start date: %v", err)
		}
		filter.StartTime = &t
	}
	if *end != "" {
		t, err := time.Parse("2006-01-02", *end)
		if err != nil {
			return NewUsageError("invalid --end date: %v", err)
		}
		endOfDay := t.Add(24*time.Hour - time.Second)
		filter.EndTime = &endOfDay
	}

	entries, err := log.Query(filter)
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(entries)
	}
	if len(entries) == 0 {
		Println("No audit entries found.")
		return nil
	}
	Printf("%-20s %-20s %-10s %-10s %s\n", "TIME", "TOOL", "RISK", "POLICY", "RESULT")
	Println(strings.Repeat("-", 80))
	for _, e := range entries {
		ts := e.Timestamp.Format("2006-01-02 15:04")
		result := TruncateDisplay(e.Result, 20)
		Printf("%-20s %-20s %-10s %-10s %s\n",
			ts, TruncateDisplay(e.ToolName, 20), string(e.RiskLevel), string(e.PolicyAction), result)
	}
	return nil
}

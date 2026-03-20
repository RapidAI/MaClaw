package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/misc"
)

// RunSwarm 处理 swarm 子命令。
func RunSwarm(args []string) error {
	if len(args) == 0 {
		return &UsageError{Msg: swarmUsage()}
	}
	switch args[0] {
	case "create":
		return swarmCreate(args[1:])
	case "status":
		return swarmStatus(args[1:])
	case "cancel":
		return swarmCancel(args[1:])
	case "resume":
		return swarmResume(args[1:])
	case "list":
		return swarmList()
	default:
		return &UsageError{Msg: swarmUsage()}
	}
}

func swarmUsage() string {
	return `用法: maclaw-tui swarm <command>

Commands:
  create   创建执行计划（从 JSON 文件或内联）
  status   查看计划状态
  cancel   取消计划
  resume   恢复失败的计划
  list     列出可恢复的计划`
}

func getOrchestrator() *misc.TaskOrchestrator {
	dataDir := ResolveDataDir()
	persistPath := filepath.Join(dataDir, "swarm_plans.json")
	return misc.NewTaskOrchestratorWithPersist(nil, persistPath)
}

func swarmCreate(args []string) error {
	if len(args) == 0 {
		return &UsageError{Msg: "用法: maclaw-tui swarm create <plan.json> 或 --desc <描述> --tasks <task1,task2,...>"}
	}

	var description string
	var subTasks []misc.PlanSubTask

	// 从 JSON 文件加载
	if !strings.HasPrefix(args[0], "--") {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("读取计划文件失败: %w", err)
		}
		var plan struct {
			Description string             `json:"description"`
			SubTasks    []misc.PlanSubTask `json:"sub_tasks"`
		}
		if err := json.Unmarshal(data, &plan); err != nil {
			return fmt.Errorf("解析计划文件失败: %w", err)
		}
		description = plan.Description
		subTasks = plan.SubTasks
	} else {
		// 内联模式
		for i := 0; i < len(args); i++ {
			switch args[i] {
			case "--desc":
				if i+1 < len(args) {
					description = args[i+1]
					i++
				}
			case "--tasks":
				if i+1 < len(args) {
					for _, t := range strings.Split(args[i+1], ",") {
						t = strings.TrimSpace(t)
						if t != "" {
							subTasks = append(subTasks, misc.PlanSubTask{Description: t})
						}
					}
					i++
				}
			}
		}
	}

	if description == "" {
		description = "TUI Swarm Plan"
	}
	if len(subTasks) == 0 {
		return fmt.Errorf("至少需要一个子任务")
	}

	orch := getOrchestrator()
	plan, err := orch.CreatePlan(description, subTasks)
	if err != nil {
		return err
	}

	fmt.Printf("计划已创建: %s\n", plan.ID)
	fmt.Printf("描述: %s\n", plan.Description)
	fmt.Printf("子任务数: %d\n", len(plan.SubTasks))

	// 立即执行
	fmt.Println("开始执行...")
	if err := orch.Execute(plan.ID); err != nil {
		return fmt.Errorf("执行失败: %w", err)
	}
	fmt.Println("执行完成")
	return nil
}

func swarmStatus(args []string) error {
	if len(args) == 0 {
		return &UsageError{Msg: "用法: maclaw-tui swarm status <plan_id>"}
	}
	orch := getOrchestrator()
	plan, err := orch.GetStatus(args[0])
	if err != nil {
		return err
	}
	printPlan(plan)
	return nil
}

func swarmCancel(args []string) error {
	if len(args) == 0 {
		return &UsageError{Msg: "用法: maclaw-tui swarm cancel <plan_id>"}
	}
	orch := getOrchestrator()
	if err := orch.Cancel(args[0]); err != nil {
		return err
	}
	fmt.Printf("计划 %s 已取消\n", args[0])
	return nil
}

func swarmResume(args []string) error {
	if len(args) == 0 {
		return &UsageError{Msg: "用法: maclaw-tui swarm resume <plan_id>"}
	}
	orch := getOrchestrator()
	fmt.Printf("恢复计划 %s...\n", args[0])
	if err := orch.Resume(args[0]); err != nil {
		return fmt.Errorf("恢复失败: %w", err)
	}
	fmt.Println("执行完成")
	return nil
}

func swarmList() error {
	orch := getOrchestrator()
	plans := orch.ListResumable()
	if len(plans) == 0 {
		fmt.Println("无可恢复的计划")
		return nil
	}
	for _, p := range plans {
		printPlan(p)
		fmt.Println()
	}
	return nil
}

func printPlan(plan *misc.TaskPlan) {
	fmt.Printf("计划: %s  状态: %s\n", plan.ID, plan.Status)
	fmt.Printf("描述: %s\n", plan.Description)
	fmt.Printf("创建时间: %s\n", plan.CreatedAt.Format("2006-01-02 15:04:05"))
	for _, st := range plan.SubTasks {
		icon := "⏳"
		switch st.Status {
		case "completed":
			icon = "✅"
		case "failed":
			icon = "❌"
		case "running":
			icon = "🔄"
		}
		fmt.Printf("  %s [%s] %s: %s\n", icon, st.ID, st.Description, st.Status)
		if st.Result != "" {
			fmt.Printf("      结果: %s\n", st.Result)
		}
	}
}

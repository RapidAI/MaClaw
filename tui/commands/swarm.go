package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/swarm"
)

// swarmOrchestrator is the package-level orchestrator instance, lazily created.
var swarmOrchestrator *swarm.SwarmOrchestrator

// SwarmOrchestratorProvider allows the TUI app to inject a pre-configured
// orchestrator. If nil, a minimal one is created on demand.
var SwarmOrchestratorProvider func() *swarm.SwarmOrchestrator

func getSwarmOrchestrator() *swarm.SwarmOrchestrator {
	if swarmOrchestrator != nil {
		return swarmOrchestrator
	}
	if SwarmOrchestratorProvider != nil {
		swarmOrchestrator = SwarmOrchestratorProvider()
		return swarmOrchestrator
	}
	// Fallback: create a minimal orchestrator (no session manager, limited use).
	swarmOrchestrator = swarm.NewSwarmOrchestrator(nil, &swarm.NoopNotifier{})
	return swarmOrchestrator
}

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
	case "list":
		return swarmList()
	default:
		return &UsageError{Msg: swarmUsage()}
	}
}

func swarmUsage() string {
	return `用法: maclaw-tui swarm <command>

Commands:
  create   创建并执行 Swarm Run
           --mode greenfield --requirements <file> --project <path>
           --mode maintenance --tasks <file> --project <path>
  status   查看 Swarm Run 状态
  cancel   取消 Swarm Run
  list     列出所有 Swarm Run`
}

func swarmCreate(args []string) error {
	var mode, reqFile, tasksFile, projectPath, techStack, tool string
	var maxAgents, maxRounds int

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--mode":
			if i+1 < len(args) {
				mode = args[i+1]
				i++
			}
		case "--requirements":
			if i+1 < len(args) {
				reqFile = args[i+1]
				i++
			}
		case "--tasks":
			if i+1 < len(args) {
				tasksFile = args[i+1]
				i++
			}
		case "--project":
			if i+1 < len(args) {
				projectPath = args[i+1]
				i++
			}
		case "--tech-stack":
			if i+1 < len(args) {
				techStack = args[i+1]
				i++
			}
		case "--tool":
			if i+1 < len(args) {
				tool = args[i+1]
				i++
			}
		case "--max-agents":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &maxAgents)
				i++
			}
		case "--max-rounds":
			if i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &maxRounds)
				i++
			}
		}
	}

	if projectPath == "" {
		cwd, _ := os.Getwd()
		projectPath = cwd
	}

	var req swarm.SwarmRunRequest
	req.ProjectPath = projectPath
	req.TechStack = techStack
	req.Tool = tool
	req.MaxAgents = maxAgents
	req.MaxRounds = maxRounds

	switch strings.ToLower(mode) {
	case "greenfield":
		if reqFile == "" {
			return fmt.Errorf("greenfield 模式需要 --requirements <file>")
		}
		data, err := os.ReadFile(reqFile)
		if err != nil {
			return fmt.Errorf("读取需求文件失败: %w", err)
		}
		req.Mode = swarm.SwarmModeGreenfield
		req.Requirements = string(data)
	case "maintenance":
		if tasksFile == "" {
			return fmt.Errorf("maintenance 模式需要 --tasks <file>")
		}
		data, err := os.ReadFile(tasksFile)
		if err != nil {
			return fmt.Errorf("读取任务文件失败: %w", err)
		}
		var taskInput swarm.TaskListInput
		if err := json.Unmarshal(data, &taskInput); err != nil {
			return fmt.Errorf("解析任务文件失败: %w", err)
		}
		req.Mode = swarm.SwarmModeMaintenance
		req.TaskInput = &taskInput
	default:
		return fmt.Errorf("请指定 --mode greenfield 或 --mode maintenance")
	}

	orch := getSwarmOrchestrator()
	run, err := orch.StartSwarmRun(req)
	if err != nil {
		return fmt.Errorf("启动 Swarm Run 失败: %w", err)
	}

	Printf("Swarm Run 已启动: %s\n", run.ID)
	Printf("模式: %s  项目: %s\n", run.Mode, run.ProjectPath)

	// 轮询等待完成
	for {
		time.Sleep(2 * time.Second)
		r, err := orch.GetSwarmRun(run.ID)
		if err != nil {
			return err
		}
		if r.Status == swarm.SwarmStatusCompleted || r.Status == swarm.SwarmStatusFailed || r.Status == swarm.SwarmStatusCancelled {
			Printf("Swarm Run %s 完成，状态: %s\n", r.ID, r.Status)
			break
		}
	}
	return nil
}

func swarmStatus(args []string) error {
	if len(args) == 0 {
		return &UsageError{Msg: "用法: maclaw-tui swarm status <run_id>"}
	}
	orch := getSwarmOrchestrator()
	run, err := orch.GetSwarmRun(args[0])
	if err != nil {
		return err
	}
	printSwarmRun(run)
	return nil
}

func swarmCancel(args []string) error {
	if len(args) == 0 {
		return &UsageError{Msg: "用法: maclaw-tui swarm cancel <run_id>"}
	}
	orch := getSwarmOrchestrator()
	if err := orch.CancelSwarmRun(args[0]); err != nil {
		return err
	}
	Printf("Swarm Run %s 已取消\n", args[0])
	return nil
}

func swarmList() error {
	orch := getSwarmOrchestrator()
	runs := orch.ListSwarmRuns()
	if len(runs) == 0 {
		Println("无 Swarm Run 记录")
		return nil
	}
	for _, r := range runs {
		Printf("  %s  mode=%s  status=%s  phase=%s  tasks=%d  round=%d  created=%s\n",
			r.ID, r.Mode, r.Status, r.Phase, r.TaskCount, r.Round,
			r.CreatedAt.Format("2006-01-02 15:04:05"))
	}
	return nil
}

func printSwarmRun(run *swarm.SwarmRun) {
	Printf("Run: %s  Mode: %s  Status: %s  Phase: %s\n", run.ID, run.Mode, run.Status, run.Phase)
	Printf("Project: %s  TechStack: %s\n", run.ProjectPath, run.TechStack)
	Printf("Round: %d/%d  Created: %s\n", run.CurrentRound, run.MaxRounds, run.CreatedAt.Format("2006-01-02 15:04:05"))
	if len(run.Tasks) > 0 {
		Printf("Tasks (%d):\n", len(run.Tasks))
		for _, t := range run.Tasks {
			Printf("  [%d] %s (group=%d, deps=%v)\n", t.Index, t.Description, t.GroupID, t.Dependencies)
		}
	}
	if len(run.Agents) > 0 {
		Printf("Agents (%d):\n", len(run.Agents))
		for _, a := range run.Agents {
			Printf("  %s  role=%s  task=%d  status=%s\n", a.ID, a.Role, a.TaskIndex, a.Status)
		}
	}
}

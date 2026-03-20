package commands

import (
	"flag"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// RunLoop 执行 loop 子命令（后台任务管理）。
func RunLoop(args []string, mgr *agent.BackgroundLoopManager) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui loop <list|stop|continue>")
	}
	if mgr == nil {
		return fmt.Errorf("后台任务管理器未初始化（需要在 daemon 或 TUI 模式下运行）")
	}
	switch args[0] {
	case "list":
		return loopList(args[1:], mgr)
	case "stop":
		return loopStop(args[1:], mgr)
	case "continue":
		return loopContinue(args[1:], mgr)
	default:
		return NewUsageError("unknown loop action: %s", args[0])
	}
}

func loopList(args []string, mgr *agent.BackgroundLoopManager) error {
	fs := flag.NewFlagSet("loop list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	views := mgr.ListViews()

	if *jsonOut {
		return PrintJSON(views)
	}

	if len(views) == 0 {
		fmt.Println("无运行中的后台任务。")
		return nil
	}

	fmt.Printf("%-20s %-10s %-10s %-8s %-6s %s\n", "ID", "TYPE", "STATUS", "ITER", "QUEUE", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 80))
	for _, v := range views {
		iter := fmt.Sprintf("%d/%d", v.Iteration, v.MaxIter)
		fmt.Printf("%-20s %-10s %-10s %-8s %-6d %s\n",
			TruncateDisplay(v.ID, 20),
			v.SlotKind,
			v.Status,
			iter,
			v.QueuedCount,
			TruncateDisplay(v.Description, 30))
	}
	return nil
}

func loopStop(args []string, mgr *agent.BackgroundLoopManager) error {
	fs := flag.NewFlagSet("loop stop", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: loop stop <loop-id>")
	}
	loopID := fs.Arg(0)

	ctx := mgr.Get(loopID)
	if ctx == nil {
		return fmt.Errorf("后台任务 '%s' 不存在", loopID)
	}

	mgr.Stop(loopID)
	fmt.Printf("后台任务 '%s' 已停止。\n", loopID)
	return nil
}

func loopContinue(args []string, mgr *agent.BackgroundLoopManager) error {
	fs := flag.NewFlagSet("loop continue", flag.ExitOnError)
	rounds := fs.Int("rounds", 5, "追加迭代轮数")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: loop continue <loop-id> [--rounds N]")
	}
	loopID := fs.Arg(0)

	if err := mgr.SendContinue(loopID, *rounds); err != nil {
		return err
	}
	fmt.Printf("已向后台任务 '%s' 追加 %d 轮迭代。\n", loopID, *rounds)
	return nil
}

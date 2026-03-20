package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/clawnet"
)

// RunClawNet 执行 clawnet 子命令。
func RunClawNet(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui clawnet <status|peers|tasks|credits|knowledge|dm|swarm|prediction|topic|overlay|resume|diagnostics|nutshell|identity|leaderboard|transactions|credits-audit|auto-picker|daemon|binary|profile>")
	}

	// Commands that don't require daemon running
	switch args[0] {
	case "identity":
		return clawnetIdentity(args[1:])
	case "daemon":
		return clawnetDaemon(args[1:])
	case "binary":
		return clawnetBinary(args[1:])
	}

	client := clawnet.NewClient()
	if !client.IsRunning() {
		return fmt.Errorf("ClawNet daemon is not running. Start it first or enable clawnet_enabled in config.")
	}
	switch args[0] {
	case "status":
		return clawnetStatus(client, args[1:])
	case "peers":
		return clawnetPeers(client, args[1:])
	case "tasks":
		return clawnetTasks(client, args[1:])
	case "credits":
		return clawnetCredits(client, args[1:])
	case "knowledge":
		return clawnetKnowledge(client, args[1:])
	case "dm":
		return clawnetDM(client, args[1:])
	case "swarm":
		return clawnetSwarm(client, args[1:])
	case "prediction":
		return clawnetPrediction(client, args[1:])
	case "topic":
		return clawnetTopic(client, args[1:])
	case "overlay":
		return clawnetOverlay(client, args[1:])
	case "resume":
		return clawnetResume(client, args[1:])
	case "diagnostics":
		return clawnetDiagnostics(client, args[1:])
	case "nutshell":
		return clawnetNutshell(client, args[1:])
	case "leaderboard":
		return clawnetLeaderboard(client, args[1:])
	case "transactions":
		return clawnetTransactions(client, args[1:])
	case "credits-audit":
		return clawnetCreditsAudit(client, args[1:])
	case "auto-picker":
		return clawnetAutoPicker(client, args[1:])
	case "profile":
		return clawnetProfile(client, args[1:])
	default:
		return NewUsageError("unknown clawnet action: %s", args[0])
	}
}

func clawnetStatus(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	status, err := client.GetStatus()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(status)
	}
	fmt.Printf("ClawNet Status:\n")
	fmt.Printf("  PeerID:    %s\n", status.PeerID)
	fmt.Printf("  Peers:     %d\n", status.Peers)
	fmt.Printf("  UnreadDM:  %d\n", status.UnreadDM)
	fmt.Printf("  Version:   %s\n", status.Version)
	fmt.Printf("  Uptime:    %s\n", status.Uptime)
	return nil
}

func clawnetPeers(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet peers", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	peers, err := client.GetPeers()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(peers)
	}
	if len(peers) == 0 {
		fmt.Println("No peers connected.")
		return nil
	}
	fmt.Printf("%-20s %-20s %-10s %s\n", "PEER ID", "ADDR", "LATENCY", "COUNTRY")
	fmt.Println(strings.Repeat("-", 65))
	for _, p := range peers {
		fmt.Printf("%-20s %-20s %-10s %s\n",
			TruncateDisplay(p.PeerID, 20), TruncateDisplay(p.Addr, 20), p.Latency, p.Country)
	}
	return nil
}

func clawnetTasks(client *clawnet.Client, args []string) error {
	// Check for subcommand-style actions first
	if len(args) > 0 {
		switch args[0] {
		case "bid":
			return clawnetTaskBid(client, args[1:])
		case "assign":
			return clawnetTaskAssign(client, args[1:])
		case "claim":
			return clawnetTaskClaim(client, args[1:])
		case "submit":
			return clawnetTaskSubmit(client, args[1:])
		case "approve":
			return clawnetTaskApprove(client, args[1:])
		case "reject":
			return clawnetTaskReject(client, args[1:])
		case "cancel":
			return clawnetTaskCancel(client, args[1:])
		case "board":
			return clawnetTaskBoard(client, args[1:])
		case "submissions":
			return clawnetTaskSubmissions(client, args[1:])
		case "pick-winner":
			return clawnetTaskPickWinner(client, args[1:])
		}
	}

	// Default: list tasks
	fs := flag.NewFlagSet("clawnet tasks", flag.ExitOnError)
	status := fs.String("status", "", "按状态过滤 (open/assigned/completed)")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	tasks, err := client.ListTasks(*status)
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(tasks)
	}
	if len(tasks) == 0 {
		fmt.Println("No tasks found.")
		return nil
	}
	fmt.Printf("%-20s %-10s %-8s %s\n", "ID", "STATUS", "REWARD", "TITLE")
	fmt.Println(strings.Repeat("-", 70))
	for _, t := range tasks {
		fmt.Printf("%-20s %-10s %-8.1f %s\n",
			TruncateDisplay(t.ID, 20), t.TaskStatus, t.Reward, TruncateDisplay(t.Title, 30))
	}
	return nil
}

func clawnetTaskBid(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet tasks bid", flag.ExitOnError)
	taskID := fs.String("id", "", "任务 ID（必填）")
	amount := fs.Float64("amount", 0, "出价金额")
	message := fs.String("message", "", "出价消息")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("bid requires -id flag")
	}
	if err := client.BidOnTask(*taskID, *amount, *message); err != nil {
		return err
	}
	fmt.Printf("已对任务 %s 出价。\n", *taskID)
	return nil
}

func clawnetTaskAssign(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet tasks assign", flag.ExitOnError)
	taskID := fs.String("id", "", "任务 ID（必填）")
	peer := fs.String("peer", "", "指派的 Peer ID（必填）")
	fs.Parse(args)
	if *taskID == "" || *peer == "" {
		return fmt.Errorf("assign requires -id and -peer flags")
	}
	if err := client.AssignTask(*taskID, *peer); err != nil {
		return err
	}
	fmt.Printf("已将任务 %s 指派给 %s。\n", *taskID, *peer)
	return nil
}

func clawnetTaskClaim(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet tasks claim", flag.ExitOnError)
	taskID := fs.String("id", "", "任务 ID（必填）")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("claim requires -id flag")
	}
	if err := client.ClaimTask(*taskID); err != nil {
		return err
	}
	fmt.Printf("已认领任务 %s。\n", *taskID)
	return nil
}

func clawnetTaskSubmit(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet tasks submit", flag.ExitOnError)
	taskID := fs.String("id", "", "任务 ID（必填）")
	result := fs.String("result", "", "提交结果（必填）")
	fs.Parse(args)
	if *taskID == "" || *result == "" {
		return fmt.Errorf("submit requires -id and -result flags")
	}
	if err := client.SubmitTaskResult(*taskID, *result); err != nil {
		return err
	}
	fmt.Printf("已提交任务 %s 的结果。\n", *taskID)
	return nil
}

func clawnetTaskApprove(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet tasks approve", flag.ExitOnError)
	taskID := fs.String("id", "", "任务 ID（必填）")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("approve requires -id flag")
	}
	if err := client.ApproveTask(*taskID); err != nil {
		return err
	}
	fmt.Printf("已批准任务 %s。\n", *taskID)
	return nil
}

func clawnetTaskReject(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet tasks reject", flag.ExitOnError)
	taskID := fs.String("id", "", "任务 ID（必填）")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("reject requires -id flag")
	}
	if err := client.RejectTask(*taskID); err != nil {
		return err
	}
	fmt.Printf("已拒绝任务 %s。\n", *taskID)
	return nil
}

func clawnetTaskCancel(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet tasks cancel", flag.ExitOnError)
	taskID := fs.String("id", "", "任务 ID（必填）")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("cancel requires -id flag")
	}
	if err := client.CancelTask(*taskID); err != nil {
		return err
	}
	fmt.Printf("已取消任务 %s。\n", *taskID)
	return nil
}

func clawnetTaskBoard(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet tasks board", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)
	board, err := client.GetTaskBoard()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(board)
	}
	return PrintJSON(board)
}

func clawnetTaskSubmissions(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet tasks submissions", flag.ExitOnError)
	taskID := fs.String("id", "", "任务 ID（必填）")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("submissions requires -id flag")
	}
	subs, err := client.GetTaskSubmissions(*taskID)
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(subs)
	}
	if len(subs) == 0 {
		fmt.Println("No submissions found.")
		return nil
	}
	return PrintJSON(subs)
}

func clawnetTaskPickWinner(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet tasks pick-winner", flag.ExitOnError)
	taskID := fs.String("id", "", "任务 ID（必填）")
	winner := fs.String("winner", "", "获胜者 Peer ID（必填）")
	fs.Parse(args)
	if *taskID == "" || *winner == "" {
		return fmt.Errorf("pick-winner requires -id and -winner flags")
	}
	if err := client.PickTaskWinner(*taskID, *winner); err != nil {
		return err
	}
	fmt.Printf("已选择 %s 为任务 %s 的获胜者。\n", *winner, *taskID)
	return nil
}

func clawnetCredits(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet credits", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	credits, err := client.GetCredits()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(credits)
	}
	fmt.Printf("ClawNet Credits:\n")
	fmt.Printf("  Balance:  %.2f\n", credits.Balance)
	fmt.Printf("  Tier:     %s\n", credits.Tier)
	fmt.Printf("  Energy:   %.2f\n", credits.Energy)
	return nil
}

// ---------- Knowledge ----------

func clawnetKnowledge(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet knowledge", flag.ExitOnError)
	action := fs.String("action", "feed", "操作: feed|search|publish")
	domain := fs.String("domain", "", "按领域过滤")
	query := fs.String("q", "", "搜索关键词")
	title := fs.String("title", "", "发布标题")
	body := fs.String("body", "", "发布内容")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	switch *action {
	case "feed":
		entries, err := client.GetKnowledgeFeed(*domain, 20)
		if err != nil {
			return err
		}
		if *jsonOut {
			return PrintJSON(entries)
		}
		if len(entries) == 0 {
			fmt.Println("No knowledge entries.")
			return nil
		}
		for _, e := range entries {
			fmt.Printf("[%s] %s\n  %s\n\n", TruncateDisplay(e.ID, 12), e.Title, TruncateDisplay(e.Body, 80))
		}
	case "search":
		if *query == "" {
			return fmt.Errorf("search requires -q flag")
		}
		entries, err := client.SearchKnowledge(*query)
		if err != nil {
			return err
		}
		if *jsonOut {
			return PrintJSON(entries)
		}
		if len(entries) == 0 {
			fmt.Println("No results.")
			return nil
		}
		for _, e := range entries {
			fmt.Printf("[%s] %s\n  %s\n\n", TruncateDisplay(e.ID, 12), e.Title, TruncateDisplay(e.Body, 80))
		}
	case "publish":
		if *title == "" || *body == "" {
			return fmt.Errorf("publish requires -title and -body flags")
		}
		entry, err := client.PublishKnowledge(*title, *body)
		if err != nil {
			return err
		}
		fmt.Printf("Published: %s\n", entry.ID)
	default:
		return fmt.Errorf("unknown knowledge action: %s (use feed|search|publish)", *action)
	}
	return nil
}

// ---------- DM ----------

func clawnetDM(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet dm", flag.ExitOnError)
	action := fs.String("action", "inbox", "操作: inbox|thread|send")
	peer := fs.String("peer", "", "对方 Peer ID")
	body := fs.String("body", "", "消息内容")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	switch *action {
	case "inbox":
		msgs, err := client.GetDMInbox()
		if err != nil {
			return err
		}
		if *jsonOut {
			return PrintJSON(msgs)
		}
		if len(msgs) == 0 {
			fmt.Println("No messages.")
			return nil
		}
		for _, m := range msgs {
			fmt.Printf("[%s] %s: %s\n", m.SentAt, TruncateDisplay(m.PeerID, 16), TruncateDisplay(m.Body, 60))
		}
	case "thread":
		if *peer == "" {
			return fmt.Errorf("thread requires -peer flag")
		}
		msgs, err := client.GetDMThread(*peer, 30)
		if err != nil {
			return err
		}
		if *jsonOut {
			return PrintJSON(msgs)
		}
		for _, m := range msgs {
			fmt.Printf("[%s] %s: %s\n", m.SentAt, TruncateDisplay(m.PeerID, 16), m.Body)
		}
	case "send":
		if *peer == "" || *body == "" {
			return fmt.Errorf("send requires -peer and -body flags")
		}
		if err := client.SendDM(*peer, *body); err != nil {
			return err
		}
		fmt.Println("Message sent.")
	default:
		return fmt.Errorf("unknown dm action: %s (use inbox|thread|send)", *action)
	}
	return nil
}

// ---------- Swarm ----------

func clawnetSwarm(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet swarm", flag.ExitOnError)
	action := fs.String("action", "list", "操作: list|create|join|contribute|synthesize")
	sessionID := fs.String("id", "", "会话 ID")
	topic := fs.String("topic", "", "主题")
	question := fs.String("question", "", "问题")
	message := fs.String("message", "", "贡献内容")
	stance := fs.String("stance", "", "立场")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	switch *action {
	case "list":
		sessions, err := client.ListSwarmSessions()
		if err != nil {
			return err
		}
		if *jsonOut {
			return PrintJSON(sessions)
		}
		if len(sessions) == 0 {
			fmt.Println("No swarm sessions.")
			return nil
		}
		fmt.Printf("%-16s %-10s %s\n", "ID", "STATUS", "TOPIC")
		fmt.Println(strings.Repeat("-", 50))
		for _, s := range sessions {
			fmt.Printf("%-16s %-10s %s\n", TruncateDisplay(s.ID, 16), s.Status, TruncateDisplay(s.Topic, 30))
		}
	case "create":
		if *topic == "" {
			return fmt.Errorf("create requires -topic flag")
		}
		s, err := client.CreateSwarmSession(*topic, *question)
		if err != nil {
			return err
		}
		fmt.Printf("Created swarm session: %s\n", s.ID)
	case "join":
		if *sessionID == "" {
			return fmt.Errorf("join requires -id flag")
		}
		return client.JoinSwarm(*sessionID)
	case "contribute":
		if *sessionID == "" || *message == "" {
			return fmt.Errorf("contribute requires -id and -message flags")
		}
		return client.ContributeToSwarm(*sessionID, *message, *stance)
	case "synthesize":
		if *sessionID == "" {
			return fmt.Errorf("synthesize requires -id flag")
		}
		result, err := client.SynthesizeSwarm(*sessionID)
		if err != nil {
			return err
		}
		return PrintJSON(result)
	default:
		return fmt.Errorf("unknown swarm action: %s (use list|create|join|contribute|synthesize)", *action)
	}
	return nil
}

// ---------- Prediction Market ----------

func clawnetPrediction(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet prediction", flag.ExitOnError)
	action := fs.String("action", "list", "操作: list|create|bet|resolve|appeal|leaderboard")
	predID := fs.String("id", "", "预测 ID")
	question := fs.String("question", "", "预测问题")
	options := fs.String("options", "yes,no", "选项（逗号分隔）")
	option := fs.String("option", "", "下注/结算选项")
	amount := fs.Float64("amount", 0, "下注金额")
	reason := fs.String("reason", "", "申诉理由")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	switch *action {
	case "list":
		preds, err := client.ListPredictions()
		if err != nil {
			return err
		}
		if *jsonOut {
			return PrintJSON(preds)
		}
		if len(preds) == 0 {
			fmt.Println("No predictions.")
			return nil
		}
		fmt.Printf("%-16s %-10s %s\n", "ID", "STATUS", "QUESTION")
		fmt.Println(strings.Repeat("-", 60))
		for _, p := range preds {
			fmt.Printf("%-16s %-10s %s\n", TruncateDisplay(p.ID, 16), p.Status, TruncateDisplay(p.Question, 40))
		}
	case "create":
		if *question == "" {
			return fmt.Errorf("create requires -question flag")
		}
		opts := strings.Split(*options, ",")
		for i := range opts {
			opts[i] = strings.TrimSpace(opts[i])
		}
		pred, err := client.CreatePrediction(*question, opts)
		if err != nil {
			return err
		}
		fmt.Printf("Created prediction: %s\n", pred.ID)
	case "bet":
		if *predID == "" || *option == "" || *amount <= 0 {
			return fmt.Errorf("bet requires -id, -option, and -amount flags")
		}
		return client.PlaceBet(*predID, *option, *amount)
	case "resolve":
		if *predID == "" || *option == "" {
			return fmt.Errorf("resolve requires -id and -option flags")
		}
		return client.ResolvePrediction(*predID, *option)
	case "appeal":
		if *predID == "" || *reason == "" {
			return fmt.Errorf("appeal requires -id and -reason flags")
		}
		return client.AppealPrediction(*predID, *reason)
	case "leaderboard":
		lb, err := client.GetPredictionLeaderboard()
		if err != nil {
			return err
		}
		return PrintJSON(lb)
	default:
		return fmt.Errorf("unknown prediction action: %s (use list|create|bet|resolve|appeal|leaderboard)", *action)
	}
	return nil
}

// ---------- Topic Rooms ----------

func clawnetTopic(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet topic", flag.ExitOnError)
	action := fs.String("action", "list", "操作: list|create|messages|post")
	name := fs.String("name", "", "频道名称")
	desc := fs.String("desc", "", "频道描述")
	body := fs.String("body", "", "消息内容")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	switch *action {
	case "list":
		topics, err := client.ListTopics()
		if err != nil {
			return err
		}
		if *jsonOut {
			return PrintJSON(topics)
		}
		if len(topics) == 0 {
			fmt.Println("No topics.")
			return nil
		}
		for _, t := range topics {
			fmt.Printf("#%-20s %s\n", t.Name, t.Description)
		}
	case "create":
		if *name == "" {
			return fmt.Errorf("create requires -name flag")
		}
		return client.CreateTopic(*name, *desc)
	case "messages":
		if *name == "" {
			return fmt.Errorf("messages requires -name flag")
		}
		msgs, err := client.GetTopicMessages(*name)
		if err != nil {
			return err
		}
		if *jsonOut {
			return PrintJSON(msgs)
		}
		for _, m := range msgs {
			fmt.Printf("[%s] %s: %s\n", m.SentAt, TruncateDisplay(m.PeerID, 16), m.Body)
		}
	case "post":
		if *name == "" || *body == "" {
			return fmt.Errorf("post requires -name and -body flags")
		}
		return client.PostTopicMessage(*name, *body)
	default:
		return fmt.Errorf("unknown topic action: %s (use list|create|messages|post)", *action)
	}
	return nil
}

// ---------- Overlay Mesh ----------

func clawnetOverlay(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet overlay", flag.ExitOnError)
	action := fs.String("action", "status", "操作: status|tree|peers|add")
	uri := fs.String("uri", "", "Peer URI (for add)")
	fs.Parse(args)

	switch *action {
	case "status":
		st, err := client.GetOverlayStatus()
		if err != nil {
			return err
		}
		return PrintJSON(st)
	case "tree":
		tree, err := client.GetOverlayTree()
		if err != nil {
			return err
		}
		return PrintJSON(tree)
	case "peers":
		peers, err := client.GetOverlayPeersGeo()
		if err != nil {
			return err
		}
		return PrintJSON(peers)
	case "add":
		if *uri == "" {
			return fmt.Errorf("add requires -uri flag")
		}
		return client.AddOverlayPeer(*uri)
	default:
		return fmt.Errorf("unknown overlay action: %s (use status|tree|peers|add)", *action)
	}
}

// ---------- Resume ----------

func clawnetResume(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet resume", flag.ExitOnError)
	action := fs.String("action", "get", "操作: get|update|match")
	skills := fs.String("skills", "", "技能（逗号分隔）")
	domains := fs.String("domains", "", "领域（逗号分隔）")
	bio := fs.String("bio", "", "简介")
	taskID := fs.String("task", "", "任务 ID (for match)")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	switch *action {
	case "get":
		resume, err := client.GetResume()
		if err != nil {
			return err
		}
		if *jsonOut {
			return PrintJSON(resume)
		}
		fmt.Printf("Resume:\n")
		fmt.Printf("  Skills:  %s\n", strings.Join(resume.Skills, ", "))
		fmt.Printf("  Domains: %s\n", strings.Join(resume.Domains, ", "))
		fmt.Printf("  Bio:     %s\n", resume.Bio)
	case "update":
		skillList := splitTrim(*skills)
		domainList := splitTrim(*domains)
		resume := &clawnet.Resume{Skills: skillList, Domains: domainList, Bio: *bio}
		return client.UpdateResume(resume)
	case "match":
		if *taskID == "" {
			// Match tasks for self
			tasks, err := client.MatchTasks()
			if err != nil {
				return err
			}
			if *jsonOut {
				return PrintJSON(tasks)
			}
			if len(tasks) == 0 {
				fmt.Println("No matching tasks.")
				return nil
			}
			for _, t := range tasks {
				fmt.Printf("[%s] %.1f🐚 %s\n", t.TaskStatus, t.Reward, t.Title)
			}
		} else {
			// Match agents for a task
			agents, err := client.MatchAgentsForTask(*taskID)
			if err != nil {
				return err
			}
			return PrintJSON(agents)
		}
	default:
		return fmt.Errorf("unknown resume action: %s (use get|update|match)", *action)
	}
	return nil
}

// ---------- Diagnostics ----------

func clawnetDiagnostics(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet diagnostics", flag.ExitOnError)
	action := fs.String("action", "all", "操作: all|matrix|traffic")
	fs.Parse(args)

	switch *action {
	case "all":
		diag, err := client.GetDiagnostics()
		if err != nil {
			return err
		}
		return PrintJSON(diag)
	case "matrix":
		m, err := client.GetMatrixStatus()
		if err != nil {
			return err
		}
		return PrintJSON(m)
	case "traffic":
		t, err := client.GetTraffic()
		if err != nil {
			return err
		}
		return PrintJSON(t)
	default:
		return fmt.Errorf("unknown diagnostics action: %s (use all|matrix|traffic)", *action)
	}
}

// ---------- Nutshell ----------

func clawnetNutshell(client *clawnet.Client, args []string) error {
	mgr := clawnet.NewNutshellManager(client.BinPath())
	if len(args) == 0 {
		st := mgr.IsInstalled()
		if st.Installed {
			fmt.Printf("Nutshell installed: %s\n", st.Version)
		} else {
			fmt.Println("Nutshell not installed. Run: maclaw-tui clawnet nutshell -action install")
		}
		return nil
	}

	fs := flag.NewFlagSet("clawnet nutshell", flag.ExitOnError)
	action := fs.String("action", "status", "操作: status|install|init|check|publish|claim|deliver|pack|unpack")
	dir := fs.String("dir", "", "目录路径")
	reward := fs.Float64("reward", 50, "奖励金额")
	taskID := fs.String("task", "", "任务 ID (for claim)")
	output := fs.String("output", "", "输出路径")
	peer := fs.String("peer", "", "加密目标 Peer ID")
	file := fs.String("file", "", ".nut 文件路径")
	fs.Parse(args)

	switch *action {
	case "status":
		st := mgr.IsInstalled()
		if st.Installed {
			fmt.Printf("Nutshell installed: %s\n", st.Version)
		} else {
			fmt.Printf("Nutshell not installed. %s\n", st.Error)
		}
	case "install":
		if err := mgr.Install(); err != nil {
			return err
		}
		fmt.Println("Nutshell installed.")
	case "init":
		if *dir == "" {
			return fmt.Errorf("init requires -dir flag")
		}
		out, err := mgr.Init(*dir)
		if err != nil {
			return err
		}
		fmt.Print(out)
	case "check":
		if *dir == "" {
			return fmt.Errorf("check requires -dir flag")
		}
		out, err := mgr.Check(*dir)
		if err != nil {
			return err
		}
		fmt.Print(out)
	case "publish":
		if *dir == "" {
			return fmt.Errorf("publish requires -dir flag")
		}
		out, err := mgr.Publish(*dir, *reward)
		if err != nil {
			return err
		}
		fmt.Print(out)
	case "claim":
		if *taskID == "" {
			return fmt.Errorf("claim requires -task flag")
		}
		outDir := *output
		if outDir == "" {
			outDir = "./workspace"
		}
		out, err := mgr.Claim(*taskID, outDir)
		if err != nil {
			return err
		}
		fmt.Print(out)
	case "deliver":
		if *dir == "" {
			return fmt.Errorf("deliver requires -dir flag")
		}
		out, err := mgr.Deliver(*dir)
		if err != nil {
			return err
		}
		fmt.Print(out)
	case "pack":
		if *dir == "" || *output == "" {
			return fmt.Errorf("pack requires -dir and -output flags")
		}
		out, err := mgr.Pack(*dir, *output, *peer)
		if err != nil {
			return err
		}
		fmt.Print(out)
	case "unpack":
		if *file == "" {
			return fmt.Errorf("unpack requires -file flag")
		}
		outDir := *output
		if outDir == "" {
			outDir = "./output"
		}
		out, err := mgr.Unpack(*file, outDir)
		if err != nil {
			return err
		}
		fmt.Print(out)
	default:
		return fmt.Errorf("unknown nutshell action: %s", *action)
	}
	return nil
}

// splitTrim splits a comma-separated string and trims whitespace.
func splitTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// ---------- Identity ----------

func clawnetIdentityKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".openclaw", "clawnet", "identity.key"), nil
}

func clawnetIdentity(args []string) error {
	if len(args) == 0 {
		return clawnetIdentityHas()
	}
	switch args[0] {
	case "has-identity":
		return clawnetIdentityHas()
	case "export-identity":
		return clawnetIdentityExport(args[1:])
	case "import-identity":
		return clawnetIdentityImport(args[1:])
	case "backup-key":
		return clawnetIdentityBackup(args[1:])
	case "restore-key":
		return clawnetIdentityRestore(args[1:])
	default:
		return NewUsageError("unknown identity action: %s (use has-identity|export-identity|import-identity|backup-key|restore-key)", args[0])
	}
}

func clawnetIdentityHas() error {
	keyPath, err := clawnetIdentityKeyPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(keyPath); err == nil {
		fmt.Printf("Identity key exists: %s\n", keyPath)
	} else {
		fmt.Println("No identity key found.")
	}
	return nil
}

func clawnetIdentityExport(args []string) error {
	fs := flag.NewFlagSet("identity export-identity", flag.ExitOnError)
	output := fs.String("output", "", "导出路径（必填）")
	fs.Parse(args)
	if *output == "" {
		return fmt.Errorf("export-identity requires -output flag")
	}
	keyPath, err := clawnetIdentityKeyPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("读取 identity key 失败: %w", err)
	}
	if err := os.WriteFile(*output, data, 0600); err != nil {
		return fmt.Errorf("导出失败: %w", err)
	}
	fmt.Printf("Identity key exported to %s\n", *output)
	return nil
}

func clawnetIdentityImport(args []string) error {
	fs := flag.NewFlagSet("identity import-identity", flag.ExitOnError)
	input := fs.String("input", "", "导入路径（必填）")
	fs.Parse(args)
	if *input == "" {
		return fmt.Errorf("import-identity requires -input flag")
	}
	data, err := os.ReadFile(*input)
	if err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}
	keyPath, err := clawnetIdentityKeyPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, data, 0600); err != nil {
		return fmt.Errorf("导入失败: %w", err)
	}
	fmt.Printf("Identity key imported from %s\n", *input)
	return nil
}

func clawnetIdentityBackup(args []string) error {
	fs := flag.NewFlagSet("identity backup-key", flag.ExitOnError)
	output := fs.String("output", "", "备份路径（默认: identity.key.bak）")
	fs.Parse(args)
	keyPath, err := clawnetIdentityKeyPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("读取 identity key 失败: %w", err)
	}
	dest := *output
	if dest == "" {
		dest = keyPath + ".bak"
	}
	if err := os.WriteFile(dest, data, 0600); err != nil {
		return fmt.Errorf("备份失败: %w", err)
	}
	fmt.Printf("Identity key backed up to %s\n", dest)
	return nil
}

func clawnetIdentityRestore(args []string) error {
	fs := flag.NewFlagSet("identity restore-key", flag.ExitOnError)
	input := fs.String("input", "", "备份文件路径（默认: identity.key.bak）")
	fs.Parse(args)
	keyPath, err := clawnetIdentityKeyPath()
	if err != nil {
		return err
	}
	src := *input
	if src == "" {
		src = keyPath + ".bak"
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("读取备份文件失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, data, 0600); err != nil {
		return fmt.Errorf("恢复失败: %w", err)
	}
	fmt.Printf("Identity key restored from %s\n", src)
	return nil
}

// ---------- Leaderboard ----------

func clawnetLeaderboard(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet leaderboard", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)
	lb, err := client.GetLeaderboard()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(lb)
	}
	if len(lb) == 0 {
		fmt.Println("Leaderboard is empty.")
		return nil
	}
	return PrintJSON(lb)
}

// ---------- Transactions ----------

func clawnetTransactions(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet transactions", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)
	txns, err := client.GetCreditsTransactions()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(txns)
	}
	if len(txns) == 0 {
		fmt.Println("No transactions.")
		return nil
	}
	return PrintJSON(txns)
}

// ---------- Credits Audit ----------

func clawnetCreditsAudit(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet credits-audit", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)
	audit, err := client.GetCreditsAudit()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(audit)
	}
	if len(audit) == 0 {
		fmt.Println("No audit records.")
		return nil
	}
	return PrintJSON(audit)
}

// ---------- Auto-Picker ----------

func clawnetAutoPicker(client *clawnet.Client, args []string) error {
	if len(args) == 0 {
		return clawnetAutoPickerStatus(client)
	}
	switch args[0] {
	case "status":
		return clawnetAutoPickerStatus(client)
	case "configure":
		return clawnetAutoPickerConfigure(client, args[1:])
	case "trigger":
		return clawnetAutoPickerTrigger(client, args[1:])
	default:
		return NewUsageError("unknown auto-picker action: %s (use status|configure|trigger)", args[0])
	}
}

func clawnetAutoPickerStatus(client *clawnet.Client) error {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	hubURL := cfg.RemoteHubURL
	picker := clawnet.NewAutoTaskPicker(client, hubURL)
	status := picker.GetStatus()
	return PrintJSON(status)
}

func clawnetAutoPickerConfigure(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("auto-picker configure", flag.ExitOnError)
	enabled := fs.Bool("enabled", false, "启用自动接单")
	pollMinutes := fs.Int("poll-minutes", 5, "轮询间隔（分钟）")
	minReward := fs.Float64("min-reward", 0, "最低奖励")
	tags := fs.String("tags", "", "偏好标签（逗号分隔）")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	hubURL := cfg.RemoteHubURL
	picker := clawnet.NewAutoTaskPicker(client, hubURL)

	var tagList []string
	if *tags != "" {
		tagList = splitTrim(*tags)
	}
	picker.Configure(*enabled, *pollMinutes, *minReward, tagList)
	fmt.Printf("Auto-picker configured: enabled=%v, poll=%dm, min_reward=%.1f, tags=%v\n",
		*enabled, *pollMinutes, *minReward, tagList)
	return nil
}

func clawnetAutoPickerTrigger(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("auto-picker trigger", flag.ExitOnError)
	taskID := fs.String("task", "", "任务 ID（必填）")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("trigger requires -task flag")
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	hubURL := cfg.RemoteHubURL
	picker := clawnet.NewAutoTaskPicker(client, hubURL)
	picker.SetExecutor(func(title, desc string) (string, error) {
		return "", fmt.Errorf("CLI mode does not support task execution; use TUI or daemon mode")
	})

	result := picker.PickAndExecuteTask(*taskID)
	return PrintJSON(result)
}

// ---------- Daemon ----------

func clawnetDaemon(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: clawnet daemon <ensure|stop|info>")
	}
	switch args[0] {
	case "ensure":
		client := clawnet.NewClient()
		if err := client.EnsureDaemon(); err != nil {
			return err
		}
		fmt.Println("ClawNet daemon is running.")
		return nil
	case "stop":
		client := clawnet.NewClient()
		client.StopDaemon()
		fmt.Println("ClawNet daemon stopped.")
		return nil
	case "info":
		client := clawnet.NewClient()
		if client.IsRunning() {
			pid := client.DaemonPID()
			if pid > 0 {
				fmt.Printf("ClawNet daemon running (PID: %d)\n", pid)
			} else {
				fmt.Println("ClawNet daemon running (PID unknown — started externally)")
			}
		} else {
			fmt.Println("ClawNet daemon is not running.")
		}
		return nil
	default:
		return NewUsageError("unknown daemon action: %s (use ensure|stop|info)", args[0])
	}
}

// ---------- Binary ----------

func clawnetBinary(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: clawnet binary <install|update|path>")
	}
	switch args[0] {
	case "install":
		path, err := clawnet.Download(func(stage string, pct int, msg string) {
			fmt.Printf("[%s] %d%% %s\n", stage, pct, msg)
		})
		if err != nil {
			return err
		}
		fmt.Printf("ClawNet binary installed: %s\n", path)
		return nil
	case "update":
		client := clawnet.NewClient()
		if err := client.SelfUpdate(); err != nil {
			return err
		}
		fmt.Println("ClawNet binary updated.")
		return nil
	case "path":
		client := clawnet.NewClient()
		p := client.BinPath()
		if p == "" {
			fmt.Println("ClawNet binary not found.")
		} else {
			fmt.Println(p)
		}
		return nil
	default:
		return NewUsageError("unknown binary action: %s (use install|update|path)", args[0])
	}
}

// ---------- Profile ----------

func clawnetProfile(client *clawnet.Client, args []string) error {
	if len(args) == 0 {
		return clawnetProfileGet(client, nil)
	}
	switch args[0] {
	case "get":
		return clawnetProfileGet(client, args[1:])
	case "update":
		return clawnetProfileUpdate(client, args[1:])
	case "set-motto":
		return clawnetProfileSetMotto(client, args[1:])
	default:
		return NewUsageError("unknown profile action: %s (use get|update|set-motto)", args[0])
	}
}

func clawnetProfileGet(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet profile get", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	if args != nil {
		fs.Parse(args)
	}
	profile, err := client.GetProfile()
	if err != nil {
		return err
	}
	if jsonOut != nil && *jsonOut {
		return PrintJSON(profile)
	}
	fmt.Printf("Profile:\n")
	fmt.Printf("  PeerID: %s\n", profile.PeerID)
	fmt.Printf("  Name:   %s\n", profile.Name)
	fmt.Printf("  Bio:    %s\n", profile.Bio)
	fmt.Printf("  Motto:  %s\n", profile.Motto)
	return nil
}

func clawnetProfileUpdate(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet profile update", flag.ExitOnError)
	name := fs.String("name", "", "名称")
	bio := fs.String("bio", "", "简介")
	fs.Parse(args)
	if *name == "" && *bio == "" {
		return fmt.Errorf("update requires at least -name or -bio flag")
	}
	if err := client.UpdateProfile(*name, *bio); err != nil {
		return err
	}
	fmt.Println("Profile updated.")
	return nil
}

func clawnetProfileSetMotto(client *clawnet.Client, args []string) error {
	fs := flag.NewFlagSet("clawnet profile set-motto", flag.ExitOnError)
	motto := fs.String("motto", "", "座右铭（必填）")
	fs.Parse(args)
	if *motto == "" {
		return fmt.Errorf("set-motto requires -motto flag")
	}
	if err := client.SetMotto(*motto); err != nil {
		return err
	}
	fmt.Println("Motto updated.")
	return nil
}

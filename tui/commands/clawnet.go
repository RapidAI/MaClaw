package commands

import (
	"flag"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/clawnet"
)

// RunClawNet 执行 clawnet 子命令。
func RunClawNet(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui clawnet <status|peers|tasks|credits|knowledge|dm|swarm|prediction|topic|overlay|resume|diagnostics|nutshell>")
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

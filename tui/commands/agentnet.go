package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentnet"
)

// RunAgentNet executes AgentNet subcommands.
func RunAgentNet(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui AgentNet <status|peers|tasks|credits|knowledge|dm|swarm|prediction|topic|overlay|resume|diagnostics|nutshell|identity|leaderboard|credits-audit|auto-picker|daemon|binary|profile|service|ans|poi|reputation|discover|search|transfer|init|bundle|split|dispute|dag|ontology>")
	}

	// Commands that don't require daemon running
	switch args[0] {
	case "identity":
		return agentnetIdentity(args[1:])
	case "daemon":
		return agentnetDaemon(args[1:])
	case "binary":
		return agentnetBinary(args[1:])
	}

	client := agentnet.NewClient()
	if !client.IsRunning() {
		return fmt.Errorf("AgentNet daemon is not running. Start it first or enable agentnet_enabled in config.")
	}
	switch args[0] {
	case "status":
		return agentnetStatus(client, args[1:])
	case "peers":
		return agentnetPeers(client, args[1:])
	case "tasks":
		return agentnetTasks(client, args[1:])
	case "credits":
		return agentnetCredits(client, args[1:])
	case "knowledge":
		return agentnetKnowledge(client, args[1:])
	case "dm":
		return agentnetDM(client, args[1:])
	case "swarm":
		return agentnetSwarm(client, args[1:])
	case "prediction":
		return agentnetPrediction(client, args[1:])
	case "topic":
		return agentnetTopic(client, args[1:])
	case "overlay":
		return agentnetOverlay(client, args[1:])
	case "resume":
		return agentnetResume(client, args[1:])
	case "diagnostics":
		return agentnetDiagnostics(client, args[1:])
	case "nutshell":
		return agentnetNutshell(client, args[1:])
	case "leaderboard":
		return agentnetLeaderboard(client, args[1:])
	case "credits-audit":
		return agentnetCreditsAudit(client, args[1:])
	case "auto-picker":
		return agentnetAutoPicker(client, args[1:])
	case "profile":
		return agentnetProfile(client, args[1:])
	case "service":
		return agentnetService(client, args[1:])
	case "ans":
		return agentnetANS(client, args[1:])
	case "poi":
		return agentnetPoI(client, args[1:])
	case "reputation":
		return agentnetReputation(client, args[1:])
	case "discover":
		return agentnetDiscover(client, args[1:])
	case "search":
		return agentnetSearch(client, args[1:])
	case "transfer":
		return agentnetTransfer(client, args[1:])
	case "init":
		return agentnetInit(client, args[1:])
	case "bundle":
		return agentnetBundle(client, args[1:])
	case "split":
		return agentnetSplit(client, args[1:])
	case "dispute":
		return agentnetDispute(client, args[1:])
	case "dag":
		return agentnetDAG(client, args[1:])
	case "ontology":
		return agentnetOntology(client, args[1:])
	default:
		return NewUsageError("unknown AgentNet action: %s", args[0])
	}
}

func agentnetStatus(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON output")
	fs.Parse(args)

	status, err := client.GetStatus()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(status)
	}
	fmt.Printf("AgentNet Status:\n")
	fmt.Printf("  PeerID:    %s\n", status.PeerID)
	fmt.Printf("  Peers:     %d\n", status.Peers)
	fmt.Printf("  UnreadDM:  %d\n", status.UnreadDM)
	fmt.Printf("  Version:   %s\n", status.Version)
	fmt.Printf("  Uptime:    %s\n", status.Uptime)
	return nil
}

func agentnetPeers(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet peers", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON output")
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
		addr := p.Addr
		if addr == "" && len(p.Addrs) > 0 { addr = p.Addrs[0] }
		fmt.Printf("%-20s %-20s %-10s %s\n",
			TruncateDisplay(p.PeerID, 20), TruncateDisplay(addr, 20), p.Latency, p.Country)
	}
	return nil
}

func agentnetTasks(client *agentnet.Client, args []string) error {
	// Check for subcommand-style actions first
	if len(args) > 0 {
		switch args[0] {
		case "bid":
			return AgentNetTaskBid(client, args[1:])
		case "assign":
			return AgentNetTaskAssign(client, args[1:])
		case "claim":
			return AgentNetTaskClaim(client, args[1:])
		case "submit":
			return agentnetTasksubmit(client, args[1:])
		case "approve":
			return AgentNetTaskApprove(client, args[1:])
		case "reject":
			return AgentNetTaskReject(client, args[1:])
		case "cancel":
			return AgentNetTaskCancel(client, args[1:])
		case "board":
			return AgentNetTaskBoard(client, args[1:])
		case "submissions":
			return agentnetTasksubmissions(client, args[1:])
		case "pick-winner":
			return AgentNetTaskPickWinner(client, args[1:])
		}
	}

	// Default: list tasks
	fs := flag.NewFlagSet("AgentNet tasks", flag.ExitOnError)
	status := fs.String("status", "", "filter by status (open/assigned/completed)")
	jsonOut := fs.Bool("json", false, "JSON output")
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

func AgentNetTaskBid(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet tasks bid", flag.ExitOnError)
	taskID := fs.String("id", "", "task ID (required)")
	amount := fs.Float64("amount", 0, "bid amount")
	message := fs.String("message", "", "bid message")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("bid requires -id flag")
	}
	if err := client.BidOnTask(*taskID, *amount, *message); err != nil {
		return err
	}
	fmt.Printf("Bid placed on task %s.\n", *taskID)
	return nil
}

func AgentNetTaskAssign(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet tasks assign", flag.ExitOnError)
	taskID := fs.String("id", "", "task ID (required)")
	peer := fs.String("peer", "", "assigned Peer ID (required)")
	fs.Parse(args)
	if *taskID == "" || *peer == "" {
		return fmt.Errorf("assign requires -id and -peer flags")
	}
	if err := client.AssignTask(*taskID, *peer); err != nil {
		return err
	}
	fmt.Printf("Task %s assigned to %s.\n", *taskID, *peer)
	return nil
}

func AgentNetTaskClaim(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet tasks claim", flag.ExitOnError)
	taskID := fs.String("id", "", "task ID (required)")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("claim requires -id flag")
	}
	if err := client.ClaimTask(*taskID); err != nil {
		return err
	}
	fmt.Printf("Task %s claimed.\n", *taskID)
	return nil
}

func agentnetTasksubmit(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet tasks submit", flag.ExitOnError)
	taskID := fs.String("id", "", "task ID (required)")
	result := fs.String("result", "", "submission result (required)")
	fs.Parse(args)
	if *taskID == "" || *result == "" {
		return fmt.Errorf("submit requires -id and -result flags")
	}
	if err := client.SubmitTaskResult(*taskID, *result); err != nil {
		return err
	}
	fmt.Printf("Task %s result submitted.\n", *taskID)
	return nil
}

func AgentNetTaskApprove(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet tasks approve", flag.ExitOnError)
	taskID := fs.String("id", "", "task ID (required)")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("approve requires -id flag")
	}
	if err := client.ApproveTask(*taskID); err != nil {
		return err
	}
	fmt.Printf("Task %s approved.\n", *taskID)
	return nil
}

func AgentNetTaskReject(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet tasks reject", flag.ExitOnError)
	taskID := fs.String("id", "", "task ID (required)")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("reject requires -id flag")
	}
	if err := client.RejectTask(*taskID); err != nil {
		return err
	}
	fmt.Printf("Task %s rejected.\n", *taskID)
	return nil
}

func AgentNetTaskCancel(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet tasks cancel", flag.ExitOnError)
	taskID := fs.String("id", "", "task ID (required)")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("cancel requires -id flag")
	}
	if err := client.CancelTask(*taskID); err != nil {
		return err
	}
	fmt.Printf("Task %s cancelled.\n", *taskID)
	return nil
}

func AgentNetTaskBoard(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet tasks board", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON output")
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

func agentnetTasksubmissions(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet tasks submissions", flag.ExitOnError)
	taskID := fs.String("id", "", "task ID (required)")
	jsonOut := fs.Bool("json", false, "JSON output")
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

func AgentNetTaskPickWinner(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet tasks pick-winner", flag.ExitOnError)
	taskID := fs.String("id", "", "task ID (required)")
	winner := fs.String("winner", "", "winner Peer ID (required)")
	fs.Parse(args)
	if *taskID == "" || *winner == "" {
		return fmt.Errorf("pick-winner requires -id and -winner flags")
	}
	if err := client.PickTaskWinner(*taskID, *winner); err != nil {
		return err
	}
	fmt.Printf("Selected %s as winner for task %s.\n", *winner, *taskID)
	return nil
}

func agentnetCredits(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet credits", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON output")
	fs.Parse(args)

	credits, err := client.GetCredits()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(credits)
	}
	fmt.Printf("AgentNet Credits:\n")
	fmt.Printf("  Balance:  %.2f\n", credits.Balance)
	fmt.Printf("  Tier:     %s\n", credits.Tier)
	fmt.Printf("  Energy:   %.2f\n", credits.Energy)
	return nil
}

// ---------- Knowledge ----------

func agentnetKnowledge(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet knowledge", flag.ExitOnError)
	action := fs.String("action", "feed", "action: feed|search|publish")
	domain := fs.String("domain", "", "filter by domain")
	query := fs.String("q", "", "search query")
	title := fs.String("title", "", "publish title")
	body := fs.String("body", "", "publish body")
	jsonOut := fs.Bool("json", false, "JSON output")
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

func agentnetDM(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet dm", flag.ExitOnError)
	action := fs.String("action", "inbox", "action: inbox|thread|send")
	peer := fs.String("peer", "", "target Peer ID")
	body := fs.String("body", "", "message body")
	jsonOut := fs.Bool("json", false, "JSON output")
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
			fmt.Printf("[%s] %s: %s\n", m.Timestamp, TruncateDisplay(m.From, 16), TruncateDisplay(m.Body, 60))
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
			ts := m.Timestamp; if ts == "" { ts = m.SentAt }
			from := m.From; if from == "" { from = m.PeerID }
			fmt.Printf("[%s] %s: %s\n", ts, TruncateDisplay(from, 16), m.Body)
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

func agentnetSwarm(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet swarm", flag.ExitOnError)
	action := fs.String("action", "list", "action: list|create|join|contribute|synthesize")
	sessionID := fs.String("id", "", "session ID")
	topic := fs.String("topic", "", "topic")
	question := fs.String("question", "", "question")
	message := fs.String("message", "", "contribution content")
	stance := fs.String("stance", "", "stance")
	jsonOut := fs.Bool("json", false, "JSON output")
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

func agentnetPrediction(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet prediction", flag.ExitOnError)
	action := fs.String("action", "list", "action: list|create|bet|resolve|appeal|leaderboard")
	predID := fs.String("id", "", "prediction ID")
	question := fs.String("question", "", "prediction question")
	options := fs.String("options", "yes,no", "options (comma-separated)")
	option := fs.String("option", "", "bet/resolve option")
	amount := fs.Float64("amount", 0, "bet amount")
	reason := fs.String("reason", "", "appeal reason")
	jsonOut := fs.Bool("json", false, "JSON output")
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

func agentnetTopic(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet topic", flag.ExitOnError)
	action := fs.String("action", "list", "action: list|create|messages|post")
	name := fs.String("name", "", "channel name")
	desc := fs.String("desc", "", "channel description")
	body := fs.String("body", "", "message body")
	jsonOut := fs.Bool("json", false, "JSON output")
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
			fmt.Printf("[%s] %s: %s\n", m.Timestamp, TruncateDisplay(m.From, 16), m.Body)
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

func agentnetOverlay(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet overlay", flag.ExitOnError)
	action := fs.String("action", "status", "action: status|tree|peers|add")
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

func agentnetResume(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet resume", flag.ExitOnError)
	action := fs.String("action", "get", "action: get|update|match")
	skills := fs.String("skills", "", "skills (comma-separated)")
	domains := fs.String("domains", "", "domains (comma-separated)")
	bio := fs.String("bio", "", "bio")
	taskID := fs.String("task", "", "task ID (for match)")
	jsonOut := fs.Bool("json", false, "JSON output")
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
		resume := &agentnet.Resume{Skills: skillList, Domains: domainList, Bio: *bio}
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
				fmt.Printf("[%s] %.1f %s\n", t.TaskStatus, t.Reward, t.Title)
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

func agentnetDiagnostics(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet diagnostics", flag.ExitOnError)
	action := fs.String("action", "all", "action: all|matrix|traffic")
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

func agentnetNutshell(client *agentnet.Client, args []string) error {
	mgr := agentnet.NewNutshellManager(client.BinPath())
	if len(args) == 0 {
		st := mgr.IsInstalled()
		if st.Installed {
			fmt.Printf("Nutshell installed: %s\n", st.Version)
		} else {
			fmt.Println("Nutshell not installed. Run: maclaw-tui AgentNet nutshell -action install")
		}
		return nil
	}

	fs := flag.NewFlagSet("AgentNet nutshell", flag.ExitOnError)
	action := fs.String("action", "status", "action: status|install|init|check|publish|claim|deliver|pack|unpack")
	dir := fs.String("dir", "", "directory path")
	reward := fs.Float64("reward", 50, "reward amount")
	taskID := fs.String("task", "", "task ID (for claim)")
	output := fs.String("output", "", "output path")
	peer := fs.String("peer", "", "encryption target Peer ID")
	file := fs.String("file", "", ".nut file path")
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

func agentnetIdentityKeyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".anet", "anet", "identity.key"), nil
}

func agentnetIdentity(args []string) error {
	if len(args) == 0 {
		return agentnetIdentityHas()
	}
	switch args[0] {
	case "has-identity":
		return agentnetIdentityHas()
	case "export-identity":
		return agentnetIdentityExport(args[1:])
	case "import-identity":
		return agentnetIdentityImport(args[1:])
	case "backup-key":
		return agentnetIdentityBackup(args[1:])
	case "restore-key":
		return agentnetIdentityRestore(args[1:])
	default:
		return NewUsageError("unknown identity action: %s (use has-identity|export-identity|import-identity|backup-key|restore-key)", args[0])
	}
}

func agentnetIdentityHas() error {
	keyPath, err := agentnetIdentityKeyPath()
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

func agentnetIdentityExport(args []string) error {
	fs := flag.NewFlagSet("identity export-identity", flag.ExitOnError)
	output := fs.String("output", "", "export path (required)")
	fs.Parse(args)
	if *output == "" {
		return fmt.Errorf("export-identity requires -output flag")
	}
	keyPath, err := agentnetIdentityKeyPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("failed to read identity key: %w", err)
	}
	if err := os.WriteFile(*output, data, 0600); err != nil {
		return fmt.Errorf("export failed: %w", err)
	}
	fmt.Printf("Identity key exported to %s\n", *output)
	return nil
}

func agentnetIdentityImport(args []string) error {
	fs := flag.NewFlagSet("identity import-identity", flag.ExitOnError)
	input := fs.String("input", "", "import path (required)")
	fs.Parse(args)
	if *input == "" {
		return fmt.Errorf("import-identity requires -input flag")
	}
	data, err := os.ReadFile(*input)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	keyPath, err := agentnetIdentityKeyPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, data, 0600); err != nil {
		return fmt.Errorf("import failed: %w", err)
	}
	fmt.Printf("Identity key imported from %s\n", *input)
	return nil
}

func agentnetIdentityBackup(args []string) error {
	fs := flag.NewFlagSet("identity backup-key", flag.ExitOnError)
	output := fs.String("output", "", "backup path (default: identity.key.bak)")
	fs.Parse(args)
	keyPath, err := agentnetIdentityKeyPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("failed to read identity key: %w", err)
	}
	dest := *output
	if dest == "" {
		dest = keyPath + ".bak"
	}
	if err := os.WriteFile(dest, data, 0600); err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}
	fmt.Printf("Identity key backed up to %s\n", dest)
	return nil
}

func agentnetIdentityRestore(args []string) error {
	fs := flag.NewFlagSet("identity restore-key", flag.ExitOnError)
	input := fs.String("input", "", "backup file path (default: identity.key.bak)")
	fs.Parse(args)
	keyPath, err := agentnetIdentityKeyPath()
	if err != nil {
		return err
	}
	src := *input
	if src == "" {
		src = keyPath + ".bak"
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read backup file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, data, 0600); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}
	fmt.Printf("Identity key restored from %s\n", src)
	return nil
}

// ---------- Leaderboard ----------

func agentnetLeaderboard(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet leaderboard", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON output")
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

// ---------- Credits Audit ----------

func agentnetCreditsAudit(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet credits-audit", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON output")
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

func agentnetAutoPicker(client *agentnet.Client, args []string) error {
	if len(args) == 0 {
		return agentnetAutoPickerStatus(client)
	}
	switch args[0] {
	case "status":
		return agentnetAutoPickerStatus(client)
	case "configure":
		return agentnetAutoPickerConfigure(client, args[1:])
	case "trigger":
		return agentnetAutoPickerTrigger(client, args[1:])
	default:
		return NewUsageError("unknown auto-picker action: %s (use status|configure|trigger)", args[0])
	}
}

func agentnetAutoPickerStatus(client *agentnet.Client) error {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	hubURL := cfg.RemoteHubURL
	picker := agentnet.NewAutoTaskPicker(client, hubURL)
	status := picker.GetStatus()
	return PrintJSON(status)
}

func agentnetAutoPickerConfigure(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("auto-picker configure", flag.ExitOnError)
	enabled := fs.Bool("enabled", false, "enable auto-picker")
	pollMinutes := fs.Int("poll-minutes", 5, "poll interval (minutes)")
	minReward := fs.Float64("min-reward", 0, "minimum reward")
	tags := fs.String("tags", "", "preferred tags (comma-separated)")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	hubURL := cfg.RemoteHubURL
	picker := agentnet.NewAutoTaskPicker(client, hubURL)

	var tagList []string
	if *tags != "" {
		tagList = splitTrim(*tags)
	}
	picker.Configure(*enabled, *pollMinutes, *minReward, tagList)
	fmt.Printf("Auto-picker configured: enabled=%v, poll=%dm, min_reward=%.1f, tags=%v\n",
		*enabled, *pollMinutes, *minReward, tagList)
	return nil
}

func agentnetAutoPickerTrigger(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("auto-picker trigger", flag.ExitOnError)
	taskID := fs.String("task", "", "task ID (required)")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("trigger requires -task flag")
	}

	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	hubURL := cfg.RemoteHubURL
	picker := agentnet.NewAutoTaskPicker(client, hubURL)
	picker.SetExecutor(func(title, desc string) (string, error) {
		return "", fmt.Errorf("CLI mode does not support task execution; use TUI or daemon mode")
	})

	result := picker.PickAndExecuteTask(*taskID)
	return PrintJSON(result)
}

// ---------- Daemon ----------

func agentnetDaemon(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: AgentNet daemon <ensure|stop|info>")
	}
	switch args[0] {
	case "ensure":
		client := agentnet.NewClient()
		if err := client.EnsureDaemon(); err != nil {
			return err
		}
		fmt.Println("AgentNet daemon is running.")
		return nil
	case "stop":
		client := agentnet.NewClient()
		client.StopDaemon()
		fmt.Println("AgentNet daemon stopped.")
		return nil
	case "info":
		client := agentnet.NewClient()
		if client.IsRunning() {
			pid := client.DaemonPID()
			if pid > 0 {
				fmt.Printf("AgentNet daemon running (PID: %d)\n", pid)
			} else {
				fmt.Println("AgentNet daemon running (PID unknown - started externally)")
			}
		} else {
			fmt.Println("AgentNet daemon is not running.")
		}
		return nil
	default:
		return NewUsageError("unknown daemon action: %s (use ensure|stop|info)", args[0])
	}
}

// ---------- Binary ----------

func agentnetBinary(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: AgentNet binary <install|update|path>")
	}
	switch args[0] {
	case "install":
		path, err := agentnet.Download(func(stage string, pct int, msg string) {
			fmt.Printf("[%s] %d%% %s\n", stage, pct, msg)
		})
		if err != nil {
			return err
		}
		fmt.Printf("AgentNet binary installed: %s\n", path)
		return nil
	case "update":
		client := agentnet.NewClient()
		if err := client.SelfUpdate(); err != nil {
			return err
		}
		fmt.Println("AgentNet binary updated.")
		return nil
	case "path":
		client := agentnet.NewClient()
		p := client.BinPath()
		if p == "" {
			fmt.Println("AgentNet binary not found.")
		} else {
			fmt.Println(p)
		}
		return nil
	default:
		return NewUsageError("unknown binary action: %s (use install|update|path)", args[0])
	}
}

// ---------- Profile ----------

func agentnetProfile(client *agentnet.Client, args []string) error {
	if len(args) == 0 {
		return agentnetProfileGet(client, nil)
	}
	switch args[0] {
	case "get":
		return agentnetProfileGet(client, args[1:])
	case "update":
		return agentnetProfileUpdate(client, args[1:])
	case "set-motto":
		return agentnetProfileSetMotto(client, args[1:])
	default:
		return NewUsageError("unknown profile action: %s (use get|update|set-motto)", args[0])
	}
}

func agentnetProfileGet(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet profile get", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON output")
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

func agentnetProfileUpdate(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet profile update", flag.ExitOnError)
	name := fs.String("name", "", "name")
	bio := fs.String("bio", "", "bio")
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

func agentnetProfileSetMotto(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet profile set-motto", flag.ExitOnError)
	motto := fs.String("motto", "", "motto (required)")
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

// ---------- P2P Service Gateway ----------

func agentnetService(client *agentnet.Client, args []string) error {
	if len(args) == 0 {
		return agentnetServiceList(client)
	}
	switch args[0] {
	case "list":
		return agentnetServiceList(client)
	case "register":
		return agentnetServiceRegister(client, args[1:])
	case "unregister":
		return agentnetServiceUnregister(client, args[1:])
	case "call":
		return agentnetServiceCall(client, args[1:])
	case "discover":
		return agentnetServiceDiscover(client, args[1:])
	default:
		return NewUsageError("unknown service action: %s (use list|register|unregister|call|discover)", args[0])
	}
}

func agentnetServiceList(client *agentnet.Client) error {
	svcs, err := client.ListServices()
	if err != nil {
		return err
	}
	if len(svcs) == 0 {
		fmt.Println("No services registered.")
		return nil
	}
	for _, s := range svcs {
		fmt.Printf("  %s  %s  billing=%s price=%.1f\n", s.Name, s.Description, s.Billing, s.Price)
	}
	return nil
}

func agentnetServiceRegister(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("service register", flag.ExitOnError)
	name := fs.String("name", "", "service name")
	localURL := fs.String("url", "", "local HTTP endpoint")
	desc := fs.String("desc", "", "description")
	tags := fs.String("tags", "", "tags (comma-separated)")
	modes := fs.String("modes", "rr", "transport modes: rr,server-stream,bidi")
	billing := fs.String("billing", "free", "billing: free,per_call,per_kb")
	price := fs.Float64("price", 0, "price per call")
	freeTier := fs.Int("free-tier", 0, "free calls before billing")
	fs.Parse(args)
	if *name == "" || *localURL == "" {
		return fmt.Errorf("register requires -name and -url flags")
	}
	svc := &agentnet.Service{
		Name: *name, URL: *localURL, Description: *desc,
		Tags: splitTrim(*tags), Modes: splitTrim(*modes),
		Billing: *billing, Price: *price, FreeTier: *freeTier,
	}
	if err := client.RegisterService(svc); err != nil {
		return err
	}
	fmt.Printf("Service '%s' registered.\n", *name)
	return nil
}

func agentnetServiceUnregister(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("service unregister", flag.ExitOnError)
	name := fs.String("name", "", "service name")
	fs.Parse(args)
	if *name == "" {
		return fmt.Errorf("unregister requires -name flag")
	}
	if err := client.UnregisterService(*name); err != nil {
		return err
	}
	fmt.Printf("Service '%s' unregistered.\n", *name)
	return nil
}

func agentnetServiceCall(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("service call", flag.ExitOnError)
	peer := fs.String("peer", "", "target Peer ID")
	service := fs.String("service", "", "service name")
	method := fs.String("method", "GET", "HTTP method")
	path := fs.String("path", "/", "request path")
	body := fs.String("body", "", "request body")
	fs.Parse(args)
	if *peer == "" || *service == "" {
		return fmt.Errorf("call requires -peer and -service flags")
	}
	result, err := client.CallService(*peer, *service, *method, *path, nil, *body)
	if err != nil {
		return err
	}
	return PrintJSON(result)
}

func agentnetServiceDiscover(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("service discover", flag.ExitOnError)
	peer := fs.String("peer", "", "target Peer ID")
	fs.Parse(args)
	if *peer == "" {
		return fmt.Errorf("discover requires -peer flag")
	}
	svcs, err := client.DiscoverServices(*peer)
	if err != nil {
		return err
	}
	if len(svcs) == 0 {
		fmt.Println("No services found on peer.")
		return nil
	}
	for _, s := range svcs {
		fmt.Printf("  %s  %s  billing=%s price=%.1f modes=%v\n",
			s.Name, s.Description, s.Billing, s.Price, s.Modes)
	}
	return nil
}

// ---------- ANS (Agent Name Service) ----------

func agentnetANS(client *agentnet.Client, args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: AgentNet ans <register|resolve|lookup>")
	}
	switch args[0] {
	case "register":
		return agentnetANSRegister(client, args[1:])
	case "resolve":
		return agentnetANSResolve(client, args[1:])
	case "lookup":
		return agentnetANSLookup(client, args[1:])
	default:
		return NewUsageError("unknown ans action: %s (use register|resolve|lookup)", args[0])
	}
}

func agentnetANSRegister(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("ans register", flag.ExitOnError)
	name := fs.String("name", "", "ANS name")
	tags := fs.String("tags", "", "skill tags (comma-separated)")
	fs.Parse(args)
	if *name == "" {
		return fmt.Errorf("register requires -name flag")
	}
	entry, err := client.RegisterANS(*name, *tags)
	if err != nil {
		return err
	}
	fmt.Printf("Registered: %s -> %s\n", entry.Name, entry.DID)
	return nil
}

func agentnetANSResolve(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("ans resolve", flag.ExitOnError)
	name := fs.String("name", "", "ANS name")
	fs.Parse(args)
	if *name == "" {
		return fmt.Errorf("resolve requires -name flag")
	}
	entry, err := client.ResolveANS(*name)
	if err != nil {
		return err
	}
	fmt.Printf("%s -> %s\n", entry.Name, entry.DID)
	return nil
}

func agentnetANSLookup(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("ans lookup", flag.ExitOnError)
	tags := fs.String("tags", "", "skill tags (comma-separated)")
	limit := fs.Int("limit", 10, "max results")
	fs.Parse(args)
	if *tags == "" {
		return fmt.Errorf("lookup requires -tags flag")
	}
	entries, err := client.LookupANS(*tags, *limit)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Println("No agents found.")
		return nil
	}
	for _, e := range entries {
		fmt.Printf("  %s -> %s (tags: %s)\n", e.Name, e.DID, e.Tags)
	}
	return nil
}

// ---------- Proof of Intelligence (PoI) ----------

func agentnetPoI(client *agentnet.Client, args []string) error {
	if len(args) == 0 {
		return agentnetPoIList(client)
	}
	switch args[0] {
	case "list", "browse":
		return agentnetPoIList(client)
	case "respond":
		return agentnetPoIRespond(client, args[1:])
	case "scores":
		return agentnetPoIScores(client)
	default:
		return NewUsageError("unknown poi action: %s (use list|respond|scores)", args[0])
	}
}

func agentnetPoIList(client *agentnet.Client) error {
	challenges, err := client.ListPoIChallenges()
	if err != nil {
		return err
	}
	if len(challenges) == 0 {
		fmt.Println("No PoI challenges available.")
		return nil
	}
	return PrintJSON(challenges)
}

func agentnetPoIRespond(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("poi respond", flag.ExitOnError)
	id := fs.String("id", "", "challenge ID")
	response := fs.String("response", "", "response content")
	fs.Parse(args)
	if *id == "" || *response == "" {
		return fmt.Errorf("respond requires -id and -response flags")
	}
	if err := client.RespondToPoI(*id, map[string]interface{}{"response": *response}); err != nil {
		return err
	}
	fmt.Println("PoI response submitted.")
	return nil
}

func agentnetPoIScores(client *agentnet.Client) error {
	scores, err := client.GetPoIScores()
	if err != nil {
		return err
	}
	if len(scores) == 0 {
		fmt.Println("No PoI scores.")
		return nil
	}
	return PrintJSON(scores)
}

// ---------- Reputation ----------

func agentnetReputation(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet reputation", flag.ExitOnError)
	did := fs.String("did", "", "target DID")
	fs.Parse(args)
	if *did == "" {
		return fmt.Errorf("reputation requires -did flag")
	}
	rep, err := client.GetReputation(*did)
	if err != nil {
		return err
	}
	fmt.Printf("DID:   %s\nScore: %.2f\nTier:  %s\n", rep.DID, rep.Score, rep.Tier)
	return nil
}

// ---------- Agent Discovery ----------

func agentnetDiscover(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet discover", flag.ExitOnError)
	query := fs.String("q", "", "search query")
	fs.Parse(args)
	if *query == "" {
		return fmt.Errorf("discover requires -q flag")
	}
	agents, err := client.DiscoverAgents(*query)
	if err != nil {
		return err
	}
	if len(agents) == 0 {
		fmt.Println("No agents found.")
		return nil
	}
	return PrintJSON(agents)
}

// ---------- Cross-Domain Search ----------

func agentnetSearch(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet search", flag.ExitOnError)
	query := fs.String("q", "", "search query")
	fs.Parse(args)
	if *query == "" {
		return fmt.Errorf("search requires -q flag")
	}
	results, err := client.CrossDomainSearch(*query)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("No results.")
		return nil
	}
	return PrintJSON(results)
}

// ---------- Credits Transfer ----------

func agentnetTransfer(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet transfer", flag.ExitOnError)
	to := fs.String("to", "", "target DID")
	amount := fs.Float64("amount", 0, "transfer amount")
	reason := fs.String("reason", "", "transfer reason")
	fs.Parse(args)
	if *to == "" || *amount <= 0 {
		return fmt.Errorf("transfer requires -to and -amount flags")
	}
	if err := client.TransferCredits(*to, *amount, *reason); err != nil {
		return err
	}
	fmt.Printf("Transferred %.1f Shells to %s\n", *amount, *to)
	return nil
}

// ---------- Agent Init ----------

func agentnetInit(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet init", flag.ExitOnError)
	name := fs.String("name", "", "agent name")
	skills := fs.String("skills", "", "skill tags (comma-separated)")
	fs.Parse(args)
	if *name == "" {
		return fmt.Errorf("init requires -name flag")
	}
	if err := client.InitAgent(*name, splitTrim(*skills)); err != nil {
		return err
	}
	fmt.Printf("Agent '%s' initialized.\n", *name)
	return nil
}

// ---------- Task Bundle ----------

func agentnetBundle(client *agentnet.Client, args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: AgentNet bundle <attach|download>")
	}
	switch args[0] {
	case "attach":
		return agentnetBundleAttach(client, args[1:])
	case "download":
		return agentnetBundleDownload(client, args[1:])
	default:
		return NewUsageError("unknown bundle action: %s (use attach|download)", args[0])
	}
}

func agentnetBundleAttach(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("bundle attach", flag.ExitOnError)
	taskID := fs.String("task", "", "task ID")
	file := fs.String("file", "", ".nut file path")
	fs.Parse(args)
	if *taskID == "" || *file == "" {
		return fmt.Errorf("attach requires -task and -file flags")
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		return fmt.Errorf("read bundle file: %w", err)
	}
	if err := client.AttachBundle(*taskID, data); err != nil {
		return err
	}
	fmt.Printf("Bundle attached to task %s (%d bytes)\n", *taskID, len(data))
	return nil
}

func agentnetBundleDownload(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("bundle download", flag.ExitOnError)
	taskID := fs.String("task", "", "task ID")
	output := fs.String("output", "", "output file path")
	fs.Parse(args)
	if *taskID == "" {
		return fmt.Errorf("download requires -task flag")
	}
	data, err := client.DownloadBundle(*taskID)
	if err != nil {
		return err
	}
	outPath := *output
	if outPath == "" {
		outPath = *taskID + ".nut"
	}
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return fmt.Errorf("write bundle: %w", err)
	}
	fmt.Printf("Bundle downloaded to %s (%d bytes)\n", outPath, len(data))
	return nil
}

// ---------- Split Tasks ----------

func agentnetSplit(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet split", flag.ExitOnError)
	title := fs.String("title", "", "task title")
	reward := fs.Float64("reward", 0, "total reward")
	slots := fs.Int("slots", 2, "number of slots")
	fs.Parse(args)
	if *title == "" {
		return fmt.Errorf("split requires -title flag")
	}
	task, err := client.CreateSplitTask(*title, *reward, *slots)
	if err != nil {
		return err
	}
	fmt.Printf("Split task created: %s (slots=%d, reward=%.1f)\n", task.ID, *slots, *reward)
	return nil
}

// ---------- Disputes ----------

func agentnetDispute(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet dispute", flag.ExitOnError)
	taskID := fs.String("task", "", "task ID")
	reason := fs.String("reason", "", "dispute reason")
	fs.Parse(args)
	if *taskID == "" || *reason == "" {
		return fmt.Errorf("dispute requires -task and -reason flags")
	}
	if err := client.FileDispute(*taskID, *reason); err != nil {
		return err
	}
	fmt.Printf("Dispute filed for task %s\n", *taskID)
	return nil
}

// ---------- DAG ----------

func agentnetDAG(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet dag", flag.ExitOnError)
	intent := fs.String("intent", "", "task intent")
	steps := fs.String("steps", "", "steps (comma-separated)")
	outputs := fs.String("outputs", "", "outputs (comma-separated)")
	fs.Parse(args)
	if *intent == "" {
		return fmt.Errorf("dag requires -intent flag")
	}
	nodes, err := client.ExtractDAG(*intent, splitTrim(*steps), splitTrim(*outputs))
	if err != nil {
		return err
	}
	return PrintJSON(nodes)
}

// ---------- Ontology ----------

func agentnetOntology(client *agentnet.Client, args []string) error {
	fs := flag.NewFlagSet("AgentNet ontology", flag.ExitOnError)
	query := fs.String("q", "", "search query")
	depth := fs.Int("depth", 2, "subgraph depth")
	fs.Parse(args)
	if *query == "" {
		return fmt.Errorf("ontology requires -q flag")
	}
	result, err := client.QueryOntology(*query, *depth)
	if err != nil {
		return err
	}
	return PrintJSON(result)
}

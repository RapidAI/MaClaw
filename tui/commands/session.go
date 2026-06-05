package commands

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// SessionView 会话的 CLI 展示结构。
type SessionView struct {
	ID          string `json:"id"`
	Tool        string `json:"tool"`
	Title       string `json:"title"`
	ProjectPath string `json:"project_path"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
}

// RunSession 执行 session 子命令。
func RunSession(args []string, hubURL, token string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui session <list|start|attach|kill>")
	}

	client := NewHubClient(hubURL, token)
	if err := client.Connect(); err != nil {
		return err
	}
	defer client.Close()

	switch args[0] {
	case "list":
		return sessionList(client, args[1:])
	case "start":
		return sessionStart(client, args[1:])
	case "attach":
		return sessionAttach(client, args[1:])
	case "kill":
		return sessionKill(client, args[1:])
	default:
		return NewUsageError("unknown session action: %s", args[0])
	}
}

func sessionList(client *HubClient, args []string) error {
	fs := flag.NewFlagSet("session list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	data, err := client.Request("cli.list_sessions", nil)
	if err != nil {
		return err
	}

	var sessions []SessionView
	if err := json.Unmarshal(data, &sessions); err != nil {
		return fmt.Errorf("parse sessions: %w", err)
	}

	if *jsonOut {
		return PrintJSON(sessions)
	}

	if len(sessions) == 0 {
		fmt.Println("No active sessions.")
		return nil
	}

	fmt.Printf("%-20s %-12s %-10s %-30s %s\n", "ID", "TOOL", "STATUS", "PROJECT", "TITLE")
	fmt.Println(strings.Repeat("-", 90))
	for _, s := range sessions {
		title := s.Title
		if len(title) > 30 {
			title = title[:27] + "..."
		}
		project := s.ProjectPath
		if len(project) > 30 {
			project = "..." + project[len(project)-27:]
		}
		fmt.Printf("%-20s %-12s %-10s %-30s %s\n", s.ID, s.Tool, s.Status, project, title)
	}
	return nil
}

func sessionStart(client *HubClient, args []string) error {
	fs := flag.NewFlagSet("session start", flag.ExitOnError)
	tool := fs.String("tool", "", "工具名称 (claude, codex, opencode)")
	project := fs.String("project", "", "项目路径")
	tmpl := fs.String("template", "", "模板名称")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	payload := map[string]string{}
	if *tool != "" {
		payload["tool"] = *tool
	}
	if *project != "" {
		payload["project_path"] = *project
	}
	if *tmpl != "" {
		payload["template_name"] = *tmpl
	}

	data, err := client.Request("cli.start_session", payload)
	if err != nil {
		return err
	}

	if *jsonOut {
		var raw interface{}
		if json.Unmarshal(data, &raw) == nil {
			return PrintJSON(raw)
		}
		fmt.Println(string(data))
		return nil
	}

	var result struct {
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("parse result: %w", err)
	}
	fmt.Printf("Session created: %s (status: %s)\n", result.SessionID, result.Status)
	return nil
}

func sessionAttach(client *HubClient, args []string) error {
	fs := flag.NewFlagSet("session attach", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出（仅输出事件流为 JSON lines）")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: session attach <session-id>")
	}
	sessionID := fs.Arg(0)

	payload, _ := json.Marshal(map[string]string{"session_id": sessionID})
	env := Envelope{Type: "cli.attach_session", RequestID: requestID(), Payload: payload}
	if err := client.SendRaw(env); err != nil {
		return err
	}

	resp, err := client.ReadRaw()
	if err != nil {
		return err
	}
	if resp.Type == "error" {
		return fmt.Errorf("attach failed: %s", string(resp.Payload))
	}

	if !*jsonOut {
		fmt.Printf("Attached to session %s. Press Ctrl+C to detach.\n", sessionID)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	defer signal.Stop(sigCh)

	doneCh := make(chan struct{})

	// 读取输出
	go func() {
		defer close(doneCh)
		for {
			msg, err := client.ReadRaw()
			if err != nil {
				return
			}
			if msg.Payload != nil {
				fmt.Println(string(msg.Payload))
			}
		}
	}()

	// 读取输入
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			p, _ := json.Marshal(map[string]string{
				"session_id": sessionID,
				"input":      scanner.Text(),
			})
			client.SendRaw(Envelope{
				Type:      "cli.session_input",
				RequestID: requestID(),
				Payload:   p,
			})
		}
	}()

	select {
	case <-sigCh:
		if !*jsonOut {
			fmt.Println("\nDetaching (session continues running)...")
		}
	case <-doneCh:
		if !*jsonOut {
			fmt.Println("Session connection closed.")
		}
	}
	return nil
}

func sessionKill(client *HubClient, args []string) error {
	fs := flag.NewFlagSet("session kill", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: session kill <session-id>")
	}
	sessionID := fs.Arg(0)

	data, err := client.Request("cli.kill_session", map[string]string{"session_id": sessionID})
	if err != nil {
		return err
	}

	if *jsonOut {
		result := map[string]string{"session_id": sessionID, "status": "terminated"}
		if data != nil {
			var parsed map[string]interface{}
			if json.Unmarshal(data, &parsed) == nil {
				return PrintJSON(parsed)
			}
		}
		return PrintJSON(result)
	}

	fmt.Printf("Session %s terminated.\n", sessionID)
	return nil
}

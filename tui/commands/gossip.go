package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// gossipHTTPClient is a shared http.Client for all gossip TUI commands.
var gossipHTTPClient = &http.Client{Timeout: 30 * time.Second}

// resolveMachineID 从本地配置读取 machine_id。
func resolveMachineID() string {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	return strings.TrimSpace(cfg.RemoteMachineID)
}

// RunGossip 执行 gossip 子命令。
func RunGossip(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui gossip <browse|publish|comment|rate|comments>")
	}
	switch args[0] {
	case "browse":
		return gossipBrowse(args[1:])
	case "publish":
		return gossipPublish(args[1:])
	case "comment":
		return gossipComment(args[1:])
	case "rate":
		return gossipRate(args[1:])
	case "comments":
		return gossipComments(args[1:])
	default:
		return NewUsageError("unknown gossip action: %s", args[0])
	}
}

// gossipBrowse 浏览八卦列表。gossip browse [--page N] [--json]
func gossipBrowse(args []string) error {
	fs := flag.NewFlagSet("gossip browse", flag.ExitOnError)
	page := fs.Int("page", 1, "页码")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	base := resolveHubCenterURL()
	endpoint := fmt.Sprintf("%s/api/gossip/browse?page=%d", base, *page)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("操作失败: %w", err)
	}
	resp, err := gossipHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("操作失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("操作失败: HTTP %d — %s", resp.StatusCode, string(body))
	}

	var result struct {
		OK    bool `json:"ok"`
		Posts []struct {
			ID        string `json:"id"`
			Nickname  string `json:"nickname"`
			Content   string `json:"content"`
			Category  string `json:"category"`
			Score     int    `json:"score"`
			Votes     int    `json:"votes"`
			Locked    bool   `json:"locked"`
			CreatedAt string `json:"created_at"`
		} `json:"posts"`
		Total int `json:"total"`
		Page  int `json:"page"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("解析结果失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(result)
	}

	fmt.Printf("八卦列表 — 共 %d 条\n\n", result.Total)
	fmt.Printf("%-24s %-10s %-6s %-6s %-14s %s\n", "ID", "CATEGORY", "SCORE", "VOTES", "NICKNAME", "CONTENT")
	fmt.Println(strings.Repeat("-", 90))
	for _, p := range result.Posts {
		fmt.Printf("%-24s %-10s %-6d %-6d %-14s %s\n",
			TruncateDisplay(p.ID, 24),
			p.Category,
			p.Score,
			p.Votes,
			TruncateDisplay(p.Nickname, 14),
			TruncateDisplay(p.Content, 40))
	}
	return nil
}

// gossipPublish 发布八卦帖子。gossip publish --content "..." --category owner|project|news
func gossipPublish(args []string) error {
	fs := flag.NewFlagSet("gossip publish", flag.ExitOnError)
	content := fs.String("content", "", "帖子内容")
	category := fs.String("category", "", "分类: owner|project|news")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if *content == "" {
		return NewUsageError("usage: gossip publish --content \"...\" --category owner|project|news")
	}
	if *category == "" {
		return NewUsageError("usage: gossip publish --content \"...\" --category owner|project|news")
	}

	email := resolveEmail()
	if email == "" {
		return fmt.Errorf("邮箱未配置，请在配置中设置 remote_email")
	}
	mid := resolveMachineID()
	base := resolveHubCenterURL()

	payload, _ := json.Marshal(map[string]string{
		"machine_id": mid,
		"user_email": email,
		"content":    *content,
		"category":   *category,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/gossip/publish", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("操作失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Machine-ID", mid)

	resp, err := gossipHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("操作失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("操作失败: HTTP %d — %s", resp.StatusCode, string(body))
	}

	var result struct {
		OK   bool `json:"ok"`
		Post struct {
			ID        string `json:"id"`
			Nickname  string `json:"nickname"`
			Content   string `json:"content"`
			Category  string `json:"category"`
			CreatedAt string `json:"created_at"`
		} `json:"post"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("解析结果失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(result)
	}

	fmt.Printf("✓ 发布成功 (ID: %s)\n", result.Post.ID)
	return nil
}

// gossipComment 提交评论。gossip comment --post-id ID --content "..." [--rating 0-5]
func gossipComment(args []string) error {
	fs := flag.NewFlagSet("gossip comment", flag.ExitOnError)
	postID := fs.String("post-id", "", "帖子 ID")
	content := fs.String("content", "", "评论内容")
	rating := fs.Int("rating", 0, "评分 0-5")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if *postID == "" {
		return NewUsageError("usage: gossip comment --post-id ID --content \"...\" [--rating 0-5]")
	}
	if *content == "" {
		return NewUsageError("usage: gossip comment --post-id ID --content \"...\" [--rating 0-5]")
	}

	email := resolveEmail()
	if email == "" {
		return fmt.Errorf("邮箱未配置，请在配置中设置 remote_email")
	}
	mid := resolveMachineID()
	base := resolveHubCenterURL()

	payload, _ := json.Marshal(map[string]interface{}{
		"machine_id": mid,
		"user_email": email,
		"post_id":    *postID,
		"content":    *content,
		"rating":     *rating,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/gossip/comment", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("操作失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Machine-ID", mid)

	resp, err := gossipHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("操作失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("操作失败: HTTP %d — %s", resp.StatusCode, string(body))
	}

	var result struct {
		OK      bool `json:"ok"`
		Comment struct {
			ID        string `json:"id"`
			Nickname  string `json:"nickname"`
			Content   string `json:"content"`
			Rating    int    `json:"rating"`
			CreatedAt string `json:"created_at"`
		} `json:"comment"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("解析结果失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(result)
	}

	fmt.Printf("✓ 评论成功 (ID: %s)\n", result.Comment.ID)
	return nil
}

// gossipRate 评分帖子。gossip rate --post-id ID --rating 1-5
func gossipRate(args []string) error {
	fs := flag.NewFlagSet("gossip rate", flag.ExitOnError)
	postID := fs.String("post-id", "", "帖子 ID")
	rating := fs.Int("rating", 0, "评分 1-5")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if *postID == "" {
		return NewUsageError("usage: gossip rate --post-id ID --rating 1-5")
	}
	if *rating == 0 {
		return NewUsageError("usage: gossip rate --post-id ID --rating 1-5")
	}

	email := resolveEmail()
	if email == "" {
		return fmt.Errorf("邮箱未配置，请在配置中设置 remote_email")
	}
	mid := resolveMachineID()
	base := resolveHubCenterURL()

	payload, _ := json.Marshal(map[string]interface{}{
		"machine_id": mid,
		"user_email": email,
		"post_id":    *postID,
		"rating":     *rating,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/gossip/rate", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("操作失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Machine-ID", mid)

	resp, err := gossipHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("操作失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("操作失败: HTTP %d — %s", resp.StatusCode, string(body))
	}

	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("解析结果失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(result)
	}

	fmt.Println("✓ 评分成功")
	return nil
}

// gossipComments 查看帖子评论列表。gossip comments --post-id ID [--page N] [--json]
func gossipComments(args []string) error {
	fs := flag.NewFlagSet("gossip comments", flag.ExitOnError)
	postID := fs.String("post-id", "", "帖子 ID")
	page := fs.Int("page", 1, "页码")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if *postID == "" {
		return NewUsageError("usage: gossip comments --post-id ID [--page N] [--json]")
	}

	base := resolveHubCenterURL()
	endpoint := fmt.Sprintf("%s/api/gossip/comments?post_id=%s&page=%d", base, url.QueryEscape(*postID), *page)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("操作失败: %w", err)
	}
	resp, err := gossipHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("操作失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("操作失败: HTTP %d — %s", resp.StatusCode, string(body))
	}

	var result struct {
		OK       bool `json:"ok"`
		Comments []struct {
			ID        string `json:"id"`
			Nickname  string `json:"nickname"`
			Content   string `json:"content"`
			Rating    int    `json:"rating"`
			CreatedAt string `json:"created_at"`
		} `json:"comments"`
		Total int `json:"total"`
		Page  int `json:"page"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("解析结果失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(result)
	}

	fmt.Printf("评论列表 — 共 %d 条\n\n", result.Total)
	fmt.Printf("%-14s %-8s %-30s %s\n", "NICKNAME", "RATING", "CONTENT", "TIME")
	fmt.Println(strings.Repeat("-", 80))
	for _, c := range result.Comments {
		ratingStr := fmt.Sprintf("★%d", c.Rating)
		if c.Rating == 0 {
			ratingStr = "-"
		}
		fmt.Printf("%-14s %-8s %-30s %s\n",
			TruncateDisplay(c.Nickname, 14),
			ratingStr,
			TruncateDisplay(c.Content, 30),
			c.CreatedAt)
	}
	return nil
}

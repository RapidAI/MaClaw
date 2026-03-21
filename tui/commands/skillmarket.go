package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// RunSkillMarket 执行 skillmarket 子命令。
func RunSkillMarket(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui skillmarket <search|submit|status|account>")
	}
	switch args[0] {
	case "search":
		return smSearch(args[1:])
	case "submit":
		return smSubmit(args[1:])
	case "status":
		return smStatus(args[1:])
	case "account":
		return smAccount(args[1:])
	default:
		return NewUsageError("unknown skillmarket action: %s", args[0])
	}
}

func resolveHubCenterURL() string {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	u := strings.TrimSpace(cfg.RemoteHubCenterURL)
	if u == "" {
		u = remote.DefaultRemoteHubCenterURL
	}
	return strings.TrimRight(u, "/")
}

func resolveEmail() string {
	store := NewFileConfigStore(ResolveDataDir())
	cfg, _ := store.LoadConfig()
	return strings.TrimSpace(cfg.RemoteEmail)
}

// smSearch 搜索 SkillMarket。
func smSearch(args []string) error {
	fs := flag.NewFlagSet("skillmarket search", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	topN := fs.Int("top", 20, "返回数量")
	fs.Parse(args)

	query := strings.Join(fs.Args(), " ")
	base := resolveHubCenterURL()

	endpoint := fmt.Sprintf("%s/api/v1/skillmarket/search?q=%s&top_n=%d", base, query, *topN)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("搜索 SkillMarket 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SkillMarket 返回 HTTP %d", resp.StatusCode)
	}

	var result struct {
		Results []struct {
			ID          string  `json:"id"`
			Name        string  `json:"name"`
			Description string  `json:"description"`
			Price       int     `json:"price"`
			AvgRating   float64 `json:"avg_rating"`
			Downloads   int     `json:"downloads"`
			Author      string  `json:"author"`
		} `json:"results"`
		Total int `json:"total"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return fmt.Errorf("解析结果失败: %w", err)
	}

	if *jsonOut {
		return PrintJSON(result)
	}

	if len(result.Results) == 0 {
		fmt.Println("未找到匹配的 Skill。")
		return nil
	}

	fmt.Printf("SkillMarket 搜索结果 — 共 %d 个\n\n", result.Total)
	fmt.Printf("%-24s %-6s %-5s %-8s %-12s %s\n", "ID", "PRICE", "★", "DOWNLOADS", "AUTHOR", "NAME")
	fmt.Println(strings.Repeat("-", 90))
	for _, s := range result.Results {
		price := "free"
		if s.Price > 0 {
			price = fmt.Sprintf("%d", s.Price)
		}
		rating := fmt.Sprintf("%.1f", s.AvgRating)
		fmt.Printf("%-24s %-6s %-5s %-8d %-12s %s\n",
			TruncateDisplay(s.ID, 24),
			price,
			rating,
			s.Downloads,
			TruncateDisplay(s.Author, 12),
			TruncateDisplay(s.Name, 30))
	}
	return nil
}

// smSubmit 提交 Skill zip 到 SkillMarket。
func smSubmit(args []string) error {
	fs := flag.NewFlagSet("skillmarket submit", flag.ExitOnError)
	email := fs.String("email", "", "提交者邮箱（默认从配置读取）")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: skillmarket submit <skill.zip> [--email <email>]")
	}
	zipPath := fs.Arg(0)

	if *email == "" {
		*email = resolveEmail()
	}
	if *email == "" {
		return fmt.Errorf("邮箱未配置，请使用 --email 参数或在配置中设置 remote_email")
	}

	base := resolveHubCenterURL()

	f, err := os.Open(zipPath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("zip", filepath.Base(zipPath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return err
	}
	_ = w.WriteField("email", *email)
	w.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/skills/submit", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("提交失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("提交失败 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		SubmissionID string `json:"submission_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if *jsonOut {
		return PrintJSON(result)
	}
	fmt.Printf("✓ 提交成功，submission_id: %s\n", result.SubmissionID)
	fmt.Println("  使用 skillmarket status <submission_id> 查看审核状态")
	return nil
}

// smStatus 查询提交状态。
func smStatus(args []string) error {
	fs := flag.NewFlagSet("skillmarket status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: skillmarket status <submission-id>")
	}
	submissionID := fs.Arg(0)

	base := resolveHubCenterURL()
	endpoint := base + "/api/v1/skill-submissions/" + submissionID

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("查询失败: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Status   string `json:"status"`
		ErrorMsg string `json:"error_msg"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if *jsonOut {
		return PrintJSON(result)
	}
	fmt.Printf("提交 %s 状态: %s\n", submissionID, result.Status)
	if result.ErrorMsg != "" {
		fmt.Printf("  错误: %s\n", result.ErrorMsg)
	}
	return nil
}

// smAccount 查看 SkillMarket 账户信息。
func smAccount(args []string) error {
	fs := flag.NewFlagSet("skillmarket account", flag.ExitOnError)
	email := fs.String("email", "", "邮箱（默认从配置读取）")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if *email == "" {
		*email = resolveEmail()
	}
	if *email == "" {
		return fmt.Errorf("邮箱未配置，请使用 --email 参数或在配置中设置 remote_email")
	}

	base := resolveHubCenterURL()
	endpoint := base + "/api/v1/account/" + *email

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("查询账户失败: %w", err)
	}
	defer resp.Body.Close()

	var info struct {
		ID                string `json:"id"`
		Email             string `json:"email"`
		Status            string `json:"status"`
		Credits           int64  `json:"credits"`
		SettledCredits    int64  `json:"settled_credits"`
		PendingSettlement int64  `json:"pending_settlement"`
		VoucherCount      int    `json:"voucher_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return err
	}

	if *jsonOut {
		return PrintJSON(info)
	}
	fmt.Printf("SkillMarket 账户: %s\n", info.Email)
	fmt.Printf("  状态:     %s\n", info.Status)
	fmt.Printf("  积分:     %d\n", info.Credits)
	fmt.Printf("  已结算:   %d\n", info.SettledCredits)
	fmt.Printf("  待结算:   %d\n", info.PendingSettlement)
	fmt.Printf("  优惠券:   %d\n", info.VoucherCount)
	return nil
}

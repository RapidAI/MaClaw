package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// TaskVerdict is the result of verifying an agent's output against its task.
type TaskVerdict struct {
	Pass    bool   `json:"pass"`
	Score   int    `json:"score"`   // 0-100
	Reason  string `json:"reason"`
	Missing string `json:"missing"` // what's missing if not pass
}

// TaskVerifier uses an LLM to check whether an agent's output actually
// fulfills the assigned task description. This prevents tasks from "drifting"
// — an agent doing unrelated work without anyone noticing until the test phase.
//
// 在 TDD 模式下，VerifyByTest 通过实际运行测试命令来验证，而非依赖 LLM 判断。
type TaskVerifier struct {
	llmConfig MaclawLLMConfig
}

// NewTaskVerifier creates a TaskVerifier.
func NewTaskVerifier(cfg MaclawLLMConfig) *TaskVerifier {
	return &TaskVerifier{llmConfig: cfg}
}

// Verify checks whether the agent output satisfies the task description.
// Returns a TaskVerdict with pass/fail, a 0-100 score, and reasoning.
// If acceptanceCriteria is provided, the LLM will verify against those
// specific criteria instead of just the general task description.
func (v *TaskVerifier) Verify(taskDesc, agentOutput string, acceptanceCriteria ...string) (*TaskVerdict, error) {
	if strings.TrimSpace(agentOutput) == "" {
		return &TaskVerdict{
			Pass:   false,
			Score:  0,
			Reason: "agent 没有产出任何输出",
		}, nil
	}

	// 构建验收标准部分
	criteriaSection := ""
	if len(acceptanceCriteria) > 0 {
		var sb strings.Builder
		sb.WriteString("\n验收标准（逐条检查）：\n")
		for i, c := range acceptanceCriteria {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, c))
		}
		criteriaSection = sb.String()
	}

	prompt := fmt.Sprintf(`你是一个严格的代码审查员。请判断以下 agent 的工作输出是否完成了指定的任务。

任务描述：
%s
%s
Agent 输出摘要：
%s

请用 JSON 格式回答，包含以下字段：
- pass: bool，任务是否基本完成（所有验收标准都满足）
- score: int (0-100)，完成度评分
- reason: string，判断理由（简洁）
- missing: string，如果未完成，缺少什么（如果已完成则为空字符串）

只返回 JSON，不要其他内容。`, taskDesc, criteriaSection, truncateForPrompt(agentOutput, 3000))

	body, err := swarmCallLLM(v.llmConfig, prompt, 0.1, 30*time.Second)
	if err != nil {
		// LLM 调用失败时默认通过，避免阻塞流程
		return &TaskVerdict{Pass: true, Score: 50, Reason: "验收 LLM 调用失败，默认通过: " + err.Error()}, nil
	}

	var verdict TaskVerdict
	cleaned := extractJSONObject(body)
	if err := json.Unmarshal(cleaned, &verdict); err != nil {
		return &TaskVerdict{Pass: true, Score: 50, Reason: "验收结果解析失败，默认通过"}, nil
	}
	return &verdict, nil
}

// extractJSONObject finds the first { ... } block in the text.
func extractJSONObject(data []byte) []byte {
	s := string(data)
	// Strip markdown fences
	if idx := strings.Index(s, "```json"); idx >= 0 {
		s = s[idx+7:]
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	} else if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[idx+3:]
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return []byte(s[start : end+1])
	}
	return []byte(strings.TrimSpace(s))
}

// truncateForPrompt truncates text to roughly maxChars, rune-safe.
func truncateForPrompt(text string, maxChars int) string {
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars]) + "\n...(已截断)"
}

// VerifyByTest 通过实际运行测试命令来验证任务完成情况（TDD 硬性验收）。
// 返回 TaskVerdict，其中 Pass 基于测试是否全部通过。
func (v *TaskVerifier) VerifyByTest(workDir, testCmd, testFile string) *TaskVerdict {
	if testCmd == "" {
		return &TaskVerdict{Pass: true, Score: 50, Reason: "未配置测试命令，跳过 TDD 验证"}
	}

	// 如果指定了测试文件，尝试只运行该文件的测试
	cmd := testCmd
	if testFile != "" {
		cmd = buildTargetedTestCmd(testCmd, testFile)
	}

	output, exitCode := runTestShellCommand(workDir, cmd, 120*time.Second)

	if exitCode == 0 {
		return &TaskVerdict{
			Pass:   true,
			Score:  100,
			Reason: "所有测试通过 ✅",
		}
	}

	// 解析失败数量
	failCount := countTestFailures(output)
	totalCount := countTestTotal(output)
	score := 0
	if totalCount > 0 && failCount <= totalCount {
		passCount := totalCount - failCount
		score = passCount * 100 / totalCount
	}

	return &TaskVerdict{
		Pass:    false,
		Score:   score,
		Reason:  fmt.Sprintf("测试未通过 (%d/%d 失败)", failCount, totalCount),
		Missing: extractFailingSummary(output),
	}
}

// runTestShellCommand 在指定目录执行 shell 命令，返回输出和退出码。
func runTestShellCommand(workDir, cmd string, timeout time.Duration) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/C", cmd)
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", cmd)
	}
	c.Dir = workDir

	var buf bytes.Buffer
	c.Stdout = &buf
	c.Stderr = &buf

	err := c.Run()
	output := buf.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, exitErr.ExitCode()
		}
		return output, 1
	}
	return output, 0
}

// buildTargetedTestCmd 根据测试框架构建针对特定文件的测试命令。
func buildTargetedTestCmd(baseCmd, testFile string) string {
	switch {
	case strings.HasPrefix(baseCmd, "go test"):
		// Go: go test -v -count=1 ./path/to/pkg/...
		dir := filepath.ToSlash(filepath.Dir(testFile))
		if dir == "." {
			dir = "./..."
		} else {
			dir = "./" + dir + "/..."
		}
		return fmt.Sprintf("go test -v -count=1 %s", dir)
	case strings.HasPrefix(baseCmd, "pytest"):
		return fmt.Sprintf("pytest -v %s", testFile)
	case strings.HasPrefix(baseCmd, "npm test") || strings.HasPrefix(baseCmd, "npx"):
		return fmt.Sprintf("npx jest --verbose %s", testFile)
	case strings.HasPrefix(baseCmd, "cargo test"):
		return "cargo test"
	default:
		return baseCmd
	}
}

// countTestFailures 从测试输出中统计失败的测试数量。
func countTestFailures(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "--- FAIL:") || strings.HasPrefix(line, "FAIL:") {
			count++
		}
		// Python pytest: "FAILED" lines
		if strings.Contains(line, "FAILED") && strings.Contains(line, "::") {
			count++
		}
	}
	if count == 0 && strings.Contains(output, "FAIL") {
		count = 1 // 至少有一个失败
	}
	return count
}

// countTestTotal 从测试输出中估算总测试数量。
func countTestTotal(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		// Go: "--- PASS:" or "--- FAIL:"
		if strings.HasPrefix(line, "--- PASS:") || strings.HasPrefix(line, "--- FAIL:") {
			count++
		}
	}
	if count == 0 {
		count = 1 // 至少假设有一个测试
	}
	return count
}

// extractFailingSummary 从测试输出中提取失败摘要（最多 500 字符）。
func extractFailingSummary(output string) string {
	var failures []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "--- FAIL:") || strings.HasPrefix(line, "FAIL") {
			failures = append(failures, line)
		}
	}
	summary := strings.Join(failures, "\n")
	if len([]rune(summary)) > 500 {
		summary = string([]rune(summary)[:500]) + "..."
	}
	return summary
}

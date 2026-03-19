package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// TaskSplitter decomposes user requirements or task lists into executable
// SubTask slices. For Greenfield mode it calls an LLM; for Maintenance mode
// it parses manual text or fetches from external sources.
type TaskSplitter struct {
	llmConfig MaclawLLMConfig
}

// NewTaskSplitter creates a TaskSplitter with the given LLM configuration.
func NewTaskSplitter(cfg MaclawLLMConfig) *TaskSplitter {
	return &TaskSplitter{llmConfig: cfg}
}

// SplitRequirements uses the configured LLM to decompose product requirements
// into a list of SubTasks (Greenfield mode). Each task includes acceptance
// criteria derived from the requirements for spec-driven verification.
func (s *TaskSplitter) SplitRequirements(requirements, techStack string) ([]SubTask, error) {
	prompt := fmt.Sprintf(`You are a software architect. Decompose the following product requirements into independent development sub-tasks.
Each sub-task must include:
- description: what to implement
- expected_files: list of files that will be created or modified
- dependencies: indices of other tasks this depends on (use 0-based indexing)
- acceptance_criteria: list of specific, verifiable conditions that must be met for this task to be considered complete

Tech stack: %s

Requirements:
%s

Respond ONLY with a JSON array of objects with fields: description, expected_files, dependencies, acceptance_criteria.`, techStack, requirements)

	body, err := s.callLLM(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	var raw []struct {
		Description        string   `json:"description"`
		ExpectedFiles      []string `json:"expected_files"`
		Dependencies       []int    `json:"dependencies"`
		AcceptanceCriteria []string `json:"acceptance_criteria"`
	}
	if err := json.Unmarshal(extractJSON(body), &raw); err != nil {
		return nil, fmt.Errorf("parse LLM response: %w", err)
	}

	tasks := make([]SubTask, len(raw))
	for i, r := range raw {
		tasks[i] = SubTask{
			Index:              i,
			Description:        r.Description,
			ExpectedFiles:      r.ExpectedFiles,
			Dependencies:       r.Dependencies,
			AcceptanceCriteria: r.AcceptanceCriteria,
		}
	}
	return tasks, nil
}

// ParseTaskList parses a task list from various sources (Maintenance mode).
func (s *TaskSplitter) ParseTaskList(input TaskListInput) ([]SubTask, error) {
	switch input.Source {
	case "manual":
		return s.parseManualInput(input.Text)
	case "github":
		return s.parseGitHubIssues(input.URL)
	default:
		return nil, fmt.Errorf("unsupported task source: %s", input.Source)
	}
}

// parseManualInput splits text by newlines; each non-empty line becomes a SubTask.
func (s *TaskSplitter) parseManualInput(text string) ([]SubTask, error) {
	lines := strings.Split(text, "\n")
	var tasks []SubTask
	idx := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		tasks = append(tasks, SubTask{
			Index:         idx,
			Description:   line,
			ExpectedFiles: []string{}, // will be filled by LLM or conflict detector
		})
		idx++
	}
	return tasks, nil
}

// parseGitHubIssues fetches issues from a GitHub repo URL.
// Expected URL format: https://github.com/{owner}/{repo}
func (s *TaskSplitter) parseGitHubIssues(repoURL string) ([]SubTask, error) {
	// Extract owner/repo from URL
	repoURL = strings.TrimSuffix(repoURL, "/")
	parts := strings.Split(repoURL, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid GitHub URL: %s", repoURL)
	}
	owner := parts[len(parts)-2]
	repo := parts[len(parts)-1]

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues?state=open&per_page=50", owner, repo)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("fetch GitHub issues: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var issues []struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		return nil, fmt.Errorf("parse GitHub issues: %w", err)
	}

	tasks := make([]SubTask, len(issues))
	for i, issue := range issues {
		desc := issue.Title
		if issue.Body != "" {
			desc += "\n" + issue.Body
		}
		tasks[i] = SubTask{
			Index:         i,
			Description:   desc,
			ExpectedFiles: []string{},
		}
	}
	return tasks, nil
}

// callLLM sends a prompt to the configured LLM and returns the response text.
func (s *TaskSplitter) callLLM(prompt string) ([]byte, error) {
	return swarmCallLLM(s.llmConfig, prompt, 0.2, 120*time.Second)
}

// SplitViaAgent delegates task decomposition to a programming tool instance
// (e.g. Claude, Gemini) instead of calling the LLM API directly. This lets
// the tool analyze the actual project files and produce more accurate tasks.
// The orchestrator creates a session with the "architect" role, sends the
// requirements, and parses the structured output.
func (s *TaskSplitter) SplitViaAgent(agentOutput string) ([]SubTask, error) {
	if strings.TrimSpace(agentOutput) == "" {
		return nil, fmt.Errorf("agent output is empty")
	}

	// Try to parse structured JSON from the agent output first.
	jsonData := extractJSON([]byte(agentOutput))
	var raw []struct {
		Description        string   `json:"description"`
		ExpectedFiles      []string `json:"expected_files"`
		Dependencies       []int    `json:"dependencies"`
		AcceptanceCriteria []string `json:"acceptance_criteria"`
	}
	if err := json.Unmarshal(jsonData, &raw); err == nil && len(raw) > 0 {
		tasks := make([]SubTask, len(raw))
		for i, r := range raw {
			tasks[i] = SubTask{
				Index:              i,
				Description:        r.Description,
				ExpectedFiles:      r.ExpectedFiles,
				Dependencies:       r.Dependencies,
				AcceptanceCriteria: r.AcceptanceCriteria,
			}
		}
		return tasks, nil
	}

	// Fallback: if the agent output is not structured JSON, use LLM to
	// extract tasks from the free-form text.
	prompt := fmt.Sprintf(`以下是一个编程助手对项目需求的分析输出。请从中提取独立的开发子任务。

分析输出：
%s

请返回 JSON 数组，每个元素包含：description, expected_files, dependencies, acceptance_criteria。
acceptance_criteria 是一个字符串数组，列出该任务完成的验收条件。
只返回 JSON 数组，不要其他内容。`, agentOutput)

	body, err := s.callLLM(prompt)
	if err != nil {
		return nil, fmt.Errorf("parse agent output via LLM: %w", err)
	}

	if err := json.Unmarshal(extractJSON(body), &raw); err != nil {
		return nil, fmt.Errorf("parse extracted tasks: %w", err)
	}

	tasks := make([]SubTask, len(raw))
	for i, r := range raw {
		tasks[i] = SubTask{
			Index:              i,
			Description:        r.Description,
			ExpectedFiles:      r.ExpectedFiles,
			Dependencies:       r.Dependencies,
			AcceptanceCriteria: r.AcceptanceCriteria,
		}
	}
	return tasks, nil
}

// extractJSON tries to find a JSON array in the text (handles markdown fences).
func extractJSON(data []byte) []byte {
	s := string(data)
	// Strip markdown code fences
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
	// Find the first [ and last ]
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start >= 0 && end > start {
		return []byte(s[start : end+1])
	}
	return []byte(strings.TrimSpace(s))
}

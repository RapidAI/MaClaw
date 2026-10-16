package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func init() {
	agentservice.DesktopWeb = desktopBotWeb
}

// desktopRemoteFile is set by tests. Production asks Hub, which asks the
// Docker service that owns the user's desktop.
var desktopRemoteFile func(ctx context.Context, tenantID, userID, action, filePath, content, oldString, newString string) (string, error)

// desktopRemoteBash is set by tests. Production runs the command in the container.
var desktopRemoteBash func(ctx context.Context, tenantID, userID, command string) (string, error)

// desktopRemoteHTTP is set by tests. Production fetches through the container network.
var desktopRemoteHTTP func(ctx context.Context, tenantID, userID, rawURL string) (string, int, string, error)

func desktopBotWeb(ctx context.Context, tenantID, userID, kind, query, rawURL string, maxChars int) (string, error) {
	switch kind {
	case "search":
		return desktopBotSearch(ctx, tenantID, userID, query)
	case "fetch":
		return desktopBotFetch(ctx, tenantID, userID, rawURL, maxChars)
	default:
		return "", fmt.Errorf("desktop web action is invalid")
	}
}

func desktopBotSearch(ctx context.Context, tenantID, userID, query string) (string, error) {
	link, err := desktop.DuckDuckGoURL(query)
	if err != nil {
		return "", err
	}
	body, status, _, err := desktopContainerHTTP(ctx, tenantID, userID, link)
	if err != nil {
		return "", err
	}
	hits := desktop.ParseDuckDuckGo(body)
	if len(hits) == 0 {
		return fmt.Sprintf("Search %q returned no results from this desktop's network (HTTP %d).", query, status), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Search %q — %d results from this desktop's network:\n\n", query, len(hits))
	for i, hit := range hits {
		fmt.Fprintf(&b, "%d. %s\n   %s\n\n", i+1, hit.Title, hit.URL)
	}
	return b.String(), nil
}

func desktopBotFetch(ctx context.Context, tenantID, userID, rawURL string, maxChars int) (string, error) {
	rawURL, err := desktop.PublicURL(rawURL)
	if err != nil {
		return "", err
	}
	body, status, kind, err := desktopContainerHTTP(ctx, tenantID, userID, rawURL)
	if err != nil {
		return "", err
	}
	text := body
	if desktop.LooksLikeHTML(kind, body) {
		text = desktop.HTMLText(body)
	}
	if maxChars <= 0 {
		maxChars = 16384
	}
	if len(text) > maxChars {
		text, _ = desktop.CapHead(text, maxChars)
	}
	return fmt.Sprintf("URL: %s\nStatus: %d\n\n%s", rawURL, status, text), nil
}

func desktopContainerHTTP(ctx context.Context, tenantID, userID, rawURL string) (string, int, string, error) {
	if desktopRemoteHTTP != nil {
		return desktopRemoteHTTP(ctx, tenantID, userID, rawURL)
	}
	if client := desktopHubClientFromEnv(); client != nil {
		return client.HTTP(ctx, tenantID, userID, rawURL)
	}
	return "", 0, "", fmt.Errorf("this bot's desktop is the person's cloud desktop, and the connection is not configured")
}

func operateDesktopContainer(ctx context.Context, scope agentruntime.Scope, action string, args map[string]any) (string, error) {
	switch action {
	case "file_read", "file_write", "file_edit", "file_list":
		return desktopContainerFile(ctx, scope, action, args)
	case "bash":
		text, err := desktopContainerBash(ctx, scope, desktopArg(args, "command"))
		if err != nil {
			if strings.TrimSpace(text) == "" {
				return "", err
			}
			return "", fmt.Errorf("%s", strings.TrimSpace(text))
		}
		return text, nil
	default:
		return "", fmt.Errorf("desktop action is invalid")
	}
}

func desktopContainerFile(ctx context.Context, scope agentruntime.Scope, action string, args map[string]any) (string, error) {
	wire := strings.TrimPrefix(action, "file_")
	filePath := desktopArg(args, "path")
	if wire == "list" && filePath == "" {
		filePath = desktop.ContainerHome
	}
	resolved, err := desktop.ContainerPath(filePath)
	if err != nil {
		return "", fmt.Errorf("desktop %s rejected the path: %w", action, err)
	}
	content := desktopRawArg(args, "content")
	oldString := desktopRawArg(args, "old_string")
	newString := desktopRawArg(args, "new_string")
	if wire == "write" {
		if _, ok := args["content"].(string); !ok {
			return "", fmt.Errorf("desktop file_write requires content")
		}
		if len(content) > desktop.FileContentMax {
			return "", fmt.Errorf("desktop file is too large")
		}
	}
	if wire == "edit" {
		if oldString == "" {
			return "", fmt.Errorf("desktop file_edit requires old_string")
		}
	}
	if desktopRemoteFile != nil {
		return desktopRemoteFile(ctx, scope.TenantID, scope.UserID, wire, resolved, content, oldString, newString)
	}
	if client := desktopHubClientFromEnv(); client != nil {
		return client.File(ctx, scope.TenantID, scope.UserID, wire, resolved, content, oldString, newString)
	}
	if err := refuseLocalDesktop(ctx); err != nil {
		return "", err
	}
	return desktopLocalFile(ctx, wire, resolved, content, oldString, newString)
}

func desktopContainerBash(ctx context.Context, scope agentruntime.Scope, command string) (string, error) {
	if _, err := desktop.BashArgs(desktopContainerName(), command); err != nil {
		return "", fmt.Errorf("desktop bash rejected the command: %w", err)
	}
	if desktopRemoteBash != nil {
		return desktopRemoteBash(ctx, scope.TenantID, scope.UserID, strings.TrimSpace(command))
	}
	if client := desktopHubClientFromEnv(); client != nil {
		return client.Bash(ctx, scope.TenantID, scope.UserID, strings.TrimSpace(command))
	}
	if err := refuseLocalDesktop(ctx); err != nil {
		return "", err
	}
	return desktopLocalBash(ctx, command)
}

func desktopLocalFile(ctx context.Context, action, filePath, content, oldString, newString string) (string, error) {
	container := desktopContainerName()
	switch action {
	case "read":
		return desktopLocalOutput(ctx, desktop.FileReadArgs(container, filePath, desktop.FileReadMax), nil)
	case "bytes":
		return desktopLocalOutput(ctx, desktop.FileBytesArgs(container, filePath, desktop.FileBytesMax), nil)
	case "list":
		return desktopLocalOutput(ctx, desktop.FileListArgs(container, filePath), nil)
	case "write":
		text, err := desktopLocalOutput(ctx, desktop.FileWriteArgs(container, filePath), strings.NewReader(content))
		if err != nil {
			return text, err
		}
		return fmt.Sprintf("Wrote %s (%d bytes).", filePath, len(content)), nil
	case "edit":
		current, err := desktopLocalOutput(ctx, desktop.FileReadArgs(container, filePath, desktop.FileContentMax), nil)
		if err != nil {
			return current, err
		}
		if len(current) > desktop.FileContentMax {
			return "", fmt.Errorf("desktop file is too large to edit")
		}
		next, err := desktop.ApplyEdit(current, oldString, newString)
		if err != nil {
			return "", err
		}
		if _, err := desktopLocalOutput(ctx, desktop.FileWriteArgs(container, filePath), strings.NewReader(next)); err != nil {
			return "", err
		}
		return fmt.Sprintf("Updated %s.", filePath), nil
	default:
		return "", fmt.Errorf("desktop file action is invalid")
	}
}

func desktopLocalBash(ctx context.Context, command string) (string, error) {
	args, err := desktop.BashArgs(desktopContainerName(), command)
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, desktop.BashBudget)
	defer cancel()
	text, exitCode, err := desktopLocalCommand(ctx, args)
	if err != nil {
		if ctx.Err() != nil {
			return desktop.ProgramOutput(text, exitCode, true), nil
		}
		return "", err
	}
	return desktop.ProgramOutput(text, exitCode, false), nil
}

func desktopLocalCommand(ctx context.Context, args []string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err == nil {
		return text, 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return text, exitErr.ExitCode(), nil
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		trimmed = err.Error()
	}
	return trimmed, 0, fmt.Errorf("%s", trimmed)
}

func desktopLocalOutput(ctx context.Context, args []string, stdin *strings.Reader) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			trimmed = err.Error()
		}
		return trimmed, fmt.Errorf("%s", trimmed)
	}
	return text, nil
}

func desktopRawArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, _ := args[key].(string)
	return value
}

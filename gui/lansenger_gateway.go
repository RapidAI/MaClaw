package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

const (
	lansengerChannelID = "lansenger"
)

type lansengerStatusPayload struct {
	Status string `json:"status"`
	Output string `json:"output,omitempty"`
}

func defaultLansengerPluginSpec() string {
	return corelib.DefaultLansengerPluginSpec()
}

func (a *App) GetLansengerStatus() string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return "error"
	}
	if !cfg.LansengerEnabled {
		return "disabled"
	}
	return a.probeLansengerStatus(cfg)
}

func (a *App) InstallLansengerPlugin() map[string]string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return map[string]string{"error": "无法加载配置: " + err.Error()}
	}
	pluginSpec := cfg.EffectiveLansengerPluginSpec()
	if pluginSpec == "" {
		pluginSpec = defaultLansengerPluginSpec()
	}
	result, err := a.runLansengerCommand(90*time.Second, false, "openclaw", "plugins", "install", pluginSpec)
	if err != nil {
		combined := result
		if strings.Contains(strings.ToLower(combined), "already exists") || strings.Contains(strings.ToLower(combined), "already installed") {
			updateOut, updateErr := a.runLansengerCommand(90*time.Second, false, "openclaw", "plugins", "update", lansengerChannelID)
			if updateErr != nil {
				return map[string]string{
					"status": "error",
					"error":  buildLansengerError("插件已存在，但更新失败", updateOut, updateErr),
				}
			}
			cfg.LansengerEnabled = true
			if strings.TrimSpace(cfg.LansengerPluginSpec) == "" {
				cfg.LansengerPluginSpec = pluginSpec
			}
			if cfg.LansengerLocalMode == nil {
				cfg.SetLansengerLocal(true)
			}
			if saveErr := a.SaveConfig(cfg); saveErr != nil {
				return map[string]string{"status": "error", "error": "插件更新成功，但保存配置失败: " + saveErr.Error()}
			}
			a.emitEvent("lansenger-status-changed", lansengerStatusPayload{Status: a.probeLansengerStatus(cfg), Output: updateOut})
			return map[string]string{"status": "updated", "output": updateOut}
		}
		return map[string]string{
			"status": "error",
			"error":  buildLansengerError("插件安装失败", combined, err),
		}
	}
	cfg.LansengerEnabled = true
	cfg.LansengerPluginSpec = pluginSpec
	if cfg.LansengerLocalMode == nil {
		cfg.SetLansengerLocal(true)
	}
	if err := a.SaveConfig(cfg); err != nil {
		return map[string]string{"status": "error", "error": "插件安装成功，但保存配置失败: " + err.Error()}
	}
	status := a.probeLansengerStatus(cfg)
	a.emitEvent("lansenger-status-changed", lansengerStatusPayload{Status: status, Output: result})
	return map[string]string{"status": "installed", "output": result}
}

func (a *App) LoginLansenger() map[string]string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return map[string]string{"error": "无法加载配置: " + err.Error()}
	}
	if !cfg.LansengerEnabled {
		return map[string]string{"error": "请先安装蓝信插件"}
	}
	output, err := a.runLansengerCommand(8*time.Minute, true, "openclaw", "channels", "login", "--channel", lansengerChannelID)
	if err != nil {
		return map[string]string{"status": "error", "error": buildLansengerError("蓝信登录失败", output, err)}
	}
	status := a.probeLansengerStatus(cfg)
	a.emitEvent("lansenger-status-changed", lansengerStatusPayload{Status: status, Output: output})
	return map[string]string{"status": status, "output": output}
}

func (a *App) RestartLansenger() map[string]string {
	cfg, err := a.LoadConfig()
	if err != nil {
		return map[string]string{"error": "无法加载配置: " + err.Error()}
	}
	if !cfg.LansengerEnabled {
		return map[string]string{"error": "请先安装蓝信插件"}
	}
	output, err := a.runLansengerCommand(45*time.Second, false, "openclaw", "gateway", "restart")
	if err != nil {
		return map[string]string{"status": "error", "error": buildLansengerError("重启 OpenClaw Gateway 失败", output, err)}
	}
	status := a.probeLansengerStatus(cfg)
	a.emitEvent("lansenger-status-changed", lansengerStatusPayload{Status: status, Output: output})
	return map[string]string{"status": status, "output": output}
}

func (a *App) GetLansengerLocalMode() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true
	}
	return cfg.IsLansengerLocalMode()
}

func (a *App) SetLansengerLocalMode(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	if !enabled && cfg.RemoteMachineID == "" {
		return fmt.Errorf("请先注册到 Hub（设置 Hub 地址并完成注册），再开启多机模式")
	}
	cfg.SetLansengerLocal(enabled)
	return a.SaveConfig(cfg)
}

func (a *App) probeLansengerStatus(cfg AppConfig) string {
	if !cfg.LansengerEnabled {
		return "disabled"
	}
	output, err := a.runLansengerCommand(20*time.Second, false, "openclaw", "channels", "login", "--channel", lansengerChannelID, "--help")
	if err != nil {
		text := strings.ToLower(output + "\n" + err.Error())
		switch {
		case strings.Contains(text, "not found"), strings.Contains(text, "无法将"):
			return "missing_openclaw"
		case strings.Contains(text, "unknown command"), strings.Contains(text, "unknown flag"):
			return "installed"
		default:
			return "ready"
		}
	}
	if strings.TrimSpace(output) != "" {
		return "ready"
	}
	return "installed"
}

func buildLansengerError(prefix, output string, err error) string {
	parts := []string{prefix}
	if err != nil {
		parts = append(parts, err.Error())
	}
	trimmed := strings.TrimSpace(output)
	if trimmed != "" {
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, "\n")
}

func (a *App) runLansengerCommand(timeout time.Duration, interactive bool, name string, args ...string) (string, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if interactive {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		cmd.Stdin = nil
	} else {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}
	err := cmd.Run()
	combined := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderr.String()))
	if ctx.Err() == context.DeadlineExceeded {
		if combined == "" {
			combined = "command timed out"
		}
		return combined, fmt.Errorf("command timed out")
	}
	return strings.TrimSpace(combined), err
}

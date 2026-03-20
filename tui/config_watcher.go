package main

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	tea "github.com/charmbracelet/bubbletea"
)

// configChangedMsg 配置文件变更消息。
type configChangedMsg struct{}

// ConfigWatcher 监听 ~/.maclaw/config.json 的变更。
type ConfigWatcher struct {
	watcher  *fsnotify.Watcher
	program  *tea.Program
	logger   Logger
	mu       sync.Mutex
	stopped  bool
}

// Logger 是 ConfigWatcher 使用的日志接口（与 TUILogger 兼容）。
type Logger interface {
	Info(format string, args ...interface{})
	Error(format string, args ...interface{})
}

// NewConfigWatcher 创建配置文件监听器。
func NewConfigWatcher(logger Logger) (*ConfigWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &ConfigWatcher{watcher: w, logger: logger}, nil
}

// SetProgram 绑定 Bubble Tea Program 以发送消息。
func (cw *ConfigWatcher) SetProgram(p *tea.Program) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.program = p
}

// Start 开始监听配置文件。
func (cw *ConfigWatcher) Start() {
	configPath := resolveConfigPath()
	if err := cw.watcher.Add(configPath); err != nil {
		if cw.logger != nil {
			cw.logger.Error("config watcher: 无法监听 %s: %v", configPath, err)
		}
		return
	}
	if cw.logger != nil {
		cw.logger.Info("config watcher: 监听 %s", configPath)
	}
	go cw.loop()
}

// Stop 停止监听。
func (cw *ConfigWatcher) Stop() {
	cw.mu.Lock()
	cw.stopped = true
	cw.mu.Unlock()
	_ = cw.watcher.Close()
}

func (cw *ConfigWatcher) loop() {
	// 防抖：500ms 内多次写入只触发一次
	var debounceTimer *time.Timer
	for {
		select {
		case event, ok := <-cw.watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(500*time.Millisecond, func() {
					cw.mu.Lock()
					p := cw.program
					stopped := cw.stopped
					cw.mu.Unlock()
					if stopped {
						return
					}
					if p != nil {
						p.Send(configChangedMsg{})
					}
					if cw.logger != nil {
						cw.logger.Info("config watcher: 检测到配置变更")
					}
				})
			}
		case err, ok := <-cw.watcher.Errors:
			if !ok {
				return
			}
			if cw.logger != nil {
				cw.logger.Error("config watcher error: %v", err)
			}
		}
	}
}

func resolveConfigPath() string {
	dataDir := os.Getenv("MACLAW_DATA_DIR")
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".maclaw")
	}
	return filepath.Join(dataDir, "config.json")
}

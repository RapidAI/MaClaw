package corelib

import (
	"context"
	"errors"
	"path/filepath"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/tool"
	"golang.org/x/sync/errgroup"
)

// Kernel 是 MaClaw 内核的顶层入口，持有所有子系统的引用。
// GUI 和 TUI 各自创建一个 Kernel 实例来驱动业务逻辑。
type Kernel struct {
	opts     KernelOptions
	emitter  EventEmitter
	logger   Logger
	platform PlatformCapabilities

	// 子系统（导出字段，供上层 GUI/TUI 访问）
	// 注意：config、misc 等包因循环依赖不在此直接持有，由上层注入管理。
	ToolRegistry *tool.Registry
	ToolRouter   *tool.Router
	Scheduler    *scheduler.Manager

	// 内部状态
	initialized  bool
	shutdownOnce sync.Once
}

// NewKernel 创建并初始化内核实例。
// 所有子系统在此函数中按依赖顺序初始化。
func NewKernel(opts KernelOptions) (*Kernel, error) {
	k := &Kernel{
		opts: opts,
	}

	// 初始化 Logger
	if opts.Logger != nil {
		k.logger = opts.Logger
	} else {
		k.logger = NewDefaultLogger()
	}

	// 初始化 EventEmitter
	if opts.EventEmitter != nil {
		k.emitter = opts.EventEmitter
	} else {
		k.emitter = NoopEmitter{}
	}

	// 检测平台能力
	if opts.PlatformOverride != nil {
		k.platform = *opts.PlatformOverride
	} else {
		k.platform = DetectPlatform()
	}

	k.logger.Info("kernel initializing: os=%s arch=%s headless=%v",
		k.platform.OSName(), k.platform.Arch(), k.IsHeadless())

	// --- 按依赖顺序初始化子系统 ---

	dataDir := opts.DataDir
	if dataDir == "" {
		dataDir = "."
	}

	// 1. 配置管理 — 由上层注入（避免循环依赖 corelib ↔ corelib/config）
	// 2. misc（SharedContext/ContextBridge）— 由上层注入（corelib/misc → corelib/remote → corelib 循环）

	// 3. 工具注册表 & 路由
	k.ToolRegistry = tool.NewRegistry()
	k.ToolRouter = tool.NewRouter(nil) // DefinitionGenerator 由上层注入

	// 4. 定时任务调度器
	schedulerPath := filepath.Join(dataDir, "scheduled_tasks.json")
	sched, err := scheduler.NewManager(schedulerPath)
	if err != nil {
		k.logger.Warn("scheduler init failed (non-fatal): %v", err)
	} else {
		k.Scheduler = sched
	}

	k.initialized = true
	k.logger.Info("kernel initialized")
	return k, nil
}

// Run 启动内核事件循环，阻塞直到 ctx 被取消。
// 内部使用 errgroup 启动所有后台子系统。
func (k *Kernel) Run(ctx context.Context) error {
	if !k.initialized {
		return errors.New("kernel not initialized")
	}

	k.logger.Info("kernel starting event loop")

	g, gctx := errgroup.WithContext(ctx)

	// 定时任务调度器
	if k.Scheduler != nil {
		k.Scheduler.Start()
		g.Go(func() error {
			<-gctx.Done()
			k.Scheduler.Stop()
			return nil
		})
	}

	// 等待 ctx 取消
	g.Go(func() error {
		<-gctx.Done()
		return gctx.Err()
	})

	err := g.Wait()
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Shutdown 优雅关闭内核及所有子系统。
func (k *Kernel) Shutdown(ctx context.Context) error {
	var shutdownErr error
	k.shutdownOnce.Do(func() {
		k.logger.Info("kernel shutting down")

		// 按逆序关闭子系统
		if k.Scheduler != nil {
			k.Scheduler.Stop()
		}

		k.logger.Info("kernel shutdown complete")
	})
	return shutdownErr
}

// IsHeadless 返回当前是否运行在无头环境中。
func (k *Kernel) IsHeadless() bool {
	return !k.platform.HasDisplay()
}

// OnEvent 订阅内核事件。
func (k *Kernel) OnEvent(eventType string, handler EventHandler) {
	k.emitter.Subscribe(eventType, handler)
}

// Logger 返回内核的日志实例。
func (k *Kernel) Logger() Logger {
	return k.logger
}

// Emitter 返回内核的事件分发器。
func (k *Kernel) Emitter() EventEmitter {
	return k.emitter
}

// Platform 返回平台能力信息。
func (k *Kernel) Platform() *PlatformCapabilities {
	return &k.platform
}

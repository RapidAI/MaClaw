# 实施计划：OEM 品牌机制

## 概述

基于编译时 Go build tag 实现 OEM 品牌切换机制。核心思路：在 `corelib/brand/` 包中定义品牌配置类型和解析函数，通过 `//go:build` 条件编译选择品牌变体，各组件（GUI/TUI/Hub/Mobile）统一引用 `brand.Current()` 获取当前品牌信息。第一个 OEM 品牌为奇爪（TigerClaw，BrandID=qianxin）。

## 任务列表

- [x] 1. 创建 `corelib/brand` 核心包
  - [x] 1.1 创建 `corelib/brand/brand.go`：定义 `BrandConfig` 结构体、`ExtraToolDef` 结构体、包级变量 `currentBrand`、`Current()` 函数、`IsDefault()` 函数
    - `BrandConfig` 包含 ID、DisplayName、DisplayNameCN、WindowTitle、TrayTooltip、IconPath、IcnsPath、IcoPath、MobileAppName、ExtraTools 字段
    - `ExtraToolDef` 包含 Name、DisplayName、ConfigKey、EnvBuilderFunc、OnboardingFunc 字段
    - `Current()` 返回 `currentBrand`，`IsDefault()` 检查 `currentBrand.ID == "maclaw"`
    - 新增 `RegisterExtraTools(registry)` 函数：遍历 `Current().ExtraTools` 注册到工具注册表，检测名称冲突返回 error
    - _需求: 1.1, 1.2, 1.3, 1.4, 7.6_

  - [x] 1.2 创建 `corelib/brand/brand_default.go`：`//go:build !oem_qianxin`，在 `init()` 中设置默认品牌 MaClaw 配置
    - DisplayName="MaClaw"、WindowTitle="MaClaw"、TrayTooltip="MaClaw Dashboard"、ExtraTools=nil（初始化为空切片 `[]ExtraToolDef{}`）
    - _需求: 1.3, 2.2, 9.1_

  - [x] 1.3 创建 `corelib/brand/brand_qianxin.go`：`//go:build oem_qianxin`，在 `init()` 中设置奇爪品牌配置
    - ID="qianxin"、DisplayName="TigerClaw"、DisplayNameCN="奇爪"、WindowTitle="TigerClaw"
    - ExtraTools 包含 `{Name:"tigerclaw", DisplayName:"TigerClaw Code", ConfigKey:"tigerclaw"}`
    - _需求: 2.1, 7.1_

  - [ ]* 1.4 编写 `corelib/brand/brand_test.go` 单元测试
    - 测试默认品牌下 `Current()` 返回 DisplayName="MaClaw"、ExtraTools 为空切片
    - 测试 `IsDefault()` 在默认品牌下返回 true
    - _需求: 1.3, 2.2, 9.1_

  - [ ]* 1.5 编写属性测试：Property 1 — 品牌解析器始终返回有效配置
    - **Property 1: Brand resolver always returns a valid configuration**
    - **验证: 需求 2.3, 1.3**
    - 使用 `pgregory.net/rapid` 生成随机 BrandConfig，验证 Current() 返回值的 ID、DisplayName、WindowTitle、TrayTooltip 非空，ExtraTools 非 nil

  - [ ]* 1.6 编写属性测试：Property 5 — 工具名冲突检测
    - **Property 5: Tool name conflict detection**
    - **验证: 需求 7.6**
    - 从内置工具名列表（claude/codex/gemini 等）随机选取一个作为 ExtraToolDef.Name，调用 RegisterExtraTools，验证返回 error

- [x] 2. 检查点 — 确保核心包测试通过
  - 确保所有测试通过，如有疑问请询问用户。

- [x] 3. GUI 桌面应用品牌集成
  - [x] 3.1 修改 `gui/main.go`：将硬编码的 `Title: "MaClaw"` 替换为 `brand.Current().WindowTitle`
    - 导入 `github.com/RapidAI/CodeClaw/corelib/brand`
    - _需求: 3.1, 3.5_

  - [x] 3.2 修改 `gui/common.go`：`trayTranslations` 中的 "title" 和 "quit" 使用 `brand.Current()` 的品牌名替代硬编码的 "MaClaw"
    - en: title → brand.Current().TrayTooltip, quit → "Quit " + brand.Current().DisplayName
    - zh-Hans: title → brand.Current().DisplayNameCN + " 控制台"
    - _需求: 3.3, 3.5_

  - [x] 3.3 修改 `gui/tray_windows.go`：将 `systray.SetTitle("MaClaw")` 和 `systray.SetTooltip("MaClaw Dashboard")` 替换为品牌配置值
    - _需求: 3.3_

  - [x] 3.4 修改 `gui/tray_darwin.go`：将 `systray.SetTooltip("MaClaw Dashboard")` 替换为 `brand.Current().TrayTooltip`
    - _需求: 3.3_

  - [x] 3.5 创建 OEM 图标资源覆盖文件
    - 修改 `gui/resources_darwin.go` 添加 `//go:build !oem_qianxin` 约束
    - 修改 `gui/resources_linux.go` 添加 `//go:build !oem_qianxin` 约束
    - 修改 `gui/resources_windows.go` 添加 `//go:build !oem_qianxin` 约束
    - 创建 `gui/resources_oem_qianxin_darwin.go`（`//go:build oem_qianxin && darwin`）embed `assets/qianxin.png`
    - 创建 `gui/resources_oem_qianxin_linux.go`（`//go:build oem_qianxin && linux`）embed `assets/qianxin.png`
    - 创建 `gui/resources_oem_qianxin_windows.go`（`//go:build oem_qianxin && windows`）embed `assets/qianxin.ico`（如不存在则 embed `assets/qianxin.png`）
    - _需求: 3.2, 3.4_

- [x] 4. TUI 终端应用品牌集成
  - [x] 4.1 修改 `tui/main.go`：`--version` 输出使用 `brand.Current().DisplayName` 替代 "maclaw-tui"，`--help` 和 `printUsage()` 中使用品牌名
    - launch 命令的支持工具列表动态包含 `brand.Current().ExtraTools` 中的工具名
    - _需求: 4.1, 4.2, 7.4_

  - [x] 4.2 修改 `tui/tool_launch_env.go`：`buildToolEnv` 和 `normalizeToolName` 支持 OEM 额外工具
    - 在 `buildToolEnv` 的 switch 中添加 default 分支：检查 `brand.Current().ExtraTools`，如果匹配则调用 `ExtraToolDef.EnvBuilderFunc`（若为 nil 则使用通用 OpenAI 兼容环境变量构建）
    - 在 `toolConfigFromApp` 中添加对额外工具 ConfigKey 的支持
    - _需求: 7.2, 7.4_

  - [x] 4.3 修改 `tui/tool_launcher.go`：`ensureTUIToolOnboarding` 支持 OEM 额外工具的 OnboardingFunc
    - _需求: 7.2_

  - [ ]* 4.4 编写属性测试：Property 2 — CLI 输出反映品牌配置
    - **Property 2: CLI output reflects brand configuration**
    - **验证: 需求 4.1, 4.2, 7.4**
    - 生成随机 BrandConfig（含随机 ExtraTools），调用版本/帮助格式化函数，验证输出包含品牌名和工具名

- [x] 5. Hub 服务端品牌集成
  - [x] 5.1 修改 Hub 设备注册响应：在 "brand" 字段中返回 `brand.Current().DisplayName`
    - 查找 Hub 中设备注册/enrollment 相关的 handler，添加 brand 字段
    - _需求: 6.1_

  - [x] 5.2 修改 Hub 管理页面标题：使用 `brand.Current().DisplayName`
    - 修改 `hub/web/admin/index.html` 中的标题
    - _需求: 6.2_

  - [x] 5.3 修改 Hub 设备能力上报：包含 OEM 额外工具名称
    - _需求: 7.5_

  - [ ]* 5.4 编写属性测试：Property 3 — Hub 响应反映品牌配置
    - **Property 3: Hub responses reflect brand configuration**
    - **验证: 需求 6.1, 7.5**
    - 生成随机 BrandConfig，构建 hub 响应，验证 brand 字段和工具列表

- [x] 6. 检查点 — 确保 GUI/TUI/Hub 集成测试通过
  - 确保所有测试通过，如有疑问请询问用户。

- [x] 7. 额外工具注入机制
  - [x] 7.1 修改 GUI 工具选择界面：在工具列表中动态包含 `brand.Current().ExtraTools`
    - 修改 `gui/app.go` 中 `LaunchTool` 相关逻辑，支持额外工具的环境变量构建和启动
    - _需求: 7.3_

  - [x] 7.2 修改 GUI 工具注册：在 `gui/app.go` 的 `initRemoteInfra` 或工具初始化流程中调用 `brand.RegisterExtraTools`
    - _需求: 7.2_

  - [ ]* 7.3 编写属性测试：Property 4 — 额外工具注册到工具注册表
    - **Property 4: Extra tools are registered in tool registry**
    - **验证: 需求 7.2**
    - 生成随机 ExtraToolDef 列表（名称不与内置工具冲突），注册后验证 registry 包含所有工具

- [x] 8. 配置兼容性
  - [x] 8.1 确保 `corelib.AppConfig` 的 JSON 反序列化忽略未知品牌工具键
    - 验证当配置文件包含 "tigerclaw" 键但当前品牌为默认品牌时，加载不报错
    - _需求: 9.4, 9.2, 9.3_

  - [ ]* 8.2 编写属性测试：Property 6 — 配置加载忽略未知品牌工具键
    - **Property 6: Config loading ignores unknown brand tool keys**
    - **验证: 需求 9.4**
    - 生成随机 JSON 配置（包含随机额外键），验证反序列化成功

- [x] 9. CI/CD 构建流水线品牌支持
  - [x] 9.1 修改 `.github/workflows/main.yml`：新增 `BRAND` 输入参数
    - 默认为空（Default_Brand）
    - 非空时在 `go build` 命令中添加 `-tags oem_${BRAND}`
    - 非空时使用品牌对应的图标资源
    - 非空时产物文件名包含品牌标识
    - macOS Info.plist 中 CFBundleName 使用品牌 DisplayName
    - _需求: 8.1, 8.2, 8.3, 8.4, 8.5_

- [x] 10. 最终检查点 — 确保所有测试通过
  - 确保所有测试通过，如有疑问请询问用户。

## 备注

- 标记 `*` 的任务为可选任务，可跳过以加速 MVP 交付
- 每个任务引用了具体的需求编号，确保可追溯性
- 属性测试使用 `pgregory.net/rapid` 库，验证设计文档中定义的正确性属性
- 检查点任务确保增量验证
- Mobile（Flutter）端的品牌替换（需求 5）需要修改 Dart 代码，不在 Go 编译时 build tag 范围内，需通过构建脚本或 Flutter 环境变量注入品牌信息

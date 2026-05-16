# Requirements Document

## Introduction

本文档定义了 MaClaw/CodeClaw 项目的 OEM 品牌机制需求。OEM 机制允许在同一代码库上构建多个品牌变体，差异化范围严格限定为两个维度：

1. **产品名称和 Logo**：包括应用标题、系统托盘、图标、移动端品牌文字等所有用户可见的品牌标识
2. **额外工具注入**：OEM 品牌可以注册一个或多个额外的编程工具（如自定义 IDE 集成），这些工具在默认品牌中不存在

该机制通过编译时 build tag 或配置文件实现品牌切换，不引入运行时性能开销。

## Glossary

- **OEM_Brand_Registry**: 品牌注册表，存储所有品牌变体的元数据（名称、Logo 路径、额外工具列表）的中心化数据结构
- **Brand_Resolver**: 品牌解析器，在应用启动时根据 build tag 或配置确定当前激活的品牌标识
- **Tool_Registry**: 工具注册表，管理所有可用编程工具（claude、codex、gemini 等）的注册和发现
- **Brand_Config**: 品牌配置结构体，包含产品名称、Logo 资源路径、额外工具定义等字段
- **GUI_App**: 基于 Wails/React 的桌面 GUI 应用（gui/ 目录）
- **TUI_App**: 基于终端的 TUI 应用（tui/ 目录）
- **Mobile_App**: 基于 Flutter 的移动端应用（mobile/chat/ 目录）
- **Hub_Server**: 服务端组件（hub/ 目录），负责会话管理和设备通信
- **Build_Pipeline**: GitHub Actions CI/CD 构建流水线，负责多平台产物生成
- **Default_Brand**: 默认品牌，即当前的 "MaClaw" 品牌标识

## Requirements

### Requirement 1: 品牌注册表定义

**User Story:** 作为 OEM 合作方，我希望在代码库中声明一个品牌变体，以便构建出带有自定义名称和 Logo 的产品。

#### Acceptance Criteria

1. THE OEM_Brand_Registry SHALL 提供一个结构化的 Brand_Config 类型，包含以下字段：品牌标识符（BrandID）、产品显示名称（DisplayName）、应用窗口标题（WindowTitle）、系统托盘提示文字（TrayTooltip）、桌面图标资源路径（IconPath）、macOS icns 资源路径（IcnsPath）、Windows ico 资源路径（IcoPath）、移动端应用名称（MobileAppName）、额外工具列表（ExtraTools）
2. THE OEM_Brand_Registry SHALL 在 corelib/ 包中定义，使 gui/、tui/、hub/、mobile/chat/ 各组件均可引用
3. WHEN 未指定任何 OEM 品牌时，THE Brand_Resolver SHALL 返回 Default_Brand（DisplayName="MaClaw"，无额外工具）
4. THE OEM_Brand_Registry SHALL 保证每个 BrandID 在注册表中唯一

### Requirement 2: 编译时品牌选择

**User Story:** 作为构建工程师，我希望通过 Go build tag 选择目标品牌，以便在同一代码库上生成不同品牌的二进制产物。

#### Acceptance Criteria

1. WHEN 使用 `-tags oem_xxx` 编译时，THE Brand_Resolver SHALL 激活 BrandID 为 "xxx" 的品牌配置
2. WHEN 未指定任何 OEM build tag 时，THE Brand_Resolver SHALL 激活 Default_Brand
3. THE Brand_Resolver SHALL 在应用启动阶段（init 或 main 函数入口）完成品牌解析，后续代码通过函数调用获取当前品牌配置
4. IF 指定的 build tag 对应的 BrandID 未在 OEM_Brand_Registry 中注册，THEN THE Brand_Resolver SHALL 在编译时产生明确的错误信息

### Requirement 3: GUI 桌面应用品牌替换

**User Story:** 作为最终用户，我希望 OEM 版本的桌面应用显示 OEM 品牌的名称和图标，以便获得一致的品牌体验。

#### Acceptance Criteria

1. THE GUI_App SHALL 使用 Brand_Config.WindowTitle 作为 Wails 应用窗口标题（替代硬编码的 "MaClaw"）
2. THE GUI_App SHALL 使用 Brand_Config.IconPath 指向的图标资源作为应用图标
3. THE GUI_App SHALL 使用 Brand_Config.TrayTooltip 作为系统托盘的提示文字
4. THE GUI_App 前端 SHALL 使用品牌配置提供的 Logo 图片替代 appicon.png
5. WHEN Brand_Config.DisplayName 与 "MaClaw" 不同时，THE GUI_App SHALL 在所有用户可见的 UI 文本中使用 Brand_Config.DisplayName 替代 "MaClaw"

### Requirement 4: TUI 终端应用品牌替换

**User Story:** 作为终端用户，我希望 OEM 版本的 TUI 应用显示 OEM 品牌名称，以便在帮助信息和版本输出中看到正确的品牌。

#### Acceptance Criteria

1. THE TUI_App SHALL 在 `--version` 输出中使用 Brand_Config.DisplayName 替代 "maclaw-tui"
2. THE TUI_App SHALL 在 `--help` 输出和用法说明中使用 Brand_Config.DisplayName
3. THE TUI_App SHALL 在系统托盘标题（Linux/Windows）中使用 Brand_Config.TrayTooltip

### Requirement 5: 移动端应用品牌替换

**User Story:** 作为移动端用户，我希望 OEM 版本的移动应用显示 OEM 品牌名称，以便在登录页和应用标题中看到正确的品牌。

#### Acceptance Criteria

1. THE Mobile_App SHALL 使用 Brand_Config.MobileAppName 作为 MaterialApp 的 title 属性
2. THE Mobile_App SHALL 在登录页面显示 Brand_Config.MobileAppName 替代 "MaClaw Chat"
3. WHEN 品牌配置提供了移动端 Logo 资源路径时，THE Mobile_App SHALL 使用该 Logo 替代默认图标

### Requirement 6: Hub 服务端品牌标识

**User Story:** 作为 Hub 管理员，我希望 Hub 服务端在设备注册和 API 响应中返回正确的 OEM 品牌名称。

#### Acceptance Criteria

1. THE Hub_Server SHALL 在设备注册响应的 "brand" 字段中返回 Brand_Config.DisplayName
2. THE Hub_Server SHALL 在管理页面标题中使用 Brand_Config.DisplayName

### Requirement 7: OEM 额外工具注入

**User Story:** 作为 OEM 合作方，我希望在品牌变体中添加一个额外的编程工具，以便 OEM 用户可以使用该工具进行开发。

#### Acceptance Criteria

1. THE OEM_Brand_Registry SHALL 允许在 Brand_Config.ExtraTools 中声明额外的工具定义，每个工具包含：工具名称（Name）、显示名称（DisplayName）、环境变量构建函数引用（EnvBuilderRef）、工具配置键名（ConfigKey）
2. WHEN 当前品牌包含 ExtraTools 时，THE Tool_Registry SHALL 在启动时自动注册这些额外工具
3. WHEN 当前品牌包含 ExtraTools 时，THE GUI_App SHALL 在工具选择界面中显示这些额外工具
4. WHEN 当前品牌包含 ExtraTools 时，THE TUI_App SHALL 在 `launch` 命令的支持工具列表中包含这些额外工具
5. WHEN 当前品牌包含 ExtraTools 时，THE Hub_Server SHALL 在设备能力上报中包含这些额外工具名称
6. IF 额外工具的 Name 与已有内置工具名称冲突，THEN THE OEM_Brand_Registry SHALL 在注册时返回错误

### Requirement 8: 构建流水线品牌支持

**User Story:** 作为构建工程师，我希望 CI/CD 流水线能够根据品牌参数生成对应品牌的安装包和产物。

#### Acceptance Criteria

1. THE Build_Pipeline SHALL 接受一个 `BRAND` 参数（默认为空，表示 Default_Brand）
2. WHEN `BRAND` 参数非空时，THE Build_Pipeline SHALL 在 `go build` 命令中添加 `-tags oem_${BRAND}` 标志
3. WHEN `BRAND` 参数非空时，THE Build_Pipeline SHALL 使用品牌对应的图标资源替代默认的 appicon.png、icon.ico、AppIcon.icns
4. WHEN `BRAND` 参数非空时，THE Build_Pipeline SHALL 在产物文件名中包含品牌标识（如 `BrandName-Windows-Portable.zip`）
5. THE Build_Pipeline SHALL 在 macOS .app bundle 的 Info.plist 中使用 Brand_Config.DisplayName 作为 CFBundleName

### Requirement 9: 品牌配置的向后兼容性

**User Story:** 作为开发者，我希望 OEM 机制不影响现有的默认品牌构建和功能。

#### Acceptance Criteria

1. WHEN 不使用任何 OEM build tag 编译时，THE Brand_Resolver SHALL 产生与当前代码完全相同的行为（零回归）
2. THE OEM_Brand_Registry SHALL 不引入额外的运行时依赖或第三方库
3. THE Brand_Config 的 ExtraTools 字段 SHALL 默认为空切片，不影响默认品牌的工具列表
4. WHEN 用户配置文件中存在 OEM 额外工具的配置数据但当前品牌不包含该工具时，THE Brand_Resolver SHALL 忽略这些多余的配置数据而不报错

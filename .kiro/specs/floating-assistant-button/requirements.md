# Requirements Document

## Introduction

本文档定义浮动助手按钮功能的需求。当用户隐藏 maclaw 主窗口后，桌面最顶层显示一个圆形浮动按钮（maclaw logo），支持拖动、左键点击恢复主窗口、右键菜单隐藏，并可通过通用配置中的"显示助手入口"复选框控制开关。

## Glossary

- **Main_Window**: maclaw 应用的主窗口，包含设置面板、AI 助手面板等
- **Floating_Button**: 浮动助手按钮，独立的无边框、透明、置顶小窗口，显示 maclaw logo 和光环动画
- **FloatingAssistantManager**: Go 后端组件，管理浮动按钮窗口的生命周期（创建、显示、隐藏、销毁）
- **AppConfig**: 应用配置结构体，持久化到 `~/.maclaw/config.json`
- **Settings_Panel**: 主窗口中的"通用配置"设置面板
- **Context_Menu**: 浮动按钮的自定义右键菜单
- **Drag_Threshold**: 区分拖动和点击的距离阈值，设定为 5 像素
- **Halo_Animation**: 浮动按钮周围的脉冲发光环 CSS 动画效果
- **AI_Assistant_Panel**: 主窗口中的 AI 助手面板（navTab="ai"）

## Requirements

### Requirement 1: 主窗口隐藏时显示浮动按钮

**User Story:** As a user, I want the floating assistant button to appear when I hide the main window, so that I can quickly access the AI assistant without opening the full application.

#### Acceptance Criteria

1. WHEN the user clicks the hide button on Main_Window AND AppConfig.show_assistant_entry is true, THEN THE FloatingAssistantManager SHALL hide Main_Window and display Floating_Button on the desktop topmost layer
2. WHEN the user clicks the hide button on Main_Window AND AppConfig.show_assistant_entry is false, THEN THE FloatingAssistantManager SHALL hide Main_Window without displaying Floating_Button
3. WHEN Floating_Button is already visible AND the user triggers another show request, THEN THE FloatingAssistantManager SHALL perform no additional action
4. IF the floating window creation fails due to platform limitations or insufficient resources, THEN THE FloatingAssistantManager SHALL log the error silently and maintain Main_Window functionality

### Requirement 2: 左键点击浮动按钮恢复主窗口

**User Story:** As a user, I want to click the floating button to restore the main window and switch to the AI assistant panel, so that I can quickly resume interaction with the AI assistant.

#### Acceptance Criteria

1. WHEN the user left-clicks Floating_Button, THEN THE FloatingAssistantManager SHALL show Main_Window, emit a "switch-to-ai-panel" event, and hide Floating_Button
2. WHEN Main_Window is restored via Floating_Button click, THEN THE Main_Window SHALL switch the active navigation tab to AI_Assistant_Panel
3. WHEN the user left-clicks Floating_Button, THEN THE FloatingAssistantManager SHALL set Floating_Button visibility to false after Main_Window is shown

### Requirement 3: 浮动按钮拖动交互

**User Story:** As a user, I want to drag the floating button to reposition it on screen, so that it does not obstruct my work area.

#### Acceptance Criteria

1. WHEN the user presses and moves the mouse on Floating_Button with a displacement exceeding Drag_Threshold (5 pixels in either axis), THEN THE Floating_Button SHALL follow the mouse position in real-time
2. WHEN the user presses and releases the mouse on Floating_Button with a displacement less than or equal to Drag_Threshold, THEN THE Floating_Button SHALL treat the interaction as a left-click
3. WHEN the user completes a drag operation, THEN THE FloatingAssistantManager SHALL save the new position for subsequent displays
4. WHEN the user drags Floating_Button beyond the screen boundary, THEN THE FloatingAssistantManager SHALL clamp the position to the nearest visible screen coordinate on mouse release

### Requirement 4: 右键菜单隐藏浮动按钮

**User Story:** As a user, I want to hide the floating button via a right-click context menu, so that I can remove it from the desktop when I do not need it.

#### Acceptance Criteria

1. WHEN the user right-clicks Floating_Button, THEN THE Floating_Button SHALL display a custom Context_Menu containing a "隐藏" (Hide) option
2. WHEN the user right-clicks Floating_Button, THEN THE Floating_Button SHALL suppress the system default context menu
3. WHEN the user selects the "隐藏" option from Context_Menu, THEN THE FloatingAssistantManager SHALL hide Floating_Button without affecting Main_Window state
4. WHEN Context_Menu is displayed AND the user clicks outside of it, THEN THE Context_Menu SHALL close without performing any action

### Requirement 5: 配置项控制浮动按钮显示

**User Story:** As a user, I want to control the floating button visibility through a settings checkbox, so that I can enable or disable this feature according to my preference.

#### Acceptance Criteria

1. THE Settings_Panel SHALL display a "显示助手入口" (Show Assistant Entry) checkbox in the General settings section, on the same row as the "显示欢迎页" (Show Welcome Page) checkbox
2. WHEN the user toggles the "显示助手入口" checkbox, THEN THE Settings_Panel SHALL update AppConfig.show_assistant_entry and persist the change to disk immediately
3. WHEN AppConfig.show_assistant_entry changes from true to false AND Floating_Button is currently visible, THEN THE FloatingAssistantManager SHALL hide Floating_Button immediately
4. WHEN AppConfig.show_assistant_entry is not present in the configuration file, THEN THE AppConfig SHALL default to true
5. THE Settings_Panel SHALL support localization for the "显示助手入口" label in Chinese (Simplified), Chinese (Traditional), and English

### Requirement 6: 浮动按钮视觉效果

**User Story:** As a user, I want the floating button to have a recognizable and visually appealing appearance, so that I can easily identify and interact with it.

#### Acceptance Criteria

1. THE Floating_Button SHALL render as a 56×56 pixel circular button using the maclaw2.png logo image
2. THE Floating_Button SHALL have a transparent background with no window frame or border
3. THE Floating_Button SHALL display Halo_Animation as a pulsing glow ring around the button using CSS animation
4. THE Halo_Animation SHALL use GPU-accelerated CSS animation without JavaScript timers
5. THE Floating_Button SHALL remain on the desktop topmost layer (always-on-top) while visible

### Requirement 7: 浮动按钮与主窗口互斥可见性

**User Story:** As a user, I want the floating button and main window to not appear simultaneously, so that the interface remains clean and unambiguous.

#### Acceptance Criteria

1. WHEN Floating_Button is visible AND the user triggers Main_Window to show, THEN THE FloatingAssistantManager SHALL hide Floating_Button
2. WHEN Main_Window is visible, THEN THE FloatingAssistantManager SHALL maintain Floating_Button in a hidden state
3. THE FloatingAssistantManager SHALL ensure that Floating_Button and Main_Window are never simultaneously visible

### Requirement 8: AppConfig 序列化与默认值

**User Story:** As a developer, I want the show_assistant_entry configuration field to serialize correctly with a proper default value, so that the feature works out of the box for new installations.

#### Acceptance Criteria

1. THE AppConfig SHALL include a show_assistant_entry field of boolean type with JSON key "show_assistant_entry"
2. WHEN deserializing AppConfig from JSON where show_assistant_entry is missing, THEN THE AppConfig SHALL set show_assistant_entry to true
3. WHEN deserializing AppConfig from JSON where show_assistant_entry has an invalid type, THEN THE AppConfig SHALL set show_assistant_entry to true and log a warning
4. WHEN serializing AppConfig to JSON, THEN THE AppConfig SHALL include the show_assistant_entry field with its current boolean value

### Requirement 9: 平台原生窗口实现

**User Story:** As a developer, I want the floating window to use platform-native APIs, so that it renders correctly with transparency and always-on-top behavior on all supported platforms.

#### Acceptance Criteria

1. WHEN running on macOS, THEN THE FloatingAssistantManager SHALL create the floating window using NSWindow with floating level and clear background
2. WHEN running on Windows, THEN THE FloatingAssistantManager SHALL create the floating window using Win32 CreateWindowEx with WS_EX_TOPMOST, WS_EX_LAYERED, and WS_EX_TOOLWINDOW flags
3. WHEN running on Linux, THEN THE FloatingAssistantManager SHALL create the floating window using GTK with keep-above and undecorated properties
4. THE FloatingAssistantManager SHALL abstract platform-specific implementations behind a unified interface using build-tag files (floating_darwin.go, floating_windows.go, floating_linux.go)

### Requirement 10: 浮动按钮默认位置

**User Story:** As a user, I want the floating button to appear at a sensible default position when first shown, so that it is immediately visible and accessible.

#### Acceptance Criteria

1. WHEN Floating_Button is shown for the first time (no prior drag position saved), THEN THE FloatingAssistantManager SHALL position it at the horizontal center of the screen, 10 pixels from the top edge
2. WHEN Floating_Button has been previously dragged, THEN THE FloatingAssistantManager SHALL restore it to the last saved position

### Requirement 11: 浮动窗口资源管理

**User Story:** As a developer, I want the floating window to be destroyed when hidden rather than merely hidden, so that WebView resources are released and memory usage stays low.

#### Acceptance Criteria

1. WHEN Floating_Button is hidden, THEN THE FloatingAssistantManager SHALL destroy the floating window and release associated WebView resources
2. WHILE Floating_Button is visible, THE Floating_Button SHALL consume less than 10 MB of memory
3. THE Floating_Button SHALL use requestAnimationFrame throttling for drag operations to avoid excessive window move calls

### Requirement 12: 错误恢复

**User Story:** As a user, I want to be able to recover the main window through the system tray even if the floating button fails, so that I am never locked out of the application.

#### Acceptance Criteria

1. IF Floating_Button creation fails, THEN THE system tray icon SHALL remain functional for restoring Main_Window
2. IF Floating_Button is hidden via Context_Menu, THEN THE system tray icon SHALL remain functional for restoring Main_Window

# Implementation Plan: Floating Assistant Button

## Overview

Implement a floating assistant button that appears on the desktop topmost layer when the user hides the maclaw main window. The button displays the maclaw logo with a halo animation, supports drag repositioning, left-click to restore the main window (switching to AI panel), and right-click context menu to hide. A settings checkbox in General settings controls the feature. The implementation spans Go backend (FloatingAssistantManager, AppConfig, Wails bindings), platform-native floating windows (macOS/Windows/Linux via build tags), and React frontend (floating button UI, settings extension).

## Tasks

- [x] 1. Extend AppConfig and add FloatingAssistantManager core
  - [x] 1.1 Add `show_assistant_entry` field to AppConfig
    - Add `ShowAssistantEntry bool` field with JSON key `"show_assistant_entry"` to `AppConfig` in `corelib/app_config.go`
    - Update `UnmarshalJSON` to default `ShowAssistantEntry` to `true` when the field is absent or has an invalid type
    - Ensure `MarshalJSON` / standard serialization includes the field
    - _Requirements: 8.1, 8.2, 8.3, 8.4_

  - [x]* 1.2 Write property test for AppConfig serialization round-trip (Property 7)
    - **Property 7: AppConfig serialization round-trip with default**
    - Generate arbitrary `AppConfig` instances with random `show_assistant_entry` values; serialize to JSON and deserialize back; assert equivalence
    - Generate JSON payloads missing `show_assistant_entry`; deserialize and assert default is `true`
    - Use Go `testing/quick` or table-driven tests
    - **Validates: Requirements 8.2, 8.4**

  - [x] 1.3 Create FloatingAssistantManager struct and interface
    - Create `gui/floating_assistant.go` with `FloatingAssistantManager` struct (fields: `app *App`, `visible bool`, `posX, posY int`, `mu sync.Mutex`)
    - Implement `ShowFloatingButton()`, `HideFloatingButton()`, `IsVisible()`, `UpdatePosition(x, y int)` methods
    - `ShowFloatingButton`: check `config.ShowAssistantEntry`, check `visible`, compute default position if needed, call platform-specific window creation
    - `HideFloatingButton`: destroy floating window, set `visible = false`
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 7.3_

  - [x]* 1.4 Write unit tests for FloatingAssistantManager state transitions
    - Test ShowFloatingButton when config is false → no-op
    - Test ShowFloatingButton when already visible → idempotent
    - Test HideFloatingButton → visible becomes false
    - Test ShowFloatingButton → HideFloatingButton → ShowFloatingButton cycle
    - _Requirements: 1.1, 1.2, 1.3_

- [x] 2. Implement platform-native floating window backends
  - [x] 2.1 Define platform abstraction interface
    - Create `gui/floating_window.go` with `floatingWindow` interface: `Create(x, y, w, h int) error`, `Show()`, `Hide()`, `Destroy()`, `MoveTo(x, y int)`, `IsCreated() bool`
    - Define `newFloatingWindow(app *App) floatingWindow` factory function (dispatched by build tags)
    - _Requirements: 9.4_

  - [x] 2.2 Implement Windows floating window (`floating_windows.go`)
    - Use `//go:build windows` build tag
    - Create Win32 window via `CreateWindowEx` with `WS_EX_TOPMOST | WS_EX_LAYERED | WS_EX_TOOLWINDOW` flags
    - Embed WebView2 for rendering the React floating button UI
    - Implement frameless, transparent, always-on-top behavior
    - _Requirements: 9.2_

  - [x] 2.3 Implement macOS floating window (`floating_darwin.go`)
    - Use `//go:build darwin` build tag
    - Create NSWindow via CGo/ObjC bridge with `NSWindow.setLevel(.floating)` and `NSWindow.setBackgroundColor(.clear)`
    - Embed WKWebView for rendering
    - _Requirements: 9.1_

  - [x] 2.4 Implement Linux floating window (`floating_linux.go`)
    - Use `//go:build linux` build tag
    - Create GTK window with `gtk_window_set_keep_above` and `gtk_window_set_decorated(false)`
    - Embed WebKitWebView for rendering
    - _Requirements: 9.3_

- [x] 3. Checkpoint - Ensure backend compiles on all platforms
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Add Wails bindings and modify WindowHide
  - [x] 4.1 Wire FloatingAssistantManager into App struct
    - Add `floatingAssistant *FloatingAssistantManager` field to `App` struct in `gui/app.go`
    - Initialize in `OnStartup` or app initialization
    - _Requirements: 1.1_

  - [x] 4.2 Modify `WindowHide()` to show floating button
    - In `gui/app.go`, after `runtime.WindowHide(a.ctx)` and `UpdateTrayVisibility(false)`, call `a.floatingAssistant.ShowFloatingButton()`
    - _Requirements: 1.1, 1.2_

  - [x] 4.3 Add `OnFloatingButtonClicked()` Wails binding
    - Implement in `gui/app.go` or `gui/floating_assistant.go`
    - Call `runtime.WindowShow(a.ctx)`, toggle always-on-top, emit `"switch-to-ai-panel"` event, call `HideFloatingButton()`
    - _Requirements: 2.1, 2.2, 2.3_

  - [x] 4.4 Add `OnFloatingButtonDragged(x, y int)` Wails binding
    - Move floating window to (x, y) via platform abstraction
    - Call `UpdatePosition(x, y)` to save position
    - _Requirements: 3.3_

  - [x] 4.5 Add `HideFloatingButton()` Wails binding
    - Expose `FloatingAssistantManager.HideFloatingButton()` as a Wails-callable method for the floating window frontend
    - _Requirements: 4.3_

- [x] 5. Implement floating button React UI (second window)
  - [x] 5.1 Create FloatingButton component
    - Create `gui/frontend/src/components/FloatingButton.tsx`
    - Render 56×56px circular button using `maclaw2.png` logo
    - Transparent background, no window frame
    - Implement halo animation via CSS (pulsing glow ring)
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5_

  - [x] 5.2 Implement drag interaction with click/drag threshold
    - Track mousedown → mousemove → mouseup
    - If displacement ≤ 5px in both axes → treat as left-click → call `OnFloatingButtonClicked()`
    - If displacement > 5px in either axis → drag mode → call `OnFloatingButtonDragged(x, y)` with `requestAnimationFrame` throttling
    - _Requirements: 3.1, 3.2, 11.3_

  - [x] 5.3 Implement custom right-click context menu
    - Suppress default context menu via `preventDefault()`
    - Show custom menu with "隐藏" (Hide) option
    - Click "隐藏" → call `HideFloatingButton()` Wails binding
    - Click outside menu → close menu
    - _Requirements: 4.1, 4.2, 4.3, 4.4_

  - [x] 5.4 Create floating button CSS with halo animation
    - Create `gui/frontend/src/components/FloatingButton.css`
    - `.floating-assistant-container`: 56×56px, circular, relative positioning, `user-select: none`, `-webkit-app-region: no-drag`
    - `.floating-assistant-halo`: absolute positioned, pulsing border + box-shadow animation (2s ease-in-out infinite), GPU-accelerated
    - `.floating-assistant-logo`: 56×56px, circular, `object-fit: cover`
    - `.floating-context-menu`: positioned context menu styling
    - _Requirements: 6.1, 6.3, 6.4_

  - [x] 5.5 Create floating window entry point
    - Create a separate HTML entry or route for the floating window that renders only the `FloatingButton` component
    - Ensure minimal bundle size (no full React app loaded)
    - _Requirements: 11.1, 11.2_

- [x] 6. Checkpoint - Ensure floating button UI renders correctly
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. Write property-based tests for floating button logic
  - [x]* 7.1 Write property test for state machine consistency (Property 1)
    - **Property 1: State machine consistency under config and operations**
    - Generate arbitrary sequences of show/hide/config-change operations using fast-check `fc.commands`
    - Assert: `visible == true` only when hide-main-window occurred AND `show_assistant_entry == true` AND no subsequent click/hide/config-false
    - Assert: `ShowFloatingButton` when already visible is idempotent
    - **Validates: Requirements 1.1, 1.2, 1.3, 4.3, 5.3**

  - [x]* 7.2 Write property test for click restores main window (Property 2)
    - **Property 2: Click restores main window and switches to AI panel**
    - For any state where floating button is visible, simulate left-click
    - Assert: main window visible, nav tab set to AI panel, floating button hidden
    - **Validates: Requirements 2.1, 2.2, 2.3**

  - [x]* 7.3 Write property test for drag/click threshold (Property 3)
    - **Property 3: Drag/click threshold classification**
    - Generate arbitrary (deltaX, deltaY) pairs using `fc.integer`
    - Assert: classified as drag if `|deltaX| > 5 OR |deltaY| > 5`, click otherwise
    - **Validates: Requirements 3.1, 3.2**

  - [x]* 7.4 Write property test for drag position round-trip (Property 4)
    - **Property 4: Drag position round-trip**
    - Generate arbitrary valid positions, save via `UpdatePosition`, then show button
    - Assert: button appears at saved position
    - **Validates: Requirements 3.3, 10.2**

  - [x]* 7.5 Write property test for position clamping (Property 5)
    - **Property 5: Position clamping within screen bounds**
    - Generate arbitrary screen dimensions (W, H) and drag positions (x, y)
    - Assert: clamped position satisfies `0 ≤ x ≤ W - buttonWidth` and `0 ≤ y ≤ H - buttonHeight`
    - **Validates: Requirement 3.4**

  - [x]* 7.6 Write property test for mutual exclusivity (Property 6)
    - **Property 6: Mutual exclusivity invariant**
    - Generate arbitrary sequences of show/hide/click/config-change operations
    - Assert: floating button and main window are never simultaneously visible
    - **Validates: Requirements 7.1, 7.2, 7.3**

  - [x]* 7.7 Write property test for default position (Property 8)
    - **Property 8: Default position calculation**
    - Generate arbitrary screen widths W using `fc.integer`
    - Assert: default position is `(W/2 - 28, 10)`
    - **Validates: Requirement 10.1**

- [-] 8. Implement settings UI extension and event handling
  - [x] 8.1 Add "显示助手入口" checkbox to Settings panel
    - In `gui/frontend/src/App.tsx`, locate the "显示欢迎页" checkbox in General settings
    - Add "显示助手入口" checkbox on the same row
    - Bind to `config.show_assistant_entry` (default `true` when undefined)
    - On toggle: update config and call `SaveConfig`
    - _Requirements: 5.1, 5.2_

  - [x] 8.2 Add localization strings for the checkbox
    - Add `"showAssistantEntry"` key to English (`"Show Assistant Entry"`), Chinese Simplified (`"显示助手入口"`), and Chinese Traditional (`"顯示助手入口"`) translation objects in `App.tsx`
    - _Requirements: 5.5_

  - [x] 8.3 Handle config change → hide floating button
    - When `show_assistant_entry` changes from `true` to `false` and floating button is visible, call `HideFloatingButton()`
    - Implement via config change listener or direct call in the toggle handler
    - _Requirements: 5.3_

  - [x] 8.4 Handle "switch-to-ai-panel" event in main window
    - In `gui/frontend/src/App.tsx`, listen for Wails event `"switch-to-ai-panel"`
    - On event: call `setNavTab("ai")` to switch to AI assistant panel
    - _Requirements: 2.2_

  - [x]* 8.5 Write unit tests for settings checkbox behavior
    - Test toggle on/off updates config correctly
    - Test config change to false hides visible floating button
    - Test default value when field is missing
    - _Requirements: 5.1, 5.2, 5.3, 5.4_

- [ ] 9. Implement error handling and resource management
  - [x] 9.1 Add position clamping on drag end
    - In `FloatingAssistantManager.UpdatePosition()` or the drag handler, clamp (x, y) to screen bounds
    - Use `GetScreenDimensions()` (platform-specific) to determine bounds
    - Ensure `0 ≤ x ≤ screenWidth - 64` and `0 ≤ y ≤ screenHeight - 64`
    - _Requirements: 3.4_

  - [x] 9.2 Add silent failure handling for window creation
    - In `ShowFloatingButton()`, wrap platform window creation in error handling
    - On failure: log error, set `visible = false`, do not affect main window functionality
    - System tray remains functional for restoring main window
    - _Requirements: 1.4, 12.1, 12.2_

  - [x] 9.3 Implement window destroy on hide for resource management
    - `HideFloatingButton()` must destroy the floating window (not just hide), releasing WebView resources
    - Ensure memory usage stays under 10MB while visible
    - _Requirements: 11.1, 11.2_

  - [x] 9.4 Implement mutual exclusivity guard
    - When main window is shown (from any source), ensure floating button is hidden
    - When floating button is shown, ensure main window is hidden
    - Add guard in `OnFloatingButtonClicked()` and any other main window show paths
    - _Requirements: 7.1, 7.2, 7.3_

- [x] 10. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document using fast-check (frontend) and Go testing (backend)
- Unit tests validate specific examples and edge cases
- Platform-native window implementations (task 2) are the highest-risk items — if Wails v2 multi-window support is insufficient, the fallback to native APIs (NSWindow/Win32/GTK) is required as specified in the design
- The floating button UI runs in a separate lightweight window with minimal React bundle to keep memory under 10MB

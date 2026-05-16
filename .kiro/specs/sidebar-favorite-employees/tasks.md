# Tasks: 侧边栏重构 + 常用数字员工快捷入口

## Tasks

- [ ] 1. 数据层：AppConfig 新增 favorite_employees 字段
  - [ ] 1.1 `corelib/app_config.go`：`AppConfig` 结构体新增 `FavoriteEmployees []string` 字段（`json:"favorite_employees,omitempty"`）
  - [ ] 1.2 `gui/frontend/wailsjs/go/models.ts`：`AppConfig` 类新增 `favorite_employees?: string[]` 字段声明 + 构造函数赋值
  - [ ] 1.3 验证 JSON round-trip：SaveConfig → LoadConfig 后 favorite_employees 正确持久化和恢复
    - _Requirements: 6.1, 6.2, 6.3_

- [ ] 2. SidebarNavRail 重构：移除中间导航项，改为"系统"弹出菜单
  - [ ] 2.1 移除 `navItems` 数组定义（monitor/skills/mcp/gossip/agentnet）
  - [ ] 2.2 移除 `pinnedItems`/`collapsedItems` 过滤逻辑和 `sidebarExpanded` 展开/折叠 UI
  - [ ] 2.3 底部"设置"按钮文字从 `t('settings')` 改为"系统"（中文）/ "System"（英文），图标保留 ⚙️
  - [ ] 2.4 点击"系统"按钮改为 toggle `systemMenuOpen` state（不再调用 `switchTool('settings')`）
  - [ ] 2.5 "关于"按钮保留在"系统"按钮下方，行为不变
  - [ ] 2.6 从 `SidebarNavRailProps` 中移除不再需要的 props：`runningTaskCount`, `gossipAllowed`, `sidebarExpanded`, `setSidebarExpanded`
  - [ ] 2.7 新增 props：`favoriteEmployees: FavoriteEmployeeSlot[]`, `onStartVEConversation: (veId: string) => void`, `onReorderFavorites: (newOrder: string[]) => void`, `systemMenuItems: SystemMenuItem[]`
    - _Requirements: 1.1, 1.2, 1.3, 1.7_

- [ ] 3. SystemPopupMenu 组件（新建）
  - [ ] 3.1 创建 `gui/frontend/src/components/layout/SystemPopupMenu.tsx`
  - [ ] 3.2 实现横向菜单条布局：flex-direction: row，每项为图标+文字的纵向小块
  - [ ] 3.3 定位：absolute，left = SIDEBAR_NAV_RAIL_WIDTH (60px)，bottom 对齐触发按钮
  - [ ] 3.4 菜单项数据：设置(⚙️) | 监控(📡) | 技能(🧩) | MCP(🔌) | 八卦(🗣️) | 智网(AgentNet图标)
  - [ ] 3.5 点击菜单项：调用 `switchTool(id)` + 关闭菜单
  - [ ] 3.6 点击外部关闭：useEffect + document mousedown listener
  - [ ] 3.7 样式：圆角 10px、border、box-shadow、theme-aware 背景色
  - [ ] 3.8 gossip 项受 `gossipAllowed` 控制显隐，agentnet 受 brand 控制显隐
  - [ ] 3.9 监控项显示 runningTaskCount badge（复用原有 badge 逻辑）
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6_

- [ ] 4. FavoriteEmployeeButtons 组件（新建）
  - [ ] 4.1 创建 `gui/frontend/src/components/layout/FavoriteEmployeeButtons.tsx`
  - [ ] 4.2 渲染 0~5 个按钮，纵向排列，每个按钮宽度 = SIDEBAR_NAV_RAIL_WIDTH
  - [ ] 4.3 每个按钮内容：圆形区域（首字符 + 右下角在线状态小圆点）+ 名称（截断到 4 字符）
  - [ ] 4.4 点击按钮调用 `onStartVEConversation(veId)`（等同于双击数字员工列表项）
  - [ ] 4.5 VE 离线/已删除时按钮显示为灰色半透明，仍可点击（点击后由对话层处理离线提示）
  - [ ] 4.6 空列表时组件返回 null（不占空间）
  - [ ] 4.7 实现 HTML5 drag-and-drop 排序：draggable + onDragStart/onDragOver/onDrop/onDragEnd
  - [ ] 4.8 拖动时显示蓝色插入指示线（2px 高，在目标位置上方或下方）
  - [ ] 4.9 放下后调用 `onReorderFavorites(newOrder)` 通知父组件
    - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6_

- [ ] 5. VirtualEmployeeTab 右键菜单扩展："设为常用"
  - [ ] 5.1 `VETabProps` 新增 `favoriteEmployeeIds?: string[]` 和 `onSetFavorite?: (ve: VirtualEmployeeEntry) => void`
  - [ ] 5.2 右键菜单新增第三项：`⭐ 设为常用` / `已是常用`（灰色不可点击）
  - [ ] 5.3 判断逻辑：`favoriteEmployeeIds?.includes(ve.id)` → 显示"已是常用"灰色文字
  - [ ] 5.4 点击"设为常用"时调用 `onSetFavorite(contextMenu.ve)` + 关闭菜单
    - _Requirements: 4.1, 4.2, 4.5_

- [ ] 6. FavoriteEmployeeReplacePicker 组件（新建）
  - [ ] 6.1 创建 `gui/frontend/src/components/layout/FavoriteEmployeeReplacePicker.tsx`
  - [ ] 6.2 显示为 popover/modal，内容：标题"常用已满，选择要替换的位置" + 5 个槽位行
  - [ ] 6.3 每行显示：序号(1-5) + 当前占用的 VE 名称 + "点击替换"提示
  - [ ] 6.4 点击某行 → 调用 `onReplace(index)` → 关闭 picker
  - [ ] 6.5 点击外部或"取消"按钮关闭
  - [ ] 6.6 样式：居中 modal 或 fixed popover，圆角、阴影、theme-aware
    - _Requirements: 4.3, 4.4_

- [ ] 7. App.tsx 状态管理接线
  - [ ] 7.1 新增 state：`const [favoriteEmployees, setFavoriteEmployees] = useState<string[]>([])`
  - [ ] 7.2 从 config 加载：`useEffect` 中 `setFavoriteEmployees(config?.favorite_employees || [])`
  - [ ] 7.3 实现 `updateFavoriteEmployees(newList)`：先 `LoadConfig()` 获取最新 → merge → `SaveConfig` → `setConfig`
  - [ ] 7.4 实现 `handleSetFavorite(ve)`：< 5 直接追加；= 5 弹出 ReplacePicker
  - [ ] 7.5 实现 `handleReorderFavorites(newOrder)`：调用 `updateFavoriteEmployees`
  - [ ] 7.6 实现 `handleRemoveFavorite(veId)`：过滤后调用 `updateFavoriteEmployees`
  - [ ] 7.7 计算 `favoriteEmployeeSlots`：join favoriteEmployees IDs 与 veList 数据
  - [ ] 7.8 新增 state：`const [showReplacePicker, setShowReplacePicker] = useState<{ve: VirtualEmployeeEntry} | null>(null)`
  - [ ] 7.9 将 favoriteEmployees/handlers 传递到 AppSidebarShell → SidebarNavRail
  - [ ] 7.10 将 favoriteEmployeeIds + onSetFavorite 传递到 VirtualEmployeeTab
    - _Requirements: 2.2, 2.4, 2.5, 4.2, 4.3, 4.4, 6.1, 6.2, 6.3, 6.4_

- [ ] 8. AppSidebarShell props 透传
  - [ ] 8.1 `AppSidebarShellProps` 新增：`favoriteEmployeeSlots`, `onStartVEConversation`, `onReorderFavorites`, `systemMenuItems`, `gossipAllowed`, `runningTaskCount`
  - [ ] 8.2 透传到 `SidebarNavRail`
    - _Requirements: 2.1, 2.4_

- [ ] 9. 设置面板新增"数字员工"Tab
  - [ ] 9.1 `settingsTabs.ts`：`SettingsTabId` 新增 `'virtualEmployee'`
  - [ ] 9.2 `getSettingsTabOptions` 在 `agentnet` 之后插入 `{ id: 'virtualEmployee', label: '数字员工', desc: '常用数字员工管理' }`，受 `veAuthorized` 控制显隐（无授权时从 tabs 列表中过滤掉）
  - [ ] 9.3 创建 `gui/frontend/src/components/settings/FavoriteEmployeeSettingsPanel.tsx`
  - [ ] 9.4 面板内容：当前常用列表（拖动排序 + 移除按钮）+ "添加常用"按钮（弹出 VE 选择列表）
  - [ ] 9.5 在 App.tsx 的设置面板渲染区域新增 `settingsTab === 'virtualEmployee'` 分支
    - _Requirements: 3.1, 3.2, 3.3_

- [ ] 10. AI 助手面板标题栏移除群聊按钮
  - [ ] 10.1 `AssistantTitleBar.tsx`：移除 `{groupDiscussion && <AssistantGroupDiscussionMenu ... />}` 渲染行
  - [ ] 10.2 保留 `groupDiscussion` prop 定义（不删除接口，底层 VE 群聊仍使用）
  - [ ] 10.3 可选：移除 `AssistantGroupDiscussionMenu` 的 import（如果无其他引用）
    - _Requirements: 5.2, 5.3_

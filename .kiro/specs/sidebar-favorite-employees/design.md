# Technical Design: 侧边栏重构 + 常用数字员工快捷入口

## 架构概览

```
SidebarNavRail (60px 宽)
├── Logo + Brand Name (56px 高)
├── AI 助手按钮 (带状态光环)
├── 分隔条 (渐变线)
├── 常用数字员工按钮区 (0~5 个, 可拖动排序)
├── flex spacer
├── "系统"按钮 → 弹出 SystemPopupMenu
└── "关于"按钮
```

## 组件设计

### 1. SidebarNavRail 重构

**修改文件**: `gui/frontend/src/components/layout/SidebarNavRail.tsx`

**变更**:
- 移除中间的 navItems 渲染（monitor/skills/mcp/gossip/agentnet）
- 移除 pinnedItems/collapsedItems 逻辑
- 移除 sidebarExpanded 展开/折叠逻辑
- "设置"按钮改为"系统"按钮，点击触发 `SystemPopupMenu` 弹出
- 在分隔条和 flex spacer 之间插入 `FavoriteEmployeeButtons` 组件

**新增 Props**:
```typescript
type SidebarNavRailProps = {
    // ... 保留: navTab, brandInfo, currentIcon, brandSidebarName, switchTool, lang,
    //          maclawLLMOnline, agentNetRunning, remoteActivationStatus, t
    // 移除: runningTaskCount, gossipAllowed, config, sidebarExpanded, setSidebarExpanded
    // 新增:
    favoriteEmployees: FavoriteEmployeeSlot[];
    onStartVEConversation: (veId: string) => void;
    onReorderFavorites: (newOrder: string[]) => void;
};
```

### 2. SystemPopupMenu 组件（新建）

**文件**: `gui/frontend/src/components/layout/SystemPopupMenu.tsx`

**行为**:
- 点击"系统"按钮时，向右弹出一个横向菜单条
- 菜单条定位：absolute，left = SIDEBAR_NAV_RAIL_WIDTH，bottom 对齐"系统"按钮
- 菜单项：设置 | 监控 | 技能 | MCP | 八卦 | 智网
- 每项带图标 + 文字标签
- 点击菜单项调用 `switchTool(id)` 并关闭菜单
- 点击外部区域关闭（useEffect + document click listener）

**样式**:
```css
.system-popup-menu {
    position: absolute;
    left: 60px;  /* SIDEBAR_NAV_RAIL_WIDTH */
    bottom: 0;
    display: flex;
    flex-direction: row;
    gap: 2px;
    padding: 6px 8px;
    border-radius: 10px;
    border: 1px solid var(--theme-border);
    background: var(--theme-surface);
    box-shadow: 0 4px 16px rgba(0,0,0,0.12);
    z-index: 9999;
}
```

**数据结构**:
```typescript
interface SystemMenuItem {
    id: string;        // 'settings' | 'remote' | 'skills' | 'mcp' | 'gossip' | 'agentnet'
    icon: string;      // emoji
    label: string;     // 国际化文字
    visible: boolean;  // gossip 受 gossipAllowed 控制, agentnet 受 brand 控制
}
```

### 3. FavoriteEmployeeButtons 组件（新建）

**文件**: `gui/frontend/src/components/layout/FavoriteEmployeeButtons.tsx`

**行为**:
- 渲染 0~5 个常用数字员工按钮，纵向排列
- 每个按钮：圆形头像区（首字符 + 在线状态点）+ 名称缩略
- 点击触发对话
- 支持 HTML5 drag-and-drop 排序
- 空列表时不渲染任何内容
- 当 VE 功能未授权时（`veAuthorized=false`），整个组件不渲染，与中间面板数字员工 Tab 显隐条件一致

**数据结构**:
```typescript
interface FavoriteEmployeeSlot {
    veId: string;
    name: string;
    online: boolean;
}
```

**拖动排序实现**:
- 使用 `draggable` + `onDragStart/onDragOver/onDragEnd` 原生事件
- 拖动时显示插入指示线
- 放下后调用 `onReorderFavorites(newOrder)` 通知父组件持久化

### 4. FavoriteEmployeeReplacePicker 组件（新建）

**文件**: `gui/frontend/src/components/layout/FavoriteEmployeeReplacePicker.tsx`

**行为**:
- 当常用已满 5 个时，用户点击"设为常用"触发此组件
- 显示为 popover（定位在触发元素附近）
- 内容：5 个槽位，每个显示当前占用的数字员工名称
- 用户点击某个槽位 → 替换该位置 → 关闭 picker
- 点击外部关闭

**样式**: 类似 context menu，5 行列表，每行带序号 + 名称 + "替换"提示

### 5. VirtualEmployeeTab 右键菜单扩展

**修改文件**: `gui/frontend/src/components/ai/VirtualEmployeeTab.tsx`

**变更**:
- 新增 prop: `favoriteEmployeeIds: string[]`
- 新增 prop: `onSetFavorite: (ve: VirtualEmployeeEntry) => void`
- 右键菜单新增第三项："⭐ 设为常用" / "已是常用"（灰色）
- 判断逻辑：`favoriteEmployeeIds.includes(ve.id)` → 灰色不可点击

### 6. 设置面板新增"数字员工"Tab

**修改文件**: `gui/frontend/src/config/settingsTabs.ts`

**变更**: `SettingsTabId` 新增 `'virtualEmployee'`，在 `agentnet` 之后插入

**新建文件**: `gui/frontend/src/components/settings/FavoriteEmployeeSettingsPanel.tsx`

**内容**:
- 显示当前常用列表（最多 5 个），每项带"移除"按钮
- "添加常用"按钮 → 弹出数字员工选择列表（排除已在常用中的）
- 支持拖动排序（复用 FavoriteEmployeeButtons 的拖动逻辑）

### 7. AI 助手面板标题栏移除群聊按钮

**修改文件**: `gui/frontend/src/components/ai/AssistantTitleBar.tsx`

**变更**:
- 移除 `{groupDiscussion && <AssistantGroupDiscussionMenu ... />}` 渲染
- 保留 `groupDiscussion` prop 传递（底层 API 仍被 VE 群聊使用），但不在标题栏显示按钮

## 数据流

### 持久化

```
config.json
├── favorite_employees: string[]   // VE ID 列表，顺序即显示顺序，最多 5 个
```

**Go 侧**: `AppConfig` 新增 `FavoriteEmployees []string` 字段 (`json:"favorite_employees,omitempty"`)

**TypeScript 侧**: `models.ts` 的 `AppConfig` 新增 `favorite_employees?: string[]`

### 状态管理（App.tsx 层）

```typescript
// 新增 state
const [favoriteEmployees, setFavoriteEmployees] = useState<string[]>([]);

// 从 config 加载
useEffect(() => {
    if (config?.favorite_employees) {
        setFavoriteEmployees(config.favorite_employees);
    }
}, [config]);

// 持久化
const updateFavoriteEmployees = useCallback(async (newList: string[]) => {
    setFavoriteEmployees(newList);
    const latest = await LoadConfig();
    const updated = new main.AppConfig({ ...latest, favorite_employees: newList });
    await SaveConfig(updated);
    setConfig(updated);
}, []);

// 设为常用（含满员替换逻辑）
const handleSetFavorite = useCallback((veId: string, replaceIndex?: number) => {
    if (favoriteEmployees.includes(veId)) return;
    let newList: string[];
    if (favoriteEmployees.length < 5) {
        newList = [...favoriteEmployees, veId];
    } else if (replaceIndex !== undefined) {
        newList = [...favoriteEmployees];
        newList[replaceIndex] = veId;
    } else {
        return; // 需要先弹出 picker
    }
    updateFavoriteEmployees(newList);
}, [favoriteEmployees, updateFavoriteEmployees]);
```

### 常用按钮的 VE 信息解析

`SidebarNavRail` 接收 `favoriteEmployees: FavoriteEmployeeSlot[]`。App.tsx 层负责将 `string[]`（ID 列表）与 `VirtualEmployeeEntry[]`（从 Hub 获取的完整列表）做 join：

```typescript
const favoriteEmployeeSlots = useMemo(() => {
    return favoriteEmployees
        .map(id => {
            const ve = veList.find(v => v.id === id);
            return ve ? { veId: id, name: ve.name, online: ve.online_status === 'online' } : { veId: id, name: id.slice(0, 6), online: false };
        });
}, [favoriteEmployees, veList]);
```

找不到的 VE（已删除/离线）仍显示但标记为灰色。

## 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `components/layout/SidebarNavRail.tsx` | 重写 | 移除 navItems，加入常用按钮区 + 系统弹出菜单 |
| `components/layout/SystemPopupMenu.tsx` | 新建 | 横向弹出菜单 |
| `components/layout/FavoriteEmployeeButtons.tsx` | 新建 | 常用数字员工按钮列表（含拖动排序） |
| `components/layout/FavoriteEmployeeReplacePicker.tsx` | 新建 | 满员替换选择器 |
| `components/ai/VirtualEmployeeTab.tsx` | 修改 | 右键菜单新增"设为常用" |
| `components/ai/AssistantTitleBar.tsx` | 修改 | 移除群聊按钮渲染 |
| `components/settings/FavoriteEmployeeSettingsPanel.tsx` | 新建 | 设置面板数字员工 tab |
| `config/settingsTabs.ts` | 修改 | 新增 virtualEmployee tab |
| `App.tsx` | 修改 | 新增 favoriteEmployees 状态管理 + 传递 props |
| `components/layout/AppSidebarShell.tsx` | 修改 | 传递新 props 到 SidebarNavRail |
| Go: `corelib/app_config.go` | 修改 | AppConfig 新增 FavoriteEmployees 字段 |
| TS: `wailsjs/go/models.ts` | 修改 | AppConfig 新增 favorite_employees 字段 |

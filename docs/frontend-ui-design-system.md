# MaClaw GUI 前端 UI 设计系统规范

> 2026-11 UI review 轮次沉淀的约定。所有新增/修改 UI 的会话都应遵守；破坏基线或棘轮的提交会被 prebuild 拦截。

## 1. CSS 组织结构

- **唯一拼装源**：`guiapp/frontend/src/styles/partials/*.css`，按 `styles/app-css.manifest.json` 顺序拼为 `src/App.css`。**不要直接编辑 `App.css`**（generated header 说明了这一点）。
- 改动 partial 后必须重新拼装并刷新基线：

  ```bash
  cd guiapp/frontend
  node scripts/assemble-app-css.mjs
  node scripts/verify-app-css-baseline.mjs --update   # 刻意改动时
  node scripts/check-css-discipline.mjs
  ```

- **partial 层次约定**：`00-tokens.css` 只放变量（词汇表）；组件级布局补丁放对应编号 partial（示例：`05-conversation-layout.css` 持有会话画布的 `!important` 布局手术）。新增文件按编号插入 manifest，注意级联顺序对 `!important` 平局裁决的影响。
- 各文件暗色支持：`--theme-*` 在 `00-tokens.css` 的 dark 块集中 rebind，feature CSS 默认直接用 `var(--theme-*)` 即可；只有主题盲的 rgba 色块需要专门补。

## 2. Token 词汇表（禁止再造第三套）

| 前缀 | 归属 | 说明 |
|---|---|---|
| `--theme-*` | 权威（`00-tokens.css`） | 颜色/覆盖层语义；WCAG AA 已审计（注释见文件头） |
| `--mc-*` | mc 壳层桥接 | 定义在 `120-app-responsive.css`（如需新变量放这里） |
| `--office-*` | pet-store 遗留 | 已被 `100-pet-store-mc.css:764-770` 桥到 `--theme-*`，**不要再新增** |
| 零散家族（`--cp-diff-*` 等） | 特性局部 | 允许，但必须限定在对应文件内 |

TSX 内联样式同样必须走 `--theme-*`（现状里 NotificationItem/NotificationToast 已示范）。

## 3. 字重与字号

- **字重只允许 400/500/600/700**，展示场景可到 800/900。禁止 450–850 的可变字体中间档（450→400、550/650→600、680/720/750/760→700、850→800 的映射已完成；为何安全：微软雅黑回退只有 400/700，对中文渲染零变化）。
- **字号**起步用新 token：`--font-size-2xs..display`（`00-tokens.css`）。存量 93 种字号是历史包袱，只在触碰某文件时顺手迁移到最近档，不做盲扫。

## 4. 按钮

- 家族：`.btn-primary / .btn-secondary / .btn-link`（定义于 `50-pet-provider.css`）。
- 尺寸档：默认 `5px 14px / 0.8rem`；`.btn--sm` = `3px 10px / 0.74rem`；`.btn--xs` = `2px 8px / 0.7rem`。
- 新面板一律 class 组合（`className="btn-primary btn--sm"`），禁止内联 `{fontSize,padding}` 复制粘贴（曾为 Skills 页面 7 种手工尺寸）。

## 5. z-index 阶梯

CSS token（`00-tokens.css`）与 TS 常量（`components/ui/zIndex.ts`）**双源同值**，改动必须两处同步：

```
surfacePopover 1000 < floatingPanel 4000 < overlay 9999 < stickyToast 10000 < panelTop 40000 < topmost 99999
锚点：expert-market-overlay 119000 < CustomDialog DIALOG_Z_INDEX 120000
```

- 内联 `zIndex` 用 `Z.overlay` 等常量（数值型，测试可精确断言）。
- 新增 CSS 选择器用 `var(--z-*)`。
- **TSX 不得再出现 `zIndex: <_integer>`**（视觉守卫见第 8 节回归清单）。

## 6. 定时器与轮询

任何 `setInterval` 都应改用 `utils/visibleInterval.ts` 的 `startVisibleInterval`（隐藏时暂停、恢复时先补一拍）。已全量改造：App.tsx 6 处轮询 + GoalQuickBar/CodingAgentProcessTimeline/HubConnectionBadge/CompanionNoteStatus。
**例外**：tick 带非 UI 副作用（如触发通知/写入任务状态机）的轮询要逐个甄别，不要盲目套用。

## 7. 长列表/流式渲染

- 会话行已有 `content-visibility: auto; contain-intrinsic-size: auto 96px`（`05-conversation-layout.css`），**不要移除**；调宽 `contain-intrinsic-size` 用行的真实代表性高度。
- 流式成本靠既有三件套兜底：msgRenderCacheRef（200 条 cap）、内容指纹、增量 Markdown（`AIAssistantPanel.tsx`）。改渲染缓存失效条件时注意全量清空 = 全文重解析。

## 8. 守卫机器（prebuild 自动执行）

| 守卫 | 内容 |
|---|---|
| `check-css-discipline.mjs` | `!important` ≤1596 棘轮、TSX 内联 style ≤3275 棘轮；只减不增，刻意刷新需改基线常量并留注释 |
| `check-ui-conventions.mjs` | 本规范第 3/4/5 节：禁 `transition: all`、字重阶梯硬校验；内联 `{fontSize,padding}` 按钮（基线 64）与 ≥1000 裸 z 内联（基线 20）为棘轮，只减不增 |
| `verify-app-css-baseline.mjs` | App.css 必须与快照逐字节一致 |
| `check-main-ui-guards.mjs` | App.tsx 行数、Memory/MCP 等高危文件行数上限、config 持久化自测 |
| `check-encoding.mjs --strict-mojibake` | 乱码检测（改文件后必跑） |

## 9. 已知遗留（按风险排序，动前需视觉验证）

1. **fontFamily 散点 78 处**（`system-ui`/Apple 栈 vs `var(--font-ui)`）：Latin 字形度量不同，建议逐文件迁移并截图对照。
2. **开关统一**：15+ 文件仍是裸 checkbox；`--theme-switch-*` + `role="switch"` 是目标形态。
3. **空态**：10+ 种类名各写各的；共享 EmptyState 组件待落地。
4. **进度条 `transition: width`（16 处）**：转 `transform: scaleX` 需 CSS+TSX 成对改并验证圆角失真。
5. **侧栏收展 0.35s width 动画**（`110:7323`）：刻意保留。

—— 2026-11，UI review 系列（通知/token 接入、宽度级联清理、字重阶梯、按钮尺寸档、z 阶梯、轮询 visibleInterval 化、content-visibility）全部落地后整理。

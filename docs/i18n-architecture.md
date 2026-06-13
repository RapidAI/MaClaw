# i18n 多语言本地化架构

## 现状

项目已有完整的 i18n 基础设施，支持三种语言：
- **简体中文** (zh-Hans) — 默认语言
- **English** (en)
- **繁体中文** (zh-Hant) — 前端支持，Go 后端暂统一到 zh

## 架构总览

```
┌──────────────────────────────────────────────────────────┐
│                     AppConfig.Language                      │
│        (zh-CN / zh-Hans / zh-Hant / en / en-US)           │
└────────────┬─────────────────────────────┬────────────────┘
             │                             │
    ┌────────▼────────┐          ┌────────▼────────┐
    │   Go Backend    │          │    Frontend     │
    │  corelib/i18n   │          │   src/i18n/     │
    │                 │          │                 │
    │  T(key, lang)   │          │  t(key, lang)   │
    │  Tf(key, lang,  │          │  localizeText() │
    │     args...)    │          │  translations{} │
    └─────────────────┘          └─────────────────┘
```

## Go 后端 (`corelib/i18n/`)

### 文件结构
```
corelib/i18n/
└── i18n.go          # 翻译 key 常量 + 翻译表 + T()/Tf() API
```

### API
| 函数 | 用途 |
|------|------|
| `T(key, lang) string` | 简单翻译查找 |
| `Tf(key, lang, args...) string` | 带 fmt.Sprintf 格式化的翻译 |
| `NormalizeLang(lang) string` | 语言代码标准化 |

### 翻译 Key 命名规范
- 前缀 `msg.` — 用户可见消息
- 前缀 `msg.tui_` — TUI 专用
- 前缀 `msg.workflow_` — 工作流引擎
- 前缀 `msg.confirm_` — 确认面板
- 前缀 `msg.exec_` — 执行确认门禁

### 当前覆盖
- 200+ 翻译 key
- IM 通道状态消息 ✅
- TUI 全部界面文本 ✅
- 工作流引擎用户消息 ✅
- 确认面板文本 ✅

## 前端 (`gui/frontend/src/i18n/`)

### 文件结构
```
gui/frontend/src/i18n/
├── index.ts              # 统一入口: t(), localizeText(), normalizeLang()
└── appTranslations.ts    # Key-based 翻译表 (700+ keys, en/zh-Hans/zh-Hant)
```

### API
| 函数 | 用途 |
|------|------|
| `t(key, lang)` | Key-based 翻译查找（推荐） |
| `localizeText(lang, en, zhHans, zhHant?)` | 内联三语选择（动态文本） |
| `normalizeLang(lang)` | 语言代码标准化 |
| `translations` | 直接访问翻译表 |

### 历史遗留的 i18n 工具（已统一）
| 文件 | 函数 | 状态 |
|------|------|------|
| `components/ai/aiAssistantI18n.ts` | `localizeText()` | ✅ 已改为 re-export `src/i18n` |
| `utils/hubServiceI18n.ts` | `localizeByLang()` | Hub 专用错误翻译，保留 |
| `components/settings/imSettingsShared.ts` | `textForLang` | ✅ 已改为 `localizeText` alias |
| 各 settings/layout 组件内联 `textForLang()` | 匿名函数 | ✅ 已迁移为 `localizeText` 导入 + alias |

## 文本分类

### 走 i18n 的（用户可见）
- GUI 面板的按钮、标签、标题、提示
- IM 通道的状态/进度/错误消息
- TUI 的界面文本
- 工作流的用户确认/通知
- 错误弹框/Toast

### 不走 i18n 的（LLM 内部通信）
- System prompt 指令文本
- 工具定义 description
- Agent loop 注入的系统消息
- Drift recover / 截断恢复提示
- Steering 规则文件
- 日志消息（固定英文）

## 迁移优先级

### P0（已完成）
- [x] 创建前端统一入口 `src/i18n/index.ts`
- [x] 创建 steering 规则 `.kiro/steering/i18n-guidelines.md`
- [x] `aiAssistantI18n.ts` 改为 re-export 统一入口
- [x] 16 个组件的内联 `textForLang` 迁移到 `localizeText` 导入

### P1（逐步）
- [ ] 盘点 `gui/` 中硬编码用户可见中文字符串，迁移到 `corelib/i18n`
- [ ] 盘点 `gui/frontend/src/` 中剩余的硬编码三语文本，提取到 `appTranslations.ts`

### P2（增强）
- [ ] Go 后端支持 zh-Hant 翻译（当前统一到 zh）
- [ ] 前端 `appTranslations.ts` 补全 zh-Hant 翻译
- [ ] 新增语言支持（ja/ko 等）

## 新增翻译的工作流

### Go 后端新增一条用户可见消息

```go
// 1. 在 corelib/i18n/i18n.go 常量区新增 key
const MsgSkillInstallSuccess = "msg.skill_install_success"

// 2. zh 翻译表
"msg.skill_install_success": "✅ 技能 %s 安装成功",

// 3. en 翻译表
"msg.skill_install_success": "✅ Skill %s installed successfully",

// 4. 使用
msg := i18n.Tf(i18n.MsgSkillInstallSuccess, userLang, skillName)
```

### 前端新增一条 UI 文本

```typescript
// 1. 在 src/i18n/appTranslations.ts 两个表中添加
"en": { ..., "skillInstallSuccess": "Skill installed successfully" }
"zh-Hans": { ..., "skillInstallSuccess": "技能安装成功" }

// 2. 使用
import { t } from '../../i18n';
const msg = t('skillInstallSuccess', lang);
```

### 前端动态文本

```typescript
import { localizeText } from '../../i18n';
const msg = localizeText(lang, 
    `Skill "${name}" installed`, 
    `技能「${name}」已安装`,
    `技能「${name}」已安裝`
);
```

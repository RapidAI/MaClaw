---
inclusion: auto
---

# i18n 多语言本地化规则

## 核心原则

项目中的文本分两类，处理方式完全不同：

### 1. 用户可见文本（必须走 i18n）

- IM 通道状态/进度消息（"任务较复杂，正在处理中"）
- TUI 界面标签、帮助文本、状态栏
- GUI 前端面板文字（按钮、标签、提示、错误提示）
- 工作流引擎的用户确认/通知消息
- 确认对话框文本
- 错误消息（展示给最终用户的）

### 2. LLM 内部通信文本（不走 i18n）

- System prompt 指令
- 工具定义的 description（给 LLM 看的）
- 工具返回给 LLM 的结果消息
- Drift recover prompt / 截断恢复提示
- Steering 规则文件内容
- 注入到 conversation 中的系统消息（如 "请将内容拆分为多次写入"）

**判断标准**：这段文字最终是显示在用户界面/IM 窗口中，还是只在 LLM 的 context 中？前者走 i18n，后者不走。

## Go 后端规范

### 使用 `corelib/i18n` 包

```go
import "github.com/RapidAI/CodeClaw/corelib/i18n"

// 简单翻译
msg := i18n.T(i18n.MsgAckProcessing, userLang)

// 带格式化参数
msg := i18n.Tf(i18n.MsgAgentRoundOf, userLang, current, max)
```

### 新增翻译的步骤

1. 在 `corelib/i18n/i18n.go` 的常量区新增 key：`MsgXxxYyy = "msg.xxx_yyy"`
2. 在 `translations["zh"]` 中添加中文翻译
3. 在 `translations["en"]` 中添加英文翻译
4. 使用 `i18n.T(MsgXxxYyy, lang)` 或 `i18n.Tf(MsgXxxYyy, lang, args...)` 调用

### 禁止行为

- 禁止在 Go 代码中硬编码用户可见的中文/英文字符串
- 禁止在 Go 代码中用 `fmt.Sprintf` 直接拼接用户可见文本（用 `i18n.Tf` 替代）
- 禁止为同一个 key 创建两个不同的常量名

### 语言代码

`i18n.NormalizeLang()` 支持以下映射：
- `"zh"`, `"zh-CN"`, `"zh-Hans"` → `"zh"`
- `"zh-TW"`, `"zh-Hant"` → `"zh"`（Go 后端翻译表统一使用简体，未来如需繁体需新增 `"zh-Hant"` bucket）
- `"en"`, `"en-US"`, `"en-GB"` → `"en"`
- 空字符串 / 未知 → `"zh"`（默认语言）

**注意**：前端 `normalizeLang()` 映射到 `'zh-Hans'`/`'zh-Hant'`/`'en'` 三个值（前端翻译表有繁体 bucket）。两端行为不同是设计如此，不是 bug。

## 前端规范

### 统一入口：`src/i18n/`

前端 i18n 使用两层机制：

#### 层 1: Key-based 翻译表（推荐）

```typescript
import { t } from '../../i18n';

// 从 AppConfig.language 获取当前语言
const text = t('saveChanges', lang); // → "保存并关闭" or "Save & Close"
```

适用于：有固定 key 的 UI 文本（按钮、标签、标题、通用提示等）

#### 层 2: 内联三语函数（动态/一次性文本）

```typescript
import { localizeText } from '../../i18n';

const msg = localizeText(lang, 'Processing...', '处理中...', '處理中...');
```

适用于：动态拼接文本、模板插值、组件内一次性文本

### 新增翻译的步骤

**优先方案（Key-based）**：
1. 在 `src/i18n/appTranslations.ts` 的 `"en"` 和 `"zh-Hans"` 中添加 key
2. 可选：在 `"zh-Hant"` 中添加繁体中文翻译
3. 在组件中通过 `t('key', lang)` 使用

**备选方案（内联三语）**：
- 使用 `localizeText(lang, en, zhHans, zhHant?)` 从 `../../i18n` 导入

### 禁止行为

- 禁止在组件中重复定义 `textForLang` 匿名函数——使用 `localizeText` 导入
- 禁止硬编码用户可见的中文/英文字符串不走任何翻译函数
- 禁止在一个组件中混用多种 i18n 模式

### 注意：`t` 命名冲突

部分 settings 组件有 `t: (key: string) => string` props（已绑定 lang 的翻译函数）。当组件已有 `t` prop 时，导入应使用 `localizeText` 而非 `t`，或用别名 `import { t as i18nT } from '../../i18n'`。

### 语言值来源

前端语言值来自 `AppConfig.language`，可能的值：
- `"zh-CN"` / `"zh-Hans"` → 简体中文
- `"zh-Hant"` / `"zh-TW"` → 繁体中文
- `"en"` → English

## 新增语言的步骤

如果要添加第四种语言（如日文 `"ja"`）：

**重要限制**：`localizeText(lang, en, zhHans, zhHant?)` 只支持三种语言（参数位固定）。新增第四种语言后，所有使用 `localizeText` 的地方**只能显示 en/zh/zhHant 之一**，无法显示日文。因此：
- 新增语言后，优先使用 `t('key', lang)` key-based 模式（翻译表可无限扩展语言）
- `localizeText` 的现有调用在新语言下 fallback 到 zh-Hans（可接受的降级）

### Go 后端
1. `corelib/i18n/i18n.go`：`NormalizeLang` 添加 `"ja"` case
2. `translations` map 新增 `"ja": { ... }` 子表
3. 逐步补全所有 key 的日文翻译

### 前端
1. `src/i18n/appTranslations.ts`：新增 `"ja": { ... }` 翻译表
2. `src/i18n/index.ts`：`normalizeLang()` 新增 `'ja'` → `'ja'` 映射
3. `src/i18n/index.ts`：`t()` 无需修改（自动查找 `translations['ja']` 表）
4. `src/components/settings/GeneralSettingsPanel.tsx`：language selector 新增 `<option value="ja">日本語</option>`
5. 逐步将关键路径的 `localizeText` 调用迁移为 `t('key', lang)` key-based 模式

## 文本分类检查清单

| 场景 | 走 i18n？ | 机制 |
|------|----------|------|
| IM 聊天中的状态进度 | | `i18n.T()` |
| TUI 界面标签 | | `i18n.T()` |
| GUI 按钮/标签/标题 | | `translations[lang][key]` 或 `localizeText()` |
| 错误弹框/Toast | | 同上 |
| 工作流确认面板 | | `i18n.T()` |
| SSH 工具 description | | 硬编码中文（给 LLM 看） |
| Agent loop 注入的系统消息 | | 硬编码中文（给 LLM 看） |
| Steering 规则文件 | | 硬编码中文（给 LLM 看） |
| `buildDriftRecoverPrompt` | | 硬编码中文（给 LLM 看） |
| 日志消息 | | 英文（开发者看的） |

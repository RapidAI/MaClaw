## 🔴 高优先级（影响输出质量）

### 1. 色板映射缺陷 — 13 个色板中 7 个有 `light === bg`

```
Palette 2, 3, 4, 6, 7, 8, 13: light 和 bg 颜色完全相同
Palette 6: secondary === accent 也重复
```

这意味着实际使用中 `theme.light`（用于卡片背景、装饰色块等）和 `theme.bg`（幻灯片底色）**视觉上没有区分**。当 LLM 在浅色底上放一个 `fill: theme.light` 的卡片时，它会是**隐形的**。

**根因**: design-system.md 的色板只有 5 个颜色，但 theme 需要 5 个**功能不同**的槽位（primary/secondary/accent/light/bg），当原始色板只有 4 种实际色值时就不够分了。

**建议**: 为每个色板手动补充一个区分度足够的第 6 色，或改用 4-key theme 体系。

### 2. pitfalls.md 自相矛盾 — 禁用 accent line 但模板/示例大量使用

pitfalls.md 第 35 行和第 71 行两次强调：
> **NEVER use accent lines under titles** — these are a hallmark of AI-generated slides

但同一技能的 `slide-template.js`（官方模板）第 67-70 行就写了一条 accent line，`utils.js` 也专门提供了 `addAccentBar()` 函数，并且 design-system.md 的 Style Recipes 里也有 accent bar 的用法。刚才生成的 10 页幻灯片中有 **28 处** `theme.accent` 的装饰线。

这让 LLM 无所适从——遵循 pitfalls 就不能用 utils，用 utils 就违反 pitfalls。

**建议**: 二选一——要么删除 pitfalls 中的这条禁令（accent line 是很常见的设计元素），要么从模板和 utils 中移除。

### 3. utils.js 完全没被使用 — 形同虚设

实际生成 10 页幻灯片时，**零个 slide 文件** `require('./utils')`，而是全部手动内联了页码 badge、装饰条、accent bar 等代码。每个 slide 文件都重复了 16 行相同的 badge 代码（circle + number）。

**根因**: 
- SKILL.md 对 utils 的指引太弱（只有一句 "Tip: use utilities from utils.js"）
- slide-template.js 也没有用 utils（它自己内联了 badge）
- LLM 看到模板不用，自然也不跟着用

**建议**: 
1. slide-template.js 应该带头使用 utils
2. SKILL.md 将 "use utils" 从建议升级为**规则**（类似 Chinese font 的 MANDATORY 标注）

---

## 🟡 中优先级（影响效率和可维护性）

### 4. 缺少声明式 slide 内容接口

当前每页 slide 都是 **150-400 行的命令式代码**，手写 `addShape`、`addText`，坐标、字号逐个硬编码。这带来：
- **代码量大**: 10 页 = ~700 行手写代码，Token 消耗高
- **出错率高**: 中文引号（如 slide-03 的 `"六绝"`）会破坏字符串解析
- **样式不统一**: 页码 badge、decor bar 等细节每页都不一样

**建议**: 在 utils.js 或 compile.js 中提供更高层的布局函数，例如：

```javascript
// 声明式用法
createSlide(pres, theme, pageNum, {
  layout: 'image-left-text-right',
  image: { placeholder: true, label: '九寨沟' },
  title: '人间仙境 · 九寨沟',
  body: '...',
  tags: ['翠海叠瀑', '彩林雪峰'],
  stats: [{ value: '2000-3100m', label: '海拔范围' }]
});
```

这样 LLM 只需要写 10-20 行声明式配置而不是 300 行坐标代码。

### 5. compile.js 缺少中文引号安全处理

slide-03.js 中 `"六绝"` 的中文引号导致 SyntaxError，这类错误在实际使用中会反复出现（中文文本天然包含 `""` `''` `《》` 等字符）。

**建议**: 
- 在 pitfalls.md 中增加 "中文引号/特殊字符" 条目
- 或在 compile.js 中增加预检步骤，catch 语法错误时给出更友好的提示

### 6. 缺少图片占位符的标准方案

当前 slide 文件用色块模拟图片占位（如 `fill: { color: theme.secondary }`），但没有标准做法。每个 slide 作者用的占位方式不同（有的用 ROUNDED_RECTANGLE、有的用普通 RECTANGLE），颜色也不统一。

**建议**: 在 utils.js 中提供 `addImagePlaceholder(slide, pres, theme, { x, y, w, h, label })` 标准函数。

### 7. SKILL.md 缺少 image 支持的引导

pptxgenjs.md 中有完整的 `addImage` API 文档（path/URL/base64/sizing），但 SKILL.md 主流程中**只字未提图片**。实际 PPT 中图片是最重要的视觉元素，LLM 根本不知道可以插入图片。

**建议**: 在 Step 3 和 slide-types.md 中增加图片插入的标准工作流。

---

## 🟢 低优先级（锦上添花）

### 8. run.js compile 缺少 `--slides-dir` 参数

compile 目前硬编码为扫描 `slides/` 子目录。如果用户想编译不同的 slide 集（如 A/B 方案对比），只能手动替换文件。

### 9. 缺少 slide 模块语法预检

compile.js 在 `require()` 时才报语法错误，错误信息不够友好（如刚才的中文引号问题）。可以在加载前用 `node -c` 做一轮预检，汇总所有语法错误后再报出。

### 10. design-system.md 的色板表和 compile.js 存在两份维护

色板定义同时存在于 `design-system.md`（给 LLM 看的参考）和 `compile.js`（实际执行）。如果修改一处忘记改另一处就会不一致。可以考虑让 compile.js 读取一个 JSON 色板文件，design-system.md 也从同一来源生成。

---

## 📊 改进优先级总结

| # | 改进点 | 严重程度 | 改动量 | 影响 |
|---|--------|---------|--------|------|
| 1 | 色板 light/bg 重复 | 🔴 高 | 小 | 7/13 色板不可用 |
| 2 | pitfalls 与模板矛盾 | 🔴 高 | 小 | LLM 行为冲突 |
| 3 | utils.js 未被使用 | 🔴 高 | 中 | 代码重复，badge 不统一 |
| 4 | 缺少声明式布局接口 | 🟡 中 | 大 | Token 消耗、出错率 |
| 5 | 中文引号安全性 | 🟡 中 | 小 | 编译报错 |
| 6 | 图片占位符标准 | 🟡 中 | 小 | 视觉不一致 |
| 7 | 缺少图片插入引导 | 🟡 中 | 小 | 功能缺失 |
| 8 | --slides-dir 参数 | 🟢 低 | 小 | 灵活性 |
| 9 | 语法预检 | 🟢 低 | 小 | 用户体验 |
| 10 | 色板单一来源 | 🟢 低 | 中 | 可维护性 |

要我开始动手修复其中的某些问题吗？
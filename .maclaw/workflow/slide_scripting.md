好的，我现在开始逐个改进每个文件。按照分析结果，我的改进计划如下：

**核心改进点：**
1. **SKILL.md** — compile.js 增加 auto page numbering；补充 chart/table 类型内容页指引
2. **slide-types.md** — 修正 px→pt 单位不一致；为 Content 页补充 PptxGenJS 代码示例
3. **pitfalls.md** — 补充文本换行、字体一致性、中文字体强制规则
4. **design-system.md** — 补充 palette→theme 映射表（当前色板只列颜色没映射到 5 键）
5. **editing.md** — 补充完整的 unpack/pack Python 脚本
6. **pptxgenjs.md** — 补充 fit:"shrink" 用法、禁止 gradient 说明
7. **skill.yaml** — 版本升级

先从最关键的改进开始：

### 改进 1: SKILL.md — 完善 compile.js 模板 + 自动页码
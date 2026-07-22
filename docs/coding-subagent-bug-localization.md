# CodingSubAgent 故障定位

本地和远端 CodingSubAgent 使用相同的故障定位协议：

1. 从用户描述提取错误文本、堆栈、入口、期望/实际行为和复现信息。
2. 优先调用 `code_navigation`。后端顺序为 CodeGraph、语言服务器（`gopls`/`lsp-cli`）、`rg`；远端采用相同顺序并最终回退 `grep`。
3. 将匹配转换为带支持证据、反证、下一探针和可解释分数的 Top-10 候选。
4. 对陌生概念/精确报错、第三方依赖/API/协议、版本或兼容性事实，调用 `web_search`（精确错误 + 组件/版本），并优先用 `web_fetch` 核对官方来源；纯仓内逻辑问题明确记录无需联网的理由。
5. 调用 `report_localization` 提交根因文件/符号、因果路径、复现、支持证据、排除假设、focused tests、`research_decision`、来源和置信度。
5. Bug 任务在修改已有文件前执行硬门禁；修改目标必须是根因、带支持证据的候选，或报告中明确列出的回归测试。
6. 任务结束时再次审计定位证据；成功修复写入项目级 coding knowledge，后续任务可通过现有 `coding_knowledge_search` 召回。

## 评测

JSONL 每行使用 `needledata.LocalizationPrediction`。运行：

```powershell
go run ./cmd/maclaw-needle eval-localization -in data/needle/localization_eval_example.jsonl
```

报告包含 File Hit@1/3/5、Function Hit@1/3、MRR，以及首次定位所用工具调用数、耗时和无关读取数的中位数。真实评测集应从历史 issue/修复提交生成，不应把示例数据作为质量证明。

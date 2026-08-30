from pathlib import Path
d=Path(r"D:\workprj\aicoder\docs\design\document-generate-semantic-slice-zh.md")
t=d.read_text(encoding="utf-8")

old="`WorkflowAgentLoop` / `WorkflowDocPhase` 为真时：在 `imSemanticIntentCoverage` **之前**从本轮 Labels 去掉 `document_generate`。不要只在 resolver 里跳过模板。去掉后若不再 managed，`handled=false`，回落 legacy 工作流 `generate_pdf`。其它已迁移标签（搜索、截图）仍走 semantic。不要关掉整次 semantic 路由。"
new="`WorkflowAgentLoop` / `WorkflowDocPhase` 为真时：从本轮 Labels 派生 **routing classification**（去掉 `document_generate`），供三处共用：① `runAgentLoop` 的 `imSemanticIntentIsManaged`（否则非 pilot 工作流会被强制 shared）；② `imSemanticIntentCoverage`；③ need 展开。存盘的 `Runtime.SemanticIntent` 保持 UIC 原文。派生结果不 managed → `handled=false`，工作流环选择与今天一致。其它已迁移标签仍走 semantic。"
assert old in t, "4.3"
t=t.replace(old,new,1)

old="按 `screenshot` 的 `annotateSemanticTool`："
new="按 `screenshot` 的 `annotateSemanticTool`。**只**标注别名 `generate_pdf`，不要给已标注 `document.write.office(format=spreadsheet)` 的 `office` 再挂 PDF provision，以免 FitProof 选中仍返回 `[file_base64]` 的合并工具："
assert old in t, "4.5"
t=t.replace(old,new,1)

old="禁止把整个 `office` 挂到该 capability。Excel 写入、读文档、PPT 各要自己的 provision。未授权 effect 不得经 adapter 偷运。legacy `office` 在 unmanaged 路径可暂留，**不得**作为本切片 managed 面的后备写通道。"
new="禁止把整个 `office` 挂到 `document.generate.file`。Excel 写入保持现有 spreadsheet provision。legacy `office(action=generate_pdf)` 仅作 `generate_pdf` handler 的内部渲染实现，不出现在 semantic catalog。unmanaged 路径可暂留 office，**不得**作为本切片 managed 面的后备写通道。"
assert old in t, "office"
t=t.replace(old,new,1)

old="Classify 只应发生一次，且在 `classifyIMExecutionProfileAndSemanticContext`：必须传入 `MessageContext.RecentHistory`。semantic 面通过 `loopCtx.Runtime.SemanticIntent` 复用这次结果，不要再无 history 地 Classify 一次。现在这两处都只传了 Text/UserID。"
new="Classify 只应发生一次，且在 `classifyIMExecutionProfileAndSemanticContext`。今天 `executePreparedIMEntry` 在 `drainHistory()` **之前** Classify，RecentHistory 实际为空。应与 `prepareIMLoopContext` 并行加载 history，**先 drain 再 Classify**，并把最近若干轮 user/assistant 原文传入 `MessageContext.RecentHistory`。semantic 面只复用 `loopCtx.Runtime.SemanticIntent`。"
assert old in t, "5.1"
t=t.replace(old,new,1)

old="- `semanticCallSurfaceForSharedTurn*` 返回 `handled=true` 且 `err != nil` 时，`prepareAgentLoopStartState` **不得**再把 `tools=nil` 交给后续 LLM 循环（今天正是这条路径）。宿主构造一条助手消息后结束本轮。"
new="- `semanticCallSurfaceForSharedTurn*` 返回 `handled=true` 且 `err != nil` 时：`prepareAgentLoopStartState` 写入 `HostReject *IMAgentResponse`（文案见下），`tools` 保持空。`runAgentLoopShared` / legacy 在 `RunLoopWithUserContent` 或主迭代 **之前**若 `HostReject != nil` 则直接返回该响应。禁止只把 `tools=nil` 丢给模型。"
assert old in t, "5.6"
t=t.replace(old,new,1)

old="- **排除**：`WorkflowAgentLoop` / `WorkflowDocPhase` 在 coverage **之前**从 Labels 去掉 `document_generate`；去掉后不 managed 则 `handled=false`，阶段 PDF 回落现有 `generate_pdf` / `[file_base64]`。其它已迁移能力可继续 semantic。`coding` 工作流确认门不因「报告」两个字吃掉天气+PDF。"
new="- **排除**：工作流回合用派生 routing classification（去掉 `document_generate`）做 IsManaged/coverage/展开，以免非 pilot 工作流被强制 shared。去掉后不 managed 则 `handled=false`，环选择与今天一致，阶段 PDF 仍走 `[file_base64]`。其它已迁移能力可继续 semantic。"
assert old in t, "2.1"
t=t.replace(old,new,1)

old="| 16 | workflow-pilot 且 UIC 仅 document_generate | handled=false，回落 legacy generate_pdf |"
new="| 16 | 非 pilot `WorkflowAgentLoop` 且 UIC 为 document_generate | 不得因 IsManaged 强制 shared；handled=false，仍走今天的 legacy 工作流环 |\n| 17 | 只 annotate `generate_pdf` | catalog 不得出现 office 作为 document.generate.file 的 provider |"
assert old in t, "acc"
t=t.replace(old,new,1)

old="9. **工作流闸门**：coverage 前从 Labels 去掉 document_generate；needs 为空则 handled=false。\n10. **入站绑定**：file deliver 仅在没有 document.generate.file 时要求附件。"
new="9. **工作流闸门**：派生 routing classification，三处共用（dispatcher IsManaged、coverage、展开）。\n10. **入站绑定**：file deliver 仅在没有 document.generate.file 时要求附件。\n11. **入口时序**：先 drainHistory 再 Classify。\n12. **HostReject**：startState 带宿主响应，第一次 LLM 之前返回。\n13. **只 annotate generate_pdf**，不给 office 加 PDF provision。"
assert old in t, "land"
t=t.replace(old,new,1)

old="9. 入站绑定若漏改，无附件的天气+PDF 会在 planner 前以 trusted_document_input_missing 失败；这是本切片的发布阻断项。"
new="9. 入站绑定若漏改，无附件的天气+PDF 会在 planner 前以 trusted_document_input_missing 失败；这是发布阻断项。\n10. 若 dispatcher 仍用未过滤 SemanticIntent 做 IsManaged，非 pilot 工作流会被塞进 shared loop；这也是发布阻断项。\n11. Classify 若仍在 drainHistory 之前，continuation 回放在实现上等于没做。"
assert old in t, "risk"
t=t.replace(old,new,1)

d.write_text(t, encoding="utf-8", newline="\n")
print("ok", len(t))
for s in ["routing classification", "drainHistory", "HostReject", "只标注别名"]:
    print(s, s in t)
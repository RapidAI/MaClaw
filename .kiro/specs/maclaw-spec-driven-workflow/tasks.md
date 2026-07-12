# Implementation Plan: MaClaw Spec-Driven Workflow

## Overview

通过修改 `gui/im_message_handler.go` 中 `buildSystemPrompt()` 方法的"编程任务工作流"文本块，将现有的五步工作流（识别 → 跳过 → 确认 → 执行 → RFO）替换为八步 Spec 驱动工作流（识别 → 跳过 → 需求文档 → 设计文档 → 任务分解 → 执行 → 验收 → 续接）。纯提示词驱动方案，不新增 Go 代码中的状态机或工具。

## Tasks

- [x] 1. 替换 buildSystemPrompt() 中的编程任务工作流指令
  - [x] 1.1 替换 `im_message_handler.go` 中 `## 编程任务工作流（极其重要）` 部分为新的八步工作流指令
    - 定位 `buildSystemPrompt()` 方法中从 `### 第一步：识别任务类型` 到 `### 第五步：任务完成后 Review/Fix/Optimize（RFO Phase）` 结束（即 `### 第六步：自动续接` 之前）的文本块
    - 替换为设计文档中定义的新八步工作流指令，包含：
      - 第一步：识别任务类型（保留现有 Coding_Task vs 非编程任务区分，不变）
      - 第二步：检查跳过信号（Skip_Signal），扩展为跳过三个确认阶段，中英文双语模式
      - 第三步：需求确认（Requirements Phase），包含文档内容要求（需求背景与目标、功能需求列表、非功能需求、约束与假设）、PDF 生成与发送流程、确认规则、PDF 失败回退
      - 第四步：技术设计（Design Phase），包含文档内容要求（架构设计、接口设计、数据模型变更、实现方案概述）、PDF 生成与发送、确认规则、回退到需求阶段
      - 第五步：任务分解（TaskBreakdown Phase），包含文档内容要求（编号任务列表、任务描述和涉及文件、TDD 验收测试用例）、PDF 生成与发送、确认规则、回退到需求或设计阶段
      - 第六步：任务执行（Execution Phase），包含按顺序执行、create_session + send_and_observe 附带需求/设计上下文、TDD 测试验证、3 次重试、失败跳过、进度汇报格式
      - 第七步：完成验收（Verification Phase），包含全量 TDD 测试、完成报告（总任务数/成功失败数、每个任务结果、全量测试结果、失败摘要）、成功/失败报告
      - 第八步：自动续接（Auto-Resume），保留现有内容不变（原第六步重编号为第八步）
    - 保留现有的 `## 执行验证原则`、`## 会话失败止损原则`、`## 工具使用要点` 等后续部分不变
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 2.1, 2.2, 2.3, 2.4, 2.5, 3.1, 3.2, 3.3, 3.4, 3.5, 4.1, 4.2, 4.3, 4.4, 4.5, 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 6.1, 6.2, 6.3, 6.4, 6.5, 6.6, 7.1, 7.2, 7.3, 7.4, 8.1, 8.2, 8.3, 8.4, 8.5, 9.1, 9.2, 9.3, 9.4, 9.5, 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 11.1, 11.2, 11.3_

  - [x]* 1.2 Write property test: 五阶段顺序完整性
    - **Property 1: 五阶段顺序完整性**
    - **Validates: Requirements 1.4, 10.1, 10.2**
    - 在 `gui/im_message_handler_spec_workflow_test.go` 中使用 `testing/quick` 验证 `buildSystemPrompt()` 输出中五个阶段按严格顺序出现：Requirements Phase → Design Phase → TaskBreakdown Phase → Execution Phase → Verification Phase，且每个阶段恰好出现一次

  - [x]* 1.3 Write property test: Spec 工作流在 create_session 之前
    - **Property 2: Spec 工作流在 create_session 之前**
    - **Validates: Requirements 1.1**
    - 验证 `buildSystemPrompt()` 输出中三个确认阶段（Requirements、Design、TaskBreakdown）的指令出现在 Execution Phase 的 `create_session` 指令之前

  - [x]* 1.4 Write property test: 编程任务与非编程任务的区分
    - **Property 3: 编程任务与非编程任务的区分**
    - **Validates: Requirements 1.2, 1.3, 10.4**
    - 验证 `buildSystemPrompt()` 输出包含 Coding_Task 与非编程任务的区分标准，并明确列出非编程任务类别（信息检索、翻译、文档生成、文件操作、通信、日常助手）

  - [x]* 1.5 Write property test: 三个确认阶段的文档内容要求
    - **Property 4: 三个确认阶段的文档内容要求**
    - **Validates: Requirements 2.1, 3.1, 4.1**
    - 验证 `buildSystemPrompt()` 输出包含三个确认阶段各自的文档内容要求：(a) Requirements Phase 包含需求背景与目标、功能需求列表、非功能需求、约束与假设, (b) Design Phase 包含架构设计、接口设计、数据模型变更、实现方案概述, (c) TaskBreakdown Phase 包含编号任务列表、任务描述和涉及文件、TDD 验收测试用例

  - [x]* 1.6 Write property test: PDF 生成与发送指令
    - **Property 5: PDF 生成与发送指令**
    - **Validates: Requirements 2.2, 2.5, 3.2, 3.5, 4.2, 4.5, 8.1, 8.2, 8.3, 8.4, 8.5**
    - 验证 `buildSystemPrompt()` 输出包含：(a) PDF 生成指令引用 craft_tool 或 bash, (b) send_file with forward_to_im=true, (c) 描述性 PDF 命名, (d) PDF 旁附文字摘要, (e) PDF 生成失败时回退为格式化文本

  - [x]* 1.7 Write property test: 阶段确认与修订规则
    - **Property 6: 阶段确认与修订规则**
    - **Validates: Requirements 2.3, 2.4, 3.3, 3.4, 4.3, 4.4, 9.5**
    - 验证 `buildSystemPrompt()` 输出包含：(a) 等待用户确认后才进入下一阶段, (b) 用户修改时更新文档并重新生成 PDF, (c) 修订后使用最新版本作为后续阶段输入

  - [x]* 1.8 Write property test: Skip_Signal 双语模式与三阶段跳过
    - **Property 7: Skip_Signal 双语模式与三阶段跳过**
    - **Validates: Requirements 7.1, 7.2, 7.3, 7.4**
    - 验证 `buildSystemPrompt()` 输出包含：(a) 中文 Skip_Signal（直接做、不用问了等）和英文 Skip_Signal（just do it、go ahead 等）, (b) 跳过三个确认阶段的指令, (c) 跳过时仍进行内部规划, (d) 阶段中途收到跳过信号时跳过剩余阶段

  - [x]* 1.9 Write property test: Execution Phase TDD 验证与重试
    - **Property 8: Execution Phase TDD 验证与重试**
    - **Validates: Requirements 5.3, 5.4, 5.5, 5.6**
    - 验证 `buildSystemPrompt()` 输出包含：(a) 每个任务完成后运行 TDD 测试, (b) 测试失败最多重试 3 次, (c) 重试耗尽后跳到下一个任务, (d) 进度汇报格式（如"任务 X/Y 完成 "或"任务 X/Y 失败 "）

  - [x]* 1.10 Write property test: Verification Phase 完成报告
    - **Property 9: Verification Phase 完成报告**
    - **Validates: Requirements 6.1, 6.2, 6.3, 6.4, 6.5, 6.6**
    - 验证 `buildSystemPrompt()` 输出包含：(a) 运行全量 TDD 测试, (b) 完成报告组件（总任务数/成功失败数、每个任务结果、全量测试结果、失败摘要）, (c) 全部通过时的成功报告, (d) 有失败时列出失败项并建议下一步

  - [x]* 1.11 Write property test: 阶段间上下文传递
    - **Property 10: 阶段间上下文传递**
    - **Validates: Requirements 9.1, 9.2, 9.3, 5.2**
    - 验证 `buildSystemPrompt()` 输出包含阶段间上下文传递指令：需求 → Design Phase, 需求+设计 → TaskBreakdown Phase, 需求+设计+任务列表 → Execution Phase（通过 send_and_observe 传递给 Remote_Coding_Tool）

  - [x]* 1.12 Write property test: 阶段回退机制
    - **Property 11: 阶段回退机制**
    - **Validates: Requirements 11.1, 11.2, 11.3**
    - 验证 `buildSystemPrompt()` 输出包含：(a) 用户请求回退时返回之前阶段, (b) 回退后重新生成所有后续阶段文档, (c) 告知用户回退信息

  - [x]* 1.13 Write property test: 现有工作流规则保留
    - **Property 12: 现有工作流规则保留**
    - **Validates: Requirements 10.5**
    - 验证 `buildSystemPrompt()` 输出仍包含：(a) 会话失败止损原则, (b) 执行验证原则, (c) busy 会话不终止规则, (d) 自动续接（Auto-Resume）规则

- [x] 2. Checkpoint - 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- 核心实现仅涉及一个文件 (`gui/im_message_handler.go`) 中一个文本块的替换
- 不新增任何 Go 结构体、工具或状态机
- Property tests 在 `gui/im_message_handler_spec_workflow_test.go` 中实现，复用 `im_message_handler_coding_workflow_test.go` 中的 `randomAppConfig` 生成器和 `buildPromptForConfig` 辅助函数
- 所有 property tests 使用 `testing/quick` 包，每个属性至少 100 次迭代
- 每个 correctness property 对应一个独立的 property-based test 函数

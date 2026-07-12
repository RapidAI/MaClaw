# Implementation Plan: Coding Interaction Workflow

## Overview

通过修改 `im_message_handler.go` 中 `buildSystemPrompt()` 方法的"编程任务工作流"文本块，为 MaClaw Agent 增加 Confirmation Phase（需求确认）和 RFO Phase（Review/Fix/Optimize）两个交互环节。纯提示词驱动方案，不新增 Go 代码中的状态机或工具。

## Tasks

- [x] 1. 替换 buildSystemPrompt() 中的编程任务工作流指令
  - [x] 1.1 替换 `im_message_handler.go` 中 `## 编程任务工作流（极其重要）` 部分为新的五步工作流指令
    - 定位 `buildSystemPrompt()` 方法中从 `## 编程任务工作流（极其重要）` 到 `## 执行验证原则` 之前的文本块
    - 替换为设计文档中定义的新工作流指令，包含：
      - 第一步：识别任务类型（Coding_Task vs 非编程任务，明确列出非编程任务示例）
      - 第二步：检查跳过信号（Skip_Signal），包含中英文双语模式
      - 第三步：需求确认（Confirmation Phase），包含确认消息格式和确认阶段规则
      - 第四步：执行编程任务（保留现有 create_session → send_and_observe → 跟踪进度流程，整合确认阶段的需求理解）
      - 第五步：任务完成后 RFO Phase，包含 RFO 询问格式、三个选项（Review/Fix/Optimize）、多选顺序执行规则、失败跳过规则
    - 保留现有的 `## 执行验证原则`、`## 会话失败止损原则`、`## 工具使用要点` 等后续部分不变
    - _Requirements: 1.1, 1.2, 1.5, 2.1, 2.2, 2.3, 3.2, 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 5.1, 5.2, 5.3, 6.1, 6.2, 6.3, 6.4, 6.5, 7.1, 7.2, 7.3_

  - [x]* 1.2 Write property test: Confirmation Phase 在 create_session 之前
    - **Property 1: Confirmation Phase 在 create_session 之前**
    - **Validates: Requirements 1.1, 6.1**
    - 在 `im_message_handler_coding_workflow_test.go` 中使用 `testing/quick` 验证 `buildSystemPrompt()` 输出中确认阶段指令出现在 create_session 执行指令之前

  - [x]* 1.3 Write property test: 确认消息包含所有必需组件
    - **Property 2: 确认消息包含所有必需组件**
    - **Validates: Requirements 1.2**
    - 验证 `buildSystemPrompt()` 输出包含需求理解、实现方案、边界情况三个确认消息组件的指令

  - [x]* 1.4 Write property test: 编程任务与非编程任务的区分
    - **Property 3: 编程任务与非编程任务的区分**
    - **Validates: Requirements 1.5, 6.4, 7.1, 7.3**
    - 验证 `buildSystemPrompt()` 输出包含 Coding_Task 与非编程任务的区分标准，并明确列出非编程任务示例

  - [x]* 1.5 Write property test: Skip_Signal 双语模式
    - **Property 4: Skip_Signal 双语模式**
    - **Validates: Requirements 2.1, 2.3, 6.3**
    - 验证 `buildSystemPrompt()` 输出包含中文和英文的 Skip_Signal 模式

  - [x]* 1.6 Write property test: RFO 工作流完整性
    - **Property 5: RFO 工作流完整性**
    - **Validates: Requirements 4.1, 4.2, 5.1, 5.2, 6.2**
    - 验证 `buildSystemPrompt()` 输出包含 RFO 触发条件、三个选项、Review → Fix → Optimize 顺序执行规则

  - [x]* 1.7 Write property test: 任务失败时跳过 RFO
    - **Property 6: 任务失败时跳过 RFO**
    - **Validates: Requirements 4.6**
    - 验证 `buildSystemPrompt()` 输出包含任务失败（非零退出码或 error 状态）时跳过 RFO 的指令

  - [x]* 1.8 Write property test: 现有工作流规则保留
    - **Property 7: 现有工作流规则保留**
    - **Validates: Requirements 6.5**
    - 验证 `buildSystemPrompt()` 输出仍包含会话失败止损原则、执行验证原则、busy 会话不终止规则

- [x] 2. Checkpoint - 确保所有测试通过
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- 核心实现仅涉及一个文件 (`im_message_handler.go`) 中一个文本块的替换
- 不新增任何 Go 结构体、工具或状态机
- Property tests 验证系统提示词的结构完整性，确保提示词变更不遗漏关键工作流指令
- 所有 property tests 使用 `testing/quick` 包，生成随机 App 配置验证跨配置的通用属性

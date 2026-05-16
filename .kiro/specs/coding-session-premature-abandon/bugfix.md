# Bugfix Requirements Document

## Introduction

在飞书远程模式下，MaClaw Agent 启动编程会话（如 Claude Code）并发送编程指令后，编程工具在后台执行复杂任务（如 TodoWrite、文件写入等步骤）时，Agent 因等待超时而误判会话无响应，提前放弃编程会话并自行接管用 write_file/bash 工具直接写代码。

根本原因是多层面的：
1. `send_and_observe` 工具仅等待约 8 秒就返回，对于复杂编程任务（如编写完整的 C++ 贪吃蛇游戏）远远不够
2. 系统提示词缺乏对长时间运行编程任务的处理指导，且"不要反复轮询 get_session_output"的措辞让 Agent 不敢检查忙碌中的会话
3. `get_session_output` 在会话状态为 `busy` 时不提供任何等待提示，Agent 无法判断应该继续等待
4. Agent 迭代次数限制压力导致 Agent 倾向于放弃等待、自行完成任务

此 bug 导致用户体验严重下降：编程工具的专业能力被浪费，Agent 自行编写的代码质量通常不如专业编程工具的输出。

## Bug Analysis

### Current Behavior (Defect)

1.1 WHEN Agent 通过 `send_and_observe` 向编程会话发送复杂编程指令（需要数分钟完成）THEN `send_and_observe` 在约 8 秒后返回部分输出，会话状态仍为 `busy`，Agent 缺乏后续等待策略

1.2 WHEN 编程会话状态为 `busy`（编程工具正在执行 TodoWrite 等步骤）且 Agent 调用 `get_session_output` THEN 返回结果中不包含任何提示告知 Agent 应继续等待，Agent 误判为会话卡死

1.3 WHEN 系统提示词指示"不要反复轮询 get_session_output"THEN Agent 在编程会话仍在工作时不敢再次检查进度，导致过早放弃会话

1.4 WHEN Agent 因等待超时或迭代次数压力而放弃编程会话 THEN Agent 调用 `control_session(action: kill)` 终止正在工作的编程会话，然后自行用 `write_file` 和 `bash` 工具完成任务

### Expected Behavior (Correct)

2.1 WHEN Agent 通过 `send_and_observe` 向编程会话发送复杂编程指令 THEN `send_and_observe` SHALL 提供足够的等待时间（或支持可配置的超时参数），使编程工具有合理时间产出初始结果

2.2 WHEN 编程会话状态为 `busy` 且 Agent 调用 `get_session_output` THEN 返回结果 SHALL 包含明确提示（如"⏳ 编程工具正在工作中，请稍后再检查进度"），引导 Agent 耐心等待而非放弃会话

2.3 WHEN 系统提示词指导 Agent 处理编程会话 THEN 系统提示词 SHALL 明确区分"快速操作"和"长时间编程任务"，并指导 Agent 对长时间任务定期（如每 15-30 秒）检查进度，而非完全禁止轮询

2.4 WHEN 编程会话状态为 `busy` 且编程工具仍在正常工作 THEN Agent SHALL 继续等待并定期检查进度，不得提前终止会话或自行接管编码工作

### Unchanged Behavior (Regression Prevention)

3.1 WHEN 编程会话状态为 `exited` 且退出码非 0 THEN 系统 SHALL CONTINUE TO 立即停止对该会话的操作并告知用户错误信息（止损原则不变）

3.2 WHEN 编程会话状态为 `waiting_input`（编程工具等待用户输入）THEN `send_and_observe` SHALL CONTINUE TO 正常发送输入并返回结果

3.3 WHEN 用户发送简单文件/命令操作（如"查看某文件内容"）THEN Agent SHALL CONTINUE TO 直接使用 bash/read_file/write_file 等工具，不创建编程会话

3.4 WHEN `send_and_observe` 在等待期间检测到会话已退出或等待输入 THEN SHALL CONTINUE TO 立即返回结果，不继续等待

3.5 WHEN 编程会话真正卡死（长时间无任何新输出且状态未变化）THEN Agent SHALL CONTINUE TO 有能力终止会话并告知用户

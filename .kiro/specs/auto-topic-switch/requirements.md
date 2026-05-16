# 自动话题切换 — 需求文档

## 背景

maclaw 用户在长时间对话后，conversationMemory 中积累了大量与当前话题无关的历史上下文，导致 LLM 注意力分散、绕圈子、不执行用户的实际请求。目前只能通过 `/new` 命令手动清理，但普通用户不知道也不会用这个命令。

## 核心需求

当用户发送的新消息与当前对话话题明显不相关时，系统自动清理 conversationMemory，等效于静默开了一个新对话。长期记忆（self_identity、user_fact、preference 等）保持不变。

## 功能要求

### FR-1: 话题相关性检测（BM25 快速判断）
- 每次新消息进来，用 BM25 计算新消息与最近 N 轮对话内容的相似度
- 如果相似度高于阈值 → 判定为同一话题，正常追加
- 如果相似度低于阈值 → 进入模糊地带，触发 LLM 确认

### FR-2: 话题相关性检测（LLM 精确确认）
- 当 BM25 判定为模糊地带时，用一个极短的 LLM 调用确认
- prompt 只需要回答 "same" 或 "new"，消耗约 50-100 tokens
- 如果 LLM 判定为 new → 自动清理 conversationMemory

### FR-3: 自动清理行为
- 清理 conversationMemory（当前对话轮次）
- 保留 memoryStore 中的所有长期记忆
- 清理前，将当前对话生成一句话摘要存入 memoryStore（category: conversation_summary）
- 用户无感知，不需要任何提示或确认

### FR-4: 时间衰减因子
- 如果距离上次消息超过 30 分钟，降低"同一话题"的判定阈值
- 时间越长，越倾向于判定为新话题

### FR-5: 保持 /new 命令
- 手动 `/new` `/reset` `/clear` 命令继续保留，作为用户主动清理的手段

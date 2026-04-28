# iWorkerCenter GoalWatch 与 Push 机制

## 定位

GoalWatch 是 iWorkerCenter 的组织执行守护机制。它负责持续观察已经进入执行网络的目标、任务、A2A 协商和流程状态，在发现停滞、执行 agent 离线或协作断裂时，向对应 iWorker 发出可审计的 push。

它不是老板/董事会的控制中心。老板和董事会负责方向、约束、重大决策和资源边界；iWorkerCenter 负责把既定目标维持在可执行状态；iWorker 和真人员工负责具体执行、反馈、协商与经验沉淀。

## 为什么需要 GoalWatch

AI Native 组织不能假设一次 LLM 调用会持续推进到任务完成。现实中，LLM 可能因为上下文丢失、工具调用失败、网络异常、服务限流、推理中断、模型幻觉或简单“忘了继续”而停下。

如果 iWorkerCenter 只负责分派任务，不负责持续检测目标进展，组织会退回到“人盯人”的运行方式。GoalWatch 的作用是让组织自己拥有执行心跳：定期检查目标、任务、协商和流程状态，在低风险场景下推动恢复，在高风险场景下升级为 A2A 协商或人类 judgement node。

## 当前实现范围

第一阶段聚焦协作任务停滞和执行 agent 可用性：

- 定期扫描非终态任务：`pending`、`accepted`、`in_progress`。
- 如果任务 `updated_at` 超过停滞阈值，则生成 `goal_push`。
- 如果任务分配给某个 iWorker，但该 iWorker 没有健康的 `executor` agent，也会生成 `assigned_executor_offline` push。
- 冷却期内已经 push 过的任务不会重复刷屏。
- `goal_push` 和 `goal_push_ack` 都写入协作事件表，形成可审计记录。
- iWorker 客户端拉取自己的待处理 push，并只对低风险 `restart_executor` 做自动恢复。

默认参数：

- 停滞阈值：5 分钟。
- Push 冷却期：10 分钟。
- 后台检查周期：1 分钟。
- 每个 watcher shard 默认覆盖 50 个 iWorker。
- 单租户默认最多 16 个 watcher shard。

## 多 watcher 与热备安全

iWorkerCenter 可以多机热备，GoalWatch 也不能是单实例瓶颈。当前机制按租户内 iWorker 数量自动计算 watcher shard 数量：

- `known_iworker_count` 来自 `agentruntime.ListWithHealth` 中去重后的 `worker_id` 数量。
- `shard_count = ceil(iworker_count / workers_per_shard)`。
- `shard_count` 最小为 1，最大受 `max_watchers` 限制。
- 每个任务根据任务 ID 的 FNV hash 归属固定 shard，多个 shard 并发检查，避免全公司任务堆在一个 watcher 上。

多机热备下，多个 iWorkerCenter 节点可能同时运行 GoalWatch。为避免重复 push，`goal_push` 使用确定性事件 ID：

```text
gpush_<hash(tenant_id | task_id | cooldown_bucket)>
```

协作事件表的主键约束会自然去重：同一租户、同一任务、同一冷却窗口内，多个节点同时插入时只有一个成功，其他节点把唯一键冲突视为“已由别的 watcher 处理”，不再报错。这让热备节点可以积极运行，而不是依赖脆弱的单主锁。

## iWorker 本地 watcher 的边界

iWorker 本地电脑只是 iWorker 的身体/容器和本地加速层，不是组织事实源。记忆、任务状态、push 记录、审计与组织经验都应保存在注册的 iWorkerCenter 上，本地只能缓存。

本地 watcher 采用单飞策略：

- 前一轮自动流程未完成时，下一轮不会重复启动。
- 如果前一轮超过最大运行时间，会先取消旧 context，再启动新一轮。
- Go 运行时不能安全强杀 goroutine，因此这里的“杀掉”是协作式取消，网络调用和关键步骤必须响应 context。
- 当前只自动处理 `restart_executor` 这类低风险恢复动作。
- `accept_task`、`start_task`、`resume_task` 等业务动作只作为建议交给 executor、A2A 协商或真人 judgement node。

## 管理与观测接口

`iWorkerCenter` 提供 GoalWatch 状态接口：

```http
GET /admin/goalwatch/status
```

返回内容包括：

- 当前配置：检查周期、停滞阈值、冷却期、每 shard iWorker 数、最大 watcher 数。
- 每个租户最近一次检查的开始/结束时间。
- 最近一次检查识别到的 iWorker 数量。
- 最近一次使用的 shard 数量。
- 检查任务数、产生 push 数。
- 每个 shard 的检查结果和错误信息。

手动触发检查接口仍保留：

```http
POST /admin/goalwatch/check
GET  /admin/goalwatch/check
POST /runtime/goalwatch/check
GET  /runtime/goalwatch/check
```

客户端接口：

```http
GET  /client/goalwatch/pushes?colleague_id=<id>&limit=20
POST /client/goalwatch/pushes/{event_id}/ack
```

## 后续演进

GoalWatch 后续应从“任务停滞检测”扩展为“组织目标守护系统”：

- 任务级：发现任务未接受、未开始、执行中长时间无反馈。
- Workflow 级：发现流程步骤超时、前后步骤断裂、责任人不可用。
- A2A 级：发现协商会话长期 open、方案无人 review、决策未形成。
- 目标级：发现经营目标偏离、关键指标恶化、行动项未闭环。
- 能力域级：发现某个虚拟组织单元持续积压、质量下降或经验未沉淀。

## 一句话总结

GoalWatch 是 AI Native 组织的执行心跳：它不替老板做重大决策，也不替 iWorker 执行业务，但它能防止目标在 LLM 中断、工具失败、agent 离线或协作停滞中悄悄死亡。

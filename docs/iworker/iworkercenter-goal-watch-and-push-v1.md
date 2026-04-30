# iWorkerCenter GoalWatch �?Push 机制

## 定位

GoalWatch �?iWorkerCenter 的组织执行守护机制。它负责持续观察已经进入执行网络的目标、任务、A2A 协商和流程状态，在发现停滞、执�?agent 离线或协作断裂时，向对应 iWorker 发出可审计的 push�?
它不是老板/董事会的控制中心。老板和董事会负责方向、约束、重大决策和资源边界；iWorkerCenter 负责把既定目标维持在可执行状态；iWorker 和真人员工负责具体执行、反馈、协商与经验沉淀�?
## 为什么需�?GoalWatch

AI Native 组织不能假设一�?LLM 调用会持续推进到任务完成。现实中，LLM 可能因为上下文丢失、工具调用失败、网络异常、服务限流、推理中断、模型幻觉或简单“忘了继续”而停下�?
如果 iWorkerCenter 只负责分派任务，不负责持续检测目标进展，组织会退回到“人盯人”的运行方式。GoalWatch 的作用是让组织自己拥有执行心跳：定期检查目标、任务、协商和流程状态，在低风险场景下推动恢复，在高风险场景下升级为 A2A 协商或人�?judgement node�?
## 当前实现范围

第一阶段聚焦协作任务停滞和执�?agent 可用性：

- 定期扫描非终态任务：`pending`、`accepted`、`in_progress`�?- 如果任务 `updated_at` 超过停滞阈值，则生�?`goal_push`�?- 如果任务分配给某�?iWorker，但�?iWorker 没有健康�?`executor` agent，也会生�?`assigned_executor_offline` push�?- 冷却期内已经 push 过的任务不会重复刷屏�?- `goal_push` �?`goal_push_ack` 都写入协作事件表，形成可审计记录�?- iWorker 客户端拉取自己的待处�?push，并只对低风�?`restart_executor` 做自动恢复�?
默认参数�?
- 停滞阈值：5 分钟�?- Push 冷却期：10 分钟�?- 后台检查周期：1 分钟�?- 每个 watcher shard 默认覆盖 50 �?iWorker�?- 单租户默认最�?16 �?watcher shard�?
## �?watcher 与热备安�?
iWorkerCenter 可以多机热备，GoalWatch 也不能是单实例瓶颈。当前机制按租户�?iWorker 数量自动计算 watcher shard 数量�?
- `known_iworker_count` 来自 `agentruntime.ListWithHealth` 中去重后�?`worker_id` 数量�?- `shard_count = ceil(iworker_count / workers_per_shard)`�?- `shard_count` 最小为 1，最大受 `max_watchers` 限制�?- 每个任务根据任务 ID �?FNV hash 归属固定 shard，多�?shard 并发检查，避免全公司任务堆在一�?watcher 上�?
多机热备下，多个 iWorkerCenter 节点可能同时运行 GoalWatch。为避免重复 push，`goal_push` 使用确定性事�?ID�?
```text
gpush_<hash(tenant_id | task_id | cooldown_bucket)>
```

协作事件表的主键约束会自然去重：同一租户、同一任务、同一冷却窗口内，多个节点同时插入时只有一个成功，其他节点把唯一键冲突视为“已由别�?watcher 处理”，不再报错。这让热备节点可以积极运行，而不是依赖脆弱的单主锁�?
在确定�?push ID 之外，后�?Monitor 还会为每�?`tenant + shard_count + shard_index` 获取 `system_settings` 中的短租约。只有拿到租约的节点才会检查该 shard；租约未过期时其它热备节点会跳过，避免重复扫描和重复 push。若持有租约的节点卡死或退出，租约过期后其它节点可自动接管�?shard。手�?`/admin/goalwatch/check` 保持即时诊断语义，不受后台租约阻挡�?
## iWorker 本地 watcher 的边�?
iWorker 本地电脑只是 iWorker 的身�?容器和本地加速层，不是组织事实源。记忆、任务状态、push 记录、审计与组织经验都应保存在注册的 iWorkerCenter 上，本地只能缓存�?
本地 watcher 采用单飞策略�?
- 前一轮自动流程未完成时，下一轮不会重复启动�?- 如果前一轮超过最大运行时间，会先取消�?context，再启动新一轮�?- Go 运行时不能安全强杀 goroutine，因此这里的“杀掉”是协作式取消，网络调用和关键步骤必须响�?context�?- 当前只自动处�?`restart_executor` 这类低风险恢复动作�?- `accept_task`、`start_task`、`resume_task` 等业务动作只作为建议交给 executor、A2A 协商或真�?judgement node�?
## 管理与观测接�?
`iWorkerCenter` 提供 GoalWatch 状态接口：

```http
GET /admin/goalwatch/status
```

返回内容包括�?
- 当前配置：检查周期、停滞阈值、冷却期、每 shard iWorker 数、最�?watcher 数�?- 每个租户最近一次检查的开�?结束时间�?- 最近一次检查识别到�?iWorker 数量�?- 最近一次使用的 shard 数量�?- 检查任务数、产�?push 数�?- 每个 shard 的检查结果和错误信息�?
手动触发检查接口仍保留�?
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

### GoalWatch 健康接口

后台已提�?`GET /admin/goalwatch/health`，把 `/admin/goalwatch/status` 的原始状态聚合为 `healthy`、`warning` �?`critical`，并返回 `reasons` �?`recommended_actions`。当前已识别无近期运行、最近运行过旧、shard 错误、全�?shard 被其它节点活跃租约跳过、Monitor 未配置等状态，便于 GUI、告警器和运维脚本直接判断组织执行心跳是否正常�?
### Workflow 恢复上下�?
GoalWatch 产生�?push 已包�?`workflow_step_instance_id`。如果协作任务由 Workflow 引擎创建，iWorker 客户端、GUI 或后续自动恢复器可以直接�?push 定位到卡住的 workflow step，而不需要再从协作任务反查流程上下文。历�?push 即使 note 中没有该字段，也会从任务记录中回填�?
Workflow runtime 已支�?`POST /runtime/workflows/steps/{id}/start` �?`POST /runtime/workflows/steps/{id}/resume`。这两个动作只把非终�?step 与对�?collaboration task 同步�?`in_progress` 并写入事件，不会自动改派、跳过步骤或完成业务，因此适合作为 GoalWatch push 后的低风险恢复入口�?
### Push 恢复字段

�?`workflow_step_instance_id` �?push 会额外返�?`recovery_action`、`recovery_method` �?`recovery_path`。例如执行中�?workflow step 停滞时，push 会给�?`resume_workflow_step`、`POST`、`/runtime/workflows/steps/{id}/resume`；未开始的 step 则给�?`start_workflow_step` 与对�?start 路径。客户端可以把这些字段作为恢复入口，但仍应由 iWorker executor 或人�?judgement node 决定是否执行�?
## 后续演进

GoalWatch 后续应从“任务停滞检测”扩展为“组织目标守护系统”：

- 任务级：发现任务未接受、未开始、执行中长时间无反馈�?- Workflow 级：发现流程步骤超时、前后步骤断裂、责任人不可用�?- A2A 级：发现协商会话长期 open、方案无�?review、决策未形成�?- 目标级：发现经营目标偏离、关键指标恶化、行动项未闭环�?- 能力域级：发现某个虚拟组织单元持续积压、质量下降或经验未沉淀�?
## 一句话总结

GoalWatch �?AI Native 组织的执行心跳：它不替老板做重大决策，也不�?iWorker 执行业务，但它能防止目标�?LLM 中断、工具失败、agent 离线或协作停滞中悄悄死亡�?
### iWorker �ͻ��˻ָ����

��ǰ iWorker �����Ѳ������� `IWorkerCenterClient`����ͨ�� `X-Tenant-ID` �� `colleague_id` ��ȡ `/client/goalwatch/pushes`����ֻ�Դ� `workflow_step_instance_id` �� `recovery_action` Ϊ `start_workflow_step` / `resume_workflow_step` �� push ���� `/client/goalwatch/pushes/{event_id}/recover`���Ᵽ���˱߽磺Center ������֯��ʵ��Workflow ״̬����ƣ�iWorker watcher ֻ����������ѯ�봥������

`recover` �ӿڻ��� Center �ڲ����� workflow runtime �� start/resume�����Զ�д�� `recovered` ack��iWorker ��ֱ�Ӹ�д�������̻���䣬Ҳ���ڱ��ر���Ȩ��״̬��������ౣ�����桢��־����һ�� watcher �Ľ���״̬��

# iWorker 文档索引

本目录用于统一 `iWorker / iWorkerCenter / iWorkerCloud` 的产品边界、技术架构和开发路线。

## 建议阅读顺序

1. `iworkercenter-service-positioning-v1.md`
2. `iworker-ai-native-organization-whitepaper-v2.md`
3. `iworkercenter-executive-architecture-v2.md`
4. `iworkercenter-executive-skills-v2.md`
5. `iworker-ai-native-virtual-organization-v1.md`
6. `iworkercenter-goal-watch-and-push-v1.md`
7. `iworker-multi-agent-runtime-v1.md`
8. `iworker-skillmarket-cloud-publish-v1.md`
9. `iworker-technical-integration-and-blockers-v1.md`
10. `iworkercenter-ha-architecture-v1.md`

## 当前统一口径

- `iWorker` 是数字员工本地身体、交互终端和执行容器，负责 IM/语音协作、本地工具调用、桌面/浏览器执行和本地缓存。
- `iWorkerCenter` 是客户企业自己的 AI Native 组织运行时服务，不是普通桌面 GUI。它负责记忆、能力、workflow、A2A、GoalWatch、审计、组织编排、经营视图和高可用后台服务。
- `iWorkerCenter` 可以提供 Admin Web Console，但 Web Console 只是管理入口；产品本体是服务端运行时、API、数据和组织自动运行机制。
- `iWorkerCloud` 是我们公司的云端管控平台，只负责授权、算力、Skill Market、云端多租户 iWorkerCenter 管理和商业结算，不参与客户企业运营。
- 人类员工是 AI Native 组织的必要部分，但更像高价值 `tool` / `skill` / `judgement node`；企业沉淀应留在 AI 系统中，而不是依赖某个固定的人。
- 中层/部门在 AI Native 运营中应虚拟化为能力域、记忆域、权限域、流程域和指标域，不再作为真实控制层。
- 老板/董事会是决策者、约束制定者和重大判断节点，不是公司控制中心；常规事项应由 iWorkerCenter 在权限和风险边界内自行推动。
- iWorker 本地只能缓存，记忆、能力、workflow、push、审计和执行反馈的权威沉淀必须在注册的 iWorkerCenter。
- iWorkerCenter 拥有企业内部 Skill Market，来源包括 Cloud 导入、自研总结和本地创建；成熟 skill 可按管理员规则上传到 iWorkerCloud 并通过管理员邮箱关联收益。

## 技术线最新进展

- iWorker 已具备独立 `iworkercenter_url`、`iworkercenter_tenant_id`、`iworkercenter_colleague_id` 和 `iworkercenter_goalwatch_interval_sec` 配置。
- iWorker 本地 GoalWatch watcher 可以拉取 Center push，并对低风险 workflow start/resume push 调用 Center recover 接口。
- iWorker watcher 每次执行前会向 iWorkerCenter 上报 agent runtime heartbeat，明确本地只是 body/cache，记忆权威在 Center。
- iWorkerCenter 已具备 GoalWatch push、workflow recovery、agent runtime heartbeat、内部 Skill Market 自演化和 Cloud 发布闭环的服务端接口基础。
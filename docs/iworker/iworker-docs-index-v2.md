# iWorker 文档索引

本目录中的正式文档用于统一 `iWorker / iWorkerCenter / iWorkerCloud` 的产品口径、技术架构和开发路线。

## 建议阅读顺序

1. `iworker-ai-native-organization-whitepaper-v2.md`
2. `iworkercenter-executive-architecture-v2.md`
3. `iworkercenter-executive-skills-v2.md`
4. `iworker-ai-native-virtual-organization-v1.md`
5. `iworkercenter-goal-watch-and-push-v1.md`
6. `iworker-multi-agent-runtime-v1.md`
7. `iworker-technical-integration-and-blockers-v1.md`

## 当前统一口径

- `iWorker` 是数字员工前台，是任务承接、IM/语音协作、本地工具调用和缓存容器。
- `iWorkerCenter` 是客户企业自己的 AI Native 组织运行时，负责记忆、能力、workflow、A2A、Goal Watch、审计和组织编排。
- `iWorkerCloud` 是我们公司的云端管理平台，只负责授权、算力、Skill Market 和 Center 管理，不参与客户企业运营。
- 人类员工是 AI Native 组织的必要部分，但更像高价值 `tool` / `skill` / `judgement node`；组织沉淀应留在 AI 系统中，而不是依赖某个固定的人。
- 中层/部门在 AI Native 运营中应虚拟化为能力域、记忆域、权限域、流程域和指标域，不再作为真实控制层。
- iWorkerCenter 在无需老板/董事会重大决策时应可自行运行；老板是决策者，不是公司控制中心。
- iWorker 本地只能缓存，记忆、能力和执行反馈的权威沉淀必须在注册的 iWorkerCenter。

## 技术线最新进展

`iworker-technical-integration-and-blockers-v1.md` 已更新到执行反馈闭环：Workflow 完成步骤时可上报 capability 执行反馈，Center 持久化为 `capability_usage_events`，并开始用历史成功/失败反馈影响后续 capability 路由。

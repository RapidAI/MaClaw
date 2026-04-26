# MaClawSrv 缺口分析



这份文档用于系统梳理 `MaClawSrv` 当前已经具备的能力、还缺少的功能，以及接口一致性上还需要继续收口的地方，方便后续按优先级推进。



## 当前状态



`MaClawSrv` 作为多租户 Maclaw Agent REST 服务，已经不是“只有骨架”的阶段了，主链路已经比较完整。



目前已经比较扎实的部分：



- 租户、用户、凭证的管理端基础生命周期

- tenant / user 删除接口

- credential 的创建、列表、单项查询、更新、rotate secret、吊销

- `api_key + api_secret -> bearer token` 的用户鉴权链路

- 用户共享配置的 schema / 获取 / 更新 / 校验 / 测试

- instance / session / message / run 的运行时链路

- run 事件 SSE 推送

- skill 管理 REST 接口

- MCP 管理 REST 接口

- usage / audit / alerts / dashboard / overview / tenant summary

- `/openapi.json` 机器可读描述

- `/health`、`/livez`、`/readyz`、`/version` 运维探针



所以现在的重点，不再是“有没有基础能力”，而是“是否已经成为一个完整、稳定、对外友好的控制平面”。



## 还缺什么



### 1. 管理生命周期还可以继续成熟化



虽然 tenant 和 user 删除接口已经补上，但后续还值得继续做：



- 软删除 / 回收站语义

- 删除保护策略

- 删除前导出

- 更明确的退役流程



### 2. 凭证生命周期还没完全打磨完



现在已有的是：列表、创建、单项查询、更新、rotate secret、吊销。



还缺少：



- API key 自身的轮换能力

- 比 active / revoked 更细的 suspend / expire 生命周期

- 更完整的一次性 secret 生成与回显模型



也就是说，credential 这块已经从“只有基础能力”进入“基本可用”，但还没有完全打磨完。



### 3. 管理端搜索/过滤能力偏弱



虽然已经有分页，但对真实运营场景还不够。



建议补充的过滤能力：



- tenant 按 `status`

- tenant 按 `name`

- user 按 `status`

- user 按 `name`

- user 按 `email`

- 跨 tenant 的 user 搜索



否则一旦租户和用户规模上来，纯分页浏览会比较难用。



### 4. 缺少导出、导入、备份、迁移接口



当前还没有 REST 能力来做：



- 导出服务状态

- 导入服务状态

- tenant 级数据快照

- 环境间迁移



这会影响运维、备份恢复，以及企业化接入。



### 5. 缺少异步任务模型



现在有些操作其实已经不再适合“同步请求-同步返回”模型，比如：



- skill install

- skill import

- skill upload

- MCP start

- MCP health-check



后续更合理的方向，是增加 job 资源，例如：



- `POST /api/v1/jobs`

- `GET /api/v1/jobs/{jobId}`

- `GET /api/v1/jobs/{jobId}/events`

- `POST /api/v1/jobs/{jobId}/cancel`



这样能更好处理超时、重试、进度和历史查询。



### 6. 缺少服务级 webhook / 事件订阅



现在只有 run SSE。



还缺少的事件面包括：



- 管理面事件订阅

- tenant / user 生命周期 webhook

- run 完成通知

- skill / MCP 操作完成通知



如果外部平台要围绕 `MaClawSrv` 自动化编排，这一层会很重要。



### 7. 运维接口还差最后一截



当前除了 `GET /health`，已经补上：



- `GET /readyz`

- `GET /livez`

- `GET /version`



还缺的是：



- `GET /metrics`



这样才能更方便接 Kubernetes、Prometheus、负载均衡器和标准运维体系。



### 8. 缺少更强的聚合分析接口



已有 overview/dashboard 已经有帮助，但还不算强运营视角。



后续可考虑：



- 热门 tenant

- 超额 tenant

- 长期不活跃 tenant / user

- 错误率趋势

- skill 使用分析

- MCP 使用分析



## 接口一致性还差什么



### 1. action 型接口风格需要持续明确



现在接口风格本身是合理的，但最好持续明确告诉接入方：



- 资源型操作走 `GET / POST / PATCH / PUT / DELETE`

- 状态切换或命令型操作走 `/stop`、`/resume`、`/archive`、`/restore`、`/health-check` 这种 action 路由



这样外部调用方不会误猜某个动作是不是应该用 `PATCH status=...`。



### 2. 分页支持范围要和实际实现完全对齐



目前真正支持分页的有：



- admin tenants

- admin users

- admin credentials

- admin audit-events

- MCP servers

- skills

- instances

- sessions

- messages

- runs



如果文档漏掉 MCP 或 skills，SDK 作者就很容易按错方式接入。



### 3. OpenAPI 应该被视为最终真相



`openapi.go` 现在比 prose 文档更整洁，也更适合作为路由真相来源。



后续建议固定流程：



1. 先改 `http.go`

2. 再同步 `openapi.go`

3. 最后更新 README 和手册



### 4. 重操作接口现在在 HTTP 边界上仍显得“过于同步”



这不算 bug，但从接口成熟度看，像 skill install、MCP health-check 这类操作，后续更适合统一抽象成 job，而不是继续堆 action endpoint。



## 建议的下一步优先级



### 第一优先级



- 持续收口 README / OpenAPI / 手册一致性

- 增强 tenant / user 搜索过滤

- 补更细的指标维度与告警指标



### 第二优先级



- 增加服务级导出导入接口

- 增加更成熟的 delete / retire 策略

- 继续完善 credential 生命周期



### 第三优先级



- 增加异步 job 模型

- 增加 webhook / 事件订阅模型

- 增加更强的管理分析接口



## 结论



如果目标是让 `MaClawSrv` 成为一个对外稳定、好接、好运营的 agent 控制平面，建议按下面顺序推进：



1. 先把文档和 OpenAPI 彻底对齐。

2. 再补搜索、过滤和 metrics。

3. 再补导出导入和更成熟的生命周期策略。

4. 最后引入 job 和 webhook 这类平台化能力。



这样收益最大，也不会把当前已经成型的主链路推翻重做。






## ?????????

- ?????????????????
- ???????????????????? `delete-check` ?????????????????????? `409`?
- ????????????????????????

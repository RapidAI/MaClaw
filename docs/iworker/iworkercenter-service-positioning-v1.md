# iWorkerCenter 服务定位与交互边界 v1

## 一句话结论

`iWorkerCenter` 是客户企业自己的 AI Native 组织运行时服务，不是普通桌面 GUI，也不应该被设计成安装在老板或员工电脑上的单机控制中心。

它可以提供 Web 管理控制台，但控制台只是服务的可视化入口；真正的产品本体是后台服务、API、组织数据、任务调度、记忆系统、A2A 协议、GoalWatch、审计和高可用运行时。

## 产品分层

### iWorker

`iWorker` 是数字员工的本地身体和交互终端。

它负责：

- 接入本地文件、浏览器、桌面环境、语音、IM 和外部工具。
- 承载 interaction / executor / watcher 等本地 agent 实例。
- 缓存必要数据，加速本地访问。
- 在离线或弱网时做临时缓冲。
- 向注册的 `iWorkerCenter` 上报心跳、执行反馈和必要的同步事件。

它不负责：

- 成为企业记忆真源。
- 成为组织任务状态真源。
- 成为 skill、workflow、审计和经营判断的权威存储点。

### iWorkerCenter

`iWorkerCenter` 是企业内部部署或企业专属托管的服务程序。

它负责：

- 公司、部门、个人三级记忆的权威存储。
- 组织结构、虚拟部门、角色、权限和能力网络。
- Workflow、任务、A2A 协商、GoalWatch 和 push 机制。
- iWorker agent 实例注册、心跳、在线状态和执行状态观察。
- 企业内部 Skill Market、自演化、成熟度评价和安全治理。
- 审计、追踪、经营视图和 Executive Skills。
- 多机热备、高可用和服务端状态持久化。

它可以提供：

- `HTTP API`：供 iWorker、IM 网关、管理员控制台和外部系统调用。
- `Admin Web Console`：供管理员、老板/董事会和运维人员查看组织状态、配置规则、处理安全裁决和重大决策。
- `IM/语音入口`：供人类用更自然的方式与数字员工组织协作。

它不应该提供：

- 普通桌面 GUI 形态的单机应用。
- 依赖某台本地电脑才能运行的组织控制中心。
- 让老板/董事会直接替代组织执行网络的“人工总控台”。

### iWorkerCloud

`iWorkerCloud` 是我们作为系统厂商提供的云端管控平台。

它负责：

- iWorkerCenter 授权与多租户管理。
- 算力分发与计费。
- 顶级 Skill Market。
- 云端 iWorkerCenter 实例托管、订阅、商业结算和平台运维。

它不参与客户企业经营，不调度客户公司的任务，不读取客户企业记忆，不替代客户自己的 `iWorkerCenter`。

## Web 控制台不是桌面 GUI

`iWorkerCenter` 的 `web/admin` 或 `frontend` 目录应被理解为服务端管理控制台。

正确表述是：

- iWorkerCenter 服务程序。
- iWorkerCenter 后台服务。
- iWorkerCenter Admin Web Console。
- iWorkerCenter 管理控制台。
- iWorkerCenter API 服务。

应避免表述为：

- iWorkerCenter 桌面 GUI。
- iWorkerCenter 本地客户端。
- 老板电脑上的控制中心。
- 员工日常工作的桌面软件。

## 与老板/董事会的关系

老板和董事会是决策者、约束制定者和重大判断节点，不是公司运行控制中心。

`iWorkerCenter` 可以为老板/董事会提供经营视图和 Executive Skills，例如：

- 经营异常解释。
- 目标偏差分析。
- 资源瓶颈识别。
- 关键风险提示。
- 重大决策材料生成。
- 决策后目标拆解与执行网络分发。

但具体执行仍由 iWorker、真实员工和组织 workflow 完成。多数常规事项在规则、权限和风险边界内应由 `iWorkerCenter` 自行推动，而不是等待老板逐项控制。

## iWorker 桌面里的 Center 面板定位

`iWorker` 桌面端可以有一个 `iWorkerCenter` 配置面板，但这个面板不是 Center GUI。

它只是用于：

- 配置当前 iWorker 注册到哪个 Center。
- 设置 tenant / colleague 身份。
- 启停本地 watcher。
- 查看本地 body 与 Center 的连接、心跳和 GoalWatch 状态。

也就是说，这个面板属于 `iWorker` 本地 body 的连接设置，不属于 `iWorkerCenter` 产品本体。

## 架构原则

1. `iWorkerCenter` 是服务，不是桌面应用。
2. `iWorkerCenter` 的权威状态在服务端持久化存储中，不在本地电脑。
3. `iWorkerCenter` 可以多机热备，不能被建模为单节点单机软件。
4. `iWorker` 本地只做执行、交互和缓存，不做组织真源。
5. Web Console 是管理入口，不是组织运行本体。
6. IM/语音是人类与数字员工协作的自然入口，Web Console 主要用于观察、配置、审计和重大决策。
7. iWorkerCloud 是厂商云管控平台，不参与客户公司运营。

## 对当前开发的直接影响

- `iWorkerCenter/main.go` 保持 HTTP server 形态。
- `iWorkerCenter/frontend` 是 Admin Web Console 源码，`iWorkerCenter/cmd/iworkercenter/web/admin` 是部署用内嵌产物，不是桌面 GUI。
- `iWorker` GUI 中的 `IWorkerCenterPanel` 只保留为本地连接与 watcher 控制面板。
- 所有组织级记忆、任务、workflow、push、skill 和审计接口继续放在 `iWorkerCenter` 服务端。
- 后续不要在 `iWorkerCenter` 目录引入 Wails / Electron / 桌面 GUI 主程序。
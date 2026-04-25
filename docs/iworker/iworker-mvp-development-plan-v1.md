# iWorker MVP 开发计划

## 当前产品边界

本仓库内的三条产品线应明确区分：

- `D:/workprj/aicoder/iWorker`：数字员工前台，面向具体工作执行
- `D:/workprj/aicoder/iWorkerCenter`：数字员工组织中枢，面向组织管理与经营协同
- `D:/workprj/aicoder/iWorkerCloud`：云侧控制面，面向中心注册、授权、算力与云端运营

这三者是独立产品，不再以旧的 `hubcenter` 作为主实现承接。

## 当前正确的第一阶段切入点

第一阶段应优先开发 `iWorkerCenter`，原因是：

- 它已经有独立的 Go 服务与 React 管理台
- 它天然承接“AI Native 组织中枢”定位
- 它最适合先长出经营概览、管理驾驶舱和 Executive Skills 入口
- `iWorkerCloud` 更适合作为上层平台与云侧能力中心，而不是替代 `iWorkerCenter` 本体

## 当前已开始的第一批实现

已将 `iWorkerCenter` 的 `Overview` 页面向经营层方向推进，目标是把它从普通系统概览升级为：

- 管理层可读的经营/组织简报
- 风险与脆弱点提示
- 建议动作列表
- Executive Skills 调用入口与结构化结果输出

## 第一阶段开发顺序

### Sprint 1：iWorkerCenter 经营概览 MVP

1. 新增 `executive` 后端模块
2. 提供经营概览与 skill 调用 API
3. 将 `Overview` 页面升级为经营概览
4. 展示风险、建议动作与 skill 输出

### Sprint 2：iWorkerCenter 组织中枢强化

1. 打通角色、同事、能力、流程与经营视图的关联
2. 增加组织脆弱点与依赖识别
3. 逐步把 Executive Skills 从 mock 输出替换成真实组织数据驱动

### Sprint 3：iWorker 与 iWorkerCenter 联动

1. 让 `iWorker` 前台承接任务与执行
2. 让 `iWorkerCenter` 承担组织、管理与经营编排
3. 建立从经营问题到执行动作的下发闭环

## 暂不处理

当前不再以 `hubcenter` 作为 `iWorkerCenter` 的开发主线，也不再把其作为这条产品线的目标目录。

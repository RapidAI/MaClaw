# MaClawDataSrv 企业信息系统简化设计稿

日期：2026-06-18  
状态：对齐草案  
目标读者：产品、前端、后端、企业实施、agent 工具开发

## 一句话定位

MaClawDataSrv 是给企业信息系统用的“严谨数据底座”，不是让用户学习数据库的平台。

它应该让企业用户用最少概念完成这些事：

- 建一个业务表
- 定几个字段
- 录入、导入、查询、导出数据
- 看清楚谁改了什么
- 让 agent 或外部系统按权限读写
- 在需要时审批、回滚、审计

核心取舍：

- 数据要严谨：字段、校验、唯一性、敏感字段、审计、备份不能含糊。
- 使用要简单：默认流程不暴露太多治理、连接器、计划、证据包等高级概念。
- 架构要能长：高级能力保留，但从“默认路径”里收起来。

## 最新简化结论

不要让用户自己拼“表、视图、动作、token、关联”。

后端应该提供一个向导接口：

```text
选择应用模板 -> 后端预览会创建什么 -> 一键创建 -> 一键授权 -> 返回可用入口
```

用户默认看到的是“应用”，不是“表”。

默认应用模板应该围绕企业 MIS：

- CRM：客户、联系人、商机、订单、回款
- 进销存：供应商、采购、销售、仓库、库存、出入库
- 财务：发票、付款、收款、报销、凭证、科目
- ERP 轻量业务包：客户、供应商、物料、订单、合同、库存、财务单据

HR 可以作为补充模板，但不是第一主线。

更关键的一点：应用模板不能写死成一坨。

真正要做的是“业务组件组合”：

```text
用户信息 + 客户 + 商品 + 订单 + 发票 + 付款 + 库存流水
```

CRM、进销存、财务、ERP 只是这些组件的不同组合。

所以底层模型应该是：

```text
业务组件 Component
  -> 声明自己需要哪些表
  -> 声明自己提供哪些表
  -> 声明和其他组件怎么关联
  -> 声明默认视图/操作/token 套餐

应用蓝图 Blueprint
  -> 选择一组组件
  -> 解析依赖
  -> 合并共享表
  -> 生成完整 MIS 应用
```

用户选择的是“进销存”，后端实际装配的是：

```text
客户组件
供应商组件
商品组件
仓库组件
采购订单组件
销售订单组件
库存组件
出入库组件
```

一个应用模板是一套可直接使用的数据包，里面包含多张表、字段、关联、默认视图、默认操作和推荐权限。

单表模板仍然保留，但作为高级能力。

还有一层必须单独定义：共享主数据。

多套 MIS 应用不能各自创建一份“用户/员工/客户/商品”。否则 CRM、进销存、财务之间数据会散。

所以 DataSrv 要分三类模板：

```text
共享主数据模板：组织、用户/员工、客户、供应商、商品、仓库、会计科目
应用模板：CRM、进销存、财务、ERP 轻量包
表模板：单独补一张表
```

## 模板到底是什么

模板分四层。

### 业务组件

业务组件是最小可组合单元。

一个组件不是完整应用，也不一定只有一张表。它是一块可复用业务能力。

例子：

| 组件 | 提供 | 依赖 | 常见应用 |
| --- | --- | --- | --- |
| 用户组件 | 用户/员工 | 部门可选 | ERP、CRM、财务、审批 |
| 客户组件 | 客户、联系人 | 用户可选 | CRM、销售、财务 |
| 供应商组件 | 供应商 | 用户可选 | 采购、财务 |
| 商品组件 | 商品/物料 | 无 | 进销存、ERP |
| 仓库组件 | 仓库 | 用户可选 | 库存、ERP |
| 销售订单组件 | 销售订单 | 客户、商品、用户可选 | CRM、进销存、ERP |
| 采购订单组件 | 采购订单 | 供应商、商品、用户可选 | 进销存、ERP |
| 库存组件 | 库存、出入库流水 | 商品、仓库、订单可选 | 进销存、ERP |
| 发票组件 | 发票 | 客户/供应商、订单/合同可选 | 财务、ERP |
| 付款组件 | 收款、付款 | 发票/订单/报销可选 | 财务、ERP |
| 凭证组件 | 凭证、会计科目 | 付款/发票可选 | 财务 |

组件的好处：

- 可以组合。
- 可以复用。
- 可以升级。
- 可以让用户先小后大。
- 不会因为模板写死而变成另一个僵硬 ERP。

### 应用蓝图

应用蓝图是一组组件的推荐组合。

例如：

```text
CRM 蓝图 = 用户 + 客户 + 销售订单 + 付款
进销存蓝图 = 用户 + 客户 + 供应商 + 商品 + 仓库 + 采购订单 + 销售订单 + 库存
财务蓝图 = 用户 + 客户 + 供应商 + 发票 + 付款 + 凭证
ERP 轻量蓝图 = 用户 + 客户 + 供应商 + 商品 + 仓库 + 采购订单 + 销售订单 + 库存 + 发票 + 付款 + 凭证
```

蓝图不直接写死表结构，而是引用组件。

### 共享主数据模板

共享主数据是所有应用都能引用的基础表，只创建一份。

建议第一批共享主数据：

| 主数据 | 内部表 | 被哪些应用使用 |
| --- | --- | --- |
| 组织/部门 | `company.departments` | ERP、财务、HR、审批 |
| 用户/员工 | `company.users` 或 `hr.employees` | ERP、CRM、进销存、财务、审批 |
| 客户 | `crm.customers` 或 `sales.customers` | CRM、销售、财务、合同 |
| 供应商 | `procurement.suppliers` | 采购、进销存、财务、合同 |
| 商品/物料 | `inventory.items` | 进销存、采购、销售、ERP |
| 仓库 | `inventory.warehouses` | 进销存、ERP |
| 会计科目 | `finance.accounts` | 财务、ERP |

共享主数据有两个规则：

- 同一个 tenant 里只创建一次。
- 应用模板只能引用它，不能重复创建同义表。

如果用户先创建 CRM，再创建财务，财务模板应该复用 CRM 已经创建的客户表。

如果用户先创建进销存，再创建 ERP 轻量包，ERP 模板应该复用已有客户、供应商、商品、仓库。

### 应用模板

应用模板是默认入口。

它代表一个完整业务小应用的数据结构。

例如“进销存管理”应用模板包含：

```text
客户
供应商
商品/物料
采购订单
销售订单
仓库
库存
出入库流水
```

并自动建立关联：

```text
采购订单.supplier_ref -> 供应商
销售订单.customer_ref -> 客户
库存.item_ref -> 商品/物料
库存.warehouse_ref -> 仓库
出入库流水.item_ref -> 商品/物料
出入库流水.warehouse_ref -> 仓库
出入库流水.source_order_ref -> 采购订单/销售订单
```

应用模板创建后，系统还自动生成：

- 常用列表
- 常用详情页
- 新增/更新操作
- 默认导入模板
- 推荐 token 权限套餐

### 表模板

表模板是一张表。

例如：

```text
员工
客户
订单
合同
发票
```

它适合高级用户补充一张业务表，不适合新手作为第一入口。

## 共享表冲突怎么处理

后端向导创建应用时要先做“主数据规划”。

### 场景：先创建 CRM，再创建财务

CRM 已创建：

```text
客户
联系人
商机
销售订单
回款
```

再创建财务时，财务需要客户。

系统不再新建“财务客户表”，而是提示：

```text
将复用已有客户表：客户
财务发票、收款将关联到这个客户表。
```

内部关系：

```text
finance.invoices.customer_ref -> sales.customers
finance.receipts.customer_ref -> sales.customers
```

### 场景：先创建进销存，再创建 ERP 轻量包

进销存已创建：

```text
客户
供应商
商品
仓库
库存
采购订单
销售订单
出入库流水
```

ERP 轻量包需要这些基础表。

系统复用：

```text
客户：复用
供应商：复用
商品：复用
仓库：复用
库存：复用
```

只新增：

```text
合同
发票
收付款
凭证
```

## 共享主数据命名建议

为了避免 CRM 用 `sales.customers`、财务又想用 `finance.customers`，建议逐步规范主数据命名。

推荐：

```text
party.customers        客户
party.suppliers        供应商
company.users          用户/员工
company.departments    部门
inventory.items        商品/物料
inventory.warehouses   仓库
finance.accounts       会计科目
```

兼容当前已有表名：

```text
sales.customers        可作为 party.customers 的旧名或别名
hr.employees           可作为 company.users 的员工扩展表
procurement.suppliers  可作为 party.suppliers 的旧名或别名
```

第一版不一定立刻迁移已有模板，但向导返回时要告诉前端哪个表是“共享主数据”。

## 应用模板创建策略

每个应用模板要声明三类表：

```json
{
  "id": "mis.inventory",
  "title": "进销存管理",
  "shared_tables": [
    {"role": "customer", "preferred_dataset": "party.customers", "fallback_dataset": "sales.customers"},
    {"role": "supplier", "preferred_dataset": "party.suppliers", "fallback_dataset": "procurement.suppliers"},
    {"role": "item", "preferred_dataset": "inventory.items"},
    {"role": "warehouse", "preferred_dataset": "inventory.warehouses"}
  ],
  "owned_tables": [
    "procurement.purchase_orders",
    "sales.orders",
    "inventory.stock",
    "inventory.movements"
  ],
  "relationships": [
    "sales.orders.customer_ref -> customer",
    "procurement.purchase_orders.supplier_ref -> supplier",
    "inventory.stock.item_ref -> item",
    "inventory.stock.warehouse_ref -> warehouse"
  ]
}
```

创建时后端执行：

```text
1. 检查 shared_tables 是否已有
2. 有则复用
3. 没有则创建
4. 创建 owned_tables
5. 把 relationships 绑定到实际复用或新建的表
6. 返回完整应用清单
```

这样用户不用知道共享表在哪里，系统自己接上。

## 组件模型设计

后端应该用声明式 JSON/Go struct 表达组件。

### 组件定义

```json
{
  "id": "sales.order",
  "title": "销售订单",
  "category": "sales",
  "provides": [
    {
      "role": "sales_order",
      "dataset": "sales.orders",
      "title": "销售订单",
      "fields": [
        {"key": "order_no", "type": "string", "required": true, "unique": true},
        {"key": "customer_ref", "type": "record_ref", "ref_role": "customer"},
        {"key": "owner_ref", "type": "record_ref", "ref_role": "user"},
        {"key": "amount", "type": "number", "required": true},
        {"key": "status", "type": "enum", "values": ["draft", "confirmed", "fulfilled", "cancelled"]}
      ]
    }
  ],
  "requires": [
    {"role": "customer", "required": true},
    {"role": "user", "required": false},
    {"role": "item", "required": false}
  ],
  "views": [
    {"id": "sales.order_list", "title": "销售订单列表", "dataset_role": "sales_order"}
  ],
  "actions": [
    {"id": "sales.order_create", "title": "新增销售订单", "dataset_role": "sales_order"}
  ]
}
```

关键点：

- `role` 是语义角色，不直接绑死某张表。
- `ref_role` 表示关联到某个角色，最终由装配器解析成具体 dataset。
- 如果已有客户表满足 `customer` 角色，就复用。
- 如果没有，就由客户组件创建。

### 蓝图定义

```json
{
  "id": "mis.inventory",
  "title": "进销存管理",
  "components": [
    "identity.user",
    "party.customer",
    "party.supplier",
    "inventory.item",
    "inventory.warehouse",
    "procurement.purchase_order",
    "sales.order",
    "inventory.stock"
  ],
  "recommended_tokens": [
    {"preset": "readonly", "title": "只读查询"},
    {"preset": "operator", "title": "录入和更新"}
  ]
}
```

### 装配器逻辑

创建应用时，后端做组件装配：

```text
1. 读取蓝图 components
2. 递归补齐 requires
3. 建立 role -> dataset 映射
4. 优先复用 tenant 已存在的共享表
5. 没有则创建组件提供的表
6. 把 ref_role 编译成 ref_dataset
7. 创建默认视图和操作
8. 创建推荐 token 套餐
9. 返回创建结果和测试入口
```

### role 映射示例

用户选“进销存管理”。

系统解析出：

```text
role: user          -> company.users
role: customer      -> party.customers
role: supplier      -> party.suppliers
role: item          -> inventory.items
role: warehouse     -> inventory.warehouses
role: purchase_order -> procurement.purchase_orders
role: sales_order   -> sales.orders
role: stock         -> inventory.stock
role: movement      -> inventory.movements
```

销售订单字段：

```text
customer_ref.ref_role = customer
```

装配后变成：

```text
customer_ref.config.ref_dataset = party.customers
```

如果 tenant 已经有旧表：

```text
customer -> sales.customers
```

那就编译成：

```text
customer_ref.config.ref_dataset = sales.customers
```

组件不用知道最终用了哪张表。

## 为什么这样灵活

用户可以从小开始：

```text
只装：客户 + 订单
```

以后再加：

```text
+ 发票
+ 付款
+ 库存
```

系统不会重新建客户和订单，只会补新增组件，并把新组件关联到旧数据。

例如以后加付款组件：

```text
付款.order_ref -> 已有订单表
付款.customer_ref -> 已有客户表
```

这就是 MIS 需要的渐进式扩展。

## 后端向导要支持两种入口

### 入口 A：按蓝图创建

适合新手：

```text
我要进销存
```

系统选择一组组件。

### 入口 B：按组件自由组合

适合懂业务的实施人员：

```text
我要：用户 + 客户 + 订单 + 付款
```

系统预览：

```text
将创建：
- 用户
- 客户
- 订单
- 付款

将建立：
- 订单 -> 客户
- 订单 -> 用户
- 付款 -> 订单
- 付款 -> 客户
```

然后一键创建。

API：

```http
GET  /api/v1/simple/components
POST /api/v1/simple/apps/preview
POST /api/v1/simple/apps
```

`preview/create` 可以接受蓝图：

```json
{
  "blueprint_id": "mis.inventory"
}
```

也可以接受组件列表：

```json
{
  "components": ["identity.user", "party.customer", "sales.order", "finance.payment"]
}
```

## 第一版不要做太花

第一版只需要支持：

- 组件声明
- 蓝图声明
- 依赖解析
- 共享表复用
- `ref_role -> ref_dataset` 编译
- 预览
- 创建
- 授权 token

不要一开始做：

- 可视化拖拽建模
- 复杂多态引用
- 跨 tenant 共享
- 复杂工作流引擎
- 自动财务分录

先把“元素组合”跑顺。

## 基于模板定制和 AI 修改

企业 MIS 一定不能只有现成模板。模板只是起点。

用户需要：

- 在现有应用上加字段
- 加一张新表
- 改字段显示名
- 增加枚举值
- 增加表关联
- 新增查询视图
- 新增标准操作
- 让 AI 根据业务描述提出修改

但这些修改不能像改普通 JSON 一样随便写。因为企业数据要严谨。

所以需要“变更计划”机制。

```text
用户/AI 提出修改
  -> 后端生成变更计划
  -> 预览影响
  -> 校验风险
  -> 人确认
  -> 执行
  -> 记录审计
  -> 可回滚或继续修正
```

### 定制分级

#### L1：安全定制，直接执行

这类变更风险低，可以确认后直接执行：

- 新增可选字段
- 改字段显示名
- 改字段描述
- 增加枚举选项
- 新增普通查询视图
- 新增 token 权限，但不能扩大敏感权限

#### L2：中风险定制，需要预检

这类变更必须先预检：

- 新增必填字段
- 新增唯一约束
- 字段从普通改成敏感
- 新增表关联
- 新增标准写入操作
- 批量更新已有数据

预检要告诉用户：

```text
将影响多少表
将影响多少记录
是否存在空值
是否存在重复值
是否需要默认值
是否可能导致导入失败
```

#### L3：高风险定制，需要审批/备份

这类变更默认不让 AI 自动执行：

- 删除字段
- 删除表
- 修改字段类型
- 收紧枚举值
- 移除关联
- 批量删除数据
- 扩大 token 到敏感字段或管理权限

执行前必须：

- 创建备份
- 生成变更计划
- 管理员确认
- 写审计日志

### 后端 API：变更计划

不要让前端或 AI 直接调用一堆底层 API。

新增统一入口：

```http
POST /api/v1/simple/changes/plan
POST /api/v1/simple/changes/{planId}/apply
POST /api/v1/simple/changes/{planId}/cancel
GET  /api/v1/simple/changes/{planId}
```

### 变更计划请求示例

用户说：

```text
给客户增加“客户来源”和“是否重点客户”字段。
```

AI 或前端提交：

```json
{
  "app_id": "crm",
  "intent": "给客户增加客户来源和是否重点客户字段",
  "changes": [
    {
      "kind": "add_field",
      "target_role": "customer",
      "field": {
        "key": "source",
        "title": "客户来源",
        "type": "string",
        "enum": ["官网", "转介绍", "展会", "广告", "其他"],
        "required": false
      }
    },
    {
      "kind": "add_field",
      "target_role": "customer",
      "field": {
        "key": "is_key_account",
        "title": "是否重点客户",
        "type": "bool",
        "required": false
      }
    }
  ]
}
```

后端返回：

```json
{
  "plan_id": "plan_xxx",
  "risk": "low",
  "summary": "将给客户表新增 2 个可选字段。",
  "resolved_targets": [
    {"role": "customer", "dataset": "party.customers"}
  ],
  "steps": [
    "新增字段 party.customers.source",
    "新增字段 party.customers.is_key_account"
  ],
  "requires_backup": false,
  "requires_admin": false,
  "can_apply": true
}
```

用户点确认：

```http
POST /api/v1/simple/changes/plan_xxx/apply
```

### 新增表定制

用户说：

```text
我们还要管理售后工单，工单要关联客户和订单。
```

变更计划：

```json
{
  "app_id": "crm",
  "intent": "增加售后工单表，关联客户和订单",
  "changes": [
    {
      "kind": "add_table",
      "role": "service_ticket",
      "dataset": "service.tickets",
      "title": "售后工单",
      "fields": [
        {"key": "ticket_no", "title": "工单号", "type": "string", "required": true, "unique": true},
        {"key": "customer_ref", "title": "客户", "type": "record_ref", "ref_role": "customer"},
        {"key": "order_ref", "title": "订单", "type": "record_ref", "ref_role": "sales_order"},
        {"key": "issue", "title": "问题描述", "type": "text", "required": true},
        {"key": "status", "title": "状态", "type": "enum", "values": ["待处理", "处理中", "已解决", "关闭"]}
      ]
    }
  ]
}
```

后端解析：

```text
customer_ref -> 现有客户表
order_ref -> 现有销售订单表
```

如果当前应用没有订单组件，返回：

```text
缺少 sales_order 角色。
你可以：
1. 先添加销售订单组件
2. 让工单只关联客户
```

### AI 修改规则

AI 应用可以提出修改，但不能默认直接执行高风险变更。

AI 权限分三种：

| 权限 | 能做什么 |
| --- | --- |
| suggest | 只能生成变更计划 |
| apply_safe | 可以执行 L1 安全变更 |
| admin_apply | 可以在管理员授权后执行 L2/L3 |

默认给 AI：

```text
suggest
```

也就是：

```text
AI 能建议怎么改，但用户点确认才改。
```

### AI 变更必须可解释

AI 生成的计划必须包含：

- 为什么要改
- 改哪些表
- 加哪些字段
- 关联到哪里
- 会不会影响已有数据
- 是否需要备份
- 是否需要管理员确认

不要只给 JSON。

### 版本和回滚

每次应用组件/结构变更都要记录：

```text
app_id
component_id
dataset_id
change_plan_id
change_kind
before_schema
after_schema
actor
created_at
applied_at
```

低风险变更可以“继续修改”。

高风险变更至少要能：

- 查看变更历史
- 查看执行人
- 查看变更前后
- 从备份恢复

第一版不必做完美 schema rollback，但必须保留审计和备份路径。

## 灵活和严谨的边界

允许灵活：

- 加字段
- 加表
- 加关联
- 加视图
- 加操作
- 组合组件
- 复用共享表

保持严谨：

- 不允许绕过校验写数据
- 不允许 AI 静默删字段/删表
- 不允许无预检新增必填/唯一约束
- 不允许 token 静默扩大敏感权限
- 不允许直接改底层表结构绕开审计

这就是 DataSrv 和普通低代码表单工具的差别。

## 已设计系统沉淀为模板

一套已经设计好的 MIS 系统，也应该能变成模板。

这很重要：

- 实施团队给 A 公司搭了一套进销存，可以沉淀成“进销存行业模板”。
- 用户自己改过字段、表、关联，可以保存成“公司内部模板”。
- AI 帮用户设计出一套 CRM，可以保存成可复用模板。
- 多个项目中反复出现的结构，可以抽成组件。

### 可以导出三种资产

#### 1. 导出为应用蓝图

适合整套系统复用。

例如：

```text
当前系统：五金行业进销存
导出为：hardware_inventory.blueprint
```

包含：

- 使用了哪些组件
- 创建了哪些表
- 表之间怎么关联
- 默认视图
- 默认操作
- 默认 token 套餐
- 推荐首页看板

不包含业务数据。

#### 2. 导出为业务组件

适合把某块能力复用到其他系统。

例如：

```text
售后工单
设备台账
会员积分
项目收支
合同履约
```

一个组件可以包含：

- 一张或多张表
- 字段
- 关联
- 视图
- 操作
- 默认权限

#### 3. 导出为表模板

适合复用单张表。

例如：

```text
客户扩展字段模板
设备档案表
门店表
车辆表
```

### 导出流程

```text
选择当前应用
  -> 选择导出范围
  -> 系统扫描表、字段、关联、视图、操作
  -> 识别共享主数据
  -> 生成模板草稿
  -> 用户编辑名称、说明、行业标签
  -> 校验模板完整性
  -> 保存为模板
```

### 导出时必须处理的关键问题

#### 共享表不要重复导出

如果当前系统用了共享客户表：

```text
party.customers
```

导出应用模板时，不应该把它死死复制成一张新表。

应该导出为角色依赖：

```json
{
  "role": "customer",
  "required": true,
  "preferred_dataset": "party.customers"
}
```

这样别人安装模板时：

- 已有客户表：复用
- 没有客户表：创建

#### 关联要从 dataset 变回 role

当前系统里实际关联可能是：

```text
service.tickets.customer_ref -> party.customers
```

导出模板时应该变成：

```text
service.tickets.customer_ref -> role:customer
```

否则模板换一个环境就不灵活了。

#### 敏感字段要保留策略

例如：

```text
手机号
身份证
薪资
银行账号
税号
```

导出模板时保留：

- sensitive 标记
- 默认脱敏策略
- 推荐 token 不允许访问敏感字段

#### 数据默认不导出

模板默认只导结构，不导业务数据。

可选导出：

- 示例数据
- 枚举字典
- 初始化配置

不要导出：

- 真实客户
- 真实订单
- 真实付款
- 真实员工
- token secret
- 审计日志

### 后端 API：模板导出

```http
POST /api/v1/simple/templates/export-plan
POST /api/v1/simple/templates/export
GET  /api/v1/simple/templates
POST /api/v1/simple/templates/{templateId}/install
```

### 导出计划示例

```json
{
  "app_id": "inventory",
  "export_kind": "blueprint",
  "title": "五金行业进销存",
  "include_sample_data": false
}
```

返回：

```json
{
  "plan_id": "tpl_export_xxx",
  "summary": "将导出 8 张表、12 个关联、6 个视图、5 个操作。",
  "shared_roles": [
    {"role": "customer", "dataset": "party.customers", "mode": "dependency"},
    {"role": "supplier", "dataset": "party.suppliers", "mode": "dependency"}
  ],
  "owned_tables": [
    "inventory.items",
    "inventory.warehouses",
    "inventory.stock",
    "inventory.movements"
  ],
  "warnings": [
    "不会导出真实业务数据。",
    "不会导出 token secret。"
  ],
  "can_export": true
}
```

### 模板版本

模板需要版本号。

```text
hardware_inventory@1.0.0
hardware_inventory@1.1.0
hardware_inventory@2.0.0
```

版本规则：

- patch：文案、描述、视图小修
- minor：新增字段、新增视图、新增可选组件
- major：字段类型变化、表拆分、关系变化

安装模板后，系统要记录：

```text
installed_template_id
installed_template_version
local_customizations
```

这样以后模板升级时，能判断用户本地改过什么。

### 模板市场/内部模板库

第一版不需要做市场。

但数据结构要预留：

- 官方模板
- 企业内部模板
- 项目模板
- AI 生成模板
- 用户私有模板

模板来源要显示清楚，避免用户误装不可信模板。

### AI 如何把系统沉淀成模板

AI 可以帮忙：

- 分析当前表结构
- 识别哪些表是共享主数据
- 给字段补说明
- 识别缺失关联
- 生成模板介绍
- 生成安装后的上手说明

AI 不能直接发布模板。

流程：

```text
AI 生成模板草稿
  -> 后端校验
  -> 管理员预览
  -> 管理员保存/发布
```

### 和变更计划的关系

模板导出也是一种计划。

它不改现有数据，但会生成可复用资产，所以也要有：

- export plan
- 预览
- 校验
- 保存
- 审计

这让 DataSrv 能从“搭一个系统”进化成“沉淀企业和行业模板”。

## 模板派生和旧系统关联

用户在模板基础上改了，比如改了“用户信息表”，也应该能保存成新模板。

但不能覆盖原模板。

正确方式是：派生新模板。

```text
原模板：identity.user@1.0.0
本地修改：增加工号、门店、岗位等级、入职渠道
保存为：retail.user@1.0.0
```

新模板要记录自己来自哪里：

```json
{
  "template_id": "retail.user",
  "version": "1.0.0",
  "derived_from": {
    "template_id": "identity.user",
    "version": "1.0.0"
  },
  "base_role": "user",
  "role": "user",
  "changes": [
    {"kind": "add_field", "field": "store_ref"},
    {"kind": "add_field", "field": "job_grade"},
    {"kind": "add_field", "field": "hire_source"}
  ]
}
```

### 关键：role 不变，dataset 可以变

“用户信息”在系统里应该有稳定角色：

```text
role: user
```

原模板可以映射到：

```text
role:user -> company.users
```

零售行业派生模板也可以仍然映射到：

```text
role:user -> company.users
```

只是字段更多。

这样旧系统里所有引用用户的地方不用改：

```text
订单.owner_ref -> role:user
付款.created_by_ref -> role:user
审批.approver_ref -> role:user
库存流水.operator_ref -> role:user
```

编译后仍然连到同一张实际表：

```text
company.users
```

### 如果用户想另建一张扩展表

有些场景不适合把所有字段塞进 `company.users`。

比如：

```text
基础用户信息：company.users
门店员工信息：retail.store_staff
```

这时派生模板可以声明扩展关系：

```text
retail.store_staff.user_ref -> role:user
```

也就是：

```text
store_staff 是 user 的扩展表
```

旧系统继续用 `role:user`。

新零售系统可以同时使用：

```text
company.users
retail.store_staff
```

### 派生模板安装到已有系统

假设旧系统已有：

```text
company.users
sales.orders.owner_ref -> company.users
finance.payments.created_by_ref -> company.users
```

用户安装 `retail.user@1.0.0`。

向导应该提示：

```text
检测到当前系统已有用户表 company.users。

你可以：
1. 在现有用户表上增加零售字段
2. 新建门店员工扩展表，并关联现有用户表
```

推荐：

- 少量通用字段：加到现有表
- 行业专属复杂字段：新建扩展表

### 保存新模板时要带映射信息

保存模板不能只保存字段。

还要保存：

```text
role
dataset
derived_from
field_changes
relationship_changes
compatibility
```

示例：

```json
{
  "template_id": "retail.user",
  "kind": "component",
  "role": "user",
  "dataset_strategy": "extend_existing",
  "compatible_with": [
    {"role": "user", "datasets": ["company.users", "hr.employees"]}
  ],
  "derived_from": {"template_id": "identity.user", "version": "1.0.0"},
  "field_changes": [
    {"kind": "add_field", "key": "store_ref", "type": "record_ref", "ref_role": "store"},
    {"kind": "add_field", "key": "job_grade", "type": "string"},
    {"kind": "add_field", "key": "hire_source", "type": "string"}
  ]
}
```

### 和旧系统信息怎么关联

靠三层关联：

#### 1. 安装记录

系统记录当前应用安装过哪些模板：

```text
app_id
template_id
template_version
installed_at
installed_by
derived_from
```

#### 2. role 映射

系统记录每个业务角色当前对应哪张实际表：

```text
app_id: crm
role:user -> company.users
role:customer -> party.customers
role:sales_order -> sales.orders
```

#### 3. 本地定制记录

系统记录用户在模板基础上改了什么：

```text
template_id: identity.user@1.0.0
local_changes:
  - add_field store_ref
  - add_field job_grade
  - add_field hire_source
saved_as_template: retail.user@1.0.0
```

这样以后可以回答：

```text
这个用户表来自哪个模板？
本地改过什么？
哪些应用正在引用它？
能不能升级到新模板？
```

### 模板升级时怎么处理本地修改

如果官方 `identity.user` 从 1.0 升到 1.1，新增了 `avatar_url` 字段。

本地 `retail.user` 已经加了 `store_ref`、`job_grade`。

升级时生成合并计划：

```text
官方新增 avatar_url
本地已有 store_ref、job_grade
无冲突，可以合并
```

如果官方也新增了 `job_grade`，但类型不同：

```text
冲突：job_grade
官方类型：number
本地类型：string

请选择：
1. 保留本地字段
2. 采用官方字段
3. 新建字段 official_job_grade
```

### 后端 API：保存为模板

```http
POST /api/v1/simple/templates/save-from-app
POST /api/v1/simple/templates/save-from-component
POST /api/v1/simple/templates/{templateId}/diff
POST /api/v1/simple/templates/{templateId}/upgrade-plan
```

保存用户表为新模板：

```json
{
  "source_app_id": "retail_pos",
  "source_role": "user",
  "source_dataset": "company.users",
  "new_template_id": "retail.user",
  "title": "零售门店用户",
  "derived_from": {
    "template_id": "identity.user",
    "version": "1.0.0"
  },
  "dataset_strategy": "extend_existing"
}
```

返回：

```json
{
  "template_id": "retail.user",
  "version": "1.0.0",
  "role": "user",
  "derived_from": "identity.user@1.0.0",
  "fields": ["user_no", "name", "mobile", "store_ref", "job_grade", "hire_source"],
  "relationships": ["store_ref -> role:store"],
  "can_install_into_existing_user_table": true
}
```

### 简单结论

模板不是一次性复制。

模板要像 Git 分支一样：

```text
来源可追踪
本地可修改
可另存为新模板
可和旧系统 role 继续关联
可对比
可升级
可处理冲突
```

但 UI 不要说 Git。

UI 只说：

```text
保存为新模板
来自：基础用户模板
可用于：已有用户表或新系统
```

## 可视化关系设计器

关联关系需要可视化设计器，但它不是第一入口。

推荐分两层：

```text
新手：向导创建，系统自动连线
实施/管理员：可视化设计器检查、调整、补充连线
```

设计器不应该像专业数据库 ER 工具那么复杂。它应该是企业 MIS 关系图。

### 设计器展示什么

节点：

- 应用
- 业务组件
- 表
- 共享主数据

连线：

- 一对多：客户 -> 订单
- 引用：订单.owner_ref -> 用户
- 扩展：门店员工扩展 -> 用户
- 单据来源：付款 -> 发票 / 订单

用户看到：

```text
客户  —— 订单
订单  —— 付款
商品  —— 库存
仓库  —— 库存
用户  —— 订单负责人
```

内部保存：

```text
sales.orders.customer_ref -> role:customer
finance.payments.order_ref -> role:sales_order
inventory.stock.item_ref -> role:item
inventory.stock.warehouse_ref -> role:warehouse
sales.orders.owner_ref -> role:user
```

### 设计器能做什么

第一版支持：

- 查看当前应用关系图
- 拖线新增关联
- 修改关联字段名
- 选择关联类型
- 检查缺失目标
- 保存为变更计划

不要第一版就做：

- 复杂基数约束
- 多态引用编辑器
- 自动 SQL 迁移脚本
- 拖拽生成复杂业务流程
- 大型画布自动布局调优

### 连线创建流程

用户从“付款”拖线到“订单”。

系统弹出：

```text
你要让付款关联订单吗？

字段名：order_ref
显示名：关联订单
关联到：销售订单
是否必填：否
```

确认后，不直接改表。

生成变更计划：

```text
将在付款表新增字段 order_ref
字段类型：record_ref
关联目标：role:sales_order
实际目标表：sales.orders
风险：低
```

用户确认后执行。

### 关联类型

先只支持四种用户能懂的关系：

| UI 关系 | 内部实现 |
| --- | --- |
| 关联到 | `record_ref` |
| 属于 | `record_ref` |
| 扩展 | 新表含 `user_ref/customer_ref/...` |
| 来源于 | `record_ref` + source role |

不要让用户先选“一对一、一对多、多对多”。这些概念可以由系统解释。

例如：

```text
订单属于客户
```

内部就是：

```text
sales.orders.customer_ref -> role:customer
```

### 多对多怎么处理

MIS 里多对多通常用明细表/中间表表达。

例如订单和商品：

```text
销售订单
商品
订单明细
```

设计器里用户可以看到：

```text
销售订单 —— 订单明细 —— 商品
```

不要让用户直接画“订单 <-> 商品”然后隐藏中间结构。企业数据要严谨，明细表要能承载数量、单价、金额、税率。

### 设计器保存的不是图，而是组件关系

画布只是编辑方式。

后端保存的是：

```text
components
roles
datasets
relationships
change_plans
```

不要把坐标当业务真相。

可以保存 UI 布局：

```text
node position
collapsed groups
color
```

但业务关系必须来自后端结构。

### AI 和设计器

AI 可以在设计器里做两件事：

1. 解释当前关系图。
2. 根据一句话生成连线建议。

例如用户说：

```text
付款应该能关联订单和发票。
```

AI 生成建议：

```text
付款.order_ref -> 销售订单
付款.invoice_ref -> 发票
```

然后进入变更计划，不直接执行。

### 什么时候需要设计器

需要：

- 多应用共享主数据时
- 用户要加表、加关联时
- 模板派生时
- 导出模板前检查结构时
- AI 设计后让人审核时

不需要：

- 第一次创建 CRM/进销存
- 只导入数据
- 只生成 token
- 只查询记录

### 推荐实现顺序

1. 先做后端关系模型和变更计划。
2. 再做只读关系图。
3. 再支持拖线新增关联。
4. 最后支持 AI 辅助补线。

这样不会为了画布拖慢核心数据能力。

## 推荐应用模板

第一批只做少量高质量 MIS 模板，别铺太大。

### 1. CRM 客户销售管理

包含表：

- 客户
- 联系人
- 商机
- 销售订单
- 回款

核心关联：

- 联系人属于客户
- 商机属于客户
- 销售订单属于客户
- 销售订单可关联商机
- 回款关联销售订单或发票

默认视图：

- 客户列表
- 客户详情
- 商机跟进
- 订单列表
- 待回款订单

默认操作：

- 新增客户
- 新增联系人
- 新增商机
- 新增订单
- 登记回款

### 2. 进销存管理

包含表：

- 客户
- 供应商
- 商品/物料
- 采购订单
- 销售订单
- 仓库
- 库存
- 出入库流水

核心关联：

- 采购订单关联供应商
- 销售订单关联客户
- 库存关联商品/物料和仓库
- 出入库流水关联商品/物料、仓库和来源单据

默认视图：

- 商品库存
- 库存预警
- 采购订单
- 销售订单
- 出入库明细

默认操作：

- 新增商品
- 新增采购订单
- 新增销售订单
- 入库
- 出库
- 调整库存

### 3. 财务管理

包含表：

- 客户
- 供应商
- 发票
- 收款
- 付款
- 报销
- 会计科目
- 凭证

核心关联：

- 发票关联客户、供应商或合同
- 收款关联发票或销售订单
- 付款关联发票、采购订单或报销
- 凭证关联收款、付款、报销或发票
- 凭证明细关联会计科目

默认视图：

- 应收账款
- 应付账款
- 逾期发票
- 报销待处理
- 凭证列表
- 科目余额

### 4. ERP 轻量综合管理

包含表：

- 客户
- 供应商
- 商品/物料
- 合同
- 销售订单
- 采购订单
- 仓库
- 库存
- 发票
- 收付款
- 凭证

核心关联：

- 合同关联客户或供应商
- 销售订单关联客户和合同
- 采购订单关联供应商
- 库存关联商品/物料和仓库
- 发票关联订单或合同
- 收付款关联发票
- 凭证关联财务单据

### 5. 合同管理

包含表：

- 合同
- 客户
- 供应商
- 发票
- 付款

核心关联：

- 合同关联客户或供应商
- 发票关联合同
- 付款关联发票

## 后端向导设计

新增推荐 API，不替代现有 API。

### 1. 列出应用模板

```http
GET /api/v1/simple/apps/templates
```

返回：

```json
{
  "items": [
    {
      "id": "mis.inventory",
      "title": "进销存管理",
      "description": "客户、供应商、商品、采购、销售、仓库、库存、出入库",
      "tables": ["客户", "供应商", "商品", "采购订单", "销售订单", "仓库", "库存", "出入库流水"],
      "recommended": true
    }
  ]
}
```

### 2. 预览创建结果

```http
POST /api/v1/simple/apps/preview
```

请求：

```json
{
  "template_id": "mis.inventory",
  "app_name": "进销存管理"
}
```

返回要让用户看懂：

```json
{
  "title": "将创建进销存管理",
  "tables": [
    {"name": "客户", "fields": 8},
    {"name": "供应商", "fields": 9},
    {"name": "商品", "fields": 8},
    {"name": "采购订单", "fields": 11},
    {"name": "销售订单", "fields": 10},
    {"name": "仓库", "fields": 6},
    {"name": "库存", "fields": 8},
    {"name": "出入库流水", "fields": 12}
  ],
  "relationships": [
    "采购订单 -> 供应商",
    "销售订单 -> 客户",
    "库存 -> 商品",
    "库存 -> 仓库",
    "出入库流水 -> 商品",
    "出入库流水 -> 仓库"
  ],
  "views": ["商品库存", "库存预警", "采购订单", "销售订单", "出入库明细"],
  "tokens": [
    {"preset": "只读查询", "recommended": true},
    {"preset": "录入数据"}
  ]
}
```

### 3. 一键创建应用

```http
POST /api/v1/simple/apps
```

请求：

```json
{
  "template_id": "mis.inventory",
  "app_name": "进销存管理",
  "create_sample_data": true
}
```

返回：

```json
{
  "app_id": "inventory",
  "title": "进销存管理",
  "tables": [
    {"id": "sales.customers", "title": "客户"},
    {"id": "procurement.suppliers", "title": "供应商"},
    {"id": "inventory.items", "title": "商品"},
    {"id": "procurement.purchase_orders", "title": "采购订单"},
    {"id": "sales.orders", "title": "销售订单"},
    {"id": "inventory.warehouses", "title": "仓库"},
    {"id": "inventory.stock", "title": "库存"},
    {"id": "inventory.movements", "title": "出入库流水"}
  ],
  "next": {
    "action": "create_token",
    "label": "创建访问 token"
  }
}
```

### 4. 一键授权

```http
POST /api/v1/simple/apps/{appId}/tokens
```

请求：

```json
{
  "preset": "readonly",
  "expires_in_days": 90
}
```

返回：

```json
{
  "token": "mcd_xxx",
  "token_id": "inventory-readonly",
  "can": ["查询商品库存", "查询采购订单", "查询销售订单", "查询出入库明细"],
  "cannot": ["修改库存", "删除数据", "管理系统", "查看财务敏感字段"],
  "examples": [
    {
      "title": "查询商品库存",
      "method": "POST",
      "path": "/api/v1/data/views/inventory.stock_list/query"
    }
  ],
  "next": {
    "action": "test_query",
    "label": "测试查询"
  }
}
```

### 5. 测试 token

```http
POST /api/v1/simple/apps/{appId}/test-query
```

请求：

```json
{
  "token_id": "hr-readonly"
}
```

返回：

```json
{
  "ok": true,
  "message": "查询成功",
  "sample_records": []
}
```

这一步结束后，才算上手完成。

## 一条最简单的完整路径

用户只做 4 件事：

```text
1. 选择“进销存管理”
2. 点“创建”
3. 点“生成只读 token”
4. 点“测试查询”
```

系统做全部复杂工作：

```text
创建多张表
创建字段
创建表关联
创建默认视图
创建默认操作
创建 token 权限
记录审计日志
返回示例 API
```

## 关联怎么简化

用户不应该手工配置外键。

模板里用 `record_ref` 字段表达关联。

例如：

```text
采购订单.supplier_ref -> 供应商
销售订单.customer_ref -> 客户
库存.item_ref -> 商品
库存.warehouse_ref -> 仓库
出入库流水.item_ref -> 商品
出入库流水.warehouse_ref -> 仓库
```

UI 里显示成人话：

```text
采购订单 关联 供应商
销售订单 关联 客户
库存 关联 商品和仓库
出入库流水 关联 商品和仓库
```

后端负责：

- 字段类型是 `record_ref`
- `config.ref_dataset` 指向目标表
- 查询详情时可返回关联记录
- 导入时可按编号/name 辅助匹配记录

第一版不做复杂数据库外键约束，避免导入困难；但要做引用校验和缺失提示。

## 新手模式和专家模式

### 新手模式

只显示：

```text
应用
数据
授权
```

新手看到的是：

```text
进销存管理
CRM 客户销售管理
财务管理
ERP 轻量综合管理
财务收付款管理
```

不是：

```text
Dataset
BusinessView
BusinessAction
OperationPlan
APIKeyPolicy
```

### 专家模式

显示原有完整能力：

```text
业务表
字段
视图
操作
连接器
审计
备份
治理
```

## 产品命名再简化

| 内部 | 新手 UI |
| --- | --- |
| Domain | 应用 |
| Dataset | 表 |
| FieldDefinition | 字段 |
| Record | 数据 |
| BusinessView | 查询入口 |
| BusinessAction | 写入入口 |
| APIKeyPolicy | 授权 token |
| DatasetRelationship | 关联 |

## 实现上怎么少改

现有代码已经有基础：

- `datasetTemplates`：已有单表模板。
- `BootstrapTemplates`：已有按 domain 批量创建能力。
- `record_ref`：已有表关联表达。
- `ListRelationships`：已有关联发现。
- `BusinessView` / `BusinessAction`：已有视图和动作模型。
- `APIKeyPolicy`：已有 token 权限模型。

所以不要先重构底层。

先加一层后端向导：

```text
simple app template
  -> 调用现有 BootstrapTemplates
  -> 调用现有关系发现
  -> 调用现有 API key 创建
  -> 返回下一步和示例
```

这层的目标不是更强，而是更少选择。

## 第一版接口范围

只做 5 个接口：

```http
GET  /api/v1/simple/apps/templates
POST /api/v1/simple/apps/preview
POST /api/v1/simple/apps
POST /api/v1/simple/apps/{appId}/tokens
POST /api/v1/simple/apps/{appId}/test-query
```

只做 2 个应用模板：

- CRM 客户销售管理
- 进销存管理

先把“能 1 分钟跑通”做顺。

## 当前问题

现有 DataSrv 能力很强，但入口复杂：

- 概念太多：dataset、field、record、business action、business view、dashboard、report、connector、event、operation plan、approval、quality、governance、backup 全部摆出来，新用户不知道先做什么。
- 管理后台像“全部 API 控制台”，不是“企业数据工作台”。
- 简单场景也要理解很多高级能力，比如 operation plan、schema proposal、governance evidence。
- agent 接入能做很多事，但缺少一条推荐的最短路径。
- 权限很细，但用户需要的是“这类人能看什么、能改什么”的业务化表达。

## 设计原则

### 1. 默认只给四个主概念

普通用户只需要理解：

- 业务表：存一类业务数据，比如客户、订单、员工、合同。
- 字段：业务表里的列，有类型和校验规则。
- 数据记录：一条具体业务数据。
- 视图：给某类人或 agent 使用的查询入口。

高级概念收进二级入口：

- 审批
- 导入导出任务
- 连接器
- 审计日志
- 备份恢复
- 权限策略
- 质量检查
- 操作计划

### 2. 先模板，后自定义

企业信息系统常见表不应该从空白开始。

内置模板优先：

- 客户
- 联系人
- 销售机会
- 订单
- 合同
- 发票
- 付款
- 员工
- 供应商
- 工单
- 项目

用户第一次进入时，推荐路径是：

1. 选择业务场景。
2. 选择模板。
3. 改字段名和必填项。
4. 导入 Excel/CSV 或手工录入。
5. 生成默认视图。

### 3. 严谨性内置，不让用户到处配置

默认开启：

- tenant 隔离
- 字段类型校验
- 必填校验
- 唯一字段校验
- 敏感字段脱敏
- 修改历史
- 审计日志
- 导入前预检
- 批量修改/删除前 dry run
- 备份前置提醒

用户不用理解这些机制，也能得到保护。

### 4. 高级能力按场景出现

不要把所有能力放在首页。

例如：

- 用户点“导入数据”时，才出现导入预检、错误行、导入任务。
- 用户点“批量修改”时，才出现 dry run、影响记录、确认执行。
- 用户点“开放给 agent”时，才出现 API key、可访问视图、可执行动作。
- 用户点“合规审计”时，才出现审计导出、治理证据、访问风险。

## 简化后的产品分层

```text
企业用户看到的层
  业务表 / 字段 / 数据 / 视图

管理员看到的层
  权限 / 导入导出 / 备份 / 审计 / 连接器

系统内部层
  dataset / record / action / event / job / policy / audit
```

对外命名建议：

| 内部概念 | UI 名称 | 说明 |
| --- | --- | --- |
| Dataset | 业务表 | 用户最容易理解 |
| FieldDefinition | 字段 | 类型、必填、唯一、敏感 |
| Record | 数据记录 | 一行业务数据 |
| BusinessView | 视图 | 给人和 agent 查询的安全入口 |
| BusinessAction | 操作 | 标准化写入动作 |
| APIKeyPolicy | 接入密钥 | 给 agent/系统用 |
| OperationPlan | 执行预案 | 高风险批量操作的确认单 |
| RecordApproval | 审批 | 需要人工确认的数据变更 |
| AuditLog | 操作日志 | 谁在什么时候做了什么 |

## 推荐信息架构

Web Console 默认只保留 5 个一级入口：

1. 首页
2. 业务表
3. 数据
4. 接入
5. 管理

### 首页

展示能让用户继续工作的内容：

- 已有业务表数量
- 最近导入
- 待处理错误
- 待审批
- 最近修改
- 数据健康提示

避免首页展示复杂治理证据。

### 业务表

负责建模：

- 从模板创建业务表
- 创建空白业务表
- 编辑字段
- 设置字段类型
- 设置必填、唯一、敏感
- 预览样例数据

字段类型第一阶段保持少：

- 文本
- 长文本
- 数字
- 金额
- 日期
- 日期时间
- 枚举
- 布尔
- 关联记录
- JSON 扩展

### 数据

负责日常操作：

- 选择业务表
- 表格浏览
- 筛选
- 搜索
- 新增
- 编辑
- 删除
- 导入
- 导出
- 查看修改历史

默认不让用户写 JSON。JSON 编辑器只放到“高级编辑”。

### 接入

负责给 agent 和外部系统用：

- 创建接入密钥
- 选择能访问哪些业务表或视图
- 选择能执行哪些操作
- 查看最近调用
- 复制示例请求
- 禁用或轮换密钥

这里要提供“接入套餐”：

- 只读查询
- 可新增记录
- 可新增和更新
- 审计员
- 管理员

默认推荐只读查询。

### 管理

放低频但重要能力：

- 用户和管理员
- 租户
- 审计日志
- 备份恢复
- 质量检查
- 连接器
- 高级治理

## 最短上手流程

最短路径应该固定成一句话：

```text
从模板新建数据 -> 新建授权 token -> 选择 token 能访问的数据 -> 复制示例调用 -> 开始读写
```

用户不应该在第一天理解 dataset、view、action、policy。系统自动帮用户生成这些内部对象。

## 一分钟上手路径

### Step 1：从模板新建数据

入口：`业务表 -> 从模板新建`

用户选择模板，例如“客户”。

填写：

- 业务表名称：客户
- 所属业务：销售
- 默认字段：直接使用模板字段

点击“创建”后，系统自动生成：

- 业务表：`sales.customers`
- 字段：客户名称、联系电话、客户等级、负责人、备注
- 默认安全视图：`sales.customer_list`
- 默认详情视图：`sales.customer_detail`
- 默认操作：新增客户、更新客户
- 默认导入模板：客户 CSV 模板

页面显示下一步按钮：

```text
下一步：创建接入 token
```

### Step 2：新授权 token 绑上去

入口：创建成功页上的“创建接入 token”，或 `接入 -> 新建 token`

用户只选授权套餐：

- 只读查询
- 新增数据
- 新增和更新
- 管理员

默认推荐：只读查询。

然后选择授权范围：

- 允许访问：客户
- 允许视图：客户列表、客户详情
- 允许敏感字段：否
- 有效期：90 天

点击“生成 token”后，系统自动生成：

- API key policy
- token secret
- token prefix
- 最近调用统计
- 可访问能力清单

页面只显示一次 token：

```text
mcd_xxxxxxxxxxxxxxxxx
```

页面同时显示“已绑定到客户数据”：

```text
这个 token 可以：
- 查询客户列表
- 查询客户详情

这个 token 不可以：
- 查看敏感字段
- 修改客户
- 删除客户
- 访问其他业务表
```

### Step 3：然后马上给用户三个可执行入口

生成 token 后，不要让用户自己找 API。

页面展示三个按钮：

1. 复制 curl 示例
2. 打开在线测试
3. 交给 MaClaw agent 使用

#### 复制 curl 示例

```http
POST /api/v1/data/views/sales.customer_list/query
Authorization: Bearer mcd_xxxxxxxxxxxxxxxxx
Content-Type: application/json

{
  "limit": 20
}
```

如果授权套餐包含“新增数据”，再显示：

```http
POST /api/v1/data/business-actions/sales.customer_create/execute
Authorization: Bearer mcd_xxxxxxxxxxxxxxxxx
Content-Type: application/json

{
  "data": {
    "customer_name": "示例客户",
    "phone": "13800000000",
    "level": "A",
    "owner": "张三"
  },
  "dry_run": true
}
```

#### 打开在线测试

在线测试不暴露复杂 API，只提供：

- 查询前 20 条
- 按关键词搜索
- 新增一条样例数据
- 查看 token 权限

#### 交给 MaClaw agent 使用

显示一段可复制配置：

```json
{
  "datasrv_url": "http://127.0.0.1:18180",
  "token": "mcd_xxxxxxxxxxxxxxxxx",
  "default_table": "sales.customers",
  "recommended_view": "sales.customer_list"
}
```

并给自然语言提示：

```text
你现在可以让 MaClaw：
- 查询客户列表
- 搜索某个客户
- 整理客户数据
```

如果 token 有写权限：

```text
你还可以让 MaClaw：
- 新增客户
- 更新客户信息
```

### Step 4：首条数据体验

如果业务表为空，系统必须提示下一步：

```text
客户表已创建，但还没有数据。

你可以：
1. 手工新增一条客户
2. 导入 CSV
3. 让 agent 根据已有资料整理客户
```

推荐默认按钮：`新增一条样例数据`。

新增成功后，回到数据页，用户能马上看到第一条记录。

### Step 5：成功状态

上手流程的成功标志不是“创建完成”，而是：

```text
业务表已创建
token 已授权
至少一次查询成功
```

首页显示：

```text
客户：已可用
- 5 个字段
- 1 个授权 token
- 最近查询成功
```

## 首次体验不该出现的东西

第一次创建业务表和 token 时，不出现：

- Schema Proposal
- Operation Plan
- Governance Evidence
- Dead Letter
- Event Contract
- Connector Sync State
- OpenAPI
- 原始 JSON policy
- raw dataset 权限细节

这些能力仍然存在，但必须藏在“高级设置”里。

## 上手流程背后的内部映射

用户动作：

```text
从模板创建客户
```

内部动作：

```text
CreateDataset
UpsertFields
CreateBusinessView(customer_list)
CreateBusinessView(customer_detail)
CreateBusinessAction(customer_create)
CreateBusinessAction(customer_update)
AppendAuditLog
```

用户动作：

```text
创建只读 token，绑定客户
```

内部动作：

```text
CreateAPIKeyPolicy
AllowedViews = [sales.customer_list, sales.customer_detail]
AllowedActions = []
AllowRawData = false
AllowSensitive = false
ExpiresAt = now + 90 days
AppendAuditLog
```

用户动作：

```text
测试查询
```

内部动作：

```text
Authenticate token
Check policy
QueryBusinessView
Mask sensitive fields
TouchAPIKeyPolicyUse
Return records
```

### 场景 A：建一个客户表

1. 打开 DataSrv。
2. 点“业务表”。
3. 点“从模板创建”。
4. 选择“客户”。
5. 确认字段：
   - 客户名称：文本、必填、唯一
   - 联系电话：文本、敏感
   - 客户等级：枚举
   - 负责人：文本
6. 点“创建”。
7. 自动生成：
   - 客户列表视图
   - 客户详情视图
   - 新增客户操作
   - 更新客户操作

### 场景 B：导入订单 CSV

1. 进入“数据”。
2. 选择“订单”业务表。
3. 点“导入”。
4. 上传 CSV。
5. 系统自动映射字段。
6. 先预检。
7. 显示：
   - 可导入数量
   - 错误行
   - 重复行
   - 缺失字段
8. 用户确认导入。
9. 系统生成导入记录和审计日志。

### 场景 C：给 agent 开查询权限

1. 进入“接入”。
2. 点“创建接入密钥”。
3. 选择“只读查询”。
4. 选择视图：
   - 客户列表
   - 订单列表
5. 不允许原始数据。
6. 不允许敏感字段。
7. 生成密钥。
8. 显示 agent 示例：

```http
GET /api/v1/data/views/sales.customer_list/query
Authorization: Bearer mcd_xxx
```

## 数据严谨性设计

### 字段规则

每个字段可以有这些规则：

- 类型
- 必填
- 唯一
- 敏感
- 默认值
- 枚举选项
- 最小/最大值
- 日期范围
- 正则校验

第一版 UI 只暴露常用规则：

- 类型
- 必填
- 唯一
- 敏感
- 枚举选项

其他规则放“高级规则”。

### 写入规则

所有写入都走同一流程：

```text
请求
  -> 鉴权
  -> 权限判断
  -> 字段校验
  -> 唯一性校验
  -> 写入事务
  -> 记录修订历史
  -> 写审计日志
  -> 触发事件
```

### 批量操作规则

批量修改和批量删除必须分两步：

1. 预览影响。
2. 明确确认。

普通用户看到的是：

- 将影响多少条记录
- 哪些字段会变
- 是否有风险
- 是否建议先备份

内部仍可使用 `OperationPlan`，但 UI 不直接把它叫“操作计划”，叫“执行确认”。

## API 简化建议

保留现有 API，但给新用户推荐三组 API。

### 1. 表结构 API

```http
GET  /api/v1/data/datasets
POST /api/v1/data/datasets
GET  /api/v1/data/datasets/{datasetId}/fields
PUT  /api/v1/data/datasets/{datasetId}/fields
```

### 2. 数据 API

```http
GET  /api/v1/data/datasets/{datasetId}/records
POST /api/v1/data/datasets/{datasetId}/records
POST /api/v1/data/datasets/{datasetId}/records/query
PATCH /api/v1/data/datasets/{datasetId}/records/{recordId}
DELETE /api/v1/data/datasets/{datasetId}/records/{recordId}
```

### 3. 安全视图和动作 API

```http
GET  /api/v1/data/views
POST /api/v1/data/views/{viewId}/query
GET  /api/v1/data/business-actions
POST /api/v1/data/business-actions/{actionId}/execute
```

给 agent 推荐优先使用“视图 + 动作”，少直接碰 dataset/record。

## UI 降复杂方案

### 默认模式：业务模式

显示：

- 首页
- 业务表
- 数据
- 接入
- 管理

隐藏：

- event contracts
- dead letters
- operation plans
- raw JSON imports
- governance evidence
- connector advanced config
- raw audit CSV

### 高级模式：系统模式

管理员打开后显示全部能力。

入口可以是：

- 管理 -> 高级功能
- URL 参数
- 本地设置

### 文案替换

建议替换部分产品词：

| 当前词 | 建议词 |
| --- | --- |
| Dataset | 业务表 |
| Schema Proposal | 字段建议 |
| Operation Plan | 执行确认 |
| Governance Evidence | 审计证据 |
| Dead Letter | 失败事件 |
| Connector | 系统连接 |
| Business Action | 标准操作 |
| Business View | 安全视图 |

## 实现路线

## MaClaw GUI MIS 工具兼容方案

结论：不要替换现有 GUI MIS 工具链。

当前 MaClaw GUI 已经有一套可工作的企业数据调用路径：

```text
agent 自然语言
  -> /api/v1/data/intent/resolve
  -> 选择 BusinessAction
  -> AgentView 表单填写
  -> dry_run 校验
  -> 用户确认
  -> execute commit
```

这条路径适合“已有业务动作后，agent 帮用户执行”。新设计要补的是“还没有系统时，如何一分钟建好一套可用 MIS 数据包”。

所以兼容原则是：

- 老 `/api/v1/data/*` 继续保留，作为专家层和执行层。
- 新 `/api/v1/simple/*` 只做向导层，负责创建、装配、授权、测试。
- GUI 新增简单动作，但旧动作不下线。
- AgentView 继续复用已有表单、资源选择、字段映射、审批、结果浏览能力。
- `GET /api/v1/data/capabilities` 可以扩展返回 simple 能力，但不能改变旧字段含义。

### 当前 GUI 依赖点

| GUI 功能 | 当前入口 | 作用 | 新设计影响 | 兼容状态 |
| --- | --- | --- | --- | --- |
| MIS 连接设置 | `GetMISDataConfig` / `SaveMISDataConfig` / `TestMISDataConnection` | 保存 endpoint、token、tenant、user、role，并测试 `/readyz` 和 `/api/v1/data/backups` | 增加“打开向导”入口；测试可优先测 simple health/capabilities，失败再走旧接口 | 保留 |
| AppsPage 自动发现 | `GET /api/v1/data/capabilities` | 按 domain 生成 DataSrv 应用候选 | 如果 simple templates 存在，优先展示“CRM/进销存/财务/ERP”应用模板；否则继续用 domain | 扩展 |
| agent 意图解析 | `POST /api/v1/data/intent/resolve` | 把自然语言映射到 BusinessAction | 创建应用、加字段、加组件这类意图应走 simple change plan | 保留 |
| 业务表单 | `/api/v1/data/business-actions/{id}` | 根据 action input fields 渲染 AgentView 表单 | simple 向导也返回可渲染步骤；表单组件可复用 | 复用 |
| 写入确认 | `/execute` + `dry_run` + commit | 先校验，再提交 | simple change plan 也用“预览 -> 确认 -> apply” | 复用 |
| 关系查看 | `/api/v1/data/relationships` | 返回已有 `record_ref` 关系 | simple 增加 app 级关系图，底层仍可由 relationships 生成 | 扩展 |

### GUI 应新增的 simple tool actions

在 `mis_data_tool_action.go` 里追加动作，不删除旧动作：

```text
list_components
list_app_templates
preview_app
create_app
check_app_access
test_app_query
plan_change
apply_change_plan
get_relationship_graph
```

这些动作映射到：

```http
GET  /api/v1/simple/components
GET  /api/v1/simple/apps/templates
POST /api/v1/simple/apps/preview
POST /api/v1/simple/apps
POST /api/v1/simple/apps/{appId}/access/check
POST /api/v1/simple/apps/{appId}/test-query
POST /api/v1/simple/changes/plan
POST /api/v1/simple/changes/{planId}/apply
GET  /api/v1/simple/apps/{appId}/relationships
```

agent/App 使用策略：

- 用户说“我要建进销存/CRM/ERP/财务”，走 `list_app_templates -> preview_app -> create_app -> check_app_access -> test_app_query`。
- 用户说“给客户加字段/加售后工单/关联订单”，走 `plan_change -> apply_change_plan`。
- 用户说“查询/新增/更新业务数据”，继续走旧 `resolve_intent -> business_action`。

这样用户的第一天路径简单，老系统能力也不浪费。

### SettingsPanel 建议

当前配置页只做连接参数和测试。建议加一个明显按钮：

```text
打开 DataSrv 向导
```

点开后进入 4 步：

```text
1. 选择应用模板：CRM / 进销存 / 财务 / ERP 轻量
2. 预览将创建和复用的数据
3. 创建应用
4. 用全局 MIS token 校验权限并测试查询
```

文案从“sales、HR、finance data”调整为：

```text
用于 CRM、进销存、财务、ERP 等企业 MIS 结构化数据。
```

### MaClaw App 对 MIS 的操作模型

还要单独处理 MaClaw App。

MaClaw GUI 里已经有 `enterprise_app` 这种 app 类型。重新设计后，它不要做成另一套 DataSrv 客户端。

更合理的定位是：

```text
enterprise_app = 适合企业场景的 Skill 封装
  -> 前端用 AG UI / AgentView 渲染
  -> 业务逻辑在 skill
  -> 数据访问走统一 MIS 工具
  -> DataSrv 只负责严谨数据底座
```

这说明 MaClaw App 本身是业务入口，但底层仍是 skill 调用数据工具：

- 客户建档 App
- 采购入库 App
- 库存盘点 App
- 报销申请 App
- 客户跟进 App
- 发票审核 App

它们不应该各自写 DataSrv HTTP 调用，也不应该各自保存 token。

统一链路应该是：

```text
用户打开 MaClaw App
  -> 启动对应 skill
  -> skill 通过 AG UI 提供表单/列表/确认界面
  -> skill 调用 MIS 工具
  -> MIS 工具使用 Settings 里的 DataSrv URL/token
  -> DataSrv 执行查询、写入、变更计划、审计
  -> AG UI 展示结果
```

### MIS 工具的真正角色

MIS 相关工具应该主要服务两个调用方：

```text
Agent
  自然语言临时任务、分析、查询、修改建议

Skill / enterprise_app
  固定业务应用、标准流程、高频操作、可发布封装
```

不要让 agent 和 skill 各自发明一套 DataSrv 接入方式。它们都调用同一组 MIS tool。

推荐工具能力分层：

```text
mis.app.list_templates
mis.app.preview
mis.app.create
mis.app.get
mis.app.check_access

mis.data.query
mis.data.get_record
mis.data.upsert_record
mis.data.execute_action

mis.change.plan
mis.change.apply

mis.graph.get
mis.import.preview
mis.import.apply
```

这些工具内部再映射到 DataSrv API。agent 和 skill 只面对业务语义。

### Skill 封装方式

企业应用应封装成适合业务场景的 skill。

例如“库存盘点” skill：

```text
输入：
  仓库
  商品范围
  盘点数量
  备注

调用：
  mis.app.check_access(mis.inventory, requiredScopes)
  mis.data.query(stock_position)
  mis.data.execute_action(stock_adjust, dry_run=true)
  用户确认
  mis.data.execute_action(stock_adjust, dry_run=false)

输出：
  盘点结果
  差异清单
  审计记录
```

前端不用写库存盘点业务逻辑，只负责 AG UI 渲染：

```text
resource_picker 选择仓库
table/form 填盘点数量
approval 确认差异
result_browser 展示结果
```

### enterprise_app manifest 新模型

`enterprise_app` 应该绑定 skill，同时声明数据需求。

建议模型：

```json
{
  "schema": "maclaw.app.v1",
  "installUnit": "skill",
  "privateMarker": "x_maclaw_apps",
  "app": {
    "id": "inventory-count",
    "name": "库存盘点",
    "kind": "enterprise_app",
    "launchMode": "agent_dynamic_ui",
    "binding": {
      "skill": {
        "id": "mis-inventory-count",
        "inputMode": "form"
      },
      "mis": {
        "appId": "mis.inventory",
        "blueprintId": "mis.inventory",
        "requiredRoles": ["item", "warehouse", "stock", "movement"],
        "requiredScopes": [
          "view:inventory.stock_position",
          "action:inventory.stock_adjust"
        ],
        "setup": {
          "mode": "ensure_app",
          "templateId": "mis.inventory"
        }
      }
    }
  }
}
```

关键变化：

- `binding.skill` 决定应用逻辑。
- `binding.mis` 声明数据依赖。
- token 不在 App 里出现。
- DataSrv URL/token 仍来自全局 MIS 设置。
- GUI 启动 App 时先做 `mis.app.check_access`。

### DataSrv App 与 MaClaw App 的关系

DataSrv App Installation 是数据底座：

```text
表、字段、关系、视图、动作、role_bindings、审计策略
```

MaClaw enterprise_app 是 skill 化业务入口：

```text
图标、名称、AG UI、流程逻辑、调用哪些 MIS 工具
```

一个 DataSrv App 可以被多个 enterprise_app 复用。

例如 `mis.inventory`：

| enterprise_app | skill | 调用 MIS 能力 |
| --- | --- | --- |
| 库存查询 | `mis-inventory-query` | `mis.data.query` |
| 库存盘点 | `mis-inventory-count` | `mis.data.query` + `mis.data.execute_action` |
| 采购入库 | `mis-purchase-inbound` | `mis.data.execute_action` |
| 销售出库 | `mis-sales-outbound` | `mis.data.execute_action` |
| 商品管理 | `mis-item-manage` | `mis.data.upsert_record` |

这样数据模型只安装一次，业务 App 可以很多个。

### MaClaw App 是超级 Skill

更准确的产品定义：

```text
普通 Skill
  面向熟悉 Skill 的用户
  常从技能列表或 agent 调用
  可以没有固定 GUI 入口

MaClaw App
  本质仍是 Skill
  但有标准 AG UI 输入输出
  可以钉到应用入口
  有图标、名称、分类、固定业务场景
  普通用户能像打开应用一样使用
```

所以 `enterprise_app` 不应脱离 Skill 体系。它是“超级 Skill”：

```text
Skill runtime 负责执行
AG UI 负责交互
MIS tool 负责数据访问
App 面板负责发现和启动
DataSrv 负责数据严谨性
```

这个设计让普通用户不用理解“我要运行哪个 skill、传什么参数、怎么查数据”。他们只看到：

```text
库存盘点
客户建档
采购入库
销售出库
发票审核
```

点进去就是应用化的 AG UI。

### enterprise_app binding 新模型

`enterprise_app` binding 应从 `binding.datasrv` 改为：

```text
binding.skill + binding.mis
```

其中：

- `binding.skill`：执行哪个 skill。
- `binding.mis`：这个 skill 需要哪套 MIS 数据、哪些业务角色、哪些权限。

示例：

```json
{
  "binding": {
    "skill": {
      "id": "mis-inventory-count",
      "inputMode": "form",
      "ui": "ag_ui"
    },
    "mis": {
      "appId": "mis.inventory",
      "blueprintId": "mis.inventory",
      "requiredRoles": ["item", "warehouse", "stock", "movement"],
      "requiredScopes": ["view:inventory.stock_position", "action:inventory.stock_adjust"],
      "setup": {
        "mode": "ensure_app",
        "templateId": "mis.inventory"
      }
    }
  }
}
```

注意：

- skill 不直接保存 DataSrv URL。
- skill 不直接保存 token。
- skill 调用统一 MIS tool。
- MIS tool 从全局 Settings 读取 DataSrv URL/token。
- `requiredRoles` 用来声明业务依赖，不直接绑定 dataset。
- `requiredScopes` 只做权限校验，不做授权配置。

### App 与 Skill 的关系

一个 skill 可以有多个 App 包装。

例如同一个 `mis-sales-order` skill 可以包装成：

| App | 默认模式 | 默认参数 |
| --- | --- | --- |
| 新增销售订单 | 表单录入 | `mode=create` |
| 销售订单审核 | 审批列表 | `mode=review` |
| 销售出库 | 出库确认 | `mode=fulfill` |

一个 App 也可以组合多个 skill，但第一版建议避免。先保持：

```text
1 个 enterprise_app -> 1 个主 skill -> 多个 MIS tool 调用
```

这样最容易理解、测试、发布、回滚。

### App 启动时的检查流程

用户点击一个 MIS enterprise_app 时，GUI 不应直接假设后端已准备好。

推荐流程：

```text
1. 读取 app.manifest.binding.skill 和 app.manifest.binding.mis
2. 检查 DataSrv 连接
3. 如果 binding.mis.appId 已存在，调用 mis.app.get
4. 如果没有安装，但 binding.mis.setup.templateId 存在，打开安装向导
5. 调用 mis.app.check_access 校验 requiredScopes
6. 如果全局 MIS token 权限不足，引导管理员到 MIS 设置或 DataSrv 授权页调整
7. 启动 binding.skill.id 对应 skill
8. skill 通过 AG UI 输入输出，并调用 MIS tool 操作数据
```

用户体验要像：

```text
库存盘点需要“进销存管理”数据应用。

尚未安装。

[预览并安装] [取消]
```

安装成功后：

```text
进销存管理已可用。
当前 MIS token 权限已满足库存盘点。
现在可以开始盘点。
```

### MaClaw App 的两种 MIS 运行模式

#### 模式 A：Agent Dynamic UI

适合自然语言辅助的业务应用。

```text
用户输入：帮我把这批采购单入库
App 绑定：mis-purchase-inbound skill
skill 行为：调用 MIS tool 查询采购单、生成入库动作、dry_run、确认提交
GUI 行为：AG UI 渲染输入、确认和结果
```

这是第一版最推荐的模式，因为和普通 skill runtime 统一。

#### 模式 B：Fixed Enterprise UI

建议后续增加。

适合高频 MIS 操作，不应每次都靠自然语言：

- 客户列表
- 订单录入
- 库存盘点
- 收付款登记
- 发票审核

新增 launchMode 可考虑：

```text
enterprise_data_ui
```

或继续用 `agent_dynamic_ui`，但由 `binding.skill` 声明固定 AG UI schema：

```json
{
  "ui": {
    "mode": "table_form",
    "listView": "inventory.stock_position",
    "primaryAction": "inventory.stock_adjust",
    "detailView": "inventory.item_detail"
  }
}
```

第一版可以不做固定 UI，但 skill manifest 要预留 AG UI schema，不要把 enterprise_app 永久锁死在自由文本入口。

### App 发布与模板关系

一套设计好的系统可以成为模板，也可以发布成一组超级 Skill。

建议拆成三个可导出物：

```text
Data Template Pack
  组件、蓝图、字段、关系、视图、动作、权限套餐

Skill Pack
  企业业务逻辑、AG UI schema、MIS tool 调用流程、测试样例

App Entry Pack
  应用入口、图标、分类、默认参数、绑定哪个 skill、声明哪些 MIS 依赖
```

发布“进销存管理”时，最好同时包含：

```text
1 个 DataSrv 蓝图：mis.inventory
多个企业 Skill：
  - mis-item-manage
  - mis-purchase-inbound
  - mis-sales-outbound
  - mis-inventory-count
  - mis-inventory-query
多个 MaClaw App 入口：
  - 商品管理
  - 采购入库
  - 销售出库
  - 库存盘点
  - 库存查询
```

安装时顺序：

```text
1. 安装 Data Template Pack
2. 预览/创建 DataSrv App Installation
3. 安装 Skill Pack
4. 注册 App Entry 到应用面板
5. 每个 App Entry 绑定一个主 skill
6. 每个 App Entry 声明 binding.mis.requiredRoles/requiredScopes
7. 启动时用全局 MIS token 校验权限
```

这样 App 是普通用户入口，Skill 是业务执行单元，MIS tool 是统一数据访问层，DataSrv 是严谨数据底座。

### App Studio 新设计

既然还没有正式使用，不按旧 `binding.datasrv.domain` 设计。

App Studio 应直接围绕超级 Skill 建模：

```text
创建 App
  -> 选择/创建 skill
  -> 选择 AG UI 输入输出
  -> 选择需要的 MIS 应用模板或已安装 MIS App
  -> 选择 requiredRoles
  -> 自动建议 requiredScopes
  -> 保存到应用入口
```

App Studio 必填项：

- `app.id`
- `app.name`
- `app.kind = enterprise_app`
- `binding.skill.id`
- `binding.mis.appId` 或 `binding.mis.blueprintId`
- `binding.mis.requiredRoles`

App Studio 自动生成：

- `requiredScopes`
- 默认 AG UI schema
- 测试用例
- 上架前权限检查

这样企业 App 创建过程像“给 skill 加应用壳”，不是“手写 DataSrv API 绑定”。

### Skill 怎么生成

企业 MIS Skill 不应从空白代码开始写。

推荐生成路径：

```text
选择业务场景/模板
  -> 选择已安装 MIS App 或 DataSrv 蓝图
  -> 选择业务动作
  -> 后端生成 Skill 草稿
  -> AI 补全流程、AG UI、校验和提示语
  -> dry-run 测试
  -> 保存为 Skill
  -> 包装成 MaClaw App 入口
```

也就是：

```text
DataSrv 蓝图负责“有什么数据”
MIS tool 负责“怎么安全访问数据”
Skill 负责“业务流程怎么走”
AG UI 负责“用户怎么输入和确认”
MaClaw App 负责“普通用户从哪里打开”
```

#### 生成入口 A：从模板生成

用户选择：

```text
进销存管理 -> 库存盘点
```

系统知道库存盘点需要：

```text
requiredRoles: item, warehouse, stock, movement
requiredScopes:
  - view:inventory.stock_position
  - action:inventory.stock_adjust
```

生成 skill：

```text
skill id: mis-inventory-count
输入：仓库、商品范围、盘点数量、备注
流程：
  1. 检查 MIS 权限
  2. 查询当前库存
  3. 生成盘点差异
  4. dry_run 库存调整
  5. 用户确认
  6. 正式提交
输出：盘点结果、差异清单、审计编号
```

同时生成 AG UI：

```text
resource_picker: 选择仓库
table: 录入盘点数量
approval: 确认差异
result_browser: 展示提交结果
```

#### 生成入口 B：从自然语言生成

用户说：

```text
做一个客户跟进 App：可以选择客户，查看最近订单和回款，然后记录一次跟进。
```

AI 先生成 Skill Plan：

```json
{
  "skill_id": "mis-customer-followup",
  "title": "客户跟进",
  "required_roles": ["customer", "sales_order", "payment", "followup"],
  "required_scopes": [
    "view:crm.customer_detail",
    "view:sales.order_list",
    "view:finance.payment_list",
    "action:crm.followup_create"
  ],
  "steps": [
    "选择客户",
    "查询客户详情、订单、回款",
    "填写跟进内容和下一步计划",
    "dry_run 创建跟进记录",
    "确认提交"
  ]
}
```

后端检查：

- 当前 MIS App 是否有这些角色。
- 缺角色时能否添加组件。
- 当前全局 token 是否有权限。
- 是否需要先生成 change plan。

用户确认后，再生成 skill 文件。

#### 生成入口 C：从现有操作生成

如果 DataSrv 已有 action/view：

```text
view: inventory.stock_position
action: inventory.stock_adjust
```

可以一键生成：

```text
查询型 skill
录入型 skill
审批型 skill
导入型 skill
```

例如从 `inventory.stock_adjust` 生成“库存调整 App”。

#### Skill 草稿结构

生成出来的 skill 至少包含：

```text
SKILL.md
skill.json 或 manifest.json
ui.schema.json
workflow.json
tests/
  sample_input.json
  expected_tool_calls.json
  expected_output.json
```

概念结构：

```json
{
  "id": "mis-inventory-count",
  "kind": "mis_skill",
  "title": "库存盘点",
  "mis": {
    "app_id": "mis.inventory",
    "required_roles": ["item", "warehouse", "stock", "movement"],
    "required_scopes": ["view:inventory.stock_position", "action:inventory.stock_adjust"]
  },
  "ui": {
    "mode": "ag_ui",
    "entry": "ui.schema.json"
  },
  "tools": [
    "mis.app.check_access",
    "mis.data.query",
    "mis.data.execute_action"
  ]
}
```

#### Skill 生成器职责

后端或 GUI 需要一个 Skill Generator。

它负责：

- 根据蓝图/组件/action/view 推导 requiredRoles。
- 根据读写需求推导 requiredScopes。
- 生成 AG UI schema。
- 生成 MIS tool 调用流程。
- 生成 dry-run 和确认步骤。
- 生成测试样例。
- 生成 App Entry。

AI 可以参与生成，但不能直接绕过校验。

#### AI 生成边界

AI 适合做：

- 根据业务描述拆流程。
- 起草字段、界面、提示语。
- 选择合适的 MIS tool。
- 生成 Skill Plan。

后端必须做：

- 角色解析。
- 权限校验。
- schema 校验。
- action/view 是否存在。
- dry-run 测试。
- 风险分级。

最终保存前，用户看到：

```text
将生成 Skill：客户跟进
需要数据：客户、订单、回款、跟进记录
需要权限：查询客户/订单/回款，新增跟进记录
将创建 App 入口：客户跟进
```

确认后才写入。

## 用户不知道表结构时怎么关联数据对象

这是核心问题。

普通用户不应该看到：

```text
dataset: party.customers
field: customer_ref
ref_dataset: sales.orders
```

用户只应该看到：

```text
客户
联系人
订单
商品
仓库
库存
发票
付款
跟进记录
```

所以系统必须有一层“业务对象目录”。

### 业务对象目录 Business Object Catalog

每个 DataSrv 安装应用都维护一个目录：

```json
{
  "app_id": "mis.inventory",
  "objects": [
    {
      "role": "customer",
      "title": "客户",
      "aliases": ["客户", "客户资料", "客户信息", "往来单位"],
      "dataset": "party.customers",
      "primary_fields": ["name", "code"],
      "display_field": "name",
      "search_fields": ["name", "code", "phone"],
      "required": true
    },
    {
      "role": "sales_order",
      "title": "销售订单",
      "aliases": ["订单", "销售单", "客户订单"],
      "dataset": "sales.orders",
      "primary_fields": ["order_no"],
      "display_field": "order_no",
      "search_fields": ["order_no", "customer_name"],
      "relations": [
        {"field": "customer_ref", "target_role": "customer", "title": "客户"}
      ]
    }
  ]
}
```

用户、agent、skill 都不直接依赖 dataset 名，而依赖 `role`。

```text
客户 -> role: customer -> dataset: party.customers
订单 -> role: sales_order -> dataset: sales.orders
```

### 业务怎么知道对应哪个后端数据

不是靠用户知道表，也不是靠 AI 硬猜。

对应关系来自 5 个来源：

```text
1. 模板内置语义
2. 安装时生成的 role_bindings
3. 导入/建表时的字段映射确认
4. 业务对象目录里的别名、字段、关系、样例
5. 管理员/用户对低置信度匹配的确认
```

#### 1. 模板内置语义

比如“报销”模板本身声明：

```json
{
  "blueprint_id": "mis.expense",
  "objects": [
    {"role": "employee", "title": "员工", "shared": true},
    {"role": "department", "title": "部门", "shared": true},
    {"role": "expense_report", "title": "报销单"},
    {"role": "expense_item", "title": "报销明细"},
    {"role": "approval", "title": "审批记录"},
    {"role": "payment", "title": "付款记录"}
  ],
  "relations": [
    {"from": "expense_report", "to": "employee", "title": "报销人"},
    {"from": "expense_report", "to": "department", "title": "所属部门"},
    {"from": "expense_item", "to": "expense_report", "title": "所属报销单"},
    {"from": "approval", "to": "expense_report", "title": "审批对象"},
    {"from": "payment", "to": "expense_report", "title": "付款对象"}
  ]
}
```

所以用户说“报销”，系统先不是找表，而是找到 `mis.expense` 这个业务蓝图。

#### 2. 安装时生成 role_bindings

安装“报销”应用后，系统生成真实绑定：

```json
{
  "app_id": "mis.expense",
  "role_bindings": {
    "employee": "company.users",
    "department": "company.departments",
    "expense_report": "expense.reports",
    "expense_item": "expense.items",
    "approval": "workflow.approvals",
    "payment": "finance.payments"
  }
}
```

之后任何 skill 只要说：

```text
expense_report
```

后端就知道是：

```text
expense.reports
```

#### 3. 复用已有数据时做语义匹配

如果系统里已有一些表，安装报销模板时后端做匹配：

```text
模板需要 employee
候选：
  company.users      命中：用户、员工、姓名、部门
  hr.employees       命中：员工、工号、部门、岗位
  crm.customers      不匹配
```

返回预览：

```text
员工：建议复用 company.users
部门：建议复用 company.departments
付款记录：建议复用 finance.payments
报销单：将新建
报销明细：将新建
审批记录：将新建
```

高置信度自动使用，低置信度让用户确认。

用户看到的是：

```text
“员工”使用已有“用户信息”吗？
```

不是：

```text
请选择 dataset company.users。
```

#### 4. 导入/建表时沉淀映射

如果用户一开始从 Excel 导入：

```text
报销人, 部门, 金额, 发票号, 事由, 审批状态
```

系统做字段识别：

```text
报销人 -> employee_ref
部门 -> department_ref
金额 -> amount
发票号 -> invoice_no
事由 -> description
审批状态 -> status
```

用户确认一次后，保存到对象目录：

```json
{
  "role": "expense_report",
  "field_aliases": {
    "报销人": "employee_ref",
    "申请人": "employee_ref",
    "金额": "amount",
    "报销金额": "amount",
    "事由": "description"
  }
}
```

以后 Skill 生成和数据写入就不再问。

#### 5. Skill 生成时只绑定 role

用户说：

```text
员工可以提交报销，主管审批，财务付款。
```

Skill Generator 生成的是：

```json
{
  "required_roles": ["employee", "expense_report", "expense_item", "approval", "payment"],
  "steps": [
    {"tool": "mis.data.upsert_record", "object_role": "expense_report"},
    {"tool": "mis.data.upsert_record", "object_role": "expense_item"},
    {"tool": "mis.data.execute_action", "action_role": "submit_expense"},
    {"tool": "mis.data.execute_action", "action_role": "approve_expense"},
    {"tool": "mis.data.execute_action", "action_role": "mark_paid"}
  ]
}
```

它不写：

```text
expense.reports
expense.items
finance.payments
```

真实 dataset 由运行时 resolver 根据 `role_bindings` 找。

### Resolver 运行流程

当 skill 调：

```json
{
  "tool": "mis.data.upsert_record",
  "args": {
    "app_id": "mis.expense",
    "object_role": "expense_report",
    "data": {
      "报销人": "张三",
      "金额": 1280,
      "事由": "客户拜访差旅"
    }
  }
}
```

后端执行：

```text
1. app_id -> 找安装记录
2. object_role=expense_report -> expense.reports
3. 中文字段名 -> 字段别名映射
4. 报销人 张三 -> employee role -> company.users -> 找记录 ID
5. 校验必填、金额、权限
6. dry_run 或正式写入
7. 记录审计
```

对应关系靠目录和绑定，不靠用户懂表。

### 低置信度必须问人

如果系统无法确定：

```text
“人员”可能是：
1. 员工
2. 客户联系人
3. 供应商联系人
```

AG UI 弹业务选择：

```text
这里的“人员”指什么？
[员工] [客户联系人] [供应商联系人]
```

用户选择后保存映射：

```json
{
  "alias": "人员",
  "role": "employee",
  "scope": "mis.expense"
}
```

下次同场景不再问。

### 角色绑定 Role Binding

安装模板时，后端生成：

```json
{
  "role_bindings": {
    "customer": "party.customers",
    "item": "inventory.items",
    "warehouse": "inventory.warehouses",
    "stock": "inventory.stock",
    "sales_order": "sales.orders"
  }
}
```

以后所有引用都走：

```text
target_role -> role_bindings -> dataset
```

这样用户说“客户”，skill 写 `customer`，后端自己找到真实表。

### 对象解析 Object Resolver

新增统一解析接口：

```http
POST /api/v1/mis/objects/resolve
```

输入：

```json
{
  "app_id": "mis.inventory",
  "text": "客户",
  "context": "创建客户跟进 App"
}
```

输出：

```json
{
  "matches": [
    {
      "role": "customer",
      "title": "客户",
      "dataset": "party.customers",
      "confidence": 0.98
    }
  ]
}
```

如果有歧义：

```text
“订单”可能是：
1. 销售订单
2. 采购订单
```

GUI 用 AG UI 弹选择，不让用户选表，只让用户选业务名：

```text
你说的“订单”是销售订单还是采购订单？
```

### 字段也要业务化

用户也不知道字段名。

业务对象目录要包含字段语义：

```json
{
  "role": "customer",
  "fields": [
    {"key": "name", "title": "客户名称", "aliases": ["名称", "客户名", "公司名"], "type": "string"},
    {"key": "phone", "title": "联系电话", "aliases": ["电话", "手机号", "联系方式"], "type": "string", "sensitive": true},
    {"key": "owner_ref", "title": "负责人", "target_role": "user", "type": "record_ref"}
  ]
}
```

用户说：

```text
客户要加一个来源字段
```

解析成：

```json
{
  "target_role": "customer",
  "change": {
    "kind": "add_field",
    "field": {
      "key": "source",
      "title": "客户来源"
    }
  }
}
```

### Skill 生成时如何关联对象

Skill Generator 不问用户表结构。

它问业务问题：

```text
这个 App 主要处理什么？
- 客户
- 订单
- 库存
- 发票
```

用户选择业务对象后，生成器做：

```text
业务对象 -> role -> dataset
业务动作 -> requiredScopes
对象关系 -> 默认查询/表单
```

例如用户要生成“客户跟进”：

```text
用户选择：客户、订单、回款、跟进记录
```

后端解析：

```json
{
  "required_roles": ["customer", "sales_order", "payment", "followup"],
  "role_bindings": {
    "customer": "party.customers",
    "sales_order": "sales.orders",
    "payment": "finance.payments",
    "followup": "crm.followups"
  }
}
```

如果 `followup` 不存在，生成 change plan：

```text
当前系统没有“跟进记录”对象。
是否创建？

将创建：
- 跟进记录
- 关联客户
- 可选关联销售订单
```

### App/Skill 运行时如何找数据

Skill 调用 MIS tool 时传业务 role，不传表名：

```json
{
  "tool": "mis.data.query",
  "args": {
    "app_id": "mis.inventory",
    "object_role": "customer",
    "keyword": "华东"
  }
}
```

MIS tool 内部解析：

```text
object_role customer
  -> role_bindings.customer
  -> party.customers
  -> query view crm.customer_search
```

写入也一样：

```json
{
  "tool": "mis.data.upsert_record",
  "args": {
    "app_id": "mis.inventory",
    "object_role": "followup",
    "data": {
      "customer": "上海某公司",
      "content": "已电话沟通，月底报价",
      "next_action": "发送报价单"
    }
  }
}
```

后端把“上海某公司”解析成客户记录 ID，再写入 `customer_ref`。

### 关联关系由系统推荐

用户不画外键，也不填字段名。

系统根据对象目录推荐：

```text
跟进记录 应该关联 客户
跟进记录 可选关联 销售订单
发票 应该关联 客户/供应商
付款 应该关联 发票/订单
库存流水 应该关联 商品/仓库
```

AG UI 只展示业务句子：

```text
跟进记录将关联客户。
是否也关联销售订单？
```

后端生成：

```json
{
  "kind": "add_relationship",
  "source_role": "followup",
  "field": "customer_ref",
  "target_role": "customer"
}
```

再由 role binding 编译成真实字段和 dataset。

### 对用户的最终体验

用户创建一个 App 时只回答：

```text
这个应用处理哪些业务对象？
需要查询哪些？
需要新增/修改哪些？
对象之间怎么说得通？
```

系统负责：

```text
业务名识别
role 映射
字段映射
关系生成
权限推导
Skill 生成
AG UI 生成
DataSrv 变更计划
```

这才是“灵活但简单”。

### App 级权限

企业 MIS App 不单独保存 token。

token 只在 MaClaw 设置里的 MIS Data 配置中保存一次：

```text
Settings -> MIS Data -> DataSrv URL + Token + Tenant + User + Role
```

所有 MaClaw enterprise_app 共用这组连接配置。

App manifest 只声明自己需要什么权限，不保存密钥。

App manifest 声明需要：

```json
{
  "requiredScopes": [
    "view:inventory.stock_position",
    "action:inventory.stock_adjust"
  ]
}
```

全局 MIS token 实际拥有：

```json
{
  "allowed_views": ["inventory.stock_position"],
  "allowed_actions": ["inventory.stock_adjust"]
}
```

启动时如果不匹配：

```text
当前 token 无法执行库存调整。
需要权限：inventory.stock_adjust
可以：打开 MIS 设置 / 去 DataSrv 授权页调整 / 只读打开
```

这样避免 App 看起来能操作，实际提交时才失败。

关键边界：

- Settings 保存连接身份。
- App manifest 声明业务权限需求。
- DataSrv 决定 token 是否允许。
- GUI 只校验和提示，不在 App 内重复管理 token。

### Token、用户角色和审批

DataSrv token 需要绑定身份和权限，但不要把业务审批逻辑硬塞进 token。

建议分三层：

```text
Token 身份层
  这个请求来自哪个 tenant、哪个用户/客户端、是否允许调用 MIS tool

权限范围层
  这个 token 能查哪些对象、能执行哪些 action、能否代用户发起操作

业务审批层
  当前 actor 在组织里是什么角色，是否符合这张单据的审批规则
```

token 里应该有：

```json
{
  "tenant_id": "default",
  "principal_type": "user",
  "principal_id": "u_zhangsan",
  "scopes": [
    "mis.app.use",
    "view:expense.my_reports",
    "action:expense.submit"
  ],
  "actor_policy": {
    "mode": "self"
  }
}
```

如果是企业集成 token：

```json
{
  "principal_type": "integration",
  "principal_id": "maclaw-desktop",
  "scopes": ["mis.app.use", "mis.action.request"],
  "actor_policy": {
    "mode": "on_behalf_of",
    "allowed_user_source": "maclaw_session"
  }
}
```

审批不要只看 token role。审批要看业务 actor：

```text
actor_id -> company.users
actor_id -> company.departments
actor_id -> approval_roles
document -> amount / department / applicant / status
approval_policy -> 是否允许通过
```

例如报销审批：

```text
张三提交报销
  token 允许 action:expense.submit
  actor=张三
  写入报销单 applicant_ref=张三

李经理审批
  token 允许 action:expense.approve
  actor=李经理
  DataSrv 检查：
    李经理是不是张三的主管？
    金额是否在李经理审批额度内？
    单据状态是否 pending_manager_approval？
  通过后才更新状态
```

审批 action 应声明业务规则：

```json
{
  "action_role": "approve_expense",
  "target_role": "expense_report",
  "required_scope": "action:expense.approve",
  "approval_rule": {
    "actor_must_match": "applicant.manager",
    "max_amount_field": "approval_limit",
    "allowed_from_status": ["pending_manager_approval"],
    "next_status": "pending_finance_payment"
  }
}
```

所以：

- token 必须绑定 tenant/principal/scopes。
- MaClaw App 不保存 token。
- Skill 调 MIS tool 时传当前 actor/session。
- DataSrv 用 token 校验“能不能调用”。
- DataSrv 用审批策略校验“这个人能不能批这张单”。
- 所有提交、审批、付款都写审计日志。

这样才能既支持全局 MIS 设置，又能做严谨审批。

### AppsPage 建议

当前 AppsPage 按 `capabilities.domains` 生成候选应用。这个逻辑可保留作为 fallback。

新的优先级：

```text
1. 如果 `/api/v1/simple/apps/templates` 可用，展示应用模板卡片。
2. 如果 simple 不可用，继续按旧 `/api/v1/data/capabilities.domains` 生成卡片。
3. 如果两者都不可用，只提示去 Settings 配置 DataSrv。
```

应用模板卡片不要只显示 domain，而要显示：

- 业务名称：进销存管理
- 会创建：客户、供应商、商品、仓库、采购订单、销售订单、库存
- 会复用：已有客户/商品/用户等主数据
- 下一步：预览并创建

### AgentView 复用方式

不需要第一版就做独立复杂页面。simple API 返回结构化步骤，GUI 映射到现有 AgentView 类型：

| simple 场景 | AgentView 类型 | 说明 |
| --- | --- | --- |
| 选择应用模板 | `resource_picker` | 选 CRM、进销存、财务、ERP |
| 预览创建计划 | `result_browser` 或 `approval` | 展示将创建/复用/关联的表 |
| 修改字段 | `form` | 加字段、枚举、必填、敏感 |
| CSV 导入映射 | `field_mapper` | 复用已有字段映射组件 |
| 中高风险变更 | `approval` | 必须用户确认 |
| 创建结果 | `result_browser` | 展示 app、表、权限校验、测试结果 |

关键是后端返回“业务语言”，不要让 GUI 拼复杂逻辑。

### 关系可视化设计器

需要，但不要作为第一版上手依赖。

第一版先做“只读关系图 + 变更计划”。用户能看清楚：

```text
客户 -> 销售订单 -> 发票 -> 收款
商品 -> 库存 -> 出入库流水
供应商 -> 采购订单 -> 付款
```

第二版再允许拖线新增关联。拖线不是直接改 schema，而是生成变更计划：

```text
用户拖线：售后工单.customer_ref -> 客户
后端生成：add_relationship change plan
用户确认：apply
```

推荐图数据契约：

```json
{
  "app_id": "mis.inventory",
  "nodes": [
    {"id": "party.customers", "role": "customer", "title": "客户", "kind": "shared"},
    {"id": "sales.orders", "role": "sales_order", "title": "销售订单", "kind": "owned"}
  ],
  "edges": [
    {
      "id": "sales.orders.customer_ref",
      "from": "sales.orders",
      "field": "customer_ref",
      "to": "party.customers",
      "label": "客户"
    }
  ],
  "warnings": [
    {"level": "info", "message": "客户为共享主数据，已被 CRM 和进销存复用。"}
  ]
}
```

图上要区分：

- 共享主数据：用户、客户、供应商、商品、仓库、会计科目。
- 应用自有表：订单、发票、付款、库存流水。
- 派生/定制表：售后工单、项目、合同扩展。

### 和旧系统信息怎么关联

每次通过 simple 创建或修改，都要保存安装记录：

```json
{
  "app_id": "mis.inventory",
  "blueprint_id": "mis.inventory",
  "blueprint_version": "1.0.0",
  "role_bindings": {
    "customer": "party.customers",
    "supplier": "party.suppliers",
    "item": "inventory.items",
    "warehouse": "inventory.warehouses",
    "sales_order": "sales.orders"
  },
  "customizations": [
    {"kind": "add_field", "target_role": "customer", "field": "source"}
  ]
}
```

以后模板升级、另一个应用复用客户、AI 修改字段，都通过 `role_bindings` 找到真实表，不靠硬编码 dataset 名。

### 能力发现兼容

建议扩展 `/api/v1/data/capabilities`，新增可选字段：

```json
{
  "simple": {
    "enabled": true,
    "components": true,
    "app_templates": true,
    "change_plans": true,
    "relationship_graph": true
  }
}
```

旧 GUI 不认识这个字段也没关系。新 GUI 看到 `simple.enabled=true` 后，优先走 simple 向导。

也可以新增：

```http
GET /api/v1/simple/capabilities
```

更清爽。两者可同时支持：

- 老入口负责向后兼容。
- 新入口负责简单向导能力。

### 最小落地顺序

1. DataSrv 增加 simple app template/preview/create/token/test-query API。
2. GUI MIS tool 增加 simple actions。
3. SettingsPanel 增加“打开 DataSrv 向导”。
4. AppsPage 优先展示 simple app templates。
5. AgentView 复用现有资源选择、审批、结果浏览。
6. 后续再做关系图只读视图。
7. 最后再做拖线生成 change plan。

这个顺序风险最低：底层数据严谨性继续由旧 `/api/v1/data/*` 承担，用户上手复杂度由新 `/api/v1/simple/*` 降下来。

### Phase 1：不改底层，只改入口

目标：马上降低上手难度。

- 新增“业务模式”导航。
- 首页改成工作台。
- 业务表页优先模板创建。
- 数据页隐藏高级 JSON 操作。
- 接入页提供权限套餐。
- 高级功能收进管理页。

风险低，因为 API 和存储不动。

### Phase 2：建立简化 facade

目标：让前端和 agent 少理解内部 API。

新增一层推荐 API，不替换旧 API：

```http
POST /api/v1/simple/tables
GET  /api/v1/simple/tables
GET  /api/v1/simple/tables/{tableId}
POST /api/v1/simple/tables/{tableId}/records
POST /api/v1/simple/tables/{tableId}/query
POST /api/v1/simple/access-keys
```

内部仍映射到 dataset、fields、records、views、actions、api keys。

### Phase 3：模板和向导完善

目标：企业实施快。

- 内置 10 个常见模板。
- 支持 CSV 首行自动生成字段建议。
- 支持字段类型自动识别。
- 支持导入错误行下载。
- 支持一键生成默认视图和操作。

### Phase 4：策略引擎收口

目标：严谨但不复杂。

- 把 API key policy、字段脱敏、dataset 权限统一成 PolicyDecision。
- UI 用业务语言配置权限。
- agent 每次调用都能看到明确拒绝原因。

## 我建议的最终产品体验

用户首次打开时看到：

```text
欢迎使用 MaClawDataSrv

你可以：
1. 从模板创建业务表
2. 导入 CSV/Excel
3. 给 agent 开一个只读查询密钥
```

而不是看到几十个 API 能力模块。

DataSrv 内部仍保持严谨：

- schema
- validation
- transaction
- revision
- audit
- backup
- policy
- event

但用户默认只感知：

```text
业务表 -> 字段 -> 数据 -> 视图 -> 接入
```

这条路越短，企业用户越容易用起来。

## 端到端重规划总览

现在结论很明确：这不是单独改 MaClawDataSrv，也不是单独加几个 GUI 按钮。

需要一起重规划：

```text
MaClawDataSrv
  企业 MIS 数据底座、业务对象目录、role_bindings、权限、审批、审计

Agent Tools / MIS Tools
  agent 和 skill 共用的数据访问工具层

Skill Generator
  从业务意图、模板、对象目录生成可运行 skill

MaClaw App
  超级 Skill，带 AG UI，能放到应用入口给普通用户用

AG UI / App Studio
  生成、预览、编辑、发布企业应用的界面
```

整体链路：

```text
用户提出业务需求
  -> 业务对象解析
  -> DataSrv 模板/对象/关系规划
  -> 生成或复用 DataSrv App Installation
  -> 生成 Skill Plan
  -> 生成 AG UI schema
  -> 生成 MaClaw App Entry
  -> 权限/审批/dry-run 测试
  -> 出现在应用入口
  -> 用户日常使用
```

### 1. MaClawDataSrv 要重做成语义数据底座

DataSrv 不能只是 dataset/field/record API。

它要提供企业 MIS 语义层：

- Business Object Catalog：客户、订单、商品、报销单、付款等业务对象。
- Role Binding：`customer -> party.customers`、`expense_report -> expense.reports`。
- Relationship Catalog：对象之间的业务关系。
- Field Alias Catalog：中文字段、业务字段、真实字段映射。
- Blueprint / Component：CRM、进销存、财务、ERP 的组件化模板。
- Change Plan：新增对象、字段、关系、权限前先预览。
- Approval Policy：审批规则，不只靠 token role。
- Audit / Revision：所有写入、审批、付款可追踪。

DataSrv 对上层暴露的是：

```text
业务对象
业务动作
业务关系
业务权限
变更计划
```

不是让上层直接理解：

```text
dataset
field
ref_dataset
api_key_policy
operation_plan
```

### 2. MIS Tools 要成为统一访问层

MIS tool 是 agent 和 skill 的共同能力层。

不要出现：

```text
agent 一套 DataSrv 调用
skill 一套 DataSrv 调用
MaClaw App 又一套 DataSrv 调用
```

统一工具：

```text
mis.app.list_templates
mis.app.preview
mis.app.create
mis.app.get
mis.app.check_access

mis.object.resolve
mis.object.list
mis.object.describe

mis.data.query
mis.data.get_record
mis.data.upsert_record
mis.data.execute_action

mis.change.plan
mis.change.apply

mis.graph.get
mis.import.preview
mis.import.apply
```

调用方都只传：

```text
app_id
object_role
action_role
field_alias
actor
business data
```

工具内部读取全局 MIS 设置里的 DataSrv URL/token，再调用 DataSrv。

### 3. Skill Generator 是关键中台

MaClaw App 不应该手写大段业务逻辑。

Skill Generator 根据这些输入生成 skill：

```text
业务需求文本
DataSrv 蓝图
已安装 MIS App
业务对象目录
已有 view/action
用户确认的映射
```

输出：

```text
SKILL.md
skill manifest
AG UI schema
workflow steps
MIS tool 调用计划
sample input
expected tool calls
test cases
App Entry
```

它必须做：

- 业务对象解析。
- requiredRoles 推导。
- requiredScopes 推导。
- 缺对象时生成 change plan。
- AG UI 表单/表格/审批/结果页生成。
- dry-run 测试。
- 保存为可编辑 skill。

AI 可以协助生成，但后端必须做结构校验和权限校验。

### 4. MaClaw App 要定义为超级 Skill

MaClaw App 本质：

```text
可放到应用入口的超级 Skill
```

它比普通 skill 多：

- 固定业务入口。
- 图标、分类、名称、说明。
- 默认参数。
- AG UI 输入输出。
- requiredRoles / requiredScopes。
- 可测试、可发布、可复制。

它不应该：

- 保存 DataSrv token。
- 直接硬写 dataset。
- 绕过 MIS tool 调 DataSrv。
- 把审批逻辑写死在前端。

推荐 manifest：

```json
{
  "app": {
    "kind": "enterprise_app",
    "binding": {
      "skill": {"id": "mis-expense-claim", "ui": "ag_ui"},
      "mis": {
        "appId": "mis.expense",
        "requiredRoles": ["employee", "expense_report", "approval", "payment"],
        "requiredScopes": ["action:expense.submit", "view:expense.my_reports"]
      }
    }
  }
}
```

### 5. AG UI 要从“动态面板”升级为企业应用 UI 层

AG UI 不只是临时 AgentView。

企业 MIS App 需要这些标准布局：

- `resource_picker`：选择客户、员工、仓库、订单。
- `form`：录入报销、订单、付款。
- `table`：明细行、盘点清单、订单列表。
- `field_mapper`：导入 Excel 字段映射。
- `approval`：提交确认、审批确认、付款确认。
- `result_browser`：结果、审计号、错误行。
- `relationship_graph`：对象关系图。
- `dashboard`：库存、销售、报销状态概览。

第一版重点：

```text
form + table + approval + result_browser + resource_picker
```

关系图和 dashboard 后置。

### 6. App Studio 要重新设计

App Studio 目标不是编辑一个 manifest JSON。

它应该是企业应用生成器：

```text
创建应用
  -> 描述业务需求
  -> 选择业务模板或已有 MIS App
  -> 确认业务对象
  -> 确认缺失对象/字段/关系
  -> 生成 Skill Plan
  -> 预览 AG UI
  -> dry-run 测试
  -> 发布到应用入口
```

App Studio 里用户看到：

```text
报销单
员工
审批
付款
发票附件
```

不是：

```text
dataset
field key
ref_dataset
action id
scope string
```

高级模式可以显示真实技术细节。

### 7. 权限和审批要全链路贯通

全局 MIS 设置保存 DataSrv URL/token。

但每次执行还要有 actor：

```text
token = 调用凭证
actor = 当前业务操作人
scope = 能不能调用这个动作
approval policy = 这个人能不能批这张单
```

Skill 调 MIS tool 时传：

```json
{
  "actor": {
    "user_id": "u_lijingli",
    "session_id": "..."
  }
}
```

DataSrv 决定：

- token 是否有效。
- scope 是否允许。
- actor 是否符合业务规则。
- 单据状态是否可变更。
- 是否需要审批或二次确认。

### 8. 推荐实施路线

#### Phase 1：语义底座

- DataSrv 增加 Business Object Catalog。
- 增加 role_bindings。
- 增加 object resolve API。
- 增加 app installation 记录。
- 先做 CRM、进销存、报销 3 个蓝图。

#### Phase 2：MIS Tools

- 建统一 `mis.*` tool 层。
- agent 和 skill 都走这层。
- 所有调用使用 object_role/action_role。
- 支持 check_access、dry_run、audit。

#### Phase 3：Skill Generator MVP

- 从模板生成 skill。
- 从自然语言生成 Skill Plan。
- 生成 AG UI schema。
- 生成测试样例。
- 支持“报销申请”和“库存盘点”两条样板链路。

#### Phase 4：MaClaw App / App Studio

- enterprise_app 改为超级 Skill。
- App Studio 从 manifest 编辑改为应用生成向导。
- 支持 AG UI 预览和布局微调。
- 支持发布到应用入口。

#### Phase 5：审批和组织角色

- DataSrv 增加 approval policy。
- company.users / departments / manager 关系标准化。
- action 执行时校验 actor。
- 审批、驳回、付款写审计。

#### Phase 6：可视化关系和模板市场

- 对象关系图。
- 拖线生成 change plan。
- 已安装系统导出成模板。
- Skill Pack + App Entry Pack 发布。

### 9. 最小可验证样板

建议先做两个端到端样板：

```text
报销申请
  员工提交 -> 主管审批 -> 财务付款 -> 查询记录

库存盘点
  选择仓库 -> 查询库存 -> 录入盘点数 -> 差异确认 -> 写库存流水
```

这两个能覆盖：

- 业务对象解析。
- 共享主数据。
- 明细表。
- 关联关系。
- 权限。
- 审批。
- dry-run。
- AG UI。
- Skill 生成。
- App 入口。

跑通这两条，再扩 CRM、采购、销售、财务。

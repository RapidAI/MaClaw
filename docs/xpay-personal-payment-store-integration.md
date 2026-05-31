# XPay 个人收款集成到 Hub Store 设计文档

## 目标

在 `hub` 的 `/card_store` 在线商店中增加“个人收款”支付方式，用于没有商户号或不想接第三方聚合支付时销售服务兑换卡和点卡。

本阶段只产出方案文档。确认后再开发。

## 已读代码范围

- XPay 源码：`D:\workprj\aicoder\xpay-code`
  - `README（读我！）.md`
  - `src/main/java/cn/exrick/controller/PayController.java`
  - `src/main/java/cn/exrick/controller/AlipayController.java`
  - `src/main/java/cn/exrick/controller/WechatController.java`
  - `src/main/java/cn/exrick/controller/PageController.java`
  - `src/main/java/cn/exrick/bean/Pay.java`
  - `src/main/java/cn/exrick/service/impl/PayServiceImpl.java`
- Hub Store 现状：`D:\workprj\aicoder\hub`
  - `hub/internal/httpapi/card_store_handlers.go`
  - `hub/internal/httpapi/card_store_handlers_test.go`
  - `hub/web/card_store/index.html`
  - `hub/web/admin/card-store-tab.js`
  - `hub/internal/httpapi/router.go`

## XPay 个人收款原理

XPay 有两类支付路径。

### 1. 官方商户接口路径

`AlipayController` 和 `WechatController` 使用官方接口生成二维码：

- 支付宝当面付：`/alipay/precreate` 调用 `AlipayTradePrecreateRequest`，返回 `qrCode`。
- 微信 Native：`/wechat/precreate` 调用 `https://api.mch.weixin.qq.com/pay/unifiedorder`，返回 `code_url`。
- 支付结果来自官方异步通知或主动查询：`/alipay/notify`、`/wechat/notify`、`/alipay/query/{out_trade_no}`、`/wechat/query/{out_trade_no}`。

这条路不是“个人收款”，需要商户配置、密钥和回调验签。

### 2. 个人收款路径

`PayController` 是 XPay 个人收款核心，特点是无官方支付回调，靠“支付标识 + 人工审核”。

流程：

1. 用户提交订单：`POST /pay/add`，填写昵称、金额、邮箱、支付方式、是否自定义金额。
2. 系统生成订单 `Pay`，状态初始为 `0` 待审核。
3. 系统为订单生成支付标识：
   - 自定义金额模式：生成四位随机码 `payNum`，要求用户在支付备注里填写。
   - 固定金额二维码模式：按支付方式从 Redis 轮询 `XPAY_NUM_{payType}`，选择同金额下不同备注编号二维码。
4. 用户看到收款二维码或支付宝 scheme，实际付款到个人账户。
5. 系统给管理员发审核邮件，邮件中有 `pass/back/edit/del/close` 链接。
6. 管理员核对到账记录后点击通过：`/pay/pass`，订单改为 `1`，并给用户发送成功邮件。
7. 拒绝则 `state=2`，通过但不公开展示则 `state=3`，打开支付宝页后可设为 `state=4` 已扫码。

关键结论：个人收款没有可靠自动确认。支付成功必须由管理员核对金额、备注、到账时间后确认。

## Hub Store 现状

当前 `/card_store` 已经有完整商品和发卡链路：

- 用户侧：`hub/web/card_store/index.html`
  - 拉商品：`GET /api/card-store/products`
  - 创建订单：`POST /api/card-store/orders`
  - 支付后轮询：`GET /api/card-store/orders/{orderNo}`
  - 找回兑换码：`POST /api/card-store/recover`
- 后端：`hub/internal/httpapi/card_store_handlers.go`
  - 配置存在 `card_store_config`
  - 订单存在 `card_store_orders`
  - 当前支付方式为 Payment FM：`startPaymentFMOrder`
  - 回调入口：`/api/zhifuxpay/notify` 和 `/api/card-store/payment/notify`
  - 回调验签后调用 `markCardStoreOrderPaid`
  - `markCardStoreOrderPaid` 会生成服务兑换卡、邮件发送、自动兑换到购买邮箱。
- 管理端：`hub/web/admin/card-store-tab.js`
  - 配置 Payment FM 信息、商品价格、销售统计。

适配点很清楚：保留商品、订单、发卡、邮件、自动兑换链路，只替换“支付启动”和“支付确认”。

## 推荐方案

新增支付模式：`personal_semimanual`。

保留现有 `payment_fm` 模式，新增“个人收款半人工确认”模式，管理员可在 Store 配置里切换。

核心原则：用户打开支付页只代表“有支付意图/可能已扫码”，不能代表支付成功。系统在这个节点给管理员发邮件和后台生成待确认订单；管理员核对到账后进入确认页，再二次点击确认，系统才发卡和兑换服务。

### 数据模型

扩展 `cardStoreConfig`：

```go
type cardStoreConfig struct {
    PaymentMode string `json:"payment_mode,omitempty"` // payment_fm | personal_semimanual
    PersonalPayment cardStorePersonalPaymentConfig `json:"personal_payment,omitempty"`
}

type cardStorePersonalPaymentConfig struct {
    EnabledChannels []string `json:"enabled_channels,omitempty"` // alipay,wechat,qq,unionpay
    AlipayUserID string `json:"alipay_user_id,omitempty"`
    AlipayAccount string `json:"alipay_account,omitempty"`
    AlipayDisplayName string `json:"alipay_display_name,omitempty"`
    QRAssets []cardStorePersonalQRAsset `json:"qr_assets,omitempty"`
    Instruction string `json:"instruction,omitempty"`
}

type cardStorePersonalQRAsset struct {
    Channel string `json:"channel"`
    Label string `json:"label,omitempty"`
    ImageURL string `json:"image_url"`
    FixedAmount float64 `json:"fixed_amount,omitempty"`
}
```

扩展 `cardStoreOrder`：

```go
PaymentMode string `json:"payment_mode,omitempty"`
PayCode string `json:"pay_code,omitempty"`          // 短备注码，如 6 位
PayInstruction string `json:"pay_instruction,omitempty"`
PayQRURL string `json:"pay_qr_url,omitempty"`
PayDeepLink string `json:"pay_deep_link,omitempty"`
ReviewStatus string `json:"review_status,omitempty"` // pending,approved,rejected
ReviewedBy string `json:"reviewed_by,omitempty"`
ReviewedAt time.Time `json:"reviewed_at,omitempty"`
ReviewNote string `json:"review_note,omitempty"`
AdminApproveTokenHash string `json:"admin_approve_token_hash,omitempty"`
AdminDeleteTokenHash string `json:"admin_delete_token_hash,omitempty"`
OpenedPaymentAt time.Time `json:"opened_payment_at,omitempty"`
ReminderMailStatus string `json:"reminder_mail_status,omitempty"`
```

新增订单状态：

- `personal_created`：订单已创建，用户还未打开支付页。
- `personal_opened`：用户已打开支付页，系统已提醒管理员核对。
- `personal_rejected`：管理员驳回或删除。
- `paid`：管理员确认后进入现有发卡成功状态。
- `issue_failed`：到账确认但发卡失败，沿用现状。

### 下单流程

`CreateCardStoreOrderHandler` 按配置分支：

1. `payment_fm`：保持现状，调用 `startPaymentFMOrder`。
2. `personal_semimanual`：
   - 创建订单。
   - 生成 `PayCode`，建议格式 `CS` 后 6 位或纯 6 位数字，用户付款备注必须填写。
   - 根据渠道生成支付信息：
     - 支付宝：优先生成可带金额和备注的 scheme。
     - 微信/QQ/云闪付：返回配置好的个人收款二维码和备注说明。
   - 返回：`status=personal_created`、`order_no`、`pay_code`、`pay_qr_url`、`pay_deep_link`、`pay_instruction`。

支付宝 scheme 可采用 XPay README 中的形式：

```text
alipays://platformapi/startapp?appId=09999988&actionType=toAccount&goBack=NO&userId={userId}&amount={amount}&memo={payCode}
```

或新版扫码转账：

```text
alipays://platformapi/startapp?appId=20000123&actionType=scan&biz_data={"s":"money","u":"{userId}","a":"{amount}","m":"{payCode}"}
```

注意：scheme 兼容性受支付宝版本和风控影响。必须保留“复制备注 + 扫二维码/手动付款”的兜底路径。

### 用户打开支付后的提醒流程

新增用户侧“打开支付”记录接口，不能直接把订单当成成功：

- `POST /api/card-store/orders/{orderNo}/payment-opened`
  - 入参：`email`、`tenant_id`。
  - 校验订单属于该邮箱，状态为 `personal_created` 或 `personal_opened`。
  - 写入 `OpenedPaymentAt`，状态改为 `personal_opened`。
  - 生成管理员一次性确认 token 和删除 token，只保存 hash。
  - 给管理员邮箱发送提醒邮件。

邮件内容包含：

- 订单号、商品、购买邮箱、金额、支付备注码、支付渠道、打开支付时间。
- “确认到账”链接：进入确认页，不直接确认。
- “删除/驳回”链接：进入删除确认页，不直接删除。
- 管理后台订单链接。

该接口幂等：同一订单重复打开支付页时，只更新最近打开时间；邮件可限制频率，例如 5 分钟内只发一次，避免用户多次点击刷邮件。

### 确认流程

新增管理员接口，全部走现有 `requireTenantAdmin`：

- `GET /card_store/admin/confirm?order_no=...&token=...`
  - 展示确认页，列出订单详情、金额、备注码、打开支付时间。
  - 页面提示管理员先到支付宝/微信核对到账。
  - 必须二次点击“确认已到账并发卡”。
- `POST /api/admin/card-store/orders/{orderNo}/approve`
  - 入参：`amount`、`pay_time`、`channel_order_no`、`note`
  - 校验管理员登录态，或校验邮件 token。
  - 校验订单存在、金额匹配、状态为 `personal_opened` 或 `personal_rejected`。
  - 调用现有 `markCardStoreOrderPaid`，复用发卡、邮件、自动兑换逻辑。
- `GET /card_store/admin/delete?order_no=...&token=...`
  - 展示删除/驳回确认页，不直接删除。
- `POST /api/admin/card-store/orders/{orderNo}/reject`
  - 入参：`note`
  - 订单改为 `personal_rejected`，记录审核人和备注。

这样可最大限度复用现有发卡链路，避免另开一套“支付成功后发卡”逻辑。

### 用户界面

`hub/web/card_store/index.html` 改动：

- 创建订单后，如果返回 `personal_created`：
  - 显示订单号、付款金额、付款备注 `pay_code`。
  - 显示二维码或“打开支付宝”按钮。
  - 加复制按钮：复制备注、复制金额。
  - 用户点击“打开支付”时先调用 `payment-opened`，触发管理员提醒邮件，再打开 deep link 或显示二维码。
  - 文案明确：付款后等待管理员核对到账，页面可继续轮询订单状态。
- 轮询订单：
  - `personal_created`：显示“请打开支付并完成付款”。
  - `personal_opened`：显示“已提醒管理员，等待核对到账”。
  - `personal_rejected`：显示驳回原因。
  - `paid`：沿用现有兑换码显示。

### 管理界面

`hub/web/admin/card-store-tab.js` 改动：

- 支付设置新增模式选择：`Payment FM` / `个人收款人工确认`。
- 个人收款配置项：
  - 支付宝 userId / 账号 / 昵称。
  - 渠道启用开关。
  - 二维码图片 URL。
  - 用户支付说明。
- 销售管理列表新增：
  - `personal_opened` 待确认订单区。
  - 显示订单号、邮箱、商品、金额、备注码、创建时间、打开支付时间、提醒邮件状态。
  - 操作：确认到账、驳回/删除、复制备注码。

### 安全和风控

- 用户打开支付页不能自动发卡。必须管理员核对到账并二次确认。
- 用户侧不能暴露任何管理员 token 或审核 URL。
- 邮件确认链接只能进入确认页，不能 GET 直改状态。
- 管理确认接口必须 tenant admin 鉴权或一次性 token 校验，并写审计日志。
- 邮件 token 只保存 hash，设置过期时间，用后失效。
- 金额必须服务端校验：确认金额需等于订单金额，或要求管理员二次确认差异。
- `PayCode` 必须唯一且短期内不复用。建议使用订单号派生加随机后缀。
- 防重复发卡：沿用 `cardStoreCardID(orderNo)` 和 `markCardStoreOrderPaid` 的幂等逻辑。
- 订单存储当前限制保留 500 条。个人收款审核可能需要更多历史，开发时建议同步评估是否改为分页持久化表或提高保留策略。

## 开发拆分

### 第一步：后端支付模式抽象

- 增加 `PaymentMode` 配置和 normalize 默认值。
- 将当前 Payment FM 启动逻辑保留为 `payment_fm` 分支。
- 新增 `startPersonalSemimanualOrder`，返回二维码/备注/scheme。
- 扩展 `cardStoreOrderCreateResponse` 和 `GetCardStoreOrderHandler`。

### 第二步：打开支付提醒

- 新增 `payment-opened` handler 和路由。
- 写入打开支付时间、状态、token hash。
- 发送管理员提醒邮件。
- 增加邮件限频和幂等处理。

### 第三步：管理员确认接口

- 新增 approve/reject handler 和路由。
- approve 内调用 `markCardStoreOrderPaid`。
- GET 确认页只展示，不修改状态。
- 增加审计日志。
- 增加测试：确认发卡、重复确认幂等、金额不匹配拒绝、驳回状态。

### 第四步：用户侧 Store UI

- 渲染 `personal_created` / `personal_opened` 支付信息。
- 点击打开支付前调用 `payment-opened`。
- 支持复制备注码和打开 deep link。
- 轮询兼容 pending/rejected/paid。

### 第五步：管理员 UI

- 配置支付模式和个人二维码。
- 销售管理增加待审核订单操作。

### 第六步：测试

建议增加测试文件仍放在 `hub/internal/httpapi/card_store_handlers_test.go`：

- `TestCreateCardStoreOrderPersonalSemimanualReturnsPayCode`
- `TestPaymentOpenedSendsAdminReminderAndMarksOpened`
- `TestPaymentOpenedIsIdempotentAndRateLimited`
- `TestApprovePersonalSemimanualOrderIssuesCard`
- `TestApprovePersonalSemimanualOrderRejectsAmountMismatch`
- `TestRejectPersonalSemimanualOrderUpdatesStatus`
- `TestApprovePersonalSemimanualOrderIsIdempotent`
- `TestPersonalSemimanualOrderIsTenantScoped`

## 不建议做的方案

- 不建议模仿 XPay 邮件审核链接，把 approve URL 发邮件给管理员。Hub 已有 admin 权限体系，应走后台页面和审计日志。
- 不建议把“打开支付页/已扫码”伪装成支付成功。它只能触发管理员提醒。
- 不建议先支持多张同金额二维码轮询。Hub 商品价格固定且订单发卡价值明确，备注码 + 人工确认更稳。

## 待确认问题

1. 个人收款首期是否只做支付宝？建议首期只做支付宝 + 自定义二维码兜底。
2. 支付宝使用哪种 scheme：`09999988 toAccount` 还是 `20000123 scan biz_data`？建议两个都配置，默认 `toAccount`。
3. 订单确认是否允许金额差异？建议默认不允许。
4. 管理端是否需要上传二维码文件，还是先填外部图片 URL？建议首期先填 URL，后续再做上传。
5. 是否要保留现有 Payment FM 默认配置？建议保留，新增模式不破坏当前店铺。
6. 管理员提醒邮件收件人用现有 Hub SMTP 管理邮箱，还是 Store 单独配置？建议 Store 单独配置，缺省回退系统管理员邮箱。

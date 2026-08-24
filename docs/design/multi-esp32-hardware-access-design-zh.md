# MaClaw 多 ESP32 硬件接入功能设计

## 1. 目标与现状

目标是让一台 MaClaw GUI 同时绑定多个 ESP32。每块硬件都是独立 client，持有独立 bearer token；GUI 的硬件设置展示绑定列表，并允许删除绑定。消息链路保持为：

`MaClaw GUI --WebSocket--> Hub --HTTP long poll--> ESP32 client`

现有远程模式已具备多 client 队列，但产品层缺少三个关键闭环：ESP32 默认使用相同编译期 `CONFIG_MACLAW_CLIENT_ID`；Hub 没有面向 GUI 的设备查询/解绑协议；GUI 只能生成配对码，不能观察或删除绑定。Hub 原有 `tokens map[token]devicePrincipal` 已经为独立 token 提供了基础，配对时也会给每个 client 生成 256 bit token。

## 2. 范围

本期支持：

- ESP32 每次启动从 Wi-Fi STA MAC 派生 `clientId`，并仅将副本写入 NVS 供诊断/迁移使用；运行时不信任 NVS 中可能由量产镜像克隆的旧值。
- 同一 GUI machine 下多个独立 client/token 并存。
- Hub 持久化 client 的名称、协议版本、配对时间、最后连接时间。
- GUI 查询硬件列表、刷新状态、按 client 解绑。
- 解绑立即撤销该 client 的全部 token、队列和临时媒体，不影响同一 GUI 下其他设备。
- 保持现有配对、握手、incoming、outgoing、ack、音频/音量/欢迎语协议兼容。

本期不支持设备改名、共享一个 client 给多个用户、Hub 多节点实时状态同步。跨 GUI 设备转移由新 MaClaw 签发的一次性配对码显式授权，不要求旧 GUI 在线或先解绑。

## 3. 身份、凭据与持久化

### 3.1 ESP32

- `clientId = esp32s3-<12 位 STA MAC hex>`。
- 第一次启动写入 NVS `maclaw/device_id`，后续均读取该值，避免固件配置变化导致身份漂移。
- 无法读取 MAC 时兼容回退到 `CONFIG_MACLAW_CLIENT_ID`。
- 配对成功返回的 `gatewayToken` 继续写入 NVS；每台物理设备因此天然拥有独立 token。

### 3.2 Hub

`devicePrincipal` 在原有 `ClientID/MachineID/TenantID/UserID` 基础上增加：

- `ClientName`
- `ProtocolVersion`
- `PairedAt`
- `LastSeenAt`

数据继续存放在 `device_gateway_credentials_v1` 系统设置 JSON 中，兼容旧记录的零值反序列化，无数据库迁移。

安全边界：列表响应永远不返回 token；删除按当前已认证 GUI 的 `MachineID + ClientID` 双重约束执行，不能删除其他 machine 的设备。

`clientId` 仍是当前协议中的物理身份声明，尚无硬件证明。一个有效的一次性配对码是将该 `clientId` 转移到签发该码的 MaClaw 的显式授权：Hub 撤销同一 `clientId` 的所有旧 bearer、旧队列和旧临时媒体，再写入新 owner 的凭据。相同 GUI 内重配同样轮换 token。该规则对 Bread Compact、EchoEar-2ST、Fangtang-4G 和 Waveshare 一致，设备端不按板型实现“旧 owner 先解绑”的分支。

配对请求先规范化并校验 `clientId`（最多 128 个协议字符），再尝试兑换配对码。一次性配对码只在新 token 成功持久化之后消费；若凭据存储暂时失败，ESP32 可使用同一码安全重试。并发兑换由 Hub 在提交 token 前再次确认配对码仍存在，确保至多一个请求成功。相同设备重配成功时，同时清除旧消息队列和旧 token 创建的临时媒体。

公网配对接口按 HTTP 连接源地址限制为每分钟 12 次尝试，并返回 `429 + Retry-After`；限流不信任未经部署策略认证的 `X-Forwarded-For`，避免攻击者伪造头部轮换桶。语音配对继续使用更严格的“连接源地址 + clientId”每分钟 6 次限制。部署在反向代理后时，应由入口层同时执行真实客户端限流。

在线时间在 Hub 内存中按每个已认证请求精确更新；持久化最多每 5 分钟一次，以避免 ESP32 长轮询造成设置存储写放大。Hub 重启后恢复的是最近一次节流落盘时间，在线判定仍以重启后收到的新请求为准。

媒体上传在读取请求体前后都会核验媒体对象及随机 token；如果上传期间设备被解绑、重配或对象被淘汰，上传以 `media not found` 失败，不能把已经撤销的媒体对象重新写回。同一媒体 ID 同时只允许一个 PUT 写入；失败会释放上传占用供设备重试，完成后的媒体内容不可变，重复 PUT 作为幂等重试成功返回但不会覆盖已发布数据。

Hub 在插入新上传预约、音频或宠物帧之前先执行 LRU 淘汰，使媒体对象数始终不超过全局 200、每 client 64；实际驻留及并发上传预留的媒体数据同时限制为全局 64 MiB、每 client 16 MiB。预约对象在开始 PUT 前只计对象数；开始上传时按声明长度预留内存，未知长度按单对象上限 10 MiB 预留。若 PUT 的 `Content-Length` 与预约大小冲突，Hub 在读取 body 前直接拒绝；chunked 请求也只读取“预约大小 + 1”字节即可识别超长数据，避免错误请求仍分配 10 MiB。上传提交时按实际字节数重新执行容量检查，并排除正在提交的对象本身。淘汰优先选择请求方自己的旧媒体，再在全局压力下选择全局最旧媒体，避免单块 ESP32 持续挤占其他设备的资源。

附件转发在 Hub 互斥锁内完成归属、上传状态与字段校验，并深拷贝媒体数据后再交给异步消息链路，避免读取与上传/解绑并发时出现数据竞争或字段与内容不一致。同一条 incoming 请求累计展开的附件不超过每 client 16 MiB，防止重复引用同一媒体放大瞬时 base64 内存。

媒体 URL 的 24 小时过期时间是从创建时计算的绝对期限，下载或附件读取只更新 LRU 热度，不延长授权期限；到期对象在上传、下载、附件解析和定期淘汰任一路径都会失效并被删除。对进程内早期构造且没有 `ExpiresAt` 的兼容对象，仍使用原有“最后访问后 24 小时”规则作为回退。

Hub 对 outgoing 消息中的内部媒体 URL 维护队列引用计数：消息尚未 ACK 或尚未因 100 条队列上限被丢弃时，该媒体不会被容量 LRU 淘汰。入队时媒体的绝对过期时间至少延长到 5 分钟后的交付宽限期，避免恰好临近 24 小时边界的音频或宠物帧在 ESP32 收到消息后立即失效；ACK 或队列溢出会释放引用，之后媒体重新参与正常淘汰。HTTP outgoing 响应使用消息的深拷贝，序列化或调用方处理不能与 Hub 内部队列并发共享可变嵌套 map。

## 4. 协议设计

### 4.1 GUI → Hub：查询

WebSocket request：

```json
{"type":"im.device_gateway_devices_list","request_id":"device-list-...","payload":{}}
```

响应：

```json
{
  "type":"im.device_gateway_devices",
  "request_id":"device-list-...",
  "payload":{"devices":[{
    "clientId":"esp32s3-aabbccddeeff",
    "clientName":"ESP32-S3 Pet",
    "protocolVersion":"1.1",
    "pairedAt":"2026-08-04T12:00:00Z",
    "lastSeenAt":"2026-08-04T12:03:00Z",
    "online":true
  }]}
}
```

在线状态使用最后一次握手时间的 90 秒窗口，是轻量提示而非强一致 presence。

### 4.2 GUI → Hub：解绑

```json
{"type":"im.device_gateway_device_delete","request_id":"device-delete-...","payload":{"clientId":"esp32s3-aabbccddeeff"}}
```

成功返回带相同 `request_id` 的 `ack`。失败返回关联 `request_id` 的 `error`。

解绑事务语义：移除所有匹配 token，清理 client queue 与 client media，再持久化；若持久化失败，Hub 回滚 token、队列和媒体并向 GUI 返回错误，不产生“运行时已解绑但重启后恢复”的分裂状态。成功后 ESP32 下一次请求得到 401，进入现有重新配对恢复流程。

### 4.3 消息路由

- `clientId="*"`：向该 GUI 绑定的所有硬件广播设置或音频。
- 具体 `clientId`：仅向目标 client 投递。
- Hub 对具体 `clientId` 的 GUI 回复同样按当前 WebSocket 的 `MachineID + ClientID` 校验归属；目标属于其他 GUI 或不存在时返回 `HARDWARE_NOT_OWNED`，不能通过猜测 client ID 跨 machine 注入消息。
- ESP32 的 bearer 必须与请求里的 `clientId` 相同，Hub 继续在 handshake/incoming/outgoing/ack 全链路校验。

## 5. GUI 交互

硬件配置内新增“接入硬件”列表：

- 初始和网关状态变化时自动加载，也可手动刷新。
- 每行展示设备名、client ID、在线/离线、最后连接时间、协议版本。
- 空状态说明如何通过配对码添加硬件。
- “解绑”是破坏性操作，二次确认明确 token 会立即失效；进行中锁定重复删除。
- 列表不展示 bearer token。

界面沿用现有设置页色彩、按钮、边框和字号，不新增装饰性视觉语言；状态同时使用圆点与文字，避免只依赖颜色。

## 6. 兼容与迁移

- 旧 ESP32 固件继续使用编译期 client ID；单设备场景不受影响。
- 新固件首次升级会生成 MAC 派生 ID。若旧 token 绑定的是旧 client ID，握手会被 Hub 拒绝，设备进入现有 pairing recovery，需要重新配对一次。
- 旧 Hub 持久化 JSON 能直接加载；新增字段均为可选。
- 原 `im.device_gateway_pairing`、HTTP device gateway v1 和第三方协议 v1.1 不改路径和必填字段。

## 7. 验收标准

1. 两块使用相同固件的 ESP32 上报不同 `clientId`，分别配对后同时出现在 GUI。
2. Hub 重启后两条绑定仍存在，各自 token 仍有效。
3. GUI 的广播音量/欢迎语能进入两条独立 outgoing 队列。
4. 删除 A 后，A 的 token 返回 401，B 仍能 handshake、poll 和 ack。
5. GUI 不显示或记录 bearer token，越权删除被拒绝。
6. Hub 单测、GUI Go 单测、前端组件测试与 TypeScript 检查通过；ESP-IDF 完整构建通过。

# 任务列表：设备掉线自动返回大厅

## 任务

- [x] 1. 改造 DeviceNotifier.fireNotification 实现自动状态恢复
  - [x] 1.1 在 fireNotification 中添加空间状态检查逻辑：当 coordinator 不为 nil 且设备离线时，读取用户 SpaceState
  - [x] 1.2 实现 SpacePrivate 处理：当掉线设备 == PrivateTarget 时，调用 ExitPrivate + ClearSelectedMachine，生成自动返回大厅通知
  - [x] 1.3 实现 SpaceMeeting 处理：当掉线设备在 Participants 中时，调用 RemoveParticipant，根据剩余数量生成不同通知（>1: 简洁通知, ==1: 仅剩1台提示, ==0: ExitMeeting + StopDiscussion + 会议结束通知）
  - [x] 1.4 移除原有的 "/call <昵称>" 手动切换提示，非相关设备离线仅发送简洁通知
- [x] 2. 编写属性测试
  - [x] 2.1 Property 1 测试：私聊目标掉线自动返回大厅（状态 + selectedMachine 清除验证）
  - [x] 2.2 Property 2 测试：自动返回大厅通知格式验证（含设备名、不含 /call）
  - [x] 2.3 Property 3 测试：会议参与者掉线移除验证
  - [x] 2.4 Property 4 测试：所有会议参与者掉线自动结束会议验证
  - [x] 2.5 Property 5 测试：非相关设备掉线简洁通知验证
- [x] 3. 编写单元测试
  - [x] 3.1 coordinator 为 nil 时的降级行为测试
  - [x] 3.2 会议仅剩 1 台设备时的特殊提示消息测试
  - [x] 3.3 防抖期间空间状态已变化的场景测试

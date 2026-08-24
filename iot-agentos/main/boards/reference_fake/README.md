# Reference Fake Profile (HAL Phase 8)

用途：证明 ESP32 Unified HAL 的可扩展性契约。它不是正式硬件 profile。

- 只新增不可变 `device_profile_t` 声明；选择既有 compact private-HAL
  contract，不修改共享业务、共享 renderer 或 shared service
- 不修改 `main/services/*`、`main/presentation/*`、`main/device_api.c` 任何共享业务
- 物理形态复用 Bread Compact 的 240x320 + direct-I2S + 单主键，便于在 host/CI 上验证 ABI 而无需新驱动

正式发布仍以 `bread-compact / echoear-2st / fangtang-4g / waveshare-amoled-1.75c` 四 profile 为准（`docs/design/esp32-unification-remaining-tasks-zh.md: G2`）。`reference-fake` 已具有独立 `sdkconfig`、Kconfig、CMake source selection、依赖锁文件与 build wrapper 分支，可作 CI/ABI 构建演练；wrapper 会拒绝 flash、monitor、erase_flash，它绝不属于 release artifact、刷写目标或实机验收对象。

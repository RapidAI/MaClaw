### 测试总结：

| 项目 | 结果 |
|------|------|
| **Skill 注册** | ✅ 成功识别 |
| **命令行参数传递** | ✅ `--text` 和 `--target-lang` 正常 |
| **环境变量注入** | ✅ 通过 `env` 参数传入 API 配置 |
| **API 调用** | ✅ 使用智谱 GLM-4-Flash 模型 |
| **翻译结果** | ✅ "The weather is beautiful today, let's go for a walk in the park." → 中文翻译输出 |

修复过程中解决了两个关键问题：
1. **参数传递**：从位置参数改为 `--text` 命名参数，兼容 Skill 引擎的模板替换机制
2. **URL 拼接**：去掉了自动追加 `/v1` 的逻辑，让 `OPENAI_BASE_URL` 直接包含完整路径前缀
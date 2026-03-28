# 浏览器测试动作录制与回放指南

## 准备（一次性）

Chrome 地址栏输入 `chrome://inspect/#remote-debugging`，勾选 "Allow remote debugging for this browser instance"，重启 Chrome。

程序会自动通过 `DevToolsActivePort` 文件发现调试端口，无需命令行参数。

## 录制测试动作

在 maclaw 的 AI 助手对话框里依次说：

### 1. 连接浏览器

```
你: 连接浏览器
```

maclaw 自动发现 DevToolsActivePort，连上 Chrome。

### 2. 开始录制

```
你: 开始录制
```

maclaw 调用 `browser_record_start`，进入录制模式。

### 3. 用自然语言描述操作步骤

```
你: 打开 https://my-app.com/login
你: 在用户名输入框输入 testuser
你: 在密码框输入 123456
你: 点击登录按钮
你: 等待页面出现"欢迎"文字
你: 截个图看看
```

maclaw 会把自然语言翻译成对应的浏览器工具调用（`browser_navigate` / `browser_type` / `browser_click` / `browser_wait` / `browser_screenshot`），每一步都会实际执行并被录制器自动记录。

### 4. 停止录制并保存

```
你: 停止录制，保存为 login_test
```

流程保存到 `~/.maclaw/browser_flows/login_test.json`。

## 回放

### 基本回放

```
你: 回放 login_test
```

maclaw 调用 `browser_task_replay`，按录制的步骤自动重新执行一遍。

### 带参数替换的回放

```
你: 回放 login_test，把 testuser 替换成 admin
```

maclaw 传 `overrides` 参数，回放时自动替换对应值。

## 查看已有录制

```
你: 列出所有录制的浏览器流程
```

## 注意事项

- 全程用中文自然语言跟 maclaw 对话即可，不需要写代码或 JSON
- maclaw 负责理解意图并调用对应的浏览器工具
- 录制器在后台自动捕获每一步操作
- 如果 Chrome 版本较老不支持 `chrome://inspect` 方式，可用命令行启动：
  ```
  chrome.exe --remote-debugging-port=9222
  ```

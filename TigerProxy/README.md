# TigerProxy 设计说明

TigerProxy 是一个面向 CodeGen 模型服务的桌面协议转发服务，复用 `corelib/oauth` 的 CodeGen SSO 登录流程和 `corelib/codegenproxy` 的协议转换能力。

## 功能范围

- SSO 登录：主界面点击 SSO 登录后，使用与 TigerClaw onboarding 相同的 CodeGen callback 登录流程获取 token。
- 上游连接：登录成功后使用 SSO token 连接 `https://codegen.qianxin-inc.cn/api/v1`，并从模型列表中保存默认模型。
- 本地监听：默认监听 `0.0.0.0:18086`，同时支持 `127.0.0.1` 与本机局域网地址访问。
- 本地 API Key：默认值为 `tigerproxy-local-key`，用户可在主界面修改并保存；OpenAI 和 Anthropic 协议共用同一个 key。
- 协议输出：
  - OpenAI: `http://127.0.0.1:18086/v1`
  - Anthropic: `http://127.0.0.1:18086/anthropic/v1`
- 模型透传：OpenAI `/v1/chat/completions` 请求体原样转发，Anthropic `/messages` 转换为 OpenAI chat completions 时保留请求中的 `model` 字段，便于 agent 工具用 `/model` 查看和切换。
- 系统托盘：Windows 下使用当前 MaClaw 图标文件，托盘菜单仅包含“显示/隐藏”和“退出”。
- 单实例运行：重复启动 TigerProxy 时，新进程会退出，并把已运行实例的主界面显示到前台。

## 数据目录

所有配置默认保存到：

```text
~/.tigerproxy/settings.json
```

配置包含监听地址、本地 API Key、SSO access token、CodeGen base URL、默认模型和账号邮箱。

## Agent 配置示例

OpenAI 协议：

```text
OPENAI_BASE_URL=http://127.0.0.1:18086/v1
OPENAI_API_KEY=tigerproxy-local-key
```

Anthropic 协议：

```text
ANTHROPIC_BASE_URL=http://127.0.0.1:18086/anthropic/v1
ANTHROPIC_AUTH_TOKEN=tigerproxy-local-key
```

对外机器访问时，将 `127.0.0.1` 替换为主界面显示的局域网 IP。

## 编译

在项目根目录执行：

```bat
build_tigerproxy.cmd
```

产物默认输出到 `dist\TigerProxy.exe`。

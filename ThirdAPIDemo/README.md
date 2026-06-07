# MaClaw Third API Demo

一个 Go + Wails 桌面版第三方接入聊天 Demo。输入 MaClaw 第三方网关 URL 和 apikey 后，会调用：

- `POST /handshake`
- `POST /incoming`
- `GET /outgoing`
- `POST /ack`

## 运行

```powershell
wails dev .\ThirdAPIDemo
```

也可以直接 Go 编译检查：

```powershell
go build ./ThirdAPIDemo
```

默认网关 URL：

```text
http://127.0.0.1:18777/api/im-gateway/v1
```

`apikey` 对应 MaClaw GUI 里第三方网关配置的 Bearer token。

## 说明

前端通过 Wails bridge 调用 Go 绑定方法：`Connect`、`Send`、`Poll`。API key 不写入文件，只在当前桌面进程内用于请求 MaClaw 第三方网关。

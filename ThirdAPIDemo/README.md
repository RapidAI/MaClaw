# MaClaw Third API Demo

Go + Wails 桌面版第三方接入聊天 Demo。输入 MaClaw 第三方网关 URL 和 API Key 后，会调用：

- `POST /handshake`
- `POST /incoming`
- `GET /outgoing`
- `POST /ack`

## 功能

- 发送文字消息
- 选择本地图片、文件、语音文件并按第三方接入协议发送
- 小文件直接以 base64 `data` 发送，大文件先请求服务端 `POST /media/upload-url`，再由客户端上传到服务端返回的 URL
- 大文件上传受服务端返回的 `upload.maxBytes` 限制；如果请求上传 URL 时提供了大于 0 的 `sizeBytes`，实际 `PUT` 字节数必须完全一致
- 上传和下载只使用当前网关返回的 `/media/{id}/upload` 或 `/media/{id}` URL，并且 URL 必须带 `mediaToken`
- 接收回复后自动 ack
- 对带 `url` 的回复附件提供保存到本地的下载按钮

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

`apikey` 对应 MaClaw 用户设置里的第三方网关 Bearer token。

## 文件协议说明

点击“选择文件”后，Demo 会根据 MIME/扩展名推断附件类型：

- `image/*` -> `image`
- `audio/*` -> `voice`
- 其它 -> `file`

小文件（不超过网关 `maxDirectBytes`）会直接填入 `data`；大文件不会由客户端提供下载 URL，而是使用 MaClaw 服务端返回的 `id`/`url`。

发送单个附件时会填充：

```json
{
  "message": {
    "type": "image",
    "text": "optional caption",
    "fileName": "screen.png",
    "mimeType": "image/png",
    "data": "<base64 for small files>",
    "id": "server-media-id-for-large-files",
    "url": "http://gateway.example/api/im-gateway/v1/media/server-media-id?mediaToken=...",
    "sizeBytes": 12345
  }
}
```

发送多个附件时会使用 `message.attachments[]`。API key 不会写入文件，只在当前桌面进程内用于请求 MaClaw 网关。

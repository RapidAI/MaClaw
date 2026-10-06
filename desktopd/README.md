# desktopd — Docker 桌面服务

desktopd 在一台安装了 Docker 的主机上为每个 `tenant+user` 运行一个独立的 Chromium
桌面容器。Hub 的 desktoppool 和 MaClawSrv 通过 HTTP `/v1/*` API 调用它。

## 部署

从仓库根目录:

```sh
build_thirdapp.cmd        # 或按需编译 desktopd 与打包 image/desktop_supervisor.py
# remote_deploy.sh 由 deploy_desktopd.cmd 调起,负责:安装二进制、首次生成
# DESKTOPD_TOKEN、写入 .env、构建带 supervisor 的镜像、注册 systemd 服务。
```

必需环境变量(ds systemd 读 `.env`):

| 变量 | 说明 |
| --- | --- |
| `DESKTOPD_TOKEN` | 主 API key(`/v1/*` 的 Bearer,首次部署自动生成) |
| `DESKTOPD_ADVERTISE_HOST` | 容器端口回调时的对外主机名(必须是 Hub/MaClawSrv 可达的名字) |
| `DESKTOPD_ADDR` | 监听地址,默认 `127.0.0.1:18081`(反代或 SSH 隧道用) |
| `DESKTOPD_STATE_DIR` | 管理面板的状态目录(部署脚本指向 `<部署目录>/state`);`off` 完全禁用面板 |
| `DESKTOPD_MAX_IDLE` | 可选空闲回收,如 `24h`;超时的桌面会被停止(登录态先落盘) |
| `DESKTOPD_ALLOW_REMOTE_SETUP` | 设为 `1` 时允许从非 loopback 来源执行一次性的面板初始化 |

## 管理面板

浏览器打开 `http://<desktopd地址>:18081/admin`(默认只绑 loopback;远程通过
SSH 隧道 `ssh -L 18081:127.0.0.1:18081 <host>` 访问,连接在服务端表现为
loopback,不受 setup 来源限制影响)。

- **首次设置**:创建面板管理员(与 API key 无关)。默认只允许来源为
  loopback 的请求执行 setup,防止暴露的面板被远端抢注。
- **登录后**:查看服务信息(Docker 状态、默认镜像与资源)、查看主 key
  (`DESKTOPD_TOKEN`,不可删除)、添加/查看/删除额外 API key(删除立即生效)、
  查看运行中的桌面容器。

Hub 侧配置(管理页"桌面服务"标签页或 `POST /api/admin/desktop-services`)
使用面板里的任意一个 key 作为 `access_token`,校验方式:
`curl -H "Authorization: Bearer <key>" http://<host>:18081/v1/health`。

## 与 Hub / MaClawSrv 的关系

- Hub 保持 desktopd 服务与用户/部门分配表;四条 API:
  `POST /v1/desktops`(创建)、`/stop`、`/session`(拿 CDP/noVNC URL)、
  `/app`(xdotool)。
- 容器把 CDP(19020)与 noVNC(6080)发布到宿主机。两者的 gate 在容器内
  (desktop_supervisor.py)要求 Bearer token;token 以 URL userinfo 形式随
  `cdp_url` / `novnc_url` 分发,CDP 客户端转成 Authorization 头,Hub 的
  noVNC 代理代为注入。旧镜像无 token 时自动回退直连行为。
- 容器内 supervisor 每用户一个 X display + profile 卷;`ensure` 幂等,
  停止前 flush 浏览器登录态。

## 排障

- 服务日志:`<部署目录>/logs/desktopd.log` / `desktopd.err.log`。
- 每用户桌面日志:`/desktops/<key>/desktop.log`(容器内)。
- `systemctl status maclaw-desktopd`;手动起:`systemctl restart maclaw-desktopd`。
- token 不生效:先确认 `.env` 的 `DESKTOPD_TOKEN` 与 Hub 配置一致;面板
  删除 key 会立即生效。
- 首次建容器慢:`docker pull` 基础镜像可能超过调用方 2 分钟超时,先手动
  `docker pull maclaw-gui:1`。

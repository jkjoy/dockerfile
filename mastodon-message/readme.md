# mastodon-message

一个基于 `NoneBot2 + OneBot V11` 的 Mastodon 通知转发工具。

它默认通过 Mastodon Streaming API 的 `WebSocket` 实时接收通知，并通过 NoneBot 向 QQ 私聊或群发送消息。断线后会自动重连，并用 REST `notifications` API 做补偿同步，避免漏消息。

## 功能

- 使用 Mastodon Streaming API WebSocket 实时接收通知
- WebSocket 断线自动重连，并用 REST API 补偿同步
- 可切换为纯轮询模式
- 首次启动默认只同步最新 ID，不补发旧通知
- 持久化保存上次已处理的通知 ID
- 支持发送到 QQ 私聊和 QQ 群
- 支持 `mention`、`reply`、`reblog`、`favourite`、`poll` 等通知类型
- 对 `visibility=direct` 的 mention 会按私信提示格式化
- 提供 `mastodon-check` 管理命令用于手动触发检查

## 目录结构

```text
mastodon-message/
├─ bot.py
├─ Dockerfile
├─ requirements.txt
├─ .env.example
└─ src/
   └─ plugins/
      └─ mastodon_message/
         ├─ __init__.py
         ├─ config.py
         └─ service.py
```

## 依赖安装

```bash
pip install -r requirements.txt
```

## 配置

先复制一份配置文件：

```bash
cp .env.example .env
```

Windows PowerShell:

```powershell
Copy-Item .env.example .env
```

然后至少修改这些配置：

- `MASTODON_BASE_URL`
- `MASTODON_ACCESS_TOKEN`
- `MASTODON_NOTIFY_PRIVATE_IDS` 或 `MASTODON_NOTIFY_GROUP_IDS`

如果你要通过 QQ 协议端反向 WebSocket 连接到 NoneBot，推荐使用：

- `DRIVER=~fastapi+~httpx+~websockets`
- OneBot V11 反向 WebSocket 地址配置为 `ws://你的主机:8080/onebot/v11/ws`

如果你使用正向 WebSocket，也可以在 `.env` 中设置 `ONEBOT_WS_URLS`。

Mastodon 相关关键配置：

- `MASTODON_TRANSPORT=streaming` 表示使用 WebSocket 实时订阅
- `MASTODON_STREAMING_URL` 可手动指定流服务地址；留空时会自动从实例信息发现
- `MASTODON_STREAM_RECONNECT_DELAY` 控制断线重连间隔
- `MASTODON_CHECK_INTERVAL` 仅在 `polling` 模式下使用

## Mastodon Token 权限

访问 `notifications` API 和 `user:notification` 流，建议用户 Token 直接授予 `read`，或至少授予 `read:notifications` 与 `read:statuses`。

## 启动

```bash
python bot.py
```

## 管理命令

配置好 `SUPERUSERS` 后，可以在 QQ 中发送：

```text
/mastodon-check
```

它会立即触发一次 REST 补偿检查，并把结果回复给超级用户。

## Docker

构建镜像：

```bash
docker build -t mastodon-message:latest .
```

运行示例：

```bash
docker run -d \
  --name mastodon-message \
  -p 8080:8080 \
  -v $(pwd)/data:/app/data \
  --env-file .env \
  mastodon-message:latest
```

Windows PowerShell:

```powershell
docker run -d `
  --name mastodon-message `
  -p 8080:8080 `
  -v ${PWD}\data:/app/data `
  --env-file .env `
  mastodon-message:latest
```

## 说明

- 该项目监听的是 Mastodon 通知，不是公共时间线。
- 首次启动时，默认只记录当前最新通知 ID，不会把旧通知全部推送到 QQ。
- 默认主链路是 WebSocket 实时订阅，断线后会自动重连。
- 状态文件默认保存在 `data/mastodon_message_state.json`。

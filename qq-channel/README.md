# 独立 QQ 双向通道

通过腾讯 [qqbot-nodejs SDK](https://github.com/tencent-connect/qqbot-nodejs) 连接 QQ 官方机器人，给 Typecho Enhancement 1.5.0+ 提供私聊、群聊主动通知和固定博客管理指令。无需模型。常驻 Node.js 服务使用 WebSocket 接收官方事件，插件经 HTTP 提交通知，管理指令经 HMAC 签名回传插件。

Enhancement 1.5.2 起仅保留「独立 QQ 通道」和「官方 QQ Bot 服务（Webhook）」，已移除 OpenClaw / Hermes 复用模式。使用本服务时选择「独立 QQ 通道」；已有绑定和管理指令配置继续有效。

## 准备与启动

1. 在 [QQ 开放平台](https://q.qq.com/) 创建机器人，取得 AppID/AppSecret，开通私聊、群聊及相应主动发送权限。按控制台当前要求启用 WebSocket 事件接入、配置测试成员/群或发布范围、出口 IP 白名单。此服务使用 `C2C_MESSAGE_CREATE` 和 `GROUP_AT_MESSAGE_CREATE`，群内指令需 @机器人。
2. 将机器人加为好友、加入目标群，并按平台要求允许主动消息。消息包含 URL 时还需完成消息域名报备。
3. 按下面任意一种方式配置。Compose 使用已发布的 `jkjoy/qq-channel:latest` 镜像，支持 amd64 和 arm64。

### 使用 `.env` 文件

在服务器复制本目录，执行：

```bash
cp .env.example .env
chmod 600 .env
openssl rand -hex 32
```

把 AppID、AppSecret 和新生成的随机值填入 `.env` 的 `BOT_API_TOKEN`。首次可先不配置管理指令。

### 不使用 `.env` 文件

在部署目录新建 `compose.yaml`，或用下面内容替换仓库中的 `compose.yaml`。移除原有的 `env_file` 配置，否则 Compose 仍会要求提供 `.env`。

先执行 `openssl rand -hex 32` 生成 `BOT_API_TOKEN`，再将下面三个必填项 `QQ_APP_ID`、`QQ_APP_SECRET`、`BOT_API_TOKEN` 替换为实际值：

```yaml
services:
  qq-channel:
    image: jkjoy/qq-channel:latest
    restart: unless-stopped
    init: true
    environment:
      QQ_APP_ID: "替换为机器人AppID"
      QQ_APP_SECRET: "替换为机器人AppSecret"
      BOT_API_TOKEN: "替换为生成的随机密钥"
      BOT_HOST: "0.0.0.0"
      BOT_PORT: "8080"
      BOT_DATA_DIR: "/data"
      # 仅需通知时留空；开启 QQ 管理时按下文配置，群白名单可留空。
      ENHANCEMENT_COMMAND_URL: ""
      ENHANCEMENT_COMMAND_SECRET: ""
      QQ_ALLOWED_USERS: ""
      QQ_ALLOWED_GROUPS: ""
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - channel-data:/data
    read_only: true
    tmpfs:
      - /tmp
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    stop_grace_period: 20s

volumes:
  channel-data:
```

配置值包含 `$` 时，在 Compose 中写成 `$$`，避免被当作变量替换。执行 `chmod 600 compose.yaml` 限制凭据文件的读取权限。

### 启动和检查

两种方式均在 `compose.yaml` 所在目录执行：

```bash
docker compose pull
docker compose up -d
docker compose logs --tail=50 qq-channel
curl http://127.0.0.1:8080/readyz
```

`/readyz` 返回 200 且 `connected:true` 表示已连接 QQ；未连接返回 503。正常响应例如：

```json
{"ok":true,"connected":true,"commands":false}
```

`commands:false` 表示未配置博客管理指令，通知、身份查询与绑定仍可使用。`/healthz` 只检查服务与存储可用性。服务需要访问 QQ 的 HTTPS API 和 WebSocket 网关，无需给 QQ 配置本服务的公网事件回调地址。

默认只向宿主机的 `127.0.0.1:8080` 开放 API。同机 PHP 可直接连接；PHP 在其他容器时应将两者加入同一个 Docker 网络，服务地址使用 `http://qq-channel:8080`，此时不使用 PHP 容器自身的 `127.0.0.1`。跨服务器访问 API 应通过 HTTPS 反向代理并限制来源。

## 绑定私聊与群聊

### 接收别名是什么

插件中的 **独立 QQ / 官方 Bot 接收别名**，是创建绑定口令时通过 `name` 自己指定的名字。下面用 `owner` 表示私聊目标、`admins` 表示群聊目标；名称可自行选择，不会自动从 QQ 资料中生成。

| 内容 | 示例 | 填写位置 |
| --- | --- | --- |
| 接收别名 | `owner`、`admins` | 插件的“独立 QQ / 官方 Bot 接收别名”及通知接口的 `targets` |
| 用户 OpenID | 机器人回复中的用户标识 | 开启博客管理时填写 `QQ_ALLOWED_USERS` 和插件的用户白名单 |
| 群 OpenID | 机器人在群内回复的群标识 | 允许群管理时填写 `QQ_ALLOWED_GROUPS` |

别名支持 1–64 位字母、数字、下划线和连字符。OpenID 来自官方事件，不能用 QQ 号、群号替代，也不能跨 AppID 复用。仅接收通知时，无需设置管理白名单。

### 1. 设置服务地址和访问令牌

以下命令使用 Bash。在部署服务器上使用本机地址；从其他机器操作时，将 `BOT_SERVICE_URL` 改为已配置的 HTTPS 服务根地址，例如 `https://bot.example.com`，末尾不要加 `/api` 或斜杠。

输入的 Token 必须与服务配置的 `BOT_API_TOKEN` 完全一致。即使部署时没有使用 `.env`，也可以在当前终端临时设置这些变量：

```bash
BOT_SERVICE_URL='http://127.0.0.1:8080'
read -rs -p 'BOT_API_TOKEN: ' BOT_API_TOKEN; echo
export BOT_SERVICE_URL BOT_API_TOKEN
```

### 2. 查询已有接收别名

```bash
curl --fail-with-body --silent --show-error "$BOT_SERVICE_URL/api/targets" \
  -H "Authorization: Bearer $BOT_API_TOKEN"
```

尚未绑定任何目标时返回：

```json
{"ok":true,"targets":[]}
```

私聊和群聊绑定完成后的响应例如：

```json
{
  "ok": true,
  "targets": [
    {"name":"admins","type":"group","openid":"GROUP_OPENID","bound_by":"USER_OPENID","updated":1800000000000},
    {"name":"owner","type":"user","openid":"USER_OPENID","bound_by":"USER_OPENID","updated":1800000000000}
  ]
}
```

`targets` 只列出已成功绑定的目标，读取其中的 `name` 即可，不需要查找 `bound` 字段。上例可在插件中填写 `owner`、`admins`。已有需要的目标时，可以直接跳到“插件配置”。

### 3. 绑定私聊目标

创建名为 `owner` 的私聊绑定：

```bash
curl --fail-with-body --silent --show-error "$BOT_SERVICE_URL/api/bindings" \
  -H "Authorization: Bearer $BOT_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"owner","type":"user"}'
```

成功创建口令时返回 HTTP `201`，响应例如：

```json
{"ok":true,"name":"owner","type":"user","command":"绑定 0123456789abcdef0123456789abcdef","expires_in":600}
```

复制**你实际请求返回的完整 `command`**，在 **10 分钟内私聊发给机器人**。上面的口令仅作格式示例，不能直接用于绑定。口令只能使用一次；仅创建口令尚未完成绑定，目标不会立即出现在 `/api/targets` 中。

机器人确认后回复：

```text
绑定成功：owner
用户 OpenID：USER_OPENID
```

这时插件的接收别名填写 `owner`，回复中的用户 OpenID 可用于以后配置 QQ 管理白名单。重新查询 `/api/targets` 可确认 `owner` 已保存。

### 4. 绑定群聊目标

先确认机器人已经加入目标群，再创建名为 `admins` 的群聊绑定：

```bash
curl --fail-with-body --silent --show-error "$BOT_SERVICE_URL/api/bindings" \
  -H "Authorization: Bearer $BOT_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"admins","type":"group"}'
```

在目标群中 **@机器人**，发送本次返回的完整 `command`。成功回复例如：

```text
绑定成功：admins
用户 OpenID：USER_OPENID
群 OpenID：GROUP_OPENID
```

插件的群通知接收别名填写 `admins`。私聊口令用于私聊、群聊口令用于目标群；类型不匹配时绑定失败。发送 `我的ID` 可再次查询当前用户 OpenID，群内需 @机器人，还会显示群 OpenID。

重新申请同名口令会使之前未使用的同名口令失效；成功重新绑定后，后续通知使用新目标，已入队通知保留原目标快照。绑定口令应由站长在目标会话使用；绑定通知目标不授予博客管理权限。

## 插件配置

在 Enhancement → QQ 中选择 **独立 QQ 通道（无需智能体，推荐）**：

| 插件设置 | 填写内容 |
| --- | --- |
| 独立 QQ / 官方 Bot 服务地址 | 同机 `http://127.0.0.1:8080`，或 PHP 可访问的 HTTPS 服务根地址，例如 `https://bot.example.com`；不要追加 `/api` |
| 服务访问令牌 | 与服务 `.env` 或 Compose `environment` 中的 `BOT_API_TOKEN` 一致 |
| 独立 QQ / 官方 Bot 接收别名 | 填写 `/api/targets` 返回的 `name`，每行一个，只填已经绑定的目标 |
| 通知中的链接 | 未报备消息域名时选省略链接 |
| 评论 / 友情链接申请通知 | 按需分别开启 |

仅绑定私聊 `owner` 时，接收别名输入框填写：

```text
owner
```

私聊 `owner` 和群聊 `admins` 都已绑定时，填写：

```text
owner
admins
```

### 保存和测试通知

保存设置后点击 **发送QQ通知测试**，检查对应私聊或群聊是否收到消息。插件显示已入服务队列和通知 ID，表示服务已接收请求；最终送达情况通过下文的状态接口查询。绑定成功只能确认目标身份，主动通知还取决于 QQ 平台权限和接收方设置。

也可以绕过插件手动提交通知，验证服务到 QQ 的投递链路。先按上文设置 `BOT_SERVICE_URL`、`BOT_API_TOKEN`，确认 `owner` 已绑定后执行：

```bash
curl --fail-with-body --silent --show-error "$BOT_SERVICE_URL/api/notifications" \
  -H "Authorization: Bearer $BOT_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"message":"QQ 通知测试","targets":["owner"],"idempotency_key":"manual-test-001"}'
```

成功受理返回 HTTP `202`，例如：

```json
{"ok":true,"status":"queued","id":"0123456789abcdef0123456789abcdef01234567"}
```

保留实际返回的 `id`，按“队列与运行维护”中的命令查询。再次测试新消息时更换 `idempotency_key`，例如 `manual-test-002`；相同 key 和相同内容返回原通知，不会再次入队。测试群通知时，将 `targets` 改为已经绑定的 `["admins"]`；同时发往两处时使用 `["owner","admins"]`，并为这次测试使用新的 key。

业务评论通知沿用插件原规则，仅通知审核通过的读者评论；待审内容可通过指令查询。

## 开启 QQ 管理

1. 私聊发送 `我的ID`，群内 @机器人发送 `我的ID`，取得需要授权的用户 OpenID 和群 OpenID。
2. 再生成一个不同的随机密钥。在 `.env` 或 `compose.yaml` 的 `environment` 中填写 `ENHANCEMENT_COMMAND_SECRET`、`QQ_ALLOWED_USERS`（逗号分隔，最多 20 个），以及允许群管理时的 `QQ_ALLOWED_GROUPS`。用户和群分别校验：允许的群内也只有允许的用户能操作。
3. 将博客指令接口填入 `ENHANCEMENT_COMMAND_URL`，例如 `https://blog.example.com/action/enhancement-edit?do=qq-agent-command`。未启用伪静态时保留 `/index.php/action/`，子目录安装也需保留完整路径及 `?do=qq-agent-command`。
4. 插件中开启“允许 QQ 侧管理博客”，填写相同的指令密钥、允许的用户 OpenID（每行一个）和执行身份的 Typecho 管理员 UID。
5. 执行 `docker compose up -d --force-recreate`，向机器人发送 `博客状态` 验证。

只需通知时，保持 `ENHANCEMENT_COMMAND_URL/SECRET` 为空及插件管理开关关闭。QQ 服务的密钥、白名单及插件管理开关均需满足才可操作；命令拒绝时检查两边配置。QQ 服务到博客需可达，远程使用 HTTPS。

| 命令 | 用途 |
| --- | --- |
| `帮助` / `/help` | 显示固定指令 |
| `我的ID` | 显示当前用户及群 OpenID |
| `绑定 口令` | 绑定通知目标 |
| `博客状态` | 查询评论数量 |
| `待审评论 5` / `已审评论 5` / `垃圾评论 5` | 列出评论，数量可省略，范围 1–20 |
| `通过评论 123` | 发布该评论 |
| `暂缓评论 123` / `垃圾标记 123` | 转为待审或垃圾评论 |
| `回复评论 123：谢谢留言` | 以设置的管理员身份回复已通过的评论 |
| `待审友链 5` | 查询待审友链 |
| `通过友链 12` / `驳回友链 12` | 显示或隐藏友链，驳回不删除记录 |

群内需 @机器人。不执行自由文本、任意脚本或自然语言任务。回复只接受 4000 字节内纯文本，博客端继续校验危险内容。单用户每分钟最多接受 10 条已识别指令，超出忽略；每条回复最多 1800 字节。身份查询、帮助与持口令绑定不需要管理白名单。

## 队列与运行维护

先按上文设置 `BOT_SERVICE_URL`、`BOT_API_TOKEN`，再将 `NOTIFICATION_ID` 替换为插件或通知接口返回的真实通知 ID：

```bash
NOTIFICATION_ID='替换为返回的通知ID'
# 查询通知最终状态：queued / sent / failed
curl --fail-with-body --silent --show-error "$BOT_SERVICE_URL/api/notifications/$NOTIFICATION_ID" \
  -H "Authorization: Bearer $BOT_API_TOKEN"
# 仅重试最终失败的目标，已成功目标不重发
curl --fail-with-body --silent --show-error -X POST "$BOT_SERVICE_URL/api/notifications/$NOTIFICATION_ID/retry" \
  -H "Authorization: Bearer $BOT_API_TOKEN"
```

顶层 `status` 为 `queued` 表示仍有目标等待投递，`sent` 表示全部目标已取得 QQ 消息回执，`failed` 表示所有目标已结束处理且至少一个失败。`targets` 中可查看每个目标的 `attempts`、`last_error` 和 `receipt`；其中目标 `status` 的数字 `0`、`1`、`2` 分别表示待投递、成功、失败。

通知提交接口为 `POST /api/notifications`，JSON 字段为 `message`（最多 1800 字节）、`targets`（1–20 个别名）、`idempotency_key`（1–128 位字母数字下划线连字符）。同一 key 和内容返回相同 ID，内容冲突返回 409；成功受理返回 202。所有 `/api/` 接口都使用 Bearer Token，正文最多 64 KiB。

SQLite 保存绑定、通知目标快照、每个目标的投递状态及入站去重。通知与 QQ 回复最多自动尝试 5 次，暂时断线时暂停；SDK 负责重连、心跳与令牌刷新。通知队列和已保存的指令结果可以跨进程恢复，WebSocket 会话本身不跨重启保存。已完成记录保留 30 天，待发记录保留至处理完成。

博客操作的请求 ID 由官方消息身份稳定生成。博客操作成功后先保存结果再回复 QQ，回复失败不会重新执行博客操作；结果不确定时提示到后台核对。网络中断可能造成 QQ 重复送达，采用至少一次投递语义，不能保证腾讯侧绝不重复。

数据保存在 Compose 的 `channel-data` 命名卷。升级用 `docker compose pull && docker compose up -d`；备份前先 `docker compose stop`，再备份卷中 SQLite 文件，避免遗漏 WAL。不要在升级时执行 `down -v`。替换 AppID 应使用新数据卷并重新绑定，服务会拒绝启动到其他 AppID 的旧卷。

主动通知仍受机器人资质、接口权限、好友/入群关系、主动接收设置、频控和消息域名限制。收到绑定回复不代表已取得主动发送权限；失败状态中仅保存脱敏错误代码，服务日志不会记录 AppSecret、令牌或评论正文。

## 常见问题

| 现象 | 检查方法 |
| --- | --- |
| 不知道插件的接收别名填什么 | 查询 `/api/targets`，填写其中的 `name`；私聊示例为 `owner`，群聊示例为 `admins`，每行一个 |
| `/api/targets` 返回空数组 | 尚未完成 QQ 内的绑定，或当前容器使用了新的数据卷；创建口令后还需向机器人发送返回的 `command` |
| API 返回 `401 unauthorized` | 核对请求和服务的 `BOT_API_TOKEN`，确认包含 `Authorization: Bearer ...`，反向代理应保留该请求头 |
| HTTPS 地址返回网页或 `404` | 核对域名是否指向 QQ 服务、反向代理是否转发 `/api/`；插件中填写服务根地址 |
| `/readyz` 返回 `503` 或 `connected:false` | 查看 `docker compose logs --tail=100 qq-channel`，检查 AppID/AppSecret、QQ 平台的 WebSocket 接入配置及服务器访问 QQ 的出站网络 |
| 提示口令无效、类型不匹配或已过期 | 重新申请口令，在 10 分钟内发到对应的私聊或群聊；同名新口令会替换旧口令 |
| 群内发送绑定命令没有回复 | 确认机器人已入群、`connected:true`，并在目标群 @机器人发送本次返回的完整命令；检查开放平台群消息权限及测试范围 |
| 通知返回 `target_not_bound` | 填写 `/api/targets` 中实际存在的 `name`，检查大小写；绑定后再发送 |
| 通知持续 `queued` 或最终 `failed` | 先检查 `/readyz`，再查询通知状态中的 `attempts`、`last_error`；核实主动发送权限、好友/入群关系、接收方设置、QQ 频率限制和消息域名报备 |
| 绑定成功但博客管理命令无响应 | 继续完成“开启 QQ 管理”的独立配置；用户白名单填写真实用户 OpenID，群管理还需群 OpenID，且用户与群都必须获准 |

## 本地验证

需要 Node.js 24+：

```bash
npm ci --ignore-scripts
npm test
```

测试用本地模拟 QQ HTTP / WebSocket 服务验证真实腾讯 SDK，覆盖鉴权、绑定、白名单、队列重试、去重及重启恢复；不连接真实 QQ。与插件的跨项目联调见插件 `tests/qq-channel/README.md`。CI 先运行测试再构建 amd64/arm64 镜像，推送到配置的 Docker Hub 账号的 `qq-channel` 仓库。

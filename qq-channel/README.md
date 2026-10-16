# 独立 QQ 双向通道

通过腾讯 [qqbot-nodejs SDK](https://github.com/tencent-connect/qqbot-nodejs) 连接 QQ 官方机器人，给 Typecho Enhancement 1.5.0+ 提供私聊、群聊主动通知和固定博客管理指令。无需安装 OpenClaw、Hermes 或配置模型。常驻 Node.js 服务使用 WebSocket 接收官方事件，插件经 HTTP 提交通知，管理指令经 HMAC 签名回传插件。

## 准备与启动

1. 在 [QQ 开放平台](https://q.qq.com/) 创建机器人，取得 AppID/AppSecret，开通私聊、群聊及相应主动发送权限。按控制台当前要求启用 WebSocket 事件接入、配置测试成员/群或发布范围、出口 IP 白名单。此服务使用 `C2C_MESSAGE_CREATE` 和 `GROUP_AT_MESSAGE_CREATE`，群内指令需 @机器人。
2. 将机器人加为好友、加入目标群，并按平台要求允许主动消息。消息包含 URL 时还需完成消息域名报备。
3. 在服务器复制本目录，执行：

```bash
cp .env.example .env
chmod 600 .env
openssl rand -hex 32
```

把 AppID、AppSecret 和新生成的随机值填入 `.env` 的 `BOT_API_TOKEN`。首次可先不配置管理指令。

```bash
docker compose up -d --build
docker compose logs --tail=50 qq-channel
curl http://127.0.0.1:8080/readyz
```

`/readyz` 返回 200 且 `connected:true` 表示已连接 QQ；未连接返回 503。`/healthz` 只检查服务与存储可用性。服务需要访问 QQ 的 HTTPS API 和 WebSocket 网关，无需给 QQ 配置本服务的公网事件回调地址。

默认只向宿主机的 `127.0.0.1:8080` 开放 API。同机 PHP 可直接连接；PHP 在其他容器时应将两者加入同一个 Docker 网络，服务地址使用 `http://qq-channel:8080`，此时不使用 PHP 容器自身的 `127.0.0.1`。跨服务器访问 API 应通过 HTTPS 反向代理并限制来源。

## 绑定私聊与群聊

在当前 shell 设置访问令牌（与 `.env` 一致），随后申请私聊绑定口令：

```bash
read -rs -p 'BOT_API_TOKEN: ' BOT_API_TOKEN; echo
export BOT_API_TOKEN
curl -sS http://127.0.0.1:8080/api/bindings \
  -H "Authorization: Bearer $BOT_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"owner","type":"user"}'
```

把返回的完整 `command`（`绑定 32位口令`）私聊发给机器人。口令 10 分钟内有效且只能使用一次，绑定结果会显示用户 OpenID。

绑定群目标时改为 `{"name":"admins","type":"group"}`，再到目标群 **@机器人** 发送返回的绑定命令。绑定结果包含用户与群 OpenID。可以用 `我的ID` 再次查询。绑定口令应由站长在目标会话使用；绑定通知目标不授予博客管理权限。

```bash
curl -sS http://127.0.0.1:8080/api/targets \
  -H "Authorization: Bearer $BOT_API_TOKEN"
```

别名支持 1–64 位字母、数字、下划线和连字符；同名重新绑定会影响后续通知，已入队通知保留原目标快照。OpenID 来自官方事件，不能用 QQ 号、群号替代，也不能跨 AppID 复用。

## 插件配置

在 Enhancement → QQ 中选择 **独立 QQ 通道（无需智能体，推荐）**：

| 插件设置 | 填写内容 |
| --- | --- |
| 独立 QQ / 官方 Bot 服务地址 | 同机 `http://127.0.0.1:8080`，或 PHP 能访问的服务根地址 |
| 服务访问令牌 | `BOT_API_TOKEN` |
| 接收别名 | 每行一个，如 `owner`、`admins` |
| 通知中的链接 | 未报备消息域名时选省略链接 |
| 评论 / 友情链接申请通知 | 按需分别开启 |

保存并点击“发送QQ通知测试”。插件显示已入服务队列和通知 ID，最终送达情况通过服务 API 查询。业务评论通知沿用插件原规则，仅通知审核通过的读者评论；待审内容可通过指令查询。

## 开启 QQ 管理

1. 私聊发送 `我的ID`，群内 @机器人发送 `我的ID`，取得需要授权的用户 OpenID 和群 OpenID。
2. 再生成一个不同的随机密钥。在 `.env` 填写 `ENHANCEMENT_COMMAND_SECRET`、`QQ_ALLOWED_USERS`（逗号分隔，最多 20 个），以及允许群管理时的 `QQ_ALLOWED_GROUPS`。用户和群分别校验：允许的群内也只有允许的用户能操作。
3. 将插件 QQ 设置显示的完整“QQ 指令接口”填入 `ENHANCEMENT_COMMAND_URL`，包括 `?do=qq-agent-command`。例如 `https://blog.example.com/action/enhancement-edit?do=qq-agent-command`；关闭重写的网站地址可能包含 `index.php`，请直接复制后台显示的值。
4. 插件中开启“允许 QQ 侧管理博客”，填写相同的指令密钥、允许的用户 OpenID（每行一个）和执行身份的 Typecho 管理员 UID。无需填写 Agent 接口 URL、Agent 通知凭据、Agent 接收目标等项目。
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

```bash
# 查询通知最终状态：queued / sent / failed
curl -sS "http://127.0.0.1:8080/api/notifications/通知ID" \
  -H "Authorization: Bearer $BOT_API_TOKEN"
# 仅重试最终失败的目标，已成功目标不重发
curl -sS -X POST "http://127.0.0.1:8080/api/notifications/通知ID/retry" \
  -H "Authorization: Bearer $BOT_API_TOKEN"
```

通知提交接口为 `POST /api/notifications`，JSON 字段为 `message`（最多 1800 字节）、`targets`（1–20 个别名）、`idempotency_key`（1–128 位字母数字下划线连字符）。同一 key 和内容返回相同 ID，内容冲突返回 409；成功受理返回 202。所有 `/api/` 接口都使用 Bearer Token，正文最多 64 KiB。

SQLite 保存绑定、通知目标快照、每个目标的投递状态及入站去重。通知与 QQ 回复最多自动尝试 5 次，暂时断线时暂停；SDK 负责重连、心跳与令牌刷新。通知队列和已保存的指令结果可以跨进程恢复，WebSocket 会话本身不跨重启保存。已完成记录保留 30 天，待发记录保留至处理完成。

博客操作的请求 ID 由官方消息身份稳定生成。博客操作成功后先保存结果再回复 QQ，回复失败不会重新执行博客操作；结果不确定时提示到后台核对。网络中断可能造成 QQ 重复送达，采用至少一次投递语义，不能保证腾讯侧绝不重复。

数据保存在 Compose 的 `channel-data` 命名卷。升级用 `docker compose up -d --build`；备份前先 `docker compose stop`，再备份卷中 SQLite 文件，避免遗漏 WAL。不要在升级时执行 `down -v`。替换 AppID 应使用新数据卷并重新绑定，服务会拒绝启动到其他 AppID 的旧卷。

主动通知仍受机器人资质、接口权限、好友/入群关系、主动接收设置、频控和消息域名限制。收到绑定回复不代表已取得主动发送权限；失败状态中仅保存脱敏错误代码，服务日志不会记录 AppSecret、令牌或评论正文。

## 本地验证

需要 Node.js 24+：

```bash
npm ci --ignore-scripts
npm test
```

测试用本地模拟 QQ HTTP / WebSocket 服务验证真实腾讯 SDK，覆盖鉴权、绑定、白名单、队列重试、去重及重启恢复；不连接真实 QQ。与插件的跨项目联调见插件 `tests/qq-channel/README.md`。CI 先运行测试再构建 amd64/arm64 镜像，推送到配置的 Docker Hub 账号的 `qq-channel` 仓库。

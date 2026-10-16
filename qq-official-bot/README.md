# QQ 官方机器人通知服务

需要无需智能体的双向通道、QQ 固定管理指令或 WebSocket 接入时，使用新的 [qq-channel 服务](../qq-channel/README.md)。本目录保留原 Webhook 通知方式；两者在插件中对应不同选项，绑定数据与凭据分别配置。

为 Typecho Enhancement 插件提供 QQ 私聊、群聊主动通知。服务接收 QQ 官方事件回调，验证签名，用一次性口令绑定接收目标，再由持久队列向 `https://api.bot.qq.com` 发送消息。机器人凭据保存在本服务，Typecho 通过独立 API Token 提交通知。

## 在 QQ 开放平台准备机器人

1. 前往 [QQ 开放平台](https://q.qq.com/)创建机器人，根据账号资质选择个人或企业类型，取得机器人 **AppID** 和 **AppSecret**。
2. 在控制台申请或开启私聊、群聊消息，以及相应的主动发送权限。实际可用能力、审核要求、测试成员和上线范围以当前控制台为准。
3. 配置事件订阅：私聊使用 `C2C_MESSAGE_CREATE`；群聊使用 `GROUP_AT_MESSAGE_CREATE`，控制台支持且已开通时也可订阅 `GROUP_MESSAGE_CREATE`。将事件投递地址设为后续部署的 `https://bot.example.com/qq/webhook`。
4. 按当前控制台要求配置回调域名、服务器出口 IP 白名单及消息 URL 报备。通知内容携带网站链接时，对应域名也需要满足平台要求。
5. 私聊接收者添加机器人为好友；群聊将机器人添加到目标群。接收方还需允许该机器人的主动消息。

机器人权限、接收方设置和官方频率限制共同决定消息能否送达；绑定成功表示已识别目标，实际发送权限仍需通过测试消息确认。正式及测试范围使用官方控制台配置。

## Docker 部署

需要 Docker Engine 和 Compose 插件。以下命令在 `qq-official-bot` 目录运行；命令示例使用 Bash。

```bash
cp .env.example .env
python -c "import secrets; print(secrets.token_urlsafe(32))"
```

编辑 `.env`：填写 `QQ_APP_ID`、`QQ_APP_SECRET`，将生成的随机字符串填入 `BOT_API_TOKEN`。Token 至少 32 个字符，生成结果直接填写为单行值，无需加引号。请勿使用示例占位值。

Windows PowerShell 可以用 `Copy-Item .env.example .env` 复制配置，随机 Token 生成命令相同。

| 变量 | 说明 |
| --- | --- |
| `QQ_APP_ID` | QQ 官方机器人 AppID |
| `QQ_APP_SECRET` | QQ 官方机器人 AppSecret，用于凭证获取及事件验签 |
| `BOT_API_TOKEN` | Typecho 和管理 API 共用的独立随机密钥，至少 32 个字符 |
| `BOT_DATA_DIR` | 持久数据目录，Compose 固定为 `/data` |
| `BOT_PORT` | 容器监听端口，Compose 固定为 `8080` |

```bash
docker compose up -d --build
docker compose ps
curl --fail --silent --show-error http://127.0.0.1:8080/healthz
docker compose logs --tail=100 qq-official-bot
```

容器使用 Python 3.12、非 root 用户和 Waitress，默认映射 `127.0.0.1:8080`，退出后自动重启。绑定关系与发送队列保存在命名卷 `bot-data` 的 `/data` 中；正常更新和重启保留数据。`docker compose down -v` 会删除该卷，同时删除绑定和队列。

从仓库根目录启动时，使用：

```bash
docker compose -f qq-official-bot/compose.yaml up -d --build
```

单独构建镜像时，构建上下文也必须是服务目录：

```bash
docker build -t qq-official-bot:local -f qq-official-bot/Dockerfile qq-official-bot
```

如果将命名卷改为宿主机目录挂载，请先让该目录可由容器用户 `10001:10001` 写入。`/healthz` 用于检查本地服务，不会证明 QQ 权限已开通或测试消息已送达。

## 配置 HTTPS 事件回调

在已有 HTTPS 站点的 Nginx `server` 内增加下列配置，并配置该域名的有效 TLS 证书：

```nginx
location = /qq/webhook {
    client_max_body_size 1m;
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
}

location / {
    return 404;
}
```

将 `https://bot.example.com/qq/webhook` 保存到 QQ 开放平台。服务响应官方首次校验挑战，随后验证事件请求签名；此接口不使用 `BOT_API_TOKEN`。反向代理应保留请求体及官方 `X-Signature-*` 请求头。

公网仅需开放 `/qq/webhook`。管理 API 由 Typecho 通过本机或内网连接；如果 Typecho 在另一台机器上，请为 `/api/` 另行设置 HTTPS 及来源限制，并保留 `Authorization` 请求头。不同容器之间的 `127.0.0.1` 各自独立：将 Typecho 和本服务接入同一 Docker 网络后，可使用 `http://qq-official-bot:8080` 访问。

## 绑定私聊和群聊

所有 `/api/` 请求都必须携带 `Authorization: Bearer BOT_API_TOKEN`。以下 Bash 示例从 `.env` 读取 Token 为普通数据，命令在服务目录运行：

```bash
BOT_SERVICE_URL='http://127.0.0.1:8080'
BOT_API_TOKEN="$(python -c "from pathlib import Path; print(next(line.split('=', 1)[1].strip() for line in Path('.env').read_text(encoding='utf-8-sig').splitlines() if line.startswith('BOT_API_TOKEN=')))")"
```

为博客所有者创建私聊绑定：

```bash
curl --fail-with-body --silent --show-error \
  "$BOT_SERVICE_URL/api/bindings" \
  -H "Authorization: Bearer $BOT_API_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"name":"owner","type":"user"}'
```

接口返回 `command`，例如 `/bind <一次性口令>`。接收者在 **10 分钟内**私聊机器人，原样发送该指令。服务从官方事件中取得 `user_openid`，保存为别名 `owner` 的目标。

创建群聊绑定：

```bash
curl --fail-with-body --silent --show-error \
  "$BOT_SERVICE_URL/api/bindings" \
  -H "Authorization: Bearer $BOT_API_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"name":"blog-group","type":"group"}'
```

在目标群中 **@机器人**并发送返回的 `command`。服务从群消息事件中取得 `group_openid`，保存为别名 `blog-group` 的目标。每个口令仅能使用一次，请只交给目标接收者或目标群管理员。

查看已绑定目标：

```bash
curl --fail-with-body --silent --show-error \
  "$BOT_SERVICE_URL/api/targets" \
  -H "Authorization: Bearer $BOT_API_TOKEN"
```

别名可以自定义，使用简单 ASCII 名称，例如 `owner`、`blog-group`。QQ 号和群号无法代替官方 OpenID；绑定必须经由机器人收到的官方消息事件完成。

确认绑定状态后再发送通知。重新绑定同名目标需要重新创建口令；已有队列继续使用入队时的接收者快照。

## 接入 Typecho Enhancement

在 Enhancement 插件的 QQ 通知设置中选择官方机器人，并填写：

| 插件配置 | 示例 |
| --- | --- |
| `qq_notify_provider` | `official` |
| `qq_official_url` | `http://127.0.0.1:8080`，或 Typecho 可访问的内网/HTTPS 服务地址 |
| `qq_official_token` | `.env` 中的 `BOT_API_TOKEN` |
| `qq_official_targets` | 每行一个已绑定别名，例如 `owner` 和 `blog-group` |
| `qq_official_include_links` | 根据 QQ 平台的消息 URL 报备结果选择是否携带网站链接 |

继续使用插件已有的评论通知、友链通知开关。官方模式将通知提交给本服务的持久队列，由后台发送。Enhancement 1.5.2 起仅保留本官方 Webhook 服务和独立 `qq-channel` 服务，已移除 OpenClaw / Hermes 复用模式及旧 go-cqhttp 推送方式；旧模式用户需重新选择并配置受支持通道。使用本服务时选择「官方 QQ Bot 服务（Webhook）」。**AppSecret 只填写在 Bot 服务中**。

先通过插件的测试通知确认目标和权限，再开启所需通知类型。

## 手动测试及队列状态

```bash
curl --fail-with-body --silent --show-error \
  "$BOT_SERVICE_URL/api/notifications" \
  -H "Authorization: Bearer $BOT_API_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"message":"博客通知测试","targets":["owner","blog-group"],"idempotency_key":"manual-demo-001"}'
```

成功接收时返回 HTTP `202`，例如 `{"ok":true,"id":"...","status":"queued"}`。`queued` 表示队列已接收，最终结果通过返回的 `id` 查询。每条业务通知使用独立的 `idempotency_key`；同一事件重试时复用该值，避免重复入队。反复手动测试时换用新的键。

插件测试按钮也会显示通知 ID，服务日志以通知 ID 和目标别名记录投递结果。查询时的 `sent` 表示 QQ 接口返回了有效消息 ID；`failed` 可查看各目标的 `last_error`。若 QQ 返回待审核结果，服务记录 `message_audit_pending`，由管理员核实审核结果后决定是否重试。

```bash
NOTIFICATION_ID='替换为返回的id'
curl --fail-with-body --silent --show-error \
  "$BOT_SERVICE_URL/api/notifications/$NOTIFICATION_ID" \
  -H "Authorization: Bearer $BOT_API_TOKEN"
```

服务处理凭证缓存、限速及可重试错误；最终失败时先查看队列状态与服务日志，修复权限或配置后，可手动重试失败目标。已成功的目标不会因此再次发送：

```bash
curl --fail-with-body --silent --show-error --request POST \
  "$BOT_SERVICE_URL/api/notifications/$NOTIFICATION_ID/retry" \
  -H "Authorization: Bearer $BOT_API_TOKEN"
```

常见排查顺序：检查 `/healthz` 和容器日志；确认 `.env` 的 AppID、AppSecret 与开放平台机器人一致；检查回调验证和事件订阅；确认机器人已加好友或入群且已绑定；最后检查主动消息权限、接收方允许设置、频率限制以及消息中的 URL 报备。

同一接收目标的发送间隔至少 3.1 秒，可重试错误最多自动尝试 5 次；官方仍会检查机器人整体频控及每日额度。活动队列上限为 1000 个目标投递任务。持久记录包含通知正文、目标 OpenID 和历史状态，存放在数据卷中；请按站点需要管理备份与保留时间。

服务采用至少一次投递：HTTP 重复提交会按幂等键去重，但 QQ 已成功接收、服务尚未写回状态时进程中断，恢复后仍可能重复发送。首次回调校验仅接受由字母、数字、下划线或连字符组成的不透明挑战 token，普通消息事件始终验证原始请求体的 Ed25519 签名。

## 开发验证

本地运行需要 Python 3.12 与 `requirements.txt` 中的依赖。测试使用临时 SQLite 和模拟 QQ 客户端，不向真实 QQ 发送消息：

```bash
python -m pip install -r requirements.txt
python -m unittest discover -s tests -v
```

若本机同时有 PHP 和 Enhancement 插件，可设置 `ENHANCEMENT_PLUGIN_DIR` 为插件的绝对目录后执行同一测试命令，额外验证 PHP → Waitress HTTP 服务 → 后台发送队列的完整流程。未设置时自动跳过该项跨项目测试。

## 官方资料

- [访问凭证](https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/access-token.html)
- [消息接口与发送规则](https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/overview.html)

自动化测试使用模拟官方接口；实际消息送达还需部署者使用真实机器人凭据，在已获授权的私聊和群聊中验证。

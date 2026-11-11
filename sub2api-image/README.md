# 映象 · sub2api 图片工作台

通过 sub2api 生成图片，在网页中浏览和下载自己的创作历史。独立应用，使用 Node.js 和内置 SQLite，无需外部数据库或前端构建。

- 游客无需登录或验证即可生成图片；支持注册、登录和退出。
- 按图片张数限制每日额度：游客 3 张、注册用户 10 张、付费用户 100 张。
- 管理员网页登录后，可在「用户管理」中手动调整用户等级；不接入支付平台。
- 中文创作界面，支持尺寸、质量、数量和背景设置，模型由部署环境统一指定。
- 后台串行任务队列，关闭网页后继续生成，支持进度查看和失败提示。
- 原图保存在服务器，历史支持搜索、筛选、分页、预览、下载、提示词复用和删除。
- 各用户的历史和图片按归属隔离，API Key 仅通过服务端环境变量配置。

## Docker Compose 启动

工作流发布镜像后，在本目录复制配置示例：

```bash
cp .env.example .env
```

PowerShell 使用 `Copy-Item .env.example .env`。

编辑 `.env`，填写 `SUB2API_BASE_URL`、`SUB2API_API_KEY`、`SUB2API_MODEL`。地址可以是 `https://sub2api.example.com`、以 `/v1` 结尾的地址，或部署在子路径下的 API 地址。模型需为该 Key 对应分组实际支持的生图模型。

设置 `ADMIN_PASSWORD` 为 8–128 字符，首次启动会创建 `ADMIN_USERNAME` 对应的管理员，默认用户名为 `admin`。密码留空时应用仍可启动，但不会创建管理员。管理员使用网页登录，不使用浏览器 Basic 验证。

```bash
docker compose pull
docker compose up -d
```

访问 <http://localhost:3080> 即可开始创作。网页不提供 API 地址、Key 或模型切换；修改 `.env` 后执行 `docker compose up -d` 更新容器环境。

默认只绑定本机。需要局域网访问时，可将 `BIND_ADDRESS` 改成相应监听地址。游客按客户端 IP 共享额度；通过受控反向代理访问时，按实际代理跳数配置 `TRUST_PROXY`，一层代理设为 `1`，并确保应用仅能通过这些代理访问。默认 `0` 不信任 `X-Forwarded-For`。

```bash
docker compose logs -f
docker compose down
```

Compose 使用 Docker Hub 镜像 `jkjoy/sub2api-image:latest`。更新时执行 `docker compose pull && docker compose up -d`；需要固定版本时，将 `compose.yaml` 中的 `latest` 换成工作流发布的 7 位提交号。

从本地源码构建：

```bash
docker build -t jkjoy/sub2api-image:latest .
docker compose up -d
```

## 使用与每日额度

无需注册即可输入提示词，选择参数并点击「开始生成」。桌面支持 `Ctrl+Enter`，macOS 支持 `⌘+Enter`。每次提交创建独立记录，同一时刻执行一个任务，最多允许 20 个正在处理或排队的任务。

| 身份 | 每日图片额度 | 额度归属 |
| --- | --- | --- |
| 游客 | 3 张 | 客户端 IP，同一 IP 的游客共享额度 |
| 注册用户 | 10 张 | 登录账号 |
| 付费用户 | 100 张 | 登录账号，由管理员手动设置等级 |
| 管理员 | 100 张 | 管理员账号 |

额度按北京时间 `Asia/Shanghai` 的自然日计算，每天 00:00 开始新一天。提交时先预留所选图片数量，完成后按实际保存的图片张数扣除；生成失败或未保存的部分归还额度，删除图片不返还额度。跨午夜完成的任务仍结算在提交当天。

游客历史通过当前浏览器的 HttpOnly Cookie 识别；清除 Cookie 或更换浏览器无法继续访问原游客历史，同一 IP 的每日额度不会因此重置。注册账号与游客的额度、历史保持独立，注册或登录不会认领游客图片，也不转移已用额度；退出后恢复当前浏览器的游客历史。

登录后可在不同浏览器查看该账号的历史。「历史记录」支持搜索、状态筛选、大图浏览和原图下载；「再次创作」填回提示词与参数，手动确认后生成。

不同模型支持的尺寸、数量、质量和背景不同，界面提供常用参数，不代表上游支持全部组合。透明背景需模型支持，原图格式为上游实际返回的 PNG、JPEG 或 WebP，单张最多 30 MB。

对于 basic 分组，在 `.env` 中设置 `SUB2API_BASIC_MODE=true`。此时仅向上游发送 `prompt` 和 `response_format: b64_json`，不发送模型、尺寸、质量、背景或数量，由上游决定参数，每次生成 1 张。

## 用户管理

通过 `.env` 中的管理员用户名和密码登录，打开「用户管理」，将目标账号调整为「注册用户」或「付费用户」。付费等级代表每天 100 张的额度，由管理员手动授予或降级，没有在线支付、订单或自动续费。

已有管理员后，修改 `ADMIN_PASSWORD` 并更新容器会更新密码并撤销其旧登录会话；管理员用户名也可通过 `ADMIN_USERNAME` 修改。若目标用户名已被其他账号使用，应用会拒绝创建或改名，不会将该账号自动提升为管理员。密码留空不删除已经创建的管理员。

## 数据保存与升级

```text
data/
├── history.sqlite    # 历史、图片索引、账号、会话及每日用量
├── history.sqlite-*  # SQLite 运行期间的 WAL/SHM 文件
└── images/           # 生成的原始图片
```

直接运行时默认使用应用目录下的 `data/`，可通过 `DATA_DIR` 修改。Compose 使用命名卷 `image-data` 挂载到 `/app/data`，重建容器会保留数据；`docker compose down -v` 会删除该数据卷。

备份时先停止服务，再备份完整数据目录或 Docker 数据卷，并单独保存包含上游 Key 和管理员配置的 `.env`。删除历史记录也会删除对应本地原图。

服务重启会将未完成任务标为失败，保留已保存图片，释放未使用的预留额度，不会自动重发付费请求。多图任务中后续下载失败时，也保留此前成功下载的图片。

从旧版共享工作台升级时，需将上游地址、Key 和模型填写到 `.env`；应用不再读取旧 `data/config.json`，也不使用 `AUTH_USERNAME` / `AUTH_PASSWORD`。旧共享历史仅管理员可查看，不会公开给游客或新注册用户。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SUB2API_BASE_URL` | 空 | sub2api 根地址或以 `/v1` 结尾的 API 地址，部署时填写 |
| `SUB2API_API_KEY` | 空 | sub2api API Key，部署时填写，不通过网页设置 |
| `SUB2API_MODEL` | `gpt-image-2` | 全站固定的生图模型，按实际分组填写 |
| `SUB2API_BASIC_MODE` | `false` | basic 分组兼容模式，每次生成 1 张并省略高级参数 |
| `GENERATION_TIMEOUT_SECONDS` | `900` | 一次生图及下载总超时，5–1800 秒 |
| `ADMIN_USERNAME` | `admin` | 环境配置的管理员用户名 |
| `ADMIN_PASSWORD` | 空 | 管理员密码，8–128 字符；首次创建及后续更新使用 |
| `TRUST_PROXY` | `0` | 信任的受控反向代理跳数；默认不信任代理头 |
| `DATA_DIR` | 应用目录下的 `data` | 持久化目录，容器内为 `/app/data` |
| `HOST` | `0.0.0.0` | 监听地址，示例 `.env` 为 `127.0.0.1`，Compose 内为 `0.0.0.0` |
| `PORT` | `3000` | 服务端口，Compose 内固定 3000 |
| `WEB_PORT` | `3080` | Compose 映射到宿主机的端口 |
| `BIND_ADDRESS` | `127.0.0.1` | Compose 宿主机绑定地址 |

## 直接使用 Node.js

需要 **Node.js 24 或更新版本**，SQLite 使用 Node.js 内置模块。

```bash
npm ci
cp .env.example .env
```

PowerShell 使用 `Copy-Item .env.example .env`。填写 `.env` 中的上游和管理员配置后运行：

```bash
npm start
```

访问 <http://localhost:3000>。修改 `.env` 后重启服务；开发时可以运行 `npm run dev`。

## 接口与常见问题

服务端使用 `Authorization: Bearer <API Key>` 调用 `POST /v1/images/generations`。支持 `data[].b64_json` 和 `data[].url`，收到 URL 后立即下载到本地，避免临时链接过期。后台任务由本应用管理，不依赖 sub2api 默认关闭且要求对象存储的异步接口。

- **未配置上游**：在 `.env` 中填写地址、Key、模型，然后更新容器或重启 Node.js 进程。
- **没有可用账号或模型权限错误**：确认 Key 的分组支持生图和配置的模型；basic 分组设置 `SUB2API_BASIC_MODE=true`。
- **参数错误**：调整到上游实际支持的尺寸、质量、数量或背景；模型由部署者在环境变量中修改。
- **游客额度识别不正确**：检查 `TRUST_PROXY` 和反向代理是否按实际访问路径配置；同一公网 IP 的游客共享 3 张额度。
- **生成超时**：默认最多等待 900 秒。超时后上游仍可能生成或计费，应先检查 sub2api 状态，再决定是否重试；程序不会自动重试生图请求。
- **反向代理连接中断**：应用与 sub2api 之间的代理需允许长时间请求；浏览器与本应用之间使用短请求轮询。
- **Compose 无法连接宿主机的 sub2api**：容器的 `localhost` 指向自身。Docker Desktop 可使用 `http://host.docker.internal:端口`；同一 Docker 网络可使用 sub2api 服务名与容器端口。
- **Node.js 提示 SQLite 为实验功能**：这是内置 SQLite 的提示，无需安装其他 SQLite 依赖。

程序使用单实例，数据卷应由一个应用进程使用，不支持多副本同时写入同一数据目录。

sub2api 接口参考：[图片接口说明](https://github.com/Wei-Shaw/sub2api/blob/main/docs/OPENAI_IMAGES_CODEX.md)、[图片请求解析与能力选择](https://github.com/Wei-Shaw/sub2api/blob/main/backend/internal/service/openai_images.go)。

## GitHub Actions 发布

工作流位于 [`.github/workflows/sub2api-image.yml`](../.github/workflows/sub2api-image.yml)，推送到 `main` 且修改本应用或工作流时运行，也可手动触发。使用 Node.js 24 安装依赖并测试，通过后构建 `linux/amd64`、`linux/arm64`，发布：

- `jkjoy/sub2api-image:latest`
- `jkjoy/sub2api-image:<7位提交号>`

沿用仓库 Secrets：`DOCKER_USER_NAME` 为拥有镜像推送权限的 Docker Hub 用户名，`DOCKER_ACCESS_TOKEN` 为访问令牌。

## 验证

```bash
npm test
```

测试使用本机模拟上游，不消耗实际 API 额度。`GET /healthz` 提供不调用上游的进程健康检查。

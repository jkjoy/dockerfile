# 映象 · sub2api 图片工作台

通过 sub2api 生成图片，并在网页中浏览永久保存在本地的创作历史。独立应用，无需安装外部数据库，也无需前端构建。

- 中文创作界面，支持自定义模型、方形/横向/纵向尺寸、质量、数量和透明背景。
- 基础兼容模式：只发送提示词，兼容不能指定高级参数的 basic 分组。
- 后台串行任务队列，关闭网页后继续生成；排队中、生成中及失败状态均可查看。
- 图片自动保存到服务器，支持大图预览、多图切换、原图下载。
- 历史按时间浏览，支持提示词/模型搜索、状态筛选、分页、参数复用和删除。
- API Key 仅保存在服务端，可选全站用户名/密码验证。

## Docker Compose 启动

工作流发布镜像后，在本目录执行：

```bash
cp .env.example .env
docker compose pull
docker compose up -d
```

PowerShell 复制命令为 `Copy-Item .env.example .env`。

访问 <http://localhost:3080>，点击「连接设置」填写 sub2api 地址和 API Key。地址可填写 `https://sub2api.example.com` 或 `https://sub2api.example.com/v1`，也支持部署在子路径下的地址。

也可以先在 `.env` 中填写 `SUB2API_BASE_URL`、`SUB2API_API_KEY` 和 `SUB2API_MODEL`。网页保存后的连接配置优先于环境变量，重启后仍会保留；后续可直接在网页修改。

默认仅绑定本机。需要局域网或反向代理访问时，将 `.env` 中 `BIND_ADDRESS` 改成所需地址，例如 `0.0.0.0`；通过 `AUTH_PASSWORD` 设置访问密码，用户名默认为 `admin`。访问者共享同一个工作台、配置与历史。

```bash
docker compose logs -f
docker compose down
```

Compose 使用 Docker Hub 镜像 `jkjoy/sub2api-image:latest`。更新时执行 `docker compose pull && docker compose up -d`；需要固定版本时，将 `compose.yaml` 中的 `latest` 换成工作流发布的 7 位提交号。

如需从本地源码构建，可以执行：

```bash
docker build -t jkjoy/sub2api-image:latest .
docker compose up -d
```

## GitHub Actions 发布

工作流位于 [`.github/workflows/sub2api-image.yml`](../.github/workflows/sub2api-image.yml)。推送到 `main` 且修改本应用或工作流时自动运行，也可以在 GitHub Actions 页面手动触发。

工作流先使用 Node.js 24 安装依赖并执行测试，通过后构建 `linux/amd64`、`linux/arm64` 镜像，发布到 Docker Hub：

- `jkjoy/sub2api-image:latest`
- `jkjoy/sub2api-image:<7位提交号>`

沿用本仓库的两个 GitHub Actions Secrets：

| Secret | 用途 |
| --- | --- |
| `DOCKER_USER_NAME` | Docker Hub 登录用户名，需有 `jkjoy/sub2api-image` 的推送权限 |
| `DOCKER_ACCESS_TOKEN` | Docker Hub Access Token |

## 直接使用 Node.js

需要 **Node.js 24 或更新版本**，SQLite 使用 Node.js 内置模块。

```bash
npm ci
cp .env.example .env
npm start
```

PowerShell 同样使用 `Copy-Item .env.example .env`。访问 <http://localhost:3000>，在网页中配置连接。开发时可以运行 `npm run dev`。

## 使用方式

1. 在「连接设置」保存地址、API Key 和默认模型。默认 `gpt-image-2`，也可以输入你分组实际支持的模型名称。
2. 写下提示词，选择参数，点击「开始生成」。桌面浏览器支持 `Ctrl+Enter`，macOS 支持 `⌘+Enter`。
3. 在预览区查看进度与结果。每次提交创建独立记录，同一时刻执行一个任务，最多允许 20 个正在处理/排队的任务。
4. 在「历史记录」搜索、筛选和放大图片；「再次创作」会填回提示词与参数，确认后再手动生成。

「获取模型」读取 sub2api 的 `/v1/models`，筛选图片模型；如果分组未公开模型列表，可以直接输入模型名称。

不同模型支持的尺寸、数量、质量和背景并不相同。默认界面提供 GPT Image 常用尺寸，DALL·E 用户可使用 1024×1024、standard/hd，并以其模型数量限制为准。透明背景需要上游模型支持，输出为上游实际返回的 PNG、JPEG 或 WebP，单张最多 30 MB。

**basic 分组**请展开「更多设置」，勾选「基础兼容模式」。此时只向上游发送 `prompt` 和 `response_format: b64_json`，不发送模型、尺寸、质量、背景或数量，由上游决定参数，生成 1 张。切换模式后网页会保留原有选择，便于之后复用。

## 数据保存与备份

数据目录包含：

```text
data/
├── config.json       # 网页保存的连接配置，包含 API Key
├── history.sqlite    # 提示词、参数、状态与图片索引
├── history.sqlite-*  # SQLite 运行期间的 WAL/SHM 文件
└── images/           # 生成的原始图片
```

直接运行时默认目录为当前应用下的 `data/`，可用 `DATA_DIR` 修改。Compose 使用命名卷 `image-data`，挂载到容器 `/app/data`；容器重建后仍然保留数据。执行 `docker compose down -v` 会删除该数据卷。

备份时先停止服务，再备份完整数据目录或 Docker 数据卷。配置文件含有 API Key，应与图片和数据库一并妥善保存。删除历史记录也会删除该记录对应的本地原图。

服务重启会将尚未完成的任务标为失败，保留已经保存的图片，不会自动重新提交。多图任务中后续下载失败时，也保留此前成功下载的图片，可在详情中继续下载。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SUB2API_BASE_URL` | 空 | sub2api 根地址或以 `/v1` 结尾的地址 |
| `SUB2API_API_KEY` | 空 | sub2api 生成的 API Key |
| `SUB2API_MODEL` | `gpt-image-2` | 默认模型，可在网页修改 |
| `GENERATION_TIMEOUT_SECONDS` | `900` | 一次生图及下载的总超时，5–1800 秒 |
| `AUTH_USERNAME` | `admin` | 启用访问验证后的用户名 |
| `AUTH_PASSWORD` | 空 | 非空时对页面、API 和图片启用 Basic 验证 |
| `DATA_DIR` | 应用目录下的 `data` | 持久化目录；容器内为 `/app/data` |
| `HOST` | `0.0.0.0` | 服务监听地址；示例 `.env` 为 `127.0.0.1`，Compose 内为 `0.0.0.0` |
| `PORT` | `3000` | 服务端口；Compose 内固定 3000 |
| `WEB_PORT` | `3080` | Compose 映射到宿主机的端口 |
| `BIND_ADDRESS` | `127.0.0.1` | Compose 宿主机绑定地址 |

## 接口与常见问题

应用实际调用 `POST /v1/images/generations`，用 `Authorization: Bearer <API Key>` 验证。支持 `data[].b64_json` 以及 `data[].url`，收到 URL 后立即下载到本地，避免临时链接过期。没有使用 sub2api 默认关闭且要求对象存储的异步接口；后台任务由本应用管理。

- **没有可用账号或模型权限错误**：确认 Key 对应的分组支持生图及所选模型；basic 分组开启基础兼容模式。
- **参数错误**：调整为上游实际支持的尺寸、质量、数量或背景。接口可自定义模型，但不代表当前分组支持所有模型。
- **生成超时**：默认等待最多 900 秒。即使网页请求已超时，上游仍可能生成或计费；先检查 sub2api 状态，再决定是否重新提交，程序不会自动重试生图请求。
- **反向代理连接中断**：应用与 sub2api 之间的代理需允许长时间生图请求。上游代理的读取超时应与生图超时匹配；浏览器与本应用之间使用短请求轮询。
- **Compose 访问不到宿主机的 sub2api**：容器的 `localhost` 指向自身。Docker Desktop 可使用 `http://host.docker.internal:端口`；同一 Docker 网络可使用 sub2api 服务名及容器端口。
- **密钥看起来为空**：网页不会回传已保存的 Key，输入框留空即保留原值。
- **Node.js 提示 SQLite 为实验功能**：这是 Node.js 内置 SQLite 的提示；无需安装其他 SQLite 依赖。

程序是单实例、共享工作台，数据卷应交给一个应用进程使用，不支持多副本同时写入同一数据目录。

sub2api 接口参考：[图片接口说明](https://github.com/Wei-Shaw/sub2api/blob/main/docs/OPENAI_IMAGES_CODEX.md)、[图片请求解析与能力选择](https://github.com/Wei-Shaw/sub2api/blob/main/backend/internal/service/openai_images.go)。

## 验证

```bash
npm test
```

测试使用本机模拟上游，不消耗实际 API 额度，覆盖请求兼容、密钥处理、后台生图、原图落盘、历史持久化、搜索分页、删除与访问验证。

`GET /healthz` 提供不调用上游的进程健康检查。

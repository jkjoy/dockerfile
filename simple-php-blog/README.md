# Simple PHP Blog

为 [jkjoy/Simple-PHP-Blog](https://github.com/jkjoy/Simple-PHP-Blog) 提供 Alpine 3.22 + PHP 8.4 CLI 镜像。只安装预编译的 PHP 运行包，包含 SQLite、cURL、ZIP、fileinfo、mbstring、session、OpenSSL 和 OPcache。

通过 `php -S 0.0.0.0:8080 -t /app /app/router.php` 启动，使用项目自带路由处理伪静态、静态资源和私有目录保护。PHP 以 `www-data` 身份运行，`tini` 负责信号转发和子进程回收。

## 启动

```bash
docker compose -f simple-php-blog/compose.yaml up -d
```

访问 `http://localhost:8080/install.php`，设置站点信息和管理员账号。安装前后健康检查均可使用。

镜像：`jkjoy/simple-php-blog:latest`（也提供 `alpine` 标签）。也可使用版本标签（例如 `v1.14.15`）或 `source-<完整源码提交 SHA>`。

从旧 Apache 镜像升级时，将端口映射从 `8080:80` 改为 `8080:8080`。

从旧版镜像升级时，将五个数据卷的容器挂载目标从 `/var/www/html/…` 改为 `/app/…`，保留原有卷名或宿主机目录。使用本仓库更新后的 Compose 重建容器即可复用同名数据卷，启动时会设置目录所有者。

默认 `PHP_CLI_SERVER_WORKERS=4`，增加四个工作进程，避免单个慢请求阻塞所有访问。可通过同名环境变量调整（至少为 `2`）。PHP 内置服务器适合个人博客和低流量部署；高并发或复杂 HTTP 服务需求建议使用 PHP-FPM 配合专用 Web 服务器。

## 持久化

Compose 为以下五个目录创建命名卷，重建容器后保留内容：

| 容器目录 | 内容 |
| --- | --- |
| `/app/data` | SQLite 数据库、配置、安装锁 |
| `/app/cache` | 设置缓存、限流和重置令牌等运行数据 |
| `/app/uploads` | 上传附件 |
| `/app/themes` | 自定义主题 |
| `/app/plugins` | 插件和语言包 |

容器启动时自动设置以上目录的 `www-data`（UID/GID `82`）写入权限，并为首次挂载的空插件目录补充内置语言包。绑定宿主机目录时也使用相同的容器路径；目录需支持修改所有者。备份时同时保存这五个目录，勿执行 `docker compose down -v` 删除数据卷。

通过镜像升级核心程序：

```bash
docker compose -f simple-php-blog/compose.yaml pull
docker compose -f simple-php-blog/compose.yaml up -d
```

后台安装的主题和插件保存在卷中。后台一键更新核心程序只影响当前容器，重建后以所用镜像的版本为准。

## 本地构建与测试

```bash
docker build -t simple-php-blog:local ./simple-php-blog
bash simple-php-blog/test.sh simple-php-blog:local
```

默认源码固定为本地博客当前版本 `v1.14.15`（`bd79a54eaf5967d41ea2178f88ba2ac3b1076638`）。可通过 `--build-arg BLOG_REF=<源码提交或标签>` 指定其他版本。构建仅从上游仓库获取程序文件，不包含本机博客的数据库、配置、缓存或上传内容。

## GitHub Actions

[工作流](../.github/workflows/simple-php-blog.yml) 在此目录或工作流变动时触发，也支持手动填写上游 `blog_ref`（默认 `main`）。工作流先解析为固定提交，再分别在 amd64、arm64 原生 runner 上构建并测试安装、登录会话、静态资源、伪静态、私有目录保护、进程退出和重建后的数据保留，同时检查镜像解压体积不超过 100 MiB；全部通过后才发布双架构镜像，并从仓库拉取摘要对应镜像验证源码版本，记录各平台压缩体积。

推送至本仓库 `main` 或在 `main` 手动运行时发布至 Docker Hub。拉取请求仅执行构建和测试。使用仓库现有 Secrets：`DOCKER_USER_NAME`、`DOCKER_ACCESS_TOKEN`。

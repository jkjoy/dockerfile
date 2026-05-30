# Gomail Suite

Go + SQLite 实现的 SMTP 发信、IP 归属地查询、二维码生成一体化 API 服务。后台接口用于配置 SMTP、管理 API Key、查看发信日志，并提供 IP/QR 工具页；业务接口统一使用带权限的 API Key。

## 启动

```powershell
$env:ADMIN_TOKEN="change-me-admin-token"
$env:GOADMIN_USER="admin"
$env:GOADMIN_PASSWORD="change-me-goadmin-password"
$env:ADDR=":8080"
$env:DB_PATH="gomail.db"
go run .
```

Docker 构建需要在仓库根目录执行，因为镜像会复制 `ip/data` 到 `/app/data`：

```bash
docker build -f gomail/Dockerfile -t gomail-suite .
docker run -d \
  --name gomail-suite \
  -p 8080:8080 \
  -e ADMIN_TOKEN=change-me-admin-token \
  -e GOADMIN_USER=admin \
  -e GOADMIN_PASSWORD=change-me-goadmin-password \
  -e DB_PATH=/data/gomail.db \
  -v gomail-data:/data \
  gomail-suite
```

环境变量：

- `ADMIN_TOKEN`：管理员令牌，必填。
- `GOADMIN_USER`：GoAdmin 后台初始管理员账号，默认 `admin`，只在首次初始化后台用户时使用。
- `GOADMIN_PASSWORD`：GoAdmin 后台初始管理员密码，默认 `admin`，只在首次初始化后台用户时使用。
- `ADDR`：监听地址，默认 `:8080`。
- `DB_PATH`：SQLite 文件路径，默认 `gomail.db`。
- `CITY_DB_PATH`：MaxMind City 数据库路径；未设置时依次查找 `./data/GeoLite2-City.mmdb`、`../ip/data/GeoLite2-City.mmdb`、`./ip/data/GeoLite2-City.mmdb`。
- `ASN_DB_PATH`：MaxMind ASN 数据库路径，可选。
- `QQWRY_PATH`：纯真 IP 库路径，可选。

后台地址：

- API 调用文档首页：`http://localhost:8080/`
- `http://localhost:8080/admin`
- SMTP 配置与测试：`/admin/tools/smtp`
- API Key 管理：`/admin/tools/api-keys`
- 发信日志：`/admin/info/email_logs`
- IP 查询工具：`/admin/tools/ip`
- 二维码工具：`/admin/tools/qrcode`

## 权限设计

- 后台管理接口：`Authorization: Bearer <ADMIN_TOKEN>` 或 `X-Admin-Token: <ADMIN_TOKEN>`。
- GoAdmin 后台：使用 GoAdmin 登录账号密码，和 `ADMIN_TOKEN` 分开。
- 业务接口：`Authorization: Bearer <API_KEY>`。
- API Key 只保存 SHA-256 哈希，创建时只返回一次明文。
- 当前支持权限：`send:mail`、`lookup:ip`、`generate:qrcode`。
- 创建 API Key 时如果不传 `permissions`，默认只授予 `send:mail`。
- SMTP 密码不会在查询接口返回，只返回 `password_set`。
- GoAdmin 后台中的 API Key 表为只读；创建和撤销 Key 仍通过后台管理 API，避免在后台表单里直接写入无效哈希或泄露明文。

## 路由分配

| 路由 | 方法 | 权限 | 说明 |
|---|---:|---|---|
| `/healthz` | GET | 无 | 健康检查 |
| `/admin` | GET | GoAdmin 登录 | 后台仪表盘 |
| `/api/admin/smtp` | GET/PUT | `ADMIN_TOKEN` | SMTP 配置 |
| `/api/admin/test-email` | POST | `ADMIN_TOKEN` | 发送测试邮件 |
| `/api/admin/api-keys` | GET/POST | `ADMIN_TOKEN` | API Key 管理 |
| `/api/admin/api-keys/{id}` | DELETE | `ADMIN_TOKEN` | 撤销 API Key |
| `/api/admin/ip/lookup?ip=8.8.8.8` | GET | `ADMIN_TOKEN` | 后台 IP 查询接口 |
| `/api/admin/qrcode` | GET/POST | `ADMIN_TOKEN` | 后台二维码生成接口 |
| `/api/v1/mail/send` | POST | `send:mail` | 发信 |
| `/api/v1/ip/{ip}` | GET | `lookup:ip` | 查询指定 IP |
| `/api/v1/ip/me` | GET | `lookup:ip` | 查询调用方 IP |
| `/api/v1/qrcode` | GET/POST | `generate:qrcode` | 生成二维码图片 |

## 后台接口

### 设置 SMTP

`PUT /api/admin/smtp`

```json
{
  "host": "smtp.example.com",
  "port": 587,
  "username": "user@example.com",
  "password": "smtp-password",
  "from_email": "user@example.com",
  "from_name": "Gomail",
  "encryption": "starttls"
}
```

`encryption` 可选值：

- `starttls`：常用于 587。
- `tls`：常用于 465。
- `none`：不加密。

更新 SMTP 时如果不传 `password`，会保留旧密码。
如果 `username` 为空，则允许不配置密码，适合无需认证的 SMTP relay。

也可以在 GoAdmin 后台的 `SMTP Config` 页面维护 SMTP 配置并发送测试邮件。编辑时密码框留空表示保留旧密码。

### 查看 SMTP

`GET /api/admin/smtp`

### 创建发信 Key

`POST /api/admin/api-keys`

```json
{
  "name": "production-app",
  "permissions": ["send:mail", "lookup:ip", "generate:qrcode"],
  "expires_at": "2026-12-31T00:00:00Z"
}
```

响应中的 `key` 是唯一一次返回的明文 API Key。

### 查看 Key

`GET /api/admin/api-keys`

### 撤销 Key

`DELETE /api/admin/api-keys/{id}`

### 发送测试邮件

`POST /api/admin/test-email`

```json
{
  "to": ["receiver@example.com"],
  "subject": "SMTP test",
  "text": "hello"
}
```

## 发信接口

`POST /api/v1/mail/send`

```json
{
  "to": ["to@example.com"],
  "cc": ["cc@example.com"],
  "bcc": ["bcc@example.com"],
  "subject": "Hello",
  "text": "Plain text body",
  "html": "<p>HTML body</p>",
  "reply_to": "support@example.com"
}
```

规则：

- `to`、`cc`、`bcc` 至少要有一个收件人。
- 单次最多 100 个收件人。
- `subject` 必填。
- `text` 和 `html` 至少填一个；两者都填时发送 `multipart/alternative`。
- 发件人固定使用后台 SMTP 配置里的 `from_email` 和 `from_name`，避免调用方伪造发件人。

示例：

```powershell
curl.exe -X POST http://localhost:8080/api/v1/mail/send `
  -H "Authorization: Bearer gm_xxx" `
  -H "Content-Type: application/json" `
  -d '{"to":["to@example.com"],"subject":"Hello","text":"Hi"}'
```

## IP 查询接口

`GET /api/v1/ip/{ip}` 或 `GET /api/v1/ip?ip={ip}`

```powershell
curl.exe http://localhost:8080/api/v1/ip/8.8.8.8 `
  -H "Authorization: Bearer gm_xxx"
```

响应示例：

```json
{
  "ip": "8.8.8.8",
  "country": "美国",
  "country_code": "US",
  "city": "山景城",
  "isp": "Google LLC",
  "latitude": 37.4056,
  "longitude": -122.0775,
  "source": "MaxMind"
}
```

## 二维码接口

`GET /api/v1/qrcode?content=Hello&size=512&format=png`

参数：

- `content`：二维码内容，必填。
- `size`：图片尺寸，默认 `256`，范围 `64` 到 `2048`。
- `format`：`png` 或 `svg`，默认 `png`。
- `fg_color`：前景色，默认 `#000000`。
- `bg_color`：背景色，默认 `#FFFFFF`。

```powershell
curl.exe "http://localhost:8080/api/v1/qrcode?content=https://example.com&size=512&format=png" `
  -H "Authorization: Bearer gm_xxx" `
  -o qrcode.png
```

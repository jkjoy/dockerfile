package main

import (
	htmltmpl "html/template"
	"net/http"
	"strings"
)

func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(strings.ReplaceAll(apiDocsHTML, "{{BASE_URL}}", htmltmpl.HTMLEscapeString(publicBaseURL(r)))))
}

func publicBaseURL(r *http.Request) string {
	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	if host == "" {
		host = "localhost:8080"
	}
	return scheme + "://" + host
}

const apiDocsHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Gomail Suite API Docs</title>
  <style>
    :root {
      --bg: #f6f7f9;
      --panel: #ffffff;
      --ink: #17202a;
      --muted: #667085;
      --line: #d9dee7;
      --soft-line: #edf0f4;
      --accent: #0f766e;
      --accent-ink: #0b4f49;
      --code-bg: #101820;
      --code-ink: #dce8ef;
      --warn-bg: #fff7ed;
      --warn-ink: #9a3412;
      --radius: 8px;
      --shadow: 0 8px 24px rgba(16, 24, 40, .08);
    }

    * { box-sizing: border-box; }

    html { scroll-behavior: smooth; }

    body {
      margin: 0;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", Arial, sans-serif;
      color: var(--ink);
      background: var(--bg);
      line-height: 1.55;
      letter-spacing: 0;
    }

    a { color: var(--accent); text-decoration: none; }
    a:hover { color: var(--accent-ink); text-decoration: underline; }

    .shell {
      display: grid;
      grid-template-columns: 260px minmax(0, 1fr);
      min-height: 100vh;
    }

    .sidebar {
      position: sticky;
      top: 0;
      height: 100vh;
      padding: 28px 22px;
      background: #ffffff;
      border-right: 1px solid var(--line);
      overflow-y: auto;
    }

    .brand {
      margin-bottom: 24px;
      padding-bottom: 18px;
      border-bottom: 1px solid var(--soft-line);
    }

    .brand strong {
      display: block;
      font-size: 18px;
      line-height: 1.2;
    }

    .brand span {
      display: block;
      margin-top: 6px;
      color: var(--muted);
      font-size: 13px;
    }

    .nav-title {
      margin: 18px 0 8px;
      color: var(--muted);
      font-size: 11px;
      font-weight: 700;
      text-transform: uppercase;
    }

    .nav a {
      display: block;
      padding: 7px 0;
      color: #344054;
      font-size: 14px;
    }

    .nav a:hover { color: var(--accent); }

    .main {
      max-width: 1120px;
      width: 100%;
      padding: 34px 34px 72px;
    }

    .topbar {
      display: flex;
      gap: 12px;
      align-items: center;
      justify-content: space-between;
      margin-bottom: 20px;
    }

    .base-url {
      min-width: 0;
      display: flex;
      align-items: center;
      gap: 10px;
      padding: 9px 12px;
      border: 1px solid var(--line);
      border-radius: var(--radius);
      background: var(--panel);
      color: var(--muted);
      font-size: 13px;
    }

    .base-url code {
      color: var(--ink);
      overflow-wrap: anywhere;
    }

    .button-row {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
    }

    .btn {
      display: inline-flex;
      align-items: center;
      min-height: 38px;
      padding: 8px 12px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: var(--panel);
      color: var(--ink);
      font-size: 14px;
      font-weight: 600;
      text-decoration: none;
    }

    .btn.primary {
      border-color: var(--accent);
      background: var(--accent);
      color: #fff;
    }

    .btn:hover { text-decoration: none; border-color: var(--accent); }
    .btn.primary:hover { color: #fff; background: var(--accent-ink); }

    .hero {
      padding: 28px;
      border: 1px solid var(--line);
      border-radius: var(--radius);
      background: var(--panel);
      box-shadow: var(--shadow);
    }

    h1 {
      margin: 0;
      font-size: 34px;
      line-height: 1.2;
      letter-spacing: 0;
    }

    .lead {
      max-width: 780px;
      margin: 12px 0 0;
      color: var(--muted);
      font-size: 16px;
    }

    .quick-grid {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 12px;
      margin-top: 22px;
    }

    .metric {
      padding: 14px;
      border: 1px solid var(--soft-line);
      border-radius: var(--radius);
      background: #fbfcfe;
    }

    .metric strong {
      display: block;
      font-size: 18px;
    }

    .metric span {
      display: block;
      margin-top: 4px;
      color: var(--muted);
      font-size: 13px;
    }

    section {
      margin-top: 26px;
      padding: 24px;
      border: 1px solid var(--line);
      border-radius: var(--radius);
      background: var(--panel);
    }

    h2 {
      margin: 0 0 14px;
      font-size: 24px;
      line-height: 1.25;
      letter-spacing: 0;
    }

    h3 {
      margin: 22px 0 10px;
      font-size: 17px;
      line-height: 1.3;
      letter-spacing: 0;
    }

    p { margin: 8px 0; }

    .note {
      padding: 12px 14px;
      border: 1px solid #fed7aa;
      border-radius: var(--radius);
      background: var(--warn-bg);
      color: var(--warn-ink);
      font-size: 14px;
    }

    .table-wrap {
      overflow-x: auto;
      border: 1px solid var(--soft-line);
      border-radius: var(--radius);
    }

    table {
      width: 100%;
      border-collapse: collapse;
      font-size: 14px;
      background: var(--panel);
    }

    th, td {
      padding: 11px 12px;
      border-bottom: 1px solid var(--soft-line);
      text-align: left;
      vertical-align: top;
    }

    th {
      background: #f8fafc;
      color: #344054;
      font-weight: 700;
      white-space: nowrap;
    }

    tr:last-child td { border-bottom: 0; }
    td code, p code, li code {
      padding: 2px 5px;
      border-radius: 4px;
      background: #eef2f6;
      color: #263445;
      font-size: 13px;
    }

    .endpoint {
      margin-top: 16px;
      padding-top: 16px;
      border-top: 1px solid var(--soft-line);
    }

    .endpoint:first-of-type {
      padding-top: 0;
      border-top: 0;
    }

    .route {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      align-items: center;
      margin-bottom: 8px;
    }

    .method {
      min-width: 58px;
      padding: 3px 7px;
      border-radius: 4px;
      background: #e6f4f1;
      color: var(--accent-ink);
      font-size: 12px;
      font-weight: 800;
      text-align: center;
    }

    .method.post { background: #eef4ff; color: #1d4ed8; }
    .method.put { background: #fef3c7; color: #92400e; }
    .method.delete { background: #fee2e2; color: #b91c1c; }

    .path {
      font-family: Menlo, Consolas, "Liberation Mono", monospace;
      font-size: 14px;
      overflow-wrap: anywhere;
    }

    .permission {
      margin-left: auto;
      color: var(--muted);
      font-size: 13px;
    }

    .codebox {
      position: relative;
      margin: 10px 0 14px;
    }

    pre {
      margin: 0;
      padding: 14px;
      border-radius: var(--radius);
      background: var(--code-bg);
      color: var(--code-ink);
      overflow-x: auto;
      font-size: 13px;
      line-height: 1.5;
    }

    .copy {
      position: absolute;
      top: 8px;
      right: 8px;
      padding: 5px 8px;
      border: 1px solid rgba(255,255,255,.2);
      border-radius: 5px;
      background: rgba(255,255,255,.08);
      color: #fff;
      cursor: pointer;
      font-size: 12px;
    }

    .copy:hover { background: rgba(255,255,255,.16); }

    .two-col {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 14px;
    }

    footer {
      margin-top: 26px;
      color: var(--muted);
      font-size: 13px;
    }

    @media (max-width: 960px) {
      .shell { display: block; }
      .sidebar {
        position: relative;
        height: auto;
        border-right: 0;
        border-bottom: 1px solid var(--line);
      }
      .nav {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        gap: 0 14px;
      }
      .main { padding: 24px 18px 54px; }
      .topbar { align-items: stretch; flex-direction: column; }
      .quick-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
      .two-col { grid-template-columns: 1fr; }
      .permission { width: 100%; margin-left: 66px; }
    }

    @media (max-width: 560px) {
      h1 { font-size: 28px; }
      section, .hero { padding: 18px; }
      .quick-grid { grid-template-columns: 1fr; }
      .nav { grid-template-columns: 1fr; }
      th, td { padding: 10px; }
    }
  </style>
</head>
<body>
  <div class="shell">
    <aside class="sidebar" aria-label="文档目录">
      <div class="brand">
        <strong>Gomail Suite</strong>
        <span>Mail + IP + QR Code API</span>
      </div>
      <nav class="nav">
        <div class="nav-title">Start</div>
        <a href="#overview">概览</a>
        <a href="#auth">认证方式</a>
        <a href="#routes">路由总览</a>
        <a href="#errors">错误格式</a>
        <div class="nav-title">Admin API</div>
        <a href="#admin-smtp">SMTP 配置</a>
        <a href="#admin-keys">API Key 管理</a>
        <a href="#admin-tools">后台工具接口</a>
        <div class="nav-title">Business API</div>
        <a href="#mail-send">发送邮件</a>
        <a href="#ip-lookup">IP 查询</a>
        <a href="#qrcode">二维码生成</a>
      </nav>
    </aside>

    <main class="main">
      <div class="topbar">
        <div class="base-url">Base URL <code>{{BASE_URL}}</code></div>
        <div class="button-row">
          <a class="btn primary" href="/admin">进入后台</a>
          <a class="btn" href="/healthz">健康检查</a>
        </div>
      </div>

      <header class="hero" id="overview">
        <h1>Gomail Suite API 调用文档</h1>
        <p class="lead">这个服务把 SMTP 发信、IP 归属地查询、二维码生成整合到同一个 HTTP API。后台接口使用管理员令牌，业务接口使用带权限的 API Key。</p>
        <div class="quick-grid">
          <div class="metric"><strong>SMTP</strong><span>配置、测试、发信</span></div>
          <div class="metric"><strong>API Keys</strong><span>哈希存储，只返回一次明文</span></div>
          <div class="metric"><strong>IP Lookup</strong><span>MaxMind + QQWry</span></div>
          <div class="metric"><strong>QR Code</strong><span>PNG / SVG 输出</span></div>
        </div>
      </header>

      <section id="auth">
        <h2>认证方式</h2>
        <div class="two-col">
          <div>
            <h3>后台管理接口</h3>
            <p>用于配置 SMTP、创建和撤销 API Key、后台测试邮件。任选一种 Header：</p>
            <div class="codebox"><button class="copy" type="button">复制</button><pre>Authorization: Bearer &lt;ADMIN_TOKEN&gt;
X-Admin-Token: &lt;ADMIN_TOKEN&gt;</pre></div>
          </div>
          <div>
            <h3>业务调用接口</h3>
            <p>用于发信、IP 查询、二维码生成。必须使用创建 API Key 时返回的明文 Key：</p>
            <div class="codebox"><button class="copy" type="button">复制</button><pre>Authorization: Bearer &lt;API_KEY&gt;</pre></div>
          </div>
        </div>
        <div class="note">API Key 明文只在创建时返回一次；数据库只保存 SHA-256 哈希。请在创建后立即保存明文 Key。</div>
      </section>

      <section id="routes">
        <h2>路由总览</h2>
        <div class="table-wrap">
          <table>
            <thead><tr><th>方法</th><th>路径</th><th>认证</th><th>说明</th></tr></thead>
            <tbody>
              <tr><td>GET</td><td><code>/healthz</code></td><td>无</td><td>服务健康检查</td></tr>
              <tr><td>GET</td><td><code>/api/admin/smtp</code></td><td>ADMIN_TOKEN</td><td>读取 SMTP 配置，不返回密码明文</td></tr>
              <tr><td>PUT</td><td><code>/api/admin/smtp</code></td><td>ADMIN_TOKEN</td><td>保存 SMTP 配置</td></tr>
              <tr><td>POST</td><td><code>/api/admin/test-email</code></td><td>ADMIN_TOKEN</td><td>用当前 SMTP 配置发送测试邮件</td></tr>
              <tr><td>GET</td><td><code>/api/admin/api-keys</code></td><td>ADMIN_TOKEN</td><td>查看 API Key 列表</td></tr>
              <tr><td>POST</td><td><code>/api/admin/api-keys</code></td><td>ADMIN_TOKEN</td><td>创建 API Key</td></tr>
              <tr><td>DELETE</td><td><code>/api/admin/api-keys/{id}</code></td><td>ADMIN_TOKEN</td><td>撤销 API Key</td></tr>
              <tr><td>POST</td><td><code>/api/v1/mail/send</code></td><td>send:mail</td><td>发送邮件</td></tr>
              <tr><td>GET</td><td><code>/api/v1/ip/{ip}</code></td><td>lookup:ip</td><td>查询指定 IP</td></tr>
              <tr><td>GET</td><td><code>/api/v1/ip/me</code></td><td>lookup:ip</td><td>查询调用方 IP</td></tr>
              <tr><td>GET / POST</td><td><code>/api/v1/qrcode</code></td><td>generate:qrcode</td><td>生成 PNG 或 SVG 二维码</td></tr>
            </tbody>
          </table>
        </div>
      </section>

      <section id="errors">
        <h2>错误格式</h2>
        <p>JSON 接口失败时统一返回：</p>
        <div class="codebox"><button class="copy" type="button">复制</button><pre>{
  "error": "invalid api key"
}</pre></div>
        <p>SMTP 保存、测试或发信失败时会额外返回 <code>status</code>、<code>kind</code>、<code>hint</code>，用于判断是 DNS、端口、加密、认证还是投递限制问题。</p>
        <div class="codebox"><button class="copy" type="button">复制</button><pre>{
  "error": "smtp delivery failed: 535 5.7.8 authentication failed",
  "status": 502,
  "kind": "auth",
  "hint": "SMTP authentication failed. Check the username, password, and app password or authorization code."
}</pre></div>
        <div class="table-wrap">
          <table>
            <thead><tr><th>状态码</th><th>场景</th></tr></thead>
            <tbody>
              <tr><td>400</td><td>参数格式错误，例如邮箱地址、IP、二维码颜色不合法</td></tr>
              <tr><td>401</td><td>缺少或错误的管理员令牌 / API Key</td></tr>
              <tr><td>403</td><td>API Key 缺少接口所需权限</td></tr>
              <tr><td>404</td><td>SMTP 未配置或撤销的 Key 不存在</td></tr>
              <tr><td>409</td><td>发信前 SMTP 尚未配置</td></tr>
              <tr><td>502</td><td>SMTP 服务连接、认证或投递失败</td></tr>
            </tbody>
          </table>
        </div>
      </section>

      <section id="admin-smtp">
        <h2>后台接口：SMTP 配置</h2>
        <div class="endpoint">
          <div class="route"><span class="method">GET</span><span class="path">/api/admin/smtp</span><span class="permission">ADMIN_TOKEN</span></div>
          <p>读取当前 SMTP 配置。响应不会包含密码明文，只返回 <code>password_set</code>。</p>
          <div class="codebox"><button class="copy" type="button">复制</button><pre>curl -H "Authorization: Bearer &lt;ADMIN_TOKEN&gt;" \
  "{{BASE_URL}}/api/admin/smtp"</pre></div>
        </div>

        <div class="endpoint">
          <div class="route"><span class="method put">PUT</span><span class="path">/api/admin/smtp</span><span class="permission">ADMIN_TOKEN</span></div>
          <p>保存 SMTP 配置。更新时不传 <code>password</code> 会保留旧密码。</p>
          <div class="table-wrap">
            <table>
              <thead><tr><th>字段</th><th>类型</th><th>说明</th></tr></thead>
              <tbody>
                <tr><td><code>host</code></td><td>string</td><td>SMTP 主机，必填</td></tr>
                <tr><td><code>port</code></td><td>number</td><td>1 到 65535，常用 587 或 465</td></tr>
                <tr><td><code>username</code></td><td>string</td><td>SMTP 用户名；为空时允许无认证 relay</td></tr>
                <tr><td><code>password</code></td><td>string</td><td>SMTP 密码；更新时可省略</td></tr>
                <tr><td><code>from_email</code></td><td>string</td><td>发件邮箱，必须是邮箱地址</td></tr>
                <tr><td><code>from_name</code></td><td>string</td><td>发件人显示名</td></tr>
                <tr><td><code>encryption</code></td><td>string</td><td><code>starttls</code>、<code>tls</code>、<code>none</code>。<code>tls</code> 对应常说的 SSL/SMTPS，通常端口 465；<code>starttls</code> 通常端口 587；<code>none</code> 只建议内网 relay。</td></tr>
              </tbody>
            </table>
          </div>
          <div class="codebox"><button class="copy" type="button">复制</button><pre>curl -X PUT "{{BASE_URL}}/api/admin/smtp" \
  -H "Authorization: Bearer &lt;ADMIN_TOKEN&gt;" \
  -H "Content-Type: application/json" \
  -d '{
    "host": "smtp.example.com",
    "port": 587,
    "username": "sender@example.com",
    "password": "smtp-password",
    "from_email": "sender@example.com",
    "from_name": "Gomail",
    "encryption": "starttls"
  }'</pre></div>
        </div>

        <div class="endpoint">
          <div class="route"><span class="method post">POST</span><span class="path">/api/admin/test-email</span><span class="permission">ADMIN_TOKEN</span></div>
          <p>使用当前 SMTP 配置发送测试邮件。</p>
          <div class="codebox"><button class="copy" type="button">复制</button><pre>curl -X POST "{{BASE_URL}}/api/admin/test-email" \
  -H "Authorization: Bearer &lt;ADMIN_TOKEN&gt;" \
  -H "Content-Type: application/json" \
  -d '{
    "to": ["receiver@example.com"],
    "subject": "SMTP test",
    "text": "hello from Gomail"
  }'</pre></div>
        </div>
      </section>

      <section id="admin-keys">
        <h2>后台接口：API Key 管理</h2>
        <div class="table-wrap">
          <table>
            <thead><tr><th>权限</th><th>可调用接口</th></tr></thead>
            <tbody>
              <tr><td><code>send:mail</code></td><td><code>POST /api/v1/mail/send</code></td></tr>
              <tr><td><code>lookup:ip</code></td><td><code>GET /api/v1/ip/{ip}</code>、<code>GET /api/v1/ip/me</code></td></tr>
              <tr><td><code>generate:qrcode</code></td><td><code>GET /api/v1/qrcode</code>、<code>POST /api/v1/qrcode</code></td></tr>
            </tbody>
          </table>
        </div>

        <div class="endpoint">
          <div class="route"><span class="method post">POST</span><span class="path">/api/admin/api-keys</span><span class="permission">ADMIN_TOKEN</span></div>
          <p>创建 API Key。<code>expires_at</code> 可选，必须是 RFC3339 时间。明文 <code>key</code> 只返回一次。</p>
          <div class="codebox"><button class="copy" type="button">复制</button><pre>curl -X POST "{{BASE_URL}}/api/admin/api-keys" \
  -H "Authorization: Bearer &lt;ADMIN_TOKEN&gt;" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "production-app",
    "permissions": ["send:mail", "lookup:ip", "generate:qrcode"],
    "expires_at": "2026-12-31T00:00:00Z"
  }'</pre></div>
          <div class="codebox"><button class="copy" type="button">复制</button><pre>{
  "api_key": {
    "id": 1,
    "name": "production-app",
    "prefix": "gm_xxxxxxxx",
    "permissions": ["send:mail", "lookup:ip", "generate:qrcode"],
    "created_at": "2026-05-30T00:00:00Z",
    "expires_at": "2026-12-31T00:00:00Z"
  },
  "key": "gm_full_plain_key_returned_once"
}</pre></div>
        </div>

        <div class="endpoint">
          <div class="route"><span class="method">GET</span><span class="path">/api/admin/api-keys</span><span class="permission">ADMIN_TOKEN</span></div>
          <p>列出所有 API Key 的元数据，不返回明文 Key。</p>
        </div>

        <div class="endpoint">
          <div class="route"><span class="method delete">DELETE</span><span class="path">/api/admin/api-keys/{id}</span><span class="permission">ADMIN_TOKEN</span></div>
          <p>撤销一个仍处于 active 状态的 API Key。</p>
          <div class="codebox"><button class="copy" type="button">复制</button><pre>curl -X DELETE "{{BASE_URL}}/api/admin/api-keys/1" \
  -H "Authorization: Bearer &lt;ADMIN_TOKEN&gt;"</pre></div>
        </div>
      </section>

      <section id="admin-tools">
        <h2>后台工具接口</h2>
        <p>这些接口使用管理员令牌，方便后台或脚本直接测试 IP 查询和二维码生成。</p>
        <div class="endpoint">
          <div class="route"><span class="method">GET</span><span class="path">/api/admin/ip/lookup?ip=8.8.8.8</span><span class="permission">ADMIN_TOKEN</span></div>
        </div>
        <div class="endpoint">
          <div class="route"><span class="method">GET</span><span class="path">/api/admin/qrcode?content=Hello&amp;size=512&amp;format=png</span><span class="permission">ADMIN_TOKEN</span></div>
          <p>响应是图片二进制，<code>Content-Type</code> 为 <code>image/png</code> 或 <code>image/svg+xml</code>。</p>
        </div>
      </section>

      <section id="mail-send">
        <h2>业务接口：发送邮件</h2>
        <div class="endpoint">
          <div class="route"><span class="method post">POST</span><span class="path">/api/v1/mail/send</span><span class="permission">send:mail</span></div>
          <p>调用方不能指定发件人，发件人固定使用 SMTP 配置里的 <code>from_email</code> 和 <code>from_name</code>。</p>
          <div class="table-wrap">
            <table>
              <thead><tr><th>字段</th><th>类型</th><th>说明</th></tr></thead>
              <tbody>
                <tr><td><code>to</code></td><td>string[]</td><td>主收件人</td></tr>
                <tr><td><code>cc</code></td><td>string[]</td><td>抄送，可选</td></tr>
                <tr><td><code>bcc</code></td><td>string[]</td><td>密送，可选，不会写入邮件头</td></tr>
                <tr><td><code>subject</code></td><td>string</td><td>必填</td></tr>
                <tr><td><code>text</code></td><td>string</td><td>纯文本正文；<code>text</code> 和 <code>html</code> 至少一个</td></tr>
                <tr><td><code>html</code></td><td>string</td><td>HTML 正文；和 <code>text</code> 同时存在时发送 multipart/alternative</td></tr>
                <tr><td><code>reply_to</code></td><td>string</td><td>回复地址，可选</td></tr>
              </tbody>
            </table>
          </div>
          <div class="codebox"><button class="copy" type="button">复制</button><pre>curl -X POST "{{BASE_URL}}/api/v1/mail/send" \
  -H "Authorization: Bearer &lt;API_KEY&gt;" \
  -H "Content-Type: application/json" \
  -d '{
    "to": ["to@example.com"],
    "cc": ["cc@example.com"],
    "bcc": ["hidden@example.com"],
    "subject": "Hello",
    "text": "Plain text body",
    "html": "&lt;p&gt;HTML body&lt;/p&gt;",
    "reply_to": "support@example.com"
  }'</pre></div>
          <div class="codebox"><button class="copy" type="button">复制</button><pre>{
  "status": "sent",
  "recipients": 3
}</pre></div>
        </div>
      </section>

      <section id="ip-lookup">
        <h2>业务接口：IP 查询</h2>
        <div class="endpoint">
          <div class="route"><span class="method">GET</span><span class="path">/api/v1/ip/{ip}</span><span class="permission">lookup:ip</span></div>
          <div class="route"><span class="method">GET</span><span class="path">/api/v1/ip?ip={ip}</span><span class="permission">lookup:ip</span></div>
          <p>查询指定 IP 的国家、省份、城市、ISP、经纬度和数据源。</p>
          <div class="codebox"><button class="copy" type="button">复制</button><pre>curl "{{BASE_URL}}/api/v1/ip/8.8.8.8" \
  -H "Authorization: Bearer &lt;API_KEY&gt;"</pre></div>
          <div class="codebox"><button class="copy" type="button">复制</button><pre>{
  "ip": "8.8.8.8",
  "country": "美国",
  "country_code": "US",
  "province": "",
  "region": "",
  "city": "",
  "isp": "谷歌",
  "latitude": 37.751,
  "longitude": -97.822,
  "source": "MaxMind"
}</pre></div>
        </div>
        <div class="endpoint">
          <div class="route"><span class="method">GET</span><span class="path">/api/v1/ip/me</span><span class="permission">lookup:ip</span></div>
          <p>按 <code>CF-Connecting-IP</code>、<code>X-Real-IP</code>、<code>X-Forwarded-For</code>、连接地址的顺序识别调用方 IP。</p>
        </div>
      </section>

      <section id="qrcode">
        <h2>业务接口：二维码生成</h2>
        <div class="endpoint">
          <div class="route"><span class="method">GET</span><span class="path">/api/v1/qrcode</span><span class="permission">generate:qrcode</span></div>
          <div class="route"><span class="method post">POST</span><span class="path">/api/v1/qrcode</span><span class="permission">generate:qrcode</span></div>
          <p>返回二维码图片二进制。GET 使用 query 参数；POST 可以提交 JSON 或表单。</p>
          <div class="table-wrap">
            <table>
              <thead><tr><th>参数</th><th>默认值</th><th>说明</th></tr></thead>
              <tbody>
                <tr><td><code>content</code></td><td>无</td><td>二维码内容，必填，最大 4096 字节</td></tr>
                <tr><td><code>size</code></td><td>256</td><td>图片尺寸，范围 64 到 2048</td></tr>
                <tr><td><code>format</code></td><td>png</td><td><code>png</code> 或 <code>svg</code></td></tr>
                <tr><td><code>fg_color</code></td><td>#000000</td><td>前景色，6 位十六进制颜色</td></tr>
                <tr><td><code>bg_color</code></td><td>#FFFFFF</td><td>背景色，6 位十六进制颜色</td></tr>
              </tbody>
            </table>
          </div>
          <div class="codebox"><button class="copy" type="button">复制</button><pre>curl "{{BASE_URL}}/api/v1/qrcode?content=https%3A%2F%2Fexample.com&amp;size=512&amp;format=png" \
  -H "Authorization: Bearer &lt;API_KEY&gt;" \
  -o qrcode.png</pre></div>
          <div class="codebox"><button class="copy" type="button">复制</button><pre>curl -X POST "{{BASE_URL}}/api/v1/qrcode" \
  -H "Authorization: Bearer &lt;API_KEY&gt;" \
  -H "Content-Type: application/json" \
  -d '{
    "content": "https://example.com",
    "size": 512,
    "format": "svg",
    "fg_color": "#111827",
    "bg_color": "#FFFFFF"
  }' \
  -o qrcode.svg</pre></div>
        </div>
      </section>

      <footer>
        <p>后台页面：<a href="/admin">/admin</a>。后台工具页提供 SMTP 测试、API Key 管理、IP 查询和二维码预览。</p>
      </footer>
    </main>
  </div>

  <script>
    document.querySelectorAll('.copy').forEach(function(button) {
      button.addEventListener('click', function() {
        var pre = button.parentElement.querySelector('pre');
        if (!pre) return;
        navigator.clipboard.writeText(pre.innerText).then(function() {
          var old = button.textContent;
          button.textContent = '已复制';
          setTimeout(function() { button.textContent = old; }, 1200);
        });
      });
    });
  </script>
</body>
</html>`

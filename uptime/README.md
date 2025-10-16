# uptime-cert-service

提供两个 HTTP 接口：
- `GET /uptime` - 返回宿主机与容器的开机/运行信息（JSON）。
- `GET /cert?host=example.com[:port]` - 返回指定域名证书到期信息（JSON）。

## 构建镜像
```bash
docker build -t uptime-cert:latest .

#!/usr/bin/env sh
set -e

# 将容器启动时间写入文件，供 /uptime 的 container_uptime 使用
echo "$(date +%s)" > /tmp/container_start_time

# 激活虚拟环境不是必须（镜像里用全局安装）
# 启动 gunicorn
exec gunicorn -w 3 -b 0.0.0.0:5000 app:app

#!/bin/sh
set -e  # 遇到错误立即退出

if [ -d "/app" ] && [ -z "$(ls -A /app/ 2>/dev/null)" ]; then
    echo "/app/ 目录为空，正在下载 WordPress..."
    curl -o wordpress.tar.gz https://wordpress.org/latest.tar.gz
    tar -xzf wordpress.tar.gz --strip-components=1 -C /app
    rm wordpress.tar.gz
    echo "WordPress 下载完成。"
else
    echo "/app/ 目录已存在内容，跳过下载。"
fi

# 使用 exec 启动进程，让 Nginx 成为 PID 1（能接收信号）
# 但需要先启动 PHP-FPM
php-fpm83 --nodaemonize &
PHP_FPM_PID=$!

# 捕获退出信号，确保 PHP-FPM 也能被停止
trap "kill $PHP_FPM_PID; exit" TERM INT

nginx -g "daemon off;"

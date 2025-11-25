#!/bin/sh
set -e  # 遇到错误立即退出

if [ -d "/app" ] && [ -z "$(ls -A /app/ 2>/dev/null)" ]; then
    echo "/app/ 目录为空，正在下载 WordPress..."
    curl -o wordpress.zip https://jkjoy.github.io/dockerfile/wordpress-sqlite/latest.zip
    unzip wordpress.zip -d /app
    rm wordpress.zip
    echo "WordPress 下载完成。"

else
    echo "/app/ 目录已存在内容，跳过下载。"
fi

# 但需要先启动 PHP-FPM
php-fpm84 --nodaemonize &
PHP_FPM_PID=$!

# 捕获退出信号，确保 PHP-FPM 也能被停止
trap "kill $PHP_FPM_PID; exit" TERM INT

nginx -g "daemon off;"
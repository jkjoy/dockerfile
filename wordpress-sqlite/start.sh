#!/bin/sh
set -e  # 遇到错误立即退出

if [ -d "/app" ] && [ -z "$(ls -A /app/ 2>/dev/null)" ]; then
    echo "/app/ 目录为空，正在安装 WordPress..."
    # 复制本地已经解压的 WordPress 文件
    cp -r /usr/src/wordpress/* /app/
    echo "WordPress 安装完成。"

#再次赋予 /app 目录适当的权限
    chown -R nginx:nginx /app
else
    echo "/app/ 目录已存在内容，跳过安装。"
fi

# 但需要先启动 PHP-FPM
php-fpm84 --nodaemonize &
PHP_FPM_PID=$!

# 捕获退出信号，确保 PHP-FPM 也能被停止
trap "kill $PHP_FPM_PID; exit" TERM INT

nginx -g "daemon off;"
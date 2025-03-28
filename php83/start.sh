#!/bin/sh
set -e  # 遇到错误立即退出

# 定义变量
TYPECHO_URL="https://github.com/typecho/typecho/releases/latest/download/typecho.zip"
INSTALL_DIR="/app"
ZIP_FILE="/tmp/typecho.zip"

# 如果 /app 为空，则下载安装 Typecho
if [ -z "$(ls -A $INSTALL_DIR 2>/dev/null)" ]; then
    echo "检测到 /app 为空，开始安装 Typecho..."

    # 使用 curl 下载最新版 Typecho
    echo "正在下载 Typecho..."
    if ! curl -sSL "$TYPECHO_URL" -o "$ZIP_FILE"; then
        echo "❌ 下载失败！请检查网络或URL"
        exit 1
    fi

    # 解压到 /app（直接解压到目标目录）
    echo "解压文件中..."
    if ! unzip -q "$ZIP_FILE" -d "$INSTALL_DIR"; then
        echo "❌ 解压失败！请检查ZIP文件完整性"
        exit 1
    fi

    # 清理和赋权
    rm -f "$ZIP_FILE"
    chown -R nginx:nginx "$INSTALL_DIR"
    chmod -R 755 "$INSTALL_DIR"
    echo "✅ Typecho 安装完成！"
else
    echo "检测到 /app 已存在内容，跳过安装"
fi

# 启动 PHP-FPM (后台运行)
php-fpm -D
sleep 2  # 等待 PHP-FPM 启动

# 检查 PHP-FPM 是否运行
if ! pgrep "php-fpm" >/dev/null; then
    echo "❌ PHP-FPM 启动失败！"
    exit 1
fi

# 启动 Nginx (前台运行)
echo "启动 Nginx..."
nginx -g "daemon off;"
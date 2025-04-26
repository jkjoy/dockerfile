#!/bin/sh
set -e  # 遇到错误立即退出

# 定义变量
TYPECHO_URL="https://github.com/typecho/typecho/releases/latest/download/typecho.zip"
INSTALL_DIR="/app"
ZIP_FILE="/tmp/typecho.zip"
TESTORE_URL="https://jkjoy.github.io/dockerfile/php83/typecho/TeStore.zip"
TESTORE_ZIP="/tmp/TeStore.zip"
PLUGINS_DIR="$INSTALL_DIR/usr/plugins"

# 如果 /app 中没有 index.php，则下载安装 Typecho
if [ ! -f "$INSTALL_DIR/index.php" ]; then
    echo "检测到 /app 中没有 index.php，开始安装 Typecho..."

    # 使用 curl 下载最新版 Typecho
    echo "正在下载 Typecho..."
    if ! curl -sSL "$TYPECHO_URL" -o "$ZIP_FILE"; then
        echo "❌ Typecho 下载失败！请检查网络或URL"
        exit 1
    fi

    # 解压到 /app
    echo "解压 Typecho..."
    if ! unzip -q "$ZIP_FILE" -d "$INSTALL_DIR"; then
        echo "❌ Typecho 解压失败！请检查ZIP文件完整性"
        exit 1
    fi

    # 下载 TeStore 插件
    echo "下载 TeStore 插件..."
    if ! curl -sSL "$TESTORE_URL" -o "$TESTORE_ZIP"; then
        echo "⚠️ TeStore 插件下载失败，跳过安装"
    else
        # 创建插件目录（如果不存在）
        mkdir -p "$PLUGINS_DIR"
        
        # 解压 TeStore 到插件目录
        echo "解压 TeStore 插件..."
        if unzip -q "$TESTORE_ZIP" -d "$PLUGINS_DIR"; then
            echo "✅ TeStore 插件安装完成！"
        else
            echo "⚠️ TeStore 插件解压失败，跳过安装"
        fi
    fi

    # 清理和赋权
    rm -f "$ZIP_FILE" "$TESTORE_ZIP"
    chown -R nginx:nginx "$INSTALL_DIR"
    chmod -R 755 "$INSTALL_DIR"
    echo "✅ Typecho 及插件安装完成！"
else
    echo "检测到 /app 中已存在 index.php，跳过安装"
fi

# 启动 PHP-FPM (后台运行)
php-fpm83 -D
sleep 2  # 等待 PHP-FPM 启动

# 检查 PHP-FPM 是否运行
if ! pgrep "php-fpm83" >/dev/null; then
    echo "❌ PHP-FPM 启动失败！"
    exit 1
fi

# 启动 Nginx (前台运行)
echo "启动 Nginx..."
nginx -g "daemon off;"
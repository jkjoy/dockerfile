#!/bin/sh
set -e  # 遇到错误立即退出

# 日志文件
LOG_FILE="/var/log/setup.log"
mkdir -p "$(dirname $LOG_FILE)"
exec > >(tee -i $LOG_FILE) 2>&1

# 定义变量
TYPECHO_URL="https://github.com/typecho/typecho/releases/latest/download/typecho.zip"
INSTALL_DIR="/app"
ZIP_FILE="/tmp/typecho.zip"
TESTORE_URL="https://jkjoy.github.io/dockerfile/php83/typecho/TeStore.zip"
TESTORE_ZIP="/tmp/TeStore.zip"
PLUGINS_DIR="$INSTALL_DIR/usr/plugins"

# 检查 Typecho 是否已安装
if [ ! -f "$INSTALL_DIR/index.php" ]; then
    echo "检测到 /app 中没有 index.php，开始安装 Typecho..."

    # 下载 Typecho
    echo "正在下载 Typecho..."
    if ! curl -sSL "$TYPECHO_URL" -o "$ZIP_FILE"; then
        echo "❌ Typecho 下载失败！请检查网络或URL: $TYPECHO_URL"
        exit 1
    fi

    # 解压 Typecho
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
        mkdir -p "$PLUGINS_DIR"
        echo "解压 TeStore 插件..."
        if unzip -q "$TESTORE_ZIP" -d "$PLUGINS_DIR"; then
            echo "✅ TeStore 插件安装完成！"
        else
            echo "⚠️ TeStore 插件解压失败，跳过安装"
        fi
    fi

    # 清理和赋权
    rm -f "$ZIP_FILE" "$TESTORE_ZIP"
    chown -R nginx:nginx "$INSTALL_DIR" || true
    chmod -R 755 "$INSTALL_DIR" || true
    echo "✅ Typecho 及插件安装完成！"
else
    echo "检测到 /app 中已存在 index.php，跳过安装"
fi

# 赋权
chown -R nginx:nginx "$INSTALL_DIR" || true
chmod -R 755 "$INSTALL_DIR" || true

# 检查 PHP-FPM 配置
if ! php-fpm83 -t; then
    echo "❌ PHP-FPM 配置测试失败！"
    exit 1
fi

# 启动 PHP-FPM 并显示详细日志
echo "▶️ 启动 PHP-FPM..."
php-fpm83 -D

# 检查 Nginx 配置
if ! nginx -t; then
    echo "❌ Nginx 配置测试失败！请检查 Nginx 配置文件"
    nginx -t
    exit 1
fi
echo "✅ Nginx 配置测试通过"

# 启动 Nginx
echo "▶️ 启动 Nginx..."
exec nginx -g "daemon off;"
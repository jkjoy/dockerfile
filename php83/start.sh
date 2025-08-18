#!/bin/sh
set -e  # 遇到错误立即退出

# 日志文件
LOG_FILE="/var/log/setup.log"
mkdir -p "$(dirname $LOG_FILE)"
exec > >(tee -i $LOG_FILE) 2>&1

# 定义变量
TYPECHO_URLS=(
    "https://github.com/typecho/typecho/releases/latest/download/typecho.zip"
    "https://typecho.org/downloads/typecho.zip"
    "https://gh.ods.ee/https://github.com/typecho/typecho/releases/latest/download/typecho.zip"
)
TESTORE_URLS=(
    "https://gh.ods.ee/https://jkjoy.github.io/dockerfile/php83/typecho/TeStore.zip"
    "https://cdn.jsdelivr.net/gh/jkjoy/dockerfile/php83/typecho/TeStore.zip"
    "https://jkjoy.github.io/dockerfile/php83/typecho/TeStore.zip"
)
THEME_URLS=(
    "https://github.com/jkjoy/typecho-theme-farallon/releases/download/0.8.0/farallon.zip"
    "https://cdn.jsdelivr.net/gh/jkjoy/dockerfile/php83/typecho/farallon.zip"
    "https://gh.ods.ee/https://github.com/jkjoy/typecho-theme-farallon/releases/download/0.8.0/farallon.zip"
)
INSTALL_DIR="/app"
TYPECHO_TMPFILE="/tmp/typecho.zip"
TESTORE_ZIP="/tmp/TeStore.zip"
PLUGINS_DIR="$INSTALL_DIR/usr/plugins"
THEME_FILE="/tmp/farallon.zip"
THEME_DIR="$INSTALL_DIR/usr/themes/farallon"

# 下载函数，带重试机制
download_file() {
    local urls=("$@")
    local output_file="${urls[-1]}"
    unset 'urls[${#urls[@]}-1]'  # 移除最后一个元素（输出文件路径）

    for url in "${urls[@]}"; do
        echo "尝试从 $url 下载..."
        if curl -sSL --connect-timeout 10 --retry 2 "$url" -o "$output_file"; then
            echo "✅ 下载成功！"
            return 0
        else
            echo "⚠️ 从 $url 下载失败，尝试下一个地址..."
        fi
    done
    echo "❌ 所有下载地址均失败！"
    return 1
}

# 检查 Typecho 是否已安装
if [ ! -f "$INSTALL_DIR/index.php" ]; then
    echo "检测到 /app 中没有 index.php，开始安装 Typecho..."

    # 下载 Typecho
    echo "正在下载 Typecho..."
    if ! download_file "${TYPECHO_URLS[@]}" "$TYPECHO_TMPFILE"; then
        echo "❌ Typecho 下载失败！请检查网络或URL"
        exit 1
    fi

    # 解压 Typecho
    echo "解压 Typecho..."
    if ! unzip -q "$TYPECHO_TMPFILE" -d "$INSTALL_DIR"; then
        echo "❌ Typecho 解压失败！请检查ZIP文件完整性"
        exit 1
    fi

    # 下载 TeStore 插件
    echo "下载 TeStore 插件..."
    if ! download_file "${TESTORE_URLS[@]}" "$TESTORE_ZIP"; then
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

    # 下载并安装 Farallon 主题
    echo "下载 Farallon 主题..."
    if ! download_file "${THEME_URLS[@]}" "$THEME_FILE"; then
        echo "⚠️ Farallon 主题下载失败，跳过安装"
    else
        mkdir -p "$THEME_DIR"
        echo "解压 Farallon 主题..."
        if unzip -q "$THEME_FILE" -d "$THEME_DIR"; then
            echo "✅ Farallon 主题安装完成！"
        else
            echo "⚠️ Farallon 主题解压失败，跳过安装"
        fi
    fi

    # 清理和赋权
    rm -f "$TYPECHO_TMPFILE" "$TESTORE_ZIP" "$THEME_FILE"
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
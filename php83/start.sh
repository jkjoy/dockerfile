#!/bin/sh
set -e  # 遇到错误立即退出

# 日志文件
LOG_FILE="/var/log/setup.log"
LOG_DIR="$(dirname $LOG_FILE)"

# 确保日志目录存在并有正确权限
mkdir -p "$LOG_DIR"
chown nginx:nginx "$LOG_DIR" || true
chmod 755 "$LOG_DIR" || true

# 初始化日志文件
touch "$LOG_FILE" || { echo "❌ 无法创建日志文件 $LOG_FILE"; exit 1; }
chown nginx:nginx "$LOG_FILE" || true
chmod 644 "$LOG_FILE" || true

# 重定向输出到日志文件
exec > >(tee -a "$LOG_FILE") 2>&1
echo "📅 脚本启动时间: $(date)"

# 定义变量（使用空格分隔的字符串代替数组）
TYPECHO_URLS="https://jkjoy.github.io/dockerfile/php83/typecho/typecho.zip https://github.com/typecho/typecho/releases/latest/download/typecho.zip https://typecho.org/downloads/typecho.zip https://gh.ods.ee/https://github.com/typecho/typecho/releases/latest/download/typecho.zip"
TESTORE_URLS="https://jkjoy.github.io/dockerfile/php83/typecho/TeStore.zip https://cdn.jsdelivr.net/gh/jkjoy/dockerfile/php83/typecho/TeStore.zip"
THEME_URLS="https://jkjoy.github.io/dockerfile/php83/typecho/farallon.zip https://cdn.jsdelivr.net/gh/jkjoy/dockerfile/php83/typecho/farallon.zip https://gh.ods.ee/https://github.com/jkjoy/typecho-theme-farallon/releases/download/0.8.0/farallon.zip https://github.com/jkjoy/typecho-theme-farallon/releases/download/0.8.0/farallon.zip"
INSTALL_DIR="/app"
TYPECHO_TMPFILE="/tmp/typecho.zip"
TESTORE_ZIP="/tmp/TeStore.zip"
THEME_FILE="/tmp/farallon.zip"
PLUGINS_DIR="$INSTALL_DIR/usr/plugins"
THEME_DIR="$INSTALL_DIR/usr/themes"

# 下载函数，带重试机制
download_file() {
    urls="$1"
    output_file="$2"

    for url in $urls; do
        echo "尝试从 $url 下载..."
        if curl -sSL --connect-timeout 10 --retry 3 --retry-delay 2 "$url" -o "$output_file"; then
            echo "✅ 下载成功！"
            return 0
        else
            echo "⚠️ 从 $url 下载失败，尝试下一个地址..."
        fi
    done
    echo "❌ 所有下载地址均失败！"
    return 1
}

# 检查并清理临时文件
cleanup_temp_files() {
    echo "清理临时文件..."
    rm -f "$TYPECHO_TMPFILE" "$TESTORE_ZIP" "$THEME_FILE" || true
    echo "✅ 临时文件清理完成"
}

# 检查 Typecho 是否已安装
if [ ! -f "$INSTALL_DIR/index.php" ]; then
    echo "检测到 /app 中没有 index.php，开始安装 Typecho..."

    # 确保安装目录存在
    mkdir -p "$INSTALL_DIR" || { echo "❌ 无法创建安装目录 $INSTALL_DIR"; exit 1; }
    chown nginx:nginx "$INSTALL_DIR" || true
    chmod 755 "$INSTALL_DIR" || true

    # 下载 Typecho
    echo "正在下载 Typecho..."
    if ! download_file "$TYPECHO_URLS" "$TYPECHO_TMPFILE"; then
        echo "❌ Typecho 下载失败！请检查网络或URL"
        cleanup_temp_files
        exit 1
    fi

    # 解压 Typecho
    echo "解压 Typecho..."
    if ! unzip -q "$TYPECHO_TMPFILE" -d "$INSTALL_DIR"; then
        echo "❌ Typecho 解压失败！请检查ZIP文件完整性"
        cleanup_temp_files
        exit 1
    fi

    # 下载 TeStore 插件
    echo "下载 TeStore 插件..."
    if ! download_file "$TESTORE_URLS" "$TESTORE_ZIP"; then
        echo "⚠️ TeStore 插件下载失败，跳过安装"
    else
        mkdir -p "$PLUGINS_DIR" || { echo "❌ 无法创建插件目录 $PLUGINS_DIR"; exit 1; }
        echo "解压 TeStore 插件..."
        if unzip -q "$TESTORE_ZIP" -d "$PLUGINS_DIR"; then
            echo "✅ TeStore 插件安装完成！"
        else
            echo "⚠️ TeStore 插件解压失败，跳过安装"
        fi
    fi

    # 下载并安装 Farallon 主题
    echo "下载 Farallon 主题..."
    if ! download_file "$THEME_URLS" "$THEME_FILE"; then
        echo "⚠️ Farallon 主题下载失败，跳过安装"
    else
        mkdir -p "$THEME_DIR" || { echo "❌ 无法创建主题目录 $THEME_DIR"; exit 1; }
        echo "解压 Farallon 主题..."
        if unzip -q "$THEME_FILE" -d "$THEME_DIR"; then
            echo "✅ Farallon 主题安装完成！"
        else
            echo "⚠️ Farallon 主题解压失败，跳过安装"
        fi
    fi

    # 清理临时文件
    cleanup_temp_files

    # 赋权
    echo "设置安装目录权限..."
    chown -R nginx:nginx "$INSTALL_DIR" || { echo "⚠️ 赋权失败，但继续执行"; }
    chmod -R 755 "$INSTALL_DIR" || { echo "⚠️ 设置权限失败，但继续执行"; }
    echo "✅ Typecho 及插件/主题安装完成！"
else
    echo "检测到 /app 中已存在 index.php，跳过安装"
fi

# 再次确保权限
echo "确保安装目录权限..."
chown -R nginx:nginx "$INSTALL_DIR" || { echo "⚠️ 赋权失败，但继续执行"; }
chmod -R 755 "$INSTALL_DIR" || { echo "⚠️ 设置权限失败，但继续执行"; }

# 检查 PHP-FPM 配置
echo "检查 PHP-FPM 配置..."
if ! php-fpm83 -t; then
    echo "❌ PHP-FPM 配置测试失败！"
    cat /var/log/php-fpm.log || echo "⚠️ 无法读取 PHP-FPM 日志"
    cleanup_temp_files
    exit 1
fi
echo "✅ PHP-FPM 配置测试通过"

# 启动 PHP-FPM
echo "▶️ 启动 PHP-FPM..."
if ! php-fpm83 -D; then
    echo "❌ PHP-FPM 启动失败！"
    cat /var/log/php-fpm.log || echo "⚠️ 无法读取 PHP-FPM 日志"
    cleanup_temp_files
    exit 1
fi
echo "✅ PHP-FPM 启动成功"

# 检查 Nginx 配置
echo "检查 Nginx 配置..."
if ! nginx -t; then
    echo "❌ Nginx 配置测试失败！"
    nginx -t 2>>"$LOG_FILE" || true
    cleanup_temp_files
    exit 1
fi
echo "✅ Nginx 配置测试通过"

# 启动 Nginx
echo "▶️ 启动 Nginx..."
exec nginx -g "daemon off;" || { echo "❌ Nginx 启动失败！"; cat /var/log/nginx/error.log || echo "⚠️ 无法读取 Nginx 错误日志"; exit 1; }
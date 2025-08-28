#!/bin/sh
set -e  # 遇到错误立即退出

# ----------------------------------------------------------
# 0. 预检：确保 nginx 用户存在
# ----------------------------------------------------------
id nginx &>/dev/null || adduser -D -s /bin/false nginx

# ----------------------------------------------------------
# 1. 日志设置
# ----------------------------------------------------------
LOG_FILE="/var/log/setup.log"
LOG_DIR="$(dirname "$LOG_FILE")"

mkdir -p "$LOG_DIR"
chown nginx:nginx "$LOG_DIR"   2>/dev/null || true
chmod 755 "$LOG_DIR"           2>/dev/null || true

touch "$LOG_FILE"              || { echo "❌ 无法创建日志文件 $LOG_FILE"; exit 1; }
chown nginx:nginx "$LOG_FILE"  2>/dev/null || true
chmod 644 "$LOG_FILE"          2>/dev/null || true

# 同时输出到终端和日志
exec > >(tee -a "$LOG_FILE") 2>&1
echo "📅 脚本启动时间: $(date)"

# ----------------------------------------------------------
# 2. 变量
# ----------------------------------------------------------
HADSKY_URLS="https://jkjoy.github.io/dockerfile/hadsky/hadsky/latest.zip"
INSTALL_DIR="/app"
TMPFILE="/tmp/hadsky.zip"

cleanup_temp_files() { rm -f "$TMPFILE"; }

# ----------------------------------------------------------
# 3. 下载函数（修复版）
# ----------------------------------------------------------
download_file() {
    local urls="$1"
    local output_file="$2"
    local url rc

    # 用 for 循环避免子 Shell 导致 return 失效
    for url in $urls; do
        [ -z "$url" ] && continue
        echo "尝试从 $url 下载..."
        if curl -fsSL --connect-timeout 10 --retry 3 --retry-delay 2 \
               "$url" -o "$output_file"; then
            echo "✅ 下载成功！"
            return 0
        else
            echo "⚠️ 从 $url 下载失败，尝试下一个地址..."
        fi
    done
    echo "❌ 所有下载地址均失败！"
    return 1
}

# ----------------------------------------------------------
# 4. Hadsky 安装
# ----------------------------------------------------------
if [ ! -f "$INSTALL_DIR/index.php" ]; then
    echo "检测到 $INSTALL_DIR 中没有 index.php，开始安装 Hadsky..."

    mkdir -p "$INSTALL_DIR" || { echo "❌ 无法创建安装目录 $INSTALL_DIR"; exit 1; }
    chown nginx:nginx "$INSTALL_DIR" 2>/dev/null || true
    chmod 755 "$INSTALL_DIR"          2>/dev/null || true

    # 下载
    echo "正在下载..."
    if ! download_file "$HADSKY_URLS" "$TMPFILE"; then
        cleanup_temp_files
        exit 1
    fi

    # 解压
    echo "解压..."
    if ! unzip -q "$TMPFILE" -d "$INSTALL_DIR"; then
        echo "❌ 解压失败！请检查 ZIP 文件完整性"
        cleanup_temp_files
        exit 1
    fi

    # 赋权
    echo "设置安装目录权限..."
    chown -R nginx:nginx "$INSTALL_DIR" 2>/dev/null || echo "⚠️ 赋权失败，但继续执行"
    chmod -R 755 "$INSTALL_DIR"         2>/dev/null || echo "⚠️ 设置权限失败，但继续执行"
    echo "✅ Hadsky 安装完成！"

    cleanup_temp_files
else
    echo "检测到 $INSTALL_DIR 中已存在 index.php，跳过安装"
fi

# ----------------------------------------------------------
# 5. 再次确保权限
# ----------------------------------------------------------
chown -R nginx:nginx "$INSTALL_DIR" 2>/dev/null || echo "⚠️ 赋权失败，但继续执行"
chmod -R 755 "$INSTALL_DIR"         2>/dev/null || echo "⚠️ 设置权限失败，但继续执行"

# ----------------------------------------------------------
# 6. PHP-FPM 启动
# ----------------------------------------------------------
echo "检查 PHP-FPM 配置..."
# 自适应 php-fpm 可执行文件名
PHP_FPM_BIN=""
for cmd in php-fpm83 php-fpm8 php-fpm; do
    command -v "$cmd" >/dev/null && PHP_FPM_BIN="$cmd" && break
done
[ -z "$PHP_FPM_BIN" ] && { echo "❌ 找不到 php-fpm 可执行文件"; exit 1; }

if ! "$PHP_FPM_BIN" -t; then
    echo "❌ PHP-FPM 配置测试失败！"
    cat /var/log/php-fpm.log 2>/dev/null || echo "⚠️ 无法读取 PHP-FPM 日志"
    exit 1
fi
echo "✅ PHP-FPM 配置测试通过"

echo "▶️ 启动 PHP-FPM..."
"$PHP_FPM_BIN" -D || {
    echo "❌ PHP-FPM 启动失败！"
    cat /var/log/php-fpm.log 2>/dev/null || echo "⚠️ 无法读取 PHP-FPM 日志"
    exit 1
}
echo "✅ PHP-FPM 启动成功"

# ----------------------------------------------------------
# 7. Nginx 启动
# ----------------------------------------------------------
echo "检查 Nginx 配置..."
if ! nginx -t; then
    echo "❌ Nginx 配置测试失败！"
    nginx -t 2>>"$LOG_FILE" || true
    exit 1
fi
echo "✅ Nginx 配置测试通过"

echo "▶️ 启动 Nginx..."
exec nginx -g "daemon off;" || {
    echo "❌ Nginx 启动失败！"
    cat /var/log/nginx/error.log 2>/dev/null || echo "⚠️ 无法读取 Nginx 错误日志"
    exit 1
}
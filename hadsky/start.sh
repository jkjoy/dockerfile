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
HADSKY_URLS="https://jkjoy.github.io/dockerfile/hadsky/hadsky/latest.zip"
INSTALL_DIR="/app"
TMPFILE="/tmp/hadsky.zip"
cleanup_temp_files() { rm -f "$TMPFILE"; }

# 下载函数，带重试机制
download_file() {
    urls="$1"
    output_file="$2"

    printf '%s\n' $urls | while read -r url; do
        [ -z "$url" ] && continue
        echo "尝试从 $url 下载..."
        ...
    done
}       

# 检查 Hadsky 是否已安装
if [ ! -f "$INSTALL_DIR/index.php" ]; then
    echo "检测到 /app 中没有 index.php，开始安装 Hadsky..."

    # 确保安装目录存在
    mkdir -p "$INSTALL_DIR" || { echo "❌ 无法创建安装目录 $INSTALL_DIR"; exit 1; }
    chown nginx:nginx "$INSTALL_DIR" || true
    chmod 755 "$INSTALL_DIR" || true

    # 下载 Hadsky
    echo "正在下载..."
    if ! download_file "$HADSKY_URLS" "$TMPFILE"; then
        echo "❌ 下载失败！请检查网络或URL"
        cleanup_temp_files
        exit 1
    fi

    # 解压 
    echo "解压..."
    if ! unzip -q "$TMPFILE" -d "$INSTALL_DIR"; then
        echo "❌ 解压失败！请检查ZIP文件完整性"
        cleanup_temp_files
        exit 1
    fi

    # 赋权
    echo "设置安装目录权限..."
    chown -R nginx:nginx "$INSTALL_DIR" || { echo "⚠️ 赋权失败，但继续执行"; }
    chmod -R 755 "$INSTALL_DIR" || { echo "⚠️ 设置权限失败，但继续执行"; }
    echo "✅ Hadsky安装完成！"
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
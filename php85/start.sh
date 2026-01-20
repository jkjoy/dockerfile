#!/bin/sh
set -e  # 遇到错误立即退出

# 日志文件
LOG_FILE="/var/log/setup.log"
LOG_DIR="$(dirname $LOG_FILE)"

# 日志函数
log_info() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') [INFO] $*"
}

log_error() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') [ERROR] $*" >&2
}

log_warn() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') [WARN] $*"
}

log_success() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') [SUCCESS] ✅ $*"
}

# 优雅退出处理
cleanup_on_exit() {
    exit_code=$?
    if [ $exit_code -ne 0 ]; then
        log_error "脚本异常退出，退出码: $exit_code"
        log_info "正在清理资源..."
        pkill -TERM php-fpm85 2>/dev/null || true
        pkill -TERM nginx 2>/dev/null || true
        cleanup_temp_files
    fi
}

trap cleanup_on_exit EXIT INT TERM

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
log_info "======================================"
log_info "📅 脚本启动时间: $(date)"
log_info "======================================"

# 定义变量（使用空格分隔的字符串代替数组）
TYPECHO_URLS="https://www.jkjoy.de/dockerfile/php85/download/typecho.zip"
TESTORE_URLS="https://www.jkjoy.de/dockerfile/php85/download/UploadPlugin.zip"
INSTALL_DIR="/app"
TYPECHO_TMPFILE="/tmp/typecho.zip"
TESTORE_ZIP="/tmp/UploadPlugin.zip"
PLUGINS_DIR="$INSTALL_DIR/usr/plugins"

# 下载函数，带重试机制
download_file() {
    urls="$1"
    output_file="$2"
    description="${3:-文件}"

    log_info "开始下载 $description..."
    for url in $urls; do
        log_info "尝试从 $url 下载..."
        if curl -sSL --connect-timeout 10 --retry 3 --retry-delay 2 --fail "$url" -o "$output_file"; then
            # 验证文件是否成功下载且不为空
            if [ -f "$output_file" ] && [ -s "$output_file" ]; then
                local file_size=$(stat -c%s "$output_file" 2>/dev/null || stat -f%z "$output_file" 2>/dev/null || echo "未知")
                log_success "$description 下载成功！文件大小: $file_size 字节"
                return 0
            else
                log_warn "下载的文件为空或不存在，尝试下一个地址..."
                rm -f "$output_file"
            fi
        else
            log_warn "从 $url 下载失败，尝试下一个地址..."
        fi
    done
    log_error "所有下载地址均失败！$description"
    return 1
}

# 检查并清理临时文件
cleanup_temp_files() {
    log_info "清理临时文件..."
    rm -f "$TYPECHO_TMPFILE" "$TESTORE_ZIP" "$THEME_FILE" || true
    log_success "临时文件清理完成"
}

# 解压函数，带错误处理
extract_zip() {
    zip_file="$1"
    dest_dir="$2"
    description="${3:-ZIP文件}"

    log_info "开始解压 $description 到 $dest_dir..."
    if [ ! -f "$zip_file" ]; then
        log_error "$description 不存在: $zip_file"
        return 1
    fi

    if ! unzip -q -o "$zip_file" -d "$dest_dir"; then
        log_error "$description 解压失败！可能是ZIP文件损坏"
        return 1
    fi

    log_success "$description 解压完成"
    return 0
}

# 设置目录权限
set_permissions() {
    target_dir="$1"
    description="${2:-目录}"

    log_info "设置 $description 权限..."
    if chown -R nginx:nginx "$target_dir" 2>/dev/null; then
        log_success "$description 所有者设置成功"
    else
        log_warn "$description 所有者设置失败，但继续执行"
    fi

    if chmod -R 755 "$target_dir" 2>/dev/null; then
        log_success "$description 权限设置成功"
    else
        log_warn "$description 权限设置失败，但继续执行"
    fi
}

# 检查 Typecho 是否已安装
if [ ! -f "$INSTALL_DIR/index.php" ]; then
    log_info "======================================"
    log_info "检测到 /app 中没有 index.php，开始安装 Typecho..."
    log_info "======================================"

    # 确保安装目录存在
    log_info "创建安装目录 $INSTALL_DIR..."
    if ! mkdir -p "$INSTALL_DIR"; then
        log_error "无法创建安装目录 $INSTALL_DIR"
        exit 1
    fi
    set_permissions "$INSTALL_DIR" "安装目录"

    # 下载 Typecho
    if ! download_file "$TYPECHO_URLS" "$TYPECHO_TMPFILE" "Typecho"; then
        log_error "Typecho 下载失败！请检查网络或URL"
        cleanup_temp_files
        exit 1
    fi

    # 解压 Typecho
    if ! extract_zip "$TYPECHO_TMPFILE" "$INSTALL_DIR" "Typecho"; then
        log_error "Typecho 解压失败！请检查ZIP文件完整性"
        cleanup_temp_files
        exit 1
    fi

    # 下载并安装 UploadPlugin 插件
    log_info "======================================"
    log_info "开始安装 UploadPlugin 插件..."
    log_info "======================================"

    # 确保插件目录存在
    if ! mkdir -p "$PLUGINS_DIR"; then
        log_error "无法创建插件目录 $PLUGINS_DIR"
        cleanup_temp_files
        exit 1
    fi

    # 下载插件
    if download_file "$TESTORE_URLS" "$TESTORE_ZIP" "UploadPlugin插件"; then
        # 解压插件到临时目录
        PLUGIN_TEMP_DIR="/tmp/upload_plugin_temp"
        mkdir -p "$PLUGIN_TEMP_DIR"

        if extract_zip "$TESTORE_ZIP" "$PLUGIN_TEMP_DIR" "UploadPlugin插件"; then
            # 移动插件到正确位置
            if [ -d "$PLUGIN_TEMP_DIR/UploadPlugin" ]; then
                log_info "安装 UploadPlugin 插件到 $PLUGINS_DIR..."
                mv "$PLUGIN_TEMP_DIR/UploadPlugin" "$PLUGINS_DIR/" || {
                    log_warn "移动插件失败，但继续执行"
                }
                log_success "UploadPlugin 插件安装完成"
            else
                log_warn "插件目录结构不符合预期，跳过插件安装"
            fi
            rm -rf "$PLUGIN_TEMP_DIR"
        else
            log_warn "UploadPlugin 插件解压失败，跳过插件安装"
        fi
    else
        log_warn "UploadPlugin 插件下载失败，跳过插件安装"
    fi

    # 清理临时文件
    cleanup_temp_files

    # 设置最终权限
    set_permissions "$INSTALL_DIR" "Typecho 安装目录"

    log_info "======================================"
    log_success "Typecho 及插件安装完成！"
    log_info "======================================"
else
    log_info "检测到 /app 中已存在 index.php，跳过安装"
fi

# 再次确保权限
log_info "确保安装目录权限..."
set_permissions "$INSTALL_DIR" "安装目录"

log_info "======================================"
log_info "开始启动服务..."
log_info "======================================"

# 检查 PHP-FPM 配置
log_info "检查 PHP-FPM 配置..."
if ! php-fpm85 -t; then
    log_error "PHP-FPM 配置测试失败！"
    if [ -f /var/log/php-fpm.log ]; then
        log_error "PHP-FPM 日志内容:"
        tail -n 20 /var/log/php-fpm.log
    fi
    exit 1
fi
log_success "PHP-FPM 配置测试通过"

# 启动 PHP-FPM
log_info "▶️ 启动 PHP-FPM..."
if ! php-fpm85 -D; then
    log_error "PHP-FPM 启动失败！"
    if [ -f /var/log/php-fpm.log ]; then
        log_error "PHP-FPM 日志内容:"
        tail -n 20 /var/log/php-fpm.log
    fi
    exit 1
fi
log_success "PHP-FPM 启动成功"

# 等待 PHP-FPM 完全启动
log_info "等待 PHP-FPM 完全启动..."
sleep 2

# 验证 PHP-FPM 进程
if pgrep -f "php-fpm85" > /dev/null; then
    log_success "PHP-FPM 进程运行正常"
else
    log_error "PHP-FPM 进程未找到！"
    exit 1
fi

# 检查 Nginx 配置
log_info "检查 Nginx 配置..."
if ! nginx -t 2>&1; then
    log_error "Nginx 配置测试失败！"
    nginx -t 2>&1 | tee -a "$LOG_FILE"
    exit 1
fi
log_success "Nginx 配置测试通过"

# 启动 Nginx (前台运行)
log_info "▶️ 启动 Nginx（前台模式）..."
log_info "======================================"
log_success "所有服务启动完成！"
log_info "======================================"
log_info "Typecho 应该已经可以访问了"
log_info "日志文件: $LOG_FILE"
log_info "======================================"

# 执行 Nginx（这会阻塞，保持容器运行）
exec nginx -g "daemon off;" || {
    log_error "Nginx 启动失败！"
    if [ -f /var/log/nginx/error.log ]; then
        log_error "Nginx 错误日志:"
        tail -n 20 /var/log/nginx/error.log
    fi
    exit 1
}

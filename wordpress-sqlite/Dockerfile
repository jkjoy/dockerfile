FROM nginx:stable-alpine
WORKDIR /app

# 安装 PHP 扩展（精简）
RUN apk --no-cache add \
    php84 php84-fpm php84-common \
    php84-pdo php84-sqlite3 php84-pdo_sqlite \
    php84-zip php84-curl php84-gd php84-intl php84-xsl \
    php84-mbstring php84-dom php84-openssl php84-zlib \
    php84-fileinfo php84-opcache php84-imap php84-exif \
    php84-pecl-imagick php84-pecl-redis \
    php84-mysqli \
    unzip

# 复制配置文件
COPY wordpress-sqlite/nginx.conf /etc/nginx/nginx.conf
COPY wordpress-sqlite/php.ini /etc/php84/php.ini
COPY wordpress-sqlite/www.conf /etc/php84/php-fpm.d/www.conf
COPY wordpress-sqlite/default /etc/nginx/sites-available/default

RUN chown -R nginx:nginx /app && chmod -R 755 /app

# 创建软链接（确保 nginx.conf 已包含此目录）
RUN mkdir -p /etc/nginx/sites-enabled && \
    ln -sf /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default

# 复制并设置启动脚本
COPY wordpress-sqlite/start.sh /start.sh
RUN chmod +x /start.sh

EXPOSE 80
CMD ["/start.sh"]

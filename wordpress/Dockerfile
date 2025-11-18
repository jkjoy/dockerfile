FROM nginx:stable-alpine
WORKDIR /app

# 安装 PHP 扩展（精简）
RUN apk --no-cache add \
    php83 php83-fpm php83-common \
    php83-pdo php83-sqlite3 php83-pdo_sqlite \
    php83-zip php83-curl php83-gd php83-intl php83-xsl \
    php83-mbstring php83-dom php83-openssl php83-zlib \
    php83-fileinfo php83-opcache php83-imap php83-exif \
    php83-pecl-imagick php83-pecl-redis \
    php83-mysqli php83-pdo_mysql

# 复制配置文件
COPY wordpress/nginx.conf /etc/nginx/nginx.conf
COPY wordpress/php.ini /etc/php83/php.ini
COPY wordpress/www.conf /etc/php83/php-fpm.d/www.conf
COPY wordpress/default /etc/nginx/sites-available/default

RUN chown -R nginx:nginx /app && chmod -R 755 /app

# 创建软链接（确保 nginx.conf 已包含此目录）
RUN mkdir -p /etc/nginx/sites-enabled && \
    ln -sf /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default

# 复制并设置启动脚本
COPY wordpress/start.sh /start.sh
RUN chmod +x /start.sh

EXPOSE 80
CMD ["/start.sh"]

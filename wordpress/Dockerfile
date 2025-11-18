FROM nginx:stable-alpine
WORKDIR /app
RUN chown -R nginx:nginx /app && chmod -R 755 /app
# 安装 PHP 和必要的扩展
RUN apk --no-cache add \
    imagemagick-dev \
    php83 \
    php83-fpm \
    php83-pdo \
    php83-sqlite3 \
    php83-zip \
    php83-curl \
    php83-gd \
    php83-intl \
    php83-xsl \
    php83-mbstring \
    php83-dom \
    php83-json \
    php83-openssl \
    php83-pdo_sqlite \
    php83-zlib \
    php83-fileinfo \
    php83-opcache \
    php83-imap \
    php83-redis \
    php83-exif \
    php83-pecl-imagick \
    && rm -rf /var/cache/apk/* \
    && mkdir -p /etc/nginx/sites-available/

# 复制自定义 PHP 和 Nginx 配置文件
COPY wordpress/nginx.conf /etc/nginx/nginx.conf
COPY wordpress/php.ini /etc/php83/php.ini
COPY wordpress/www.conf /etc/php83/php-fpm.d/www.conf
COPY wordpress/default /etc/nginx/sites-available/default
# 复制 WordPress 文件和其他资源
COPY wordpress/config.php /app/wp-config.php
# 复制启动脚本
COPY wordpress/start.sh /start.sh

RUN chmod +x /start.sh \
    && mkdir -p /etc/nginx/sites-enabled \
    && ln -s /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default

# 曝光必要端口
EXPOSE 80

# 启动服务
CMD ["/start.sh"]

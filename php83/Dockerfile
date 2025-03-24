# 使用 PHP-FPM 的 Alpine 镜像
FROM php:8.3-fpm-alpine

# 设置工作目录
WORKDIR /app

RUN apk add --no-cache lighttpd \
    # 运行时依赖
    libpng \
    libwebp \
    libjpeg-turbo \
    icu \
    libzip \
    freetype \
    zip \
    # 构建依赖
    && apk add --no-cache --virtual .build-deps \
        autoconf \
        pkgconf \
        make \
        gcc \
        libc-dev \
        zlib-dev \
        libpng-dev \
        libwebp-dev \
        libjpeg-turbo-dev \
        icu-dev \
        libzip-dev \
        freetype-dev \
    # 安装 PHP 扩展
    && docker-php-ext-configure gd --with-freetype --with-jpeg --with-webp \
    && docker-php-ext-install -j$(nproc) \
        pdo_mysql \
        mysqli \
        gd \
        intl \
        opcache \
        zip \
        exif \
    && pecl install redis \
    && docker-php-ext-enable redis \
       exif \
    # 清理构建依赖
    && apk del .build-deps \
    && rm -rf /var/cache/apk/* /tmp/* \
    # PHP 配置优化
    && cp /usr/local/etc/php/php.ini-production /usr/local/etc/php/php.ini \
    && sed -i \
        -e 's/upload_max_filesize = .*/upload_max_filesize = 100M/' \
        -e 's/post_max_size = .*/post_max_size = 100M/' \
        /usr/local/etc/php/php.ini

# 配置 Lighttpd
COPY php83/lighttpd.conf /etc/lighttpd/lighttpd.conf

# 复制应用文件
COPY php83/index.php /app/index.php

# 设置权限
RUN chown -R www-data:www-data /app

# 暴露端口
EXPOSE 80

# 启动 Lighttpd 和 PHP-FPM
CMD ["sh", "-c", "php-fpm & lighttpd -D -f /etc/lighttpd/lighttpd.conf"]

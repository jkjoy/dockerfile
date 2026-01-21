FROM php:8.5-fpm-alpine

WORKDIR /app

# Install nginx and PHP dependencies, build extensions, then remove build deps
RUN apk add --no-cache --virtual .build-deps \
    libzip-dev \
    libpng-dev \
    libjpeg-turbo-dev \
    freetype-dev \
    libxml2-dev \
    libxslt-dev \
    icu-dev \
    oniguruma-dev \
    postgresql-dev \
    imagemagick-dev \
    autoconf \
    g++ \
    make \
    && apk add --no-cache \
    nginx \
    zip \
    curl \
    tidyhtml \
    mysql-client \
    && docker-php-ext-install -j$(nproc) zip gd intl pdo_mysql pdo_pgsql xsl tidy \
    && pecl install imagick redis \
    && docker-php-ext-enable imagick redis \
    && apk del .build-deps \
    && rm -rf /tmp/* /var/cache/apk/*

# Create nginx directories
RUN mkdir -p /etc/nginx/sites-enabled /run/nginx

# Copy configuration files
COPY --chown=www-data:www-data php85/nginx.conf /etc/nginx/nginx.conf
COPY --chown=www-data:www-data php85/php.ini /usr/local/etc/php/php.ini
COPY --chown=www-data:www-data php85/www.conf /usr/local/etc/php-fpm.d/www.conf
COPY --chown=www-data:www-data php85/default /etc/nginx/sites-available/default
COPY --chown=www-data:www-data php85/start.sh /start.sh

# Set up permissions
RUN chmod +x /start.sh \
    && chown -R www-data:www-data /app \
    && chown -R www-data:www-data /var/lib/nginx \
    && ln -sf /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default

EXPOSE 80 443

CMD ["/start.sh"]
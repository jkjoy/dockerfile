FROM php:8.5-fpm-alpine

WORKDIR /app

# Install nginx and PHP dependencies
RUN apk --no-cache add \
    nginx \
    libzip-dev \
    libpng-dev \
    libjpeg-turbo-dev \
    freetype-dev \
    libxml2-dev \
    libxslt-dev \
    icu-dev \
    imap-dev \
    imagemagick-dev \
    oniguruma-dev \
    postgresql-dev \
    mysql-client \
    zip \
    curl \
    tidyhtml

# Install PHP extensions (with error tolerance)
RUN docker-php-ext-configure gd --with-freetype --with-jpeg || true
RUN docker-php-ext-install zip || true
RUN docker-php-ext-install gd || true
RUN docker-php-ext-install imap || true
RUN docker-php-ext-install intl || true
RUN docker-php-ext-install pdo_mysql || true
RUN docker-php-ext-install pdo_pgsql || true
RUN docker-php-ext-install exif || true
RUN docker-php-ext-install xsl || true
RUN docker-php-ext-install tidy || true

# Install PECL extensions
RUN pecl install imagick && \
    docker-php-ext-enable imagick || true

RUN pecl install redis && \
    docker-php-ext-enable redis || true

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
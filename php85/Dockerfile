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
    libmagickwand-dev \
    libtidy-dev \
    oniguruma-dev \
    postgresql-dev \
    mysql-client \
    zip \
    curl \
    tidyhtml

# Install PHP extensions
RUN docker-php-ext-configure gd --with-freetype --with-jpeg \
    && docker-php-ext-install \
        pdo \
        pdo_mysql \
        pdo_pgsql \
        pdo_sqlite \
        zip \
        curl \
        gd \
        intl \
        xsl \
        mbstring \
        dom \
        opcache \
        imap \
        exif \
        ctype \
        tidy \
        tokenizer \
        xml \
        fileinfo

# Install PECL extensions
RUN pecl install imagick redis && \
    docker-php-ext-enable imagick redis

# Create nginx directories
RUN mkdir -p /etc/nginx/sites-enabled /run/nginx

# Copy configuration files
COPY nginx.conf /etc/nginx/nginx.conf
COPY php.ini /usr/local/etc/php/php.ini
COPY www.conf /usr/local/etc/php-fpm.d/www.conf
COPY default /etc/nginx/sites-available/default
COPY start.sh /start.sh

# Set up permissions
RUN chmod +x /start.sh \
    && chown -R www-data:www-data /app \
    && chown -R www-data:www-data /var/lib/nginx \
    && ln -sf /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default

EXPOSE 80 443

CMD ["/start.sh"]
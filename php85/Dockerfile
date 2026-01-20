FROM nginx:alpine

WORKDIR /app

# Install packages
RUN apk --no-cache add \
    php85 \
    php85-fpm \
    php85-pdo \
    php85-sqlite3 \
    php85-pdo_pgsql \
    php85-zip \
    php85-curl \
    php85-gd \
    php85-intl \
    php85-xsl \
    php85-mbstring \
    php85-dom \
    php85-json \
    php85-openssl \
    php85-pdo_sqlite \
    php85-zlib \
    php85-fileinfo \
    php85-opcache \
    php85-imap \
    php85-exif \
    php85-pecl-imagick \
    php85-ctype \
    php85-tidy \
    php85-tokenizer \
    php85-session \
    php85-pdo_mysql \
    php85-pecl-redis \
    php85-xml \
    php85-iconv \
    && mkdir -p /etc/nginx/sites-enabled \
    && rm -rf /var/cache/apk/*

# Copy configuration files
COPY php85/nginx.conf /etc/nginx/nginx.conf
COPY php85/php.ini /etc/php85/php.ini
COPY php85/www.conf /etc/php85/php-fpm.d/www.conf
COPY php85/default /etc/nginx/sites-available/default
COPY php85/start.sh /start.sh

# Set up permissions and symlinks in a single layer
RUN chmod +x /start.sh \
    && chown -R nginx:nginx /app \
    && ln -sf /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default

EXPOSE 80 443

CMD ["/start.sh"]
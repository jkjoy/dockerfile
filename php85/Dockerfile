FROM nginx:alpine

WORKDIR /app

# Install PHP and dependencies
RUN apk update && apk --no-cache add \
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
    php85-ctype \
    php85-tidy \
    php85-tokenizer \
    php85-session \
    php85-pdo_mysql \
    php85-xml \
    php85-iconv \
    php85-pecl-redis \
    unzip \
    curl

# Create nginx directories
RUN mkdir -p /etc/nginx/sites-enabled /run/php-fpm

# Copy configuration files
COPY --chown=nginx:nginx php85/nginx.conf /etc/nginx/nginx.conf
COPY --chown=nginx:nginx php85/php.ini /etc/php85/php.ini
COPY --chown=nginx:nginx php85/www.conf /etc/php85/php-fpm.d/www.conf
COPY --chown=nginx:nginx php85/default /etc/nginx/sites-available/default
COPY --chown=nginx:nginx php85/start.sh /start.sh

# Set up permissions
RUN chmod +x /start.sh \
    && chown -R nginx:nginx /app \
    && ln -sf /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default

EXPOSE 80 443

CMD ["/start.sh"]

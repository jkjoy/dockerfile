FROM nginx:alpine

WORKDIR /app

# Install packages
RUN apk --no-cache add \
    php83 \
    php83-fpm \
    php83-pdo \
    php83-sqlite3 \
    php83-pdo_pgsql \
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
    php83-exif \
    php83-pecl-imagick \
    php83-ctype \
    php83-tidy \
    php83-tokenizer \
    php83-session \
    php83-pdo_mysql \
    php83-pecl-redis \
    php83-xml \
    php83-xmlwriter \
    php83-xmlreader \
    php83-simplexml \
    php83-iconv \
    && mkdir -p /etc/nginx/sites-enabled \
    && rm -rf /var/cache/apk/*

# Copy configuration files
COPY hadsky/nginx.conf /etc/nginx/nginx.conf
COPY hadsky/php.ini /etc/php83/php.ini
COPY hadsky/www.conf /etc/php83/php-fpm.d/www.conf
COPY hadsky/default /etc/nginx/sites-available/default
COPY hadsky/start.sh /start.sh


# Set up permissions and symlinks in a single layer
RUN chmod +x /start.sh \
    && ln -sf /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default

EXPOSE 80

CMD ["/start.sh"]
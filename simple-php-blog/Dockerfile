# syntax=docker/dockerfile:1
FROM alpine:3.22 AS source

# Pin the default build to the same revision as the local blog project.
ARG BLOG_REF=bd79a54eaf5967d41ea2178f88ba2ac3b1076638
RUN apk add --no-cache ca-certificates curl tar \
    && curl --fail --location --retry 3 \
        "https://github.com/jkjoy/Simple-PHP-Blog/archive/${BLOG_REF}.tar.gz" \
        --output /tmp/blog.tar.gz \
    && mkdir /source /app \
    && tar -xzf /tmp/blog.tar.gz --strip-components=1 -C /source \
    && cd /source \
    && cp .htaccess index.php install.php router.php update.php favicon.png logo.png /app/ \
    && cp -a assets plugins themes /app/

FROM php:8.4-apache-bookworm

ARG BLOG_REF=bd79a54eaf5967d41ea2178f88ba2ac3b1076638
LABEL org.opencontainers.image.title="Simple PHP Blog" \
      org.opencontainers.image.source="https://github.com/jkjoy/Simple-PHP-Blog" \
      org.opencontainers.image.revision="${BLOG_REF}"

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates curl libonig-dev libsqlite3-dev libzip-dev \
    && docker-php-ext-install -j"$(nproc)" mbstring pdo_sqlite zip \
    && a2enmod rewrite \
    && rm -rf /var/lib/apt/lists/*

COPY apache.conf /etc/apache2/conf-available/blog.conf
COPY php.ini /usr/local/etc/php/conf.d/blog.ini
COPY --from=source --chown=www-data:www-data /app/ /var/www/html/
COPY --from=source /app/plugins/ /usr/src/blog-plugins/
COPY --chmod=755 entrypoint.sh /usr/local/bin/blog-entrypoint

RUN a2enconf blog \
    && mkdir -p /var/www/html/data /var/www/html/cache /var/www/html/uploads \
    && chown -R www-data:www-data /var/www/html

WORKDIR /var/www/html
VOLUME ["/var/www/html/data", "/var/www/html/cache", "/var/www/html/uploads", "/var/www/html/themes", "/var/www/html/plugins"]
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD curl --fail --silent --show-error --location --max-time 4 http://127.0.0.1/ > /dev/null || exit 1
ENTRYPOINT ["blog-entrypoint"]
CMD ["apache2-foreground"]

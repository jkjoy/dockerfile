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
    && cp index.php install.php router.php update.php favicon.png logo.png /app/ \
    && cp -a assets plugins /app/ \
    && mkdir /app/themes

FROM alpine:3.22

ARG BLOG_REF=bd79a54eaf5967d41ea2178f88ba2ac3b1076638
LABEL org.opencontainers.image.title="Simple PHP Blog" \
      org.opencontainers.image.source="https://github.com/jkjoy/Simple-PHP-Blog" \
      org.opencontainers.image.revision="${BLOG_REF}"

RUN apk add --no-cache \
        ca-certificates php84 php84-curl php84-fileinfo php84-mbstring \
        php84-opcache php84-openssl php84-pdo_sqlite php84-session php84-zip \
        su-exec tini \
    && adduser -S -D -H -u 82 -G www-data www-data \
    && ln -s /usr/bin/php84 /usr/local/bin/php

COPY php.ini /etc/php84/conf.d/99-blog.ini
COPY --from=source --chown=www-data:www-data /app/ /app/
COPY --from=source /app/plugins/ /usr/src/blog-plugins/
COPY --chmod=755 entrypoint.sh /usr/local/bin/blog-entrypoint

RUN mkdir -p /app/data /app/cache /app/uploads \
    && mkdir -m 700 /tmp/php-sessions \
    && chown www-data:www-data /tmp/php-sessions \
    && chown -R www-data:www-data /app

ENV PHP_CLI_SERVER_WORKERS=4
WORKDIR /app
VOLUME ["/app/data", "/app/cache", "/app/uploads", "/app/themes", "/app/plugins"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD php -r '$context = stream_context_create(["http" => ["timeout" => 3]]); exit(@file_get_contents("http://127.0.0.1:8080/", false, $context) === false ? 1 : 0);'
ENTRYPOINT ["tini", "-g", "--", "blog-entrypoint"]
CMD ["php", "-S", "0.0.0.0:8080", "-t", "/app", "/app/router.php"]

#!/bin/sh
set -eu

if [ "${1:-}" = php ] && [ "${2:-}" = -S ]; then
    for directory in data cache uploads themes plugins; do
        mkdir -p "/var/www/html/$directory"
    done

    # Empty bind mounts also need the bundled language plugins. Preserve updates.
    for plugin in english-language russian-language; do
        if [ ! -e "/var/www/html/plugins/$plugin" ]; then
            cp -a "/usr/src/blog-plugins/$plugin" /var/www/html/plugins/
        fi
    done

    mkdir -p /tmp/php-sessions
    if [ "$(id -u)" = 0 ]; then
        chown -R www-data:www-data \
            /var/www/html/data /var/www/html/cache /var/www/html/uploads \
            /var/www/html/themes /var/www/html/plugins /tmp/php-sessions
        exec su-exec www-data "$@"
    fi
fi

exec "$@"

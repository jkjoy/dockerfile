#!/bin/sh
set -eu

if [ "${1:-}" = php ] && [ "${2:-}" = -S ]; then
    for directory in data cache uploads themes plugins; do
        mkdir -p "/app/$directory"
    done

    # Empty bind mounts also need the bundled language plugins. Preserve updates.
    for plugin in english-language russian-language; do
        if [ ! -e "/app/plugins/$plugin" ]; then
            cp -a "/usr/src/blog-plugins/$plugin" /app/plugins/
        fi
    done

    mkdir -p /tmp/php-sessions
    if [ "$(id -u)" = 0 ]; then
        chown -R www-data:www-data \
            /app/data /app/cache /app/uploads \
            /app/themes /app/plugins /tmp/php-sessions
        exec su-exec www-data "$@"
    fi
fi

exec "$@"

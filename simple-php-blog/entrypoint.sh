#!/bin/sh
set -eu

if [ "${1:-}" = 'apache2-foreground' ]; then
    for directory in data cache uploads themes plugins; do
        mkdir -p "/var/www/html/$directory"
    done

    # Empty bind mounts also need the bundled language plugins. Preserve updates.
    for plugin in english-language russian-language; do
        if [ ! -e "/var/www/html/plugins/$plugin" ]; then
            cp -a "/usr/src/blog-plugins/$plugin" /var/www/html/plugins/
        fi
    done

    chown -R www-data:www-data \
        /var/www/html/data /var/www/html/cache /var/www/html/uploads \
        /var/www/html/themes /var/www/html/plugins
fi

exec docker-php-entrypoint "$@"

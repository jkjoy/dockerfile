#!/usr/bin/env bash
set -euo pipefail

image=${1:?Usage: bash test.sh IMAGE}
prefix="sblog-smoke-$(date +%s)-$$-${RANDOM}"
container="${prefix}-web"
work_dir=$(mktemp -d)
volumes=()
mounts=()

cleanup() {
    local status=$?
    trap - EXIT
    if (( status != 0 )); then
        docker logs "$container" >&2 || true
        docker inspect "$container" --format '{{json .State}}' >&2 || true
        for response in "$work_dir"/*.html; do
            [[ -f "$response" ]] || continue
            printf '\n--- %s ---\n' "${response##*/}" >&2
            tail -c 12000 "$response" >&2
        done
    fi
    docker rm -fv "$container" >/dev/null 2>&1 || true
    for volume in "${volumes[@]}"; do
        docker volume rm "$volume" >/dev/null 2>&1 || true
    done
    rm -rf "$work_dir"
    exit "$status"
}
trap cleanup EXIT

for directory in data cache uploads themes plugins; do
    volume="${prefix}-${directory}"
    docker volume create "$volume" >/dev/null
    volumes+=("$volume")
    mounts+=(--mount "type=volume,source=${volume},target=/var/www/html/${directory}")
done

start_container() {
    local publish=$1
    docker run -d --name "$container" \
        --publish "$publish" "${mounts[@]}" "$image" >/dev/null
    for ((attempt=0; attempt<60; attempt++)); do
        health=$(docker inspect "$container" --format '{{.State.Health.Status}}')
        if [[ "$health" == healthy ]]; then
            return
        fi
        if [[ "$(docker inspect "$container" --format '{{.State.Running}}')" != true ]]; then
            printf 'Container stopped before becoming healthy.\n' >&2
            return 1
        fi
        sleep 2
    done
    printf 'Container did not become healthy within 120 seconds.\n' >&2
    return 1
}

start_container '127.0.0.1::8080'
host_port=$(docker port "$container" 8080/tcp | sed -n 's/^127\.0\.0\.1://p')
[[ "$host_port" =~ ^[0-9]+$ ]]
base_url="http://127.0.0.1:${host_port}"

printf 'Checking PHP extensions and runtime permissions...\n'
docker exec "$container" php -r '
foreach (["pdo_sqlite", "curl", "zip", "fileinfo", "mbstring", "session", "openssl"] as $extension) {
    if (!extension_loaded($extension)) {
        fwrite(STDERR, "Missing PHP extension: $extension\n"); exit(1);
    }
}
if (!in_array("sqlite", PDO::getAvailableDrivers(), true)) { exit(1); }
'
docker exec -i "$container" php <<'PHP'
<?php
$servers = 0;
foreach (glob('/proc/[0-9]*/cmdline') as $file) {
    $arguments = explode("\0", (string)@file_get_contents($file));
    if (!in_array(basename($arguments[0] ?? ''), ['php', 'php84'], true)
        || !in_array('-S', $arguments, true)) { continue; }
    $status = (string)file_get_contents(dirname($file) . '/status');
    if (!preg_match('/^Uid:\s+(\d+)\s+(\d+)/m', $status, $uid)
        || (int)$uid[1] === 0 || (int)$uid[2] === 0) {
        fwrite(STDERR, "A PHP web process runs as root.\n"); exit(1);
    }
    $servers++;
}
if ($servers < 2) { fwrite(STDERR, "Expected multiple PHP CLI web workers.\n"); exit(1); }
PHP
docker exec --user www-data "$container" php -r '
foreach (["data", "cache", "uploads", "themes", "plugins"] as $directory) {
    $probe = "/var/www/html/$directory/.smoke-write";
    if (file_put_contents($probe, "writable") === false || !unlink($probe)) { exit(1); }
}
'

printf 'Installing a fresh blog...\n'
curl -fsS --max-time 20 "$base_url/install.php?lang=en" -o "$work_dir/environment.html"
grep -Fq 'This server meets the installation requirements.' "$work_dir/environment.html"
if grep -Fq 'class="environment-check is-error"' "$work_dir/environment.html"; then
    printf 'At least one installer environment check failed.\n' >&2
    exit 1
fi
password="Test-${RANDOM}-${RANDOM}-${RANDOM}"
curl -fsS --max-time 30 "$base_url/install.php" \
    --data-urlencode 'lang=en' \
    --data-urlencode 'site_name=Container smoke test' \
    --data-urlencode 'admin_username=smoke-admin' \
    --data-urlencode 'admin_email=admin@example.com' \
    --data-urlencode "admin_password=$password" \
    --data-urlencode "admin_password2=$password" \
    -o "$work_dir/installed.html"
grep -Fq 'Installation complete' "$work_dir/installed.html"
curl -fsS --max-time 20 "$base_url/" -o "$work_dir/home.html"
grep -Fq 'Hello World' "$work_dir/home.html"
curl -fsS --max-time 20 "$base_url/assets/index.css" -o "$work_dir/index.css"
[[ -s "$work_dir/index.css" ]]
curl -fsS --max-time 20 "$base_url/archives" -o "$work_dir/archives.html"
grep -Fq 'Hello World' "$work_dir/archives.html"

printf 'Checking administrator login and sessions across concurrent requests...\n'
cookie_jar="$work_dir/cookies.txt"
curl -fsS --max-time 20 --cookie-jar "$cookie_jar" \
    "$base_url/login" -o "$work_dir/login.html"
csrf=$(sed -n '/name="csrf_token" value="/ { s/.*name="csrf_token" value="\([a-f0-9]\{32\}\)".*/\1/p; q; }' "$work_dir/login.html")
[[ "$csrf" =~ ^[a-f0-9]{32}$ ]]
curl -fsS --max-time 20 --cookie "$cookie_jar" --cookie-jar "$cookie_jar" \
    --data-urlencode "csrf_token=$csrf" \
    --data-urlencode 'username=smoke-admin' \
    --data-urlencode "password=$password" \
    --dump-header "$work_dir/login.headers" \
    "$base_url/login" -o "$work_dir/login-result.html"
grep -Eq '^HTTP/[0-9.]+ 30[23]' "$work_dir/login.headers"
requests=()
for ((request=0; request<12; request++)); do
    curl -fsS --max-time 30 --cookie "$cookie_jar" \
        "$base_url/admin" -o "$work_dir/admin-${request}.html" &
    requests+=("$!")
done
for request in "${requests[@]}"; do
    wait "$request"
done
admin_csrf=''
for ((request=0; request<12; request++)); do
    grep -Fq 'id="admin-sidebar"' "$work_dir/admin-${request}.html"
    token=$(sed -n '/name="csrf_token" value="/ { s/.*name="csrf_token" value="\([a-f0-9]\{32\}\)".*/\1/p; q; }' "$work_dir/admin-${request}.html")
    [[ "$token" =~ ^[a-f0-9]{32}$ ]]
    if [[ -n "$admin_csrf" && "$admin_csrf" != "$token" ]]; then
        printf 'Concurrent requests did not share the same PHP session.\n' >&2
        exit 1
    fi
    admin_csrf=$token
done

printf 'Checking private paths with real canary files...\n'
docker exec -i --user www-data "$container" php <<'PHP'
<?php
$root = '/var/www/html';
foreach (['themes/smoke-private', 'plugins/smoke-private'] as $directory) {
    if (!mkdir("$root/$directory", 0755, true) && !is_dir("$root/$directory")) { exit(1); }
}
foreach (['.smoke-secret.txt', 'data/smoke-secret.txt', 'cache/smoke-secret.txt',
    'themes/smoke-private/secret.php', 'plugins/smoke-private/secret.php'] as $file) {
    if (file_put_contents("$root/$file", 'private-canary') === false) { exit(1); }
}
foreach (['themes/smoke-private/public.css', 'plugins/smoke-private/public.css'] as $file) {
    if (file_put_contents("$root/$file", '/* public-canary */') === false) { exit(1); }
}
if (file_put_contents("$root/uploads/smoke-persistent.txt", 'persistent-upload') === false) { exit(1); }
PHP
for path in \
    '/data/config.php' '/cache/settings.php' '/.git/config' \
    '/plugins/english-language/plugin.php' '/.smoke-secret.txt' \
    '/data/smoke-secret.txt' '/cache/smoke-secret.txt' \
    '/themes/smoke-private/secret.php' '/plugins/smoke-private/secret.php'; do
    code=$(curl -sS --max-time 15 --output "$work_dir/private.html" --write-out '%{http_code}' "$base_url$path")
    if [[ "$code" != 403 && "$code" != 404 ]]; then
        printf 'Private path %s returned HTTP %s.\n' "$path" "$code" >&2
        exit 1
    fi
done
for path in '/themes/smoke-private/public.css' '/plugins/smoke-private/public.css'; do
    curl -fsS --max-time 15 "$base_url$path" -o "$work_dir/public.css"
    grep -Fxq '/* public-canary */' "$work_dir/public.css"
done
for path in '/d%61ta/smoke-secret.txt' '/%2esmoke-secret.txt' \
    '/plugins/smoke-private/secret%2ephp' '/themes%2fsmoke-private%2fsecret.php'; do
    code=$(curl -sS --path-as-is --max-time 15 --output "$work_dir/private.html" \
        --write-out '%{http_code}' "$base_url$path")
    if [[ "$code" != 404 ]]; then
        printf 'Encoded private path %s returned HTTP %s.\n' "$path" "$code" >&2
        exit 1
    fi
done

printf 'Recreating the container with the same persistent volumes...\n'
docker stop --time 10 "$container" >/dev/null
if [[ "$(docker inspect "$container" --format '{{.State.ExitCode}}')" == 137 ]]; then
    printf 'Container required SIGKILL instead of stopping on SIGTERM.\n' >&2
    exit 1
fi
docker rm -v "$container" >/dev/null
start_container "127.0.0.1:${host_port}:8080"
curl -fsS --max-time 20 "$base_url/" -o "$work_dir/recreated.html"
grep -Fq 'Hello World' "$work_dir/recreated.html"
grep -Fq 'Container smoke test' "$work_dir/recreated.html"
curl -fsS --max-time 20 "$base_url/install.php?lang=en" -o "$work_dir/locked.html"
grep -Fq 'Installation locked' "$work_dir/locked.html"
curl -fsS --max-time 20 "$base_url/uploads/smoke-persistent.txt" -o "$work_dir/upload.txt"
grep -Fxq 'persistent-upload' "$work_dir/upload.txt"
docker exec -i --user www-data "$container" php <<'PHP'
<?php
$root = '/var/www/html';
foreach (['data/install.lock', 'cache/settings.php', 'themes/smoke-private/secret.php',
    'plugins/smoke-private/secret.php', 'plugins/english-language/plugin.php',
    'plugins/russian-language/plugin.php'] as $file) {
    if (!is_file("$root/$file")) { fwrite(STDERR, "Missing persistent file: $file\n"); exit(1); }
}
$config = require "$root/data/config.php";
$db = new PDO('sqlite:' . "$root/data/" . basename($config['db_file']));
$name = $db->query("SELECT value FROM settings WHERE name = 'site_name'")->fetchColumn();
if ($name !== 'Container smoke test') { fwrite(STDERR, "Site settings were not preserved.\n"); exit(1); }
PHP
printf 'Container smoke test passed: nonroot workers, sessions, installation, routing, access protection, shutdown, and persistence.\n'

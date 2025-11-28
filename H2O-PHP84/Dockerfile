FROM alpine:latest AS builder

# ----- Build H2O -----
RUN apk add --no-cache \
    git \
    build-base \
    cmake \
    openssl-dev \
    zlib-dev \
    libuv-dev \
    readline-dev \
    linux-headers \
    ninja \
    autoconf \
    automake \
    libtool

WORKDIR /src
RUN git clone --depth=1 https://github.com/h2o/h2o.git .

RUN cmake -B build -GNinja \
    -DCMAKE_BUILD_TYPE=Release \
    -DWITH_BUNDLED_SSL=OFF \
    -DWITH_MRUBY=OFF \
    -DWITH_H2OLOG=OFF \
    -DWITH_LIBUV=ON \
    .

RUN cmake --build build --target h2o


# ======================
# === Runtime Image ====
# ======================
FROM alpine:latest

# 系统工具 + PHP 8.4
RUN apk add --no-cache \
    bash \
    ca-certificates \
    openssl \
    libuv \
    zlib \
    php84 php84-fpm php84-session php84-mysqli php84-json php84-opcache \
    php84-curl php84-xml php84-dom php84-simplexml php84-zip php84-gd \
    php84-mbstring php84-iconv php84-openssl php84-phar php84-ctype php84-fileinfo

# 复制 H2O
COPY --from=builder /usr/local/bin/h2o /usr/local/bin/h2o
COPY --from=builder /usr/local/share/h2o /usr/local/share/h2o

# 目录
RUN mkdir -p /app/www /app/certs /app/logs

# -------------------------------------
# PHP-FPM 8.4 配置
# -------------------------------------
RUN mkdir -p /etc/php84

# php-fpm.conf（不 daemonize）
RUN printf "\
[global]\n\
daemonize = no\n\
\n\
[www]\n\
user = nobody\n\
group = nobody\n\
listen = 127.0.0.1:9000\n\
listen.owner = nobody\n\
listen.group = nobody\n\
pm = dynamic\n\
pm.max_children = 10\n\
pm.start_servers = 2\n\
pm.min_spare_servers = 2\n\
pm.max_spare_servers = 5\n\
" > /etc/php84/php-fpm.conf

# php.ini（上传文件最大 100M）
RUN printf "\
file_uploads = On\n\
upload_max_filesize = 100M\n\
post_max_size = 100M\n\
memory_limit = 256M\n\
max_execution_time = 300\n\
max_input_time = 300\n\
date.timezone = UTC\n\
" > /etc/php84/php.ini


# -------------------------------------
# H2O 配置
# -------------------------------------
RUN printf "\
listen:\n\
  port: 80\n\
\n\
listen:\n\
  port: 443\n\
  ssl:\n\
    minimum-version: TLSv1.2\n\
    certificate-file: /app/certs/fullchain.pem\n\
    key-file: /app/certs/key.pem\n\
\n\
hosts:\n\
  default:\n\
    paths:\n\
      /:\n\
        file.dir: /app/www\n\
        # WordPress rewrite\n\
        redirect:\n\
          mruby.handler: |\n\
            Proc.new do |env|\n\
              path = env[\"PATH_INFO\"]\n\
              root = \"/app/www\"\n\
              if File.file?(\"#{root}#{path}\") || File.directory?(\"#{root}#{path}\")\n\
                [399, {}, []]\n\
              else\n\
                query = env[\"QUERY_STRING\"]\n\
                new_path = \"/index.php\"\n\
                new_path += \"?#{query}\" unless query.empty?\n\
                [307, { \"Location\" => new_path }, []]\n\
              end\n\
            end\n\
\n\
      \"\\.php$\":\n\
        fastcgi.connect:\n\
          host: 127.0.0.1\n\
          port: 9000\n\
        fastcgi.params:\n\
          SCRIPT_FILENAME: /app/www/%path\n\
" > /app/h2o.conf


# 启动脚本（同时启动 php-fpm + h2o）
RUN printf "\
#!/bin/sh\n\
php-fpm84 -c /etc/php84/php-fpm.conf &\n\
sleep 1\n\
exec h2o -c /app/h2o.conf\n\
" > /start.sh && chmod +x /start.sh

EXPOSE 80 443
CMD ["/start.sh"]

# ==========================
#   Build Stage
# ==========================
FROM alpine:latest AS builder

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


# ==========================
#   Runtime Stage
# ==========================
FROM alpine:latest AS runtime

RUN apk add --no-cache \
    openssl \
    libuv \
    zlib

# 复制 h2o
COPY --from=builder /src/build/h2o /usr/local/bin/h2o

# 创建最小配置
RUN mkdir -p /etc/h2o
RUN printf "\
listen:\n\
  port: 80\n\
\n\
hosts:\n\
  default:\n\
    paths:\n\
      /:\n\
        file.dir: /app\n\
" > /etc/h2o/h2o.conf

# 默认网站目录
RUN mkdir -p /app && \
    echo 'Hello from H2O!' > /app/index.html

EXPOSE 80
CMD ["h2o", "-c", "/etc/h2o/h2o.conf"]

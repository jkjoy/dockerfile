FROM alpine:latest AS builder

# Build dependencies
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

# 获取 H2O 源码
WORKDIR /src
RUN git clone --depth=1 https://github.com/h2o/h2o.git .

# 编译 H2O
RUN cmake -B build -GNinja \
    -DCMAKE_BUILD_TYPE=Release \
    -DWITH_BUNDLED_SSL=OFF \
    -DWITH_MRUBY=OFF \
    -DWITH_H2OLOG=OFF \
    -DWITH_LIBUV=ON \
    .
RUN cmake --build build --target h2o

FROM alpine:latest AS runtime

# 必需的运行时依赖
RUN apk add --no-cache \
    openssl \
    libuv \
    zlib

# 复制编译后文件
COPY --from=builder /src/build/h2o /usr/local/bin/h2o
COPY --from=builder /src/examples/h2o.conf /etc/h2o/h2o.conf

# 暴露端口
EXPOSE 80
EXPOSE 443

# 默认配置文件启动
CMD ["h2o", "-c", "/etc/h2o/h2o.conf"]

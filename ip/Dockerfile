# 构建阶段
FROM golang:1.24-alpine AS builder

WORKDIR /app

# 安装编译依赖
RUN apk add --no-cache git make

# 复制源码
COPY . .

# 下载依赖并构建
RUN go mod download
RUN CGO_ENABLED=0 go build -o geoip-api .

# 运行阶段
FROM alpine:3.18

WORKDIR /app

# 从构建阶段复制可执行文件
COPY --from=builder /app/geoip-api .

# 设置默认数据库路径（可通过 -v 挂载覆盖）
ENV CITY_DB_PATH=/data/GeoLite2-City.mmdb
ENV ASN_DB_PATH=/data/GeoLite2-ASN.mmdb
ENV QQWRY_PATH=/data/qqwry.dat
ENV PORT=8080

# 创建数据目录（确保权限正确）
RUN mkdir -p /data && \
    chown nobody:nobody /data && \
    chmod 755 /data

COPY /ip/data /app/data

# 切换到非root用户
USER nobody

EXPOSE $PORT

ENTRYPOINT ["/app/geoip-api"]
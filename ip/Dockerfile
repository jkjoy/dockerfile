# 基于官方 Golang 镜像构建
FROM golang:1.24-alpine AS builder

WORKDIR /app

# 复制 go.mod 和 go.sum 以利用缓存
COPY go.mod ./
RUN go mod download

# 复制源代码
COPY ip/. .

# 构建可执行文件
RUN go build -o ipserver main.go

# 使用更小的镜像运行
FROM alpine:latest

WORKDIR /app

# 拷贝可执行文件和数据文件
COPY --from=builder /app/ipserver .
COPY ip/data ./data

EXPOSE 8080

# 如果你的 main.go 监听其他端口，请修改 EXPOSE 和 CMD
CMD ["./ipserver"]
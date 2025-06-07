FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY ip/. .
RUN go mod download
RUN go build -o ipserver main.go
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/ipserver .
COPY ip/data ./data
EXPOSE 8080
CMD ["./ipserver"]
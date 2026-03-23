FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY qrcode/go.mod qrcode/go.sum ./
RUN go mod download
COPY qrcode/. .
RUN go build -o qrcode main.go

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/qrcode .
COPY --from=builder /app/index.html .
EXPOSE 8080
CMD ["./qrcode"]

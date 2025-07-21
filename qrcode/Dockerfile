FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY qrcode/. .
RUN go mod download
RUN go build -o qrcode main.go
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/qrcode .
#COPY qrcode/data ./data
EXPOSE 8080
CMD ["./qrcode"]
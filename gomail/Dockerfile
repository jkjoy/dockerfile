FROM golang:1.24-alpine AS builder
WORKDIR /src

COPY gomail/go.mod gomail/go.sum ./gomail/
WORKDIR /src/gomail
RUN go mod download

COPY gomail/*.go ./
RUN go build -o gomail .

FROM alpine:latest
WORKDIR /app

COPY --from=builder /src/gomail/gomail .
COPY ip/data ./data

EXPOSE 8080
CMD ["./gomail"]

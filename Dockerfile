# Stage 1: Build the bot service
FROM golang:1.22-alpine AS bot_builder

WORKDIR /app
COPY go.mod go.sum ./
COPY bot/ ./bot/
RUN cd bot && go build -o /bot

# Stage 2: Build the downloader service
FROM golang:1.22-alpine AS downloader_builder

WORKDIR /app
COPY go.mod go.sum ./
COPY downloader/ ./downloader/
RUN cd downloader && go build -o /downloader

# Stage 3: Build the admin service
FROM golang:1.22-alpine AS admin_builder

WORKDIR /app
COPY go.mod go.sum ./
COPY admin/ ./admin/
RUN cd admin && go build -o /admin

# Stage 4: Create the final image
FROM alpine:latest

WORKDIR /root/

COPY --from=bot_builder /bot /bot
COPY --from=downloader_builder /downloader /downloader
COPY --from=admin_builder /admin /admin

RUN apk add --no-cache ca-certificates youtube-dl sqlite

CMD ["sh"]

# Stage 1: Build the application
FROM golang:1.19-alpine AS builder

WORKDIR /app

COPY bot/ ./bot/
COPY downloader/ ./downloader/
COPY admin/ ./admin/

RUN cd bot && go build -o /bot
RUN cd downloader && go build -o /downloader
RUN cd admin && go build -o /admin

# Stage 2: Create the final image
FROM alpine:latest

WORKDIR /root/

COPY --from=builder /bot /bot
COPY --from=builder /downloader /downloader
COPY --from=builder /admin /admin

RUN apk add --no-cache ca-certificates youtube-dl sqlite

CMD ["sh"]

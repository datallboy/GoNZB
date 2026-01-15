# Stage 1: Build
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod download

# Inject version info during Docker build
ARG VERSION=dev
ARG BUILD_TIME=unknown
RUN go build -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" -o gonzb cmd/gonzb/main.go

# Stage 2: Run
FROM alpine:latest
WORKDIR /app
# Install certs for secure Usenet connections (TLS/SSL)
RUN apk --no-cache add \
    --repository=https://dl-cdn.alpinelinux.org/alpine/edge/testing/ \
    --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community/ \
    ca-certificates \
    par2cmdline-turbo \
    unzip \
    7zip

# Symlink unrar to 7z since Alpine doesn't package unrar
# 7z 'x' command is compatible with unrar extract
RUN ln -s /usr/bin/7z /usr/bin/unrar

# Create directories for config and download
RUN mkdir /config /downloads

COPY --from=builder /app/gonzb .

# Copy the EXAMPLE config so users can find it inside the container if needed
COPY config.yaml.example /app/config.yaml.example

# Default command
ENTRYPOINT ["./gonzb"]
CMD ["--config", "/config/config.yaml"]
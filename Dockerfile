# Stage 1: Build UI assets
FROM node:22-alpine AS ui-builder
WORKDIR /app/ui
COPY ui/package*.json ./
RUN npm ci
COPY ui ./
RUN npm run build

# Stage 2: Build binary
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
COPY --from=ui-builder /app/internal/webui/dist /app/internal/webui/dist
RUN go mod download

# Inject version info during Docker build
ARG VERSION=dev
ARG BUILD_TIME=unknown
RUN go build -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" -o gonzb cmd/gonzb/main.go

# Stage 3: Build real unrar for Alpine
FROM alpine:3.23 AS unrar-builder
RUN apk add --no-cache build-base make wget tar

WORKDIR /tmp/unrar

ARG UNRAR_URL=https://www.rarlab.com/rar/unrarsrc-7.2.4.tar.gz

RUN wget -O unrar.tar.gz "${UNRAR_URL}" \
    && tar -xzf unrar.tar.gz --strip-components=1 \
    && make -f makefile

# Stage 4: Run
FROM alpine:3.23
WORKDIR /app
# Install certs for secure Usenet connections (TLS/SSL)
RUN apk --no-cache add \
    --repository=https://dl-cdn.alpinelinux.org/alpine/edge/testing/ \
    --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community/ \
    ca-certificates \
    libstdc++ \
    par2cmdline-turbo \
    unzip \
    7zip \
    su-exec

COPY --from=unrar-builder /tmp/unrar/unrar /usr/bin/unrar
RUN chmod +x /usr/bin/unrar

COPY docker/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Create directories for config and download
RUN mkdir /config /downloads /completed
RUN mkdir -p /store/metadata /store/nzbs

COPY --from=builder /app/gonzb .

# Copy the EXAMPLE config so users can find it inside the container if needed
COPY config.yaml.example /app/config.yaml.example

# Default command
ENTRYPOINT ["/entrypoint.sh"]
CMD ["--config", "/config/config.yaml"]

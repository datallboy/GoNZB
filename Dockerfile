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
ARG UNRAR_SHA256=b02e571a33af7711cd803080500370dc1d28eea82b2032480819d27462ad8b31

RUN wget -O unrar.tar.gz "${UNRAR_URL}" \
    && echo "${UNRAR_SHA256}  unrar.tar.gz" | sha256sum -c - \
    && tar -xzf unrar.tar.gz --strip-components=1 \
    && make -f makefile

# Stage 4: Run
FROM alpine:3.23
WORKDIR /app
# Install certs for secure Usenet connections (TLS/SSL)
RUN apk --no-cache add \
    ca-certificates=20260611-r0 \
    libstdc++=15.2.0-r2 \
    unzip=6.0-r16 \
    7zip=25.01-r0 \
    su-exec=0.3-r0 \
    && apk --no-cache add \
    --repository=https://dl-cdn.alpinelinux.org/alpine/edge/testing/ \
    par2cmdline-turbo=1.4.0-r0
RUN ln -s /usr/bin/7zz /usr/bin/7za

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

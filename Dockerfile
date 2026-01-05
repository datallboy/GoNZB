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
RUN apk --no-cache add ca-certificates

# Create directories for config and download
RUN mkdir /config /downloads

COPY --from=builder /app/gonzb .

# Copy the EXAMPLE config so users can find it inside the container if needed
COPY config.yaml.example /app/config.yaml.example

# Default command
ENTRYPOINT ["./gonzb"]
CMD ["--config", "/config/config.yaml"]
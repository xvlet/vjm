# ==========================================
# Stage 1: Builder
# ==========================================
FROM golang:1.25.12-alpine AS builder

# Install necessary system packages for build (timezone data, certificates, git)
RUN apk update && apk add --no-cache git tzdata ca-certificates

WORKDIR /app

# Copy go.mod and go.sum first to cache module dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and build
COPY . .
ARG VERSION=dev
# CGO_ENABLED=0: Build as statically linked binary to remove external C library dependencies
# -s -w: Remove debugging symbols to further reduce binary size
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.Version=${VERSION}" -o vjm ./cmd/vjm

# ==========================================
# Stage 2: Final (Minimal Image)
# ==========================================
FROM alpine:3.19

# Copy timezone data and root certificates from builder (for HTTPS communication and time synchronization)
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the compiled executable from builder stage
COPY --from=builder /app/vjm /usr/local/bin/vjm

# Set default timezone to UTC,
# This allows overriding via the -e TZ=Asia/Seoul option when running the container
ENV TZ=UTC

# Set entrypoint to run the application
ENTRYPOINT ["vjm"]
# Set default command to show help if no parameters are provided
CMD ["-h"]

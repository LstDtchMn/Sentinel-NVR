# ---- Build Stage ----
FROM golang:1.24.7-bookworm AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /sentinel ./cmd/sentinel

# ---- Runtime Stage ----
# Based on Debian bookworm for FFmpeg and GPU driver compatibility.
# GPU support:
#   - NVIDIA: Use the nvidia-container-toolkit (docker --gpus all)
#   - Intel:  Mount /dev/dri for VAAPI/QSV access
#   - Coral:  Mount /dev/bus/usb for USB TPU
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    # Intel GPU (VAAPI / QSV) drivers
    vainfo \
    # wget is needed by the Docker health check (docker-compose.yml)
    wget \
    # Shared libs needed at runtime
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user with fixed UID/GID for volume permission consistency.
# Must match the chown instructions in docker-compose.yml (UID 10001).
RUN groupadd -r -g 10001 sentinel && useradd -r -u 10001 -g sentinel -s /bin/false sentinel

# Copy binary
COPY --from=builder /sentinel /usr/local/bin/sentinel

# Default directories
RUN mkdir -p /etc/sentinel /data /media/hot /media/cold \
    && chown -R sentinel:sentinel /etc/sentinel /data /media

# Removed COPY configs/sentinel.yml since it is mounted via docker-compose

USER sentinel

EXPOSE 8099

ENTRYPOINT ["sentinel"]
CMD ["-config", "/etc/sentinel/sentinel.yml"]

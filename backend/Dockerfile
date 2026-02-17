# ---- Build Stage ----
FROM golang:1.23-bookworm AS builder

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
    intel-media-va-driver-non-free \
    vainfo \
    # Shared libs needed at runtime
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -r -s /bin/false sentinel

# Copy binary
COPY --from=builder /sentinel /usr/local/bin/sentinel

# Default directories
RUN mkdir -p /etc/sentinel /data /media/hot /media/cold \
    && chown -R sentinel:sentinel /data /media

COPY --chown=sentinel:sentinel configs/sentinel.yml /etc/sentinel/sentinel.yml

USER sentinel

EXPOSE 8099

ENTRYPOINT ["sentinel"]
CMD ["-config", "/etc/sentinel/sentinel.yml"]

# syntax=docker/dockerfile:1

# ----------------------------------------
# Build stage
# ----------------------------------------
FROM golang:1.26.2-bookworm AS builder

WORKDIR /src

# Download dependencies first for better layer caching.
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source tree and build a static binary.
# CGO_ENABLED=0 is a hard project constraint (pure-Go SQLite).
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/serenebach ./cmd/serenebach

# ----------------------------------------
# Helper stage: create empty data directories with correct ownership.
# ----------------------------------------
FROM builder AS dirs
RUN mkdir -p /data/img /data/templates /data/public && \
    chown -R 65532:65532 /data

# ----------------------------------------
# Runtime stage
# ----------------------------------------
# distroless/static is the smallest secure base for Go static binaries.
# The :nonroot tag runs as UID 65532 (no shell, minimal attack surface).
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /home/nonroot

# Copy the compiled binary.
COPY --from=builder --chown=nonroot:nonroot /bin/serenebach /usr/local/bin/serenebach

# Pre-create the data directory tree so SQLite and uploads work
# even when the container is run without an explicit volume mount.
# In production you should still mount a volume here for persistence.
COPY --from=dirs --chown=nonroot:nonroot /data /home/nonroot/data

# Default paths (override at runtime if desired).
ENV SB_DB=/home/nonroot/data/serenebach.db
ENV SB_IMAGE_DIR=/home/nonroot/data/img
ENV SB_TEMPLATE_DIR=/home/nonroot/data/templates
ENV SB_REBUILD_OUT=/home/nonroot/data/public

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/serenebach"]
CMD ["serve"]

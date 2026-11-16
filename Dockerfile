# Meerkat CRM - All-in-one image
#
# One Go binary serves the React bundle, the API, CardDAV and /health. No nginx, no
# supervisord: the process the image starts is the application.
#
# Build context is the repository root:
#   docker build -t meerkat-crm .

# =============================================================================
# Stage 1: Build the Go backend (cross-compiled, CGO disabled - pure-Go SQLite)
# =============================================================================
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend-builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

# Copy go mod files first for better caching
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# Copy backend source code
COPY backend/ .

# Build the application (glebarez/sqlite is pure Go (no CGO, i.e. without QEMU)
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o meerkat .

# =============================================================================
# Stage 2: Build the React frontend (same-origin API, served by the Go binary)
# =============================================================================
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder

WORKDIR /app

# Copy package files first for better caching
COPY frontend/package.json frontend/yarn.lock* frontend/package-lock.json* ./

# Install dependencies
RUN if [ -f yarn.lock ]; then yarn install --frozen-lockfile; \
    elif [ -f package-lock.json ]; then npm ci; \
    else npm install; fi

# Copy frontend source code
COPY frontend/ .

# The API is same-origin: the binary below answers /api/ itself. Must stay empty so the
# bundle uses relative URLs.
ENV REACT_APP_API_URL=""

RUN if [ -f yarn.lock ]; then yarn build; else npm run build; fi

# =============================================================================
# Stage 3: Runtime - one unprivileged process, listening on PORT
# =============================================================================
FROM alpine:3.24

# No sqlite package needed - the backend uses a pure-Go SQLite driver.
RUN apk add --no-cache \
    ca-certificates \
    tzdata

WORKDIR /app

# Nothing in the image runs as root, so there is no entrypoint to remap a uid or chown a
# volume: run the container as whoever owns the mounted directories.
RUN addgroup -g 1001 -S appgroup && adduser -u 1001 -S appuser -G appgroup

# Both data directories exist in the image, owned by the run-as uid, so that Docker seeds
# that ownership into a fresh named volume. A bind mount gets the host's ownership instead
# and has to be chowned on the host.
RUN mkdir -p /app/data /app/static/photos && \
    chown -R appuser:appgroup /app/data /app/static

# The binary and the bundle it serves
COPY --from=backend-builder /app/meerkat /app/meerkat
COPY --from=frontend-builder /app/build /app/frontend

# Default environment
ENV PORT=8080
ENV SQLITE_DB_PATH=/app/data/meerkat.db
ENV PROFILE_PHOTO_DIR=/app/static/photos
ENV FRONTEND_DIR=/app/frontend
ENV GIN_MODE=release

# The bundle is read, never written; root-owned 0644 is enough.
USER 1001:1001

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:8080/health || exit 1

# CMD rather than ENTRYPOINT, so `docker run --entrypoint sh` can open the container.
CMD ["/app/meerkat"]

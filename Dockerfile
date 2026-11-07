# Estágio 1: Build do Frontend (React + TypeScript + Vite)
FROM node:22-alpine AS frontend-builder
WORKDIR /build/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Estágio 2: Build do Backend (Go 1.23)
FROM golang:1.23-alpine AS backend-builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/chat-api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/userctl ./cmd/userctl

# Estágio 3: Runtime mínimo e não-root
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata wget && \
    addgroup -S chatgroup && adduser -S chatuser -G chatgroup

WORKDIR /app
RUN mkdir -p /app/uploads /app/public && chown -R chatuser:chatgroup /app

COPY --from=backend-builder /out/chat-api /app/chat-api
COPY --from=backend-builder /out/userctl /app/userctl
COPY --from=frontend-builder /build/frontend/dist /app/public

USER chatuser

ENV APP_ENV=production \
    PORT=8080 \
    STATIC_FILES_DIR=/app/public \
    UPLOAD_STORAGE_DIR=/app/uploads \
    SECURE_COOKIES=true \
    COOKIE_SAME_SITE=none \
    SEED_DEV_USERS=false

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=3 \
  CMD wget -qO- http://127.0.0.1:${PORT:-8080}/readyz || exit 1

ENTRYPOINT ["/app/chat-api"]


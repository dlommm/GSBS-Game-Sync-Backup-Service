# GSBS Server — Docker image
# Multi-stage: build the Go binary (CGO for SQLite), then run in a minimal image.
FROM golang:1.25-alpine AS builder
WORKDIR /app

# SQLite driver needs CGO and Alpine build deps
RUN apk add --no-cache gcc musl-dev sqlite-dev

# Copy module files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG BUILD_DATE=
ARG COMMIT=
# WebUI embedded assets (Tailwind CSS, favicon)
RUN apk add --no-cache nodejs npm bash \
  && go run ./cmd/resize-icon \
  && ./script/build-webui.sh
RUN CGO_ENABLED=1 go build \
  -ldflags "-X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE} -X main.Commit=${COMMIT}" \
  -o /gsbs-server ./server

# Runtime image (Alpine for small size; SQLite binary may link against libsqlite3)
FROM alpine:3.19
RUN apk add --no-cache ca-certificates sqlite-libs wget
# Optional: add user for non-root run (uncomment if desired)
# RUN adduser -D -g '' appuser
# USER appuser

WORKDIR /app
COPY --from=builder /gsbs-server .

# Default port; override with GSBS_ADDR if needed
EXPOSE 8080

ENTRYPOINT ["/app/gsbs-server"]

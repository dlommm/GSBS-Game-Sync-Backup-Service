# GSBS Server — Docker image
# Multi-stage: build the Go binary (CGO for SQLite), then run in a minimal image.
FROM golang:1.24-alpine AS builder
WORKDIR /app

# SQLite driver needs CGO and Alpine build deps
RUN apk add --no-cache gcc musl-dev sqlite-dev

# Copy module files first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /gsbs-server ./server

# Runtime image (Alpine for small size; SQLite binary may link against libsqlite3)
FROM alpine:3.19
RUN apk add --no-cache ca-certificates sqlite-libs
# Optional: add user for non-root run (uncomment if desired)
# RUN adduser -D -g '' appuser
# USER appuser

WORKDIR /app
COPY --from=builder /gsbs-server .

# Default port; override with GSBS_ADDR if needed
EXPOSE 8080

ENTRYPOINT ["/app/gsbs-server"]

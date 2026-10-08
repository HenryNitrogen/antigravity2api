# Multi-stage build Dockerfile (supports building on server with Go 1.22+)
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY . .

# Build static Linux binary inside container
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /app/antigravity2api ./cmd/server

# Final lightweight production image
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/antigravity2api /app/antigravity2api

EXPOSE 8088
ENV PORT=8088
ENV HOST=0.0.0.0

ENTRYPOINT ["/app/antigravity2api"]

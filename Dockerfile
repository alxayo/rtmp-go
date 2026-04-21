# Multi-stage build for the RTMP server
FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod ./
# No go.sum — standard library only, no external dependencies
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /rtmp-server ./cmd/rtmp-server

# Runtime: minimal image with CA certificates
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /rtmp-server /rtmp-server

EXPOSE 1935

ENTRYPOINT ["/rtmp-server"]

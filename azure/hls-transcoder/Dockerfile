# Multi-stage build for the HLS transcoder service.
# Stage 1: compile the Go webhook controller.
# Stage 2: Alpine runtime with FFmpeg for HLS transcoding.

FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /hls-transcoder .

# Runtime: Alpine with FFmpeg (required for transcoding).
# Unlike the rtmp-server (distroless), we need a real OS image for FFmpeg.
FROM alpine:3.20

RUN apk add --no-cache ffmpeg && \
    adduser -D -u 1000 hlsuser

COPY --from=builder /hls-transcoder /hls-transcoder

USER hlsuser

ENTRYPOINT ["/hls-transcoder"]

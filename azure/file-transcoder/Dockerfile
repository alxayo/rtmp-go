# Multi-stage build for the VOD File Transcoder.
# Stage 1: Compile the Go binary.
# Stage 2: Alpine runtime with FFmpeg for HLS transcoding.
#
# Alpine's FFmpeg package includes:
#   - libx264 (H.264)
#   - libvpx (VP8/VP9)
#   - libaom-av1 (AV1 fallback — SVT-AV1 is NOT in Alpine's package)
#   - libopus (Opus audio)
#   - aac (AAC audio)
#
# Note: For SVT-AV1 support, you would need to build FFmpeg from source
# or use a custom Alpine package. The Go code detects this and falls back
# to libaom-av1 automatically.

# Stage 1: Build the Go binary
FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /file-transcoder .

# Stage 2: Runtime with FFmpeg
# Alpine 3.20 provides FFmpeg 6.x with hardware codec support
FROM alpine:3.20

RUN apk add --no-cache ffmpeg && \
    adduser -D -u 1000 transcoder

COPY --from=builder /file-transcoder /usr/local/bin/file-transcoder

USER transcoder

ENTRYPOINT ["file-transcoder"]

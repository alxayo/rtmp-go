# Multi-stage build for the Azure Blob Storage sidecar
FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /blob-sidecar .

# Runtime: minimal image with CA certificates for Azure TLS
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /blob-sidecar /blob-sidecar

ENTRYPOINT ["/blob-sidecar"]

# Azure Blob Storage Sidecar

A standalone service that uploads rtmp-go recording segments to Azure Blob Storage. Supports multi-tenant routing where different streams are stored in different storage accounts.

## Operating Modes

| Mode | Flag | How it works | Latency |
|------|------|-------------|---------|
| **watch** (default) | `-mode watch` | Filesystem watching (fsnotify) with stabilization delay | ~2s after segment close |
| **events** | `-mode events` | Reads hook events from stdin (piped from rtmp-go) | Instant on segment close |
| **webhook** | `-mode webhook` | HTTP server receiving webhook events from rtmp-go | Instant on segment close |

## Features

- **Triple mode** — filesystem watching, stdin events, or HTTP webhook receiver
- **HTTP ingest mode** — direct FFmpeg uploads of segments via PUT /ingest/{path}
- **Multi-tenant** — route streams to different Azure storage accounts
- **Dual resolution** — JSON config file + HTTP API fallback
- **Hot-reload** — send SIGHUP to reload tenant config without restart
- **Self-healing** — on restart (watch mode), uploads any segments missed during downtime
- **Configurable cleanup** — optionally delete local files after upload
- **Bounded concurrency** — configurable worker pool prevents network saturation

## Quick Start

### Watch Mode (no rtmp-go changes needed)

```bash
# Build
cd azure/blob-sidecar
go build -o blob-sidecar .

# Run alongside rtmp-go
./blob-sidecar \
  -mode watch \
  -watch-dir /path/to/recordings \
  -config tenants.json \
  -workers 4 \
  -cleanup=true
```

### Events Mode (recommended for single-process deployments)

Requires rtmp-go to be started with the stdio hook enabled:

```bash
# Start rtmp-go with stdio hook, pipe stderr to sidecar
rtmp-server \
  -listen :1935 \
  -record-dir ./recordings \
  -hook-stdio-format json \
  2>&1 | blob-sidecar -mode events -config tenants.json -workers 4 -cleanup=true
```

Or with process substitution (keeps rtmp-go stdout separate):

```bash
rtmp-server -listen :1935 -record-dir ./recordings -hook-stdio-format json \
  2> >(blob-sidecar -mode events -config tenants.json)
```

### Webhook Mode (recommended for container deployments)

Best for Azure Container Apps, Kubernetes, or any deployment where rtmp-server and
the sidecar run as separate containers. rtmp-server pushes events via HTTP webhook:

```bash
# Start sidecar as HTTP webhook listener
blob-sidecar \
  -mode webhook \
  -listen-addr :8080 \
  -config tenants.json \
  -workers 4 \
  -cleanup=true

# Start rtmp-go with webhook hooks pointing at sidecar
rtmp-server \
  -listen :1935 \
  -record-dir ./recordings \
  -hook-webhook "segment_complete=http://sidecar-host:8080/events" \
  -hook-webhook "recording_start=http://sidecar-host:8080/events" \
  -hook-webhook "recording_stop=http://sidecar-host:8080/events"
```

The sidecar exposes two endpoints:
- `POST /events` — receives hook events (JSON body matching `hooks.Event` schema)
- `GET /health` — liveness/readiness probe (returns `200 OK`)

### HTTP Ingest Mode (for direct FFmpeg uploads)

HTTP ingest mode allows FFmpeg or other tools to directly upload segments and playlists to blob storage via HTTP PUT requests. This eliminates the need for SMB mounts and provides significantly lower latency.

#### Start Ingest Server

```bash
# Ingest server on separate port (with optional bearer token auth)
blob-sidecar \
  -ingest-addr :8081 \
  -ingest-storage blob \
  -ingest-token "my-secret-token" \
  -ingest-max-body 100000000 \
  -config tenants.json

# Or use local filesystem backend for testing/development
blob-sidecar \
  -ingest-addr :8081 \
  -ingest-storage local \
  -ingest-local-dir /tmp/ingest-files \
  -config tenants.json
```

#### Upload Segments

```bash
# Upload a segment with bearer token auth
curl -X PUT \
  -H "Authorization: Bearer my-secret-token" \
  -H "Content-Length: 47185920" \
  --data-binary @segment_001.ts \
  http://localhost:8081/ingest/hls/live_test/seg_00001.ts

# Upload playlist (no auth)
curl -X PUT \
  -H "Content-Length: 156" \
  --data-binary @index.m3u8 \
  http://localhost:8081/ingest/hls/live_test/index.m3u8
```

#### Endpoints

| Path | Method | Purpose | Auth |
|------|--------|---------|------|
| `/ingest/{blobPath}` | `PUT` | Upload segment/playlist | Optional bearer token |
| `/health` | `GET` | Liveness/readiness probe | None |

#### Security

- **Path validation** — rejects path traversal (`..`, absolute paths, null bytes)
- **Size limits** — enforces max body size (default 50MB, configurable)
- **Content-Length validation** — requires explicit size, rejects chunked encoding
- **Bearer token auth** — optional per-request authentication
- **Segment validation** — rejects `.ts` files smaller than 1KB (incomplete segments)

#### Advantages

- **Zero latency** — synchronous uploads complete before FFmpeg continues
- **No filesystem watches** — direct HTTP uploads, no filesystem polling overhead
- **Scalable** — HTTP request-based, works with load balancers and auto-scaling
- **Stateless** — multiple instances can handle uploads without coordination
- **Optional auth** — bearer token authentication can be enabled via `-ingest-token`

## Configuration

### Tenant Config File (`tenants.json`)

```json
{
  "tenants": {
    "live": {
      "storage_account": "https://account.blob.core.windows.net",
      "container": "recordings",
      "credential": "managed-identity"
    },
    "tenant-a": {
      "storage_account": "https://tenanta.blob.core.windows.net",
      "container": "streams",
      "credential": "connection-string",
      "connection_string_env": "TENANT_A_CONN_STRING"
    }
  },
  "default": {
    "storage_account": "https://default.blob.core.windows.net",
    "container": "unrouted",
    "credential": "managed-identity"
  },
  "api_fallback": {
    "enabled": true,
    "url": "https://your-api.com/api/tenants/resolve",
    "timeout": "5s",
    "cache_ttl": "5m"
  }
}
```

### Credential Types

| Type | Description |
|------|-------------|
| `managed-identity` | Uses Azure DefaultAzureCredential (Managed Identity, env vars, Azure CLI) |
| `connection-string` | Uses connection string from the env var specified in `connection_string_env` |

### Tenant Resolution Order

1. **Exact match** — stream key matches a tenant key exactly
2. **App prefix** — "live/stream1" matches tenant key "live"
3. **Longest prefix** — hierarchical matching for complex keys
4. **API fallback** — HTTP API call with response caching
5. **Default** — catch-all storage account

### Stream Key Extraction

The sidecar extracts the stream key from segment file paths:

- **Nested layout**: `recordings/live_mystream/20260419_seg001.flv` → stream key: `live/mystream`
- **Flat layout**: `recordings/live_mystream_20260419_103406_seg001.flv` → stream key: `live/mystream`

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-mode` | `watch` | Operating mode: `watch`, `events`, or `webhook` |
| `-watch-dir` | `recordings` | Directory to watch for segment files (watch mode only) |
| `-listen-addr` | `:8080` | HTTP listen address (webhook mode only) |
| `-config` | `tenants.json` | Path to tenant configuration file |
| `-workers` | `4` | Number of concurrent upload workers |
| `-cleanup` | `false` | Delete local files after successful upload |
| `-stabilize-duration` | `2s` | Wait time after last write before uploading (watch mode only) |
| `-log-level` | `info` | Log level: debug, info, warn, error |
| `-ingest-addr` | `:8081` | HTTP listen address for ingest endpoint |
| `-ingest-storage` | `blob` | Storage backend for ingest: `blob` or `local` |
| `-ingest-local-dir` | `` | Root directory for local storage backend (required if `-ingest-storage=local`) |
| `-ingest-token` | `` | Optional bearer token for ingest authentication (empty = auth disabled) |
| `-ingest-max-body` | `50MB` | Maximum request body size in bytes |

## Signals

| Signal | Action |
|--------|--------|
| `SIGHUP` | Reload tenant configuration file |
| `SIGTERM` / `SIGINT` | Graceful shutdown (finish in-progress uploads) |

## Container Deployment

### Docker

```bash
docker build -t blob-sidecar .
docker run -v /recordings:/recordings \
  -e AZURE_TENANT_ID=... \
  -e AZURE_CLIENT_ID=... \
  blob-sidecar -watch-dir /recordings -config /config/tenants.json
```

### Azure Container Apps (Sidecar)

```yaml
spec:
  template:
    containers:
    - name: rtmp-go
      image: myregistry.azurecr.io/rtmp-go:latest
      args: ["-record-dir", "/recordings", "-segment-duration", "3m"]
      volumeMounts:
      - name: recordings
        mountPath: /recordings

    - name: blob-sidecar
      image: myregistry.azurecr.io/blob-sidecar:latest
      args: ["-watch-dir", "/recordings", "-config", "/config/tenants.json", "-cleanup"]
      volumeMounts:
      - name: recordings
        mountPath: /recordings
      - name: config
        mountPath: /config

    volumes:
    - name: recordings
      emptyDir: {}
    - name: config
      secret:
        secretName: tenants-config
```

## API Fallback

When enabled, the sidecar calls an HTTP API to resolve unknown stream keys:

```
GET https://your-api.com/api/tenants/resolve?stream_key=live/mystream
```

Expected response:
```json
{
  "storage_account": "https://account.blob.core.windows.net",
  "container": "recordings",
  "credential": "managed-identity"
}
```

Responses are cached for the configured TTL to minimize API calls.

## Events Mode Details

In events mode, the sidecar reads JSON hook events from stdin. rtmp-go emits these
on stderr when configured with `-hook-stdio-format json`.

### Event Format

```
RTMP_EVENT: {"type":"segment_complete","timestamp":1714168200,"conn_id":"abc123","stream_key":"live/stream1","data":{"path":"/recordings/live_stream1_20260420_143000_seg001.flv","size":47185920,"segment_index":1,"duration_ms":180000,"codec":"H264","format":"flv"}}
```

### Supported Events

| Event | Action |
|-------|--------|
| `segment_complete` | Triggers upload of the segment file |
| `recording_start` | Logged (informational) |
| `recording_stop` | Logged (informational) |

### Advantages over Watch Mode

- **Zero latency** — upload starts immediately when segment is closed (no 2s stabilization wait)
- **Accurate stream key** — provided directly by rtmp-go (no filename parsing heuristics)
- **Rich metadata** — size, duration, codec, segment index available without file inspection
- **Lower resource usage** — no fsnotify watchers or directory scanning

### Container Apps Deployment (Webhook Mode — Recommended)

Webhook mode works natively with separate Container Apps since it uses HTTP:

```yaml
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: rtmp-server
        image: myregistry.azurecr.io/rtmp-go:latest
        args:
          - "-listen"
          - ":1935"
          - "-record-dir"
          - "/recordings"
          - "-segment-duration"
          - "3m"
          - "-hook-webhook"
          - "segment_complete=http://blob-sidecar:8080/events"
          - "-hook-webhook"
          - "recording_start=http://blob-sidecar:8080/events"
          - "-hook-webhook"
          - "recording_stop=http://blob-sidecar:8080/events"
        volumeMounts:
        - name: recordings
          mountPath: /recordings

      - name: blob-sidecar
        image: myregistry.azurecr.io/blob-sidecar:latest
        args: ["-mode", "webhook", "-listen-addr", ":8080", "-config", "/config/tenants.json", "-cleanup"]
        ports:
        - containerPort: 8080
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
        volumeMounts:
        - name: recordings
          mountPath: /recordings
        - name: config
          mountPath: /config

      volumes:
      - name: recordings
        emptyDir: {}
      - name: config
        secret:
          secretName: tenants-config
```

### Container Apps Deployment (Events Mode)

Events mode requires piping stderr between containers. Use Docker Compose or a shell wrapper:

```yaml
# Azure Container Apps with sidecar logging
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
      - name: rtmp-server
        image: myregistry.azurecr.io/rtmp-go:latest
        args:
          - "-listen"
          - ":1935"
          - "-record-dir"
          - "/recordings"
          - "-segment-duration"
          - "3m"
          - "-hook-stdio-format"
          - "json"
        volumeMounts:
        - name: recordings
          mountPath: /recordings

      - name: blob-sidecar
        image: myregistry.azurecr.io/blob-sidecar:latest
        args: ["-mode", "events", "-config", "/config/tenants.json", "-cleanup"]
        stdin: true
        volumeMounts:
        - name: recordings
          mountPath: /recordings
        - name: config
          mountPath: /config

      volumes:
      - name: recordings
        emptyDir: {}
      - name: config
        secret:
          secretName: tenants-config
```

> **Note**: In Kubernetes, piping stderr between containers requires a logging sidecar
> or shared log file. For direct piping, use Docker Compose or a shell wrapper:
> ```bash
> rtmp-server ... -hook-stdio-format json 2>&1 | blob-sidecar -mode events ...
> ```

# Azure Blob Storage Sidecar

A standalone service that watches rtmp-go's recording directory and uploads completed segment files to Azure Blob Storage. Supports multi-tenant routing where different streams are stored in different storage accounts.

## Features

- **Zero rtmp-go modifications** — uses filesystem watching (fsnotify)
- **Multi-tenant** — route streams to different Azure storage accounts
- **Dual resolution** — JSON config file + HTTP API fallback
- **Hot-reload** — send SIGHUP to reload tenant config without restart
- **Self-healing** — on restart, uploads any segments missed during downtime
- **Configurable cleanup** — optionally delete local files after upload
- **Bounded concurrency** — configurable worker pool prevents network saturation

## Quick Start

```bash
# Build
cd cmd/blob-sidecar
go build -o blob-sidecar .

# Configure tenants
cp tenants.example.json tenants.json
# Edit tenants.json with your Azure storage accounts

# Run
./blob-sidecar \
  -watch-dir /path/to/recordings \
  -config tenants.json \
  -workers 4 \
  -cleanup=true \
  -log-level info
```

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
| `-watch-dir` | `recordings` | Directory to watch for segment files |
| `-config` | `tenants.json` | Path to tenant configuration file |
| `-workers` | `4` | Number of concurrent upload workers |
| `-cleanup` | `false` | Delete local files after successful upload |
| `-stabilize-duration` | `2s` | Wait time after last write before uploading |
| `-log-level` | `info` | Log level: debug, info, warn, error |

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

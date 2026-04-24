# Plan: HTTP Ingest Mode for Blob-Sidecar

## TL;DR

Add an HTTP ingest mode to the blob-sidecar so FFmpeg (in the HLS transcoder) can PUT segment and playlist files directly via HTTP, bypassing the Azure Files SMB mount entirely. The sidecar receives bytes, uploads synchronously to Azure Blob Storage, and returns 200 — giving FFmpeg natural backpressure and guaranteed segment-before-playlist ordering. This is an additional mode alongside the existing watch/events/webhook modes.

## Current Architecture (Problems)

```
FFmpeg → Azure Files SMB mount → SegmentNotifier polls (4s) → webhook to sidecar → sidecar reads from SMB → Blob Storage
```

Issues: SMB cache quirks, fsnotify unreliable on SMB, 8+ second detection latency (two poll cycles), self-healing master.m3u8 hacks, shared filesystem coupling.

## Proposed Architecture (HTTP Ingest)

```
FFmpeg --method PUT→ blob-sidecar /ingest/{path} → synchronous upload → Blob Storage
```

Eliminates: Azure Files mount, SegmentNotifier, polling, SMB workarounds.

---

## Phase 1: Blob-Sidecar — HTTP Ingest Endpoint

### Step 1.1: Storage Backend Interface

Introduce a `StorageBackend` interface so the ingest handler can write to Azure Blob **or** the local filesystem, selected via a flag.

```go
type StorageBackend interface {
    Store(ctx context.Context, blobPath string, reader io.Reader, size int64) error
}
```

**Two implementations:**

**`BlobBackend`** — wraps the existing Uploader for Azure Blob Storage:
- New method on Uploader: `UploadStream(ctx, tenant, streamKey, blobName string, reader io.Reader, size int64) error`
- Reuses existing `getClient()` for Azure SDK client caching
- Uses `azblob.Client.UploadStream()` instead of `UploadFile()`
- Same undersized .ts rejection (Content-Length < 1KB)
- Same blob path construction: `{path_prefix}/{streamKey}/{filename}`
- The `BlobBackend` struct wraps the Uploader + Router to resolve tenants from the path

**`LocalBackend`** — writes to a local directory (for testing without Azure credentials):
- New flag: `-ingest-local-dir /tmp/hls-ingest` (root directory for local storage)
- Writes files to `{local-dir}/{blobPath}`, preserving the full path hierarchy from the URL
- Creates parent directories with `os.MkdirAll`
- Uses explicit `f.Sync()` before close to ensure data is flushed
- Example: PUT `/ingest/hls/live_test/stream_0/seg_00001.ts` → writes `/tmp/hls-ingest/hls/live_test/stream_0/seg_00001.ts`

**Flag**: `-ingest-storage blob|local` (default: `blob`)
- `blob`: uploads to Azure Blob Storage via the existing Uploader + tenant routing
- `local`: writes to local filesystem at `-ingest-local-dir` — no Azure credentials, no tenant config needed

This makes local development trivial: run the sidecar with `-ingest-storage local -ingest-local-dir ./output`, point FFmpeg at it, and inspect the HLS output on disk.

Files: `azure/blob-sidecar/storage_backend.go` (new), `azure/blob-sidecar/uploader.go` (add UploadStream)

### Step 1.2: Create `ingest_handler.go`

New file implementing the HTTP ingest endpoint. The handler is storage-agnostic — it receives the `StorageBackend` interface and delegates storage to whichever backend is configured.

**Endpoint**: `PUT /ingest/{streamKey...}/{filename}`
- Wildcard path: everything after `/ingest/` is the blob path (preserves HLS directory structure)
- Example: `PUT /ingest/hls/live_test/stream_0/seg_00001.ts`
  - streamKey extracted: `hls/live_test/stream_0`
  - filename: `seg_00001.ts`
  - blob path: `{prefix}/hls/live_test/stream_0/seg_00001.ts`

**Handler logic**:
1. Validate method is PUT (405 otherwise)
2. Extract stream key and filename from URL path (reject path traversal: `..`, absolute paths)
3. Enforce max body size (configurable, default 50MB — covers largest plausible HLS segment)
4. Resolve tenant via existing `Router.ResolveByStreamKey()` (blob backend only; local backend ignores tenants)
5. Call `StorageBackend.Store()` — **synchronous** (critical for ordering: FFmpeg waits for 200 before sending playlist PUT)
6. Return 201 Created on success, 500 on upload failure
7. Log: stream_key, blob_name, size_bytes, duration_ms

**Security**:
- Optional `Authorization: Bearer {token}` header validation (new `-ingest-token` flag)
- `http.MaxBytesReader` on request body
- Path traversal validation (no `..` components, no absolute paths)
- Content-Length required

**Why synchronous (not queued)?**
FFmpeg sends segment PUT, waits for response, THEN sends playlist PUT. Synchronous upload guarantees segments exist in blob before the playlist references them — solving the "playlist-before-segment" 404 problem naturally.

Files: `azure/blob-sidecar/ingest_handler.go` (new)

### Step 1.3: Wire ingest endpoint into main.go

- Add flags:
  - `-ingest-addr :8081` — HTTP listen address for ingest endpoint
  - `-ingest-storage blob|local` (default: `blob`)
  - `-ingest-local-dir ./output` — root directory for local storage backend
  - `-ingest-token ""` — optional bearer token for auth
  - `-ingest-max-body 50MB` — max request body size
- Decision: always start the ingest server on a configurable addr, independent of sidecar mode — this way HTTP ingest works alongside any existing mode (watch/events/webhook).
- On startup:
  - If `-ingest-storage=blob`: create `BlobBackend` with Uploader + Router (requires tenant config)
  - If `-ingest-storage=local`: create `LocalBackend` with `-ingest-local-dir` (no Azure deps needed)
- Register `PUT /ingest/` route
- Register `GET /health` on same mux (already exists for webhook)

Files: `azure/blob-sidecar/main.go`

### Step 1.4: Tests for ingest handler and storage backends

**Ingest handler tests:**
- `TestIngestHandler_PutSegment` — PUT .ts file, verify backend Store() called with correct path/bytes
- `TestIngestHandler_PutPlaylist` — PUT .m3u8 file, verify Store() called
- `TestIngestHandler_PathTraversal` — Reject `../` in path (400)
- `TestIngestHandler_OversizedBody` — Reject body > max size (413)
- `TestIngestHandler_AuthToken` — Reject missing/invalid token when configured (401)
- `TestIngestHandler_MethodNotAllowed` — GET returns 405
- `TestIngestHandler_UndersizedSegment` — .ts < 1KB rejected

**Local backend tests:**
- `TestLocalBackend_StoreSegment` — Verify file written to correct path with correct bytes
- `TestLocalBackend_CreatesDirectories` — Verify parent dirs created automatically
- `TestLocalBackend_StorePlaylist` — Verify .m3u8 written and flushed

Files: `azure/blob-sidecar/ingest_handler_test.go` (new), `azure/blob-sidecar/storage_backend_test.go` (new)

---

## Phase 2: HLS Transcoder — HTTP Output Mode

### Step 2.1: Add `-output-mode` flag

New flag: `-output-mode file|http` (default: `file` for backward compat)
- `file`: Current behavior — write to `-hls-dir`, use SegmentNotifier
- `http`: FFmpeg outputs via HTTP PUT to blob-sidecar ingest endpoint

New flag: `-ingest-url` (e.g., `http://blob-sidecar:8081`) — base URL for ingest endpoint

Files: `azure/hls-transcoder/main.go`, `azure/hls-transcoder/transcoder.go` (TranscoderConfig struct)

### Step 2.2: Build HTTP-mode FFmpeg arguments

New methods in `transcoder.go`:
- `buildABRArgsHTTP(rtmpURL, ingestBaseURL, streamKey string) []string`
- `buildCopyArgsHTTP(rtmpURL, ingestBaseURL, streamKey string) []string`

**ABR mode HTTP args** (key differences from file mode):
```
-method PUT
-hls_segment_filename "http://blob-sidecar:8081/ingest/hls/{safeKey}/stream_%v/seg_%05d.ts"
"http://blob-sidecar:8081/ingest/hls/{safeKey}/stream_%v/index.m3u8"
```

**Key changes from file mode:**
- Add `-method PUT` flag (tells FFmpeg HLS muxer to use HTTP PUT)
- Replace filesystem paths with HTTP URLs
- Can use `-master_pl_name master.m3u8` (no SMB rename issue over HTTP)
- Add `-http_persistent 1` for connection reuse (reduces connection setup overhead)
- No need for `-hls_flags delete_segments` (blob lifecycle managed separately)

**Copy mode HTTP args**: Same pattern, single stream variant

Files: `azure/hls-transcoder/transcoder.go`

### Step 2.3: Master playlist handling in HTTP mode

Two options:
- **Option A**: Use FFmpeg's `-master_pl_name` — FFmpeg generates and PUTs it. Works over HTTP (no SMB rename quirk). *Recommended — simplest.*
- **Option B**: Keep explicit `writeMasterPlaylist()` but PUT via HTTP instead of file write.

Recommend Option A. Remove the need for `writeMasterPlaylist()` in HTTP mode entirely.

If `-master_pl_name` doesn't work reliably with FFmpeg HTTP output, fall back to Option B: transcoder sends an HTTP PUT with the master playlist bytes to `{ingestURL}/ingest/hls/{safeKey}/master.m3u8` before starting FFmpeg.

Files: `azure/hls-transcoder/transcoder.go`

### Step 2.4: Skip SegmentNotifier in HTTP mode

In `Transcoder.Start()`, only launch `SegmentNotifier.WatchStream()` goroutine when `output-mode=file`. In HTTP mode, FFmpeg sends segments directly — no polling needed.

Also skip:
- Output directory creation (`os.MkdirAll`)  
- `writeMasterPlaylist()` call (handled by FFmpeg or pre-PUT)
- Self-healing master.m3u8 logic in notifier

Files: `azure/hls-transcoder/transcoder.go`

### Step 2.5: Transcoder tests

- `TestBuildABRArgsHTTP` — Verify FFmpeg args include `-method PUT`, correct URLs, `http_persistent`
- `TestBuildCopyArgsHTTP` — Same for copy mode
- `TestStartHTTPMode_NoNotifier` — Verify notifier not started in HTTP mode
- `TestStartHTTPMode_NoLocalDirCreation` — Verify no filesystem writes in HTTP mode

Files: `azure/hls-transcoder/transcoder_test.go`

---

## Phase 3: Integration & Deployment

### Step 3.1: Dockerfile updates

- **blob-sidecar Dockerfile**: No changes needed (already builds Go binary)
- **hls-transcoder Dockerfile**: No changes needed (already has FFmpeg + Go binary)
- **Container Apps deployment**: Update environment variables/args:
  - blob-sidecar: add `-ingest-addr :8081` to container args
  - hls-transcoder: add `-output-mode http -ingest-url http://blob-sidecar:8081`
  - Remove Azure Files mount from hls-transcoder if using HTTP-only mode

### Step 3.2: Update deployment scripts

Update `azure/deploy.sh` or Bicep templates to:
- Expose port 8081 on blob-sidecar (internal only, within VNet)
- Pass new flags to both containers
- Optionally remove Azure Files volume mount (only if no longer needed)

Files: `azure/infra/main.bicep` (if applicable), `azure/deploy.sh`

---

## Relevant Files

**Blob-sidecar (modify):**
- `azure/blob-sidecar/uploader.go` — Add `UploadStream()` method
- `azure/blob-sidecar/main.go` — Wire ingest HTTP server, new flags for storage backend
- `azure/blob-sidecar/router.go` — Reuse `ResolveByStreamKey()` (no changes needed)

**Blob-sidecar (create):**
- `azure/blob-sidecar/storage_backend.go` — `StorageBackend` interface + `BlobBackend` + `LocalBackend`
- `azure/blob-sidecar/ingest_handler.go` — New PUT endpoint handler
- `azure/blob-sidecar/ingest_handler_test.go` — Ingest handler tests
- `azure/blob-sidecar/storage_backend_test.go` — LocalBackend tests

**HLS transcoder (modify):**
- `azure/hls-transcoder/main.go` — New flags (`-output-mode`, `-ingest-url`)
- `azure/hls-transcoder/transcoder.go` — HTTP-mode FFmpeg args, conditional notifier/dir creation

**HLS transcoder (create):**
- `azure/hls-transcoder/transcoder_test.go` — Tests for HTTP mode args

**Reference (no changes):**
- `azure/blob-sidecar/config.go` — `StorageTarget`, `TenantConfig` structs
- `azure/blob-sidecar/resolver_file.go` — `FileResolver.Resolve()` pattern
- `azure/hls-transcoder/notifier.go` — Existing poll-based approach (kept for file mode)

---

## Verification

1. **Unit tests**: `cd azure/blob-sidecar && go test -race ./...` — ingest handler, storage backends
2. **Unit tests**: `cd azure/hls-transcoder && go test -race ./...` — HTTP arg building
3. **Local filesystem test (no Azure needed)**:
   - Start blob-sidecar: `./blob-sidecar -ingest-addr :8081 -ingest-storage local -ingest-local-dir ./test-output`
   - Run FFmpeg with HTTP PUT: `ffmpeg -re -i test.mp4 -f hls -method PUT -hls_time 3 -hls_segment_filename "http://localhost:8081/ingest/hls/test/seg_%05d.ts" "http://localhost:8081/ingest/hls/test/index.m3u8"`
   - Verify HLS output: `ls -la ./test-output/hls/test/` — should contain `index.m3u8` + `seg_*.ts`
   - Play locally: `ffplay ./test-output/hls/test/index.m3u8`
4. **Azurite integration test (Azure emulator, no real account)**:
   - Start Azurite: `docker run -p 10000:10000 mcr.microsoft.com/azure-storage/azurite`
   - Start blob-sidecar with connection string pointing at Azurite
   - Run FFmpeg with HTTP PUT → verify segments in Azurite
5. **FFmpeg HTTP muxer validation**: Run FFmpeg standalone with `-method PUT` against a simple HTTP server to confirm segment/playlist ordering and `-master_pl_name` behavior
6. **Deployed test**: Deploy both containers to Azure Container Apps, stream via RTMP, verify HLS segments in Blob Storage without Azure Files mount

---

## Decisions

- **Synchronous upload in ingest handler** — FFmpeg waits for 200 before sending next file. This guarantees segment-before-playlist ordering. Latency is acceptable (< 500ms per segment in same Azure region).
- **Independent ingest server** — Runs on its own address/port regardless of sidecar mode (watch/events/webhook). Allows mixed operation during migration.
- **Backward compatible** — All existing modes (watch, events, webhook) remain unchanged. HTTP ingest is additive.
- **No BlobFuse2** — Pure HTTP as specified.
- **Scope boundary** — This plan covers the sidecar ingest endpoint and transcoder HTTP output mode. Does NOT cover: segment caching/CDN, player-side changes, cleanup/lifecycle of old blobs.

## Further Considerations

1. **FFmpeg `-master_pl_name` over HTTP**: Needs manual verification that FFmpeg 6.x/7.x supports this flag when outputting to HTTP URLs. If not, fall back to explicit HTTP PUT from the transcoder Go code. Recommend a quick `ffmpeg -f hls -method PUT -master_pl_name master.m3u8 ...` test before implementing.
2. **Connection reuse (`-http_persistent`)**: FFmpeg's HTTP persistent connection flag may have compatibility issues with some Go HTTP servers. Should test and potentially tune the sidecar's HTTP server timeouts (`ReadTimeout`, `IdleTimeout`).
3. **Segment deletion / blob lifecycle**: With HTTP ingest, there are no local files to clean up. Blob lifecycle policies (Azure Storage) should handle TTL-based cleanup of old segments. This is out of scope but worth noting.

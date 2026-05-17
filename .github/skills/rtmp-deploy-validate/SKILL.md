---
name: rtmp-deploy-validate
description: 'Deploy RTMP ingest server changes to Azure and validate live stream pipeline. Use when: RTMP server code changed, need to rebuild and push RTMP images, validate transcoder receives webhooks, verify keepalive prevents scale-to-zero, test live stream end-to-end, update RTMP server container app image, rebuild rtmp-server/hls-transcoder/blob-sidecar in ACR. NOT for full-stack deploy (use azure-deploy) or local Podman dev.'
argument-hint: 'Describe what changed (e.g., "added keepalive webhook support")'
---

# RTMP Deploy & Validate

Rebuild RTMP-side container images in ACR, update container apps, and validate the full ingest-to-HLS pipeline with a live stream test.

**Scope**: rtmp-server, hls-transcoder, blob-sidecar (rec + hls). Does NOT touch StreamGate platform or HLS server images — use `azure-deploy` skill for full-stack.

## Environment Discovery

```bash
# Get resource names (suffix derived from environmentName in Bicep)
az containerapp list -g event-periscope-ne \
  --query '[].{name:name, image:properties.template.containers[0].image}' -o table

# Get ACR name
ACR_NAME=$(az acr list -g event-periscope-ne --query '[0].name' -o tsv)

# Get Log Analytics workspace ID (for log queries)
WORKSPACE_ID=$(az containerapp env list -g event-periscope-ne \
  --query '[0].properties.appLogsConfiguration.logAnalyticsConfiguration.customerId' -o tsv)
```

Container app names follow pattern `<service>-<suffix>` where suffix comes from `uniqueString(environmentName)`.

## Procedure

### Step 1: Build and Test Locally

**Goal**: Verify changes compile and existing tests pass before pushing to ACR.

```bash
cd /Users/alex/Code/rtmp-go
go build -o rtmp-server.exe ./cmd/rtmp-server
go test -race $(go list ./... | grep -v tests/integration)
```

All unit tests must pass. Integration test failures in `TestPublishToPlayRelay`, `TestRelayMultipleSubscribers`, and `TestQuickstartScenario` are known pre-existing — ignore those.

If Bicep templates were changed:
```bash
cd /Users/alex/Code/rtmp-go/azure/infra && az bicep build --file main.bicep
```

### Step 2: Build Images in ACR

**Goal**: Push updated container images to Azure Container Registry.

Generate the image tag from the current git state:
```bash
SHORT_SHA=$(cd /Users/alex/Code/rtmp-go && git rev-parse --short HEAD)
TAG="v1.0.0-${SHORT_SHA}-$(date +%Y%m%d)"
echo "Tag: $TAG"
```

Build each image (runs in ACR — uses the Dockerfile from the repo, not local Docker):
```bash
ACR_NAME=$(az acr list -g event-periscope-ne --query '[0].name' -o tsv)

# rtmp-server (~1–2 min)
az acr build --registry "$ACR_NAME" \
  --image "rtmp-server:${TAG}" \
  --file /Users/alex/Code/rtmp-go/Dockerfile \
  /Users/alex/Code/rtmp-go \
  --no-logs --output none

# blob-sidecar (~2–3 min)
az acr build --registry "$ACR_NAME" \
  --image "blob-sidecar:${TAG}" \
  --file /Users/alex/Code/rtmp-go/azure/blob-sidecar/Dockerfile \
  /Users/alex/Code/rtmp-go/azure/blob-sidecar \
  --no-logs --output none

# hls-transcoder (~3–5 min, includes FFmpeg)
az acr build --registry "$ACR_NAME" \
  --image "hls-transcoder:${TAG}" \
  --file /Users/alex/Code/rtmp-go/azure/hls-transcoder/Dockerfile \
  /Users/alex/Code/rtmp-go/azure/hls-transcoder \
  --no-logs --output none
```

**Note**: `az acr build` sends the local directory (including uncommitted changes) to ACR for build. The git SHA in the tag identifies the base commit but the image may include uncommitted work.

### Step 3: Update Container Apps with New Images

**Goal**: Point container apps to the newly built images.

**Option A — Bicep redeploy** (if infra/args changed):
```bash
# Get required params from existing deployment
IDENTITY_ID=$(az identity list -g event-periscope-ne --query '[0].id' -o tsv)
ACR_SERVER="${ACR_NAME}.azurecr.io"

az deployment group create \
  --resource-group event-periscope-ne \
  --name "rtmpgo-update-$(date +%s)" \
  --template-file /Users/alex/Code/rtmp-go/azure/infra/main.bicep \
  --parameters environmentName=eventperiscopene \
    rtmpServerImage="${ACR_SERVER}/rtmp-server:${TAG}" \
    blobSidecarImage="${ACR_SERVER}/blob-sidecar:${TAG}" \
    hlsTranscoderImage="${ACR_SERVER}/hls-transcoder:${TAG}" \
    <additional params as needed>
```

**Option B — Image-only update** (no infra changes, faster):
```bash
ACR_SERVER="${ACR_NAME}.azurecr.io"
SUFFIX=$(az containerapp list -g event-periscope-ne --query '[0].name' -o tsv | sed 's/.*-//')

# Update RTMP server
az containerapp update \
  --name "rtmp-server-${SUFFIX}" -g event-periscope-ne \
  --image "${ACR_SERVER}/rtmp-server:${TAG}" \
  --revision-suffix "img-$(date +%s)" --output none

# Update HLS transcoder (multi-container — update main container)
az containerapp update \
  --name "hls-transcoder-${SUFFIX}" -g event-periscope-ne \
  --image "${ACR_SERVER}/hls-transcoder:${TAG}" \
  --revision-suffix "img-$(date +%s)" --output none

# Update blob sidecars
for prefix in rec-blob-sidecar hls-blob-sidecar; do
  az containerapp update \
    --name "${prefix}-${SUFFIX}" -g event-periscope-ne \
    --image "${ACR_SERVER}/blob-sidecar:${TAG}" \
    --revision-suffix "img-$(date +%s)" --output none
done
```

**Choosing between A and B**: Use Bicep (A) when container command args, env vars, scaling rules, or infrastructure changed. Use image-only (B) for pure code changes with no config changes.

### Step 4: Verify Deployed Configuration

**Goal**: Confirm container apps are running new images with correct config.

```bash
# Verify image tags
az containerapp list -g event-periscope-ne \
  --query '[].{name:name, image:properties.template.containers[0].image}' -o table

# Verify RTMP server command args (webhooks, keepalive, auth mode)
az containerapp show --name "rtmp-server-${SUFFIX}" -g event-periscope-ne \
  --query "properties.template.containers[0].command" -o json 2>/dev/null \
  | python3 -c "import json,sys; [print(c) for c in json.load(sys.stdin)]"

# Verify active replicas
for svc in rtmp-server hls-transcoder rec-blob-sidecar; do
  echo "=== ${svc}-${SUFFIX} ==="
  az containerapp revision list --name "${svc}-${SUFFIX}" -g event-periscope-ne \
    --query "[?properties.active].{name:name,replicas:properties.replicas,state:properties.runningState}" -o table
done
```

**Key config to verify**:
- RTMP server has `-hook-webhook` entries for `publish_start`, `publish_stop`, `stream_keepalive`, `segment_complete`, `recording_start`, `recording_stop`
- RTMP server has `-hook-keepalive-interval 60s` (prevents transcoder scale-to-zero)
- Webhook URLs point to correct internal FQDNs
- Auth mode is `callback` with correct callback URL

### Step 5: Verify Endpoints Respond

```bash
# Custom domains (may need managed cert provisioning if domains were re-bound)
curl -s -o /dev/null -w "platform: %{http_code}\n" --max-time 30 https://play.event-periscope.com
curl -s -o /dev/null -w "hls: %{http_code}\n" --max-time 30 https://hls.event-periscope.com

# Direct FQDNs (bypass custom domain/cert)
PLATFORM_FQDN=$(az containerapp show --name "sg-platform-${SUFFIX}" -g event-periscope-ne --query 'properties.configuration.ingress.fqdn' -o tsv)
curl -s -o /dev/null -w "platform-direct: %{http_code}\n" --max-time 30 "https://${PLATFORM_FQDN}"
```

Expected: 200 for platform, 404 for HLS (no stream path). If custom domains return 000 (connection refused), managed certificates may still be provisioning — wait 2–5 min and retry.

**If custom domain bindings were lost** (common after Bicep redeploy):
```bash
CONTAINER_ENV_NAME=$(az containerapp env list -g event-periscope-ne --query '[0].name' -o tsv)

# Re-bind play.event-periscope.com
az containerapp hostname add -g event-periscope-ne -n "sg-platform-${SUFFIX}" \
  --hostname "play.event-periscope.com" --output none 2>&1
az containerapp hostname bind -g event-periscope-ne -n "sg-platform-${SUFFIX}" \
  --hostname "play.event-periscope.com" --environment "$CONTAINER_ENV_NAME" \
  --validation-method CNAME --output none

# Re-bind hls.event-periscope.com
az containerapp hostname add -g event-periscope-ne -n "sg-hls-${SUFFIX}" \
  --hostname "hls.event-periscope.com" --output none 2>&1
az containerapp hostname bind -g event-periscope-ne -n "sg-hls-${SUFFIX}" \
  --hostname "hls.event-periscope.com" --environment "$CONTAINER_ENV_NAME" \
  --validation-method CNAME --output none
```

### Step 6: Live Stream Test

**Goal**: Publish a test stream and verify the entire pipeline end-to-end.

#### 6a. Create a test event in the admin console

Open `https://play.event-periscope.com/admin`, create a live event, note the event UUID and stream key.

#### 6b. Publish with FFmpeg

The stream key format is `live/<eventId>-<hash>` — copy the full key from the admin console event details.

```bash
# Replace <stream-key> with the full key from admin (e.g. live/myevent-a1b2c3d4e5f6)
ffmpeg -re -f lavfi -i testsrc2=size=1280x720:rate=30 \
  -f lavfi -i sine=frequency=440:sample_rate=44100 \
  -c:v libx264 -preset ultrafast -tune zerolatency -b:v 2000k \
  -c:a aac -b:a 128k \
  -f flv "rtmp://stream.event-periscope.com/<stream-key>"
```

If no test media is needed, use a real file:
```bash
ffmpeg -re -i test.mp4 -c copy -f flv "rtmp://stream.event-periscope.com/<stream-key>"
```

#### 6c. Verify transcoder receives webhook and starts FFmpeg

```bash
WORKSPACE_ID=$(az containerapp env list -g event-periscope-ne \
  --query '[0].properties.appLogsConfiguration.logAnalyticsConfiguration.customerId' -o tsv)

# Wait ~30s after publish, then check (Log Analytics has ~15s ingestion delay)
az monitor log-analytics query --workspace "$WORKSPACE_ID" \
  --analytics-query "ContainerAppConsoleLogs_CL \
    | where ContainerAppName_s == 'hls-transcoder-${SUFFIX}' \
    | where TimeGenerated > ago(5m) \
    | where Log_s has 'publish_start' or Log_s has 'FFmpeg' or Log_s has 'keepalive' \
    | order by TimeGenerated asc \
    | project TimeGenerated, Log_s" -o table 2>/dev/null
```

**Expected log sequence**:
1. `publish_start event received` — webhook arrived from RTMP server
2. `starting FFmpeg transcoder` — with RTMP URL, profile, output mode
3. `FFmpeg transcoder started` — PID assigned
4. `stream_keepalive event received` — every ~60s (prevents scale-to-zero)

#### 6d. Verify HLS segments in blob storage

```bash
STORAGE_ACCOUNT=$(az storage account list -g event-periscope-ne --query '[0].name' -o tsv)
EVENT_ID="<event-uuid>"

az storage blob list --account-name "$STORAGE_ACCOUNT" \
  --container-name hls-content \
  --prefix "${EVENT_ID}/" \
  --query "[].{name:name, size:properties.contentLength}" -o table \
  --auth-mode login | head -20
```

**Expected**: `master.m3u8`, `stream_0/index.m3u8`, `stream_0/indexN.ts`, and similar for `stream_1/` and `stream_2/` (3 ABR variants).

#### 6e. Verify RTMP server keepalive ticks

```bash
az monitor log-analytics query --workspace "$WORKSPACE_ID" \
  --analytics-query "ContainerAppConsoleLogs_CL \
    | where ContainerAppName_s has 'rtmp-server' \
    | where TimeGenerated > ago(5m) \
    | where Log_s has 'keepalive' \
    | order by TimeGenerated asc \
    | project TimeGenerated, Log_s" -o table 2>/dev/null
```

Should see `stream_keepalive` hook fires every 60s while publisher is connected.

#### 6f. Verify viewer playback

Open the event in the admin console preview player, or use the viewer portal with an access token. Check browser DevTools Network tab for:
- `master.m3u8` → 200 with ABR variant list
- `stream_N/index.m3u8` → 200 with segment list
- `stream_N/indexN.ts` → 200 (actual video segments)

#### 6g. Verify stream stop lifecycle

Stop FFmpeg (Ctrl+C), then verify cleanup:

```bash
az monitor log-analytics query --workspace "$WORKSPACE_ID" \
  --analytics-query "ContainerAppConsoleLogs_CL \
    | where ContainerAppName_s has 'hls-transcoder' or ContainerAppName_s has 'rtmp-server' \
    | where TimeGenerated > ago(2m) \
    | where Log_s has 'disconnect' or Log_s has 'publish_stop' or Log_s has 'stopping' or Log_s has 'teardown' \
    | order by TimeGenerated asc \
    | project TimeGenerated, ContainerAppName_s, Log_s" -o table 2>/dev/null
```

**Expected**: RTMP server fires `publish_stop` → transcoder stops FFmpeg → platform session unlocked.

## Common Failures

### `az acr build` timeout or OOM
The hls-transcoder image includes FFmpeg and is ~600MB. ACR build may take 5+ min.
- Add `--timeout 1800` for slow builds
- If OOM: check Dockerfile for unnecessary COPY layers

### Transcoder scales to zero during stream
**Symptom**: FFmpeg killed by SIGTERM (signal 15) after ~5 min of streaming.
**Cause**: Container Apps HTTP scaler sees no inbound requests after the initial `publish_start` webhook.
**Fix**: Verify `-hook-keepalive-interval 60s` and `stream_keepalive` webhook are configured on RTMP server. The 60s interval is well within the default 300s cooldown.

### Custom domain bindings stripped after Bicep redeploy
**Symptom**: `curl https://play.event-periscope.com` returns connection refused (000).
**Cause**: Bicep redeployment may remove custom domain bindings not declared in the template.
**Fix**: Re-bind using commands in Step 5.

### `RoleAssignmentExists` error in Bicep
**Error**: `The role assignment already exists.`
**Cause**: Bicep tries to create role assignments that already exist from a previous deployment.
**Impact**: Benign — all resources are actually deployed. Only the Bicep deployment status shows "Failed".
**Fix**: Ignore this error. Verify container apps have correct images and args.

### Image updated but container still runs old code
**Cause**: Named volume or revision not updated.
**Fix**: Force new revision with `--revision-suffix "fix-$(date +%s)"`. Verify active revision shows new image.

## Lessons Learned

- `az acr build` sends the local working directory to ACR — uncommitted changes ARE included in the image
- Go `flag` package ignores env vars — must pass all config as CLI args in Container Apps `command` array
- Container Apps ingress only exposes 80/443 — internal services use `.internal.` FQDNs on default ports
- HLS transcoder is multi-container (transcoder + blob-sidecar) — FFmpeg uploads to localhost:8081 (zero Envoy proxy)
- Log Analytics ingestion has ~15–30s delay — wait before querying after an event
- Bicep is mostly idempotent but custom domain bindings and role assignments are common re-run pain points

---
name: rtmp-go-stream-troubleshoot
description: 'Troubleshoot RTMP-go ingest-to-HLS playback pipeline on Azure Container Apps. Use when: stream not playing, HLS viewer blank, RTMP publish fails, transcoder not producing segments, webhook failures, stream state stuck, session not unlocking. Validates end-to-end: RTMP auth → publish → webhook → transcoder → HLS segments → blob storage → HLS server → viewer playback.'
argument-hint: 'Describe the symptom (e.g., "stream publishes but HLS viewer is blank")'
---

# RTMP-Go Stream Troubleshooting

End-to-end validation that an RTMP ingest stream produces playable HLS output. Covers the full pipeline: RTMP server → webhooks → HLS transcoder → blob storage → HLS server → viewer.

## Environment

- **Resource Group**: Determined from deployment (check `azure/infra/main.bicep` or ask user)
- **Services**: rtmp-server, hls-transcoder, sg-platform, sg-hls-server (all Azure Container Apps)
- **Log Analytics**: Query via `az containerapp logs show` or `az monitor log-analytics query`
- **Stream identification**: Event UUID + stream key (format: `live/<stream-key-hash>`)

## Procedure

Work through each layer sequentially. Stop at the first failure point and fix it before proceeding.

### Step 1: Verify RTMP Authentication

**Goal**: Confirm the RTMP server can authenticate publish requests.

```bash
# Check RTMP server logs for auth attempts
az containerapp logs show --name <rtmp-server> --resource-group <rg> --tail 50 2>/dev/null \
  | grep -i "auth\|publish\|credential"
```

**Common failures**:
- `invalid credentials` → INTERNAL_API_KEY mismatch between RTMP server and platform
- `context deadline exceeded` on auth callback → Platform app unreachable or slow
- `INVALID_STATE_ERR: 11` → Stream state locked (previous session not closed)

**Fix auth key mismatch**:
1. Get platform's key: `az containerapp show --name <platform> --resource-group <rg> --query "properties.template.containers[0].env[?name=='INTERNAL_API_KEY'].value" -o tsv`
2. Compare with RTMP server's key: same query on rtmp-server container
3. Update the mismatched one via YAML update + forced revision suffix

**Fix locked state**:
- Check if platform auto-unlocks stale sessions during re-auth (look for "RTMP session manually unlocked")
- If not: verify `publish_stop` webhooks are configured (see Step 4)

### Step 2: Verify RTMP Publish Succeeds

**Goal**: Confirm the stream is accepted and media flows.

```bash
az containerapp logs show --name <rtmp-server> --resource-group <rg> --tail 30 2>/dev/null \
  | grep -i "publish authenticated\|Codecs detected\|sequence header\|recorder"
```

**Expected sequence**:
1. `publish authenticated` — auth callback returned 200
2. `Codecs detected` — video (H264) + audio (AAC)
3. `Cached video/audio sequence header` — ready to relay
4. `segmented recorder initialized` — recording started

**If publish succeeds but no codecs**: Client is connecting but not sending media. Check OBS/FFmpeg output settings.

### Step 3: Verify Webhook Delivery (publish_start)

**Goal**: Confirm the RTMP server fires `publish_start` to the HLS transcoder.

```bash
# Check for hook execution in RTMP server logs
az containerapp logs show --name <rtmp-server> --resource-group <rg> --tail 50 2>/dev/null \
  | grep -i "Hook\|webhook"
```

**Expected**: `Hook registered` entries for `publish_start`, `publish_stop` at startup.
**On publish**: No errors (hooks fire async, errors show as `Hook execution failed`).

**Common failures**:
- `context deadline exceeded` → Target service unreachable (wrong FQDN, ingress misconfigured, service crash-looping)
- 404 → Wrong endpoint path in webhook URL
- No hook registered → Webhook env vars not configured in container app YAML

**Verify hook targets are reachable**:
```bash
# Check transcoder is running
az containerapp revision list --name <hls-transcoder> --resource-group <rg> \
  --query "[?properties.active].{name:name,state:properties.runningState}" -o table
```

### Step 4: Verify HLS Transcoder Receives Event and Starts FFmpeg

**Goal**: Confirm the transcoder spawns FFmpeg to pull the RTMP stream and produce HLS.

```bash
az containerapp logs show --name <hls-transcoder> --resource-group <rg> --tail 50 2>/dev/null \
  | grep -i "publish_start\|FFmpeg\|starting\|master.m3u8"
```

**Expected sequence**:
1. `publish_start event received` — webhook arrived
2. `starting FFmpeg transcoder` — with RTMP URL, profile, output mode
3. `FFmpeg transcoder started` — PID assigned
4. `master.m3u8 uploaded via HTTP` — first HLS manifest produced (~7s after start)

**Common failures**:
- Transcoder crash-looping → Check `az containerapp revision list` for `Failed` state
- `missing required flag: -platform-url` → Empty platform URL in container args
- Wrong API key → Transcoder can't fetch event config from platform
- `exit status 255` on FFmpeg → Can't connect to RTMP source (check RTMP URL, token, network)

**Fix crash-looping transcoder**:
```bash
# Get current args
az containerapp show --name <hls-transcoder> --resource-group <rg> \
  --query "properties.template.containers[0].args" -o json

# Export YAML, fix args, redeploy with new revision suffix
az containerapp show --name <hls-transcoder> --resource-group <rg> -o yaml > /tmp/transcoder.yaml
# Edit the args array, then:
az containerapp update --name <hls-transcoder> --resource-group <rg> --yaml /tmp/transcoder.yaml
```

### Step 5: Verify HLS Segments Reach Blob Storage

**Goal**: Confirm `.m3u8` and `.ts` files are uploaded to Azure Blob Storage.

```bash
# Check blob sidecar logs
az containerapp logs show --name <hls-transcoder> --resource-group <rg> --container blob-sidecar --tail 30 2>/dev/null \
  | grep -i "upload\|segment\|blob"

# Or check blob storage directly
az storage blob list --account-name <storage> --container-name hls-content \
  --prefix "<event-id>/" --query "[].name" -o tsv | head -20
```

**Expected**: `master.m3u8`, `stream_0/`, `stream_1/`, `stream_2/` with `.m3u8` playlists and `.ts` segments.

**Common failures**:
- `segment_complete` webhook returns 404 → Blob sidecar endpoint path wrong
- No segments in blob → FFmpeg not producing output (check transcoder logs for FFmpeg stderr)
- Partial segments → Stream disconnected mid-write

### Step 6: Verify HLS Server Serves Content

**Goal**: Confirm the HLS server can serve segments with proper JWT auth.

```bash
# Check HLS server health
az containerapp logs show --name <hls-server> --resource-group <rg> --tail 20 2>/dev/null \
  | grep -i "listening\|error\|revocation"

# Test with curl (need valid JWT)
curl -H "Authorization: Bearer <jwt>" "https://<hls-server-fqdn>/streams/<event-id>/master.m3u8"
```

**Common failures**:
- 401 → JWT expired or signing secret mismatch (`PLAYBACK_SIGNING_SECRET`)
- 404 → Wrong path mapping (`STREAM_KEY_PREFIX` misconfigured)
- 502 → HLS server can't reach upstream blob storage (`UPSTREAM_ORIGIN` wrong)

### Step 7: Verify Viewer Playback

**Goal**: Confirm the admin HLS preview or viewer portal plays the stream.

- Open admin console → event → preview player
- Check browser DevTools Network tab for `.m3u8` and `.ts` requests
- Check for CORS errors (HLS server `CORS_ALLOWED_ORIGIN` must match platform domain)

### Step 8: Verify Stream Stop Lifecycle

**Goal**: Confirm stopping the stream properly unlocks state.

```bash
# Monitor all services simultaneously during stop
az containerapp logs show --name <rtmp-server> --resource-group <rg> --follow --tail 0 2>/dev/null \
  | grep -i "teardown\|disconnect\|publish_stop"
```

**Expected on stop**:
1. RTMP server: `stream teardown` → `connection disconnected`
2. RTMP server fires `publish_stop` webhooks
3. Transcoder: `publish_stop event received` → `stopping FFmpeg` → `platform RTMP session closed`
4. Platform: RtmpSession record gets `endedAt` set

**If state doesn't unlock**: Check that both transcoder AND direct platform webhooks are configured for `publish_stop`.

## Key Environment Variables to Cross-Check

| Variable | Must Match Between |
|----------|-------------------|
| `INTERNAL_API_KEY` | RTMP server ↔ Platform |
| `PLAYBACK_SIGNING_SECRET` | Platform ↔ HLS Server |
| Platform URL (in transcoder args) | Transcoder → Platform internal FQDN |
| Webhook URLs (in RTMP server env) | RTMP server → Transcoder + Platform internal FQDNs |

## Quick Diagnostic Commands

```bash
# List all active revisions across services
for svc in rtmp-server hls-transcoder sg-platform sg-hls-server; do
  echo "=== $svc ==="
  az containerapp revision list --name "${svc}-<suffix>" --resource-group <rg> \
    --query "[?properties.active].{name:name,state:properties.runningState}" -o table
done

# Check for crash-looping containers
az containerapp revision list --name <app> --resource-group <rg> \
  --query "[?properties.runningState=='Failed' || properties.runningState=='Degraded']" -o table

# Force new revision after YAML update
az containerapp update --name <app> --resource-group <rg> --yaml /tmp/app.yaml --revision-suffix "fix-$(date +%s)"
```

## Lessons Learned

- Go `flag` package ignores env vars — must pass config as CLI args in Container Apps
- `allowInsecure: true` required for internal HTTP-only communication between containers
- Container Apps ingress only exposes port 80/443 — use internal FQDNs with default ports
- Always verify the actual deployed image/args before assuming config is correct
- Webhook hooks fire async with 30s timeout — if target is down, they silently fail after timeout
- The platform auto-unlocks stale sessions on re-auth as a safety net, but proper `publish_stop` delivery is the correct fix
- Deploy scripts (`deploy-unified.sh`, `rtmp-go/azure/deploy.sh`) run independently — secrets can drift between services
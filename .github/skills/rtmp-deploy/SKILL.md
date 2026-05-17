---
name: rtmp-deploy
description: 'Deploy or update the RTMP ingest server stack on Azure Container Apps using only Azure CLI. Use when: build RTMP server image, push to ACR, update container app, deploy new version, rollback RTMP server, update blob sidecar, update HLS transcoder, set up DNS for RTMP, check deployment status, change RTMP server flags, update webhook URLs, update auth config. No Bicep templates — pure CLI operations.'
argument-hint: 'Action + optional component, e.g. "build and deploy", "update rtmp-server only", "rollback", "dns setup", "status"'
---

# RTMP Server Deployment (CLI-Only)

Deploy and update the RTMP ingest server stack on Azure Container Apps using Azure CLI only. Covers image builds, container app updates, DNS, and rollback — independent of Bicep templates and GitHub Actions.

## When to Use

- Build and push a new RTMP server image to ACR
- Update one or all container apps with a new image
- Change RTMP server configuration (flags, webhooks, auth)
- Set up or verify DNS for the RTMP ingest domain
- Rollback to a previous revision
- Check deployment status across all apps

## Prerequisites

- Azure CLI logged in (`az account show`)
- Container Apps extension (`az extension show --name containerapp`)
- Docker context not required — builds happen in ACR

## Environment Discovery

The user may provide a resource group or domain as part of the slash-command argument. If not provided, use these defaults:

```bash
# Defaults — override via argument or env vars
RESOURCE_GROUP="${RESOURCE_GROUP:-event-periscope-ne}"
DNS_ZONE_NAME="${DNS_ZONE_NAME:-event-periscope.com}"
DNS_RESOURCE_GROUP="${DNS_RESOURCE_GROUP:-rg-dns}"
RTMP_SUBDOMAIN="${RTMP_SUBDOMAIN:-stream}"
DOMAIN="${RTMP_SUBDOMAIN}.${DNS_ZONE_NAME}"

# Auto-discover resource names
ACR_NAME=$(az acr list -g "$RESOURCE_GROUP" --query "[0].name" -o tsv)
ACR_LOGIN_SERVER=$(az acr show -n "$ACR_NAME" --query "loginServer" -o tsv)

RTMP_APP=$(az containerapp list -g "$RESOURCE_GROUP" \
  --query "[?contains(name,'rtmp-server')].name | [0]" -o tsv)
SIDECAR_APP=$(az containerapp list -g "$RESOURCE_GROUP" \
  --query "[?contains(name,'rec-blob-sidecar')].name | [0]" -o tsv)
TRANSCODER_APP=$(az containerapp list -g "$RESOURCE_GROUP" \
  --query "[?contains(name,'hls-transcoder')].name | [0]" -o tsv)

# Current images (for reference / partial updates)
CURRENT_RTMP_IMAGE=$(az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "properties.template.containers[0].image" -o tsv)
CURRENT_SIDECAR_IMAGE=$(az containerapp show -g "$RESOURCE_GROUP" -n "$SIDECAR_APP" \
  --query "properties.template.containers[0].image" -o tsv)
CURRENT_TRANSCODER_IMAGE=$(az containerapp show -g "$RESOURCE_GROUP" -n "$TRANSCODER_APP" \
  --query "properties.template.containers[0].image" -o tsv)
```

## Container Apps in the Stack

| App | Role | Ingress | Image |
|-----|------|---------|-------|
| `rtmp-server-*` | RTMP ingest, auth, recording, webhooks | TCP external :1935 (+:1936 RTMPS) | `rtmp-server:tag` |
| `rec-blob-sidecar-*` | Recording segment upload to blob storage | HTTP internal :8081 | `blob-sidecar:tag` |
| `hls-transcoder-*` | ABR transcoding (FFmpeg + blob-sidecar co-located) | HTTP internal :8090 | Multi-container: `hls-transcoder:tag` + `blob-sidecar:tag` |

## Procedures

### 1. Status Check

```bash
echo "=== Container App Revisions ==="
for APP in "$RTMP_APP" "$SIDECAR_APP" "$TRANSCODER_APP"; do
  echo "--- $APP ---"
  az containerapp revision list -g "$RESOURCE_GROUP" -n "$APP" \
    --query "[?properties.active].{name: name, image: properties.template.containers[0].image, state: properties.runningState, created: properties.createdTime}" \
    -o table 2>/dev/null
done

echo ""
echo "=== Current Images ==="
echo "RTMP Server:    $CURRENT_RTMP_IMAGE"
echo "Blob Sidecar:   $CURRENT_SIDECAR_IMAGE"
echo "HLS Transcoder: $CURRENT_TRANSCODER_IMAGE"

echo ""
echo "=== RTMP Server Command Flags ==="
az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "properties.template.containers[0].command" -o json

echo ""
echo "=== Ingress Config ==="
az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "properties.configuration.ingress.{targetPort: targetPort, exposedPort: exposedPort, transport: transport, additionalPorts: additionalPortMappings}" -o json
```

### 2. Build Images in ACR

Build one or all images. The RTMP server Dockerfile is at the repo root; sidecar and transcoder are under `azure/`.

```bash
IMAGE_TAG="v$(date +%s)"
PROJECT_ROOT="/Users/alex/Code/rtmp-go"

# Build RTMP server
echo "Building rtmp-server:${IMAGE_TAG}..."
az acr build \
  --registry "$ACR_NAME" \
  --image "rtmp-server:${IMAGE_TAG}" \
  --file "$PROJECT_ROOT/Dockerfile" \
  "$PROJECT_ROOT" \
  --output none

# Build blob sidecar
echo "Building blob-sidecar:${IMAGE_TAG}..."
az acr build \
  --registry "$ACR_NAME" \
  --image "blob-sidecar:${IMAGE_TAG}" \
  --file "$PROJECT_ROOT/azure/blob-sidecar/Dockerfile" \
  "$PROJECT_ROOT/azure/blob-sidecar" \
  --output none

# Build HLS transcoder
echo "Building hls-transcoder:${IMAGE_TAG}..."
az acr build \
  --registry "$ACR_NAME" \
  --image "hls-transcoder:${IMAGE_TAG}" \
  --file "$PROJECT_ROOT/azure/hls-transcoder/Dockerfile" \
  "$PROJECT_ROOT/azure/hls-transcoder" \
  --output none

echo "All images built: tag=${IMAGE_TAG}"
```

**To build only the RTMP server** (most common case): run just the first `az acr build` block.

### 3. Deploy New Image

Update a container app with a new image. Creates a new revision automatically.

```bash
# Update RTMP server
az containerapp update \
  -g "$RESOURCE_GROUP" \
  -n "$RTMP_APP" \
  --image "${ACR_LOGIN_SERVER}/rtmp-server:${IMAGE_TAG}" \
  --revision-suffix "deploy-$(date +%s)" \
  -o none

# Update blob sidecar
az containerapp update \
  -g "$RESOURCE_GROUP" \
  -n "$SIDECAR_APP" \
  --image "${ACR_LOGIN_SERVER}/blob-sidecar:${IMAGE_TAG}" \
  --revision-suffix "deploy-$(date +%s)" \
  -o none

# Update HLS transcoder (multi-container — update primary container)
az containerapp update \
  -g "$RESOURCE_GROUP" \
  -n "$TRANSCODER_APP" \
  --image "${ACR_LOGIN_SERVER}/hls-transcoder:${IMAGE_TAG}" \
  --revision-suffix "deploy-$(date +%s)" \
  -o none
```

**Verify** after deploying:

```bash
sleep 20
for APP in "$RTMP_APP" "$SIDECAR_APP" "$TRANSCODER_APP"; do
  STATE=$(az containerapp revision list -g "$RESOURCE_GROUP" -n "$APP" \
    --query "[?properties.active].properties.runningState | [0]" -o tsv 2>/dev/null)
  echo "$APP: $STATE"
done
```

### 4. Update RTMP Server Configuration

Change server flags without rebuilding the image. Requires exporting the full container spec, modifying command array, and updating.

```bash
# Get current command array
az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "properties.template.containers[0].command" -o json

# To change specific flags, export YAML, edit, and apply:
az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" -o yaml > /tmp/rtmp-app.yaml

# Edit the command array in /tmp/rtmp-app.yaml, then:
az containerapp update -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --yaml /tmp/rtmp-app.yaml \
  --revision-suffix "config-$(date +%s)" \
  -o none
```

**Common flag changes**:

| Change | Flag |
|--------|------|
| Auth mode | `-auth-mode callback` / `-auth-mode token` / `-auth-mode none` |
| Auth callback URL | `-auth-callback https://play.example.com/api/rtmp/auth` |
| Log level | `-log-level debug` / `-log-level info` |
| Metrics endpoint | `-metrics-addr :8080` |
| Recording | `-record-all true` / `-record-all false` |
| Segment duration | `-segment-duration 2m` |
| RTMPS | `-tls-listen :1936 -tls-cert /certs/tls-cert -tls-key /certs/tls-key` |

### 5. DNS Setup

Create or verify the CNAME record pointing `stream.<domain>` to the RTMP container app.

```bash
RTMP_FQDN=$(az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "properties.configuration.ingress.fqdn" -o tsv)

# Check existing CNAME
EXISTING=$(az network dns record-set cname show \
  -g "$DNS_RESOURCE_GROUP" -z "$DNS_ZONE_NAME" -n "$RTMP_SUBDOMAIN" \
  --query "cnameRecord.cname" -o tsv 2>/dev/null)

if [ -n "$EXISTING" ]; then
  echo "CNAME already exists: ${RTMP_SUBDOMAIN}.${DNS_ZONE_NAME} -> $EXISTING"
  if [ "$EXISTING" != "$RTMP_FQDN" ]; then
    echo "WARNING: CNAME target mismatch! Expected: $RTMP_FQDN"
  fi
else
  echo "Creating CNAME: ${RTMP_SUBDOMAIN}.${DNS_ZONE_NAME} -> $RTMP_FQDN"
  az network dns record-set cname set-record \
    -g "$DNS_RESOURCE_GROUP" -z "$DNS_ZONE_NAME" \
    -n "$RTMP_SUBDOMAIN" -c "$RTMP_FQDN" \
    --output none
  echo "CNAME created"
fi

# Verify DNS resolution
echo ""
echo "DNS resolution:"
dig +short "${RTMP_SUBDOMAIN}.${DNS_ZONE_NAME}" CNAME 2>/dev/null || \
  nslookup "${RTMP_SUBDOMAIN}.${DNS_ZONE_NAME}" 2>/dev/null | head -5

# Verify connectivity
nc -z -w5 "${DOMAIN}" 1935 && echo "RTMP :1935 OPEN" || echo "RTMP :1935 CLOSED"
```

### 6. Rollback

Activate a previous revision and deactivate the current one.

```bash
# List all revisions (including inactive)
az containerapp revision list -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "sort_by([].{name: name, active: properties.active, state: properties.runningState, created: properties.createdTime, image: properties.template.containers[0].image}, &created)" \
  -o table

# Activate a previous revision (replace with actual revision name)
ROLLBACK_REVISION="rtmp-server-hu2hm2lknz5jm--PREVIOUS_SUFFIX"
az containerapp revision activate -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --revision "$ROLLBACK_REVISION"

# Deactivate the bad revision
BAD_REVISION="rtmp-server-hu2hm2lknz5jm--CURRENT_SUFFIX"
az containerapp revision deactivate -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --revision "$BAD_REVISION"

# Shift all traffic to the rollback revision
az containerapp ingress traffic set -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --revision-weight "${ROLLBACK_REVISION}=100"
```

### 7. View Container Logs

```bash
# Recent logs (startup + errors)
az containerapp logs show -g "$RESOURCE_GROUP" -n "$RTMP_APP" --tail 50 2>/dev/null \
  | grep -i -E 'listen|start|error|tls|rtmps|auth|hook' | head -20

# Follow logs in real-time
az containerapp logs show -g "$RESOURCE_GROUP" -n "$RTMP_APP" --follow

# Sidecar logs
az containerapp logs show -g "$RESOURCE_GROUP" -n "$SIDECAR_APP" --tail 30 2>/dev/null

# Transcoder logs
az containerapp logs show -g "$RESOURCE_GROUP" -n "$TRANSCODER_APP" --tail 30 2>/dev/null
```

## Key Gotchas

1. **Image builds happen in ACR** — no local Docker required. Use `az acr build` which uploads source and builds remotely
2. **`az containerapp update --image` creates a new revision automatically** — no need for separate `--revision-suffix` unless you want a descriptive name
3. **HLS transcoder is multi-container** — it has both the FFmpeg transcoder and a blob-sidecar. Update each container's image separately if needed
4. **Changing command flags requires YAML export/import** — `az containerapp update` can't modify individual command args inline
5. **Scale-to-zero apps need traffic to start** — if `minReplicas=0`, the app won't have a running revision until it receives a request. Health probes on the sidecar keep it alive once started
6. **DNS is in a separate resource group** (`rg-dns`) — don't look for DNS records in the main resource group
7. **After Bicep redeploy, Key Vault certs get overwritten with placeholders** — run the rtmps-cert-manage skill to re-upload real certs

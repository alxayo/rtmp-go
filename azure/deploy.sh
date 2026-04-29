#!/usr/bin/env bash
# ============================================================================
# Deploy rtmp-go to Azure Container Apps
# ============================================================================
# Usage:
#   ./deploy.sh                          # interactive — prompts for auth token
#   RTMP_AUTH_TOKEN="live/stream=secret" ./deploy.sh   # non-interactive
#
# Environment variables:
#   RTMP_AUTH_TOKEN        — RTMP auth token (format: streamKey=secret)
#   RTMP_AUTH_CALLBACK_URL — optional auth callback URL (overrides token auth, e.g. https://platform/api/rtmp/auth)
#   STREAMGATE_HOOKS_URL   — optional Streamgate publish lifecycle webhook endpoint
#   INTERNAL_API_KEY       — optional API key for webhook hook authentication
#   RESOURCE_GROUP         — override resource group name (default: rg-rtmpgo)
#   LOCATION               — Azure region (default: eastus2)
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Verify container app is running after deployment ---
verify_deployment() {
  local app_name="$1"
  local max_retries=12  # 2 minutes with 10s intervals
  local retry=0

  echo "  Verifying $app_name..."

  while [ $retry -lt $max_retries ]; do
    local status
    status=$(az containerapp revision list \
      --name "$app_name" \
      -g "$RESOURCE_GROUP" \
      --query "[?properties.active].properties.runningState" \
      -o tsv 2>/dev/null | head -1)

    if echo "$status" | grep -qi "Running"; then
      echo "    ✓ $app_name is running"
      return 0
    fi

    retry=$((retry + 1))
    echo "    Waiting for $app_name to be ready... ($retry/$max_retries)"
    sleep 10
  done

  echo "    ✗ WARNING: $app_name may not be running after deployment"
  return 1
}

DEPLOY_WARNINGS=0

# --- Configuration ---
RESOURCE_GROUP="${RESOURCE_GROUP:-rg-rtmpgo}"
LOCATION="${LOCATION:-eastus2}"
STREAMGATE_HOOKS_URL="${STREAMGATE_HOOKS_URL:-}"
INTERNAL_API_KEY="${INTERNAL_API_KEY:-}"
RTMP_AUTH_CALLBACK_URL="${RTMP_AUTH_CALLBACK_URL:-}"
IMAGE_TAG="v$(date +%s)"

echo "============================================"
echo "  rtmp-go Azure Deployment"
echo "============================================"
echo "Resource Group:  $RESOURCE_GROUP"
echo "Location:        $LOCATION"
echo "Project Root:    $PROJECT_ROOT"
echo "============================================"

# --- Verify Azure CLI login ---
if ! az account show &>/dev/null; then
  echo "ERROR: Not logged in to Azure CLI. Run 'az login' first."
  exit 1
fi

# --- Step 1: Create Resource Group ---
echo ""
echo ">>> Step 1/5: Create resource group..."
if az group show --name "$RESOURCE_GROUP" &>/dev/null; then
  echo "    Resource group '$RESOURCE_GROUP' already exists."
else
  az group create --name "$RESOURCE_GROUP" --location "$LOCATION" --output none
  echo "    Resource group '$RESOURCE_GROUP' created."
fi

# --- Step 2: Prompt for auth token if not set ---
if [ -z "${RTMP_AUTH_TOKEN:-}" ]; then
  echo ""
  read -rp "Enter RTMP auth token (format: streamKey=secret, e.g. live/stream=mysecret123): " RTMP_AUTH_TOKEN
  if [ -z "$RTMP_AUTH_TOKEN" ]; then
    echo "ERROR: Auth token is required."
    exit 1
  fi
fi

# --- Step 3: Deploy Bicep infrastructure (first pass — creates ACR + infra) ---
echo ""
echo ">>> Step 2/5: Deploying infrastructure (Bicep)..."
DEPLOY_OUTPUT=$(az deployment group create \
  --resource-group "$RESOURCE_GROUP" \
  --template-file "$SCRIPT_DIR/infra/main.bicep" \
  --parameters "$SCRIPT_DIR/infra/main.parameters.json" \
  --parameters rtmpAuthToken="$RTMP_AUTH_TOKEN" \
  --parameters streamgateHooksUrl="$STREAMGATE_HOOKS_URL" \
  --parameters internalApiKey="$INTERNAL_API_KEY" \
  --parameters rtmpAuthCallbackUrl="$RTMP_AUTH_CALLBACK_URL" \
  --query 'properties.outputs' \
  --output json)

# Parse outputs (first pass)
ACR_NAME=$(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['registryName']['value'])")
ACR_LOGIN_SERVER=$(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['registryLoginServer']['value'])")
RTMP_FQDN=$(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['rtmpAppFqdn']['value'])")

echo "    Infrastructure deployed."
echo "    ACR: $ACR_LOGIN_SERVER"

# --- Step 4: Build & push Docker images using ACR Tasks ---
echo ""
echo ">>> Step 3/5: Building Docker images in ACR..."

echo "    Building rtmp-server..."
az acr build \
  --registry "$ACR_NAME" \
  --image "rtmp-server:${IMAGE_TAG}" \
  --file "$PROJECT_ROOT/Dockerfile" \
  "$PROJECT_ROOT" \
  --no-logs --output none

echo "    Building blob-sidecar..."
az acr build \
  --registry "$ACR_NAME" \
  --image "blob-sidecar:${IMAGE_TAG}" \
  --file "$PROJECT_ROOT/azure/blob-sidecar/Dockerfile" \
  "$PROJECT_ROOT/azure/blob-sidecar" \
  --no-logs --output none

echo "    Building hls-transcoder..."
az acr build \
  --registry "$ACR_NAME" \
  --image "hls-transcoder:${IMAGE_TAG}" \
  --file "$PROJECT_ROOT/azure/hls-transcoder/Dockerfile" \
  "$PROJECT_ROOT/azure/hls-transcoder" \
  --no-logs --output none

echo "    Images built and pushed."

# --- Step 5: Redeploy Bicep with real container images ---
echo ""
echo ">>> Step 4/5: Deploying container apps with built images..."
DEPLOY_OUTPUT=$(az deployment group create \
  --resource-group "$RESOURCE_GROUP" \
  --template-file "$SCRIPT_DIR/infra/main.bicep" \
  --parameters "$SCRIPT_DIR/infra/main.parameters.json" \
  --parameters rtmpAuthToken="$RTMP_AUTH_TOKEN" \
  --parameters streamgateHooksUrl="$STREAMGATE_HOOKS_URL" \
  --parameters internalApiKey="$INTERNAL_API_KEY" \
  --parameters rtmpAuthCallbackUrl="$RTMP_AUTH_CALLBACK_URL" \
  --parameters rtmpServerImage="${ACR_LOGIN_SERVER}/rtmp-server:${IMAGE_TAG}" \
  --parameters blobSidecarImage="${ACR_LOGIN_SERVER}/blob-sidecar:${IMAGE_TAG}" \
  --parameters hlsTranscoderImage="${ACR_LOGIN_SERVER}/hls-transcoder:${IMAGE_TAG}" \
  --query 'properties.outputs' \
  --output json)

RTMP_FQDN=$(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['rtmpAppFqdn']['value'])")

# --- Step 6: Verify ---
echo ""
echo ">>> Step 5/5: Verifying deployment..."
RTMP_APP_NAME=$(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['rtmpAppName']['value'])")
SIDECAR_APP_NAME=$(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['sidecarAppName']['value'])")
HLS_APP_NAME=$(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['hlsAppName']['value'])")
HLS_SIDECAR_APP_NAME=$(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['hlsSidecarAppName']['value'])")

verify_deployment "$RTMP_APP_NAME" || DEPLOY_WARNINGS=$((DEPLOY_WARNINGS + 1))
verify_deployment "$SIDECAR_APP_NAME" || DEPLOY_WARNINGS=$((DEPLOY_WARNINGS + 1))
verify_deployment "$HLS_APP_NAME" || DEPLOY_WARNINGS=$((DEPLOY_WARNINGS + 1))
# hls-sidecar is co-located (scaled to 0), skip verification

# --- Deployment Summary ---
echo ""
echo "=== Deployment Summary ==="
echo "Image tag: $IMAGE_TAG"
echo "Resources deployed:"
for app in "$RTMP_APP_NAME" "$SIDECAR_APP_NAME" "$HLS_APP_NAME" "$HLS_SIDECAR_APP_NAME"; do
  az containerapp show --name "$app" -g "$RESOURCE_GROUP" \
    --query "{Name:name, Revision:properties.latestRevisionName, FQDN:properties.configuration.ingress.fqdn}" \
    -o table 2>/dev/null
done
if [ "$DEPLOY_WARNINGS" -gt 0 ]; then
  echo ""
  echo "⚠ $DEPLOY_WARNINGS app(s) may not be running — check Azure Portal for details."
fi

SUBSCRIPTION=$(az account show --query 'id' --output tsv)

echo ""
echo "============================================"
echo "  Deployment Complete!"
echo "============================================"
echo ""
echo "RTMP Endpoint (ACA FQDN):"
echo "  rtmp://${RTMP_FQDN}/live/stream?token=<your-secret>"
echo ""
echo "Custom Domain Endpoint (after DNS setup):"
echo "  rtmp://stream.port-80.com/live/stream?token=<your-secret>"
echo ""
echo "Test with ffmpeg (recommended for clean source stream):"
echo "  ffmpeg -re -i test.mp4 \\"
echo "    -c:v libx264 -profile:v baseline -bf 0 -g 60 -keyint_min 60 \\"
echo "    -b:v 4500k -maxrate 5000k -bufsize 9000k -preset veryfast \\"
echo "    -c:a aac -b:a 128k -ar 48000 \\"
echo "    -f flv \"rtmp://${RTMP_FQDN}/live/stream?token=<your-secret>\""
echo ""
echo "OBS Studio (Output > Encoder: x264):"
echo "  Server:      rtmp://${RTMP_FQDN}/live"
echo "  Stream Key:  stream?token=<your-secret>"
echo "  Profile:     Baseline  |  B-frames: 0"
echo "  Rate Control: CBR 4500 Kbps  |  Keyframe Interval: 2s"
echo "  See docs/obs-streaming-guide.md for full settings"
echo ""
echo "Azure Portal:"
echo "  https://portal.azure.com/#@/resource/subscriptions/${SUBSCRIPTION}/resourceGroups/${RESOURCE_GROUP}/overview"
echo ""
echo "DNS Setup (run once, then update GoDaddy nameservers):"
echo "  RTMP_APP_FQDN=\"${RTMP_FQDN}\" ./dns-deploy.sh"
echo ""
echo "To tear down app resources:  ./destroy.sh"
echo "To tear down DNS zone:       ./dns-destroy.sh"
echo "============================================"

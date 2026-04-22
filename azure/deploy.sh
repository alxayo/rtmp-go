#!/usr/bin/env bash
# ============================================================================
# Deploy rtmp-go to Azure Container Apps
# ============================================================================
# Usage:
#   ./deploy.sh                          # interactive — prompts for auth token
#   RTMP_AUTH_TOKEN="live/stream=secret" ./deploy.sh   # non-interactive
#
# Environment variables:
#   RTMP_AUTH_TOKEN  — RTMP auth token (format: streamKey=secret)
#   RESOURCE_GROUP   — override resource group name (default: rg-rtmpgo)
#   LOCATION         — Azure region (default: eastus2)
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# --- Configuration ---
RESOURCE_GROUP="${RESOURCE_GROUP:-rg-rtmpgo}"
LOCATION="${LOCATION:-eastus2}"

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
  --query 'properties.outputs' \
  --output json)

# Parse outputs
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
  --image rtmp-server:latest \
  --file "$PROJECT_ROOT/Dockerfile" \
  "$PROJECT_ROOT" \
  --no-logs --output none

echo "    Building blob-sidecar..."
az acr build \
  --registry "$ACR_NAME" \
  --image blob-sidecar:latest \
  --file "$PROJECT_ROOT/azure/blob-sidecar/Dockerfile" \
  "$PROJECT_ROOT/azure/blob-sidecar" \
  --no-logs --output none

echo "    Building hls-transcoder..."
az acr build \
  --registry "$ACR_NAME" \
  --image hls-transcoder:latest \
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
  --parameters rtmpServerImage="${ACR_LOGIN_SERVER}/rtmp-server:latest" \
  --parameters blobSidecarImage="${ACR_LOGIN_SERVER}/blob-sidecar:latest" \
  --parameters hlsTranscoderImage="${ACR_LOGIN_SERVER}/hls-transcoder:latest" \
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

RTMP_STATUS=$(az containerapp show --name "$RTMP_APP_NAME" --resource-group "$RESOURCE_GROUP" \
  --query 'properties.runningStatus' --output tsv 2>/dev/null || echo "Unknown")
SIDECAR_STATUS=$(az containerapp show --name "$SIDECAR_APP_NAME" --resource-group "$RESOURCE_GROUP" \
  --query 'properties.runningStatus' --output tsv 2>/dev/null || echo "Unknown")
HLS_STATUS=$(az containerapp show --name "$HLS_APP_NAME" --resource-group "$RESOURCE_GROUP" \
  --query 'properties.runningStatus' --output tsv 2>/dev/null || echo "Unknown")
HLS_SIDECAR_STATUS=$(az containerapp show --name "$HLS_SIDECAR_APP_NAME" --resource-group "$RESOURCE_GROUP" \
  --query 'properties.runningStatus' --output tsv 2>/dev/null || echo "Unknown")

echo "    rtmp-server:       $RTMP_STATUS"
echo "    blob-sidecar:      $SIDECAR_STATUS"
echo "    hls-transcoder:    $HLS_STATUS"
echo "    hls-blob-sidecar:  $HLS_SIDECAR_STATUS"

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
echo "Test with ffmpeg:"
echo "  ffmpeg -re -i test.mp4 -c copy -f flv \\"
echo "    \"rtmp://${RTMP_FQDN}/live/stream?token=<your-secret>\""
echo ""
echo "OBS Studio:"
echo "  Server:     rtmp://${RTMP_FQDN}/live"
echo "  Stream Key:  stream?token=<your-secret>"
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

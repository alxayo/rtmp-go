#!/usr/bin/env bash
# ============================================================================
# Deploy Azure Monitoring Dashboard for rtmp-go + StreamGate
# ============================================================================
# Creates a shared Azure Portal dashboard with CPU, memory, network, HTTP,
# replica, restart, and storage metrics for all Container Apps.
#
# Usage:
#   ./deploy-dashboard.sh
#
# Prerequisites:
#   - Azure CLI logged in
#   - rtmp-go and StreamGate already deployed to rg-rtmpgo
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RESOURCE_GROUP="${RESOURCE_GROUP:-rg-rtmpgo}"

echo "============================================"
echo "  Monitoring Dashboard Deployment"
echo "============================================"
echo "Resource Group: $RESOURCE_GROUP"
echo "============================================"

# --- Verify Azure CLI login ---
if ! az account show &>/dev/null; then
  echo "ERROR: Not logged in to Azure CLI. Run 'az login' first."
  exit 1
fi

# --- Discover container app names by tags ---
echo ""
echo ">>> Discovering container apps..."

RTMP_SERVER=$(az containerapp list -g "$RESOURCE_GROUP" --query "[?tags.role=='rtmp-server'].name | [0]" -o tsv 2>/dev/null)
REC_SIDECAR=$(az containerapp list -g "$RESOURCE_GROUP" --query "[?tags.role=='rec-blob-sidecar'].name | [0]" -o tsv 2>/dev/null)
HLS_TRANSCODER=$(az containerapp list -g "$RESOURCE_GROUP" --query "[?tags.role=='hls-transcoder'].name | [0]" -o tsv 2>/dev/null)
HLS_SIDECAR=$(az containerapp list -g "$RESOURCE_GROUP" --query "[?tags.role=='hls-blob-sidecar'].name | [0]" -o tsv 2>/dev/null)
SG_HLS=$(az containerapp list -g "$RESOURCE_GROUP" --query "[?tags.role=='hls-server'].name | [0]" -o tsv 2>/dev/null)
SG_PLATFORM=$(az containerapp list -g "$RESOURCE_GROUP" --query "[?tags.role=='platform'].name | [0]" -o tsv 2>/dev/null)

# Validate all found
for APP_VAR in RTMP_SERVER REC_SIDECAR HLS_TRANSCODER HLS_SIDECAR SG_HLS SG_PLATFORM; do
  if [ -z "${!APP_VAR}" ]; then
    echo "ERROR: Could not find container app with expected tag. Missing: $APP_VAR"
    echo "       Ensure all apps are tagged (role=rtmp-server, etc.)"
    exit 1
  fi
done

echo "    RTMP Server:       $RTMP_SERVER"
echo "    Rec Blob Sidecar:  $REC_SIDECAR"
echo "    HLS Transcoder:    $HLS_TRANSCODER"
echo "    HLS Blob Sidecar:  $HLS_SIDECAR"
echo "    SG HLS Server:     $SG_HLS"
echo "    SG Platform:       $SG_PLATFORM"

# --- Discover shared resources ---
echo ""
echo ">>> Discovering shared resources..."

STORAGE_ACCOUNT=$(az storage account list -g "$RESOURCE_GROUP" --query "[0].name" -o tsv 2>/dev/null)
LOG_ANALYTICS=$(az monitor log-analytics workspace list -g "$RESOURCE_GROUP" --query "[0].name" -o tsv 2>/dev/null)
CONTAINER_ENV=$(az containerapp env list -g "$RESOURCE_GROUP" --query "[0].name" -o tsv 2>/dev/null)

echo "    Storage Account:   $STORAGE_ACCOUNT"
echo "    Log Analytics:     $LOG_ANALYTICS"
echo "    Container Env:     $CONTAINER_ENV"

# --- Deploy dashboard ---
echo ""
echo ">>> Deploying monitoring dashboard..."

DEPLOY_OUTPUT=$(az deployment group create \
  --resource-group "$RESOURCE_GROUP" \
  --name "monitoring-dashboard" \
  --template-file "$SCRIPT_DIR/infra/dashboard.bicep" \
  --parameters \
    rtmpServerApp="$RTMP_SERVER" \
    recBlobSidecarApp="$REC_SIDECAR" \
    hlsTranscoderApp="$HLS_TRANSCODER" \
    hlsBlobSidecarApp="$HLS_SIDECAR" \
    sgHlsServerApp="$SG_HLS" \
    sgPlatformApp="$SG_PLATFORM" \
    storageAccountName="$STORAGE_ACCOUNT" \
    logAnalyticsName="$LOG_ANALYTICS" \
    containerEnvName="$CONTAINER_ENV" \
  --query 'properties.outputs' \
  --output json)

DASHBOARD_NAME=$(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['dashboardName']['value'])")
PORTAL_URL=$(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['portalUrl']['value'])")

echo ""
echo "============================================"
echo "  Dashboard Deployed!"
echo "============================================"
echo ""
echo "Dashboard: $DASHBOARD_NAME"
echo ""
echo "Open in Azure Portal:"
echo "  $PORTAL_URL"
echo ""
echo "Sections:"
echo "  • CPU Utilization (all 6 Container Apps)"
echo "  • Memory Working Set (all 6 Container Apps)"
echo "  • Network TX/RX (all 6 Container Apps)"
echo "  • HTTP Requests (StreamGate + rtmp-go services)"
echo "  • Replica Count & Container Restarts"
echo "  • Storage Transactions & Bandwidth"
echo "  • Resource Inventory Table"
echo ""
echo "To remove: az resource delete --ids $(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['dashboardId']['value'])")"
echo "============================================"

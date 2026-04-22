#!/usr/bin/env bash
# ============================================================================
# Deploy Azure DNS Zone for custom domain (stream.port-80.com)
# ============================================================================
# Creates an Azure DNS Zone in a dedicated resource group and optionally
# adds a CNAME record pointing to the RTMP Container App FQDN.
#
# Usage:
#   # First time — create zone, get nameservers for GoDaddy delegation:
#   ./dns-deploy.sh
#
#   # After main deploy — create/update zone + CNAME record:
#   RTMP_APP_FQDN="azapp....azurecontainerapps.io" ./dns-deploy.sh
#
# Environment variables:
#   RTMP_APP_FQDN       — Container App FQDN (from deploy.sh output). Optional.
#   DNS_RESOURCE_GROUP   — Resource group for DNS zone (default: rg-dns)
#   DNS_ZONE_NAME        — Domain name (default: port-80.com)
#   DNS_SUBDOMAIN        — Subdomain for streaming (default: stream)
#   LOCATION             — Azure region for the RG (default: eastus2)
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# --- Configuration ---
DNS_RESOURCE_GROUP="${DNS_RESOURCE_GROUP:-rg-dns}"
DNS_ZONE_NAME="${DNS_ZONE_NAME:-port-80.com}"
DNS_SUBDOMAIN="${DNS_SUBDOMAIN:-stream}"
LOCATION="${LOCATION:-eastus2}"
RTMP_APP_FQDN="${RTMP_APP_FQDN:-}"

echo "============================================"
echo "  DNS Zone Deployment"
echo "============================================"
echo "Resource Group:  $DNS_RESOURCE_GROUP"
echo "DNS Zone:        $DNS_ZONE_NAME"
echo "Subdomain:       $DNS_SUBDOMAIN"
if [ -n "$RTMP_APP_FQDN" ]; then
  echo "CNAME Target:    $RTMP_APP_FQDN"
else
  echo "CNAME Target:    (skipped — set RTMP_APP_FQDN to create)"
fi
echo "============================================"

# --- Verify Azure CLI login ---
if ! az account show &>/dev/null; then
  echo "ERROR: Not logged in to Azure CLI. Run 'az login' first."
  exit 1
fi

# --- Step 1: Create resource group ---
echo ""
echo ">>> Step 1/3: Create resource group..."
if az group show --name "$DNS_RESOURCE_GROUP" &>/dev/null 2>&1; then
  echo "    Resource group '$DNS_RESOURCE_GROUP' already exists."
else
  az group create --name "$DNS_RESOURCE_GROUP" --location "$LOCATION" --output none
  echo "    Resource group '$DNS_RESOURCE_GROUP' created."
fi

# --- Step 2: Deploy DNS Bicep template ---
echo ""
echo ">>> Step 2/3: Deploying DNS zone..."

DEPLOY_PARAMS=("$SCRIPT_DIR/infra/dns.parameters.json")
if [ -n "$RTMP_APP_FQDN" ]; then
  DEPLOY_PARAMS+=("rtmpAppFqdn=$RTMP_APP_FQDN")
fi

DEPLOY_OUTPUT=$(az deployment group create \
  --resource-group "$DNS_RESOURCE_GROUP" \
  --template-file "$SCRIPT_DIR/infra/dns.bicep" \
  --parameters "${DEPLOY_PARAMS[@]}" \
  --query 'properties.outputs' \
  --output json)

# Parse outputs
NAME_SERVERS=$(echo "$DEPLOY_OUTPUT" | python3 -c "
import sys, json
ns = json.load(sys.stdin)['nameServers']['value']
for s in ns:
    print('    ' + s)
")
CUSTOM_DOMAIN=$(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['customDomain']['value'])")
CNAME_TARGET=$(echo "$DEPLOY_OUTPUT" | python3 -c "import sys,json; print(json.load(sys.stdin)['cnameTarget']['value'])")

echo "    DNS zone '$DNS_ZONE_NAME' deployed."

# --- Step 3: Display results ---
echo ""
echo ">>> Step 3/3: Results"
echo ""
echo "============================================"
echo "  DNS Zone Deployed!"
echo "============================================"
echo ""
echo "Azure DNS Nameservers:"
echo "$NAME_SERVERS"
echo ""

if [ -n "$RTMP_APP_FQDN" ]; then
  echo "CNAME Record:"
  echo "    $DNS_SUBDOMAIN.$DNS_ZONE_NAME → $CNAME_TARGET"
  echo ""
  echo "Streaming URL (after GoDaddy delegation):"
  echo "    rtmp://${CUSTOM_DOMAIN}/live/stream?token=<your-secret>"
  echo ""
fi

# --- GoDaddy instructions ---
echo "--------------------------------------------"
echo "  GoDaddy Nameserver Delegation (one-time)"
echo "--------------------------------------------"
echo ""
echo "To delegate $DNS_ZONE_NAME to Azure DNS:"
echo ""
echo "  1. Log in to GoDaddy: https://dcc.godaddy.com/domains/$DNS_ZONE_NAME/dns"
echo "  2. Scroll to 'Nameservers' → click 'Change'"
echo "  3. Select 'Enter my own nameservers (advanced)'"
echo "  4. Replace ALL existing nameservers with:"
echo ""
echo "$NAME_SERVERS"
echo ""
echo "  5. Click 'Save' and confirm the warning"
echo ""
echo "Propagation typically takes a few minutes (up to 48h)."
echo "Verify with: nslookup -type=NS $DNS_ZONE_NAME"
echo ""
echo "This only needs to be done ONCE. Subsequent runs"
echo "of this script just update the CNAME record."
echo "============================================"

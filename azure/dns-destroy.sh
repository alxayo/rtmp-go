#!/usr/bin/env bash
# ============================================================================
# Destroy Azure DNS Zone — removes the DNS resource group
# ============================================================================
# WARNING: This will delete the DNS zone and all records. After deletion,
# stream.port-80.com will stop resolving. If you recreate the zone later,
# Azure will assign NEW nameservers and you'll need to update GoDaddy again.
#
# Usage:
#   ./dns-destroy.sh                              # prompts for confirmation
#   ./dns-destroy.sh --yes                        # skip confirmation
#   DNS_RESOURCE_GROUP=rg-dns ./dns-destroy.sh    # custom resource group
# ============================================================================
set -euo pipefail

DNS_RESOURCE_GROUP="${DNS_RESOURCE_GROUP:-rg-dns}"
SKIP_CONFIRM=false

for arg in "$@"; do
  case "$arg" in
    --yes|-y) SKIP_CONFIRM=true ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

# --- Verify Azure CLI login ---
if ! az account show &>/dev/null; then
  echo "ERROR: Not logged in to Azure CLI. Run 'az login' first."
  exit 1
fi

# --- Check resource group exists ---
if ! az group show --name "$DNS_RESOURCE_GROUP" &>/dev/null 2>&1; then
  echo "Resource group '$DNS_RESOURCE_GROUP' does not exist. Nothing to destroy."
  exit 0
fi

# --- List resources ---
echo "============================================"
echo "  DNS Zone Teardown"
echo "============================================"
echo ""
echo "Resource Group: $DNS_RESOURCE_GROUP"
echo ""
echo "WARNING: Deleting the DNS zone means:"
echo "  - stream.port-80.com will STOP resolving"
echo "  - Recreating the zone assigns NEW nameservers"
echo "  - You will need to update GoDaddy nameservers again"
echo ""
echo "The following resources will be PERMANENTLY DELETED:"
echo ""
az resource list --resource-group "$DNS_RESOURCE_GROUP" \
  --query '[].{Name:name, Type:type}' --output table 2>/dev/null
echo ""

# --- Confirm ---
if [ "$SKIP_CONFIRM" = false ]; then
  read -rp "Type the resource group name to confirm deletion [$DNS_RESOURCE_GROUP]: " CONFIRM
  if [ "$CONFIRM" != "$DNS_RESOURCE_GROUP" ]; then
    echo "Aborted. Input did not match resource group name."
    exit 1
  fi
fi

# --- Delete ---
echo ""
echo "Deleting resource group '$DNS_RESOURCE_GROUP' and all resources..."
az group delete --name "$DNS_RESOURCE_GROUP" --yes --no-wait

echo ""
echo "============================================"
echo "  Deletion initiated (async)"
echo "============================================"
echo ""
echo "Azure is deleting resources in the background."
echo "This typically takes 1-2 minutes."
echo ""
echo "Check status:"
echo "  az group show --name $DNS_RESOURCE_GROUP --query properties.provisioningState --output tsv"
echo ""
echo "Or wait synchronously:"
echo "  az group wait --name $DNS_RESOURCE_GROUP --deleted"
echo "============================================"

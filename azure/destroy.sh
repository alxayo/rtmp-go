#!/usr/bin/env bash
# ============================================================================
# Destroy rtmp-go Azure environment — removes ALL resources
# ============================================================================
# Usage:
#   ./destroy.sh                           # deletes rg-rtmpgo (prompts for confirmation)
#   ./destroy.sh --yes                     # skip confirmation prompt
#   RESOURCE_GROUP=rg-custom ./destroy.sh  # delete a custom resource group
# ============================================================================
set -euo pipefail

RESOURCE_GROUP="${RESOURCE_GROUP:-rg-rtmpgo}"
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
if ! az group show --name "$RESOURCE_GROUP" &>/dev/null 2>&1; then
  echo "Resource group '$RESOURCE_GROUP' does not exist. Nothing to destroy."
  exit 0
fi

# --- List resources that will be deleted ---
echo "============================================"
echo "  rtmp-go Azure Teardown"
echo "============================================"
echo ""
echo "Resource Group: $RESOURCE_GROUP"
echo ""
echo "The following resources will be PERMANENTLY DELETED:"
echo ""
az resource list --resource-group "$RESOURCE_GROUP" \
  --query '[].{Name:name, Type:type}' --output table 2>/dev/null
echo ""

# --- Confirm ---
if [ "$SKIP_CONFIRM" = false ]; then
  read -rp "Type the resource group name to confirm deletion [$RESOURCE_GROUP]: " CONFIRM
  if [ "$CONFIRM" != "$RESOURCE_GROUP" ]; then
    echo "Aborted. Input did not match resource group name."
    exit 1
  fi
fi

# --- Delete ---
echo ""
echo "Deleting resource group '$RESOURCE_GROUP' and all resources..."
az group delete --name "$RESOURCE_GROUP" --yes --no-wait

echo ""
echo "============================================"
echo "  Deletion initiated (async)"
echo "============================================"
echo ""
echo "Azure is deleting resources in the background."
echo "This typically takes 2-5 minutes."
echo ""
echo "Check status:"
echo "  az group show --name $RESOURCE_GROUP --query properties.provisioningState --output tsv"
echo ""
echo "Or wait synchronously:"
echo "  az group wait --name $RESOURCE_GROUP --deleted"
echo "============================================"

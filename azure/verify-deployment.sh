#!/bin/bash
# ============================================================================
# verify-deployment.sh — Verify Phase 3 HTTP Ingest Deployment
# ============================================================================
# Validates that blob-sidecar and hls-transcoder are correctly configured
# for HTTP ingest communication within Azure Container Apps.
#
# Usage:
#   ./azure/verify-deployment.sh
#
# Exit codes:
#   0 = All checks passed ✓
#   1 = One or more checks failed ✗
# ============================================================================

set -euo pipefail

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
RESOURCE_GROUP="${RESOURCE_GROUP:-rg-rtmpgo}"
LOCATION="${LOCATION:-eastus2}"
FAILED_CHECKS=0

# Helper functions
info() {
    echo -e "${BLUE}ℹ${NC} $*"
}

success() {
    echo -e "${GREEN}✓${NC} $*"
}

error() {
    echo -e "${RED}✗${NC} $*"
    FAILED_CHECKS=$((FAILED_CHECKS + 1))
}

warning() {
    echo -e "${YELLOW}⚠${NC} $*"
}

# ============================================================================
# Check 1: Resource Group exists
# ============================================================================
check_resource_group() {
    info "Checking resource group: $RESOURCE_GROUP"
    
    if ! az group show -g "$RESOURCE_GROUP" &>/dev/null; then
        error "Resource group '$RESOURCE_GROUP' not found"
        error "Create resource group with: az group create -g $RESOURCE_GROUP -l $LOCATION"
        exit 1
    fi
    
    success "Resource group exists"
}

# ============================================================================
# Check 2: blob-sidecar container app exists
# ============================================================================
check_blob_sidecar_exists() {
    info "Checking blob-sidecar container app"
    
    BLOB_APPS=$(az containerapp list -g "$RESOURCE_GROUP" --query "[?tags.role=='rec-blob-sidecar'] | [0].name" -o tsv 2>/dev/null || true)
    
    if [ -z "$BLOB_APPS" ]; then
        error "blob-sidecar container app not found"
        return 1
    fi
    
    BLOB_SIDECAR_APP="$BLOB_APPS"
    success "blob-sidecar found: $BLOB_SIDECAR_APP"
}

# ============================================================================
# Check 3: hls-transcoder container app exists
# ============================================================================
check_hls_transcoder_exists() {
    info "Checking hls-transcoder container app"
    
    HLS_APPS=$(az containerapp list -g "$RESOURCE_GROUP" --query "[?tags.role=='hls-transcoder'] | [0].name" -o tsv 2>/dev/null || true)
    
    if [ -z "$HLS_APPS" ]; then
        error "hls-transcoder container app not found"
        return 1
    fi
    
    HLS_TRANSCODER_APP="$HLS_APPS"
    success "hls-transcoder found: $HLS_TRANSCODER_APP"
}

# ============================================================================
# Check 4: blob-sidecar environment variables
# ============================================================================
check_blob_sidecar_env() {
    info "Checking blob-sidecar environment variables"
    
    ENV_JSON=$(az containerapp show -g "$RESOURCE_GROUP" -n "$BLOB_SIDECAR_APP" --query 'template.containers[0].env' -o json 2>/dev/null || echo "[]")
    
    # Check for required env variables
    INGEST_ADDR=$(echo "$ENV_JSON" | jq -r '.[] | select(.name=="INGEST_ADDR") | .value' 2>/dev/null || true)
    INGEST_STORAGE=$(echo "$ENV_JSON" | jq -r '.[] | select(.name=="INGEST_STORAGE") | .value' 2>/dev/null || true)
    INGEST_TOKEN=$(echo "$ENV_JSON" | jq -r '.[] | select(.name=="INGEST_TOKEN") | .secretRef' 2>/dev/null || true)
    INGEST_MAX_BODY=$(echo "$ENV_JSON" | jq -r '.[] | select(.name=="INGEST_MAX_BODY") | .value' 2>/dev/null || true)
    
    if [ -z "$INGEST_ADDR" ]; then
        error "INGEST_ADDR not set on blob-sidecar"
    else
        success "INGEST_ADDR=$INGEST_ADDR"
    fi
    
    if [ -z "$INGEST_STORAGE" ]; then
        error "INGEST_STORAGE not set on blob-sidecar"
    else
        success "INGEST_STORAGE=$INGEST_STORAGE"
    fi
    
    if [ -z "$INGEST_TOKEN" ]; then
        error "INGEST_TOKEN not configured on blob-sidecar"
    else
        success "INGEST_TOKEN configured (secret reference)"
    fi
    
    if [ -z "$INGEST_MAX_BODY" ]; then
        error "INGEST_MAX_BODY not set on blob-sidecar"
    else
        success "INGEST_MAX_BODY=$INGEST_MAX_BODY bytes"
    fi
}

# ============================================================================
# Check 5: hls-transcoder environment variables
# ============================================================================
check_hls_transcoder_env() {
    info "Checking hls-transcoder environment variables"
    
    ENV_JSON=$(az containerapp show -g "$RESOURCE_GROUP" -n "$HLS_TRANSCODER_APP" --query 'template.containers[0].env' -o json 2>/dev/null || echo "[]")
    
    # Check for required env variables
    OUTPUT_MODE=$(echo "$ENV_JSON" | jq -r '.[] | select(.name=="OUTPUT_MODE") | .value' 2>/dev/null || true)
    INGEST_URL=$(echo "$ENV_JSON" | jq -r '.[] | select(.name=="INGEST_URL") | .value' 2>/dev/null || true)
    INGEST_TOKEN=$(echo "$ENV_JSON" | jq -r '.[] | select(.name=="INGEST_TOKEN") | .secretRef' 2>/dev/null || true)
    
    if [ -z "$OUTPUT_MODE" ]; then
        error "OUTPUT_MODE not set on hls-transcoder (should be 'http' for Phase 3)"
    else
        if [ "$OUTPUT_MODE" = "http" ]; then
            success "OUTPUT_MODE=http (Phase 3 HTTP ingest enabled)"
        else
            warning "OUTPUT_MODE=$OUTPUT_MODE (expected 'http' for Phase 3)"
        fi
    fi
    
    if [ -z "$INGEST_URL" ]; then
        error "INGEST_URL not set on hls-transcoder"
    else
        success "INGEST_URL=$INGEST_URL"
    fi
    
    if [ -z "$INGEST_TOKEN" ]; then
        error "INGEST_TOKEN not configured on hls-transcoder"
    else
        success "INGEST_TOKEN configured (secret reference)"
    fi
}

# ============================================================================
# Check 6: Container Apps status
# ============================================================================
check_container_status() {
    info "Checking container app provisioning state"
    
    BLOB_STATE=$(az containerapp show -g "$RESOURCE_GROUP" -n "$BLOB_SIDECAR_APP" --query 'properties.provisioningState' -o tsv 2>/dev/null || true)
    if [ "$BLOB_STATE" = "Succeeded" ]; then
        success "blob-sidecar provisioning state: $BLOB_STATE"
    else
        error "blob-sidecar provisioning state: $BLOB_STATE (expected 'Succeeded')"
    fi
    
    HLS_STATE=$(az containerapp show -g "$RESOURCE_GROUP" -n "$HLS_TRANSCODER_APP" --query 'properties.provisioningState' -o tsv 2>/dev/null || true)
    if [ "$HLS_STATE" = "Succeeded" ]; then
        success "hls-transcoder provisioning state: $HLS_STATE"
    else
        error "hls-transcoder provisioning state: $HLS_STATE (expected 'Succeeded')"
    fi
}

# ============================================================================
# Check 7: Network connectivity simulation
# ============================================================================
check_network_connectivity() {
    info "Checking network topology (Phase 3 HTTP ingest)"
    
    # Get the container environment name
    ENV_NAME=$(az containerapp show -g "$RESOURCE_GROUP" -n "$BLOB_SIDECAR_APP" --query 'properties.managedEnvironmentId' -o tsv | xargs basename)
    
    # Get the default domain
    DEFAULT_DOMAIN=$(az containerapp env show -g "$RESOURCE_GROUP" -n "$ENV_NAME" --query 'properties.defaultDomain' -o tsv 2>/dev/null || true)
    
    if [ -z "$DEFAULT_DOMAIN" ]; then
        warning "Could not retrieve default domain from Container Apps Environment"
    else
        success "Container Apps Environment domain: $DEFAULT_DOMAIN"
        
        # Show the expected internal DNS address for blob-sidecar:8081
        BLOB_INTERNAL_DNS="$BLOB_SIDECAR_APP.internal.$DEFAULT_DOMAIN:8081"
        success "Expected blob-sidecar ingest endpoint: http://$BLOB_INTERNAL_DNS"
    fi
}

# ============================================================================
# Check 8: Azure Files volume (Phase 3 rollback safety)
# ============================================================================
check_azure_files_mount() {
    info "Checking Azure Files mount (Phase 3 rollback safety)"
    
    VOLUMES=$(az containerapp show -g "$RESOURCE_GROUP" -n "$HLS_TRANSCODER_APP" --query 'template.volumes' -o json 2>/dev/null || echo "[]")
    HLS_OUTPUT_VOL=$(echo "$VOLUMES" | jq '.[] | select(.name=="hls-output")' 2>/dev/null || true)
    
    if [ -n "$HLS_OUTPUT_VOL" ]; then
        success "Azure Files mount preserved for fallback (hls-output volume exists)"
        info "  → To rollback: set OUTPUT_MODE=file on hls-transcoder"
    else
        warning "Azure Files mount not found on hls-transcoder (Phase 3 only mode)"
    fi
}

# ============================================================================
# Check 9: Health probe configuration
# ============================================================================
check_health_probe() {
    info "Checking health probe configuration"
    
    PROBES=$(az containerapp show -g "$RESOURCE_GROUP" -n "$BLOB_SIDECAR_APP" --query 'template.containers[0].probes' -o json 2>/dev/null || echo "[]")
    PROBE_COUNT=$(echo "$PROBES" | jq 'length' 2>/dev/null || echo "0")
    
    if [ "$PROBE_COUNT" -gt 0 ]; then
        success "Health probes configured on blob-sidecar ($PROBE_COUNT probes)"
    else
        warning "No health probes detected on blob-sidecar (optional, but recommended)"
    fi
}

# ============================================================================
# Main execution
# ============================================================================
main() {
    echo ""
    echo "╭────────────────────────────────────────────────────────╮"
    echo "│  Phase 3 HTTP Ingest Deployment Verification          │"
    echo "╰────────────────────────────────────────────────────────╯"
    echo ""
    
    # Check if Azure CLI is available
    if ! command -v az &>/dev/null; then
        error "Azure CLI not found. Install from: https://learn.microsoft.com/en-us/cli/azure/install-azure-cli"
        exit 1
    fi
    
    # Check if jq is available
    if ! command -v jq &>/dev/null; then
        warning "jq not found. Some checks will be skipped."
        warning "Install with: brew install jq (macOS) or apt-get install jq (Linux)"
    fi
    
    # Run checks
    check_resource_group
    check_blob_sidecar_exists && check_blob_sidecar_env || error "Failed to get blob-sidecar environment variables"
    check_hls_transcoder_exists && check_hls_transcoder_env || error "Failed to get hls-transcoder environment variables"
    check_container_status
    check_network_connectivity
    check_azure_files_mount
    check_health_probe
    
    echo ""
    echo "╭────────────────────────────────────────────────────────╮"
    
    if [ "$FAILED_CHECKS" -eq 0 ]; then
        echo "│  ${GREEN}All checks passed! ✓${NC}                              │"
        echo "│  HTTP ingest deployment is ready.                  │"
        echo "╰────────────────────────────────────────────────────────╯"
        echo ""
        success "Deployment verification complete"
        return 0
    else
        echo "│  ${RED}$FAILED_CHECKS check(s) failed${NC}                             │"
        echo "│  Please review errors above.                       │"
        echo "╰────────────────────────────────────────────────────────╯"
        echo ""
        error "Deployment verification failed"
        return 1
    fi
}

# Run main function and exit with appropriate code
main
exit $?

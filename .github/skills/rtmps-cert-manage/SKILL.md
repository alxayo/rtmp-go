---
name: rtmps-cert-manage
description: 'Manage RTMPS TLS certificates for RTMP ingest server on Azure. Use when: issue Let''s Encrypt certificate, renew TLS cert, rotate RTMPS certificate, upload cert to Key Vault, troubleshoot RTMPS not working, verify TLS handshake, debug certificate errors, create Key Vault for RTMPS, check cert expiry, RTMPS connectivity test. Uses Azure CLI and acme.sh only — no Bicep templates or GitHub Actions.'
argument-hint: 'Action + optional domain override, e.g. "renew stream.event-periscope.com" or just "status"'
---

# RTMPS Certificate Management

Manage the full TLS certificate lifecycle for the RTMP ingest server on Azure Container Apps using only Azure CLI and acme.sh. Independent of Bicep templates and GitHub Actions workflows.

## When to Use

- Check RTMPS status and certificate expiry
- Issue a new Let's Encrypt certificate for the RTMP ingest domain
- Renew an expiring certificate
- Upload certificate to Azure Key Vault
- Troubleshoot RTMPS connectivity or TLS handshake failures
- Full certificate rotation (issue + upload + restart)
- Create Key Vault infrastructure when it doesn't exist yet

## Prerequisites

- Azure CLI logged in (`az account show`)
- `acme.sh` installed (`~/.acme.sh/acme.sh`)
- Azure DNS zone accessible (DNS-01 challenge validation)
- "Key Vault Secrets Officer" role on the Key Vault (or ability to self-assign)

## Environment Discovery

All commands start by discovering the live environment. Do NOT hardcode resource names.

The user may provide a domain (e.g. `stream.example.com`) as part of the slash-command argument. If not provided, use these defaults:

```bash
# Defaults — override via argument or env vars
RESOURCE_GROUP="${RESOURCE_GROUP:-event-periscope-ne}"
DNS_ZONE_NAME="${DNS_ZONE_NAME:-event-periscope.com}"
DNS_RESOURCE_GROUP="${DNS_RESOURCE_GROUP:-rg-dns}"
RTMP_SUBDOMAIN="${RTMP_SUBDOMAIN:-stream}"
DOMAIN="${RTMP_SUBDOMAIN}.${DNS_ZONE_NAME}"

# Auto-discover resource names
RTMP_APP=$(az containerapp list -g "$RESOURCE_GROUP" \
  --query "[?tags.role=='rtmp-server'].name | [0]" -o tsv)
KV_NAME=$(az keyvault list -g "$RESOURCE_GROUP" --query "[0].name" -o tsv)
RTMP_FQDN=$(az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "properties.configuration.ingress.fqdn" -o tsv)

# Azure identity (for acme.sh DNS plugin + RBAC)
SUBSCRIPTION_ID=$(az account show --query id -o tsv)
TENANT_ID=$(az account show --query tenantId -o tsv)
MY_OID=$(az ad signed-in-user show --query id -o tsv 2>/dev/null || \
  az account show --query "user.name" -o tsv)
```

## Procedures

### 1. Status Check

Check current RTMPS state: is it enabled, is the cert valid, what's the expiry.

```bash
# Check if RTMPS port 1936 is exposed in ingress
az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "properties.configuration.ingress.{
    targetPort: targetPort,
    transport: transport,
    additionalPorts: additionalPortMappings
  }" -o json

# Check if TLS flags are in the container command
az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "properties.template.containers[0].command" -o json | grep -E 'tls-listen|tls-cert|tls-key'

# Check Key Vault exists and has cert secrets
az keyvault secret show --vault-name "$KV_NAME" --name "tls-cert" \
  --query "{name: name, contentType: contentType, created: attributes.created, updated: attributes.updated}" -o json 2>/dev/null

# Test TLS handshake from outside (most definitive test)
echo | openssl s_client -connect "${DOMAIN}:1936" -servername "$DOMAIN" 2>/dev/null \
  | grep -E 'subject=|issuer=|Verify return'

# Test port connectivity
nc -z -w5 "$DOMAIN" 1935 && echo "RTMP  :1935 OPEN" || echo "RTMP  :1935 CLOSED"
nc -z -w5 "$DOMAIN" 1936 && echo "RTMPS :1936 OPEN" || echo "RTMPS :1936 CLOSED"

# Check container logs for TLS status
az containerapp logs show -g "$RESOURCE_GROUP" -n "$RTMP_APP" --tail 50 2>/dev/null \
  | grep -i -E 'tls|rtmps|cert'
```

**Healthy output** should show:
- `INFO TLS certificate loaded subject=stream.event-periscope.com`
- `INFO RTMPS server listening listen_addr=[::]:1936`
- `INFO RTMPS enabled tls_addr=[::]:1936`
- `Verify return code: 0 (ok)` from openssl

**Unhealthy output** indicates:
- `ERROR RTMPS listener failed to start (plain RTMP still active)` — cert invalid/missing, plain RTMP still works
- `Verify return code: 20 (unable to get local issuer certificate)` — cert chain incomplete (upload fullchain.cer not just the leaf)
- Port 1936 CLOSED — RTMPS not enabled in ingress config (needs `enableRtmps=true` in Bicep deploy)

### 2. Check Local Certificate

Check the locally cached acme.sh certificate before issuing/renewing.

```bash
# Check ECC cert (preferred), fall back to RSA
CERT_DIR="$HOME/.acme.sh/${DOMAIN}_ecc"
[ -d "$CERT_DIR" ] || CERT_DIR="$HOME/.acme.sh/${DOMAIN}"

if [ -d "$CERT_DIR" ]; then
  echo "Local cert found: $CERT_DIR"
  openssl x509 -in "$CERT_DIR/fullchain.cer" -noout -subject -dates -issuer
else
  echo "No local certificate found for $DOMAIN"
fi
```

### 3. Issue or Renew Certificate

Uses acme.sh with Azure DNS plugin (DNS-01 challenge). No HTTP server needed — works purely through DNS TXT records.

```bash
# Set Azure DNS credentials for acme.sh
export AZUREDNS_SUBSCRIPTIONID="$SUBSCRIPTION_ID"
export AZUREDNS_TENANTID="$TENANT_ID"
export AZUREDNS_MANAGEDIDENTITY=true

# Issue new cert (skips if not due for renewal)
~/.acme.sh/acme.sh --issue -d "$DOMAIN" --dns dns_azure --dnssleep 30

# Force renewal (even if not expiring)
~/.acme.sh/acme.sh --issue -d "$DOMAIN" --dns dns_azure --dnssleep 30 --force

# Use staging server for testing (no rate limits)
~/.acme.sh/acme.sh --issue -d "$DOMAIN" --dns dns_azure --dnssleep 30 --staging
```

**Exit codes**: 0 = issued/renewed, 2 = not due for renewal (not an error).

**Cert files produced** (in `~/.acme.sh/${DOMAIN}_ecc/`):
- `fullchain.cer` — leaf + intermediate (upload this as tls-cert)
- `${DOMAIN}.key` — private key (upload this as tls-key)
- `ca.cer` — CA certificate only

### 4. Upload Certificate to Key Vault

```bash
CERT_DIR="$HOME/.acme.sh/${DOMAIN}_ecc"
[ -d "$CERT_DIR" ] || CERT_DIR="$HOME/.acme.sh/${DOMAIN}"

# Upload fullchain (leaf + intermediate for proper chain verification)
az keyvault secret set \
  --vault-name "$KV_NAME" \
  --name "tls-cert" \
  --file "$CERT_DIR/fullchain.cer" \
  --content-type "application/x-pem-file" \
  -o none

# Upload private key
az keyvault secret set \
  --vault-name "$KV_NAME" \
  --name "tls-key" \
  --file "$CERT_DIR/${DOMAIN}.key" \
  --content-type "application/x-pem-file" \
  -o none
```

**If "Forbidden" RBAC error**: The current user needs "Key Vault Secrets Officer" role:

```bash
KV_ID=$(az keyvault show --name "$KV_NAME" --query "id" -o tsv)
MY_OID=$(az ad signed-in-user show --query id -o tsv)
az role assignment create \
  --role "Key Vault Secrets Officer" \
  --assignee-object-id "$MY_OID" \
  --assignee-principal-type "User" \
  --scope "$KV_ID" \
  -o none
# Wait 15-30s for RBAC propagation, then retry upload
```

### 5. Activate Certificate on Container

Container Apps cache Key Vault secrets per revision. A simple `revision restart` does NOT refresh secrets — you must create a new revision.

```bash
# Create new revision to pull latest Key Vault secrets
SUFFIX="tls-$(date +%s)"
az containerapp update \
  -g "$RESOURCE_GROUP" \
  -n "$RTMP_APP" \
  --revision-suffix "$SUFFIX" \
  -o none

# Wait for new revision to be running
sleep 25
az containerapp revision list -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "[?properties.active].{name: name, state: properties.runningState}" -o table
```

**Critical**: `az containerapp revision restart` reuses the same cached secrets. Only `az containerapp update --revision-suffix` forces a new secret pull from Key Vault.

### 6. Full Rotation (Issue + Upload + Activate)

Combines steps 3-5 into a single operation.

```bash
# 1. Set DNS credentials
export AZUREDNS_SUBSCRIPTIONID="$SUBSCRIPTION_ID"
export AZUREDNS_TENANTID="$TENANT_ID"
export AZUREDNS_MANAGEDIDENTITY=true

# 2. Issue/renew cert
~/.acme.sh/acme.sh --issue -d "$DOMAIN" --dns dns_azure --dnssleep 30 --force

# 3. Upload to Key Vault
CERT_DIR="$HOME/.acme.sh/${DOMAIN}_ecc"
[ -d "$CERT_DIR" ] || CERT_DIR="$HOME/.acme.sh/${DOMAIN}"

az keyvault secret set --vault-name "$KV_NAME" --name "tls-cert" \
  --file "$CERT_DIR/fullchain.cer" --content-type "application/x-pem-file" -o none
az keyvault secret set --vault-name "$KV_NAME" --name "tls-key" \
  --file "$CERT_DIR/${DOMAIN}.key" --content-type "application/x-pem-file" -o none

# 4. Create new revision
az containerapp update -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --revision-suffix "tls-$(date +%s)" -o none

# 5. Verify
sleep 25
echo | openssl s_client -connect "${DOMAIN}:1936" -servername "$DOMAIN" 2>/dev/null \
  | grep -E 'subject=|issuer=|Verify return'
```

### 7. Create Key Vault (First-Time Setup)

If Key Vault doesn't exist and you need to create RTMPS infrastructure without Bicep.

```bash
# Generate name matching the Bicep convention
RESOURCE_TOKEN=$(az containerapp env list -g "$RESOURCE_GROUP" \
  --query "[0].name" -o tsv | sed 's/azenv//')
KV_NAME="azkv${RESOURCE_TOKEN}"

# Create Key Vault with RBAC auth
az keyvault create \
  --name "$KV_NAME" \
  --resource-group "$RESOURCE_GROUP" \
  --location "$(az group show -g "$RESOURCE_GROUP" --query location -o tsv)" \
  --enable-rbac-authorization true \
  --sku standard \
  -o none

# Grant the container app's managed identity "Key Vault Secrets User" (read-only)
IDENTITY_PRINCIPAL=$(az containerapp show -g "$RESOURCE_GROUP" -n "$RTMP_APP" \
  --query "identity.userAssignedIdentities.*.principalId | [0]" -o tsv)
KV_ID=$(az keyvault show --name "$KV_NAME" --query "id" -o tsv)

az role assignment create \
  --role "Key Vault Secrets User" \
  --assignee-object-id "$IDENTITY_PRINCIPAL" \
  --assignee-principal-type "ServicePrincipal" \
  --scope "$KV_ID" \
  -o none

# Grant yourself "Key Vault Secrets Officer" (read+write)
MY_OID=$(az ad signed-in-user show --query id -o tsv)
az role assignment create \
  --role "Key Vault Secrets Officer" \
  --assignee-object-id "$MY_OID" \
  --assignee-principal-type "User" \
  --scope "$KV_ID" \
  -o none

# Wait for RBAC propagation
sleep 20
```

After creating Key Vault, proceed with steps 3-5 (issue cert, upload, activate).

## Troubleshooting Decision Tree

```
RTMPS not working?
├─ Port 1936 CLOSED from outside?
│  ├─ Check ingress: additionalPortMappings should include port 1936
│  └─ Fix: redeploy with enableRtmps=true or add port via az containerapp ingress
├─ Port 1936 OPEN but TLS handshake fails?
│  ├─ Check container logs for "RTMPS listener failed to start"
│  │  ├─ "failed to find any PEM data" → placeholder cert in KV, run Upload + Activate
│  │  ├─ "tls: private key does not match public key" → cert/key mismatch, re-issue
│  │  └─ No TLS error in logs → TLS flags missing from container command
│  └─ Check openssl output
│     ├─ "Verify return code: 20" → uploaded leaf cert only, need fullchain.cer
│     ├─ "Verify return code: 10" → cert expired, renew
│     └─ Connection timeout → network/firewall issue, not cert issue
├─ "RTMPS listener failed to start" in logs but port 1936 open?
│  └─ Container Apps still routes to 1936 but nobody listens → connection reset
└─ Was working, stopped after redeploy?
   └─ Bicep overwrites KV secrets with placeholders — re-upload cert + new revision
```

## Key Gotchas

1. **`revision restart` does NOT refresh Key Vault secrets** — must use `az containerapp update --revision-suffix` to create a new revision
2. **Bicep redeploy overwrites certs** — the Bicep template seeds placeholder values in Key Vault secrets on every deploy. After any Bicep deployment, re-run the upload + activate steps
3. **Upload fullchain.cer, not the leaf cert** — the leaf cert alone causes "unable to get local issuer certificate" errors on clients
4. **RBAC propagation takes 15-30 seconds** — after assigning Key Vault roles, wait before retrying secret operations
5. **Let's Encrypt rate limits** — 5 duplicate certs per week per domain. Use `--staging` for testing. Exit code 2 means "not due for renewal" (not an error)
6. **TLS listener failure is non-fatal** — the RTMP server logs an error and continues serving plain RTMP on port 1935. RTMPS becomes available after restart with valid certs
7. **acme.sh cert paths** — ECC certs go to `~/.acme.sh/${DOMAIN}_ecc/`, RSA to `~/.acme.sh/${DOMAIN}/`. Always check ECC first (preferred, smaller)

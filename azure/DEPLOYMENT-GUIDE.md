# Azure Deployment & Troubleshooting Guide

> **Complete operational reference for deploying rtmp-go and StreamGate to Azure Container Apps.**
>
> This guide covers every Azure service, Bicep template section, environment variable, deployment command, validation step, and known issue. Use it as the authoritative source for all future deployments.

---

## Table of Contents

0. [Step-by-Step Deployment Runbook](#0-step-by-step-deployment-runbook)
1. [Architecture Overview](#1-architecture-overview)
2. [Azure Services Inventory](#2-azure-services-inventory)
3. [Resource Naming Convention](#3-resource-naming-convention)
4. [Prerequisites](#4-prerequisites)
5. [Phase 1: Deploy rtmp-go](#5-phase-1-deploy-rtmp-go)
6. [Phase 2: Deploy StreamGate](#6-phase-2-deploy-streamgate)
7. [Phase 3: DNS Setup](#7-phase-3-dns-setup)
8. [Bicep Template Reference](#8-bicep-template-reference)
9. [Environment Variables Reference](#9-environment-variables-reference)
10. [Container Apps Detailed Configuration](#10-container-apps-detailed-configuration)
11. [Storage Architecture](#11-storage-architecture)
12. [Secrets & Authentication Flow](#12-secrets--authentication-flow)
13. [Post-Deployment Validation](#13-post-deployment-validation)
14. [End-to-End Streaming Test](#14-end-to-end-streaming-test)
15. [Troubleshooting Guide](#15-troubleshooting-guide)
16. [Teardown Procedures](#16-teardown-procedures)
17. [Cost Analysis](#17-cost-analysis)
18. [Performance Verification](#18-performance-verification)
19. [Quick Reference Card](#19-quick-reference-card)

---

## 0. Step-by-Step Deployment Runbook

> **Copy-paste deployment guide.** Follow these steps in order to go from zero to a fully deployed system with custom DNS names on `port-80.com`. Total time: ~25-35 minutes.

### 0.1 Prerequisites Checklist

Verify all required tools are installed:

```bash
az --version          # Azure CLI 2.50+
python3 --version     # Python 3 (used by deploy scripts to parse JSON)
openssl version       # OpenSSL (for secret generation)
```

Log in to Azure and select the correct subscription:

```bash
az login
az account set --subscription "Visual Studio Enterprise"
az account show --query '{Name:name, Id:id}' --output table
```

### 0.2 Generate Secrets

Generate all secrets up front. **Save these values** — you'll need them across multiple steps and for any future redeployments.

```bash
# 1. RTMP Auth Token — shared secret for broadcaster publish authentication
export RTMP_AUTH_TOKEN="$(openssl rand -base64 24 | tr -d '/+=' | head -c 32)"
echo "RTMP_AUTH_TOKEN=$RTMP_AUTH_TOKEN"

# 2. Playback Signing Secret — HMAC-SHA256 key for JWT tokens
export PLAYBACK_SIGNING_SECRET="$(openssl rand -hex 32)"
echo "PLAYBACK_SIGNING_SECRET=$PLAYBACK_SIGNING_SECRET"

# 3. Internal API Key — shared key for platform↔HLS revocation sync
export INTERNAL_API_KEY="$(openssl rand -base64 24)"
echo "INTERNAL_API_KEY=$INTERNAL_API_KEY"

# 4. Admin Password Hash — bcrypt hash for the StreamGate admin console
#    Run interactively (enter a password ≥ 8 characters when prompted):
cd streamgate
npm run hash-password
cd ..
#    Copy the output hash and export it:
export ADMIN_PASSWORD_HASH='$2b$12$...'   # ← paste the full hash here
```

### 0.3 Step 1 — Deploy rtmp-go Infrastructure (~10-15 min)

This creates the Azure resource group, VNet, Container Apps Environment, ACR, Storage Account, Managed Identity, and all rtmp-go container apps (rtmp-server, blob-sidecar, hls-transcoder with co-located blob-sidecar).

```bash
cd rtmp-go/azure
RTMP_AUTH_TOKEN="$RTMP_AUTH_TOKEN" ./deploy.sh
```

**What happens** (5 automated steps):
1. Creates resource group `rg-rtmpgo` in `eastus2`
2. Deploys Bicep template with placeholder container images (creates all infrastructure)
3. Builds 3 Docker images remotely via ACR Tasks (`rtmp-server`, `blob-sidecar`, `hls-transcoder`)
4. Redeploys Bicep with the real ACR images
5. Verifies all 4 container apps are running

**Save from output**: The `RTMP_APP_FQDN` value (e.g., `azappXXX.azurecontainerapps.io`) — needed for DNS setup.

### 0.4 Step 2 — Deploy StreamGate (~10-15 min)

This deploys into the **same resource group**, sharing the ACR, Storage Account, Container Apps Environment, and Managed Identity created by rtmp-go. Creates the StreamGate platform (Next.js) and HLS media server (Express.js) container apps.

```bash
cd ../../streamgate/azure
ADMIN_PASSWORD_HASH="$ADMIN_PASSWORD_HASH" \
PLAYBACK_SIGNING_SECRET="$PLAYBACK_SIGNING_SECRET" \
INTERNAL_API_KEY="$INTERNAL_API_KEY" \
./deploy.sh
```

**What happens** (7 automated steps):
1. Verifies rtmp-go deployment exists in `rg-rtmpgo`
2. Discovers shared infrastructure (ACR, Storage, Identity, ACA Env) from rtmp-go outputs
3. Validates secrets are set (auto-generates `PLAYBACK_SIGNING_SECRET` and `INTERNAL_API_KEY` if not provided)
4. Deploys Bicep — first pass with placeholder images and URLs
5. Builds 2 Docker images via ACR Tasks (`streamgate-platform`, `streamgate-hls`)
6. Redeploys Bicep — second pass with real images, resolved FQDNs, and SAS token
7. Verifies both container apps are running

**Save from output**: `PLATFORM_FQDN` and `HLS_FQDN` values, plus the `PLAYBACK_SIGNING_SECRET` and `INTERNAL_API_KEY` if they were auto-generated.

### 0.5 Step 3 — Create DNS Zone & RTMP CNAME (~2 min)

The DNS zone lives in a **separate resource group** (`rg-dns`) so it survives teardowns. If the zone already exists (from a previous deployment), this step just adds/updates the CNAME record.

```bash
cd ../../rtmp-go/azure

# Create/update the DNS zone + add stream.port-80.com CNAME
# Replace the FQDN with the value from Step 1 output
RTMP_APP_FQDN="<rtmp-app-fqdn-from-step1>" ./dns-deploy.sh
```

**First-time only**: Update your domain registrar (e.g., GoDaddy) with the Azure nameservers printed by the script:

1. Go to `https://dcc.godaddy.com/domains/port-80.com/dns`
2. **Nameservers** → **Change** → **Enter my own nameservers (advanced)**
3. Replace all nameservers with Azure's (e.g., `ns1-02.azure-dns.com.`, etc.)
4. Save and confirm

Verify propagation:
```bash
nslookup -type=NS port-80.com
```

### 0.6 Step 4 — Add StreamGate DNS CNAMEs (~1 min)

```bash
cd ../../streamgate/azure

# Replace FQDNs with values from Step 2 output
PLATFORM_APP_FQDN="<platform-fqdn-from-step2>" \
HLS_SERVER_FQDN="<hls-fqdn-from-step2>" \
./dns-deploy.sh
```

This creates:
- `watch.port-80.com` → StreamGate Platform App
- `hls.port-80.com` → StreamGate HLS Server

### 0.7 Step 5 — Redeploy StreamGate with Custom Domains & Managed SSL (~10 min)

After DNS records are created, redeploy StreamGate so it uses the custom domain URLs for CORS, HLS base URL, and platform↔HLS communication. The deploy script auto-detects custom domains from the DNS zone **and automatically binds them with Azure managed SSL certificates**.

```bash
cd streamgate/azure   # (if not already there)

ADMIN_PASSWORD_HASH="$ADMIN_PASSWORD_HASH" \
PLAYBACK_SIGNING_SECRET="$PLAYBACK_SIGNING_SECRET" \
INTERNAL_API_KEY="$INTERNAL_API_KEY" \
./deploy.sh
```

The script will detect the DNS CNAMEs and automatically:
1. Configure env vars: `HLS_SERVER_BASE_URL`, `CORS_ALLOWED_ORIGIN`, `PLATFORM_APP_URL`
2. Bind `watch.port-80.com` to `sg-platform` with a managed SSL certificate
3. Bind `hls.port-80.com` to `sg-hls` with a managed SSL certificate

Managed certificate provisioning takes **up to 20 minutes** per domain (Azure ACME domain validation). The binding uses CNAME validation — no additional TXT records are needed beyond the CNAMEs created in Step 4.

> **Note**: `stream.port-80.com` does NOT get a custom domain binding because the RTMP server uses TCP transport (port 1935), not HTTPS. The CNAME record alone is sufficient for RTMP.

Alternatively, set custom domain URLs explicitly to override auto-detection:
```bash
HLS_SERVER_BASE_URL="https://hls.port-80.com" \
CORS_ALLOWED_ORIGIN="https://watch.port-80.com" \
ADMIN_PASSWORD_HASH="$ADMIN_PASSWORD_HASH" \
PLAYBACK_SIGNING_SECRET="$PLAYBACK_SIGNING_SECRET" \
INTERNAL_API_KEY="$INTERNAL_API_KEY" \
./deploy.sh
```

### 0.8 Step 6 — Validate the Deployment

```bash
# Verify all 6 container apps are running
az containerapp list --resource-group rg-rtmpgo \
  --query '[].{Name:name, Status:properties.runningStatus}' --output table

# Verify DNS resolution
nslookup stream.port-80.com
nslookup watch.port-80.com
nslookup hls.port-80.com

# Verify managed SSL certificates (both should show "Succeeded")
az containerapp env certificate list -g rg-rtmpgo -n azenvdu7fhxanu5cak \
  --query "[].{name:name, subject:properties.subjectName, state:properties.provisioningState}" -o table

# Verify custom domain bindings (should show "SniEnabled")
az containerapp hostname list -g rg-rtmpgo -n sg-platform-olog3klyrk7fw -o table
az containerapp hostname list -g rg-rtmpgo -n sg-hls-olog3klyrk7fw -o table

# Verify HTTPS with TLS (should show TLSv1.3 and valid cert)
curl -sv https://watch.port-80.com 2>&1 | grep -E "SSL|subject|HTTP/"
curl -sv https://hls.port-80.com/health 2>&1 | grep -E "SSL|subject|HTTP/"

# Health check HLS server
curl -s https://hls.port-80.com/health

# Health check Platform
curl -s -o /dev/null -w "HTTP %{http_code}\n" https://watch.port-80.com
```

### 0.9 Step 7 — End-to-End Streaming Test

```bash
# Publish a test RTMP stream (recommended: transcode-friendly flags)
ffmpeg -re -i test.mp4 \
  -c:v libx264 -profile:v baseline -bf 0 -g 60 -keyint_min 60 \
  -b:v 4500k -maxrate 5000k -bufsize 9000k -preset veryfast \
  -c:a aac -b:a 128k -ar 48000 \
  -f flv "rtmp://stream.port-80.com/live/test_stream?token=$RTMP_AUTH_TOKEN"
```

Then in the StreamGate admin console:
1. Open `https://watch.port-80.com/admin` and log in
2. Create an event with stream key `test_stream`
3. Generate viewer tokens
4. Open `https://watch.port-80.com` in a new browser tab
5. Enter a token code → video should play

### 0.10 Quick Reference: All Scripts

| Script | Location | Purpose |
|--------|----------|---------|
| `deploy.sh` | `rtmp-go/azure/` | Deploy rtmp-go infrastructure + apps |
| `destroy.sh` | `rtmp-go/azure/` | Delete entire `rg-rtmpgo` resource group |
| `dns-deploy.sh` | `rtmp-go/azure/` | Create DNS zone + `stream` CNAME |
| `dns-destroy.sh` | `rtmp-go/azure/` | Delete DNS zone |
| `deploy.sh` | `streamgate/azure/` | Deploy StreamGate into existing rtmp-go env |
| `destroy.sh` | `streamgate/azure/` | Remove only StreamGate components (keeps rtmp-go) |
| `dns-deploy.sh` | `streamgate/azure/` | Add `watch` + `hls` CNAMEs to existing zone |
| `dns-destroy.sh` | `streamgate/azure/` | Remove StreamGate DNS records |

### 0.11 Teardown

```bash
# Remove StreamGate only (keeps rtmp-go running):
cd streamgate/azure && ./destroy.sh

# Remove everything (deletes entire rg-rtmpgo resource group):
cd rtmp-go/azure && ./destroy.sh

# Remove DNS zone (requires re-configuring registrar nameservers if recreated):
cd rtmp-go/azure && ./dns-destroy.sh
```

---

## 1. Architecture Overview

The system consists of **two deployments** sharing a single Azure resource group:

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  Resource Group: rg-rtmpgo                                                  │
│                                                                             │
│  ┌─────────────────────── Container Apps Environment ──────────────────┐    │
│  │                         (VNet: 10.0.0.0/16)                         │    │
│  │                                                                     │    │
│  │  ┌─────────────────┐   webhooks   ┌──────────────────┐             │    │
│  │  │  rtmp-server     │────────────→│  blob-sidecar     │             │    │
│  │  │  (TCP 1935 ext)  │             │  (HTTP 8080 int)  │             │    │
│  │  │  0.5 vCPU / 1Gi  │             │  0.25 vCPU /0.5Gi │             │    │
│  │  └────────┬─────────┘             └────────┬──────────┘             │    │
│  │           │ webhooks                       │ uploads                 │    │
│  │           ▼                                ▼                        │    │
│  │  ┌─────────────────────────────────────────────────┐               │    │
│  │  │  hls-transcoder (multi-container app)            │               │    │
│  │  │  (HTTP 8090 int) — 4 vCPU / 8 GiB total        │               │    │
│  │  │                                                  │               │    │
│  │  │  ┌─────────────────┐  localhost  ┌────────────┐ │               │    │
│  │  │  │  hls-transcoder  │───:8081───→│ blob-sidecar│ │               │    │
│  │  │  │  (FFmpeg ABR)    │  HTTP PUT  │ (ingest +   │ │               │    │
│  │  │  │  3.5 vCPU / 7Gi │            │  blob upload)│ │               │    │
│  │  │  └─────────────────┘            │ 0.5 vCPU/1Gi│ │               │    │
│  │  │                                  └──────┬─────┘ │               │    │
│  │  └─────────────────────────────────────────┼───────┘               │    │
│  │                                            │ uploads                │    │
│  │                                            ▼                        │    │
│  │  ┌─────────────────┐             ┌──────────────────┐             │    │
│  │  │  sg-platform     │  JWT auth   │  sg-hls          │             │    │
│  │  │  (HTTP 3000 ext) │←──────────→│  (HTTP 4000 ext) │             │    │
│  │  │  0.5 vCPU / 1Gi  │  revocation │  0.5 vCPU / 1Gi  │             │    │
│  │  └────────┬─────────┘   polling   └────────┬─────────┘             │    │
│  │           │                                │ reads                  │    │
│  │           ▼                                ▼                        │    │
│  │  Azure Files: streamgate-data    Azure Blob Storage: hls-content   │    │
│  │  (1 GiB, SQLite DB)             (upstream proxy via SAS token)     │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                             │
│  ┌─────────────────┐  ┌─────────────┐  ┌───────────────────────────────┐   │
│  │  ACR (Basic)     │  │  Managed ID  │  │  Storage Account              │   │
│  │  (Docker images) │  │  (RBAC)      │  │  Files + Blob                 │   │
│  └─────────────────┘  └─────────────┘  └───────────────────────────────┘   │
│                                                                             │
│  Log Analytics Workspace (all container logs)                               │
└─────────────────────────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────┐
│  Resource Group: rg-dns                   │
│  DNS Zone: port-80.com                    │
│    stream.port-80.com → rtmp-server FQDN  │
│    watch.port-80.com  → sg-platform FQDN  │
│    hls.port-80.com    → sg-hls FQDN       │
└───────────────────────────────────────────┘
```

> **Note:** The standalone `hls-blob-sidecar` Container App still exists (scaled to 0)
> for rollback to Phase 3 if needed. In Phase 4, the blob-sidecar runs co-located
> inside the `hls-transcoder` Container App, communicating via `localhost:8081`
> to bypass the Envoy HTTP/2 CONNECT tunnel bug (envoyproxy/envoy#28329).

### Data Flow

1. **Ingest**: Broadcaster publishes RTMP to `rtmp-server` (TCP 1935)
2. **Recording**: `rtmp-server` writes FLV segments to Azure Files `recordings/` share and fires webhooks
3. **Blob Upload (recordings)**: `blob-sidecar` receives webhook, uploads FLV to Blob Storage, optionally deletes local file
4. **HLS Transcoding**: `hls-transcoder` receives `publish_start` webhook, spawns FFmpeg to subscribe to RTMP stream, transcodes to 3 ABR renditions (1080p/720p/480p)
5. **Blob Upload (HLS)**: FFmpeg PUTs segments via HTTP to the co-located blob-sidecar at `localhost:8081`, which buffers and uploads to Azure Blob Storage (`hls-content` container)
6. **Viewer Access**: Viewer enters ticket on `sg-platform`, receives JWT, player requests HLS from `sg-hls` with JWT auth
7. **HLS Delivery**: `sg-hls` validates JWT, proxies from Azure Blob Storage (upstream proxy with SAS token)

---

## 2. Azure Services Inventory

| Service | Purpose | SKU/Tier | Deployed By |
|---------|---------|----------|-------------|
| **Resource Group** (`rg-rtmpgo`) | Contains all resources | — | rtmp-go deploy.sh |
| **Log Analytics Workspace** | Container log aggregation | Per-GB (pay-as-you-go) | rtmp-go main.bicep |
| **Virtual Network** | Private networking for ACA | 10.0.0.0/16 | rtmp-go main.bicep |
| **Container Apps Environment** | Shared ACA runtime | Consumption (serverless) | rtmp-go main.bicep |
| **Azure Container Registry** | Docker image storage | Basic ($5/mo) | rtmp-go main.bicep |
| **Storage Account** | Files + Blob storage | Standard LRS | rtmp-go main.bicep |
| **User-Assigned Managed Identity** | ACR pull + Blob RBAC | — | rtmp-go main.bicep |
| **Container App: rtmp-server** | RTMP ingest | 0.5 vCPU / 1Gi | rtmp-go main.bicep |
| **Container App: blob-sidecar** | Recording upload to Blob | 0.25 vCPU / 0.5Gi | rtmp-go main.bicep |
| **Container App: hls-transcoder** | FFmpeg ABR transcoding + co-located blob-sidecar (multi-container) | 4 vCPU / 8Gi total | rtmp-go main.bicep |
| **Container App: hls-blob-sidecar** | _(scaled to 0 — co-located in transcoder since Phase 4)_ | 0.25 vCPU / 0.5Gi | rtmp-go main.bicep |
| **Container App: sg-platform** | Next.js viewer/admin portal | 0.5 vCPU / 1Gi | streamgate main.bicep |
| **Container App: sg-hls** | Express.js HLS media server | 0.5 vCPU / 1Gi | streamgate main.bicep |
| **Resource Group** (`rg-dns`) | DNS zone (survives teardowns) | — | rtmp-go dns-deploy.sh |
| **Azure DNS Zone** (`port-80.com`) | Custom domain resolution | Hosted zone | rtmp-go dns.bicep |

### RBAC Assignments (on Managed Identity)

| Role | Scope | Purpose |
|------|-------|---------|
| `AcrPull` | Container Registry | Pull Docker images from ACR |
| `Storage Blob Data Contributor` | Storage Account | Read/write blob containers |

---

## 3. Resource Naming Convention

All resources use a deterministic naming scheme based on `uniqueString()`:

```
az{prefix}${uniqueString(subscription().id, resourceGroup().id, location, environmentName)}
```

For `environmentName = "rtmpgo"`, `location = "eastus2"`, in subscription `4e0d0fc6-...`, resource group `rg-rtmpgo`:

| Resource | Prefix | Example Name |
|----------|--------|-------------|
| Container Apps Environment | `azenv` | `azenvdu7fhxanu5cak` |
| Container Registry | `azacr` | `azacrdu7fhxanu5cak` |
| Storage Account | `azst` | `azstdu7fhxanu5cak` |
| Managed Identity | `azid` | `aziddu7fhxanu5cak` |
| Virtual Network | `azvnet` | `azvnetdu7fhxanu5cak` |
| Log Analytics | `azlog` | `azlogdu7fhxanu5cak` |
| rtmp-server app | `azapp` + `1` | `azappdu7fhxanu5cak1` |
| blob-sidecar app | `azapp` + `2` | `azappdu7fhxanu5cak2` |
| hls-transcoder app | `azapp` + `3` | `azappdu7fhxanu5cak3` |
| hls-blob-sidecar app | `azapp` + `4` | `azappdu7fhxanu5cak4` |

StreamGate uses a different prefix pattern:

| Resource | Pattern | Example Name |
|----------|---------|-------------|
| sg-platform | `sg-platform-${resourceToken}` | `sg-platform-olog3klyrk7fw` |
| sg-hls | `sg-hls-${resourceToken}` | `sg-hls-olog3klyrk7fw` |

---

## 4. Prerequisites

### Required Tools

```bash
# Azure CLI (2.50+)
az --version

# Azure CLI login
az login
az account set --subscription "Visual Studio Enterprise"

# Verify subscription
az account show --query '{Name:name, Id:id}' --output table

# Docker (for local testing only — Azure builds use ACR Tasks)
docker --version

# Python 3 (used by deploy scripts to parse JSON output)
python3 --version

# FFmpeg (for testing RTMP ingest)
ffmpeg -version
```

### Required Accounts & Secrets

| Item | Description | How to Generate |
|------|-------------|-----------------|
| Azure Subscription | Visual Studio Enterprise or Pay-as-you-go | Azure portal |
| RTMP Auth Token | Shared secret for RTMP publish authentication | Any strong random string |
| Playback Signing Secret | HMAC-SHA256 key for JWT tokens | `openssl rand -hex 32` |
| Internal API Key | Key for revocation sync between platform↔HLS | `openssl rand -base64 24` |
| Admin Password Hash | bcrypt hash for admin console login | `cd streamgate && npm run hash-password` |
| Domain (optional) | Registered domain for custom DNS | GoDaddy / any registrar |

---

## 5. Phase 1: Deploy rtmp-go

### 5.1 Quick Deploy

```bash
cd rtmp-go/azure

# Set the RTMP auth token (prompted interactively if empty)
export RTMP_AUTH_TOKEN="YourSecretToken"

# Deploy everything
./deploy.sh
```

### 5.2 What deploy.sh Does (5 Steps)

| Step | Action | Details |
|------|--------|---------|
| 1/5 | Create resource group | `az group create --name rg-rtmpgo --location eastus2` |
| 2/5 | Deploy Bicep (placeholder images) | Creates all infrastructure with `containerapps-helloworld` placeholder images |
| 3/5 | Build Docker images via ACR Tasks | Builds 3 images remotely: `rtmp-server`, `blob-sidecar`, `hls-transcoder` |
| 4/5 | Redeploy Bicep with real images | Same Bicep template, now with ACR image references |
| 5/5 | Verify | Checks running status of all 4 container apps |

### 5.3 Environment Variables for deploy.sh

| Variable | Default | Description |
|----------|---------|-------------|
| `RTMP_AUTH_TOKEN` | *(prompted)* | Shared secret for RTMP publish auth |
| `RESOURCE_GROUP` | `rg-rtmpgo` | Azure resource group name |
| `LOCATION` | `eastus2` | Azure region |

### 5.4 Bicep Parameters (rtmp-go)

File: `azure/infra/main.parameters.json`

| Parameter | Default | Description |
|-----------|---------|-------------|
| `environmentName` | `rtmpgo` | Base name for `uniqueString()` resource naming |
| `location` | `eastus2` | Azure region |
| `rtmpAuthToken` | *(empty — set at deploy time)* | RTMP publish authentication token |
| `rtmpServerImage` | *(empty = placeholder)* | ACR image for rtmp-server |
| `blobSidecarImage` | *(empty = placeholder)* | ACR image for blob-sidecar |
| `hlsTranscoderImage` | *(empty = placeholder)* | ACR image for hls-transcoder |

### 5.5 Deploy Output

After successful deployment, the script outputs:

```
RTMP Endpoint (ACA FQDN):
  rtmp://<rtmp-app-fqdn>/live/stream?token=<your-secret>

Test with ffmpeg:
  ffmpeg -re -i test.mp4 -c copy -f flv \
    "rtmp://<rtmp-app-fqdn>/live/stream?token=<your-secret>"

OBS Studio:
  Server:     rtmp://<rtmp-app-fqdn>/live
  Stream Key:  stream?token=<your-secret>
```

**Save the RTMP App FQDN** — needed for DNS setup and StreamGate configuration.

### 5.6 ACR Image Build Details

All three images are built remotely using ACR Tasks (no local Docker required):

| Image | Dockerfile | Build Context | Base Image |
|-------|-----------|---------------|------------|
| `rtmp-server:<tag>` | `Dockerfile` (repo root) | repo root | `golang:1.25-alpine` → `distroless/static` |
| `blob-sidecar:<tag>` | `azure/blob-sidecar/Dockerfile` | `azure/blob-sidecar/` | `golang:1.25-alpine` → `distroless/static` |
| `hls-transcoder:<tag>` | `azure/hls-transcoder/Dockerfile` | `azure/hls-transcoder/` | `golang:1.25-alpine` → `alpine:3.20` + FFmpeg |

> **Image tags**: `deploy.sh` uses timestamp-based tags (`v<epoch>`, e.g. `v1719500000`) instead of `:latest`. This ensures each deployment creates a new revision and avoids stale image caching. The tag is printed in the deployment summary output.

---

## 6. Phase 2: Deploy StreamGate

> **Prerequisite**: rtmp-go must be deployed first. StreamGate deploys *into* the same resource group and shares the ACR, Storage Account, Container Apps Environment, and Managed Identity.

### 6.1 Quick Deploy

```bash
cd streamgate/azure

# Required: bcrypt hash of admin password
# Generate with: cd ../streamgate && npm run hash-password
export ADMIN_PASSWORD_HASH='$2b$12$...'

# Optional: set explicitly, otherwise auto-generated
export PLAYBACK_SIGNING_SECRET="your-hmac-secret"
export INTERNAL_API_KEY="your-api-key"

# Deploy
./deploy.sh
```

### 6.2 What deploy.sh Does (7 Steps)

| Step | Action | Details |
|------|--------|---------|
| 1/7 | Verify rtmp-go deployment | Queries deployment outputs from resource group |
| 2/7 | Discover shared infrastructure | Extracts ACR, Storage, Identity, ACA Env names from rtmp-go outputs |
| 3/7 | Configure secrets | Auto-generates `PLAYBACK_SIGNING_SECRET` and `INTERNAL_API_KEY` if not set; prompts for `ADMIN_PASSWORD_HASH` |
| 4/7 | Deploy Bicep (first pass) | Creates StreamGate-specific resources with placeholder images; uses `PLACEHOLDER` for cross-app URLs |
| 5/7 | Build Docker images | Builds `streamgate-platform` and `streamgate-hls` via ACR Tasks |
| 6/7 | Redeploy Bicep (second pass) | Real images + resolved FQDNs + SAS token for blob proxy; then binds custom domains with managed SSL certificates (if DNS CNAMEs detected) |
| 7/7 | Verify | Checks running status of both container apps |

### 6.3 Two-Pass Deploy Pattern + Custom Domain Binding

StreamGate uses a **two-pass Bicep deployment** because of circular dependencies:

- **Pass 1**: Creates both container apps with placeholder URLs. The HLS server needs the platform FQDN for revocation polling, but the platform FQDN doesn't exist until the app is created.
- **Pass 2**: Now that both apps exist and have FQDNs, redeploy with correct `platformAppUrl`, `corsAllowedOrigin`, `hlsServerBaseUrl`, real container images, and a SAS token.

**Custom domain binding** (after Pass 2): If the deploy script detects DNS CNAME records for `watch.port-80.com` and/or `hls.port-80.com`, it automatically:
1. Adds the hostname to the container app via `az containerapp hostname add`
2. Binds a managed SSL certificate via `az containerapp hostname bind --validation-method CNAME`

This is done via Azure CLI (not Bicep) because managed certificate provisioning is an async operation that doesn't fit well in declarative Bicep templates. The binding is **idempotent** — if the hostname is already bound, it skips the operation.

### 6.4 Environment Variables for deploy.sh

| Variable | Default | Description |
|----------|---------|-------------|
| `RESOURCE_GROUP` | `rg-rtmpgo` | Must match rtmp-go's resource group |
| `LOCATION` | `eastus2` | Azure region |
| `ADMIN_PASSWORD_HASH` | *(required)* | bcrypt hash of admin console password |
| `PLAYBACK_SIGNING_SECRET` | *(auto-generated)* | HMAC-SHA256 key for JWT tokens |
| `INTERNAL_API_KEY` | *(auto-generated)* | Shared key for platform↔HLS revocation sync |
| `HLS_SERVER_BASE_URL` | *(auto-detected from DNS)* | Override HLS server public URL (for custom domains) |
| `CORS_ALLOWED_ORIGIN` | *(auto-detected from DNS)* | Override CORS origin (for custom domains) |
| `PLATFORM_APP_URL` | *(auto-detected from DNS)* | Override platform URL for HLS→Platform revocation sync |
| `DNS_RESOURCE_GROUP` | `rg-dns` | Resource group containing DNS zone (for auto-detection) |
| `DNS_ZONE_NAME` | `port-80.com` | Domain name (for auto-detection) |
| `ADMIN_ALLOWED_IP` | *(auto-detected)* | IP restriction for /admin console |

### 6.5 SAS Token Generation

The deploy script generates a read-only SAS token for the `hls-content` blob container with 1-year expiry:

```bash
az storage container generate-sas \
  --account-name "$STORAGE_ACCOUNT" \
  --name hls-content \
  --permissions rl \
  --expiry "$(date -u -v+1y '+%Y-%m-%dT%H:%MZ')" \
  --https-only \
  -o tsv
```

This token is passed to the HLS server as `UPSTREAM_SAS_TOKEN` for blob proxy fallback.

### 6.6 ACR Image Build Details

| Image | Dockerfile | Build Context | Base Image |
|-------|-----------|---------------|------------|
| `streamgate-platform:latest` | `platform/Dockerfile` | streamgate root | `node:20-alpine` (multi-stage) |
| `streamgate-hls:latest` | `hls-server/Dockerfile` | streamgate root | `node:20-alpine` (multi-stage) |

**Note**: Both StreamGate images use the monorepo root as build context because they need the `shared/` package.

---

## 7. Phase 3: DNS Setup

DNS is deployed to a **separate resource group** (`rg-dns`) so it survives teardowns of the main `rg-rtmpgo`.

### 7.1 Step 1: Create DNS Zone (One-Time)

```bash
cd rtmp-go/azure

# First time — creates the zone, outputs nameservers
./dns-deploy.sh
```

### 7.2 Step 2: Update Domain Registrar (One-Time)

After the DNS zone is created, Azure assigns 4 nameservers. You must update your domain registrar:

1. Log in to GoDaddy: `https://dcc.godaddy.com/domains/port-80.com/dns`
2. Scroll to **Nameservers** → click **Change**
3. Select **Enter my own nameservers (advanced)**
4. Replace ALL existing nameservers with Azure's (e.g.):
   ```
   ns1-04.azure-dns.com.
   ns2-04.azure-dns.net.
   ns3-04.azure-dns.org.
   ns4-04.azure-dns.info.
   ```
5. Save and confirm

**Verification** (may take minutes to 48 hours):
```bash
nslookup -type=NS port-80.com
```

### 7.3 Step 3: Add CNAME for RTMP

```bash
cd rtmp-go/azure

# Add CNAME: stream.port-80.com → rtmp-server FQDN
RTMP_APP_FQDN="azappdu7fhxanu5cak1.azurecontainerapps.io" ./dns-deploy.sh
```

### 7.4 Step 4: Add CNAMEs for StreamGate

```bash
cd streamgate/azure

# Add CNAMEs: watch.port-80.com + hls.port-80.com
PLATFORM_APP_FQDN="sg-platform-olog3klyrk7fw.azurecontainerapps.io" \
HLS_SERVER_FQDN="sg-hls-olog3klyrk7fw.azurecontainerapps.io" \
./dns-deploy.sh
```

### 7.5 Step 5: Redeploy StreamGate with Custom Domains & SSL

After DNS propagates, redeploy StreamGate so the HLS server and platform use custom domain URLs. The script also automatically binds custom domains with managed SSL certificates:

```bash
cd streamgate/azure

# Just re-run deploy.sh — it auto-detects CNAMEs, binds custom domains, and provisions SSL
ADMIN_PASSWORD_HASH='$2b$12$...' \
PLAYBACK_SIGNING_SECRET="..." \
INTERNAL_API_KEY="..." \
./deploy.sh

# Or explicitly override URLs:
HLS_SERVER_BASE_URL="https://hls.port-80.com" \
CORS_ALLOWED_ORIGIN="https://watch.port-80.com" \
ADMIN_PASSWORD_HASH='$2b$12$...' \
PLAYBACK_SIGNING_SECRET="..." \
INTERNAL_API_KEY="..." \
./deploy.sh
```

### 7.6 Custom Domain SSL Binding

After DNS records are deployed, the StreamGate `deploy.sh` script automatically binds custom domains with **Azure managed SSL certificates**. This happens during Step 6/7 (after the second Bicep pass).

| Domain | Container App | Binding | SSL |
|--------|--------------|---------|-----|
| `watch.port-80.com` | sg-platform | SniEnabled | Managed cert (auto-provisioned) |
| `hls.port-80.com` | sg-hls | SniEnabled | Managed cert (auto-provisioned) |
| `stream.port-80.com` | rtmp-server | N/A | None (TCP transport, not HTTPS) |

Managed certificates are provisioned via ACME domain validation using the CNAME records. Provisioning takes **up to 20 minutes** per domain. The `deploy.sh` script starts the provisioning but does not wait for completion — certificates finish provisioning asynchronously.

To check certificate status after deployment:
```bash
az containerapp env certificate list -g rg-rtmpgo -n azenvdu7fhxanu5cak \
  --query "[].{subject:properties.subjectName, state:properties.provisioningState}" -o table
```

To check hostname bindings:
```bash
az containerapp hostname list -g rg-rtmpgo -n <app-name> -o table
```

### 7.7 DNS Records Summary

| Subdomain | Record Type | Target | Deployed By |
|-----------|-------------|--------|-------------|
| `stream.port-80.com` | CNAME | rtmp-server ACA FQDN | rtmp-go dns.bicep |
| `watch.port-80.com` | CNAME | sg-platform ACA FQDN | streamgate dns.bicep |
| `hls.port-80.com` | CNAME | sg-hls ACA FQDN | streamgate dns.bicep |

---

## 8. Bicep Template Reference

### 8.1 rtmp-go `main.bicep` (659 lines)

File: `azure/infra/main.bicep`

| Section | Lines | Resources Created |
|---------|-------|-------------------|
| Parameters | 1–30 | `environmentName`, `location`, `rtmpAuthToken` (secure), 3 image params |
| Variables | 30–55 | `uniqueString()` resource names, tenant config JSON |
| Log Analytics | 55–70 | Workspace for container logs |
| VNet | 70–100 | 10.0.0.0/16 with subnet 10.0.0.0/23 delegated to `Microsoft.App/environments` |
| Container Apps Env | 100–120 | ACA environment on VNet with Log Analytics |
| ACR | 120–140 | Basic SKU container registry |
| Storage Account | 140–190 | Standard LRS + 2 file shares + 2 blob containers |
| Managed Identity | 190–220 | User-assigned + AcrPull + Storage Blob Data Contributor RBAC |
| Storage Mounts | 220–260 | `recordings` (ReadWrite) + `hls-output` (ReadWrite) on ACA Env |
| rtmp-server app | 260–370 | External TCP 1935, webhooks to sidecar + transcoder |
| blob-sidecar app | 370–440 | Internal HTTP 8080, webhook mode, cleanup=true |
| hls-transcoder app | 440–530 | Internal HTTP 8090, multi-container: transcoder (3.5 CPU/7Gi) + blob-sidecar (0.5 CPU/1Gi), ingest via localhost:8081 |
| hls-blob-sidecar app | 530–610 | Scaled to 0 (co-located in transcoder since Phase 4) |
| Outputs | 610–659 | Names, FQDNs, identity info |

#### Tenant Configuration (JSON Secrets)

The blob sidecars receive tenant routing config as a JSON secret. Each sidecar gets its own `tenants.json`:

**blob-sidecar** (recordings):
```json
{
  "tenants": {},
  "default": {
    "storage_account": "https://<account>.blob.core.windows.net",
    "container": "recordings",
    "credential": "managed-identity"
  }
}
```

**co-located blob-sidecar** (HLS content — runs inside hls-transcoder app):
```json
{
  "tenants": {},
  "default": {
    "storage_account": "https://<account>.blob.core.windows.net",
    "container": "hls-content",
    "credential": "managed-identity",
    "path_prefix": "hls"
  }
}
```

### 8.2 StreamGate `main.bicep` (~420 lines)

File: `streamgate/azure/infra/main.bicep`

| Section | Resources Created |
|---------|-------------------|
| Parameters | 20+ params including refs to existing rtmp-go resources, 3 secure params |
| Existing Resources | References to ACA Env, Storage Account, File Service (from rtmp-go) |
| File Shares | `streamgate-data` (1 GiB, SQLite) + `segment-cache` (10 GiB) |
| Storage Mounts | `streamgate-data` + `segment-cache` (both ReadWrite) |
| sg-hls app | External HTTP 4000, JWT validation, Azure Files + Blob proxy |
| sg-platform app | External HTTP 3000, Next.js, SQLite on Azure Files |
| Outputs | App names, FQDNs |

**Key Design**: The HLS app is defined *before* the platform app in Bicep so its FQDN can be referenced in the platform's `HLS_SERVER_BASE_URL` environment variable. The reverse dependency (HLS needing platform URL for revocation polling) is resolved via the two-pass deploy pattern.

### 8.3 DNS Bicep Templates

**rtmp-go `dns.bicep`**: Creates DNS Zone + optional CNAME (`stream.port-80.com`)
**streamgate `dns.bicep`**: Adds CNAMEs to *existing* zone (`watch.port-80.com`, `hls.port-80.com`)

---

## 9. Environment Variables Reference

### 9.1 rtmp-server Container

| Variable | Value in Bicep | Description |
|----------|---------------|-------------|
| `INTERNAL_API_KEY` | Secret ref | Key for platform API access (config fetch + RTMP auth callback) |

The rtmp-server container uses **CLI arguments** in its command array:

```
-listen :1935
-log-level info
-record-all true
-record-dir /recordings
-segment-duration 15m
-auth-mode token
-auth-token "*=<RTMP_AUTH_TOKEN>"
-hook-webhook "publish_start=http://<sidecar-fqdn>/hooks"
-hook-webhook "publish_stop=http://<sidecar-fqdn>/hooks"
-hook-webhook "record_segment=http://<sidecar-fqdn>/hooks"
-hook-webhook "publish_start=http://<transcoder-fqdn>/hooks"
-hook-webhook "publish_stop=http://<transcoder-fqdn>/hooks"
-hook-concurrency 20
```

> **Remote config fetch**: At startup, rtmp-server calls `configfetch.FetchRemoteConfig()` to retrieve `PLAYBACK_SIGNING_SECRET` and `RTMP_AUTH_TOKEN` from the platform API if they are not already set in the environment. This requires `INTERNAL_API_KEY` and the platform URL. See [§12.4](#124-remote-config-fetch-rtmp-server) for details.

### 9.2 blob-sidecar Container

| Variable/Flag | Value | Description |
|---------------|-------|-------------|
| `-mode` | `webhook` | HTTP webhook listener mode |
| `-listen-addr` | `:8080` | HTTP listen port |
| `-config` | `/secrets/tenants.json` | Tenant routing config path |
| `-watch-dir` | `/recordings` | Directory for segment files |
| `-cleanup` | `true` (recordings) / `false` (HLS) | Delete local files after upload |
| `-log-level` | `info` | Log verbosity |
| `AZURE_CLIENT_ID` | Managed Identity Client ID | For managed identity auth to Blob Storage |

### 9.3 hls-transcoder Container

| Flag | Value | Description |
|------|-------|-------------|
| `-listen-addr` | `:8090` | HTTP listen port for webhooks |
| `-hls-dir` | `/hls-output` | Root directory for HLS output |
| `-rtmp-host` | `<rtmp-server-internal-fqdn>` | RTMP server hostname |
| `-rtmp-port` | `1935` | RTMP server port |
| `-rtmp-token` | `<RTMP_AUTH_TOKEN>` | Auth token for RTMP subscribe |
| `-mode` | `abr` | Multi-bitrate adaptive streaming |
| `-output-mode` | `http` | HTTP ingest to co-located blob-sidecar |
| `-ingest-url` | `http://localhost:8081/ingest/` | Co-located blob-sidecar (same Container App) |
| `-ingest-token` | `<INGEST_AUTH_TOKEN>` | Bearer token for HTTP ingest auth |
| `-log-level` | `info` | Log verbosity |

### 9.4 sg-platform Container (StreamGate Platform)

| Variable | Source | Description |
|----------|--------|-------------|
| `DATABASE_URL` | `file:/data/streamgate.db` | SQLite database path (local copy; see entrypoint) |
| `PLAYBACK_SIGNING_SECRET` | Secret ref | HMAC-SHA256 key for JWT signing |
| `INTERNAL_API_KEY` | Secret ref | Key for `/api/revocations` endpoint |
| `ADMIN_PASSWORD_HASH` | Secret ref | bcrypt hash for admin login |
| `HLS_SERVER_BASE_URL` | Derived from HLS app FQDN or override | URL prefix for HLS player |
| `NEXT_PUBLIC_APP_NAME` | `StreamGate` | App branding |
| `SESSION_TIMEOUT_SECONDS` | `60` | Seconds before inactive session is abandoned |
| `ADMIN_ALLOWED_IP` | Auto-detected or override | IP restriction for /admin |

**Docker Entrypoint** (`docker-entrypoint.sh`):
- SQLite doesn't work reliably on Azure Files (SMB locking issues)
- Strategy: copies DB from Azure Files mount (`/data/`) to local disk (`/tmp/`), runs Prisma migrations, syncs back every 60 seconds
- Final sync on container exit via `trap`

### 9.5 sg-hls Container (StreamGate HLS Server)

| Variable | Source | Description |
|----------|--------|-------------|
| `PORT` | `4000` | HTTP listen port |
| `PLAYBACK_SIGNING_SECRET` | Secret ref | Must match platform's secret |
| `INTERNAL_API_KEY` | Secret ref | For polling `/api/revocations` |
| `STREAM_ROOT` | `/hls-output` | Azure Files mount for local HLS files |
| `UPSTREAM_ORIGIN` | `https://<storage>.blob.core.windows.net/hls-content` | Blob Storage URL for upstream proxy (no `/hls` suffix — blobs stored at `{eventId}/...`) |
| `UPSTREAM_SAS_TOKEN` | Secret ref | SAS token for blob access |
| `STREAM_KEY_PREFIX` | _(empty)_ | Prefix prepended to event ID when building upstream URLs. **Must be empty** for HTTP ingest pipeline (blobs stored at `{eventId}/`, not `live_{eventId}/`). Only set to `live_` if using file-based Azure Files SMB mode. |
| `SEGMENT_CACHE_ROOT` | `/segment-cache` | Azure Files mount for cached proxy segments |
| `SEGMENT_CACHE_MAX_SIZE_GB` | `8` | LRU eviction threshold |
| `SEGMENT_CACHE_MAX_AGE_HOURS` | `72` | Age-based cleanup threshold |
| `REVOCATION_POLL_INTERVAL_MS` | `30000` | How often to poll platform for revocations |
| `PLATFORM_APP_URL` | Resolved on second pass | Platform URL for revocation polling |
| `CORS_ALLOWED_ORIGIN` | Derived from platform FQDN or override | CORS header value |

### 9.6 Shared Secrets

| Secret | Used By | Must Match? |
|--------|---------|-------------|
| `RTMP_AUTH_TOKEN` | rtmp-server, hls-transcoder | Yes — transcoder subscribes to RTMP with this token |
| `PLAYBACK_SIGNING_SECRET` | sg-platform (signs JWTs), sg-hls (validates JWTs) | **Yes — must be identical** |
| `INTERNAL_API_KEY` | sg-platform (serves `/api/revocations`), sg-hls (polls it) | **Yes — must be identical** |
| `ADMIN_PASSWORD_HASH` | sg-platform only | No — single service |

---

## 10. Container Apps Detailed Configuration

### 10.1 Ingress Configuration

| App | Transport | Port | External? | Custom Domain | Description |
|-----|-----------|------|-----------|---------------|-------------|
| rtmp-server | TCP | 1935 | Yes (external) | `stream.port-80.com` (CNAME only, no SSL) | Public RTMP ingest endpoint |
| blob-sidecar | HTTP | 8080 | No (internal) | — | Only receives webhooks from rtmp-server |
| hls-transcoder | HTTP | 8090 | No (internal) | — | Only receives webhooks from rtmp-server. Multi-container: co-located blob-sidecar on localhost:8081 |
| sg-platform | HTTP | 3000 | Yes (external) | `watch.port-80.com` (managed SSL) | Public viewer portal + admin |
| sg-hls | HTTP | 4000 | Yes (external) | `hls.port-80.com` (managed SSL) | Public HLS media delivery |

### 10.2 Resource Allocation

| App | vCPU | Memory | Why |
|-----|------|--------|-----|
| rtmp-server | 0.5 | 1Gi | Handles RTMP connections, FLV writing |
| blob-sidecar | 0.25 | 0.5Gi | Light I/O: read file → upload blob |
| hls-transcoder (transcoder container) | 3.5 | 7Gi | FFmpeg ABR encoding (CPU-intensive) |
| hls-transcoder (blob-sidecar container) | 0.5 | 1Gi | HTTP ingest + blob upload (co-located) |
| sg-platform | 0.5 | 1Gi | Next.js SSR + SQLite |
| sg-hls | 0.5 | 1Gi | File serving + JWT validation |

### 10.3 Scaling

All container apps are configured with `minReplicas: 1, maxReplicas: 1` (single-instance). This is intentional:
- RTMP server maintains stateful TCP connections
- Blob sidecars process webhook events sequentially
- HLS transcoder manages FFmpeg child processes
- SQLite database requires single-writer

### 10.4 Volume Mounts

| App | Volume | Mount Path | Share | Access |
|-----|--------|------------|-------|--------|
| rtmp-server | recordings | `/recordings` | recordings (10 GiB) | ReadWrite |
| blob-sidecar | recordings | `/recordings` | recordings (10 GiB) | ReadWrite |
| sg-platform | streamgate-data | `/data` | streamgate-data (1 GiB) | ReadWrite |
| sg-hls | hls-output | `/hls-output` | hls-output (50 GiB) | ReadWrite |
| sg-hls | segment-cache | `/segment-cache` | segment-cache (10 GiB) | ReadWrite |

---

## 11. Storage Architecture

### 11.1 Azure Files Shares

| Share Name | Size | Mounted By | Purpose |
|------------|------|------------|---------|
| `recordings` | 10 GiB | rtmp-server, blob-sidecar | FLV recording segments |
| `streamgate-data` | 1 GiB | sg-platform | SQLite database (synced to local disk) |

### 11.2 Blob Storage Containers

| Container | Access Level | Used By | Purpose |
|-----------|-------------|---------|---------|
| `recordings` | Private | blob-sidecar (upload) | Long-term FLV recording archive |
| `hls-content` | Private | co-located blob-sidecar (upload), sg-hls (read via SAS) | HLS segment archive + upstream proxy source |

### 11.3 Data Lifecycle

1. **Live stream arrives** → rtmp-server writes FLV segments to `recordings/` file share
2. **blob-sidecar** uploads segments to `recordings` blob container, **deletes local** (cleanup=true)
3. **hls-transcoder** receives publish webhook, spawns FFmpeg to transcode RTMP → HLS
4. **FFmpeg** PUTs HLS segments to co-located blob-sidecar at `localhost:8081`, which uploads to `hls-content` blob container
5. **sg-hls** proxies from `hls-content` blob storage (upstream proxy with SAS token)
6. Blob storage persists indefinitely; file shares cleaned up as needed

---

## 12. Secrets & Authentication Flow

### 12.1 RTMP Authentication

```
Broadcaster → RTMP TCP 1935 → rtmp-server
                                   │
                                   ├─ Auth mode: token
                                   ├─ Token format: stream_key?token=SECRET
                                   ├─ Wildcard match: "*=SECRET" (any stream key)
                                   └─ Reject if token missing/wrong → RTMP error
```

### 12.2 Viewer Authentication

```
Viewer → Enter ticket code → sg-platform /api/tokens/validate
                                   │
                                   ├─ Validate code (12-char base62)
                                   ├─ Check: not expired, not revoked, event active
                                   ├─ Check: single-device (no active session)
                                   ├─ Rate limit: 5/min per IP
                                   ├─ Issue JWT (HMAC-SHA256, 1h expiry)
                                   │   Claims: sub, eid, sid, sp, iat, exp
                                   └─ Return JWT to browser

Browser → HLS request + Authorization: Bearer <JWT> → sg-hls
                                   │
                                   ├─ Validate JWT signature (HMAC-SHA256)
                                   ├─ Check expiry
                                   ├─ Check path prefix match
                                   ├─ Check revocation cache
                                   └─ Serve HLS content or 401/403

Player → Every 50min → sg-platform /api/playback/refresh
Player → Every 30s → sg-platform /api/playback/heartbeat
Player → On close → sg-platform /api/playback/release (sendBeacon)
```

### 12.3 Internal Communication

```
sg-hls → GET /api/revocations?since=<timestamp> → sg-platform
           │
           ├─ Header: X-Internal-Api-Key: <INTERNAL_API_KEY>
           ├─ Polls every 30 seconds
           └─ Returns revoked token IDs + deactivated event IDs
```

### 12.4 Remote Config Fetch (rtmp-server)

The `configfetch` package (`internal/configfetch/`) allows rtmp-server to fetch missing secrets from the platform at startup, removing the need to pass every secret through Bicep parameters.

```
rtmp-server (startup) → GET /api/internal/config?keys=PLAYBACK_SIGNING_SECRET,RTMP_AUTH_TOKEN → sg-platform
                          │
                          ├─ Header: X-Internal-Api-Key: <INTERNAL_API_KEY>
                          ├─ Only requests keys not already in environment
                          ├─ Sets fetched values via os.Setenv()
                          └─ Non-fatal on failure (logs warning, continues with env vars)
```

This requires `INTERNAL_API_KEY` to be set on rtmp-server (added to Bicep as `internalApiKey` param → secret). The hls-transcoder also receives the key via the `-platform-api-key` flag for its own platform API calls.

---

## 13. Post-Deployment Validation

### 13.1 Verify Container Status

```bash
RESOURCE_GROUP="rg-rtmpgo"

# Check all container apps
az containerapp list --resource-group $RESOURCE_GROUP \
  --query '[].{Name:name, Status:properties.runningStatus, FQDN:properties.configuration.ingress.fqdn}' \
  --output table
```

Expected output: all 6 apps with `Running` status.

### 13.2 Check Container Logs

```bash
# rtmp-server logs
az containerapp logs show \
  --name azappdu7fhxanu5cak1 \
  --resource-group rg-rtmpgo \
  --tail 50

# StreamGate platform logs
az containerapp logs show \
  --name sg-platform-olog3klyrk7fw \
  --resource-group rg-rtmpgo \
  --tail 50

# HLS server logs
az containerapp logs show \
  --name sg-hls-olog3klyrk7fw \
  --resource-group rg-rtmpgo \
  --tail 50
```

### 13.3 Health Checks

```bash
# HLS server health
curl -s https://<hls-fqdn>/health

# Platform health (should return HTML or redirect)
curl -sI https://<platform-fqdn>/

# RTMP server (TCP — should accept connection then close if no handshake)
nc -zv <rtmp-fqdn> 1935
```

### 13.4 Verify Storage

```bash
STORAGE_ACCOUNT="azstdu7fhxanu5cak"

# List file shares
az storage share list --account-name $STORAGE_ACCOUNT --output table

# Expected: recordings, hls-output, streamgate-data, segment-cache

# List blob containers
az storage container list --account-name $STORAGE_ACCOUNT --output table

# Expected: recordings, hls-content
```

### 13.5 Verify ACR Images

```bash
ACR_NAME="azacrdu7fhxanu5cak"

az acr repository list --name $ACR_NAME --output table

# Expected: rtmp-server, blob-sidecar, hls-transcoder, streamgate-platform, streamgate-hls

# Verify image tags are timestamp-based (not :latest)
az acr repository show-tags --name $ACR_NAME --repository rtmp-server --output table
# Expected: tags like v1719500000 (not "latest")
```

### 13.5a Verify Config API Connectivity

Confirm rtmp-server can reach the platform config endpoint (used for remote config fetch at startup):

```bash
# From your machine (replace <key> with your INTERNAL_API_KEY)
curl -s -H "X-Internal-Api-Key: <key>" \
  "https://watch.port-80.com/api/internal/config?keys=PLAYBACK_SIGNING_SECRET"
# Expected: {"data":{"PLAYBACK_SIGNING_SECRET":"..."}}

# Check rtmp-server logs for successful config fetch
az containerapp logs show --name <rtmp-app-name> -g rg-rtmpgo --tail 20 \
  | grep -i "config fetch"
```

### 13.5b Verify Deploy Health Checks

The deploy script runs `verify_deployment()` for each container app after deployment. Look for `✓` markers in the deploy output:

```
✓ <app-name> is running
```

If any app shows `✗ WARNING`, check the Azure Portal for revision provisioning errors.

### 13.6 Verify DNS (if configured)

```bash
nslookup stream.port-80.com
nslookup watch.port-80.com
nslookup hls.port-80.com
```

### 13.7 Verify Custom Domains & SSL Certificates

```bash
# Check managed certificate provisioning status (both should show "Succeeded")
az containerapp env certificate list -g rg-rtmpgo -n azenvdu7fhxanu5cak \
  --query "[].{subject:properties.subjectName, state:properties.provisioningState}" -o table

# Check hostname bindings (should show "SniEnabled")
az containerapp hostname list -g rg-rtmpgo -n sg-platform-olog3klyrk7fw -o table
az containerapp hostname list -g rg-rtmpgo -n sg-hls-olog3klyrk7fw -o table

# Verify HTTPS/TLS on custom domains
curl -sv https://watch.port-80.com 2>&1 | grep -E "SSL|subject|HTTP/"
curl -sv https://hls.port-80.com/health 2>&1 | grep -E "SSL|subject|HTTP/"

# Expected output includes:
#   SSL connection using TLSv1.3 / AEAD-CHACHA20-POLY1305-SHA256
#   subject: CN=watch.port-80.com
#   SSL certificate verify ok.
```

> **Note**: `stream.port-80.com` (RTMP server) does NOT have an SSL certificate binding because it uses TCP transport on port 1935, not HTTPS. The CNAME record alone provides the custom domain for RTMP.

### 13.8 Verify Managed Identity RBAC

```bash
IDENTITY_NAME="aziddu7fhxanu5cak"

az role assignment list \
  --assignee $(az identity show --name $IDENTITY_NAME --resource-group rg-rtmpgo --query principalId -o tsv) \
  --output table
```

---

## 14. End-to-End Streaming Test

### 14.1 RTMP Publish Test

For best results, use encoder flags that produce a clean RTMP source stream. Avoid B-frames and ensure fixed keyframe intervals (see [OBS Streaming Guide](../../docs/obs-streaming-guide.md) for full recommendations).

```bash
# Recommended: transcode-friendly flags (baseline profile, no B-frames, fixed GOP)
ffmpeg -re -i test.mp4 \
  -c:v libx264 -profile:v baseline -bf 0 -g 60 -keyint_min 60 \
  -b:v 4500k -maxrate 5000k -bufsize 9000k -preset veryfast \
  -c:a aac -b:a 128k -ar 48000 \
  -f flv "rtmp://stream.port-80.com/live/test_stream?token=YourSecretToken"

# Quick test (copy mode — no re-encoding, but source must be well-formed):
ffmpeg -re -i test.mp4 -c copy -f flv \
  "rtmp://stream.port-80.com/live/test_stream?token=YourSecretToken"

# Using ACA FQDN (before DNS setup):
ffmpeg -re -i test.mp4 -c copy -f flv \
  "rtmp://azappdu7fhxanu5cak1.azurecontainerapps.io/live/test_stream?token=YourSecretToken"
```

> **OBS Studio:** See [docs/obs-streaming-guide.md](../../docs/obs-streaming-guide.md) for detailed OBS settings that produce the cleanest source stream for the ABR transcoder.

### 14.2 Verify HLS Output

After publishing for ~15 seconds:

```bash
# Check HLS files on the file share
az storage file list \
  --share-name hls-output \
  --account-name azstdu7fhxanu5cak \
  --path "test_stream" \
  --output table
```

Expected: directories for each quality (e.g., `stream_0/`, `stream_1/`, `stream_2/`) with `.m3u8` and `.ts` files.

### 14.3 Verify Blob Upload

```bash
# Check recordings blob
az storage blob list \
  --container-name recordings \
  --account-name azstdu7fhxanu5cak \
  --output table

# Check HLS content blob
az storage blob list \
  --container-name hls-content \
  --account-name azstdu7fhxanu5cak \
  --prefix "hls/" \
  --output table
```

### 14.4 StreamGate Viewer Test

1. **Admin Console**: Navigate to `https://<platform-fqdn>/admin`
2. **Create Event**: Set stream key to `test_stream` (matching the RTMP publish)
3. **Generate Tokens**: Create 1+ viewer tokens
4. **Publish Stream**: Run the ffmpeg command above
5. **Viewer Portal**: Navigate to `https://<platform-fqdn>`, enter a token code
6. **Verify**: Video should play in the HLS player

### 14.5 Verify Recording Upload

After stopping the ffmpeg publish:

```bash
# Check that local FLV segments were cleaned up (blob-sidecar cleanup=true)
az storage file list \
  --share-name recordings \
  --account-name azstdu7fhxanu5cak \
  --path "test_stream" \
  --output table

# Check that they were uploaded to blob
az storage blob list \
  --container-name recordings \
  --account-name azstdu7fhxanu5cak \
  --prefix "test_stream" \
  --output table
```

---

## 15. Troubleshooting Guide

### 15.1 Container Won't Start

**Symptom**: Container app shows `Failed` or `Waiting` status.

```bash
# Check system logs (provisioning issues)
az containerapp logs show --name <app-name> --resource-group rg-rtmpgo --type system --tail 100

# Check console logs (application errors)
az containerapp logs show --name <app-name> --resource-group rg-rtmpgo --tail 100

# Check revision status
az containerapp revision list --name <app-name> --resource-group rg-rtmpgo \
  --query '[].{Name:name, Status:properties.runningState, Created:properties.createdTime}' --output table
```

**Common causes**:
- **Image pull failure**: Check ACR has the image, managed identity has `AcrPull` role
- **Volume mount failure**: Storage account keys may have rotated; redeploy Bicep
- **Missing secret**: Ensure all required secrets are set in Bicep

### 15.2 RTMP Connection Refused

**Symptom**: `ffmpeg` fails to connect to the RTMP endpoint.

**Checks**:
1. Verify the FQDN resolves: `nslookup <rtmp-fqdn>`
2. Verify port 1935 is reachable: `nc -zv <rtmp-fqdn> 1935`
3. Check if the container is running: `az containerapp show --name <name> --resource-group rg-rtmpgo --query properties.runningStatus`
4. Check ingress config: must be `external: true`, `transport: tcp`, `targetPort: 1935`

### 15.3 RTMP Auth Failure

**Symptom**: ffmpeg connects but stream is rejected (no media flow).

**Checks**:
1. Verify token format: `rtmp://host/live/key?token=SECRET`
2. Check logs for auth error: `az containerapp logs show --name <rtmp-app> ...`
3. Ensure `RTMP_AUTH_TOKEN` matches what was deployed

### 15.4 HLS Not Being Generated

**Symptom**: No HLS files appear in `hls-output` file share after publishing.

**Checks**:
1. Check hls-transcoder logs for webhook receipt:
   ```bash
   az containerapp logs show --name <hls-transcoder-app> --resource-group rg-rtmpgo --tail 50
   ```
2. Look for FFmpeg startup logs and errors
3. Verify the transcoder can reach the RTMP server internally:
   - Check `-rtmp-host` flag matches the rtmp-server internal FQDN
   - Check `-rtmp-token` matches `RTMP_AUTH_TOKEN`
4. Check available disk space on hls-output share

### 15.5 Azure Files SMB Flush Issue (KNOWN BUG — FIXED, FILE MODE ONLY)

**Symptom**: `master.m3u8` is empty or stale on Azure Files.

**Root Cause**: `os.WriteFile()` doesn't trigger an SMB FLUSH — data remains in the client-side cache and isn't visible to other mounts.

**Fix Applied**: The `writeMasterPlaylist()` function now uses `f.Sync()` after writing. The StreamGate HLS server has a dynamic fallback that generates `master.m3u8` from the directory structure if the file is missing or empty.

**Note**: In HTTP ingest mode (`-output-mode http`), this issue doesn't apply. FFmpeg's `-master_pl_name` only writes to the local filesystem even in HTTP output mode, so the transcoder has a dedicated `uploadMasterPlaylist()` function that generates and uploads `master.m3u8` via HTTP PUT to the blob-sidecar after FFmpeg starts (2-second delay for variant playlists to be created).

**If it recurs**: Check that the rtmp-go binary includes commit `eefd0e3` or later.

### 15.6 Publish Webhook Race Condition (KNOWN BUG — NOT YET FIXED)

**Symptom**: When a broadcaster disconnects and immediately reconnects, the `publish_stop` webhook from the old connection may arrive *after* the `publish_start` for the new connection, killing the new FFmpeg process.

**Workaround**: Wait 2-3 seconds between stopping and restarting a publish to the same stream key.

**Proper Fix Needed**: Connection-ID-based stop handling in the hls-transcoder (track which connection started each FFmpeg process and only stop it when the matching connection disconnects).

### 15.7 SQLite on Azure Files (SMB Locking Issues)

**Symptom**: Platform app crashes with SQLite locking errors.

**Root Cause**: SQLite requires POSIX file locking which SMB doesn't fully support.

**Solution Applied**: The `docker-entrypoint.sh` copies the database from Azure Files to local disk (`/tmp/streamgate.db`), runs the app against the local copy, and syncs back to Azure Files every 60 seconds. The DATABASE_URL env var is overridden in the entrypoint.

**Risk**: If the container crashes between syncs, up to 60 seconds of data may be lost.

### 15.8 SAS Token Expired

**Symptom**: HLS server returns 403/404 for blob-proxied segments.

**Fix**: Regenerate the SAS token and redeploy:

```bash
# Generate new 1-year SAS token
NEW_SAS=$(az storage container generate-sas \
  --account-name azstdu7fhxanu5cak \
  --name hls-content \
  --permissions rl \
  --expiry "$(date -u -v+1y '+%Y-%m-%dT%H:%MZ')" \
  --https-only -o tsv)

# Redeploy StreamGate with new token
cd streamgate/azure
# ... run deploy.sh with UPSTREAM_SAS_TOKEN or let the script regenerate it
```

### 15.9 Revocation Sync Not Working

**Symptom**: Revoked tokens still work for HLS playback.

**Checks**:
1. Verify `INTERNAL_API_KEY` matches between platform and HLS server
2. Verify `PLATFORM_APP_URL` is correct in HLS server env (not `PLACEHOLDER`)
3. Check HLS server logs for polling errors:
   ```bash
   az containerapp logs show --name <hls-app> --resource-group rg-rtmpgo --tail 50 | grep revocation
   ```
4. If PLATFORM_APP_URL is `https://PLACEHOLDER`, you need to run the StreamGate deploy script — it was only partially deployed (first pass completed, second pass didn't run)

### 15.10 Admin Console Inaccessible

**Symptom**: `/admin` returns 403 Forbidden.

**Cause**: `ADMIN_ALLOWED_IP` is set and your IP doesn't match.

**Fix**:
```bash
# Check current setting
az containerapp show --name <platform-app> --resource-group rg-rtmpgo \
  --query 'properties.template.containers[0].env[?name==`ADMIN_ALLOWED_IP`]'

# Redeploy with your current IP
ADMIN_ALLOWED_IP=$(curl -s https://ifconfig.me)
# ... redeploy
```

### 15.11 Container Restart Loop

**Symptom**: Container continuously restarts.

```bash
# Check how many restarts
az containerapp revision list --name <app-name> --resource-group rg-rtmpgo \
  --query '[0].properties.runningState'

# Get recent logs to find crash reason
az containerapp logs show --name <app-name> --resource-group rg-rtmpgo --tail 200
```

**Common causes**:
- Missing config file (blob-sidecar needs `/secrets/tenants.json`)
- Missing environment variable (sg-hls requires `PLAYBACK_SIGNING_SECRET`, `PLATFORM_APP_URL`, etc.)
- Database migration failure (sg-platform)

### 15.12 Viewing Log Analytics Queries

```bash
# Query container logs in Log Analytics (KQL)
az monitor log-analytics query \
  --workspace <workspace-id> \
  --analytics-query "ContainerAppConsoleLogs_CL | where ContainerAppName_s == '<app-name>' | order by TimeGenerated desc | take 50" \
  --output table
```

Or use the Azure Portal → Log Analytics Workspace → Logs → query:
```kql
ContainerAppConsoleLogs_CL
| where ContainerAppName_s == "azappdu7fhxanu5cak1"
| order by TimeGenerated desc
| take 100
```

---

### 15.13 Choppy / Stuttering HLS Playback

**Symptom**: Video plays but drops frames, stutters, or has periodic glitches. Resource utilization (CPU/memory) looks normal.

**Root Causes** (in order of likelihood):

1. **Non-monotonic DTS timestamps from source encoder**
   - **Logs**: `[hls] Non-monotonic DTS in output stream`, `[aac] Queue input is backward in time`
   - **Cause**: Source encoder sends audio frames with backwards timestamps (common with B-frames or variable-frame-rate sources)
   - **Fix (transcoder)**: `-async 1 -vsync cfr` flags in FFmpeg force audio resample and constant frame rate (applied in `buildABRArgs`)
   - **Fix (source)**: Use baseline H.264 profile with B-frames disabled (see below)

2. **H.264 decoder reference frame errors**
   - **Logs**: `[h264] co located POCs unavailable`, `[h264] mmco: unref short failure`, `[h264] Missing reference picture`
   - **Cause**: Source encoder sends B-frames or irregular reference chains that the transcoder's H.264 decoder can't resolve
   - **Fix (source)**: Configure encoder with `-profile:v baseline -bf 0` (FFmpeg) or Profile=Baseline, B-frames=0 (OBS)

3. **SMB mount segment deletion conflicts**
   - **Logs**: `[hls muxer] failed to delete old segment ... No such file or directory`
   - **Cause**: FFmpeg's `-hls_flags delete_segments` races with the blob-sidecar's segment polling on Azure Files SMB
   - **Fix (transcoder)**: Use `-hls_flags independent_segments` instead (blob-sidecar manages segment lifecycle)

4. **Segment duration too short for the upload pipeline**
   - **Cause**: 2-second segments may not complete the full pipeline (SMB write → sidecar poll → blob upload → player fetch) before the next segment is due
   - **Fix (transcoder)**: Use `-hls_time 3` for 3-second segments

**Recommended source encoder settings** (FFmpeg):
```bash
ffmpeg -re -i input.mp4 \
  -c:v libx264 -profile:v baseline -bf 0 \
  -g 60 -keyint_min 60 \
  -b:v 4500k -preset veryfast \
  -c:a aac -b:a 128k -ar 48000 \
  -f flv "rtmp://stream.port-80.com/live/stream?token=SECRET"
```

**Recommended OBS settings**: See [docs/obs-streaming-guide.md](../../docs/obs-streaming-guide.md)

### 15.14 Managed SSL Certificate Stuck in "Pending"

**Symptom**: After deploying with custom domains, the managed certificate stays in `Pending` state for over 20 minutes.

```bash
az containerapp env certificate list -g rg-rtmpgo -n azenvdu7fhxanu5cak \
  --query "[].{subject:properties.subjectName, state:properties.provisioningState}" -o table
```

**Checks**:
1. Verify the CNAME record resolves to the correct ACA FQDN:
   ```bash
   nslookup watch.port-80.com   # should resolve to the ACA environment IP
   ```
2. Verify the domain verification ID matches (TXT record is optional with CNAME validation, but check if one exists):
   ```bash
   az containerapp env show -g rg-rtmpgo -n azenvdu7fhxanu5cak \
     --query "properties.customDomainConfiguration.customDomainVerificationId" -o tsv
   ```
3. If stuck, delete and re-create the binding:
   ```bash
   az containerapp hostname delete -g rg-rtmpgo -n <app-name> --hostname <domain> --yes
   az containerapp hostname add -g rg-rtmpgo -n <app-name> --hostname <domain>
   az containerapp hostname bind -g rg-rtmpgo -n <app-name> \
     --hostname <domain> --environment <env-name> --validation-method CNAME
   ```

### 15.15 Azure CLI PascalCase JSON Keys (v2.85+ Breaking Change)

**Symptom**: Deploy script fails to parse DNS CNAME records; `az network dns record-set cname show` returns empty results when querying `cnameRecord.cname`.

**Root Cause**: Azure CLI v2.85+ changed JSON output to use **PascalCase** property names (`CNAMERecord.cname`) instead of the previously documented **camelCase** (`cnameRecord.cname`).

**Fix Applied**: The `deploy.sh` script queries `CNAMERecord.cname` (uppercase). If your Azure CLI version uses a different case, update the JMESPath query accordingly:

```bash
# Azure CLI v2.85+:
az network dns record-set cname show --query 'CNAMERecord.cname' ...

# Older Azure CLI versions:
az network dns record-set cname show --query 'cnameRecord.cname' ...
```

### 15.16 Custom Domain Not Binding (RTMP Server)

**Symptom**: Attempting to bind `stream.port-80.com` to the RTMP server fails.

**Root Cause**: The RTMP server uses **TCP transport** on port 1935. Azure Container Apps managed certificates and custom domain bindings with SSL only work for HTTP/HTTPS ingress. TCP-only apps cannot have managed certificates.

**Solution**: No SSL binding is needed or possible for `stream.port-80.com`. The CNAME record alone routes RTMP traffic. Clients connect via `rtmp://stream.port-80.com:1935/...` (unencrypted RTMP protocol).

### 15.17 HLS HTTP Ingest Pipeline — Step-by-Step Troubleshooting

The HLS HTTP ingest pipeline has 6 stages. When video isn't playing, systematically verify each stage in order. The first failure point is the root cause.

**Pipeline stages:**
```
1. RTMP Publish → 2. publish_start Webhook → 3. FFmpeg Start →
4. HTTP PUT to Sidecar → 5. Sidecar → Azure Blob → 6. HLS Player Fetch
```

#### Stage 1: Verify RTMP Stream is Publishing

**What to check**: Is the RTMP server receiving the stream?

```bash
# Real-time logs
az containerapp logs show --name <rtmp-server-app> -g rg-rtmpgo --tail 20

# Log Analytics (KQL) — check for publisher connection
az monitor log-analytics query -w "<workspace-id>" --analytics-query "
ContainerAppConsoleLogs_CL
| where ContainerAppName_s startswith 'rtmp-server'
| where TimeGenerated > ago(5m)
| where Log_s contains 'publisher registered' or Log_s contains 'auth'
| project TimeGenerated, Log_s
| order by TimeGenerated desc
| take 5
" -o table
```

**Expected**: `publisher registered` log with the stream key.
**If missing**: Check RTMP auth token, ffmpeg connection URL, firewall rules.

#### Stage 2: Verify publish_start Webhook Delivery

**What to check**: Did the RTMP server deliver the webhook to the transcoder?

```bash
az monitor log-analytics query -w "<workspace-id>" --analytics-query "
ContainerAppConsoleLogs_CL
| where ContainerAppName_s startswith 'hls-transcoder'
| where TimeGenerated > ago(5m)
| where Log_s contains 'publish_start'
| project TimeGenerated, Log_s
| order by TimeGenerated desc
| take 5
" -o table
```

**Expected**: `publish_start event received` with the stream key.
**If missing**:
- Check transcoder ingress is `allowInsecure: true` (rtmp-server sends plain HTTP webhooks). **Warning**: `az containerapp ingress update --transport http` resets `allowInsecure` to `false` — always re-apply `--allow-insecure` after any transport change.
- Check RTMP server webhook config points to the correct transcoder FQDN
- Check transcoder revision is running: `az containerapp revision list -n <transcoder> -g rg-rtmpgo -o table`

#### Stage 3: Verify FFmpeg Started

**What to check**: Did the transcoder launch FFmpeg with the correct arguments?

```bash
az monitor log-analytics query -w "<workspace-id>" --analytics-query "
ContainerAppConsoleLogs_CL
| where ContainerAppName_s startswith 'hls-transcoder'
| where TimeGenerated > ago(5m)
| where Log_s contains 'FFmpeg' or Log_s contains 'starting'
| project TimeGenerated, Log_s
| order by TimeGenerated desc
| take 10
" -o table
```

**Expected**: `FFmpeg transcoder started` with `output_mode:http` and a `pid`.
**If `output_mode:file`**: The transcoder is using the old file-based mode. Redeploy with `-output-mode http`.
**If FFmpeg exits immediately**: Check RTMP subscribe token matches, check RTMP server FQDN is reachable.

#### Stage 4: Verify FFmpeg HTTP PUT to Sidecar (MOST COMMON FAILURE POINT)

**What to check**: Is FFmpeg successfully uploading segments via HTTP PUT?

```bash
# Check for TCP/connection errors from FFmpeg
az monitor log-analytics query -w "<workspace-id>" --analytics-query "
ContainerAppConsoleLogs_CL
| where ContainerAppName_s startswith 'hls-transcoder'
| where TimeGenerated > ago(5m)
| where Log_s contains 'error' or Log_s contains 'tcp' or Log_s contains 'Failed' or Log_s contains 'timed'
| project TimeGenerated, Log_s
| order by TimeGenerated desc
| take 10
" -o table
```

**Common failures and fixes:**

| Error | Root Cause | Fix |
|-------|-----------|-----|
| `Connection to tcp://...sidecar...:8081 failed: Operation timed out` | Ingest URL includes `:8081` — Container Apps ingress only exposes port 80/443 | Remove `:8081` from `-ingest-url`. Ingress routes port 80 → targetPort 8081. |
| `Failed to open master play list file 'http://...'` | Same as above, or `allowInsecure: false` on sidecar | Fix URL and/or enable `allowInsecure` |
| FFmpeg runs but sidecar logs show zero requests | `allowInsecure: false` on sidecar ingress — HTTP PUT requests are silently rejected/redirected to HTTPS | `az containerapp ingress update -n <sidecar> -g rg-rtmpgo --allow-insecure` |
| `FFmpeg process exited with error, exit status 255` | FFmpeg couldn't reach the ingest endpoint after retries | Check all of the above |

**Quick diagnostic commands:**

```bash
# Check sidecar ingress config
az containerapp show -n <hls-sidecar> -g rg-rtmpgo \
  --query "properties.configuration.ingress.{targetPort:targetPort, allowInsecure:allowInsecure}" -o json

# Check transcoder ingest URL (look for :8081 — should NOT be present)
az containerapp show -n <hls-transcoder> -g rg-rtmpgo \
  --query "properties.template.containers[0].command" -o json | grep -A1 ingest-url
```

**Correct configuration:**
- Sidecar ingress: `targetPort: 8081`, `allowInsecure: true`
- Transcoder ingest URL: `http://<sidecar-name>.internal.<domain>/ingest/` (no `:8081`)

#### Stage 5: Verify Sidecar Receives and Uploads Segments

**What to check**: Is the sidecar processing PUT requests and uploading to Azure Blob?

```bash
# Real-time sidecar logs
az containerapp logs show --name <hls-sidecar> -g rg-rtmpgo --tail 30

# Log Analytics
az monitor log-analytics query -w "<workspace-id>" --analytics-query "
ContainerAppConsoleLogs_CL
| where ContainerAppName_s startswith 'hls-blob-sidecar'
| where TimeGenerated > ago(5m)
| where Log_s contains 'uploaded' or Log_s contains 'error' or Log_s contains 'ingest'
| project TimeGenerated, Log_s
| order by TimeGenerated desc
| take 20
" -o table
```

**Expected**: `stream uploaded` logs with blob paths and sizes.

**Common failures:**

| Error | Root Cause | Fix |
|-------|-----------|-----|
| `segment too small: -1 bytes` | Size validation rejects chunked transfers (FFmpeg sends `Content-Length: -1`) | Update sidecar code: skip size check when `size < 0` (chunked) |
| `blob upload: unexpected EOF` | Request body piped directly to Azure SDK without buffering | Buffer body into memory first, then upload with `bytes.NewReader` |
| `blob upload: context canceled` | Upload context tied to HTTP request lifetime — client disconnects before upload completes | Use `context.Background()` for blob upload, not `r.Context()` |
| `unauthorized upload attempt` | Ingest token mismatch between transcoder and sidecar | Verify both use the same `ingestToken` value |

#### Stage 6: Verify Blobs in Storage

**What to check**: Are HLS segments actually in Azure Blob Storage?

```bash
# List blobs for the event
az storage blob list \
  --account-name <storage-account> \
  --container-name hls-content \
  --prefix "<event-id>/" \
  --query '[].{name:name, size:properties.contentLength, modified:properties.lastModified}' \
  -o table --auth-mode login | head -20
```

**Expected**: `.ts` segment files and `.m3u8` playlist files under `{eventId}/stream_0/`, `{eventId}/stream_1/`, `{eventId}/stream_2/`, plus `{eventId}/master.m3u8`.
**If empty**: Go back to Stage 5 and check sidecar upload logs.

#### Summary: The Three Most Common HTTP Ingest Failures

These three issues account for the vast majority of HTTP ingest pipeline failures:

1. **`:8081` in the ingest URL** — Container Apps only exposes port 80/443 via FQDN. The ingress `targetPort` routes internally. Remove `:8081` from the URL.

2. **`allowInsecure: false` on sidecar** — FFmpeg sends `http://` PUT requests. Without `allowInsecure: true`, they're silently rejected. This produces the confusing symptom of "FFmpeg is running with no errors, but sidecar receives zero requests." **Warning**: `az containerapp ingress update --transport http` resets `allowInsecure` to `false` — always re-apply `--allow-insecure` after changing transport.

3. **Chunked transfer encoding** — FFmpeg's HLS muxer uses chunked encoding (no `Content-Length`). The sidecar must buffer the full body before uploading to Azure Blob Storage, and size validation must handle `Content-Length: -1`.

4. **TCP transport breaks HTTP routing** — Setting `transport: tcp` on any internal HTTP service's ingress causes ALL requests to that service to fail. Container Apps' Envoy proxy only routes HTTP traffic correctly with `transport: http`. **Never use `transport: tcp` for blob-sidecar or any HTTP-based service.**

5. **`master.m3u8` missing from blob** — FFmpeg's `-master_pl_name` only writes to the local filesystem, even in HTTP output mode. The transcoder explicitly uploads `master.m3u8` via HTTP PUT after FFmpeg starts (2-second delay). If it fails (e.g., sidecar unreachable), `master.m3u8` will be absent from blob storage. The HLS server has a dynamic fallback that generates `master.m3u8` by probing variant playlists.

6. **`STREAM_KEY_PREFIX` path mismatch** — The HTTP ingest pipeline stores blobs at `{eventId}/stream_N/...` (no `live_` prefix). If the HLS server has `STREAM_KEY_PREFIX=live_`, it will look for `live_{eventId}/...` and get 404s. Set `STREAM_KEY_PREFIX` to empty for HTTP ingest deployments.

---

## 16. Teardown Procedures

### 16.1 Remove StreamGate Only (Keep rtmp-go)

```bash
cd streamgate/azure
./destroy.sh
```

This **selectively** removes only StreamGate components:
- Container Apps: `sg-platform`, `sg-hls`
- Storage mounts: `streamgate-data`, `segment-cache`
- File shares: `streamgate-data`, `segment-cache`
- ACR images: `streamgate-platform:latest`, `streamgate-hls:latest`

**Does NOT** delete the resource group or any rtmp-go resources.

### 16.2 Remove All App Resources

```bash
cd rtmp-go/azure
./destroy.sh

# Prompts: type "rg-rtmpgo" to confirm
# Or skip confirmation:
./destroy.sh --yes
```

This deletes the **entire resource group** and all resources within it (both rtmp-go and StreamGate).

### 16.3 Remove DNS Zone

```bash
# rtmp-go DNS (stream.port-80.com)
cd rtmp-go/azure
./dns-destroy.sh

# StreamGate DNS records (watch + hls)
cd streamgate/azure
./dns-destroy.sh
```

**Warning**: Destroying the DNS zone means Azure assigns **new nameservers** when recreated. You'll need to update GoDaddy again.

### 16.4 Check Deletion Status

```bash
# Async deletion — check if complete
az group show --name rg-rtmpgo --query properties.provisioningState --output tsv

# Wait synchronously
az group wait --name rg-rtmpgo --deleted
```

---

## 17. Cost Analysis

### 17.1 Always-On (Current Configuration)

| Component | Monthly Cost |
|-----------|-------------|
| Container Apps (6 apps, consumption plan) | ~$60-80 |
| Azure Container Registry (Basic) | $5 |
| Storage Account (Standard LRS) | ~$2-5 |
| Log Analytics (ingestion) | ~$5-10 |
| Azure DNS Zone | ~$0.50 |
| VNet (no peering/gateway) | $0 |
| **Total** | **~$75-100/mo** |

### 17.2 Scheduled Operation (93% Cost Reduction)

Scale container apps to 0 replicas when not streaming:

```bash
# Scale down (off hours)
for APP in azappdu7fhxanu5cak1 azappdu7fhxanu5cak2 azappdu7fhxanu5cak3 azappdu7fhxanu5cak4 sg-platform-olog3klyrk7fw sg-hls-olog3klyrk7fw; do
  az containerapp update --name $APP --resource-group rg-rtmpgo --min-replicas 0 --max-replicas 0
done

# Scale up (before streaming)
for APP in azappdu7fhxanu5cak1 azappdu7fhxanu5cak2 azappdu7fhxanu5cak3 azappdu7fhxanu5cak4 sg-platform-olog3klyrk7fw sg-hls-olog3klyrk7fw; do
  az containerapp update --name $APP --resource-group rg-rtmpgo --min-replicas 1 --max-replicas 1
done
```

See `azure/005-COST-ANALYSIS.md` for detailed analysis of scheduled operation (~$7/mo for 4 hours/week streaming).

---

## 18. Performance Verification

After deployment or resource changes, run these commands to validate the system is healthy and properly sized.

### 18.1 Prerequisites

All commands require the Azure CLI and the Log Analytics workspace GUID:

```bash
# Get the Log Analytics workspace GUID (not the resource ID)
LAW_GUID=$(az monitor log-analytics workspace show \
  --resource-group rg-rtmpgo \
  --workspace-name "$(az monitor log-analytics workspace list --resource-group rg-rtmpgo --query '[0].name' -o tsv)" \
  --query customerId -o tsv)
echo "Workspace GUID: $LAW_GUID"
```

### 18.2 Send a Test Stream

Send a 60-second test stream with transcode-friendly settings (baseline profile, no B-frames, 1-second keyframes):

```bash
ffmpeg -re \
  -i "test-video.mp4" \
  -t 60 \
  -map 0:v:0 -map 0:a:0 \
  -c:v libx264 -profile:v baseline -bf 0 \
  -g 60 -keyint_min 60 \
  -b:v 4500k -maxrate 5000k -bufsize 9000k \
  -preset veryfast \
  -c:a aac -b:a 128k -ar 48000 \
  -f flv "rtmp://stream.port-80.com/live/<EVENT_UUID>?token=<RTMP_AUTH_TOKEN>"
```

> **Note:** The `-re` flag sends at real-time speed. At ~0.6x encoding speed, a 60-second stream takes ~97 seconds wall time. "Resumed reading" lag messages in the ffmpeg output are from the local `-re` rate limiter, not server-side issues.

### 18.3 Check for Slow Subscriber Drops

Query the RTMP server logs for "slow subscriber" messages. These indicate the transcoder couldn't read RTMP data fast enough, causing the RTMP server's outbound buffer to overflow.

```bash
# Count drops in a time window (adjust timestamps to your test window)
az monitor log-analytics query -w "$LAW_GUID" \
  --analytics-query "
    ContainerAppConsoleLogs_CL
    | where ContainerAppName_s == 'rtmp-server-du7fhxanu5cak'
    | where Log_s has 'slow subscriber'
    | where TimeGenerated > datetime(2026-04-24T09:29:00Z)
    | summarize count()
  " -o table
```

**Expected result:** `Count_` should be **0**. Any non-zero value means the transcoder is falling behind.

To see the timestamps of individual drops:

```bash
az monitor log-analytics query -w "$LAW_GUID" \
  --analytics-query "
    ContainerAppConsoleLogs_CL
    | where ContainerAppName_s == 'rtmp-server-du7fhxanu5cak'
    | where Log_s has 'slow subscriber'
    | where TimeGenerated > datetime(2026-04-24T09:29:00Z)
    | order by TimeGenerated desc
    | take 10
    | project TimeGenerated, Log_s
  " -o table
```

### 18.4 Measure CPU and Memory Utilization

Query Azure Monitor metrics for the transcoder container during the test window:

```bash
SUB_ID=$(az account show --query id -o tsv)

az monitor metrics list \
  --resource "/subscriptions/${SUB_ID}/resourceGroups/rg-rtmpgo/providers/Microsoft.App/containerApps/hls-transcoder-du7fhxanu5cak" \
  --metric "UsageNanoCores" "WorkingSetBytes" \
  --start-time "2026-04-24T09:29:00Z" \
  --end-time "2026-04-24T09:35:00Z" \
  --interval PT1M \
  --aggregation Average Maximum \
  -o table
```

**Interpreting results:**

| Metric | Unit | Conversion | Healthy Range |
|--------|------|------------|---------------|
| `UsageNanoCores` | Nanocores | Divide by 1,000,000,000 to get vCPU | < 80% of allocated vCPU |
| `WorkingSetBytes` | Bytes | Divide by 1,048,576 to get MiB | < 50% of allocated memory |

**Reference values (2 vCPU / 4 GiB, 1080p copy + 720p/480p ultrafast):**

| Metric | Average | Peak | % of Allocation |
|--------|---------|------|-----------------|
| CPU | 0.96 vCPU | 0.97 vCPU | 48% |
| Memory | 195 MiB | 201 MiB | 5% |

### 18.5 Check Transcoder Logs for Errors

```bash
az monitor log-analytics query -w "$LAW_GUID" \
  --analytics-query "
    ContainerAppConsoleLogs_CL
    | where ContainerAppName_s == 'hls-transcoder-du7fhxanu5cak'
    | where TimeGenerated > datetime(2026-04-24T09:29:00Z)
    | where Log_s has 'error' or Log_s has 'stop' or Log_s has 'exit'
    | order by TimeGenerated desc
    | take 10
    | project TimeGenerated, Log_s
  " -o table
```

**Expected messages (not errors):**

| Message | Meaning |
|---------|---------|
| `publish_stop event received` | Normal — RTMP stream ended, transcoder notified |
| `FFmpeg process exited with error ... exit status 255` | Normal — FFmpeg received SIGTERM from publish_stop handler |
| `Non-monotonic DTS` (in FFmpeg stderr) | Normal — expected with B-frame content, FFmpeg handles it |

**Actual errors to investigate:**

| Message | Likely Cause |
|---------|-------------|
| `slow subscriber` in RTMP server logs | Transcoder CPU insufficient; increase vCPU or reduce renditions |
| `FFmpeg process exited with error ... exit status 1` | FFmpeg encoding failure; check FFmpeg stderr in logs |
| `connection refused` / `dial tcp` errors | RTMP server not running or network issue |

### 18.6 Verify Container Resource Allocation

Confirm the running container has the expected CPU and memory:

```bash
az containerapp show \
  --name hls-transcoder-du7fhxanu5cak \
  --resource-group rg-rtmpgo \
  --query "properties.template.containers[0].resources" -o table
```

**Expected output:**

```
Cpu    Gpu    Memory
-----  -----  --------
2      0      4Gi
```

### 18.7 Runtime Resource Scaling (Without Redeployment)

To test different resource allocations without rebuilding images:

```bash
# Scale up (e.g., for higher bitrate streams)
az containerapp update \
  --name hls-transcoder-du7fhxanu5cak \
  --resource-group rg-rtmpgo \
  --cpu 4 --memory 8Gi

# Scale back to recommended
az containerapp update \
  --name hls-transcoder-du7fhxanu5cak \
  --resource-group rg-rtmpgo \
  --cpu 2 --memory 4Gi

# Verify new allocation
az containerapp show \
  --name hls-transcoder-du7fhxanu5cak \
  --resource-group rg-rtmpgo \
  --query "properties.template.containers[0].resources" -o table
```

> **Important:** After finding the right size via `az containerapp update`, update `azure/infra/main.bicep` to match so the next `deploy.sh` run preserves the change.

### 18.8 Sizing Guidelines

| Workload | Recommended | Notes |
|----------|-------------|-------|
| 1080p copy + 720p/480p ultrafast | 2 vCPU / 4 GiB | Peak ~0.97 vCPU (48%), 201 MiB (5%) |
| 3× H.264 encode (no copy) | 4 vCPU / 8 GiB | Will cause drops at 2 vCPU |
| Copy-only (single rendition) | 0.5 vCPU / 1 GiB | Minimal CPU needed |

**Root cause of slow subscriber drops:** When FFmpeg can't encode fast enough, it stops reading from the RTMP socket. TCP backpressure builds up, and the RTMP server's bounded outbound buffer (100 messages) overflows, dropping media messages. The fix is either: (1) reduce encoding work (copy passthrough + ultrafast preset), or (2) increase vCPU allocation.

---

## 19. Quick Reference Card

### Deploy Everything from Scratch

```bash
# 1. Deploy rtmp-go infrastructure
cd rtmp-go/azure
export RTMP_AUTH_TOKEN="YourSecretToken"
./deploy.sh
# → Save RTMP_APP_FQDN from output

# 2. Deploy StreamGate
cd ../../streamgate/azure
export ADMIN_PASSWORD_HASH='$2b$12$...'
export PLAYBACK_SIGNING_SECRET=$(openssl rand -hex 32)
export INTERNAL_API_KEY=$(openssl rand -base64 24)
./deploy.sh
# → Save PLATFORM_FQDN, HLS_FQDN, secrets from output

# 3. Set up DNS (one-time)
cd ../../rtmp-go/azure
./dns-deploy.sh                                    # Create zone
RTMP_APP_FQDN="<fqdn>" ./dns-deploy.sh             # Add stream CNAME
# → Update GoDaddy nameservers

cd ../../streamgate/azure
PLATFORM_APP_FQDN="<fqdn>" HLS_SERVER_FQDN="<fqdn>" ./dns-deploy.sh

# 4. Redeploy StreamGate with custom domains + managed SSL
#    (auto-detects CNAMEs, binds custom domains, provisions SSL certs)
ADMIN_PASSWORD_HASH='$2b$12$...' \
PLAYBACK_SIGNING_SECRET="<saved-secret>" \
INTERNAL_API_KEY="<saved-key>" \
./deploy.sh

# 5. Verify SSL certificates (wait up to 20 min for provisioning)
az containerapp env certificate list -g rg-rtmpgo -n azenvdu7fhxanu5cak \
  --query "[].{subject:properties.subjectName, state:properties.provisioningState}" -o table
curl -sv https://watch.port-80.com 2>&1 | grep -E "SSL|subject"
curl -sv https://hls.port-80.com/health 2>&1 | grep -E "SSL|subject"
```

### Test Publish

```bash
ffmpeg -re -i test.mp4 -c copy -f flv \
  "rtmp://stream.port-80.com/live/test_stream?token=YourSecretToken"
```

### View Logs

```bash
# Replace <app-name> with the container app name
az containerapp logs show --name <app-name> --resource-group rg-rtmpgo --tail 100
```

### Rebuild & Redeploy Single Image

```bash
# Example: rebuild only rtmp-server
ACR_NAME="azacrdu7fhxanu5cak"
az acr build --registry $ACR_NAME --image rtmp-server:latest --file Dockerfile .

# Force revision restart
az containerapp revision restart --name azappdu7fhxanu5cak1 --resource-group rg-rtmpgo \
  --revision $(az containerapp revision list --name azappdu7fhxanu5cak1 --resource-group rg-rtmpgo --query '[0].name' -o tsv)
```

### Emergency: Scale Down All Apps

```bash
for APP in $(az containerapp list --resource-group rg-rtmpgo --query '[].name' -o tsv); do
  az containerapp update --name $APP --resource-group rg-rtmpgo --min-replicas 0 --max-replicas 0
done
```

### Portal Links

```
Resource Group:  https://portal.azure.com/#@/resource/subscriptions/4e0d0fc6-fdf1-4821-94cc-6efbf6ba0667/resourceGroups/rg-rtmpgo/overview
DNS Zone:        https://portal.azure.com/#@/resource/subscriptions/4e0d0fc6-fdf1-4821-94cc-6efbf6ba0667/resourceGroups/rg-dns/overview
```

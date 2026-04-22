# RTMP-Go Azure Deployment

This directory contains infrastructure-as-code (Bicep), deployment scripts, and architecture research for running rtmp-go on Azure Container Apps.

## Quick Start

### Prerequisites

- [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli) installed and logged in (`az login`)
- An active Azure subscription
- Bash shell (macOS/Linux or WSL on Windows)

### Deploy

Run the deploy script from the project root:

```bash
# Interactive — prompts for auth token
./azure/deploy.sh

# Non-interactive
RTMP_AUTH_TOKEN="live/stream=mysecret123" ./azure/deploy.sh
```

The script performs the following steps:

1. **Creates a resource group** (`rg-rtmpgo` in `eastus2` by default)
2. **Deploys Bicep infrastructure** — VNet, Container Apps Environment, ACR, Storage Account, Managed Identity with RBAC roles
3. **Builds Docker images** in ACR using ACR Tasks (no local Docker required) — `rtmp-server` and `blob-sidecar`
4. **Redeploys with real images** — updates container apps from placeholder to the built images
5. **Verifies** both container apps are running and prints the RTMP endpoint

On completion it prints the RTMP URL, ffmpeg test command, and OBS Studio settings.

#### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `RTMP_AUTH_TOKEN` | *(prompted)* | Auth token in `streamKey=secret` format |
| `RESOURCE_GROUP` | `rg-rtmpgo` | Azure resource group name |
| `LOCATION` | `eastus2` | Azure region |

### Destroy

Remove all Azure resources:

```bash
# Interactive — requires typing resource group name to confirm
./azure/destroy.sh

# Skip confirmation
./azure/destroy.sh --yes
```

The script:

1. Lists all resources that will be deleted
2. Asks for confirmation (type the resource group name)
3. Deletes the entire resource group asynchronously (takes 2-5 minutes)

> **Note:** `destroy.sh` only removes the app resource group (`rg-rtmpgo`). The DNS zone in `rg-dns` is preserved so you don't lose your GoDaddy nameserver delegation. Use `dns-destroy.sh` separately if you want to remove DNS too.

### Custom Domain (stream.port-80.com)

The DNS scripts manage an Azure DNS Zone in a **separate resource group** (`rg-dns`) so the domain configuration survives app teardowns.

#### First-Time Setup

**1. Create the DNS zone and get Azure nameservers:**

```bash
./azure/dns-deploy.sh
```

This creates the `rg-dns` resource group with a DNS zone for `port-80.com` and prints 4 Azure DNS nameservers.

**2. Delegate your domain at GoDaddy (one-time):**

1. Go to [GoDaddy DNS Management](https://dcc.godaddy.com/domains/port-80.com/dns)
2. Scroll to **Nameservers** → click **Change**
3. Select **"Enter my own nameservers (advanced)"**
4. Replace all existing nameservers with the 4 Azure values printed by the script, e.g.:
   ```
   ns1-03.azure-dns.com
   ns2-03.azure-dns.net
   ns3-03.azure-dns.org
   ns4-03.azure-dns.info
   ```
5. Click **Save** and confirm the warning dialog

Propagation typically takes a few minutes (up to 48 hours). Verify with:

```bash
nslookup -type=NS port-80.com
```

**3. Add the CNAME record (after deploying the app):**

After running `deploy.sh`, use the ACA FQDN from its output to create the CNAME:

```bash
RTMP_APP_FQDN="azappXXXXX1.eastus2.azurecontainerapps.io" ./azure/dns-deploy.sh
```

**4. Verify end-to-end:**

```bash
nslookup stream.port-80.com          # Should resolve to the ACA FQDN
ffmpeg -re -i test.mp4 -c copy -f flv \
  "rtmp://stream.port-80.com/live/stream?token=<your-secret>"
```

#### Subsequent Deploys

After the initial GoDaddy delegation, you only need to re-run `dns-deploy.sh` with the FQDN if the ACA environment was recreated (new FQDN). The nameservers don't change as long as `rg-dns` exists.

```bash
# After a fresh deploy.sh, update the CNAME if the FQDN changed:
RTMP_APP_FQDN="<new-fqdn>" ./azure/dns-deploy.sh
```

#### DNS Environment Variables

| Variable | Default | Description |
|---|---|---|
| `RTMP_APP_FQDN` | *(empty)* | ACA FQDN — when set, creates/updates the CNAME record |
| `DNS_RESOURCE_GROUP` | `rg-dns` | Resource group for the DNS zone |
| `DNS_ZONE_NAME` | `port-80.com` | Your registered domain name |
| `DNS_SUBDOMAIN` | `stream` | Subdomain for the streaming endpoint |
| `LOCATION` | `eastus2` | Azure region for the resource group |

#### Remove DNS

```bash
# Interactive — requires typing resource group name to confirm
./azure/dns-destroy.sh

# Skip confirmation
./azure/dns-destroy.sh --yes
```

> **Warning:** Deleting the DNS zone means `stream.port-80.com` stops resolving. If you recreate it later, Azure assigns **new** nameservers and you'll need to update GoDaddy again.

### What Gets Deployed

```
Resource Group (rg-rtmpgo)
├── Virtual Network          — 10.0.0.0/16 with Container Apps subnet
├── Container Apps Environment — with VNet integration for TCP ingress
├── Container Registry (Basic) — stores rtmp-server and blob-sidecar images
├── Storage Account            — Azure Files (shared volume) + Blob (recordings archive)
├── Managed Identity           — AcrPull + Storage Blob Data Contributor roles
├── rtmp-server Container App  — TCP ingress on port 1935, token auth, 2-min segment recording
└── blob-sidecar Container App — watches /recordings, uploads segments to Blob Storage
```

### File Structure

```
azure/
├── deploy.sh                 # One-command deploy (creates everything from scratch)
├── destroy.sh                # One-command teardown (deletes app resource group)
├── dns-deploy.sh             # DNS zone + CNAME record deployment
├── dns-destroy.sh            # DNS zone teardown (separate from app destroy)
├── infra/
│   ├── main.bicep            # All Azure resources (Bicep IaC)
│   ├── main.parameters.json  # Default parameter values
│   ├── dns.bicep             # DNS Zone + CNAME record (Bicep IaC)
│   └── dns.parameters.json   # DNS parameter defaults (domain, subdomain)
└── blob-sidecar/             # Blob upload sidecar (Go module with Dockerfile)
```

---

## Architecture Research

The documents below contain research and planning for advanced deployment patterns (scheduled streaming, cost optimization). They are **not required** to deploy — the scripts above handle everything.

### Document Overview

Comprehensive research and implementation guides for deploying RTMP-go to Azure Container Apps with **scheduled streaming** (93% cost reduction).

Total: ~3,200 lines, 100KB of detailed analysis, code examples, and deployment procedures.

---

## Start Here: 5-Minute Overview

### The Challenge
- RTMP-go costs **$148/month** to run 24/7 on Azure
- But you only broadcast **10 hours/week** (scheduled events)
- 98% of the time, services are idle and wasting money

### The Solution
**Scheduled Streaming with Auto-Scale**:
1. Azure Function timer checks Streamgate event calendar every 5 minutes
2. If broadcast starts in <10 min → Scale RTMP/FFmpeg services **up** (minReplicas: 0 → 1)
3. If broadcast ended >10 min ago → Scale services **down** (minReplicas: 1 → 0)
4. Services only run during events + pre/post buffers

### The Result
- **Cost: $148/month → $7/month** ✅ 93% savings!
- **No RTMP-go core changes** (uses sidecar for cloud storage)
- **Fully automatic** (no manual intervention)
- **Cloud-agnostic** (swap Azure ↔ AWS ↔ GCS later)
- **Break-even**: 6 months | **Annual savings**: $1,000+

---

## 📖 Document Guide

Read documents in this order based on your role:

### For Decision Makers / Architects

1. **[001-ARCHITECTURE-OVERVIEW.md](001-ARCHITECTURE-OVERVIEW.md)** (15 min read)
   - High-level design choices
   - Always-on vs scheduled comparison
   - Sidecar vs inline architecture
   - Key decisions made and rationale
   - **→ Answer**: Do we approve this approach?

2. **[005-COST-ANALYSIS.md](005-COST-ANALYSIS.md)** (20 min read)
   - Detailed cost breakdown (always-on vs scheduled)
   - Year-round impact ($1,000+ savings)
   - Sensitivity analysis (what if load changes?)
   - ROI calculation (6-month break-even)
   - **→ Answer**: Is the engineering effort justified?

### For Engineers / Implementers

3. **[003-SIDECAR-PERFORMANCE.md](003-SIDECAR-PERFORMANCE.md)** (25 min read)
   - Why sidecar architecture is superior
   - Performance impact: CPU, memory, latency
   - IPC mechanism comparison (stdout vs socket vs HTTP)
   - Real-world performance data
   - Failure modes & resilience
   - **→ Answer**: What are the technical trade-offs?

4. **[002-SCHEDULED-ORCHESTRATION.md](002-SCHEDULED-ORCHESTRATION.md)** (30 min read)
   - Azure Function timer logic (complete TypeScript code)
   - Streamgate API integration
   - ARM REST API calls for scaling
   - Deployment steps & configuration
   - Testing procedures
   - **→ Answer**: How does orchestration work?

5. **[004-IMPLEMENTATION-GUIDE.md](004-IMPLEMENTATION-GUIDE.md)** (40 min read)
   - Step-by-step code modifications for RTMP-go
   - Segment Storage Service (Node.js & Python)
   - Docker container setup
   - Azure Container Apps deployment
   - Complete checklist & debugging
   - **→ Answer**: How do I build and deploy this?

### For Project Managers / Leads

6. **[006-DEPLOYMENT-CHECKLIST.md](006-DEPLOYMENT-CHECKLIST.md)** (20 min read)
   - 11-hour implementation timeline
   - 7 phases with checkpoints
   - Cost calculator for your use case
   - Common issues & troubleshooting
   - Success criteria
   - **→ Answer**: What's the timeline and what could go wrong?

---

## 📑 Quick Navigation

### By Topic

**Cost & ROI**
- [005-COST-ANALYSIS.md](005-COST-ANALYSIS.md) - Full financial analysis

**Architecture & Design**
- [001-ARCHITECTURE-OVERVIEW.md](001-ARCHITECTURE-OVERVIEW.md) - High-level design
- [003-SIDECAR-PERFORMANCE.md](003-SIDECAR-PERFORMANCE.md) - Technical deep dive

**Implementation**
- [002-SCHEDULED-ORCHESTRATION.md](002-SCHEDULED-ORCHESTRATION.md) - Azure Function code
- [004-IMPLEMENTATION-GUIDE.md](004-IMPLEMENTATION-GUIDE.md) - Full integration guide
- [006-DEPLOYMENT-CHECKLIST.md](006-DEPLOYMENT-CHECKLIST.md) - Timeline & checklist

**Code Examples**
- Node.js Segment Storage Service → [004-IMPLEMENTATION-GUIDE.md](004-IMPLEMENTATION-GUIDE.md) Part 2A
- Python Segment Storage Service → [004-IMPLEMENTATION-GUIDE.md](004-IMPLEMENTATION-GUIDE.md) Part 2B
- Azure Function (TypeScript) → [002-SCHEDULED-ORCHESTRATION.md](002-SCHEDULED-ORCHESTRATION.md)
- Docker build → [004-IMPLEMENTATION-GUIDE.md](004-IMPLEMENTATION-GUIDE.md) Part 3
- Container Apps YAML → [004-IMPLEMENTATION-GUIDE.md](004-IMPLEMENTATION-GUIDE.md) Part 4

---

## 🔑 Key Insights

### 1. Sidecar Pattern (Not Inline Modifications)

Instead of adding Azure SDK to RTMP-go:
```
RTMP-go          Segment Storage Service
├─ Record stream  ├─ Read metadata from stdin
└─ Output         └─ Upload to Blob Storage
  metadata → 
```

**Benefits:**
- ✅ RTMP-go stays cloud-agnostic
- ✅ Easy to swap Azure ↔ AWS ↔ GCS
- ✅ Clean separation of concerns
- ✅ Reusable for other streaming servers

**Cost**: ~200-500ms per segment (acceptable for 3-min segments)

See: **[003-SIDECAR-PERFORMANCE.md](003-SIDECAR-PERFORMANCE.md)**

### 2. Scheduled Orchestration (Not Always-On)

Instead of running services 24/7:
```
Events API (Streamgate)
    ↓
Azure Function Timer (every 5 min)
    ├─ Check: Is broadcast starting soon?
    │  Yes → Scale minReplicas: 0 → 1
    │
    └─ Check: Did broadcast just end?
       Yes → Scale minReplicas: 1 → 0
```

**Benefits:**
- ✅ 93% cost reduction ($148 → $7/month)
- ✅ Fully automatic scaling
- ✅ True scale-to-zero for RTMP (was impossible before)
- ✅ No manual intervention needed

**Prerequisite**: Events must be pre-scheduled (your use case ✓)

See: **[002-SCHEDULED-ORCHESTRATION.md](002-SCHEDULED-ORCHESTRATION.md)**

### 3. Performance Impact: Negligible

Sidecar adds latency **only** to segment uploads (async background work):
```
Stream 1: 3 hrs/week active, 20+ hrs idle
          ├─ RTMP ingress: 0% overhead
          ├─ Segment recording: 0% overhead
          └─ Upload to Blob: ~200-500ms (async, non-blocking)

Impact on live broadcast: ZERO
```

Even at 10 Mbps (large segments), upload time is ~1-2 seconds.
Since segments are 3 minutes long, latency is completely masked.

See: **[003-SIDECAR-PERFORMANCE.md](003-SIDECAR-PERFORMANCE.md) - Performance Analysis**

---

## 🚀 Implementation Path

### Phase 1: Design Review (1 hour)
1. Read [001-ARCHITECTURE-OVERVIEW.md](001-ARCHITECTURE-OVERVIEW.md)
2. Read [005-COST-ANALYSIS.md](005-COST-ANALYSIS.md)
3. Confirm with team: Approve sidecar approach?

### Phase 2: RTMP-go Modification (2 hours)
1. Follow [004-IMPLEMENTATION-GUIDE.md](004-IMPLEMENTATION-GUIDE.md) Part 1
2. Add `--segment-metadata-cmd` flag
3. Create segment notifier
4. Local testing with ffmpeg

### Phase 3: Build Container (2 hours)
1. Follow [004-IMPLEMENTATION-GUIDE.md](004-IMPLEMENTATION-GUIDE.md) Part 2 & 3
2. Create Segment Storage Service (Node.js or Python)
3. Build Docker image
4. Test locally

### Phase 4: Deploy to Azure (1 hour)
1. Follow [004-IMPLEMENTATION-GUIDE.md](004-IMPLEMENTATION-GUIDE.md) Part 4
2. Create Container Apps
3. Deploy container image
4. Verify logs

### Phase 5: Orchestration Function (1 hour)
1. Follow [002-SCHEDULED-ORCHESTRATION.md](002-SCHEDULED-ORCHESTRATION.md)
2. Create Azure Function (timer-trigger)
3. Configure Streamgate API integration
4. Deploy and test

### Phase 6: E2E Testing (2 hours)
1. Follow [006-DEPLOYMENT-CHECKLIST.md](006-DEPLOYMENT-CHECKLIST.md) Phase 5
2. Create test event in Streamgate
3. Watch Function scale services
4. Publish test RTMP stream
5. Verify upload to Blob

### Phase 7: Production Hardening (2 hours)
1. Follow [006-DEPLOYMENT-CHECKLIST.md](006-DEPLOYMENT-CHECKLIST.md) Phase 6
2. Enable monitoring & alerts
3. Test error scenarios
4. Document runbooks

**Total Time: ~11 hours** | **Savings: $1,000+/year** ✅

See: **[006-DEPLOYMENT-CHECKLIST.md](006-DEPLOYMENT-CHECKLIST.md)**

---

## 🏗️ Architecture Diagram

```
┌─────────────────────────────────────────────────────────┐
│ STREAMGATE (Your Existing Platform)                     │
│ ├─ Events table (scheduled broadcasts)                 │
│ └─ API: GET /api/events?status=upcoming                │
└──────────────────────┬──────────────────────────────────┘
                       │ (query every 5 min)
                       ↓
┌─────────────────────────────────────────────────────────┐
│ AZURE FUNCTION (ScheduleStreamOrchestrator)             │
│ ├─ Timer trigger: every 5 minutes                       │
│ ├─ Logic: Scale services based on event schedule       │
│ └─ API: PATCH minReplicas via ARM                       │
└──────────────────────┬──────────────────────────────────┘
                       │ (PATCH scaling)
                       ↓
┌─────────────────────────────────────────────────────────┐
│ AZURE CONTAINER APPS (minReplicas=0 by default)         │
│                                                         │
│ ┌─────────────────┐  ┌─────────────────┐  ┌──────────┐ │
│ │ RTMP Server     │  │ FFmpeg HLS      │  │ HLS Srv  │ │
│ │ (Port 1935)     │  │ Transcoder      │  │ (3000)   │ │
│ │ + Sidecar       │  │                 │  │          │ │
│ │   Service       │  │                 │  │          │ │
│ └────────┬────────┘  └────────┬────────┘  └────┬─────┘ │
│          │                    │                 │       │
│          └────────────────────┼─────────────────┘       │
│                               │                         │
│         ┌──────────────────────┴──────────────┐         │
│         ↓                                     ↓         │
│    /tmp/segments (ephemeral)        Metadata → stdout  │
│                                                         │
│ Inside Sidecar Service:                                 │
│ ├─ Read metadata from stdin                            │
│ ├─ Upload file to Blob Storage                         │
│ └─ Clean up local copy                                 │
│                                                         │
└────────────────────┬────────────────────────────────────┘
                     │ (upload)
                     ↓
┌─────────────────────────────────────────────────────────┐
│ AZURE BLOB STORAGE (Segments 3 min × N bitrates)        │
│ ├─ Cost: ~$0.018/GB                                     │
│ ├─ Lifecycle: Delete after 30 days                      │
│ └─ Archive: Available for VOD/replay                    │
└─────────────────────────────────────────────────────────┘
```

---

## 💰 Cost Comparison

### Always-On (What You're Paying Now)
```
RTMP Server:     24×7 @ 0.5 vCPU = $21/month
FFmpeg HLS:      24×7 @ 1.0 vCPU = $48/month
HLS Playback:    24×7 @ 0.5 vCPU = $20/month
Storage:         Continuous       = $2/month
─────────────────────────────────────────────
TOTAL:           $91/month = $1,100/year
```

### Scheduled (What You'll Pay After)
```
RTMP Server:     12h/week @ 0.5 vCPU = $1.40/month
FFmpeg HLS:      12h/week @ 1.0 vCPU = $3.13/month
HLS Playback:    ~4h/week @ 0.5 vCPU = $0.38/month
Storage:         Continuous          = $2/month
─────────────────────────────────────────────
TOTAL:           $7/month = $84/year
```

### Your Savings
```
$91 - $7 = $84/month = $1,000+/year ✅ 93% reduction!
```

See: **[005-COST-ANALYSIS.md](005-COST-ANALYSIS.md)** for detailed breakdown

---

## ❓ FAQ

**Q: Do I need to modify RTMP-go core?**
A: Yes, but minimally. Just add a flag to output segment metadata. No cloud-specific code.

**Q: What if I want to use AWS S3 later?**
A: Swap the Segment Storage Service (sidecar). RTMP-go doesn't change.

**Q: What if Streamgate API is down?**
A: Function logs error, services keep running (safe default). Manual scaling available.

**Q: What about network latency uploading segments?**
A: 200-500ms per 3-min segment is negligible and fully asynchronous. Zero impact on live stream.

**Q: How long until this pays for itself?**
A: 6 months of engineering cost (estimated $600) vs $1,000/year savings = break-even in 6 months.

**Q: Can I use this for other streaming servers?**
A: Yes! Segment Storage Service is generic. Any server can output JSON to stdout.

See: **[001-ARCHITECTURE-OVERVIEW.md](001-ARCHITECTURE-OVERVIEW.md)** for more FAQs

---

## 📞 Support

### For Architecture Questions
→ Read **[001-ARCHITECTURE-OVERVIEW.md](001-ARCHITECTURE-OVERVIEW.md)**

### For Performance Questions
→ Read **[003-SIDECAR-PERFORMANCE.md](003-SIDECAR-PERFORMANCE.md)**

### For Cost Questions
→ Read **[005-COST-ANALYSIS.md](005-COST-ANALYSIS.md)**

### For Implementation Questions
→ Read **[004-IMPLEMENTATION-GUIDE.md](004-IMPLEMENTATION-GUIDE.md)**

### For Deployment Questions
→ Read **[006-DEPLOYMENT-CHECKLIST.md](006-DEPLOYMENT-CHECKLIST.md)**

### For Orchestration Questions
→ Read **[002-SCHEDULED-ORCHESTRATION.md](002-SCHEDULED-ORCHESTRATION.md)**

---

## 📊 Key Metrics

| Metric | Value |
|--------|-------|
| **Monthly Cost Reduction** | 93% ($84 savings) |
| **Annual Savings** | $1,000+ |
| **Break-Even Time** | 6 months |
| **Implementation Time** | 11 hours |
| **Segment Upload Latency** | 200-500ms (async) |
| **Impact on Live Stream** | Zero |
| **Scale-to-Zero Enabled** | ✅ Yes (sidecar pattern) |
| **Cloud-Agnostic** | ✅ Yes (sidecar pattern) |

---

## ✅ Success Criteria

You'll know this is working when:

1. ✅ RTMP-go accepts connections and outputs metadata
2. ✅ Segment Storage Service uploads to Blob within 500ms
3. ✅ Azure Function scales services based on event schedule
4. ✅ Container Apps minReplicas = 0 between broadcasts
5. ✅ Monthly bill = ~$7 (was $91)
6. ✅ No manual scaling needed
7. ✅ All code is open-source and reusable

---

## 🎓 Reading Tips

- **Busy executives**: Read 001 + 005 (35 min)
- **Architects**: Read 001, 003, 005 (60 min)
- **Engineers**: Read all 6 documents (120 min)
- **Implementers**: Focus on 004 + 006 (90 min)

---

## 📝 Document Stats

| Document | Lines | Purpose |
|----------|-------|---------|
| 001-ARCHITECTURE-OVERVIEW.md | 344 | Design decisions |
| 002-SCHEDULED-ORCHESTRATION.md | 652 | Function implementation |
| 003-SIDECAR-PERFORMANCE.md | 563 | Technical analysis |
| 004-IMPLEMENTATION-GUIDE.md | 727 | Step-by-step code |
| 005-COST-ANALYSIS.md | 411 | Financial analysis |
| 006-DEPLOYMENT-CHECKLIST.md | 428 | Project management |
| **TOTAL** | **3,125** | **Complete guide** |

---

## 🚀 Next Steps

1. **Today**: Read 001 + 005 (35 min)
2. **Tomorrow**: Read 003 (25 min) + discuss with team
3. **This week**: Read 002 + 004 + 006 (90 min) + start implementation
4. **Next week**: Deploy to Azure
5. **Week after**: Live streaming with 93% cost savings! 🎉

---

**Created**: April 20, 2026  
**Version**: 1.0  
**Status**: Ready for implementation  
**Branch**: `azure`

Happy streaming! 🎬

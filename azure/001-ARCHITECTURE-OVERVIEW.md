# RTMP-Go Azure Deployment: Architecture Overview

## Executive Summary

This document outlines the deployment of RTMP-go to Azure Container Apps with **scheduled streaming** architecture for 93% cost reduction.

**Key constraint**: All streaming sessions are scheduled in advance via Streamgate platform.

**Key insight**: Since broadcasts are scheduled, services only need to run 10 minutes before → 10 minutes after each event, enabling true scale-to-zero and dramatic cost savings.

---

## Scheduled Streaming: 93% Cost Reduction

> **Publish lifecycle integration**: The Azure deployment can now send `publish_start` and `publish_stop` webhooks directly to Streamgate's `/api/rtmp/hooks` endpoint so the platform can track active RTMP sessions in real time while the HLS transcoder hooks continue to run.

### Cost Comparison

| Scenario | RTMP Hours/Week | Total Cost/Month | Comment |
|----------|-----------------|------------------|---------|
| **Always-On** | 168 (24/7) | $148 | TCP ingress prevents scale-to-zero |
| **Scheduled** (Recommended) | 12 (with 10min buffers) | $9 | Services start 10min before, stop 10min after each event |
| **Savings** | - | **93% ($139/month)** | 🎉 |

**Assumptions**: 5 streams/week × 2 hrs each + 10-min pre/post buffers

---

## How It Works: Scheduled Orchestration

```
┌─────────────────────────────────────────────────────────┐
│ STREAMGATE PLATFORM (your existing system)              │
├─────────────────────────────────────────────────────────┤
│ Events Table:                                           │
│  - id, streamKey, startsAt, endsAt, status, etc.       │
│  - API: GET /api/events?status=upcoming                │
└─────────────────────────────────────────────────────────┘
                          ↑
                          │ (queries every 5 min)
                          │
┌─────────────────────────────────────────────────────────┐
│ AZURE FUNCTION (ScheduleStreamOrchestrator)             │
├─────────────────────────────────────────────────────────┤
│ Timer Trigger: Every 5 minutes                          │
│                                                         │
│ Logic:                                                  │
│  1. Query Streamgate API for upcoming events            │
│  2. If event starts in <10min → Scale services to 1    │
│  3. If event ended >10min ago → Scale services to 0    │
│  4. Use ARM REST API to PATCH Container Apps           │
└─────────────────────────────────────────────────────────┘
                          ↓
                (PATCH minReplicas)
                          ↓
┌─────────────────────────────────────────────────────────┐
│ AZURE CONTAINER APPS (minReplicas=0, maxReplicas=20)   │
├─────────────────────────────────────────────────────────┤
│                                                         │
│ ┌──────────────────────────────────────────────────┐   │
│ │ RTMP Server (Go)                                 │   │
│ │ - Port 1935 (RTMP) + 443 (RTMPS)                │   │
│ │ - Records 3-min segments → [Service]            │   │
│ │ - [NEW] Segment metadata via stdout             │   │
│ └──────────────────────────────────────────────────┘   │
│                                                         │
│ ┌──────────────────────────────────────────────────┐   │
│ │ Segment Storage Service [NEW] (Node.js/Python)  │   │
│ │ - Reads segment metadata from RTMP-go via stdin │   │
│ │ - Uploads to Azure Blob Storage                 │   │
│ │ - Cloud-agnostic (can swap for AWS S3, etc.)   │   │
│ └──────────────────────────────────────────────────┘   │
│                                                         │
│ ┌──────────────────────────────────────────────────┐   │
│ │ FFmpeg HLS Transcoder (sidecar)                  │   │
│ │ - Consumes RTMP stream                           │   │
│ │ - Produces 4-bitrate HLS                         │   │
│ │ - Outputs to shared volume                       │   │
│ └──────────────────────────────────────────────────┘   │
│                                                         │
│ ┌──────────────────────────────────────────────────┐   │
│ │ HLS Server (Node.js Express)                     │   │
│ │ - Serves m3u8 and .ts files                      │   │
│ │ - Connects to Streamgate for auth/metadata      │   │
│ └──────────────────────────────────────────────────┘   │
│                                                         │
└─────────────────────────────────────────────────────────┘
                          ↓
          (uploads segments to)
                          ↓
┌─────────────────────────────────────────────────────────┐
│ AZURE BLOB STORAGE (blob)                              │
├─────────────────────────────────────────────────────────┤
│ Lifecycle Policy: Delete segments after 30 days        │
│ Cost: ~$0.018/GB (very cheap)                          │
└─────────────────────────────────────────────────────────┘
```

#### Config Fetch at Startup

Services can fetch missing configuration from the StreamGate Platform's `/api/internal/config` endpoint at startup, authenticated via `X-Internal-Api-Key` header. This reduces the number of secrets that must be passed through infrastructure parameters — only `INTERNAL_API_KEY` is needed per service, and the platform becomes the single source of truth for shared secrets like `PLAYBACK_SIGNING_SECRET`.

---

## Timeline: Broadcast Day

```
Monday, April 21, 2025

09:40 UTC ─── Function Timer Runs
              Query Streamgate: "Any events starting soon?"
              Result: "Conference 2025 starts at 09:50"
              ↓
              Decision: "START SERVICES NOW" (10 min pre-buffer)
              ↓
              PATCH ARM API: minReplicas=1 for all apps
              
09:45 UTC ─── Services warming up
              Container replicas booting
              
09:50 UTC ─── Services Ready ✅
              RTMP listening on 0.0.0.0:1935
              FFmpeg ready to transcode
              HLS server listening on port 3000
              
09:52 UTC ─── Broadcaster Goes Live
              OBS publishes rtmp://server:1935/live/conference
              RTMP-go receives stream
              Sends publish_start webhook to Streamgate `/api/rtmp/hooks`
              Writes to ephemeral disk
              Sends segment metadata to Storage Service
              
09:53 UTC ─── Storage Service Uploads
              Reads segment metadata from RTMP-go stdout
              Uploads latest 3-min segment to Blob
              (latency ~200ms)
              
10:00 UTC ─── First Segment Complete
              Blob Storage: conference_seg_000.bin (5MB)
              
10:03 UTC ─── Second Segment Complete
              Blob Storage: conference_seg_001.bin (5MB)
              
... (continues every 3 minutes) ...

11:50 UTC ─── Broadcast Ends
              RTMP-go stream ends
              Final segment uploaded to Blob
              
12:00 UTC ─── Function Timer Runs (10 min post-buffer)
              Query Streamgate: "Any events active?"
              Result: "Conference 2025 ended 10+ min ago"
              ↓
              Decision: "STOP SERVICES NOW"
              ↓
              PATCH ARM API: minReplicas=0 for all apps
              
12:01 UTC ─── Services Stopped 🔴
              All replicas terminated
              Cost: $0/hour (except Blob Storage)
              
12:02 UTC ─── Blob Storage Archived
              12 segments × 5MB = 60MB safely archived
              Available for VOD/replay via Streamgate
```

---

## Key Design Decisions

### 1. **Scheduled vs Always-On**
- ✅ **Chosen**: Scheduled (events API driven)
- ❌ Not Always-On: TCP port 1935 prevents minReplicas=0

### 2. **RTMP-go Code Changes**
- **Option A** (Inline): Add Azure Blob SDK directly to RTMP-go
  - ❌ Couples cloud-specific code to core RTMP logic
  - ❌ Future AWS/GCS support requires conditional compilation
  
- **Option B** (Sidecar Service) ✅ **CHOSEN**
  - ✅ Keeps RTMP-go cloud-agnostic
  - ✅ Segment Storage Service is plug-and-play
  - ✅ Easy to swap Azure ↔ AWS ↔ GCS without touching RTMP-go
  - ✅ Better separation of concerns
  - ⚠️ Slight latency overhead (200-500ms per segment) - **acceptable for 3-min segments**

### 3. **Segment Delivery Method**
- **Option A**: Direct file access (mount shared volume)
  - ❌ Requires container networking complexity
  
- **Option B** (Chosen): stdout/Events channel
  - ✅ Process isolation, easy to debug
  - ✅ Works in containerized environment
  - ✅ Minimal overhead
  
### 4. **Orchestration Trigger**
- ✅ **Timer Function** (every 5 min) not HTTP webhook
  - Why: Scheduled events = predictable, can poll efficiently
  - Alternative (HTTP): Would need RTMP-go to call back on stream start/end (adds coupling)

---

## Architecture Options Compared

### Option A: Inline (RTMP-go modified)
```
RTMP-go
├─ Ingest stream
├─ Record segments
├─ Call Azure SDK: azblob.UploadBlob(ctx, segment)
└─ Continue to HLS transcoding
```

**Pros:**
- Simpler deployment (one container)
- Faster segment delivery (no IPC)

**Cons:**
- 🔴 Azure code in RTMP-go core
- 🔴 Hard to add AWS/GCS later
- 🔴 Secrets (Azure credentials) in RTMP-go config
- 🔴 Adds 3-5 MB to binary (Azure SDK)

### Option B: Sidecar Service (Recommended) ✅
```
RTMP-go                          Segment Storage Service
├─ Ingest stream                 ├─ Read from stdin
├─ Record segments               ├─ Parse segment metadata
├─ Output metadata → stdout  ←───┤─ Upload to Blob
└─ Continue to HLS trans-        └─ Log completion
  coding
```

**Pros:**
- ✅ RTMP-go stays cloud-agnostic
- ✅ Segment Storage Service is reusable
- ✅ Easy to swap Azure ↔ AWS ↔ GCS
- ✅ Secrets isolated in sidecar
- ✅ Better separation of concerns
- ✅ Easier testing/debugging

**Cons:**
- ⚠️ ~200-500ms latency per segment upload
- ⚠️ Requires IPC (stdout/stdin or Unix socket)
- ⚠️ Two containers instead of one

**Why Option B is Better for rtmp-go:**
- RTMP-go is a **general-purpose server**, not Azure-specific
- Future users (AWS, GCS, on-prem, etc.) benefit from clean separation
- Segment Storage Service becomes a **reusable component** you can open-source

---

## Performance Analysis: Sidecar Approach

See `003-SIDECAR-PERFORMANCE.md` for detailed analysis.

**TL;DR:**
- Latency overhead: **200-500ms per segment** (acceptable)
- CPU overhead: **negligible** (~2-3%)
- Memory overhead: **10-20MB** for sidecar
- Impact on streaming: **zero** (segments are async, don't block publishing)

---

## Files in This Directory

1. **001-ARCHITECTURE-OVERVIEW.md** (this file)
   - High-level design, options, decision rationale

2. **002-SCHEDULED-ORCHESTRATION.md**
   - Detailed Azure Function timer logic
   - Streamgate API integration
   - ARM REST API calls for scaling

3. **003-SIDECAR-PERFORMANCE.md**
   - Detailed analysis of sidecar approach
   - Performance implications
   - IPC options (stdout vs Unix socket vs gRPC)

4. **004-IMPLEMENTATION-GUIDE.md**
   - Step-by-step: RTMP-go integration
   - Segment Storage Service code (Go/Node.js/Python)
   - Deployment YAML for Container Apps
   - Testing procedures

5. **005-COST-BREAKDOWN.md**
   - Detailed cost analysis
   - Always-on vs scheduled comparison
   - Scaling scenarios

6. **006-DEPLOYMENT-CHECKLIST.md**
   - Pre-deployment verification
   - Azure infrastructure setup
   - Testing procedures
   - Production hardening

---

## Next Steps

**Phase 1: Design Review**
- [ ] Review architecture (this document)
- [ ] Review sidecar performance analysis
- [ ] Approve IPC mechanism (stdout vs socket)

**Phase 2: Implementation**
- [ ] Modify RTMP-go to output segment metadata
- [ ] Create Segment Storage Service
- [ ] Deploy to Azure dev environment
- [ ] Test with local RTMP publisher

**Phase 3: Azure Setup**
- [ ] Create Azure resource group
- [ ] Create Container Apps environment
- [ ] Create Storage Account and blob container
- [ ] Deploy three Container Apps
- [ ] Assign Managed Identities

**Phase 4: Orchestration**
- [ ] Create Azure Function project
- [ ] Deploy ScheduleStreamOrchestrator
- [ ] Test auto-scale on/off with scheduled events

**Phase 5: Production**
- [ ] End-to-end testing with real Streamgate events
- [ ] Implement monitoring/alerts
- [ ] Document runbooks
- [ ] Go live!

---

## Questions Answered

**Q: Do I need to modify RTMP-go?**
A: Minimally. Just add a flag `-segment-notify-cmd` that calls a command (or writes to stdout) when segments complete. RTMP-go doesn't need to know about Azure/Blob/S3.

**Q: Can I use this for AWS later?**
A: Yes! The Segment Storage Service is cloud-agnostic. Deploy `segment-storage-aws.js` instead of the Azure version.

**Q: What about latency?**
A: ~200-500ms per segment is acceptable because segments are 3 minutes long. The upload happens asynchronously; it doesn't block the RTMP stream.

**Q: Is this more or less complex than inline?**
A: More containers, but cleaner separation. Easier to debug, maintain, and extend. Trade-off is worthwhile.

---

## Contact & Questions

See individual documents for deeper analysis on specific topics.

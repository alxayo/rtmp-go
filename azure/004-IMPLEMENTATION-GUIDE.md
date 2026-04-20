# Implementation Guide: Sidecar Segment Storage Service

## Overview

This guide walks through implementing the **sidecar service** approach where RTMP-go outputs segment metadata via stdout, and a separate service uploads to Azure Blob Storage.

**Key benefit**: RTMP-go stays cloud-agnostic; you can swap Azure ↔ AWS ↔ GCS without touching RTMP-go.

---

## Part 1: Modify RTMP-go to Output Segment Metadata

### Step 1: Add Flag for Segment Notification

File: `cmd/rtmp-server/flags.go`

```go
var (
  // ... existing flags ...
  
  segmentMetadataCmd = flag.String(
    "segment-metadata-cmd",
    "",
    "Command to invoke when segment completes (receives JSON on stdin). "+
      "Example: /app/segment-storage-service. Leave empty to disable.")
  
  segmentMetadataInterval = flag.Int(
    "segment-metadata-interval",
    3, // minutes
    "Target segment duration in minutes (default 3).")
)
```

### Step 2: Create Segment Metadata Notifier

Create: `internal/rtmp/server/segment_notifier.go`

```go
package server

import (
  "encoding/json"
  "fmt"
  "io"
  "os"
  "os/exec"
  "time"
)

// SegmentMetadata describes a recorded segment
type SegmentMetadata struct {
  Stream      string    `json:"stream"`      // Stream key (e.g., "live/conference")
  Index       int64     `json:"index"`       // Segment sequence number
  Size        int64     `json:"size"`        // File size in bytes
  Path        string    `json:"path"`        // Absolute path to segment file
  StartTime   int64     `json:"start_time"` // Segment start timestamp (ms)
  EndTime     int64     `json:"end_time"`   // Segment end timestamp (ms)
  DurationMs  int64     `json:"duration_ms"`
  Timestamp   time.Time `json:"timestamp"`  // When recorded
}

// SegmentNotifier sends segment metadata to an external service
type SegmentNotifier struct {
  cmd string // Command to run (e.g., "/app/segment-storage-service")
}

// NewSegmentNotifier creates a notifier (cmd can be empty to disable)
func NewSegmentNotifier(cmd string) *SegmentNotifier {
  return &SegmentNotifier{cmd: cmd}
}

// NotifySegment sends segment metadata to the external service
func (n *SegmentNotifier) NotifySegment(meta SegmentMetadata) error {
  if n.cmd == "" {
    return nil // Disabled
  }

  // Marshal to JSON
  data, err := json.Marshal(meta)
  if err != nil {
    return fmt.Errorf("marshal metadata: %w", err)
  }

  // Run command with metadata on stdin
  cmd := exec.Command(n.cmd)
  cmd.Stdin = bytes.NewReader(append(data, '\n'))
  cmd.Stdout = os.Stdout
  cmd.Stderr = os.Stderr

  // Execute asynchronously (don't block on upload)
  go func() {
    if err := cmd.Run(); err != nil {
      // Log error but don't fail (background operation)
      fmt.Fprintf(os.Stderr, "segment notifier error: %v\n", err)
    }
  }()

  return nil
}
```

### Step 3: Wire Notifier into Server

File: `cmd/rtmp-server/main.go`

```go
package main

import (
  // ... imports ...
  "github.com/alxayo/rtmp-go/internal/rtmp/server"
)

func main() {
  flag.Parse()

  // Create server
  srv := server.NewServer(
    *listen,
    *recordPath,
    logger,
  )

  // If segment metadata command specified, enable notifications
  if *segmentMetadataCmd != "" {
    srv.SegmentNotifier = server.NewSegmentNotifier(*segmentMetadataCmd)
    logger.Info("segment notifications enabled", "cmd", *segmentMetadataCmd)
  }

  // Start server
  if err := srv.ListenAndServe(*listen); err != nil {
    logger.Error("server error", "err", err)
    os.Exit(1)
  }
}
```

### Step 4: Invoke Notifier on Segment Rotation

When segments rotate (typically in the recorder), call the notifier:

```go
// In recorder callback when segment completes:
metadata := server.SegmentMetadata{
  Stream:     streamKey,
  Index:      segmentIndex,
  Size:       int64(fileSize),
  Path:       segmentFilePath,
  StartTime:  segmentStartTimeMs,
  EndTime:    segmentEndTimeMs,
  DurationMs: segmentEndTimeMs - segmentStartTimeMs,
  Timestamp:  time.Now(),
}

if err := notifier.NotifySegment(metadata); err != nil {
  logger.Error("failed to notify segment", "err", err)
  // Don't fail; continue streaming
}
```

---

## Part 2: Create Segment Storage Service

### Option A: Node.js Implementation (Recommended)

**File**: `segment-storage-service/index.ts`

```typescript
import * as readline from 'readline'
import * as fs from 'fs'
import * as path from 'path'
import { BlobServiceClient } from '@azure/storage-blob'
import { DefaultAzureCredential } from '@azure/identity'

interface SegmentMetadata {
  stream: string
  index: number
  size: number
  path: string
  start_time: number
  end_time: number
  duration_ms: number
  timestamp: string
}

class SegmentStorageService {
  private rl: readline.Interface
  private blobClient: BlobServiceClient
  private containerName: string
  private uploadQueue: Map<string, Promise<void>> = new Map()

  constructor() {
    // Initialize readline
    this.rl = readline.createInterface({
      input: process.stdin,
      output: process.stdout,
      terminal: false,
    })

    // Initialize Azure Blob Storage client
    const accountName = process.env.AZURE_STORAGE_ACCOUNT_NAME
    const containerName = process.env.AZURE_CONTAINER_NAME || 'segments'
    this.containerName = containerName

    if (accountName) {
      // Using DefaultAzureCredential (Managed Identity)
      const blobEndpoint = `https://${accountName}.blob.core.windows.net`
      this.blobClient = new BlobServiceClient(
        blobEndpoint,
        new DefaultAzureCredential()
      )
    } else {
      // Using connection string (for local testing)
      const connectionString =
        process.env.AZURE_STORAGE_CONNECTION_STRING ||
        'UseDevelopmentStorage=true' // Azure Storage Emulator
      this.blobClient =
        BlobServiceClient.fromConnectionString(connectionString)
    }

    // Start listening for segment metadata
    this.startListening()
  }

  private startListening(): void {
    this.rl.on('line', (line) => {
      try {
        const metadata: SegmentMetadata = JSON.parse(line)
        this.handleSegment(metadata)
      } catch (error: any) {
        console.error(`Failed to parse metadata: ${error.message}`)
      }
    })

    this.rl.on('close', () => {
      console.log('Stdin closed, waiting for pending uploads...')
      this.waitForPendingUploads().then(() => {
        console.log('All uploads completed, exiting.')
        process.exit(0)
      })
    })

    process.on('SIGINT', () => {
      console.log('Received SIGINT, shutting down gracefully...')
      this.rl.close()
    })

    process.on('SIGTERM', () => {
      console.log('Received SIGTERM, shutting down gracefully...')
      this.rl.close()
    })
  }

  private async handleSegment(metadata: SegmentMetadata): Promise<void> {
    const blobName = `${metadata.stream}/segment_${String(metadata.index).padStart(6, '0')}.bin`
    const uploadKey = `${metadata.stream}:${metadata.index}`

    console.log(`📦 Segment received: ${blobName} (${this.formatBytes(metadata.size)})`)

    // Start async upload
    const uploadPromise = this.uploadSegment(metadata, blobName)
    this.uploadQueue.set(uploadKey, uploadPromise)

    // Clean up queue entry when done
    uploadPromise
      .then(() => {
        this.uploadQueue.delete(uploadKey)
      })
      .catch((error) => {
        console.error(`Upload failed for ${blobName}: ${error.message}`)
        this.uploadQueue.delete(uploadKey)
      })
  }

  private async uploadSegment(
    metadata: SegmentMetadata,
    blobName: string
  ): Promise<void> {
    try {
      const containerClient = this.blobClient.getContainerClient(
        this.containerName
      )
      const blockBlobClient = containerClient.getBlockBlobClient(blobName)

      // Create container if it doesn't exist
      await containerClient.createIfNotExists()

      // Upload file
      const startTime = Date.now()
      const fileStream = fs.createReadStream(metadata.path)

      await blockBlobClient.uploadStream(fileStream, metadata.size, {
        metadata: {
          stream: metadata.stream,
          index: String(metadata.index),
          duration_ms: String(metadata.duration_ms),
          recorded_at: metadata.timestamp,
        },
      })

      const duration = Date.now() - startTime
      console.log(
        `✅ Uploaded: ${blobName} in ${duration}ms`
      )

      // Clean up local file after successful upload
      try {
        fs.unlinkSync(metadata.path)
        console.log(`🗑️  Deleted local file: ${metadata.path}`)
      } catch (error: any) {
        console.warn(`Failed to delete local file: ${error.message}`)
      }
    } catch (error: any) {
      console.error(`❌ Upload failed for ${blobName}: ${error.message}`)
      // Retry logic would go here (exponential backoff)
      // For now, log and continue
    }
  }

  private async waitForPendingUploads(): Promise<void> {
    if (this.uploadQueue.size === 0) {
      return
    }

    console.log(`⏳ Waiting for ${this.uploadQueue.size} pending uploads...`)
    const promises = Array.from(this.uploadQueue.values())
    await Promise.all(promises)
  }

  private formatBytes(bytes: number): string {
    if (bytes === 0) return '0 Bytes'
    const k = 1024
    const sizes = ['Bytes', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i]
  }
}

// Start service
const service = new SegmentStorageService()
console.log('✅ Segment Storage Service started, listening for metadata on stdin')
```

**File**: `segment-storage-service/package.json`

```json
{
  "name": "segment-storage-service",
  "version": "1.0.0",
  "description": "Stores RTMP segments to cloud storage",
  "main": "dist/index.js",
  "scripts": {
    "build": "tsc",
    "start": "node dist/index.js",
    "dev": "ts-node index.ts"
  },
  "dependencies": {
    "@azure/identity": "^3.4.0",
    "@azure/storage-blob": "^3.23.0"
  },
  "devDependencies": {
    "@types/node": "^20.0.0",
    "ts-node": "^10.9.0",
    "typescript": "^5.0.0"
  }
}
```

**File**: `segment-storage-service/tsconfig.json`

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "module": "commonjs",
    "lib": ["ES2020"],
    "outDir": "./dist",
    "rootDir": "./",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "forceConsistentCasingInFileNames": true
  },
  "include": ["index.ts"],
  "exclude": ["node_modules"]
}
```

### Option B: Python Implementation

**File**: `segment-storage-service/main.py`

```python
#!/usr/bin/env python3

import json
import sys
import os
import asyncio
import logging
from pathlib import Path
from azure.identity import DefaultAzureCredential
from azure.storage.blob import BlobServiceClient, ContainerClient

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class SegmentStorageService:
    def __init__(self):
        self.account_name = os.getenv('AZURE_STORAGE_ACCOUNT_NAME')
        self.container_name = os.getenv('AZURE_CONTAINER_NAME', 'segments')
        self.upload_queue = {}
        
        # Initialize blob client
        if self.account_name:
            blob_endpoint = f"https://{self.account_name}.blob.core.windows.net"
            self.blob_client = BlobServiceClient(
                blob_endpoint,
                credential=DefaultAzureCredential()
            )
        else:
            conn_string = os.getenv('AZURE_STORAGE_CONNECTION_STRING', 
                                    'UseDevelopmentStorage=true')
            self.blob_client = BlobServiceClient.from_connection_string(conn_string)
    
    async def handle_segment(self, metadata: dict):
        stream = metadata.get('stream')
        index = metadata.get('index')
        path = metadata.get('path')
        size = metadata.get('size')
        
        blob_name = f"{stream}/segment_{index:06d}.bin"
        
        logger.info(f"📦 Segment received: {blob_name} ({self.format_bytes(size)})")
        
        # Schedule async upload
        task = asyncio.create_task(self.upload_segment(metadata, blob_name))
        self.upload_queue[f"{stream}:{index}"] = task
    
    async def upload_segment(self, metadata: dict, blob_name: str):
        try:
            container_client = self.blob_client.get_container_client(self.container_name)
            
            # Create container if not exists
            try:
                container_client.create_container()
            except:
                pass  # Container already exists
            
            # Upload blob
            start_time = asyncio.get_event_loop().time()
            
            with open(metadata['path'], 'rb') as data:
                container_client.upload_blob(
                    name=blob_name,
                    data=data,
                    overwrite=True,
                    metadata={
                        'stream': metadata['stream'],
                        'index': str(metadata['index']),
                        'duration_ms': str(metadata['duration_ms']),
                        'recorded_at': metadata['timestamp'],
                    }
                )
            
            duration = (asyncio.get_event_loop().time() - start_time) * 1000
            logger.info(f"✅ Uploaded: {blob_name} in {duration:.0f}ms")
            
            # Clean up local file
            try:
                Path(metadata['path']).unlink()
                logger.info(f"🗑️  Deleted local file: {metadata['path']}")
            except Exception as e:
                logger.warning(f"Failed to delete local file: {e}")
        
        except Exception as e:
            logger.error(f"❌ Upload failed for {blob_name}: {e}")
    
    async def run(self):
        logger.info("✅ Segment Storage Service started")
        
        try:
            # Read from stdin
            loop = asyncio.get_event_loop()
            while True:
                line = await loop.run_in_executor(None, sys.stdin.readline)
                if not line:
                    break
                
                try:
                    metadata = json.loads(line)
                    await self.handle_segment(metadata)
                except json.JSONDecodeError as e:
                    logger.error(f"Failed to parse metadata: {e}")
        
        except KeyboardInterrupt:
            logger.info("Received interrupt, shutting down gracefully...")
        finally:
            # Wait for pending uploads
            if self.upload_queue:
                logger.info(f"⏳ Waiting for {len(self.upload_queue)} pending uploads...")
                await asyncio.gather(*self.upload_queue.values(), return_exceptions=True)
            
            logger.info("All uploads completed, exiting.")
            sys.exit(0)
    
    @staticmethod
    def format_bytes(bytes_count: int) -> str:
        for unit in ['Bytes', 'KB', 'MB', 'GB']:
            if bytes_count < 1024:
                return f"{bytes_count:.1f} {unit}"
            bytes_count /= 1024
        return f"{bytes_count:.1f} TB"

if __name__ == '__main__':
    service = SegmentStorageService()
    asyncio.run(service.run())
```

**File**: `segment-storage-service/requirements.txt`

```
azure-storage-blob==3.23.0
azure-identity==3.4.0
```

---

## Part 3: Docker Container Setup

### Dockerfile for RTMP-go + Segment Service

**File**: `Dockerfile`

```dockerfile
# Stage 1: Build RTMP-go
FROM golang:1.21-alpine AS builder-rtmp

WORKDIR /src
COPY . .

RUN go build -o /usr/local/bin/rtmp-server ./cmd/rtmp-server

# Stage 2: Build Segment Storage Service
FROM node:18-alpine AS builder-segment

WORKDIR /app
COPY segment-storage-service/package*.json ./
RUN npm ci

COPY segment-storage-service/. .
RUN npm run build

# Stage 3: Runtime
FROM alpine:3.18

RUN apk add --no-cache \
    ca-certificates \
    ffmpeg \
    nodejs

# Copy RTMP server
COPY --from=builder-rtmp /usr/local/bin/rtmp-server /app/rtmp-server

# Copy Segment Storage Service
COPY --from=builder-segment /app/node_modules /app/node_modules
COPY --from=builder-segment /app/dist /app/segment-service

# Create volumes
RUN mkdir -p /recordings /tmp/segments

# Set working directory
WORKDIR /app

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD nc -z localhost 1935 || exit 1

ENTRYPOINT ["/app/rtmp-server"]
CMD ["-listen", "0.0.0.0:1935", "-record-all", "true", \
     "-record-path", "/recordings", \
     "-segment-metadata-cmd", "/app/segment-service-runner.sh"]
```

**File**: `segment-service-runner.sh`

```bash
#!/bin/sh
# Wrapper script to run segment storage service

export NODE_OPTIONS="--max-old-space-size=512"
exec node /app/segment-service/index.js
```

---

## Part 4: Azure Container Apps Deployment

**File**: `container-app.yaml`

```yaml
apiVersion: containerapp.io/v1beta1
kind: ContainerApp
metadata:
  name: rtmp-server
  resourceGroup: my-streaming-rg
spec:
  template:
    containers:
    - name: rtmp-go
      image: myregistry.azurecr.io/rtmp-go:latest
      ports:
      - containerPort: 1935
        transport: Tcp
      env:
      - name: SEGMENT_METADATA_CMD
        value: "node /app/segment-service/index.js"
      - name: AZURE_CONTAINER_NAME
        value: "segments"
      - name: AZURE_STORAGE_ACCOUNT_NAME
        value: "mystorageaccount"
      - name: RUST_LOG
        value: "info"
      resources:
        cpu: "1"
        memory: "2Gi"
      volumeMounts:
      - name: segments
        mountPath: /recordings
        
  volumes:
  - name: segments
    emptyDir: {}
    
  scale:
    minReplicas: 0
    maxReplicas: 10
    rules:
    - name: tcp-scale
      custom:
        query: "avg(azure.communication.stream_bitrate)"
        metadata:
          metricName: "StreamBitrate"

  ingress:
    transport: tcp
    targetPort: 1935
    allowInsecure: false
    external: true

  identityType: "SystemAssigned"

  secrets:
  - name: storage-key
    value: ${AZURE_STORAGE_KEY}
```

---

## Part 5: Deployment Checklist

### Pre-Deployment
- [ ] RTMP-go compiles with segment notifier code
- [ ] Segment Storage Service builds successfully
- [ ] Container image builds without errors
- [ ] Environment variables configured
- [ ] Managed Identity has Storage Blob Data Contributor role

### Deployment
- [ ] Push image to Azure Container Registry
- [ ] Deploy Container App with YAML
- [ ] Verify container starts and runs
- [ ] Check logs for errors

### Post-Deployment
- [ ] Test with local RTMP publisher
- [ ] Verify segments appear in Blob Storage
- [ ] Check upload latency (should be <1 second)
- [ ] Monitor logs for errors
- [ ] Test graceful shutdown (SIGTERM handling)

---

## Debugging

### View Service Logs
```bash
az containerapp logs show \
  -n rtmp-server \
  -g my-streaming-rg \
  --container-name rtmp-go \
  --follow
```

### Test Segment Metadata Format
```bash
# Simulate segment metadata
cat << 'EOF' | node segment-storage-service/dist/index.js
{"stream":"live/test","index":1,"size":5242880,"path":"/tmp/test.bin","start_time":1629312000000,"end_time":1629312180000,"duration_ms":180000,"timestamp":"2026-04-20T21:59:10.351+03:00"}
EOF
```

### Check Blob Storage Contents
```bash
az storage blob list \
  -c segments \
  --account-name mystorageaccount \
  --output table
```

---

## Summary

**What you've implemented:**
1. ✅ RTMP-go outputs segment metadata via command invocation
2. ✅ Segment Storage Service reads metadata and uploads to Blob
3. ✅ No Azure-specific code in RTMP-go core
4. ✅ Easy to swap storage backend later (AWS S3, GCS, MinIO)

**Next steps:**
- Deploy to Azure Container Apps
- Integrate with ScheduleStreamOrchestrator for auto-scaling
- Test end-to-end with scheduled events

See `002-SCHEDULED-ORCHESTRATION.md` for orchestration setup.

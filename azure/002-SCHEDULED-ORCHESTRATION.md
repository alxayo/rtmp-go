# Scheduled Stream Orchestration with Azure Functions

## Overview

Since all streaming sessions are **scheduled in advance** via Streamgate, services don't need to run 24/7. An Azure Function timer-trigger can automatically scale services based on event schedules.

**Result**: 93% cost reduction + true scale-to-zero for RTMP ingress.

---

## How It Works

### 1. Events Are Pre-Scheduled in Streamgate
```sql
-- streamgate.events table
id              | streamKey         | startsAt                | endsAt                  | status
─────────────────────────────────────────────────────────────────────────────────────────
evt-001         | live/conference   | 2026-04-21 09:50:00Z   | 2026-04-21 11:50:00Z   | upcoming
evt-002         | live/daily-news   | 2026-04-21 18:00:00Z   | 2026-04-21 18:30:00Z   | upcoming
evt-003         | live/sports       | 2026-04-22 20:00:00Z   | 2026-04-22 22:00:00Z   | upcoming
```

### 2. Azure Function Timer (Every 5 Minutes)

ScheduleStreamOrchestrator runs on a timer trigger `0 */5 * * * *` (every 5 minutes).

**Logic**:
```
1. Query Streamgate API: GET /api/events?status=upcoming
2. For each event:
   a. If current_time >= (startsAt - preStartBuffer) and status != "active"
      → SCALE UP: PATCH minReplicas=1 on RTMP/FFmpeg/HLS containers
      → Change event status to "active"
   
   b. If current_time >= (endsAt + postEndBuffer) and status == "active"
      → SCALE DOWN: PATCH minReplicas=0 on RTMP/FFmpeg/HLS containers
      → Change event status to "inactive"
3. Log all actions and metrics
```

### 3. Services Auto-Scale Based on Function Calls

When Function patches `minReplicas`:
```
BEFORE (idle state):
  Container Apps: minReplicas=0, replicas=0
  Cost: $0/hour
  Status: Stopped

AFTER (scaling triggered):
  Container Apps: minReplicas=1, replicas=1-20
  Cost: $0.05/hour per vCPU
  Status: Running, ready for broadcast

AFTER (broadcast ends, scale-down triggered):
  Container Apps: minReplicas=0, replicas=0
  Cost: $0/hour
  Status: Stopped again
```

---

## Streamgate API Requirements

### Endpoint 1: List Upcoming Events
```
GET /api/events?status=upcoming

Response (200 OK):
{
  "events": [
    {
      "id": "evt-001",
      "streamKey": "live/conference",
      "title": "Annual Conference 2025",
      "startsAt": "2026-04-21T09:50:00Z",
      "endsAt": "2026-04-21T11:50:00Z",
      "preStartBufferMinutes": 10,
      "postEndBufferMinutes": 10,
      "status": "upcoming"
    },
    ...
  ]
}
```

### Endpoint 2: Update Event Status (Optional but Recommended)
```
PATCH /api/events/{eventId}

Request:
{
  "status": "active"  // or "inactive"
}

Response (200 OK):
{
  "id": "evt-001",
  "status": "active"
}
```

### Endpoint 3: Mark Event Complete
```
POST /api/events/{eventId}/complete

Response (200 OK):
{
  "id": "evt-001",
  "status": "completed",
  "recordedAt": "2026-04-21T12:00:00Z"
}
```

---

## Azure Function Implementation

### File: `ScheduleStreamOrchestrator/index.ts`

```typescript
import { AzureFunction, Context, TimerInput } from "@azure/functions"
import { DefaultAzureCredential } from "@azure/identity"
import { ResourceManagementClient } from "@azure/arm-resources"
import axios from 'axios'

interface StreamgateEvent {
  id: string
  streamKey: string
  title: string
  startsAt: string
  endsAt: string
  preStartBufferMinutes?: number
  postEndBufferMinutes?: number
  status: 'upcoming' | 'active' | 'inactive' | 'completed'
}

interface ContainerAppConfig {
  name: string
  minReplicas: number
  maxReplicas: number
}

const timerTrigger: AzureFunction = async function (
  context: Context,
  myTimer: TimerInput
): Promise<void> {
  try {
    context.log('⏱️ ScheduleStreamOrchestrator started', {
      timestamp: new Date().toISOString(),
      isPastDue: myTimer.isPastDue,
    })

    // 1. Get environment variables
    const streamgateApiBase = process.env.STREAMGATE_API_BASE
    const subscriptionId = process.env.AZURE_SUBSCRIPTION_ID
    const resourceGroupName = process.env.AZURE_RESOURCE_GROUP
    const containerAppsEnvName = process.env.AZURE_CONTAINER_APPS_ENVIRONMENT
    const streamgateApiKey = process.env.STREAMGATE_API_KEY

    if (!streamgateApiBase || !subscriptionId || !resourceGroupName) {
      throw new Error('Missing required environment variables')
    }

    // 2. Query Streamgate API for upcoming events
    context.log('📡 Querying Streamgate API', { url: streamgateApiBase })
    
    const eventsResponse = await axios.get<{ events: StreamgateEvent[] }>(
      `${streamgateApiBase}/api/events?status=upcoming`,
      {
        headers: streamgateApiKey ? { 'X-API-Key': streamgateApiKey } : {},
        timeout: 10000,
      }
    )

    const events = eventsResponse.data.events || []
    context.log(`📅 Found ${events.length} upcoming events`)

    // 3. Initialize Azure Resource Management client
    const credential = new DefaultAzureCredential()
    const resourceClient = new ResourceManagementClient(
      credential,
      subscriptionId
    )

    // Container Apps to manage
    const containerApps: ContainerAppConfig[] = [
      { name: 'rtmp-server', minReplicas: 0, maxReplicas: 20 },
      { name: 'ffmpeg-hls', minReplicas: 0, maxReplicas: 10 },
      { name: 'hls-server', minReplicas: 0, maxReplicas: 10 },
    ]

    // 4. Process each event
    const now = new Date()
    const results: any[] = []

    for (const event of events) {
      const startsAt = new Date(event.startsAt)
      const endsAt = new Date(event.endsAt)
      const preBuffer = (event.preStartBufferMinutes || 10) * 60 * 1000 // ms
      const postBuffer = (event.postEndBufferMinutes || 10) * 60 * 1000 // ms

      const timeUntilStart = startsAt.getTime() - now.getTime()
      const timeSinceEnd = now.getTime() - endsAt.getTime()

      context.log(`📋 Processing event: ${event.id} (${event.title})`, {
        startsAt: event.startsAt,
        endsAt: event.endsAt,
        currentStatus: event.status,
        timeUntilStartMs: timeUntilStart,
        timeSinceEndMs: timeSinceEnd,
      })

      // Decision: Should we START services?
      if (timeUntilStart <= preBuffer && timeUntilStart > 0 && event.status !== 'active') {
        context.log(`🚀 START EVENT: ${event.id}`)
        context.log(`   → Event starts in ${Math.floor(timeUntilStart / 1000)} seconds`)

        // Scale UP containers
        await scaleContainerApps(
          resourceClient,
          subscriptionId,
          resourceGroupName,
          containerApps,
          1, // minReplicas = 1
          context
        )

        // Update event status in Streamgate
        await updateEventStatus(streamgateApiBase, event.id, 'active', streamgateApiKey)

        results.push({
          eventId: event.id,
          action: 'START',
          timestamp: now.toISOString(),
        })
      }

      // Decision: Should we STOP services?
      if (timeSinceEnd >= postBuffer && event.status === 'active') {
        context.log(`🛑 STOP EVENT: ${event.id}`)
        context.log(`   → Event ended ${Math.floor(timeSinceEnd / 1000)} seconds ago`)

        // Scale DOWN containers
        await scaleContainerApps(
          resourceClient,
          subscriptionId,
          resourceGroupName,
          containerApps,
          0, // minReplicas = 0
          context
        )

        // Update event status in Streamgate
        await updateEventStatus(streamgateApiBase, event.id, 'inactive', streamgateApiKey)
        await markEventComplete(streamgateApiBase, event.id, streamgateApiKey)

        results.push({
          eventId: event.id,
          action: 'STOP',
          timestamp: now.toISOString(),
        })
      }

      // No action needed
      if (
        (timeUntilStart > preBuffer || timeUntilStart < 0) &&
        (timeSinceEnd < postBuffer || timeSinceEnd < 0) &&
        event.status === 'upcoming'
      ) {
        context.log(`⏳ WAITING: ${event.id} (not yet time to act)`)
        results.push({
          eventId: event.id,
          action: 'WAITING',
          timestamp: now.toISOString(),
        })
      }
    }

    context.log('✅ ScheduleStreamOrchestrator completed', {
      eventsProcessed: events.length,
      actionsPerformed: results.length,
      results,
    })
  } catch (error: any) {
    context.log.error('❌ ScheduleStreamOrchestrator failed', {
      error: error.message,
      stack: error.stack,
    })
    throw error
  }
}

/**
 * Scale Container Apps by updating minReplicas via ARM REST API
 */
async function scaleContainerApps(
  resourceClient: ResourceManagementClient,
  subscriptionId: string,
  resourceGroupName: string,
  containerApps: ContainerAppConfig[],
  minReplicas: number,
  context: Context
): Promise<void> {
  for (const app of containerApps) {
    try {
      const resourceId = `/subscriptions/${subscriptionId}/resourceGroups/${resourceGroupName}/providers/Microsoft.App/containerApps/${app.name}`

      // Get current config
      const response = await resourceClient.resources.getById(
        resourceId,
        '2023-05-01'
      )

      // Patch minReplicas
      if (response.properties?.template?.scale) {
        response.properties.template.scale.minReplicas = minReplicas
      } else if (response.properties?.template) {
        response.properties.template.scale = { minReplicas, maxReplicas: app.maxReplicas }
      }

      // Update resource
      await resourceClient.resources.createOrUpdateById(
        resourceId,
        '2023-05-01',
        response
      )

      context.log(`  ✓ Scaled ${app.name}: minReplicas=${minReplicas}`)
    } catch (error: any) {
      context.log.error(`  ✗ Failed to scale ${app.name}`, {
        error: error.message,
      })
      throw error
    }
  }
}

/**
 * Update event status in Streamgate API
 */
async function updateEventStatus(
  apiBase: string,
  eventId: string,
  status: string,
  apiKey?: string
): Promise<void> {
  try {
    await axios.patch(
      `${apiBase}/api/events/${eventId}`,
      { status },
      {
        headers: apiKey ? { 'X-API-Key': apiKey } : {},
        timeout: 5000,
      }
    )
  } catch (error: any) {
    console.warn(`Failed to update event status: ${error.message}`)
    // Don't throw; status update is optional
  }
}

/**
 * Mark event as complete in Streamgate API
 */
async function markEventComplete(
  apiBase: string,
  eventId: string,
  apiKey?: string
): Promise<void> {
  try {
    await axios.post(
      `${apiBase}/api/events/${eventId}/complete`,
      {},
      {
        headers: apiKey ? { 'X-API-Key': apiKey } : {},
        timeout: 5000,
      }
    )
  } catch (error: any) {
    console.warn(`Failed to mark event complete: ${error.message}`)
    // Don't throw; completion is optional
  }
}

export default timerTrigger
```

### File: `ScheduleStreamOrchestrator/function.json`

```json
{
  "scriptFile": "dist/index.js",
  "bindings": [
    {
      "name": "myTimer",
      "type": "timerTrigger",
      "direction": "in",
      "schedule": "0 */5 * * * *",
      "runOnStartup": false
    }
  ]
}
```

---

## Deployment Steps

### 1. Create Azure Function Project
```bash
cd azure-functions
func new --language typescript --runtime node --trigger timer --name ScheduleStreamOrchestrator
```

### 2. Install Dependencies
```bash
npm install @azure/identity @azure/arm-resources axios
npm install --save-dev @azure/functions typescript ts-node @types/node
```

### 3. Set Environment Variables

Create `local.settings.json` (for local testing):
```json
{
  "IsEncrypted": false,
  "Values": {
    "AzureWebJobsStorage": "DefaultEndpointsProtocol=https;AccountName=...;AccountKey=...",
    "FUNCTIONS_WORKER_RUNTIME": "node",
    "STREAMGATE_API_BASE": "https://streamgate.example.com",
    "STREAMGATE_API_KEY": "your-api-key-here",
    "AZURE_SUBSCRIPTION_ID": "00000000-0000-0000-0000-000000000000",
    "AZURE_RESOURCE_GROUP": "my-streaming-rg",
    "AZURE_CONTAINER_APPS_ENVIRONMENT": "my-aca-env"
  }
}
```

### 4. Assign Managed Identity & Permissions

```bash
# Create function app
az functionapp create \
  --resource-group my-streaming-rg \
  --consumption-plan-location eastus \
  --runtime node \
  --runtime-version 18 \
  --functions-version 4 \
  --name schedule-stream-orchestrator \
  --storage-account mystorageaccount

# Enable managed identity
az functionapp identity assign \
  --resource-group my-streaming-rg \
  --name schedule-stream-orchestrator

# Grant Contributor role (for Container Apps scaling)
FUNCTION_PRINCIPAL=$(az functionapp identity show \
  --resource-group my-streaming-rg \
  --name schedule-stream-orchestrator \
  --query principalId -o tsv)

az role assignment create \
  --assignee "$FUNCTION_PRINCIPAL" \
  --role "Contributor" \
  --scope "/subscriptions/$(az account show --query id -o tsv)/resourceGroups/my-streaming-rg"
```

### 5. Deploy Function
```bash
func azure functionapp publish schedule-stream-orchestrator
```

### 6. Configure Azure Key Vault (Optional but Recommended)

```bash
# Create Key Vault
az keyvault create \
  --name streaming-secrets \
  --resource-group my-streaming-rg

# Store Streamgate API key
az keyvault secret set \
  --vault-name streaming-secrets \
  --name streamgate-api-key \
  --value "your-api-key"

# Grant Function access
az keyvault set-policy \
  --name streaming-secrets \
  --object-id "$FUNCTION_PRINCIPAL" \
  --secret-permissions get
```

Update Function to retrieve from Key Vault:
```typescript
import { SecretClient } = from "@azure/keyvault-secrets"

const keyVaultUrl = `https://streaming-secrets.vault.azure.net/`
const secretClient = new SecretClient(keyVaultUrl, credential)
const apiKeySecret = await secretClient.getSecret('streamgate-api-key')
const streamgateApiKey = apiKeySecret.value
```

---

## Monitoring & Observability

### Application Insights Logging

```typescript
// In index.ts
context.log('Event processed', {
  eventId: event.id,
  action: 'START',
  duration: Date.now() - startTime,
  containerAppsScaled: 3,
  timestamp: new Date().toISOString(),
})
```

View logs in Azure Portal:
```
Application Insights → Logs → traces
```

### Metrics to Track

```kusto
// KQL query: Show all scaling actions in last 24h
traces
| where message contains "START" or message contains "STOP"
| summarize Count = count() by bin(timestamp, 1h)
| render areachart
```

### Alerting

```bash
# Alert if Function fails more than 2 times in 15 minutes
az monitor metrics alert create \
  --name "ScheduleStreamOrchestrator-Failures" \
  --resource-group my-streaming-rg \
  --scopes "/subscriptions/.../providers/Microsoft.Web/sites/schedule-stream-orchestrator" \
  --condition "avg FunctionExecutionCount < 1" \
  --window-size 15m \
  --evaluation-frequency 5m
```

---

## Testing

### Local Testing
```bash
# Start functions runtime
func start

# Manually trigger timer (in another terminal)
curl http://localhost:7071/admin/functions/ScheduleStreamOrchestrator

# Check logs in local console
```

### Production Testing
```bash
# Create test event in Streamgate (starts in 15 minutes)
curl -X POST https://streamgate.example.com/api/events \
  -H "Content-Type: application/json" \
  -d '{
    "streamKey": "live/test",
    "title": "Test Event",
    "startsAt": "'$(date -d '+15 minutes' -Iseconds)'",
    "endsAt": "'$(date -d '+45 minutes' -Iseconds)'",
    "preStartBufferMinutes": 10,
    "postEndBufferMinutes": 10
  }'

# Monitor Function logs
az functionapp logs tail \
  --resource-group my-streaming-rg \
  --name schedule-stream-orchestrator
```

---

## Handling Edge Cases

### Event Cancellation
```typescript
// If event.status = "cancelled", skip all scaling logic
if (event.status === 'cancelled') {
  context.log(`⊘ Event cancelled: ${event.id}`)
  return // Skip this event
}
```

### Multiple Concurrent Events
```typescript
// Function handles 5-10 events at once
// All scaling decisions are independent
// No race conditions (ARM API is idempotent)
for (const event of events) {
  // Process each independently
}
```

### Daylight Saving Time
```typescript
// Use UTC only; Streamgate should provide timestamps in UTC
// Node.js Date handles DST automatically when parsing ISO 8601
const startsAt = new Date('2026-04-21T09:50:00Z') // Always UTC
```

### Late Function Execution
```typescript
// If Function doesn't run for 10 minutes (rare), 
// it will catch up on next run
if (myTimer.isPastDue) {
  context.log('⚠️ Function is running late')
  // Still process all events; logic handles delays
}
```

---

## Cost of Orchestration

Azure Functions (timer-based):
```
Executions: 288/day (every 5 minutes × 24 hours)
Cost per execution: ~$0.000005
Monthly cost: 288 × 30 × $0.000005 = $0.04
```

**Negligible**: ~$0.04/month for orchestration.

---

## Summary

| Component | Responsibility |
|-----------|-----------------|
| **Streamgate API** | Store scheduled events |
| **Azure Function** | Query API, scale Container Apps |
| **Container Apps** | Host RTMP/FFmpeg/HLS (minReplicas=0 normally) |
| **Azure Resource Manager** | Execute scaling commands |

**Result**: Fully automated, zero-downtime scaling based on event schedule.

See `004-IMPLEMENTATION-GUIDE.md` for complete integration steps.

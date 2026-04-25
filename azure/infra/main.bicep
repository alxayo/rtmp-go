// ============================================================================
// Azure Container Apps Infrastructure for rtmp-go
// ============================================================================
// Deploys:
//   - Log Analytics Workspace
//   - Container Apps Environment
//   - Azure Container Registry (Basic SKU)
//   - Storage Account + blob container for recordings
//   - User Managed Identity (AcrPull + Storage Blob Data Contributor)
//   - Container App 1: rtmp-server (TCP ingress on 1935)
//   - Container App 2: blob-sidecar (internal HTTP ingress on 8080 for webhooks)
//   - Container App 3: hls-transcoder (multi-container: FFmpeg transcoder + blob-sidecar co-located)
//   - Container App 4: hls-blob-sidecar (SCALED TO ZERO — kept for rollback, replaced by co-located sidecar)
//
// Phase 4 (Co-located Sidecar — current):
//   - blob-sidecar runs as a second container inside hls-transcoder Container App
//   - FFmpeg uploads segments to localhost:8081 (zero Envoy proxy involvement)
//   - Eliminates Azure Container Apps HTTP/2 CONNECT tunnel bug (envoyproxy/envoy#28329)
//     that caused ~23% segment drop rate when using cross-app HTTP PUT
//   - The standalone hls-blob-sidecar app is scaled to 0 (kept for rollback)
//
// Usage:
//   az deployment group create -g <rg> -f main.bicep -p main.parameters.json
// ============================================================================

targetScope = 'resourceGroup'

// ---------- Parameters ----------

@description('Base name used for generating unique resource names')
param environmentName string

@description('Azure region for all resources')
param location string = resourceGroup().location

@description('RTMP auth token in format streamKey=secret (e.g. live/stream=mysecret123)')
@secure()
param rtmpAuthToken string

@description('RTMP auth callback URL for delegated authentication (e.g. https://platform.example.com/api/rtmp/auth). When set, overrides token-based auth.')
param rtmpAuthCallbackUrl string = ''

@description('Container image for rtmp-server (set after ACR build)')
param rtmpServerImage string = ''

@description('Container image for blob-sidecar (set after ACR build)')
param blobSidecarImage string = ''

@description('Container image for hls-transcoder (set after ACR build)')
param hlsTranscoderImage string = ''

@description('Bearer token for HTTP ingest authentication (enables PUT /upload/{path} endpoint on blob-sidecar:8081)')
@secure()
param ingestToken string = 'dev-ingest-token-change-in-production'

@description('Maximum upload size for HTTP ingest in bytes (default 50MB for Phase 3)')
param ingestMaxBodyBytes int = 52428800

// ---------- Variables ----------

var resourceToken = uniqueString(subscription().id, resourceGroup().id, location, environmentName)

// Resource names: az{prefix}{token} pattern per IaC rules
var logAnalyticsName = 'azlog${resourceToken}'
var containerEnvName = 'azenv${resourceToken}'
var registryName = 'azacr${resourceToken}'
var storageAccountName = 'azst${resourceToken}'
var identityName = 'azid${resourceToken}'
var rtmpAppName = 'rtmp-server-${resourceToken}'
var sidecarAppName = 'rec-blob-sidecar-${resourceToken}'
var hlsAppName = 'hls-transcoder-${resourceToken}'
var hlsSidecarAppName = 'hls-blob-sidecar-${resourceToken}'
var blobContainerName = 'recordings'
var hlsBlobContainerName = 'hls-content'
var vnetName = 'azvnet${resourceToken}'
var subnetName = 'containerapps'

// Tenant config for the blob-sidecar (uses managed identity to access blob storage)
#disable-next-line secure-secrets-in-params
var tenantsJsonValue = '{"tenants":{"live":{"storage_account":"https://${storageAccountName}.blob.core.windows.net","container":"recordings","credential":"managed-identity"}},"default":{"storage_account":"https://${storageAccountName}.blob.core.windows.net","container":"recordings","credential":"managed-identity"},"api_fallback":{"enabled":false}}'

// Tenant config for the HLS blob-sidecar — routes "hls/*" stream keys to hls-content container
#disable-next-line secure-secrets-in-params
var hlsTenantsJsonValue = '{"tenants":{"hls":{"storage_account":"https://${storageAccountName}.blob.core.windows.net","container":"hls-content","credential":"managed-identity"}},"default":{"storage_account":"https://${storageAccountName}.blob.core.windows.net","container":"hls-content","credential":"managed-identity"},"api_fallback":{"enabled":false}}'

// ---------- Virtual Network (required for TCP ingress) ----------

resource vnet 'Microsoft.Network/virtualNetworks@2024-01-01' = {
  name: vnetName
  location: location
  properties: {
    addressSpace: {
      addressPrefixes: [
        '10.0.0.0/16'
      ]
    }
    subnets: [
      {
        name: subnetName
        properties: {
          addressPrefix: '10.0.0.0/23'
          delegations: [
            {
              name: 'Microsoft.App.environments'
              properties: {
                serviceName: 'Microsoft.App/environments'
              }
            }
          ]
        }
      }
    ]
  }
}

// ---------- Log Analytics Workspace ----------

resource logAnalytics 'Microsoft.OperationalInsights/workspaces@2023-09-01' = {
  name: logAnalyticsName
  location: location
  properties: {
    sku: {
      name: 'PerGB2018'
    }
    retentionInDays: 30
  }
}

// ---------- Container Apps Environment ----------

resource containerEnv 'Microsoft.App/managedEnvironments@2024-03-01' = {
  name: containerEnvName
  location: location
  properties: {
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsConfiguration: {
        customerId: logAnalytics.properties.customerId
        sharedKey: logAnalytics.listKeys().primarySharedKey
      }
    }
    vnetConfiguration: {
      infrastructureSubnetId: vnet.properties.subnets[0].id
      internal: false
    }
  }
}

// Shared ephemeral storage for recordings volume
resource recordingsStorage 'Microsoft.App/managedEnvironments/storages@2024-03-01' = {
  name: 'recordings'
  parent: containerEnv
  properties: {
    azureFile: {
      accountName: storageAccount.name
      accountKey: storageAccount.listKeys().keys[0].value
      shareName: fileShare.name
      accessMode: 'ReadWrite'
    }
  }
}

// HLS output storage for hls-transcoder
resource hlsStorage 'Microsoft.App/managedEnvironments/storages@2024-03-01' = {
  name: 'hls-output'
  parent: containerEnv
  properties: {
    azureFile: {
      accountName: storageAccount.name
      accountKey: storageAccount.listKeys().keys[0].value
      shareName: hlsFileShare.name
      accessMode: 'ReadWrite'
    }
  }
}

// ---------- Azure Container Registry ----------

resource registry 'Microsoft.ContainerRegistry/registries@2023-11-01-preview' = {
  name: registryName
  location: location
  sku: {
    name: 'Basic'
  }
  properties: {
    adminUserEnabled: false
    // Anonymous pull disabled per security best practices
    anonymousPullEnabled: false
  }
}

// ---------- Storage Account + Blob Container ----------

resource storageAccount 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  name: storageAccountName
  location: location
  kind: 'StorageV2'
  sku: {
    name: 'Standard_LRS'
  }
  properties: {
    minimumTlsVersion: 'TLS1_2'
    supportsHttpsTrafficOnly: true
    allowBlobPublicAccess: false
    // Key access needed for Azure Files mount; blob access via managed identity
    allowSharedKeyAccess: true
  }
}

resource blobService 'Microsoft.Storage/storageAccounts/blobServices@2023-05-01' = {
  name: 'default'
  parent: storageAccount
}

resource blobContainer 'Microsoft.Storage/storageAccounts/blobServices/containers@2023-05-01' = {
  name: blobContainerName
  parent: blobService
  properties: {
    publicAccess: 'None'
  }
}

// Blob container for HLS segments and playlists (private — accessed via SAS token)
resource hlsBlobContainer 'Microsoft.Storage/storageAccounts/blobServices/containers@2023-05-01' = {
  name: hlsBlobContainerName
  parent: blobService
  properties: {
    publicAccess: 'None'
  }
}

// Azure Files share for shared volume between containers
resource fileService 'Microsoft.Storage/storageAccounts/fileServices@2023-05-01' = {
  name: 'default'
  parent: storageAccount
}

resource fileShare 'Microsoft.Storage/storageAccounts/fileServices/shares@2023-05-01' = {
  name: 'recordings'
  parent: fileService
  properties: {
    shareQuota: 10 // 10 GiB quota
  }
}

// Azure Files share for HLS output (shared between hls-transcoder and future HLS server)
// PHASE 3 NOTE: This mount is kept for rollback safety and compatibility with older deployments.
// New Phase 3 deployments using OUTPUT_MODE=http don't require this mount.
// hls-transcoder can switch back to file mode by setting OUTPUT_MODE=file (requires Azure Files).
resource hlsFileShare 'Microsoft.Storage/storageAccounts/fileServices/shares@2023-05-01' = {
  name: 'hls-output'
  parent: fileService
  properties: {
    shareQuota: 50 // 50 GiB quota — 3 renditions × 2s segments at ~8 Mbps combined
  }
}

// ---------- User Managed Identity ----------

resource identity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: identityName
  location: location
}

// AcrPull role: allows pulling container images from ACR
resource acrPullRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(registry.id, identity.id, '7f951dda-4ed3-4680-a7ca-43fe172d538d')
  scope: registry
  properties: {
    principalId: identity.properties.principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', '7f951dda-4ed3-4680-a7ca-43fe172d538d')
  }
}

// Storage Blob Data Contributor: allows sidecar to upload blobs
resource storageBlobRole 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(storageAccount.id, identity.id, 'ba92f5b4-2d11-453d-a403-e96b0029c9fe')
  scope: storageAccount
  properties: {
    principalId: identity.properties.principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'ba92f5b4-2d11-453d-a403-e96b0029c9fe')
  }
}

// ---------- Container App: rtmp-server ----------

resource rtmpApp 'Microsoft.App/containerApps@2024-03-01' = {
  name: rtmpAppName
  location: location
  tags: {
    role: 'rtmp-server'
    component: 'rtmp-go'
  }
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${identity.id}': {}
    }
  }
  properties: {
    managedEnvironmentId: containerEnv.id
    configuration: {
      // ACR pull using managed identity
      registries: [
        {
          server: registry.properties.loginServer
          identity: identity.id
        }
      ]
      // TCP ingress on port 1935 for RTMP traffic
      ingress: {
        external: true
        targetPort: 1935
        transport: 'tcp'
        exposedPort: 1935
      }
      secrets: [
        {
          name: 'rtmp-auth-token'
          value: rtmpAuthToken
        }
      ]
    }
    template: {
      containers: [
        {
          name: 'rtmp-server'
          image: !empty(rtmpServerImage) ? rtmpServerImage : 'mcr.microsoft.com/azuredocs/containerapps-helloworld:latest'
          resources: {
            cpu: json('0.5')
            memory: '1Gi'
          }
          command: !empty(rtmpServerImage) ? concat([
            '/rtmp-server'
            '-listen'
            ':1935'
          ], !empty(rtmpAuthCallbackUrl) ? [
            '-auth-mode'
            'callback'
            '-auth-callback'
            rtmpAuthCallbackUrl
          ] : [
            '-auth-mode'
            'token'
            '-auth-token'
            rtmpAuthToken
          ], [
            '-record-all'
            'true'
            '-record-dir'
            '/recordings'
            '-segment-duration'
            '2m'
            '-hook-webhook'
            'segment_complete=http://${sidecarAppName}.internal.${containerEnv.properties.defaultDomain}/events'
            '-hook-webhook'
            'recording_start=http://${sidecarAppName}.internal.${containerEnv.properties.defaultDomain}/events'
            '-hook-webhook'
            'recording_stop=http://${sidecarAppName}.internal.${containerEnv.properties.defaultDomain}/events'
            '-hook-webhook'
            'publish_start=http://${hlsAppName}.internal.${containerEnv.properties.defaultDomain}/events'
            '-hook-webhook'
            'publish_stop=http://${hlsAppName}.internal.${containerEnv.properties.defaultDomain}/events'
            '-log-level'
            'info'
          ]) : []
          volumeMounts: [
            {
              volumeName: 'recordings'
              mountPath: '/recordings'
            }
          ]
        }
      ]
      volumes: [
        {
          name: 'recordings'
          storageName: recordingsStorage.name
          storageType: 'AzureFile'
        }
      ]
      scale: {
        minReplicas: 1
        maxReplicas: 1
      }
    }
  }
  dependsOn: [
    acrPullRole
  ]
}

// ---------- Container App: blob-sidecar ----------

resource sidecarApp 'Microsoft.App/containerApps@2024-03-01' = {
  name: sidecarAppName
  location: location
  tags: {
    role: 'rec-blob-sidecar'
    component: 'rtmp-go'
  }
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${identity.id}': {}
    }
  }
  properties: {
    managedEnvironmentId: containerEnv.id
    configuration: {
      registries: [
        {
          server: registry.properties.loginServer
          identity: identity.id
        }
      ]
      // Internal-only HTTP ingress for HLS segment ingest (port 8081)
      // FFmpeg sends HLS segments via HTTP PUT to the sidecar's ingest endpoint
      // IMPORTANT: transport MUST be 'http' — 'tcp' breaks Container Apps internal routing
      ingress: {
        external: false
        targetPort: 8081
        transport: 'http'
        allowInsecure: true // Required: internal services communicate over plain HTTP
      }
      secrets: [
        {
          name: 'tenants-json'
          #disable-next-line use-secure-value-for-secure-inputs
          value: tenantsJsonValue
        }
        {
          name: 'ingest-token'
          value: ingestToken
        }
      ]
    }
    template: {
      containers: [
        {
          name: 'blob-sidecar'
          image: !empty(blobSidecarImage) ? blobSidecarImage : 'mcr.microsoft.com/azuredocs/containerapps-helloworld:latest'
          resources: {
            cpu: json('0.25')
            memory: '0.5Gi'
          }
          command: !empty(blobSidecarImage) ? [
            '/blob-sidecar'
            '-mode'
            'webhook'
            '-listen-addr'
            ':8080'
            '-config'
            '/config/tenants-json'
            '-workers'
            '4'
            '-cleanup'
            'true'
            // Phase 3 HTTP Ingest: CLI flags for direct segment uploads
            '-ingest-addr'
            ':8081'
            '-ingest-storage'
            'blob'
            '-ingest-token'
            ingestToken
            '-ingest-max-body'
            string(ingestMaxBodyBytes)
            '-log-level'
            'info'
          ] : []
          env: [
            {
              name: 'AZURE_CLIENT_ID'
              value: identity.properties.clientId
            }
          ]
          volumeMounts: [
            {
              volumeName: 'recordings'
              mountPath: '/recordings'
            }
            {
              volumeName: 'sidecar-config'
              mountPath: '/config'
            }
          ]
          // Phase 3: Health probe for HTTP ingest endpoint
          // Checks that blob-sidecar is responding on port 8081 /health
          // Required for Azure Container Apps to track readiness and restart unhealthy instances
          probes: [
            {
              type: 'Startup'
              httpGet: {
                path: '/health'
                port: 8081
              }
              initialDelaySeconds: 5
              periodSeconds: 10
            }
            {
              type: 'Liveness'
              httpGet: {
                path: '/health'
                port: 8081
              }
              initialDelaySeconds: 10
              periodSeconds: 30
            }
          ]
        }
      ]
      volumes: [
        {
          name: 'recordings'
          storageName: recordingsStorage.name
          storageType: 'AzureFile'
        }
        {
          name: 'sidecar-config'
          storageType: 'Secret'
        }
      ]
      scale: {
        minReplicas: 1
        maxReplicas: 1
      }
    }
  }
  dependsOn: [
    acrPullRole
    storageBlobRole
  ]
}

// ---------- Container App: hls-transcoder ----------
// Multi-container app: FFmpeg transcoder + blob-sidecar co-located.
// Phase 4: blob-sidecar runs as a second container inside this app, reachable
// at localhost:8081. FFmpeg PUTs segments directly to localhost — zero Envoy
// proxy involvement. This eliminates the HTTP/2 CONNECT tunnel RST_STREAM bug
// (envoyproxy/envoy#28329) that caused ~23% segment drops in Phase 3.
//
// Container 1: hls-transcoder (3.5 vCPU / 7 GiB) — FFmpeg ABR transcoding
// Container 2: blob-sidecar (0.5 vCPU / 1 GiB) — buffers + uploads to Blob Storage
// Total: 4 vCPU / 8 GiB (Container Apps Consumption plan maximum per app)

resource hlsApp 'Microsoft.App/containerApps@2024-03-01' = {
  name: hlsAppName
  location: location
  tags: {
    role: 'hls-transcoder'
    component: 'rtmp-go'
  }
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${identity.id}': {}
    }
  }
  properties: {
    managedEnvironmentId: containerEnv.id
    configuration: {
      registries: [
        {
          server: registry.properties.loginServer
          identity: identity.id
        }
      ]
      // Internal-only HTTP ingress for receiving webhook events from rtmp-server
      ingress: {
        external: false
        targetPort: 8090
        transport: 'http'
        allowInsecure: true // Required: rtmp-server sends webhooks over plain HTTP inside VNet
      }
      secrets: [
        {
          name: 'rtmp-auth-token'
          value: rtmpAuthToken
        }
        {
          name: 'ingest-token'
          value: ingestToken
        }
        {
          name: 'hls-tenants-json'
          #disable-next-line use-secure-value-for-secure-inputs
          value: hlsTenantsJsonValue
        }
      ]
    }
    template: {
      containers: [
        // Container 1: HLS Transcoder (FFmpeg)
        {
          name: 'hls-transcoder'
          image: !empty(hlsTranscoderImage) ? hlsTranscoderImage : 'mcr.microsoft.com/azuredocs/containerapps-helloworld:latest'
          resources: {
            // ABR transcoding: 1080p copy + 2 ultrafast encodes (720p/480p)
            // 3.5 vCPU / 7 GiB — leaves 0.5 vCPU / 1 GiB for the co-located blob-sidecar
            cpu: json('3.5')
            memory: '7Gi'
          }
          command: !empty(hlsTranscoderImage) ? [
            '/hls-transcoder'
            '-listen-addr'
            ':8090'
            '-hls-dir'
            '/hls-output'
            '-rtmp-host'
            '${rtmpAppName}.${containerEnv.properties.defaultDomain}'
            '-rtmp-port'
            '1935'
            '-rtmp-token'
            last(split(rtmpAuthToken, '='))
            '-mode'
            'abr'
            // Phase 4: HTTP output to co-located blob-sidecar via localhost
            // No Envoy proxy, no HTTP/2 CONNECT tunnel — direct localhost TCP
            '-output-mode'
            'http'
            '-ingest-url'
            'http://localhost:8081/ingest/'
            '-ingest-token'
            ingestToken
            '-log-level'
            'info'
          ] : []
          volumeMounts: [
            {
              volumeName: 'hls-output'
              mountPath: '/hls-output'
            }
          ]
        }
        // Container 2: Blob Sidecar (co-located, reachable via localhost:8081)
        {
          name: 'blob-sidecar'
          image: !empty(blobSidecarImage) ? blobSidecarImage : 'mcr.microsoft.com/azuredocs/containerapps-helloworld:latest'
          resources: {
            cpu: json('0.5')
            memory: '1Gi'
          }
          command: !empty(blobSidecarImage) ? [
            '/blob-sidecar'
            '-mode'
            'webhook'
            '-listen-addr'
            ':8080'
            '-config'
            '/config/hls-tenants-json'
            '-workers'
            '4'
            '-cleanup'
            'false'
            '-ingest-addr'
            ':8081'
            '-ingest-storage'
            'blob'
            '-ingest-token'
            ingestToken
            '-ingest-max-body'
            string(ingestMaxBodyBytes)
            '-log-level'
            'info'
          ] : []
          env: [
            {
              name: 'AZURE_CLIENT_ID'
              value: identity.properties.clientId
            }
          ]
          probes: [
            {
              type: 'Startup'
              httpGet: {
                path: '/health'
                port: 8081
              }
              initialDelaySeconds: 5
              periodSeconds: 10
            }
            {
              type: 'Liveness'
              httpGet: {
                path: '/health'
                port: 8081
              }
              initialDelaySeconds: 10
              periodSeconds: 30
            }
          ]
          volumeMounts: [
            {
              volumeName: 'sidecar-config'
              mountPath: '/config'
            }
          ]
        }
      ]
      volumes: [
        {
          // EmptyDir volume — HTTP mode doesn't write segments to disk, but the
          // -hls-dir flag still needs a valid path for FFmpeg's internal state.
          name: 'hls-output'
          storageType: 'EmptyDir'
        }
        {
          name: 'sidecar-config'
          storageType: 'Secret'
        }
      ]
      scale: {
        minReplicas: 1
        maxReplicas: 1
      }
    }
  }
  dependsOn: [
    acrPullRole
    storageBlobRole
  ]
}

// ---------- Container App: hls-blob-sidecar ----------
// DEPRECATED (Phase 4): This standalone sidecar is scaled to 0.
// The blob-sidecar now runs co-located inside the hls-transcoder Container App
// (localhost:8081) to avoid the Envoy HTTP/2 CONNECT tunnel RST_STREAM bug
// (envoyproxy/envoy#28329). This resource is kept for rollback to Phase 3.
//
// To rollback: set minReplicas=1 here and change hls-transcoder's -ingest-url
// back to 'http://${hlsSidecarAppName}.internal.${domain}/ingest/'

resource hlsSidecarApp 'Microsoft.App/containerApps@2024-03-01' = {
  name: hlsSidecarAppName
  location: location
  tags: {
    role: 'hls-blob-sidecar'
    component: 'rtmp-go'
  }
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${identity.id}': {}
    }
  }
  properties: {
    managedEnvironmentId: containerEnv.id
    configuration: {
      registries: [
        {
          server: registry.properties.loginServer
          identity: identity.id
        }
      ]
      // Internal-only HTTP ingress routed to the ingest port (8081)
      // CRITICAL: targetPort must be 8081 (not 8080) so the FQDN routes to the ingest server.
      // The webhook listener on :8080 is only used in file-mode rollback.
      // CRITICAL: allowInsecure must be true — FFmpeg sends HTTP PUT (not HTTPS).
      // Without this, all segment uploads are silently rejected/redirected.
      ingress: {
        external: false
        targetPort: 8081
        transport: 'http'
        allowInsecure: true // REQUIRED: FFmpeg uses http:// for PUT uploads
      }
      secrets: [
        {
          name: 'hls-tenants-json'
          #disable-next-line use-secure-value-for-secure-inputs
          value: hlsTenantsJsonValue
        }
        {
          name: 'ingest-token'
          value: ingestToken
        }
      ]
    }
    template: {
      containers: [
        {
          name: 'hls-blob-sidecar'
          image: !empty(blobSidecarImage) ? blobSidecarImage : 'mcr.microsoft.com/azuredocs/containerapps-helloworld:latest'
          resources: {
            // 1 vCPU / 2 GiB — buffers entire segment bodies in memory before uploading to Azure Blob.
            // FFmpeg uses chunked transfer encoding (no Content-Length), so each segment
            // is fully read into RAM, then uploaded with a seekable reader for retry support.
            cpu: json('1')
            memory: '2Gi'
          }
          command: !empty(blobSidecarImage) ? [
            '/blob-sidecar'
            '-mode'
            'webhook'
            '-listen-addr'
            ':8080'
            '-config'
            '/config/hls-tenants-json'
            '-workers'
            '4'
            '-cleanup'
            'false'
            // Phase 3 HTTP Ingest: CLI flags for direct segment uploads from hls-transcoder
            '-ingest-addr'
            ':8081'
            '-ingest-storage'
            'blob'
            '-ingest-token'
            ingestToken
            '-ingest-max-body'
            string(ingestMaxBodyBytes)
            '-log-level'
            'info'
          ] : []
          env: [
            {
              name: 'AZURE_CLIENT_ID'
              value: identity.properties.clientId
            }
          ]
          volumeMounts: [
            {
              volumeName: 'hls-output'
              mountPath: '/hls-output'
            }
            {
              volumeName: 'sidecar-config'
              mountPath: '/config'
            }
          ]
          // Phase 3: Health probes for HTTP ingest endpoint on port 8081
          probes: [
            {
              type: 'Startup'
              httpGet: {
                path: '/health'
                port: 8081
              }
              initialDelaySeconds: 5
              periodSeconds: 10
            }
            {
              type: 'Liveness'
              httpGet: {
                path: '/health'
                port: 8081
              }
              initialDelaySeconds: 10
              periodSeconds: 30
            }
          ]
        }
      ]
      volumes: [
        {
          name: 'hls-output'
          storageName: hlsStorage.name
          storageType: 'AzureFile'
        }
        {
          name: 'sidecar-config'
          storageType: 'Secret'
        }
      ]
      scale: {
        minReplicas: 0 // Phase 4: scaled to zero — blob-sidecar co-located in hls-transcoder
        maxReplicas: 1
      }
    }
  }
  dependsOn: [
    acrPullRole
    storageBlobRole
  ]
}

// ---------- Outputs ----------

output registryLoginServer string = registry.properties.loginServer
output registryName string = registry.name
output rtmpAppName string = rtmpApp.name
output sidecarAppName string = sidecarApp.name
output hlsAppName string = hlsApp.name
output hlsSidecarAppName string = hlsSidecarApp.name
output rtmpAppFqdn string = rtmpApp.properties.configuration.ingress.fqdn
output storageAccountName string = storageAccount.name
output identityClientId string = identity.properties.clientId
output identityName string = identity.name
output resourceGroupName string = resourceGroup().name
output environmentName string = containerEnv.name

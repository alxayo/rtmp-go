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
//   - Container App 2: blob-sidecar (internal HTTP ingress on 8080 for webhooks, 8081 for HTTP ingest)
//   - Container App 3: hls-transcoder (internal HTTP ingress on 8090, outputs to blob-sidecar via HTTP ingest on 8081)
//   - Container App 4: hls-blob-sidecar (internal HTTP ingress, uploads HLS segments to Blob Storage)
//
// Phase 3 (HTTP Ingest):
//   - blob-sidecar exposes new /upload HTTP PUT endpoint on port 8081
//   - hls-transcoder configured for OUTPUT_MODE=http, sends segments to blob-sidecar:8081 via HTTP
//   - Internal DNS allows hls-transcoder to reach blob-sidecar by name (e.g., rec-blob-sidecar-{token}.internal.{domain}:8081)
//   - No shared Azure Files mount needed for Phase 3 deployments (kept for rollback safety)
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
      // Internal-only HTTP ingress for receiving webhook events from rtmp-server
      ingress: {
        external: false
        targetPort: 8080
        transport: 'http'
        allowInsecure: true // Required: rtmp-server sends webhooks over plain HTTP
      }
      // Phase 3: Expose port 8081 for HTTP ingest endpoint on blob-sidecar
      // This enables FFmpeg and other clients to upload segments directly via HTTP PUT
      // Port 8080: Webhooks from RTMP server (publish_start, publish_stop events)
      // Port 8081: HTTP PUT /upload/{path} for direct segment uploads (Phase 3 HTTP ingest)
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
// Converts live RTMP streams to multi-bitrate adaptive HLS via FFmpeg.
// Receives publish_start/publish_stop webhooks from rtmp-server and manages
// FFmpeg process lifecycles.
// Phase 3: HTTP output mode — FFmpeg PUTs segments directly to hls-blob-sidecar:8081,
// bypassing the Azure Files SMB mount. The hls-output volume is kept for rollback safety.
// In ABR mode: 4 vCPU / 8 GiB for 1080p copy + 2-rendition transcoding (720p/480p).
// In copy mode: 0.5 vCPU / 1 GiB for remux-only passthrough.

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
      ]
    }
    template: {
      containers: [
        {
          name: 'hls-transcoder'
          image: !empty(hlsTranscoderImage) ? hlsTranscoderImage : 'mcr.microsoft.com/azuredocs/containerapps-helloworld:latest'
          resources: {
            // ABR transcoding: 1080p copy + 2 ultrafast encodes (720p/480p)
            // 4 vCPU / 8 GiB per spec — ultrafast 720p+480p need ~1.5 vCPU combined,
            // plus overhead for demux/mux, HTTP PUT I/O, and FFmpeg process management.
            cpu: json('4')
            memory: '8Gi'
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
            // Phase 3: HTTP output mode — segments sent via HTTP PUT to hls-blob-sidecar:8081
            // Eliminates Azure Files SMB mount dependency between transcoder and sidecar
            '-output-mode'
            'http'
            '-ingest-url'
            'http://${hlsSidecarAppName}.internal.${containerEnv.properties.defaultDomain}:8081/ingest/'
            '-ingest-token'
            ingestToken
            // Blob webhook URL kept for file-mode rollback (unused when output-mode=http)
            '-blob-webhook-url'
            'http://${hlsSidecarAppName}.internal.${containerEnv.properties.defaultDomain}/events'
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
      ]
      volumes: [
        {
          name: 'hls-output'
          storageName: hlsStorage.name
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

// ---------- Container App: hls-blob-sidecar ----------
// Dedicated blob-sidecar instance for uploading HLS segments and playlists
// to Azure Blob Storage. Reuses the same blob-sidecar image but with:
//   - cleanup disabled (FFmpeg manages segment rotation on the Files share)
//   - HLS-specific tenant config routing to the hls-content blob container
//   - HTTP ingest on port 8081 for direct segment uploads from hls-transcoder (Phase 3)
//   - hls-output volume mounted for reading HLS files (file-mode rollback)

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
      // Internal-only HTTP ingress for receiving webhook events from hls-transcoder
      ingress: {
        external: false
        targetPort: 8080
        transport: 'http'
        allowInsecure: true
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

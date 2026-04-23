// ============================================================================
// Azure Monitoring Dashboard for rtmp-go + StreamGate
// ============================================================================
// Deploys a shared Azure Portal dashboard monitoring all Container Apps
// and Storage Account in rg-rtmpgo.
//
// Usage:
//   az deployment group create -g rg-rtmpgo -f dashboard.bicep \
//     -p rtmpServerApp=... recBlobSidecarApp=... (etc.)
// ============================================================================

targetScope = 'resourceGroup'

@description('Resource group containing all resources')
param resourceGroupName string = resourceGroup().name

@description('Azure region for the dashboard resource')
param location string = resourceGroup().location

param rtmpServerApp string
param recBlobSidecarApp string
param hlsTranscoderApp string
param hlsBlobSidecarApp string
param sgHlsServerApp string
param sgPlatformApp string
param storageAccountName string
param logAnalyticsName string
param containerEnvName string

// ---------- Variables ----------

var sub = subscription().subscriptionId
var rg = resourceGroupName
var base = '/subscriptions/${sub}/resourceGroups/${rg}/providers'

var app1 = '${base}/Microsoft.App/containerApps/${rtmpServerApp}'
var app2 = '${base}/Microsoft.App/containerApps/${recBlobSidecarApp}'
var app3 = '${base}/Microsoft.App/containerApps/${hlsTranscoderApp}'
var app4 = '${base}/Microsoft.App/containerApps/${hlsBlobSidecarApp}'
var app5 = '${base}/Microsoft.App/containerApps/${sgHlsServerApp}'
var app6 = '${base}/Microsoft.App/containerApps/${sgPlatformApp}'
var stor = '${base}/Microsoft.Storage/storageAccounts/${storageAccountName}'

var ns = 'Microsoft.App/containerApps'
var stoNs = 'Microsoft.Storage/storageAccounts'

// Pre-computed metrics arrays — Bicep does not allow for-expressions in nested properties
var cpuMetrics = [
  { resourceMetadata: { id: app1 }, name: 'UsageNanoCores', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'RTMP Server', resourceDisplayName: 'RTMP Server' } }
  { resourceMetadata: { id: app2 }, name: 'UsageNanoCores', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'Rec Blob Sidecar', resourceDisplayName: 'Rec Blob Sidecar' } }
  { resourceMetadata: { id: app3 }, name: 'UsageNanoCores', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'HLS Transcoder', resourceDisplayName: 'HLS Transcoder' } }
  { resourceMetadata: { id: app4 }, name: 'UsageNanoCores', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'HLS Blob Sidecar', resourceDisplayName: 'HLS Blob Sidecar' } }
  { resourceMetadata: { id: app5 }, name: 'UsageNanoCores', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'SG HLS Server', resourceDisplayName: 'SG HLS Server' } }
  { resourceMetadata: { id: app6 }, name: 'UsageNanoCores', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'SG Platform', resourceDisplayName: 'SG Platform' } }
]

var memMetrics = [
  { resourceMetadata: { id: app1 }, name: 'WorkingSetBytes', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'RTMP Server', resourceDisplayName: 'RTMP Server' } }
  { resourceMetadata: { id: app2 }, name: 'WorkingSetBytes', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'Rec Blob Sidecar', resourceDisplayName: 'Rec Blob Sidecar' } }
  { resourceMetadata: { id: app3 }, name: 'WorkingSetBytes', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'HLS Transcoder', resourceDisplayName: 'HLS Transcoder' } }
  { resourceMetadata: { id: app4 }, name: 'WorkingSetBytes', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'HLS Blob Sidecar', resourceDisplayName: 'HLS Blob Sidecar' } }
  { resourceMetadata: { id: app5 }, name: 'WorkingSetBytes', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'SG HLS Server', resourceDisplayName: 'SG HLS Server' } }
  { resourceMetadata: { id: app6 }, name: 'WorkingSetBytes', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'SG Platform', resourceDisplayName: 'SG Platform' } }
]

var txMetrics = [
  { resourceMetadata: { id: app1 }, name: 'TxBytes', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'RTMP Server', resourceDisplayName: 'RTMP Server' } }
  { resourceMetadata: { id: app2 }, name: 'TxBytes', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'Rec Blob Sidecar', resourceDisplayName: 'Rec Blob Sidecar' } }
  { resourceMetadata: { id: app3 }, name: 'TxBytes', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'HLS Transcoder', resourceDisplayName: 'HLS Transcoder' } }
  { resourceMetadata: { id: app4 }, name: 'TxBytes', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'HLS Blob Sidecar', resourceDisplayName: 'HLS Blob Sidecar' } }
  { resourceMetadata: { id: app5 }, name: 'TxBytes', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'SG HLS Server', resourceDisplayName: 'SG HLS Server' } }
  { resourceMetadata: { id: app6 }, name: 'TxBytes', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'SG Platform', resourceDisplayName: 'SG Platform' } }
]

var rxMetrics = [
  { resourceMetadata: { id: app1 }, name: 'RxBytes', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'RTMP Server', resourceDisplayName: 'RTMP Server' } }
  { resourceMetadata: { id: app2 }, name: 'RxBytes', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'Rec Blob Sidecar', resourceDisplayName: 'Rec Blob Sidecar' } }
  { resourceMetadata: { id: app3 }, name: 'RxBytes', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'HLS Transcoder', resourceDisplayName: 'HLS Transcoder' } }
  { resourceMetadata: { id: app4 }, name: 'RxBytes', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'HLS Blob Sidecar', resourceDisplayName: 'HLS Blob Sidecar' } }
  { resourceMetadata: { id: app5 }, name: 'RxBytes', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'SG HLS Server', resourceDisplayName: 'SG HLS Server' } }
  { resourceMetadata: { id: app6 }, name: 'RxBytes', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'SG Platform', resourceDisplayName: 'SG Platform' } }
]

var sgHttpMetrics = [
  { resourceMetadata: { id: app5 }, name: 'Requests', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'SG HLS Server', resourceDisplayName: 'SG HLS Server' } }
  { resourceMetadata: { id: app6 }, name: 'Requests', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'SG Platform', resourceDisplayName: 'SG Platform' } }
]

var rtmpHttpMetrics = [
  { resourceMetadata: { id: app1 }, name: 'Requests', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'RTMP Server', resourceDisplayName: 'RTMP Server' } }
  { resourceMetadata: { id: app2 }, name: 'Requests', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'Rec Blob Sidecar', resourceDisplayName: 'Rec Blob Sidecar' } }
  { resourceMetadata: { id: app3 }, name: 'Requests', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'HLS Transcoder', resourceDisplayName: 'HLS Transcoder' } }
  { resourceMetadata: { id: app4 }, name: 'Requests', aggregationType: 1, namespace: ns, metricVisualization: { displayName: 'HLS Blob Sidecar', resourceDisplayName: 'HLS Blob Sidecar' } }
]

var replicaMetrics = [
  { resourceMetadata: { id: app1 }, name: 'Replicas', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'RTMP Server', resourceDisplayName: 'RTMP Server' } }
  { resourceMetadata: { id: app2 }, name: 'Replicas', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'Rec Blob Sidecar', resourceDisplayName: 'Rec Blob Sidecar' } }
  { resourceMetadata: { id: app3 }, name: 'Replicas', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'HLS Transcoder', resourceDisplayName: 'HLS Transcoder' } }
  { resourceMetadata: { id: app4 }, name: 'Replicas', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'HLS Blob Sidecar', resourceDisplayName: 'HLS Blob Sidecar' } }
  { resourceMetadata: { id: app5 }, name: 'Replicas', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'SG HLS Server', resourceDisplayName: 'SG HLS Server' } }
  { resourceMetadata: { id: app6 }, name: 'Replicas', aggregationType: 4, namespace: ns, metricVisualization: { displayName: 'SG Platform', resourceDisplayName: 'SG Platform' } }
]

var restartMetrics = [
  { resourceMetadata: { id: app1 }, name: 'RestartCount', aggregationType: 3, namespace: ns, metricVisualization: { displayName: 'RTMP Server', resourceDisplayName: 'RTMP Server' } }
  { resourceMetadata: { id: app2 }, name: 'RestartCount', aggregationType: 3, namespace: ns, metricVisualization: { displayName: 'Rec Blob Sidecar', resourceDisplayName: 'Rec Blob Sidecar' } }
  { resourceMetadata: { id: app3 }, name: 'RestartCount', aggregationType: 3, namespace: ns, metricVisualization: { displayName: 'HLS Transcoder', resourceDisplayName: 'HLS Transcoder' } }
  { resourceMetadata: { id: app4 }, name: 'RestartCount', aggregationType: 3, namespace: ns, metricVisualization: { displayName: 'HLS Blob Sidecar', resourceDisplayName: 'HLS Blob Sidecar' } }
  { resourceMetadata: { id: app5 }, name: 'RestartCount', aggregationType: 3, namespace: ns, metricVisualization: { displayName: 'SG HLS Server', resourceDisplayName: 'SG HLS Server' } }
  { resourceMetadata: { id: app6 }, name: 'RestartCount', aggregationType: 3, namespace: ns, metricVisualization: { displayName: 'SG Platform', resourceDisplayName: 'SG Platform' } }
]

// Shared display settings
var timespan24h = { relative: { duration: 86400000 }, showUTCTime: false, grain: 1 }
var lineChart = 2
var sharedTimeRangeInput = [{ name: 'sharedTimeRange', isOptional: true }]
var legendRight = { isVisible: true, position: 2, hideSubtitle: false }

// ---------- Dashboard ----------

resource dashboard 'Microsoft.Portal/dashboards@2020-09-01-preview' = {
  name: 'rtmpgo-streamgate-monitor'
  location: location
  tags: {
    'hidden-title': 'RTMP-Go & StreamGate Monitoring'
    component: 'monitoring'
  }
  properties: {
    lenses: [
      {
        order: 0
        parts: [
          // ===== Row 0: Title =====
          {
            position: { x: 0, y: 0, colSpan: 12, rowSpan: 1 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MarkdownPart'
              inputs: []
              settings: {
                content: {
                  settings: {
                    content: '## RTMP-Go & StreamGate — Live Monitoring\n**Resource Group:** `${rg}` | **Region:** `${location}` | **Environment:** `${containerEnvName}`'
                    title: ''
                    subtitle: ''
                    markdownSource: 1
                    markdownUri: null
                  }
                }
              }
            }
          }
          // ===== Row 1: CPU Section =====
          {
            position: { x: 0, y: 1, colSpan: 12, rowSpan: 1 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MarkdownPart'
              inputs: []
              settings: {
                content: {
                  settings: {
                    content: '### CPU Utilization'
                    title: ''
                    subtitle: ''
                    markdownSource: 1
                    markdownUri: null
                  }
                }
              }
            }
          }
          {
            position: { x: 0, y: 2, colSpan: 12, rowSpan: 4 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MonitorChartPart'
              inputs: sharedTimeRangeInput
              settings: {
                content: {
                  options: {
                    chart: {
                      title: 'CPU Usage (NanoCores) — All Container Apps'
                      titleKind: 1
                      metrics: cpuMetrics
                      visualization: {
                        chartType: lineChart
                        legendVisualization: legendRight
                        axisVisualization: { x: { isVisible: true, axisType: 2 }, y: { isVisible: true, axisType: 1 } }
                      }
                      timespan: timespan24h
                    }
                  }
                }
              }
            }
          }
          // ===== Row 6: Memory Section =====
          {
            position: { x: 0, y: 6, colSpan: 12, rowSpan: 1 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MarkdownPart'
              inputs: []
              settings: {
                content: {
                  settings: {
                    content: '### Memory Utilization'
                    title: ''
                    subtitle: ''
                    markdownSource: 1
                    markdownUri: null
                  }
                }
              }
            }
          }
          {
            position: { x: 0, y: 7, colSpan: 12, rowSpan: 4 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MonitorChartPart'
              inputs: sharedTimeRangeInput
              settings: {
                content: {
                  options: {
                    chart: {
                      title: 'Memory Working Set (Bytes) — All Container Apps'
                      titleKind: 1
                      metrics: memMetrics
                      visualization: {
                        chartType: lineChart
                        legendVisualization: legendRight
                        axisVisualization: { x: { isVisible: true, axisType: 2 }, y: { isVisible: true, axisType: 1 } }
                      }
                      timespan: timespan24h
                    }
                  }
                }
              }
            }
          }
          // ===== Row 11: Network Section =====
          {
            position: { x: 0, y: 11, colSpan: 12, rowSpan: 1 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MarkdownPart'
              inputs: []
              settings: {
                content: {
                  settings: {
                    content: '### Network I/O'
                    title: ''
                    subtitle: ''
                    markdownSource: 1
                    markdownUri: null
                  }
                }
              }
            }
          }
          {
            position: { x: 0, y: 12, colSpan: 6, rowSpan: 4 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MonitorChartPart'
              inputs: sharedTimeRangeInput
              settings: {
                content: {
                  options: {
                    chart: {
                      title: 'Network TX (Bytes)'
                      titleKind: 1
                      metrics: txMetrics
                      visualization: {
                        chartType: lineChart
                        legendVisualization: legendRight
                      }
                      timespan: timespan24h
                    }
                  }
                }
              }
            }
          }
          {
            position: { x: 6, y: 12, colSpan: 6, rowSpan: 4 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MonitorChartPart'
              inputs: sharedTimeRangeInput
              settings: {
                content: {
                  options: {
                    chart: {
                      title: 'Network RX (Bytes)'
                      titleKind: 1
                      metrics: rxMetrics
                      visualization: {
                        chartType: lineChart
                        legendVisualization: legendRight
                      }
                      timespan: timespan24h
                    }
                  }
                }
              }
            }
          }
          // ===== Row 16: HTTP Requests Section =====
          {
            position: { x: 0, y: 16, colSpan: 12, rowSpan: 1 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MarkdownPart'
              inputs: []
              settings: {
                content: {
                  settings: {
                    content: '### HTTP Request Traffic'
                    title: ''
                    subtitle: ''
                    markdownSource: 1
                    markdownUri: null
                  }
                }
              }
            }
          }
          {
            position: { x: 0, y: 17, colSpan: 6, rowSpan: 4 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MonitorChartPart'
              inputs: sharedTimeRangeInput
              settings: {
                content: {
                  options: {
                    chart: {
                      title: 'HTTP Requests — StreamGate'
                      titleKind: 1
                      metrics: sgHttpMetrics
                      visualization: {
                        chartType: lineChart
                        legendVisualization: legendRight
                      }
                      timespan: timespan24h
                    }
                  }
                }
              }
            }
          }
          {
            position: { x: 6, y: 17, colSpan: 6, rowSpan: 4 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MonitorChartPart'
              inputs: sharedTimeRangeInput
              settings: {
                content: {
                  options: {
                    chart: {
                      title: 'HTTP Requests — rtmp-go Services'
                      titleKind: 1
                      metrics: rtmpHttpMetrics
                      visualization: {
                        chartType: lineChart
                        legendVisualization: legendRight
                      }
                      timespan: timespan24h
                    }
                  }
                }
              }
            }
          }
          // ===== Row 21: Replicas & Restarts =====
          {
            position: { x: 0, y: 21, colSpan: 12, rowSpan: 1 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MarkdownPart'
              inputs: []
              settings: {
                content: {
                  settings: {
                    content: '### Replica Count & Restarts'
                    title: ''
                    subtitle: ''
                    markdownSource: 1
                    markdownUri: null
                  }
                }
              }
            }
          }
          {
            position: { x: 0, y: 22, colSpan: 6, rowSpan: 4 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MonitorChartPart'
              inputs: sharedTimeRangeInput
              settings: {
                content: {
                  options: {
                    chart: {
                      title: 'Replica Count'
                      titleKind: 1
                      metrics: replicaMetrics
                      visualization: {
                        chartType: lineChart
                        legendVisualization: legendRight
                      }
                      timespan: timespan24h
                    }
                  }
                }
              }
            }
          }
          {
            position: { x: 6, y: 22, colSpan: 6, rowSpan: 4 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MonitorChartPart'
              inputs: sharedTimeRangeInput
              settings: {
                content: {
                  options: {
                    chart: {
                      title: 'Container Restarts'
                      titleKind: 1
                      metrics: restartMetrics
                      visualization: {
                        chartType: lineChart
                        legendVisualization: legendRight
                      }
                      timespan: timespan24h
                    }
                  }
                }
              }
            }
          }
          // ===== Row 26: Storage =====
          {
            position: { x: 0, y: 26, colSpan: 12, rowSpan: 1 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MarkdownPart'
              inputs: []
              settings: {
                content: {
                  settings: {
                    content: '### Azure Storage (Blob & Files)'
                    title: ''
                    subtitle: ''
                    markdownSource: 1
                    markdownUri: null
                  }
                }
              }
            }
          }
          {
            position: { x: 0, y: 27, colSpan: 6, rowSpan: 4 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MonitorChartPart'
              inputs: sharedTimeRangeInput
              settings: {
                content: {
                  options: {
                    chart: {
                      title: 'Storage Transactions'
                      titleKind: 1
                      metrics: [
                        {
                          resourceMetadata: { id: stor }
                          name: 'Transactions'
                          aggregationType: 1
                          namespace: stoNs
                          metricVisualization: { displayName: 'Transactions', resourceDisplayName: storageAccountName }
                        }
                      ]
                      visualization: {
                        chartType: lineChart
                        legendVisualization: legendRight
                      }
                      timespan: timespan24h
                    }
                  }
                }
              }
            }
          }
          {
            position: { x: 6, y: 27, colSpan: 6, rowSpan: 4 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MonitorChartPart'
              inputs: sharedTimeRangeInput
              settings: {
                content: {
                  options: {
                    chart: {
                      title: 'Storage Bandwidth (Ingress / Egress)'
                      titleKind: 1
                      metrics: [
                        {
                          resourceMetadata: { id: stor }
                          name: 'Ingress'
                          aggregationType: 1
                          namespace: stoNs
                          metricVisualization: { displayName: 'Ingress', resourceDisplayName: storageAccountName }
                        }
                        {
                          resourceMetadata: { id: stor }
                          name: 'Egress'
                          aggregationType: 1
                          namespace: stoNs
                          metricVisualization: { displayName: 'Egress', resourceDisplayName: storageAccountName }
                        }
                      ]
                      visualization: {
                        chartType: lineChart
                        legendVisualization: legendRight
                      }
                      timespan: timespan24h
                    }
                  }
                }
              }
            }
          }
          // ===== Row 31: Resource Inventory =====
          {
            position: { x: 0, y: 31, colSpan: 12, rowSpan: 1 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MarkdownPart'
              inputs: []
              settings: {
                content: {
                  settings: {
                    content: '### Resource Inventory'
                    title: ''
                    subtitle: ''
                    markdownSource: 1
                    markdownUri: null
                  }
                }
              }
            }
          }
          {
            position: { x: 0, y: 32, colSpan: 12, rowSpan: 4 }
            metadata: {
              type: 'Extension/HubsExtension/PartType/MarkdownPart'
              inputs: []
              settings: {
                content: {
                  settings: {
                    content: '| Container App | Role | Component | CPU | Memory |\n|---|---|---|---|---|\n| `${rtmpServerApp}` | RTMP Server | rtmp-go | 0.5 vCPU | 1 GiB |\n| `${recBlobSidecarApp}` | Rec Blob Sidecar | rtmp-go | 0.25 vCPU | 0.5 GiB |\n| `${hlsTranscoderApp}` | HLS Transcoder | rtmp-go | 2.0 vCPU | 4 GiB |\n| `${hlsBlobSidecarApp}` | HLS Blob Sidecar | rtmp-go | 0.25 vCPU | 0.5 GiB |\n| `${sgHlsServerApp}` | HLS Server | StreamGate | 0.5 vCPU | 1 GiB |\n| `${sgPlatformApp}` | Platform | StreamGate | 0.5 vCPU | 1 GiB |\n\n**Storage:** `${storageAccountName}` · **Log Analytics:** `${logAnalyticsName}` · **Environment:** `${containerEnvName}`'
                    title: ''
                    subtitle: ''
                    markdownSource: 1
                    markdownUri: null
                  }
                }
              }
            }
          }
        ]
      }
    ]
    metadata: {
      model: {
        timeRange: {
          value: { relative: { duration: 24, timeUnit: 1 } }
          type: 'MsPortalFx.Composition.Configuration.ValueTypes.TimeRange'
        }
      }
    }
  }
}

// ---------- Outputs ----------

output dashboardId string = dashboard.id
output dashboardName string = dashboard.name
output portalUrl string = 'https://portal.azure.com/#@/dashboard/arm${dashboard.id}'

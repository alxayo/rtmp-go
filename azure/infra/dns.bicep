// ============================================================================
// Azure DNS Zone for custom domain
// ============================================================================
// Deploys:
//   - DNS Zone (e.g. port-80.com)
//   - CNAME record (e.g. stream → ACA FQDN) — only when rtmpAppFqdn is provided
//
// This template targets a SEPARATE resource group (e.g. rg-dns) so the DNS zone
// and GoDaddy nameserver delegation survive teardowns of the main rg-rtmpgo.
//
// Usage:
//   # First time — create zone, get nameservers for GoDaddy:
//   az deployment group create -g rg-dns -f dns.bicep -p dns.parameters.json
//
//   # After main deploy — add/update CNAME record:
//   az deployment group create -g rg-dns -f dns.bicep -p dns.parameters.json \
//     -p rtmpAppFqdn="azapp....azurecontainerapps.io"
// ============================================================================

targetScope = 'resourceGroup'

// ---------- Parameters ----------

@description('DNS zone name (your registered domain)')
param zoneName string

@description('Subdomain for the RTMP streaming endpoint')
param subdomain string = 'stream'

@description('FQDN of the Container App (from main deployment output). Leave empty to skip CNAME creation.')
param rtmpAppFqdn string = ''

@description('Azure region — DNS zones are global but require a location parameter')
param location string = 'global'

@description('TTL in seconds for the CNAME record')
param ttl int = 300

// ---------- DNS Zone ----------

resource dnsZone 'Microsoft.Network/dnsZones@2023-07-01-preview' = {
  name: zoneName
  location: location
  properties: {
    zoneType: 'Public'
  }
}

// ---------- CNAME Record (conditional — only when rtmpAppFqdn is provided) ----------

resource cnameRecord 'Microsoft.Network/dnsZones/CNAME@2023-07-01-preview' = if (!empty(rtmpAppFqdn)) {
  name: subdomain
  parent: dnsZone
  properties: {
    TTL: ttl
    CNAMERecord: {
      cname: rtmpAppFqdn
    }
  }
}

// ---------- Outputs ----------

output nameServers array = dnsZone.properties.nameServers
output zoneName string = dnsZone.name
output customDomain string = '${subdomain}.${zoneName}'
output cnameTarget string = !empty(rtmpAppFqdn) ? rtmpAppFqdn : '(not configured — re-run with rtmpAppFqdn parameter)'

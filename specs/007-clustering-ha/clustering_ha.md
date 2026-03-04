# Feature 007: Clustering & High Availability

**Feature**: 007-clustering-ha  
**Status**: Research / Design  
**Date**: 2026-03-04  
**Branch**: (not yet created)

## Overview

Horizontal scaling across multiple server instances with high availability
failover. When one instance fails, another takes over stream ingestion and
continues serving subscribers with minimal interruption.

### Design Constraints

- **Zero external dependencies** (stdlib only — no etcd/consul/Redis)
- **Opt-in**: disabled by default; enabled via `-cluster-*` CLI flags
- **Compatible with existing single-instance mode** (no behavioral changes unless clustering is enabled)
- **Works with standard RTMP clients** (OBS, FFmpeg, ffplay) without client-side changes

---

## Architecture

### Current Single-Instance Data Flow

```
Publisher → TCP → Handshake → Registry.SetPublisher() → BroadcastMessage() → Subscribers
                                    ↓
                              FLV Recorder
                              Relay Destinations
```

All state lives in-memory: the `Registry` map, cached sequence headers, codec
info, subscriber lists. There is no shared state layer.

### Clustered Data Flow

```
                    ┌─────────────────────────┐
                    │      Load Balancer       │
                    │   (TCP round-robin or    │
                    │    stream-key routing)   │
                    └──────┬──────────┬────────┘
                           │          │
              ┌────────────▼──┐  ┌────▼───────────┐
              │  Instance A   │  │  Instance B     │
              │  (primary for │  │  (primary for   │
              │   live/cam1)  │  │   live/cam2)    │
              │               │  │                 │
              │  Registry     │  │  Registry       │
              │  ┌──────────┐ │  │  ┌──────────┐  │
              │  │ live/cam1│◄├──├──┤ live/cam1│  │
              │  │ (pub+sub)│ │  │  │ (sub only)│  │
              │  └──────────┘ │  │  └──────────┘  │
              └───────┬───────┘  └───────┬────────┘
                      │                  │
                      └──────┬───────────┘
                             │
                    Inter-node media relay
                    (internal RTMP or custom TCP)
```

### Three Problems to Solve

1. **Discovery**: Which instance has which stream?
2. **Replication**: Sharing media data between instances
3. **Failover**: Taking over when an instance dies

---

## Component Design

### Component 1: Cluster Registry (`internal/rtmp/cluster/`)

```go
// Package cluster coordinates stream ownership and media relay across
// multiple server instances for horizontal scaling and failover.
package cluster

// Node represents a server instance in the cluster.
type Node struct {
    ID       string    // unique node identifier (e.g. hostname:port)
    Addr     string    // RTMP listen address reachable by other nodes
    APIAddr  string    // HTTP address for health checks and stream queries
    LastSeen time.Time // last heartbeat timestamp
}

// StreamOwnership tracks which node owns (publishes) each stream.
type StreamOwnership struct {
    StreamKey   string
    OwnerNodeID string    // node where the publisher is connected
    StartTime   time.Time
}

// ClusterRegistry extends the local Registry with cross-node awareness.
// It wraps the existing server.Registry and adds:
//   - Node discovery (who's in the cluster)
//   - Stream location (which node owns which stream key)
//   - Media relay (forwarding from owner to requesting nodes)
type ClusterRegistry struct {
    localRegistry *server.Registry
    nodes         map[string]*Node       // known cluster members
    ownership     map[string]string      // streamKey → ownerNodeID
    selfID        string                 // this node's ID
    mu            sync.RWMutex
    relayClients  map[string]*client.Client // active inter-node relay connections
    logger        *slog.Logger
}
```

### Component 2: Discovery & Heartbeat

A gossip-like HTTP protocol using stdlib only:

```go
// Each node periodically POSTs its state to known peers.

// Heartbeat payload (JSON over HTTP)
type Heartbeat struct {
    NodeID       string            `json:"node_id"`
    Addr         string            `json:"addr"`        // RTMP address
    Streams      []string          `json:"streams"`     // stream keys this node owns
    Subscribers  map[string]int    `json:"subscribers"` // stream key → subscriber count
    Uptime       int64             `json:"uptime_sec"`
}
```

Each node exposes:
- `GET /cluster/health` — liveness check
- `GET /cluster/streams` — what streams this node has
- `POST /cluster/heartbeat` — receive peer state

### Component 3: Cross-Node Media Relay

When a subscriber connects to Instance B but the publisher is on Instance A:

```
1. Subscriber sends "play live/cam1" to Instance B
2. Instance B checks local Registry → no publisher found
3. Instance B queries ClusterRegistry → owner is Instance A
4. Instance B creates an RTMP client connection TO Instance A
   (reusing the existing internal/rtmp/client package)
5. Instance A treats this as a normal subscriber
6. Instance B receives media and forwards to its local subscriber
```

Integration point in `command_integration.go`:

```go
// In the play handler:
func handlePlay(st *commandState, msg *chunk.Message) {
    stream := reg.GetStream(streamKey)
    
    if stream == nil || stream.Publisher == nil {
        // NEW: Check cluster for remote owner
        ownerNode := cluster.FindStreamOwner(streamKey)
        if ownerNode != "" && ownerNode != cluster.SelfID() {
            // Create inter-node relay (Instance B subscribes to Instance A)
            stream = cluster.RelayFromRemote(ownerNode, streamKey, reg)
        }
    }
    
    // ... existing play logic (add subscriber to stream)
}
```

---

## High Availability / Failover

### Subscriber Failover (Easy)

When Instance A dies, subscribers on Instance A lose their TCP connections.
They reconnect (OBS/ffplay auto-reconnect). The load balancer routes them to
Instance B. Instance B either has the stream (if publisher is there) or relays
from the instance that does.

### Publisher Failover (Hard, With Caveats)

When Instance A dies while a publisher (OBS) is streaming to it:

1. The publisher's TCP connection drops
2. OBS detects the disconnect and auto-reconnects (configurable, 1-10s delay)
3. OBS reconnects to Instance B (via load balancer)
4. Instance B becomes the new owner of that stream key
5. Subscribers who were relaying from Instance A detect the relay drop
6. They re-query the cluster, discover Instance B now owns the stream
7. They establish new relay connections to Instance B

**The gap**: There is a **1-10 second interruption** during publisher reconnect.
This is inherent to RTMP — the protocol has no built-in handoff mechanism. The
publisher must establish a new TCP connection, redo the handshake, and send new
sequence headers.

### Active-Active Dual Ingest (Best HA Option)

For near-zero-downtime, the publisher streams to two instances simultaneously:

```
Publisher (OBS)
    ├── Primary:   rtmp://instance-a:1935/live/cam1
    └── Backup:    rtmp://instance-b:1935/live/cam1
         (OBS Advanced Output → "Backup Server" feature)
```

Both instances receive the same stream. Subscribers connect to either. If
Instance A dies:
- Subscribers on A reconnect to B (already has the stream, no relay needed)
- Publisher's primary connection drops, but backup is already live
- **Zero subscriber interruption**

This requires a registry change — allow two publishers for the same stream key
when in cluster mode:

```go
// In registry.go, cluster-aware publisher logic:
func (s *Stream) SetPublisher(pub interface{}) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.Publisher != nil && !clusterMode {
        return ErrPublisherExists
    }
    // In cluster mode, accept — this may be the backup ingest
    s.Publisher = pub
    return nil
}
```

### Sequence Header Synchronization

The existing sequence header caching (`VideoSequenceHeader`,
`AudioSequenceHeader` on `Stream`) already solves the late-join problem. When a
cross-node relay establishes, the source node sends cached headers first
(`BroadcastMessage` already does this). No new code needed.

---

## CLI Flags

```bash
# Instance A
./rtmp-server -listen :1935 \
  -cluster-api-addr :8081 \
  -cluster-peers "10.0.0.2:8081,10.0.0.3:8081" \
  -cluster-node-id "node-a"

# Instance B
./rtmp-server -listen :1935 \
  -cluster-api-addr :8081 \
  -cluster-peers "10.0.0.1:8081,10.0.0.3:8081" \
  -cluster-node-id "node-b"
```

| Flag | Default | Description |
|------|---------|-------------|
| `-cluster-node-id` | (hostname) | Unique identifier for this node |
| `-cluster-api-addr` | (disabled) | HTTP address for cluster API and health checks |
| `-cluster-peers` | (none) | Comma-separated list of peer API addresses |
| `-cluster-heartbeat` | `5s` | Heartbeat interval |
| `-cluster-timeout` | `15s` | Peer considered dead after this many missed heartbeats |

---

## Metrics (extends existing expvar)

```go
var (
    ClusterNodesActive     = expvar.NewInt("rtmp_cluster_nodes_active")
    ClusterRelaysActive    = expvar.NewInt("rtmp_cluster_relays_active")
    ClusterRelayBytesSent  = expvar.NewInt("rtmp_cluster_relay_bytes_sent")
    ClusterFailovers       = expvar.NewInt("rtmp_cluster_failovers_total")
)
```

---

## Implementation Scope

| Component | New files | Touches existing | Complexity |
|---|---|---|---|
| Node discovery + heartbeat | `cluster/node.go`, `cluster/discovery.go` | `cmd/rtmp-server/flags.go`, `main.go` | Medium |
| Stream ownership tracking | `cluster/ownership.go` | `server/registry.go` | Medium |
| Cross-node relay | `cluster/relay.go` | `server/command_integration.go` | High |
| Failover detection | `cluster/health.go` | `server/server.go` | Medium |
| Dual-ingest HA | — | `server/registry.go` (relax publisher constraint) | Low |
| CLI flags + config | — | `flags.go`, `main.go` | Low |

---

## Known Limitations

1. **RTMP has no handoff** — publisher must reconnect on failure. The 1-10s gap
   is unavoidable without dual-ingest.
2. **No shared storage** — each instance records FLV independently. A failed
   instance's recording is lost unless using a shared filesystem (NFS/EFS).
3. **Stdlib-only constraint** — no etcd/consul/Redis for coordination. The HTTP
   gossip approach works for 2-5 nodes but doesn't scale to hundreds.
4. **Split-brain risk** — if two instances both think they own a stream key,
   subscribers may get duplicate/conflicting data. A simple leader-election
   (lowest node ID wins ties) mitigates this.
5. **No state transfer** — when a publisher reconnects to a new node, all
   in-flight media is lost. The new node starts fresh with new sequence headers.

---

## Recommendation

For most production deployments (2-3 nodes), the **dual-ingest** approach
provides the best HA with minimal complexity:

- OBS/encoder streams to two servers simultaneously
- Cross-node relay handles subscriber distribution
- HTTP gossip handles discovery (no external dependencies)
- Existing sequence header caching handles late-join for relayed subscribers

This gives near-zero subscriber interruption during node failures while
staying within the stdlib-only constraint.

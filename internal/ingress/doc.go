// Package ingress provides a protocol-agnostic publish lifecycle that
// coordinates authentication, hook events, metrics, and media dispatch
// for any ingest protocol (RTMP, SRT, etc.).
//
// The central type is Manager, which tracks active publish sessions and
// ensures that each stream key has at most one publisher at a time.
// Protocol-specific code (e.g. an SRT listener) creates a Publisher
// implementation and hands it to the Manager, which returns a
// PublishSession handle for pushing media into the server pipeline.
//
// RTMP continues to use its existing internal path. Only new ingest
// protocols (starting with SRT) use ingress.Manager.
package ingress

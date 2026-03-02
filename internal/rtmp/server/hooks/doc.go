// Package hooks provides an event notification system for the RTMP server.
//
// When important events occur during the server's operation (client connects,
// stream starts publishing, subscriber joins, etc.), the hook system can
// notify external systems in three ways:
//
//   - Webhook: HTTP POST with JSON event payload to a URL
//   - Shell: Execute a script with event data as environment variables
//   - Stdio: Print structured event data to stderr (for log pipelines)
//
// # Architecture
//
// The system has three main components:
//
//   - [Event]: Describes what happened (type, connection ID, stream key, metadata)
//   - [Hook]: Interface for handlers (Execute, Type, ID)
//   - [HookManager]: Central registry that maps event types to hooks and
//     dispatches events via a bounded concurrency pool
//
// # Supported Events
//
//   - connection_accept: A new TCP connection was accepted
//   - connection_close: A connection was closed
//   - publish_start: A client started publishing media
//   - play_start: A client started subscribing to a stream
//   - codec_detected: Audio/video codec was identified
//
// # Usage
//
//	manager := hooks.NewHookManager(hooks.DefaultHookConfig(), logger)
//
//	// Register a webhook for publish events
//	wh := hooks.NewWebhookHook("my-webhook", "https://api.example.com/on-publish", 10*time.Second)
//	manager.RegisterHook(hooks.EventPublishStart, wh)
//
//	// Trigger an event (done automatically by the server)
//	event := hooks.NewEvent(hooks.EventPublishStart).
//	    WithConnID("c000001").
//	    WithStreamKey("live/mystream")
//	manager.TriggerEvent(ctx, *event)
package hooks

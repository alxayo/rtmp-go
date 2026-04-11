// Package auth provides pluggable token-based authentication for RTMP
// publish and play requests.
//
// The package defines a [Validator] interface that all authentication
// backends implement. Four built-in validators are provided:
//
//   - [AllowAllValidator]: accepts every request (default, backward-compatible)
//   - [TokenValidator]: validates against an in-memory map of stream-key → token pairs
//   - [FileValidator]: loads tokens from a JSON file, supports live reload
//   - [CallbackValidator]: delegates validation to an external HTTP webhook
//
// # How Tokens Are Passed
//
// Clients (OBS, FFmpeg, ffplay) embed tokens as URL query parameters in the
// stream name field of publish/play commands:
//
//	rtmp://server/live/mystream?token=secret123
//
// The server parses these query parameters using [ParseStreamURL] and passes
// them to the validator via the [Request] struct.
//
// # Request Flow
//
// 1. Client connects with RTMP connect command (app, flashVer, etc.)
// 2. Client sends publish or play command with stream name + query params
// 3. Server calls ValidatePublish() or ValidatePlay() with the request
// 4. Validator returns nil (allowed) or an error (denied)
// 5. If validation fails, the server rejects the stream with status=1 (unauth)
//
// Authentication is enforced at the publish/play command level in the
// server package (see authenticateRequest in command_integration.go),
// NOT during connect or handshake.
//
// # Validator Implementations
//
// AllowAllValidator: Always returns nil (backward-compatible).
//
// TokenValidator: In-memory map. Useful for development and small deployments.
// Not persistent across restarts.
//
//	v := auth.NewTokenValidator()
//	v.AddToken("live/stream1", "secret123")
//	v.ValidatePublish(ctx, req)  // nil if req.StreamKey=="live/stream1" and req.QueryParams["token"]=="secret123"
//
// FileValidator: Loads tokens from a JSON file. Can be reloaded at runtime
// to pick up token changes without restarting the server.
//
//	v, _ := auth.NewFileValidator("tokens.json")
//	v.Reload()  // Reload from disk if file changed
//
// CallbackValidator: HTTP POST to a webhook. The server sends the request
// as JSON and expects status 200 (allowed) or other status (denied).
//
//	v, _ := auth.NewCallbackValidator("https://auth.example.com/validate")
//	v.ValidatePublish(ctx, req)  // Makes HTTP request to webhook
//
// # Token File Format
//
// JSON file with stream_key → token mapping:
//
//	{
//	  "live/stream1": "secret123",
//	  "live/stream2": "password456",
//	  "archive/backup": "key789"
//	}
//
// # Integration with Server
//
// The server holds a Validator instance (default: AllowAllValidator).
// Pass -auth=file:tokens.json or -auth=callback:http://... to select.
//
// # Concurrency
//
// All validators are safe for concurrent use from multiple goroutines.
// Each RTMP connection runs in a separate goroutine (readLoop), and all
// may call ValidatePublish/ValidatePlay simultaneously.
package auth

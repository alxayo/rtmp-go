// Package rpc implements RTMP command message parsing and response building.
//
// RTMP commands are AMF0-encoded messages (TypeID 20) that control the
// application-level session: connecting to the server, creating streams,
// publishing media, and subscribing to streams.
//
// # Supported Commands
//
//   - connect: Establishes the application-level connection. Parsed into
//     [ConnectCommand] with app name, tcUrl, flash version, etc.
//   - createStream: Allocates a new message stream ID. Uses
//     [StreamIDAllocator] for thread-safe ID assignment.
//   - publish: Begins publishing media on a stream key (app/streamName).
//   - play: Subscribes to a stream key for media playback.
//   - deleteStream: Releases a previously allocated stream.
//
// # Dispatcher
//
// The [Dispatcher] routes incoming command messages to registered handler
// callbacks. It decodes AMF0, identifies the command name, parses it into
// a strongly-typed struct, and invokes the corresponding handler.
//
// Unknown commands (including OBS/FFmpeg extensions like releaseStream,
// FCPublish) are logged and gracefully ignored.
//
// # Response Builders
//
//   - [BuildConnectResponse]: Creates a _result message for connect.
//   - [BuildCreateStreamResponse]: Creates a _result message with the
//     allocated stream ID.
package rpc

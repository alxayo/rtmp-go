package rpc

// Command Dispatcher
// ==================
// The dispatcher is the central routing layer for RTMP commands. When an AMF0
// command message (TypeID 20) arrives, the dispatcher:
//   1. Decodes the AMF0 payload to find the command name (e.g. "connect", "publish")
//   2. Parses the full command into a strongly-typed struct (ConnectCommand, etc.)
//   3. Calls the registered handler function for that command
//
// Unknown commands (including OBS/FFmpeg extensions like releaseStream, FCPublish)
// are logged and gracefully ignored — they don't cause errors.
//
// The dispatcher uses an appProvider callback to lazily retrieve the application
// name (set during the "connect" command) needed for publish/play parsing.

import (
	"bytes"
	"fmt"
	"log/slog"

	"github.com/alxayo/go-rtmp/internal/errors"
	"github.com/alxayo/go-rtmp/internal/logger"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// Handler function types – kept narrow to the parsed command structure.
type (
	ConnectHandler      func(*ConnectCommand, *chunk.Message) error
	CreateStreamHandler func(*CreateStreamCommand, *chunk.Message) error
	PublishHandler      func(*PublishCommand, *chunk.Message) error
	PlayHandler         func(*PlayCommand, *chunk.Message) error
	DeleteStreamHandler func(values []interface{}, msg *chunk.Message) error
	// CloseStreamHandler handles the "closeStream" command that some RTMP clients
	// (e.g. OBS, mobile apps) send instead of or in addition to "deleteStream"
	// when ending a publishing/playback session. The raw AMF0 values are passed
	// because closeStream has no formally standardized payload structure.
	CloseStreamHandler func(values []interface{}, msg *chunk.Message) error
)

// Dispatcher routes AMF0 command messages to registered handlers.
type Dispatcher struct {
	appProvider func() string

	OnConnect      ConnectHandler
	OnCreateStream CreateStreamHandler
	OnPublish      PublishHandler
	OnPlay         PlayHandler
	OnDeleteStream DeleteStreamHandler
	OnCloseStream  CloseStreamHandler

	log *slog.Logger
}

// NewDispatcher creates a dispatcher. appProvider may be nil; in that case
// publish/play parsing that relies on app will return a protocol error until
// a connect handler sets application state and a new dispatcher is built (or
// caller supplies a non-nil provider referencing mutable state).
func NewDispatcher(appProvider func() string) *Dispatcher {
	return &Dispatcher{appProvider: appProvider, log: logger.Logger().With("component", "dispatcher")}
}

// Dispatch examines msg (expected TypeID=20) and routes to the appropriate
// handler. It returns an error for parse/handler failures. Unknown commands
// are logged at warn level and produce no error.
func (d *Dispatcher) Dispatch(msg *chunk.Message) error {
	if msg == nil {
		return errors.NewProtocolError("dispatch", fmt.Errorf("nil message"))
	}
	if msg.TypeID != commandMessageAMF0TypeID {
		return errors.NewProtocolError("dispatch", fmt.Errorf("unexpected message type %d", msg.TypeID))
	}

	// Decode all AMF0 values. We decode once then branch; per current scope
	// payloads are small so this is acceptable. (If needed we could implement
	// a single-value streaming decoder to read just the first marker.)
	vals, err := amf.DecodeAll(msg.Payload)
	if err != nil {
		return errors.NewProtocolError("dispatch.decode", err)
	}
	if len(vals) == 0 {
		return errors.NewProtocolError("dispatch", fmt.Errorf("empty AMF payload"))
	}
	name, ok := vals[0].(string)
	if !ok {
		return errors.NewProtocolError("dispatch", fmt.Errorf("first AMF value not a string (command name)"))
	}

	switch name {
	case "connect":
		d.log.Debug("dispatching connect command")
		if d.OnConnect == nil {
			d.log.Error("no OnConnect handler registered")
			return d.noHandlerErr(name)
		}
		d.log.Debug("parsing connect command")
		cc, err := ParseConnectCommand(msg)
		if err != nil {
			d.log.Error("connect parse error", "error", err)
			return err
		}
		d.log.Debug("invoking OnConnect handler", "app", cc.App, "tcUrl", cc.TcURL)
		return d.OnConnect(cc, msg)
	case "createStream":
		d.log.Debug("dispatching createStream command")
		if d.OnCreateStream == nil {
			d.log.Error("no OnCreateStream handler registered")
			return d.noHandlerErr(name)
		}
		d.log.Debug("parsing createStream command")
		cs, err := ParseCreateStreamCommand(msg)
		if err != nil {
			d.log.Error("createStream parse error", "error", err)
			return err
		}
		d.log.Debug("invoking OnCreateStream handler", "txn_id", cs.TransactionID)
		return d.OnCreateStream(cs, msg)
	case "publish":
		if d.OnPublish == nil {
			return d.noHandlerErr(name)
		}
		app := d.currentApp()
		pc, err := ParsePublishCommand(app, msg)
		if err != nil {
			return err
		}
		return d.OnPublish(pc, msg)
	case "play":
		if d.OnPlay == nil {
			return d.noHandlerErr(name)
		}
		app := d.currentApp()
		pl, err := ParsePlayCommand(msg, app)
		if err != nil {
			return err
		}
		return d.OnPlay(pl, msg)
	case "deleteStream":
		if d.OnDeleteStream == nil {
			return d.noHandlerErr(name)
		}
		return d.OnDeleteStream(vals, msg)
	case "closeStream":
		// closeStream is sent by some clients (OBS, mobile apps) when ending
		// a stream. It serves the same purpose as deleteStream — we route it
		// to the registered handler so the server can clean up the publisher
		// or subscriber state and free the stream key for reuse.
		if d.OnCloseStream == nil {
			d.log.Debug("ignoring closeStream (no handler registered)")
			return nil
		}
		return d.OnCloseStream(vals, msg)
	case "releaseStream", "FCPublish", "FCUnpublish":
		// OBS/FFmpeg pre-publish commands - treat as no-ops for now
		// These are optional Flash Media Server extensions
		d.log.Debug("ignoring optional command", "name", name)
		return nil
	default:
		// Unknown command – log warning (requirements) then ignore.
		// Capture a short hex preview of payload for debugging.
		preview := previewHex(msg.Payload, 32)
		d.log.Warn("unknown command", "name", name, "len", len(vals), "payload_preview", preview)
		return nil
	}
}

func (d *Dispatcher) currentApp() string {
	if d.appProvider == nil {
		return ""
	}
	return d.appProvider()
}

func (d *Dispatcher) noHandlerErr(name string) error {
	return errors.NewProtocolError("dispatch", fmt.Errorf("no handler registered for command %q", name))
}

// previewHex returns a small hex string of the first n bytes of b.
func previewHex(b []byte, n int) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) > n {
		b = b[:n]
	}
	var buf bytes.Buffer
	for i, by := range b {
		if i > 0 {
			buf.WriteByte(' ')
		}
		fmt.Fprintf(&buf, "%02x", by)
	}
	return buf.String()
}

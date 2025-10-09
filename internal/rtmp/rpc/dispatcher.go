package rpc

// Command dispatcher (T040)
//
// The dispatcher is responsible for:
//   1. Determining the RTMP command name from an AMF0 command message (type 20)
//   2. Parsing the command into the appropriate strongly-typed struct using
//      the existing Parse* helpers (connect, createStream, publish, play)
//   3. Invoking the registered handler for that command name.
//   4. Logging and safely ignoring unknown commands (optionally a future
//      enhancement could emit an "_error" response – out of scope for now).
//
// Design notes / assumptions:
//   * We only support AMF0 command messages (TypeID=20) per current feature set.
//   * For publish / play parsing we need the application (app) name negotiated
//     during the connect command. Instead of tightly coupling to a Session
//     type (not yet implemented in earlier tasks) we accept an appProvider
//     callback so tests or higher layers can supply the current application
//     name lazily.
//   * deleteStream is routed (if a handler is provided) but not parsed into a
//     dedicated struct yet – it receives the raw decoded AMF value slice so
//     the handler can perform ad‑hoc extraction.
//
// Error handling:
//   * Parsing errors or handler errors are returned to the caller – the caller
//     decides whether to terminate the connection or send an _error response.
//   * Unknown commands return nil (non-fatal) after logging a warning.

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
)

// Dispatcher routes AMF0 command messages to registered handlers.
type Dispatcher struct {
	appProvider func() string

	OnConnect      ConnectHandler
	OnCreateStream CreateStreamHandler
	OnPublish      PublishHandler
	OnPlay         PlayHandler
	OnDeleteStream DeleteStreamHandler

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
		if d.OnConnect == nil {
			return d.noHandlerErr(name)
		}
		cc, err := ParseConnectCommand(msg)
		if err != nil {
			return err
		}
		return d.OnConnect(cc, msg)
	case "createStream":
		if d.OnCreateStream == nil {
			return d.noHandlerErr(name)
		}
		cs, err := ParseCreateStreamCommand(msg)
		if err != nil {
			return err
		}
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

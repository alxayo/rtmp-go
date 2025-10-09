package rpc

import (
	"fmt"

	"github.com/alxayo/go-rtmp/internal/errors"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// PlayCommand represents a parsed "play" command.
// Spec form (subset we care about): ["play", 0, null, streamName, start, duration, reset]
// Only streamName is strictly required for our current feature scope.
type PlayCommand struct {
	App        string        // application name (passed in separately, from session)
	StreamName string        // raw stream name component
	StreamKey  string        // full key: app/streamName
	Start      int64         // -2=live, -1=recorded, >=0 offset (seconds)
	Duration   int64         // duration if provided (seconds), -1 if not provided
	Reset      bool          // reset flag if provided
	RawValues  []interface{} // retained for debugging / future use
}

// ParsePlayCommand parses an RTMP AMF0 command message assumed to contain a
// "play" invocation. The caller must supply the current application name (from
// the connect command) so we can construct the full stream key.
//
// Expected AMF0 sequence (indices):
//
//	0: "play" (string)
//	1: transaction ID (number, typically 0) - ignored
//	2: null (command object placeholder) - ignored
//	3: streamName (string) - required
//	4: start (number) optional
//	5: duration (number) optional
//	6: reset (boolean) optional
func ParsePlayCommand(msg *chunk.Message, app string) (*PlayCommand, error) {
	if msg == nil {
		return nil, errors.NewProtocolError("play.parse", fmt.Errorf("nil message"))
	}
	if msg.TypeID != commandMessageAMF0TypeID {
		return nil, errors.NewProtocolError("play.parse", fmt.Errorf("unexpected message type %d", msg.TypeID))
	}
	vals, err := amf.DecodeAll(msg.Payload)
	if err != nil {
		return nil, errors.NewProtocolError("play.parse.decode", err)
	}
	if len(vals) < 4 { // need at least command, trx, null, streamName
		return nil, errors.NewProtocolError("play.parse", fmt.Errorf("expected >=4 AMF values, got %d", len(vals)))
	}

	// 0: command name
	name, ok := vals[0].(string)
	if !ok || name != "play" {
		return nil, errors.NewProtocolError("play.parse", fmt.Errorf("first value must be string 'play'"))
	}

	// 3: streamName
	streamName, ok := vals[3].(string)
	if !ok || streamName == "" {
		return nil, errors.NewProtocolError("play.parse", fmt.Errorf("missing stream name"))
	}

	pc := &PlayCommand{App: app, StreamName: streamName, StreamKey: fmt.Sprintf("%s/%s", app, streamName), RawValues: vals}

	// Optional arguments
	if len(vals) >= 5 {
		if v, ok := vals[4].(float64); ok { // start
			pc.Start = int64(v)
		} else {
			// Default start if not provided / wrong type: -2 (live) per common practice.
			pc.Start = -2
		}
	} else {
		pc.Start = -2
	}
	if len(vals) >= 6 {
		if v, ok := vals[5].(float64); ok { // duration
			pc.Duration = int64(v)
		} else {
			pc.Duration = -1
		}
	} else {
		pc.Duration = -1
	}
	if len(vals) >= 7 {
		if v, ok := vals[6].(bool); ok {
			pc.Reset = v
		}
	}

	return pc, nil
}

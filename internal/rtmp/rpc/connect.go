package rpc

import (
	"fmt"

	"github.com/alxayo/go-rtmp/internal/errors"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// RTMP message type ID for AMF0 command messages.
const commandMessageAMF0TypeID = 20

// CommandMessageAMF0TypeIDForTest exposes the command message type id (20)
// to other packages that need to build AMF0 command messages (e.g. server
// handlers) without exporting the constant itself. Kept small to avoid
// broadening the public API surface prematurely.
func CommandMessageAMF0TypeIDForTest() uint8 { return commandMessageAMF0TypeID }

// ConnectCommand represents the parsed contents of a "connect" command.
// Only the fields required by our current implementation scope are captured.
type ConnectCommand struct {
	TransactionID    float64
	App              string
	FlashVer         string
	TcURL            string
	ObjectEncoding   float64                // must be 0 (AMF0)
	RawCommandObject map[string]interface{} // retained for any future optional fields
}

// ParseConnectCommand parses an RTMP command message payload (type 20) assumed
// to contain a "connect" command. It validates required fields and returns a
// structured ConnectCommand. Errors are wrapped as protocol errors.
func ParseConnectCommand(msg *chunk.Message) (*ConnectCommand, error) {
	if msg == nil {
		return nil, errors.NewProtocolError("connect.parse", fmt.Errorf("nil message"))
	}
	if msg.TypeID != commandMessageAMF0TypeID {
		return nil, errors.NewProtocolError("connect.parse", fmt.Errorf("unexpected message type %d", msg.TypeID))
	}

	vals, err := amf.DecodeAll(msg.Payload)
	if err != nil {
		return nil, errors.NewProtocolError("connect.parse.decode", err)
	}
	// Expect at least 3 values: command name, transaction ID, command object
	if len(vals) < 3 {
		return nil, errors.NewProtocolError("connect.parse", fmt.Errorf("expected >=3 AMF values, got %d", len(vals)))
	}

	// 1. Command name
	name, ok := vals[0].(string)
	if !ok || name != "connect" {
		return nil, errors.NewProtocolError("connect.parse", fmt.Errorf("first value must be string 'connect'"))
	}

	// 2. Transaction ID (AMF0 Number)
	trx, ok := vals[1].(float64)
	if !ok {
		return nil, errors.NewProtocolError("connect.parse", fmt.Errorf("second value must be number transaction ID"))
	}

	// 3. Command object (AMF0 Object)
	obj, ok := vals[2].(map[string]interface{})
	if !ok {
		return nil, errors.NewProtocolError("connect.parse", fmt.Errorf("third value must be object commandObject"))
	}

	cc := &ConnectCommand{TransactionID: trx, RawCommandObject: obj}

	// Extract required fields
	if v, ok := obj["app"]; ok {
		if s, ok := v.(string); ok {
			cc.App = s
		}
	}
	if v, ok := obj["flashVer"]; ok {
		if s, ok := v.(string); ok {
			cc.FlashVer = s
		}
	}
	if v, ok := obj["tcUrl"]; ok {
		if s, ok := v.(string); ok {
			cc.TcURL = s
		}
	}
	if v, ok := obj["objectEncoding"]; ok {
		if n, ok := v.(float64); ok {
			cc.ObjectEncoding = n
		}
	}

	// Validation
	if cc.App == "" {
		return nil, errors.NewProtocolError("connect.validate", fmt.Errorf("app field required"))
	}
	if cc.ObjectEncoding != 0 { // only AMF0 supported
		return nil, errors.NewProtocolError("connect.validate", fmt.Errorf("unsupported objectEncoding %.0f (only 0 supported)", cc.ObjectEncoding))
	}

	return cc, nil
}

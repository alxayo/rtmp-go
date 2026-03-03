package rpc

import (
	"fmt"

	"github.com/alxayo/go-rtmp/internal/errors"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
	"github.com/alxayo/go-rtmp/internal/rtmp/server/auth"
)

// PublishCommand represents a parsed "publish" command.
// Spec form: ["publish", 0, null, publishingName, publishingType]
// The stream key is constructed as app + "/" + cleanName (without query params).
type PublishCommand struct {
	PublishingName string            // clean name without query params (e.g. "mystream")
	PublishingType string            // one of: live|record|append
	StreamKey      string            // app/publishingName (e.g. "live/mystream")
	QueryParams    map[string]string // parsed from raw name (e.g. {"token": "abc123"})
}

// ParsePublishCommand parses an AMF0 command message assumed to contain a
// publish invocation. The caller must supply the application name (app) that
// was negotiated during the connect command so the full stream key can be
// constructed.
// Expected AMF0 sequence:
// 0: string "publish"
// 1: number 0 (transaction id is always 0 for publish in practice - ignored)
// 2: null
// 3: string publishingName
// 4: string publishingType (live|record|append)
func ParsePublishCommand(app string, msg *chunk.Message) (*PublishCommand, error) {
	if msg == nil {
		return nil, errors.NewProtocolError("publish.parse", fmt.Errorf("nil message"))
	}
	if msg.TypeID != commandMessageAMF0TypeID { // must be AMF0 command message (type 20)
		return nil, errors.NewProtocolError("publish.parse", fmt.Errorf("unexpected message type %d", msg.TypeID))
	}
	if app == "" {
		return nil, errors.NewProtocolError("publish.parse", fmt.Errorf("app required to build stream key"))
	}

	vals, err := amf.DecodeAll(msg.Payload)
	if err != nil {
		return nil, errors.NewProtocolError("publish.parse.decode", err)
	}
	// Need at least 5 values per spec
	if len(vals) < 5 {
		return nil, errors.NewProtocolError("publish.parse", fmt.Errorf("expected >=5 AMF values, got %d", len(vals)))
	}

	// 0: command name
	name, ok := vals[0].(string)
	if !ok || name != "publish" {
		return nil, errors.NewProtocolError("publish.parse", fmt.Errorf("first value must be string 'publish'"))
	}

	// 3: publishingName (may contain query params like "mystream?token=abc")
	rawName, ok := vals[3].(string)
	if !ok {
		return nil, errors.NewProtocolError("publish.parse", fmt.Errorf("publishingName must be string"))
	}
	// Parse query parameters from the stream name (e.g. "mystream?token=abc").
	// Empty names default to "default" (some clients send empty string).
	if rawName == "" {
		rawName = "default"
	}
	parsed := auth.ParseStreamURL(rawName)
	publishingName := parsed.StreamName

	// 4: publishingType
	publishingType, ok := vals[4].(string)
	if !ok || publishingType == "" {
		return nil, errors.NewProtocolError("publish.parse", fmt.Errorf("publishingType required"))
	}
	switch publishingType {
	case "live", "record", "append":
		// valid
	default:
		return nil, errors.NewProtocolError("publish.parse", fmt.Errorf("unsupported publishingType %q", publishingType))
	}

	return &PublishCommand{
		PublishingName: publishingName,
		PublishingType: publishingType,
		StreamKey:      app + "/" + publishingName,
		QueryParams:    parsed.QueryParams,
	}, nil
}

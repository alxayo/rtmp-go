package rpc

// reconnect.go implements the E-RTMP v2 Reconnect Request feature.
//
// The RTMP server can ask a client to gracefully disconnect and reconnect
// by sending an onStatus command with the status code
// "NetConnection.Connect.ReconnectRequest". This is useful for:
//   - Server maintenance (drain connections before shutting down)
//   - Load balancing (redirect clients to a different server via tcUrl)
//   - Graceful restarts (clients reconnect automatically)

import (
	"fmt"

	"github.com/alxayo/go-rtmp/internal/errors"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// BuildReconnectRequest builds an onStatus command message that tells the
// client to reconnect. This implements the E-RTMP v2 ReconnectRequest feature.
//
// Per the E-RTMP v2 spec, the server sends:
//
//	onStatus(0, null, {
//	    level:       "status",
//	    code:        "NetConnection.Connect.ReconnectRequest",
//	    description: "Server maintenance",
//	    tcUrl:       "rtmp://new-server/app"   // optional redirect
//	})
//
// Parameters:
//   - tcUrl: optional redirect URL. If non-empty, the client should reconnect
//     to this URL instead of the original one. If empty, the client reconnects
//     to the same server it was previously connected to.
//   - description: human-readable reason for the reconnect request
//     (e.g., "Server maintenance — please reconnect").
//
// Returns a chunk.Message ready to send via Connection.SendMessage().
// The message uses CSID 3 (command stream), TypeID 20 (AMF0 command),
// and MessageStreamID 0 (connection-level, not tied to a specific stream).
func BuildReconnectRequest(tcUrl string, description string) (*chunk.Message, error) {
	// Build the info object that carries the reconnect status.
	// "level" and "code" are required by the RTMP onStatus convention.
	// "description" provides a human-readable explanation for logging/UI.
	info := map[string]interface{}{
		"level":       "status",
		"code":        "NetConnection.Connect.ReconnectRequest",
		"description": description,
	}

	// Only include tcUrl if a redirect is specified. When tcUrl is empty,
	// clients should reconnect to the same server they were connected to.
	if tcUrl != "" {
		info["tcUrl"] = tcUrl
	}

	// Encode the AMF0 payload: ["onStatus", 0.0, null, info]
	// - "onStatus" is the command name (tells the client this is a status event)
	// - 0.0 is the transaction ID (0 = no response expected from the client)
	// - nil encodes as AMF0 null (no command object, per onStatus convention)
	// - info is the status object with the reconnect details
	payload, err := amf.EncodeAll("onStatus", 0.0, nil, info)
	if err != nil {
		return nil, errors.NewProtocolError("reconnect.request.encode", fmt.Errorf("amf encode: %w", err))
	}

	// Return a fully formed chunk.Message ready for SendMessage().
	return &chunk.Message{
		CSID:            3,                       // Command messages use CSID 3 per RTMP conventions
		TypeID:          commandMessageAMF0TypeID, // 20 = AMF0 command message
		MessageStreamID: 0,                        // Connection-level message (not stream-specific)
		Payload:         payload,
		MessageLength:   uint32(len(payload)),
	}, nil
}

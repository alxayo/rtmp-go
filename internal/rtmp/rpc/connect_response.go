package rpc

import (
	"fmt"

	"github.com/alxayo/go-rtmp/internal/errors"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// BuildConnectResponse builds the standard _result response for a successful
// connect command. It returns an RTMP AMF0 command message (type 20) with the
// following structure:
// ["_result", transactionID, properties:Object, information:Object]
//
// properties fields:
//
//	fmsVer:       string (flash media server version string)
//	capabilities: number (capabilities bitmask - we expose a conventional 31)
//	mode:         number (1 per observed implementations)
//
// information fields:
//
//	level:       "status"
//	code:        "NetConnection.Connect.Success"
//	description: caller provided description
//
// The returned message uses MessageStreamID=0 (connection level). CSID is left
// as zero here; actual assignment (typically 3 for command) is handled by the
// chunk writer layer when serialising for the wire.
func BuildConnectResponse(transactionID float64, description string) (*chunk.Message, error) {
	props := map[string]interface{}{
		"fmsVer":       "FMS/3,5,7,7009", // common version string used by many simple servers
		"capabilities": 31.0,
		"mode":         1.0,
	}

	info := map[string]interface{}{
		"level":       "status",
		"code":        "NetConnection.Connect.Success",
		"description": description,
	}

	payload, err := amf.EncodeAll("_result", transactionID, props, info)
	if err != nil {
		return nil, errors.NewProtocolError("connect.response.encode", fmt.Errorf("amf encode: %w", err))
	}

	return &chunk.Message{
		// CSID intentionally 0 (unset) – writer will decide actual chunk stream (usually 3)
		TypeID:          commandMessageAMF0TypeID,
		MessageStreamID: 0,
		Payload:         payload,
		MessageLength:   uint32(len(payload)),
	}, nil
}

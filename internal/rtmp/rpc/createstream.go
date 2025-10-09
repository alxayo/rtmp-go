package rpc

import (
	"fmt"

	"github.com/alxayo/go-rtmp/internal/errors"
	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// CreateStreamCommand represents a parsed "createStream" command.
// Spec form: ["createStream", transactionID, null]
type CreateStreamCommand struct {
	TransactionID float64
}

// ParseCreateStreamCommand parses an AMF0 command message assumed to contain
// a createStream invocation. Expected AMF0 sequence:
//
//	0: string "createStream"
//	1: number transactionID
//	2: null (ignored)
func ParseCreateStreamCommand(msg *chunk.Message) (*CreateStreamCommand, error) {
	if msg == nil {
		return nil, errors.NewProtocolError("createstream.parse", fmt.Errorf("nil message"))
	}
	if msg.TypeID != commandMessageAMF0TypeID { // must be AMF0 command message (type 20)
		return nil, errors.NewProtocolError("createstream.parse", fmt.Errorf("unexpected message type %d", msg.TypeID))
	}

	vals, err := amf.DecodeAll(msg.Payload)
	if err != nil {
		return nil, errors.NewProtocolError("createstream.parse.decode", err)
	}
	if len(vals) < 3 { // need at least 3 values per spec
		return nil, errors.NewProtocolError("createstream.parse", fmt.Errorf("expected >=3 AMF values, got %d", len(vals)))
	}

	// 0: command name
	name, ok := vals[0].(string)
	if !ok || name != "createStream" {
		return nil, errors.NewProtocolError("createstream.parse", fmt.Errorf("first value must be string 'createStream'"))
	}

	// 1: transaction ID (number)
	trx, ok := vals[1].(float64)
	if !ok {
		return nil, errors.NewProtocolError("createstream.parse", fmt.Errorf("second value must be number transaction ID"))
	}

	// 2: null is ignored; we just ensure it's either nil or explicitly null marker decoded as nil.
	// No validation required beyond presence since earlier len(vals) check ensures index exists.

	return &CreateStreamCommand{TransactionID: trx}, nil
}

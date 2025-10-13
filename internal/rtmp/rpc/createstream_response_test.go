package rpc

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/amf"
)

func TestBuildCreateStreamResponse_EncodesStructure(t *testing.T) {
	alloc := NewStreamIDAllocator()
	msg, sid, err := BuildCreateStreamResponse(5.0, alloc)
	if err != nil {
		t.Fatalf("BuildCreateStreamResponse error: %v", err)
	}
	if sid != 1 {
		t.Fatalf("expected allocated stream id 1, got %d", sid)
	}
	if msg.TypeID != commandMessageAMF0TypeID {
		t.Fatalf("unexpected TypeID %d", msg.TypeID)
	}

	vals, err := amf.DecodeAll(msg.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(vals) != 4 {
		t.Fatalf("expected 4 AMF values, got %d", len(vals))
	}
	if name, ok := vals[0].(string); !ok || name != "_result" {
		t.Fatalf("first value not _result: %#v", vals[0])
	}
	if trx, ok := vals[1].(float64); !ok || trx != 5.0 {
		t.Fatalf("transaction id mismatch: %#v", vals[1])
	}
	if vals[2] != nil { // null should decode to nil
		t.Fatalf("third value expected nil, got %#v", vals[2])
	}
	if id, ok := vals[3].(float64); !ok || id != 1.0 {
		t.Fatalf("fourth value expected 1.0 stream id, got %#v", vals[3])
	}
}

func TestBuildCreateStreamResponse_SequentialIDs(t *testing.T) {
	alloc := NewStreamIDAllocator()
	// First allocation
	_, sid1, err := BuildCreateStreamResponse(1.0, alloc)
	if err != nil {
		t.Fatalf("first build error: %v", err)
	}
	// Second allocation to exercise allocator path for coverage
	_, sid2, err := BuildCreateStreamResponse(2.0, alloc)
	if err != nil {
		t.Fatalf("second build error: %v", err)
	}
	if sid1 != 1 || sid2 != 2 {
		t.Fatalf("expected stream ids 1 then 2, got %d then %d", sid1, sid2)
	}
}

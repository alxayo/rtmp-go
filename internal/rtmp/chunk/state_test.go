package chunk

import (
	"testing"
)

// helper to build headers quickly
func h(fmt uint8, csid uint32, ts uint32, ml uint32, mt uint8, msid uint32) *ChunkHeader {
	return &ChunkHeader{FMT: fmt, CSID: csid, Timestamp: ts, MessageLength: ml, MessageTypeID: mt, MessageStreamID: msid}
}

func TestChunkStreamState_Flow(t *testing.T) {
	t.Run("fmt0_single_chunk_complete", func(t *testing.T) {
		var s ChunkStreamState
		h0 := h(0, 5, 100, 5, 8, 1)
		if err := s.ApplyHeader(h0); err != nil {
			t.Fatalf("apply fmt0: %v", err)
		}
		complete, msg, err := s.AppendChunkData([]byte{1, 2, 3, 4, 5})
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		if !complete || msg == nil {
			t.Fatalf("expected complete message")
		}
		if msg.Timestamp != 100 || msg.MessageLength != 5 || msg.TypeID != 8 || msg.MessageStreamID != 1 {
			t.Fatalf("unexpected msg fields: %+v", msg)
		}
		if len(msg.Payload) != 5 {
			t.Fatalf("payload len mismatch")
		}
	})

	t.Run("fmt0_multi_chunk_with_fmt3", func(t *testing.T) {
		var s ChunkStreamState
		h0 := h(0, 6, 200, 8, 9, 2)
		if err := s.ApplyHeader(h0); err != nil {
			t.Fatalf("apply fmt0: %v", err)
		}
		if complete, _, _ := s.AppendChunkData([]byte{1, 2, 3}); complete {
			t.Fatalf("should not be complete yet")
		}
		h3 := h(3, 6, 0, 0, 0, 0) // continuation
		if err := s.ApplyHeader(h3); err != nil {
			t.Fatalf("apply fmt3: %v", err)
		}
		complete, msg, err := s.AppendChunkData([]byte{4, 5, 6, 7, 8})
		if err != nil {
			t.Fatalf("append: %v", err)
		}
		if !complete || msg == nil {
			t.Fatalf("expected complete")
		}
		if msg.Timestamp != 200 || msg.MessageLength != 8 {
			t.Fatalf("unexpected msg fields: %+v", msg)
		}
	})

	t.Run("fmt1_delta_new_message", func(t *testing.T) {
		var s ChunkStreamState
		// First message
		if err := s.ApplyHeader(h(0, 7, 300, 4, 18, 3)); err != nil {
			t.Fatalf("fmt0: %v", err)
		}
		if complete, _, err := s.AppendChunkData([]byte{1, 2, 3, 4}); err != nil || !complete {
			t.Fatalf("first complete err=%v", err)
		}
		// Second message via FMT1: delta 50, new length 3, new type 20, same stream id
		if err := s.ApplyHeader(h(1, 7, 50, 3, 20, 0)); err != nil {
			t.Fatalf("fmt1: %v", err)
		}
		if s.LastTimestamp != 350 {
			t.Fatalf("expected timestamp 350 got %d", s.LastTimestamp)
		}
		complete, msg, err := s.AppendChunkData([]byte{9, 9, 9})
		if err != nil || !complete {
			t.Fatalf("append err=%v complete=%v", err, complete)
		}
		if msg.TypeID != 20 || msg.MessageLength != 3 {
			t.Fatalf("unexpected msg fields: %+v", msg)
		}
	})

	t.Run("fmt2_delta_only_new_message", func(t *testing.T) {
		var s ChunkStreamState
		// Seed state
		if err := s.ApplyHeader(h(0, 8, 400, 6, 11, 4)); err != nil {
			t.Fatalf("fmt0: %v", err)
		}
		if complete, _, err := s.AppendChunkData([]byte{1, 2, 3, 4, 5, 6}); err != nil || !complete {
			t.Fatalf("first complete err=%v", err)
		}
		// FMT2 new message with delta 25, same length/type/stream
		if err := s.ApplyHeader(h(2, 8, 25, 0, 0, 0)); err != nil {
			t.Fatalf("fmt2: %v", err)
		}
		if s.LastTimestamp != 425 {
			t.Fatalf("expected ts 425 got %d", s.LastTimestamp)
		}
		// Re-declare length must stay same
		if s.LastMsgLength != 6 || s.LastMsgTypeID != 11 || s.LastMsgStreamID != 4 {
			t.Fatalf("fields not reused")
		}
		complete, _, err := s.AppendChunkData([]byte{7, 7, 7})
		if complete {
			t.Fatalf("should not complete yet")
		}
		complete, msg, err := s.AppendChunkData([]byte{7, 7, 7})
		if err != nil || !complete {
			t.Fatalf("final append err=%v complete=%v", err, complete)
		}
		if msg.Timestamp != 425 {
			t.Fatalf("msg timestamp mismatch")
		}
	})

	t.Run("fmt3_without_state_error", func(t *testing.T) {
		var s ChunkStreamState
		if err := s.ApplyHeader(h(3, 9, 0, 0, 0, 0)); err == nil {
			t.Fatalf("expected error for fmt3 without state")
		}
	})

	t.Run("overflow_error", func(t *testing.T) {
		var s ChunkStreamState
		if err := s.ApplyHeader(h(0, 10, 0, 4, 15, 1)); err != nil {
			t.Fatalf("fmt0: %v", err)
		}
		if _, _, err := s.AppendChunkData([]byte{1, 2, 3, 4, 5}); err == nil {
			t.Fatalf("expected overflow error")
		}
	})

	t.Run("fmt1_without_prior_state_error", func(t *testing.T) {
		var s ChunkStreamState
		if err := s.ApplyHeader(h(1, 11, 5, 3, 18, 0)); err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("fmt2_without_prior_state_error", func(t *testing.T) {
		var s ChunkStreamState
		if err := s.ApplyHeader(h(2, 12, 5, 0, 0, 0)); err == nil {
			t.Fatalf("expected error")
		}
	})
}

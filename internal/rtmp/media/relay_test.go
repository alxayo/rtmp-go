package media

import (
	"testing"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

type fakeSubscriber struct {
	received []*chunk.Message
	failSend bool
}

func (f *fakeSubscriber) SendMessage(m *chunk.Message) error {
	if f.failSend {
		return nil // simulate blocked internally; message effectively dropped
	}
	f.received = append(f.received, m)
	return nil
}

// Implement non‑blocking interface for backpressure simulation.
func (f *fakeSubscriber) TrySendMessage(m *chunk.Message) bool {
	if f.failSend {
		return false
	}
	f.received = append(f.received, m)
	return true
}

// helper to create a media message (audio=8, video=9)
func mkMsg(typeID uint8, payload []byte) *chunk.Message {
	return &chunk.Message{TypeID: typeID, Payload: payload, MessageLength: uint32(len(payload))}
}

func TestRelaySingleSubscriber(t *testing.T) {
	stream := NewStream("app/solo")
	sub := &fakeSubscriber{}
	stream.AddSubscriber(sub)

	msg := mkMsg(8, []byte{0xAF, 0x00, 0x11, 0x22}) // AAC sequence header (soundFormat=10)
	stream.BroadcastMessage(&CodecDetector{}, msg, NullLogger())

	if len(sub.received) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sub.received))
	}
	if stream.GetAudioCodec() != AudioCodecAAC {
		t.Fatalf("expected audio codec AAC, got %s", stream.GetAudioCodec())
	}
}

func TestRelayMultipleSubscribers(t *testing.T) {
	stream := NewStream("app/multi")
	s1 := &fakeSubscriber{}
	s2 := &fakeSubscriber{}
	s3 := &fakeSubscriber{}
	stream.AddSubscriber(s1)
	stream.AddSubscriber(s2)
	stream.AddSubscriber(s3)

	msg := mkMsg(9, []byte{0x17, 0x00, 0x01, 0x02, 0x03}) // AVC keyframe sequence header (codecID=7)
	stream.BroadcastMessage(&CodecDetector{}, msg, NullLogger())

	for i, s := range []*fakeSubscriber{s1, s2, s3} {
		if len(s.received) != 1 {
			t.Fatalf("subscriber %d expected 1 message, got %d", i+1, len(s.received))
		}
	}
	if stream.GetVideoCodec() != VideoCodecAVC {
		t.Fatalf("expected video codec H264, got %s", stream.GetVideoCodec())
	}
}

func TestRelaySlowSubscriberDropped(t *testing.T) {
	stream := NewStream("app/backpressure")
	slow := &fakeSubscriber{failSend: true}
	fast := &fakeSubscriber{}
	stream.AddSubscriber(slow)
	stream.AddSubscriber(fast)

	msg := mkMsg(8, []byte{0xAF, 0x01, 0xAA, 0xBB}) // AAC raw frame
	stream.BroadcastMessage(&CodecDetector{}, msg, NullLogger())

	if len(fast.received) != 1 {
		t.Fatalf("fast subscriber expected 1 message, got %d", len(fast.received))
	}
	if len(slow.received) != 0 {
		t.Fatalf("slow subscriber should have 0 received (dropped), got %d", len(slow.received))
	}
}

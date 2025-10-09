package conn

import "testing"

func TestSessionTransactionIDIncrement(t *testing.T) {
	s := NewSession()
	if got := s.TransactionID(); got != 1 {
		t.Fatalf("initial transactionID = %d, want 1", got)
	}
	next := s.NextTransactionID()
	if next != 2 {
		t.Fatalf("after first NextTransactionID got %d, want 2", next)
	}
	next = s.NextTransactionID()
	if next != 3 {
		t.Fatalf("after second NextTransactionID got %d, want 3", next)
	}
}

func TestSessionAllocateStreamID(t *testing.T) {
	s := NewSession()
	s.SetConnectInfo("live", "rtmp://example/live", "FMLE/3.0", 0)
	if s.State() != SessionStateConnected {
		t.Fatalf("expected state Connected, got %v", s.State())
	}
	id1 := s.AllocateStreamID()
	if id1 != 1 {
		t.Fatalf("first stream id = %d, want 1", id1)
	}
	if s.State() != SessionStateStreamCreated {
		t.Fatalf("expected state StreamCreated after allocation, got %v", s.State())
	}
	id2 := s.AllocateStreamID()
	if id2 != 2 {
		t.Fatalf("second stream id = %d, want 2", id2)
	}
}

func TestSessionSetStreamKey(t *testing.T) {
	s := NewSession()
	s.SetConnectInfo("live", "rtmp://example/live", "FMLE/3.0", 0)
	s.AllocateStreamID()
	key := s.SetStreamKey("live", "testStream")
	want := "live/testStream"
	if key != want || s.StreamKey() != want {
		t.Fatalf("stream key = %q, want %q", key, want)
	}
	if s.State() != SessionStatePublishing { // placeholder state set in SetStreamKey
		t.Fatalf("expected state Publishing placeholder, got %v", s.State())
	}
}

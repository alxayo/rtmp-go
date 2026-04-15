package packet

import (
	"bytes"
	"testing"
)

// TestControlPacket_RoundTrip verifies marshal→unmarshal round-trip for
// various control packet types.
func TestControlPacket_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		pkt  ControlPacket
	}{
		{
			name: "handshake",
			pkt: ControlPacket{
				Header: Header{
					IsControl:    true,
					Timestamp:    1000,
					DestSocketID: 0,
				},
				Type:         CtrlHandshake,
				Subtype:      0,
				TypeSpecific: 0,
				CIF:          []byte{0x01, 0x02, 0x03, 0x04},
			},
		},
		{
			name: "keepalive_no_cif",
			pkt: ControlPacket{
				Header: Header{
					IsControl:    true,
					Timestamp:    500000,
					DestSocketID: 42,
				},
				Type:         CtrlKeepAlive,
				Subtype:      0,
				TypeSpecific: 0,
				CIF:          nil,
			},
		},
		{
			name: "ack_with_type_specific",
			pkt: ControlPacket{
				Header: Header{
					IsControl:    true,
					Timestamp:    99999,
					DestSocketID: 0xABCD1234,
				},
				Type:         CtrlACK,
				Subtype:      0,
				TypeSpecific: 7, // ACK sequence number
				CIF:          make([]byte, 28), // ACK CIF placeholder
			},
		},
		{
			name: "nak",
			pkt: ControlPacket{
				Header: Header{
					IsControl:    true,
					Timestamp:    0xFFFFFFFF,
					DestSocketID: 0xFFFFFFFF,
				},
				Type:         CtrlNAK,
				Subtype:      0,
				TypeSpecific: 0,
				CIF:          []byte{0x80, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x05},
			},
		},
		{
			name: "shutdown",
			pkt: ControlPacket{
				Header: Header{
					IsControl:    true,
					Timestamp:    0,
					DestSocketID: 1,
				},
				Type:         CtrlShutdown,
				Subtype:      0,
				TypeSpecific: 0,
				CIF:          nil,
			},
		},
		{
			name: "ackack",
			pkt: ControlPacket{
				Header: Header{
					IsControl:    true,
					Timestamp:    12345,
					DestSocketID: 9999,
				},
				Type:         CtrlACKACK,
				Subtype:      0,
				TypeSpecific: 7, // Acknowledges ACK #7
				CIF:          nil,
			},
		},
		{
			name: "drop_request",
			pkt: ControlPacket{
				Header: Header{
					IsControl:    true,
					Timestamp:    55555,
					DestSocketID: 100,
				},
				Type:         CtrlDropReq,
				Subtype:      0,
				TypeSpecific: 0,
				CIF:          []byte{0x00, 0x00, 0x00, 0x0A, 0x00, 0x00, 0x00, 0x14},
			},
		},
		{
			name: "peer_error",
			pkt: ControlPacket{
				Header: Header{
					IsControl:    true,
					Timestamp:    77777,
					DestSocketID: 200,
				},
				Type:         CtrlPeerError,
				Subtype:      0,
				TypeSpecific: 4001, // Error code
				CIF:          nil,
			},
		},
		{
			name: "user_defined_kmreq",
			pkt: ControlPacket{
				Header: Header{
					IsControl:    true,
					Timestamp:    88888,
					DestSocketID: 300,
				},
				Type:         CtrlUserDefined,
				Subtype:      0,
				TypeSpecific: UserSubtypeKMREQ, // KMREQ subtype
				CIF:          []byte{0xDE, 0xAD, 0xBE, 0xEF},
			},
		},
		{
			name: "user_defined_kmrsp",
			pkt: ControlPacket{
				Header: Header{
					IsControl:    true,
					Timestamp:    99999,
					DestSocketID: 400,
				},
				Type:         CtrlUserDefined,
				Subtype:      0,
				TypeSpecific: UserSubtypeKMRSP, // KMRSP subtype
				CIF:          []byte{0xCA, 0xFE, 0xBA, 0xBE},
			},
		},
		{
			name: "with_subtype",
			pkt: ControlPacket{
				Header: Header{
					IsControl:    true,
					Timestamp:    10,
					DestSocketID: 20,
				},
				Type:         CtrlCongestion,
				Subtype:      0x1234,
				TypeSpecific: 0xDEADBEEF,
				CIF:          []byte{0xCA, 0xFE},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal the packet to wire format.
			data, err := tt.pkt.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary failed: %v", err)
			}

			// Verify the F bit is 1 (control packet).
			if data[0]&0x80 == 0 {
				t.Error("F bit should be 1 for control packet")
			}

			// Unmarshal the wire data back into a packet.
			got, err := UnmarshalControlPacket(data)
			if err != nil {
				t.Fatalf("UnmarshalControlPacket failed: %v", err)
			}

			// Compare every field.
			if got.Type != tt.pkt.Type {
				t.Errorf("Type: got %d, want %d", got.Type, tt.pkt.Type)
			}
			if got.Subtype != tt.pkt.Subtype {
				t.Errorf("Subtype: got %d, want %d", got.Subtype, tt.pkt.Subtype)
			}
			if got.TypeSpecific != tt.pkt.TypeSpecific {
				t.Errorf("TypeSpecific: got %d, want %d", got.TypeSpecific, tt.pkt.TypeSpecific)
			}
			if got.Timestamp != tt.pkt.Timestamp {
				t.Errorf("Timestamp: got %d, want %d", got.Timestamp, tt.pkt.Timestamp)
			}
			if got.DestSocketID != tt.pkt.DestSocketID {
				t.Errorf("DestSocketID: got %d, want %d", got.DestSocketID, tt.pkt.DestSocketID)
			}
			if !bytes.Equal(got.CIF, tt.pkt.CIF) {
				t.Errorf("CIF mismatch: got %d bytes, want %d bytes", len(got.CIF), len(tt.pkt.CIF))
			}
		})
	}
}

// TestUnmarshalControlPacket_DataBit verifies that UnmarshalControlPacket
// returns an error when given a data packet (F bit = 0).
func TestUnmarshalControlPacket_DataBit(t *testing.T) {
	buf := make([]byte, 16)
	// F bit = 0 → data packet, not control
	_, err := UnmarshalControlPacket(buf)
	if err == nil {
		t.Error("expected error for data packet, got nil")
	}
}

// TestUnmarshalControlPacket_TooShort verifies error handling for short buffers.
func TestUnmarshalControlPacket_TooShort(t *testing.T) {
	_, err := UnmarshalControlPacket(make([]byte, 10))
	if err == nil {
		t.Error("expected error for short buffer, got nil")
	}
}

// TestControlPacket_WireFormat verifies the exact byte layout of a known
// control packet to ensure wire compatibility.
func TestControlPacket_WireFormat(t *testing.T) {
	pkt := ControlPacket{
		Header: Header{
			IsControl:    true,
			Timestamp:    0x00000064, // 100
			DestSocketID: 0x00000001,
		},
		Type:         CtrlACK,     // 0x0002
		Subtype:      0,
		TypeSpecific: 0x00000003, // ACK sequence number = 3
	}

	data, err := pkt.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	// Byte 0-3: F=1, Type=0x0002, Subtype=0x0000
	// Binary: 1_000000000000010_0000000000000000
	// = 0x80020000
	if data[0] != 0x80 || data[1] != 0x02 || data[2] != 0x00 || data[3] != 0x00 {
		t.Errorf("bytes 0-3: got %02X %02X %02X %02X, want 80 02 00 00",
			data[0], data[1], data[2], data[3])
	}

	// Byte 4-7: TypeSpecific = 3
	if data[4] != 0x00 || data[5] != 0x00 || data[6] != 0x00 || data[7] != 0x03 {
		t.Errorf("bytes 4-7: got %02X %02X %02X %02X, want 00 00 00 03",
			data[4], data[5], data[6], data[7])
	}
}

// TestControlPacket_AllTypes verifies that all defined control types
// survive a round-trip marshal→unmarshal cycle.
func TestControlPacket_AllTypes(t *testing.T) {
	types := []struct {
		name string
		ct   ControlType
	}{
		{"handshake", CtrlHandshake},
		{"keepalive", CtrlKeepAlive},
		{"ack", CtrlACK},
		{"nak", CtrlNAK},
		{"congestion", CtrlCongestion},
		{"shutdown", CtrlShutdown},
		{"ackack", CtrlACKACK},
		{"dropreq", CtrlDropReq},
		{"peererror", CtrlPeerError},
		{"userdefined", CtrlUserDefined},
	}

	for _, tt := range types {
		t.Run(tt.name, func(t *testing.T) {
			pkt := ControlPacket{
				Header: Header{IsControl: true, Timestamp: 1, DestSocketID: 1},
				Type:   tt.ct,
			}
			data, err := pkt.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary failed: %v", err)
			}
			got, err := UnmarshalControlPacket(data)
			if err != nil {
				t.Fatalf("UnmarshalControlPacket failed: %v", err)
			}
			if got.Type != tt.ct {
				t.Errorf("Type: got %d, want %d", got.Type, tt.ct)
			}
		})
	}
}

// TestControlPacket_UserDefined_WireFormat verifies the exact byte layout
// of a user-defined control packet (type 0x7FFF) with a KMREQ subtype.
// This ensures the maximum 15-bit control type value survives the round trip
// and the TypeSpecific field correctly carries the subtype identifier.
func TestControlPacket_UserDefined_WireFormat(t *testing.T) {
	pkt := ControlPacket{
		Header: Header{
			IsControl:    true,
			Timestamp:    0x00001000,
			DestSocketID: 0x00000005,
		},
		Type:         CtrlUserDefined,  // 0x7FFF
		Subtype:      0,
		TypeSpecific: UserSubtypeKMREQ, // 3
		CIF:          []byte{0x01, 0x02},
	}

	data, err := pkt.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	// Bytes 0-3: F=1, Type=0x7FFF, Subtype=0x0000
	// Binary: 1_111111111111111_0000000000000000
	// = 0xFFFF0000
	if data[0] != 0xFF || data[1] != 0xFF || data[2] != 0x00 || data[3] != 0x00 {
		t.Errorf("bytes 0-3: got %02X %02X %02X %02X, want FF FF 00 00",
			data[0], data[1], data[2], data[3])
	}

	// Bytes 4-7: TypeSpecific = 3 (KMREQ subtype)
	if data[4] != 0x00 || data[5] != 0x00 || data[6] != 0x00 || data[7] != 0x03 {
		t.Errorf("bytes 4-7: got %02X %02X %02X %02X, want 00 00 00 03",
			data[4], data[5], data[6], data[7])
	}

	// Round-trip: unmarshal and verify fields
	got, err := UnmarshalControlPacket(data)
	if err != nil {
		t.Fatalf("UnmarshalControlPacket failed: %v", err)
	}
	if got.Type != CtrlUserDefined {
		t.Errorf("Type: got %d, want %d", got.Type, CtrlUserDefined)
	}
	if got.TypeSpecific != UserSubtypeKMREQ {
		t.Errorf("TypeSpecific: got %d, want %d", got.TypeSpecific, UserSubtypeKMREQ)
	}
	if len(got.CIF) != 2 || got.CIF[0] != 0x01 || got.CIF[1] != 0x02 {
		t.Errorf("CIF: got %v, want [01 02]", got.CIF)
	}
}

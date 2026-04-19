package mkv

import (
	"encoding/binary"
	"math"
	"testing"
)

// ─── ReadVINT tests ─────────────────────────────────────────────────────────
// ReadVINT reads element IDs where the width marker bit is preserved.

func TestReadVINT(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		want  uint64
		width int
	}{
		{
			name:  "1-byte ID 0x81",
			data:  []byte{0x81},
			want:  0x81,
			width: 1,
		},
		{
			name:  "1-byte SimpleBlock ID 0xA3",
			data:  []byte{0xA3},
			want:  0xA3,
			width: 1,
		},
		{
			name:  "2-byte EBMLVersion ID 0x4286",
			data:  []byte{0x42, 0x86},
			want:  0x4286,
			width: 2,
		},
		{
			name:  "3-byte TimecodeScale ID 0x2AD7B1",
			data:  []byte{0x2A, 0xD7, 0xB1},
			want:  0x2AD7B1,
			width: 3,
		},
		{
			name:  "4-byte EBML Header ID 0x1A45DFA3",
			data:  []byte{0x1A, 0x45, 0xDF, 0xA3},
			want:  0x1A45DFA3,
			width: 4,
		},
		{
			name:  "1-byte Cluster timecode ID 0xE7",
			data:  []byte{0xE7},
			want:  0xE7,
			width: 1,
		},
		{
			name:  "2-byte DocType ID 0x4282",
			data:  []byte{0x42, 0x82},
			want:  0x4282,
			width: 2,
		},
		{
			name:  "extra bytes after VINT are ignored",
			data:  []byte{0xA3, 0xFF, 0xFF},
			want:  0xA3,
			width: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, width, err := ReadVINT(tt.data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("value: got 0x%X, want 0x%X", got, tt.want)
			}
			if width != tt.width {
				t.Errorf("width: got %d, want %d", width, tt.width)
			}
		})
	}
}

func TestReadVINT_Errors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty buffer",
			data: []byte{},
		},
		{
			name: "truncated 2-byte VINT",
			data: []byte{0x42}, // needs 2 bytes, only 1 provided
		},
		{
			name: "truncated 4-byte VINT",
			data: []byte{0x1A, 0x45}, // needs 4 bytes, only 2 provided
		},
		{
			name: "truncated 3-byte VINT",
			data: []byte{0x2A, 0xD7}, // needs 3 bytes, only 2 provided
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ReadVINT(tt.data)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err != ErrBufferTooShort {
				t.Errorf("expected ErrBufferTooShort, got: %v", err)
			}
		})
	}
}

// ─── ReadVINTValue tests ────────────────────────────────────────────────────
// ReadVINTValue reads element sizes where the width marker bit is masked out.

func TestReadVINTValue(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		want  int64
		width int
	}{
		{
			name:  "1-byte size=5",
			data:  []byte{0x85},
			want:  5,
			width: 1,
		},
		{
			name:  "1-byte size=0",
			data:  []byte{0x80},
			want:  0,
			width: 1,
		},
		{
			name:  "1-byte size=127 is unknown",
			data:  []byte{0xFF},
			want:  UnknownSize,
			width: 1,
		},
		{
			name:  "2-byte size=0",
			data:  []byte{0x40, 0x00},
			want:  0,
			width: 2,
		},
		{
			name:  "2-byte size=256",
			data:  []byte{0x41, 0x00},
			want:  256,
			width: 2,
		},
		{
			name:  "2-byte size=1",
			data:  []byte{0x40, 0x01},
			want:  1,
			width: 2,
		},
		{
			name:  "2-byte all ones is unknown",
			data:  []byte{0x7F, 0xFF},
			want:  UnknownSize,
			width: 2,
		},
		{
			name:  "3-byte size=0",
			data:  []byte{0x20, 0x00, 0x00},
			want:  0,
			width: 3,
		},
		{
			name:  "3-byte all ones is unknown",
			data:  []byte{0x3F, 0xFF, 0xFF},
			want:  UnknownSize,
			width: 3,
		},
		{
			name:  "4-byte size=0",
			data:  []byte{0x10, 0x00, 0x00, 0x00},
			want:  0,
			width: 4,
		},
		{
			name:  "4-byte size=1000000",
			data:  []byte{0x10, 0x0F, 0x42, 0x40},
			want:  1000000,
			width: 4,
		},
		{
			name:  "4-byte all ones is unknown",
			data:  []byte{0x1F, 0xFF, 0xFF, 0xFF},
			want:  UnknownSize,
			width: 4,
		},
		{
			name: "8-byte all ones is unknown",
			data: []byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			want: UnknownSize, width: 8,
		},
		{
			name:  "8-byte size=0",
			data:  []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			want:  0,
			width: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, width, err := ReadVINTValue(tt.data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("value: got %d, want %d", got, tt.want)
			}
			if width != tt.width {
				t.Errorf("width: got %d, want %d", width, tt.width)
			}
		})
	}
}

func TestReadVINTValue_Errors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty buffer",
			data: []byte{},
		},
		{
			name: "truncated 2-byte size",
			data: []byte{0x40},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ReadVINTValue(tt.data)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err != ErrBufferTooShort {
				t.Errorf("expected ErrBufferTooShort, got: %v", err)
			}
		})
	}
}

// ─── ReadElementHeader tests ────────────────────────────────────────────────

func TestReadElementHeader(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		wantID    uint32
		wantSize  int64
		wantHdrLen int
	}{
		{
			name:       "SimpleBlock with size=5",
			data:       []byte{0xA3, 0x85},
			wantID:     IDSimpleBlock,
			wantSize:   5,
			wantHdrLen: 2,
		},
		{
			name: "Cluster with unknown size",
			data: []byte{
				0x1F, 0x43, 0xB6, 0x75, // Cluster ID (4 bytes)
				0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // unknown size (8 bytes)
			},
			wantID:     IDCluster,
			wantSize:   UnknownSize,
			wantHdrLen: 12,
		},
		{
			name: "EBML Header with known size",
			data: []byte{
				0x1A, 0x45, 0xDF, 0xA3, // EBML Header ID (4 bytes)
				0x85, // size = 5 (1 byte)
			},
			wantID:     IDEBMLHeader,
			wantSize:   5,
			wantHdrLen: 5,
		},
		{
			name: "Segment with unknown size",
			data: []byte{
				0x18, 0x53, 0x80, 0x67, // Segment ID (4 bytes)
				0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // unknown size
			},
			wantID:     IDSegment,
			wantSize:   UnknownSize,
			wantHdrLen: 12,
		},
		{
			name:       "TrackEntry with size=100",
			data:       []byte{0xAE, 0x80 | 100}, // ID=0xAE, size=100 with 1-byte VINT
			wantID:     IDTrackEntry,
			wantSize:   100,
			wantHdrLen: 2,
		},
		{
			name: "Tracks with 2-byte size",
			data: []byte{
				0x16, 0x54, 0xAE, 0x6B, // Tracks ID (4 bytes)
				0x41, 0x00, // size = 256 (2-byte VINT)
			},
			wantID:     IDTracks,
			wantSize:   256,
			wantHdrLen: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, size, hdrLen, err := ReadElementHeader(tt.data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id != tt.wantID {
				t.Errorf("id: got 0x%X, want 0x%X", id, tt.wantID)
			}
			if size != tt.wantSize {
				t.Errorf("size: got %d, want %d", size, tt.wantSize)
			}
			if hdrLen != tt.wantHdrLen {
				t.Errorf("headerLen: got %d, want %d", hdrLen, tt.wantHdrLen)
			}
		})
	}
}

func TestReadElementHeader_Errors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty buffer",
			data: []byte{},
		},
		{
			name: "ID only, no size bytes",
			data: []byte{0xA3},
		},
		{
			name: "truncated ID",
			data: []byte{0x1A, 0x45},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := ReadElementHeader(tt.data)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

// ─── ReadUint tests ─────────────────────────────────────────────────────────

func TestReadUint(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		width int
		want  uint64
	}{
		{
			name:  "1-byte value=0",
			data:  []byte{0x00},
			width: 1,
			want:  0,
		},
		{
			name:  "1-byte value=255",
			data:  []byte{0xFF},
			width: 1,
			want:  255,
		},
		{
			name:  "2-byte value=256",
			data:  []byte{0x01, 0x00},
			width: 2,
			want:  256,
		},
		{
			name:  "2-byte value=0x1234",
			data:  []byte{0x12, 0x34},
			width: 2,
			want:  0x1234,
		},
		{
			name:  "3-byte TimecodeScale default (1000000)",
			data:  []byte{0x0F, 0x42, 0x40},
			width: 3,
			want:  1000000,
		},
		{
			name:  "4-byte value",
			data:  []byte{0x00, 0x01, 0x00, 0x00},
			width: 4,
			want:  65536,
		},
		{
			name:  "8-byte max uint64",
			data:  []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			width: 8,
			want:  0xFFFFFFFFFFFFFFFF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReadUint(tt.data, tt.width)
			if got != tt.want {
				t.Errorf("got %d (0x%X), want %d (0x%X)", got, got, tt.want, tt.want)
			}
		})
	}
}

// ─── ReadString tests ───────────────────────────────────────────────────────

func TestReadString(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		length int
		want   string
	}{
		{
			name:   "ASCII string 'webm'",
			data:   []byte{'w', 'e', 'b', 'm'},
			length: 4,
			want:   "webm",
		},
		{
			name:   "string 'matroska'",
			data:   []byte{'m', 'a', 't', 'r', 'o', 's', 'k', 'a'},
			length: 8,
			want:   "matroska",
		},
		{
			name:   "empty string",
			data:   []byte{},
			length: 0,
			want:   "",
		},
		{
			name:   "codec ID 'V_VP9'",
			data:   []byte{'V', '_', 'V', 'P', '9'},
			length: 5,
			want:   "V_VP9",
		},
		{
			name:   "string with null terminator trimmed",
			data:   []byte{'A', 'B', 0x00, 0x00},
			length: 4,
			want:   "AB",
		},
		{
			name:   "length exceeds data",
			data:   []byte{'H', 'i'},
			length: 10,
			want:   "Hi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReadString(tt.data, tt.length)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ─── ReadFloat tests ────────────────────────────────────────────────────────

func TestReadFloat(t *testing.T) {
	// Helper: encode a float32 as 4 big-endian bytes.
	float32Bytes := func(f float32) []byte {
		var buf [4]byte
		binary.BigEndian.PutUint32(buf[:], math.Float32bits(f))
		return buf[:]
	}

	// Helper: encode a float64 as 8 big-endian bytes.
	float64Bytes := func(f float64) []byte {
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], math.Float64bits(f))
		return buf[:]
	}

	t.Run("float32 zero", func(t *testing.T) {
		got := ReadFloat(float32Bytes(0.0), 4)
		if got != 0.0 {
			t.Errorf("got %f, want 0.0", got)
		}
	})

	t.Run("float32 48000.0 (sampling frequency)", func(t *testing.T) {
		got := ReadFloat(float32Bytes(48000.0), 4)
		if got != 48000.0 {
			t.Errorf("got %f, want 48000.0", got)
		}
	})

	t.Run("float32 44100.0", func(t *testing.T) {
		got := ReadFloat(float32Bytes(44100.0), 4)
		if got != 44100.0 {
			t.Errorf("got %f, want 44100.0", got)
		}
	})

	t.Run("float64 zero", func(t *testing.T) {
		got := ReadFloat(float64Bytes(0.0), 8)
		if got != 0.0 {
			t.Errorf("got %f, want 0.0", got)
		}
	})

	t.Run("float64 48000.0 (sampling frequency)", func(t *testing.T) {
		got := ReadFloat(float64Bytes(48000.0), 8)
		if got != 48000.0 {
			t.Errorf("got %f, want 48000.0", got)
		}
	})

	t.Run("float64 duration 120000.0 ms", func(t *testing.T) {
		got := ReadFloat(float64Bytes(120000.0), 8)
		if got != 120000.0 {
			t.Errorf("got %f, want 120000.0", got)
		}
	})

	t.Run("float64 pi", func(t *testing.T) {
		got := ReadFloat(float64Bytes(math.Pi), 8)
		if got != math.Pi {
			t.Errorf("got %v, want %v", got, math.Pi)
		}
	})

	t.Run("unsupported width returns 0", func(t *testing.T) {
		got := ReadFloat([]byte{0x01, 0x02, 0x03}, 3)
		if got != 0 {
			t.Errorf("got %f, want 0", got)
		}
	})

	t.Run("buffer too short for float32", func(t *testing.T) {
		got := ReadFloat([]byte{0x01, 0x02}, 4)
		if got != 0 {
			t.Errorf("got %f, want 0", got)
		}
	})

	t.Run("buffer too short for float64", func(t *testing.T) {
		got := ReadFloat([]byte{0x01, 0x02, 0x03, 0x04}, 8)
		if got != 0 {
			t.Errorf("got %f, want 0", got)
		}
	})
}

// ─── ElementName tests ──────────────────────────────────────────────────────

func TestElementName(t *testing.T) {
	tests := []struct {
		id   uint32
		want string
	}{
		{IDEBMLHeader, "EBMLHeader"},
		{IDSegment, "Segment"},
		{IDCluster, "Cluster"},
		{IDSimpleBlock, "SimpleBlock"},
		{IDTracks, "Tracks"},
		{IDTrackEntry, "TrackEntry"},
		{IDCodecID, "CodecID"},
		{IDTimecode, "Timecode"},
		{IDVoid, "Void"},
		{IDAudio, "Audio"},
		{IDVideo, "Video"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := ElementName(tt.id)
			if got != tt.want {
				t.Errorf("ElementName(0x%X) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}

	// Verify unknown IDs produce a reasonable fallback string.
	t.Run("unknown element", func(t *testing.T) {
		got := ElementName(0xDEADBEEF)
		if got != "Unknown(0xDEADBEEF)" {
			t.Errorf("got %q for unknown ID", got)
		}
	})
}

// ─── Integration: round-trip through real Matroska element headers ───────────

func TestReadElementHeader_RealWorldHeaders(t *testing.T) {
	// This test simulates parsing a sequence of element headers as they
	// might appear at the start of a real WebM file.

	// EBML Header element with size 31 (0x9F with marker = 0x9F, value = 0x1F = 31)
	ebmlHeader := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x9F}
	id, size, hdrLen, err := ReadElementHeader(ebmlHeader)
	if err != nil {
		t.Fatalf("EBML header: %v", err)
	}
	if id != IDEBMLHeader {
		t.Errorf("EBML header ID: got 0x%X, want 0x%X", id, IDEBMLHeader)
	}
	if size != 31 {
		t.Errorf("EBML header size: got %d, want 31", size)
	}
	if hdrLen != 5 {
		t.Errorf("EBML header hdrLen: got %d, want 5", hdrLen)
	}

	// EBMLVersion element (inside EBML header): ID=0x4286, value=1
	versionElem := []byte{0x42, 0x86, 0x81, 0x01}
	id, size, hdrLen, err = ReadElementHeader(versionElem)
	if err != nil {
		t.Fatalf("EBMLVersion: %v", err)
	}
	if id != IDEBMLVersion {
		t.Errorf("EBMLVersion ID: got 0x%X, want 0x%X", id, IDEBMLVersion)
	}
	if size != 1 {
		t.Errorf("EBMLVersion size: got %d, want 1", size)
	}
	if hdrLen != 3 {
		t.Errorf("EBMLVersion hdrLen: got %d, want 3", hdrLen)
	}

	// Verify the payload is the uint value 1.
	payload := versionElem[hdrLen : hdrLen+int(size)]
	if ReadUint(payload, int(size)) != 1 {
		t.Errorf("EBMLVersion payload: got %d, want 1", ReadUint(payload, int(size)))
	}
}

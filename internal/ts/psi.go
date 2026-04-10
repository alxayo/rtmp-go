package ts

// This file implements parsers for Program-Specific Information (PSI) tables
// in MPEG-TS. PSI tables are the "table of contents" for a transport stream —
// they tell the demuxer which programs exist and which PIDs carry audio/video.
//
// The two most important PSI tables are:
//
//   - PAT (Program Association Table): Always carried on PID 0. It lists every
//     program in the stream and tells you which PID carries that program's PMT.
//
//   - PMT (Program Map Table): Describes one program. It lists all the
//     elementary streams (audio, video, etc.) in that program, their PIDs,
//     and their codec types (stream types).
//
// Discovery flow:
//   1. Read PID 0 → parse PAT → learn PMT PIDs
//   2. Read PMT PID → parse PMT → learn audio/video PIDs and codecs
//   3. Read audio/video PIDs → reassemble PES packets → get media frames

import (
	"fmt"
)

// PATEntry represents one entry in the Program Association Table.
// Each entry maps a program number to the PID where its PMT can be found.
type PATEntry struct {
	// ProgramNumber identifies the program. Program 0 is special — it
	// points to the Network Information Table (NIT) rather than a PMT.
	// Programs 1+ are actual programs containing audio/video.
	ProgramNumber uint16

	// PMTPID is the PID that carries this program's Program Map Table.
	// For program 0, this would be the NIT PID instead.
	PMTPID uint16
}

// ParsePAT parses a Program Association Table from a TS packet's payload.
//
// The PAT is always found on PID 0 and tells us where to find the PMT for
// each program. The payload starts with a PSI section header:
//
//	Byte 0:    pointer field (how many bytes to skip before the table starts)
//	Byte 1:    table_id (0x00 for PAT)
//	Bytes 2-3: section syntax indicator(1) | '0'(1) | reserved(2) | section_length(12)
//	Bytes 4-5: transport stream ID
//	Byte 6:    reserved(2) | version(5) | current/next indicator(1)
//	Byte 7:    section number
//	Byte 8:    last section number
//	Then:      4-byte entries, each containing program_number(16) | reserved(3) | PID(13)
//	Last 4:    CRC32 (we skip verification for simplicity)
func ParsePAT(payload []byte) ([]PATEntry, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("ts: PAT payload too short")
	}

	// The pointer field tells us how many bytes to skip before the table.
	// This is used when a section doesn't start at the beginning of the payload.
	pointerField := int(payload[0])
	tableStart := 1 + pointerField

	// We need at least 8 bytes for the section header (table_id through
	// last_section_number) plus the pointer field.
	if tableStart+8 > len(payload) {
		return nil, fmt.Errorf("ts: PAT payload too short for section header")
	}

	// Verify this is actually a PAT (table_id = 0x00).
	tableID := payload[tableStart]
	if tableID != 0x00 {
		return nil, fmt.Errorf("ts: expected PAT table_id 0x00, got 0x%02X", tableID)
	}

	// Extract section_length from the lower 12 bits of bytes 2-3.
	// Section length counts bytes after the section_length field itself,
	// up to and including the CRC32.
	sectionLength := int(payload[tableStart+1]&0x0F)<<8 | int(payload[tableStart+2])

	// Make sure we have enough data for the entire section.
	sectionEnd := tableStart + 3 + sectionLength
	if sectionEnd > len(payload) {
		return nil, fmt.Errorf("ts: PAT section length %d exceeds payload", sectionLength)
	}

	// The PAT entries start after the 5 fixed header bytes (transport_stream_id
	// through last_section_number) and end 4 bytes before the section end (CRC32).
	// So entries occupy bytes [tableStart+8 .. sectionEnd-4).
	entriesStart := tableStart + 8
	entriesEnd := sectionEnd - 4 // Exclude CRC32

	if entriesEnd < entriesStart {
		// No entries (empty PAT)
		return nil, nil
	}

	// Each PAT entry is exactly 4 bytes:
	//   Bytes 0-1: program_number (16 bits)
	//   Bytes 2-3: reserved(3 bits) | PID(13 bits)
	entryData := payload[entriesStart:entriesEnd]
	if len(entryData)%4 != 0 {
		return nil, fmt.Errorf("ts: PAT entry data length %d not multiple of 4", len(entryData))
	}

	entries := make([]PATEntry, 0, len(entryData)/4)
	for i := 0; i < len(entryData); i += 4 {
		entry := PATEntry{
			ProgramNumber: uint16(entryData[i])<<8 | uint16(entryData[i+1]),
			PMTPID:        uint16(entryData[i+2]&0x1F)<<8 | uint16(entryData[i+3]),
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// PMTStream describes one elementary stream within a program.
// Each PMT contains a list of these — one for each audio or video track.
type PMTStream struct {
	// StreamType identifies the codec. See the StreamType* constants
	// in stream_types.go (e.g., 0x1B = H.264, 0x0F = AAC).
	StreamType uint8

	// PID is the transport stream PID that carries this elementary stream's
	// data. The demuxer will look for TS packets with this PID to get the
	// actual audio or video data.
	PID uint16
}

// PMT holds the parsed Program Map Table for one program.
// The PMT tells us everything we need to know to decode a program:
// which PID carries timing information and which PIDs carry media data.
type PMT struct {
	// PCRPID is the PID that carries the Program Clock Reference (PCR).
	// The PCR is used for clock synchronization between encoder and decoder.
	PCRPID uint16

	// Streams lists all elementary streams in this program. Each entry
	// tells us the codec type and the PID to listen on for that stream.
	Streams []PMTStream
}

// ParsePMT parses a Program Map Table from a TS packet's payload.
//
// The PMT layout after the PSI section header:
//
//	Byte 0:    pointer field
//	Byte 1:    table_id (0x02 for PMT)
//	Bytes 2-3: section syntax indicator(1) | '0'(1) | reserved(2) | section_length(12)
//	Bytes 4-5: program_number
//	Byte 6:    reserved(2) | version(5) | current/next(1)
//	Byte 7:    section_number
//	Byte 8:    last_section_number
//	Bytes 9-10: reserved(3) | PCR_PID(13)
//	Bytes 11-12: reserved(4) | program_info_length(12)
//	Then:      program descriptors (skip program_info_length bytes)
//	Then:      stream entries, each:
//	           stream_type(8) | reserved(3) | elementary_PID(13) |
//	           reserved(4) | ES_info_length(12) | descriptors
//	Last 4:    CRC32
func ParsePMT(payload []byte) (*PMT, error) {
	if len(payload) < 1 {
		return nil, fmt.Errorf("ts: PMT payload too short")
	}

	// Skip the pointer field, just like in the PAT.
	pointerField := int(payload[0])
	tableStart := 1 + pointerField

	// Need at least 12 bytes for the PMT header (through program_info_length).
	if tableStart+12 > len(payload) {
		return nil, fmt.Errorf("ts: PMT payload too short for header")
	}

	// Verify this is a PMT (table_id = 0x02).
	tableID := payload[tableStart]
	if tableID != 0x02 {
		return nil, fmt.Errorf("ts: expected PMT table_id 0x02, got 0x%02X", tableID)
	}

	// Extract section_length.
	sectionLength := int(payload[tableStart+1]&0x0F)<<8 | int(payload[tableStart+2])
	sectionEnd := tableStart + 3 + sectionLength
	if sectionEnd > len(payload) {
		return nil, fmt.Errorf("ts: PMT section length %d exceeds payload", sectionLength)
	}

	pmt := &PMT{}

	// Extract PCR PID from bytes 9-10 (relative to tableStart).
	// The PCR PID is in the lower 13 bits.
	pmt.PCRPID = uint16(payload[tableStart+8]&0x1F)<<8 | uint16(payload[tableStart+9])

	// Extract program_info_length from bytes 11-12 (relative to tableStart).
	// This tells us how many bytes of program-level descriptors to skip.
	programInfoLength := int(payload[tableStart+10]&0x0F)<<8 | int(payload[tableStart+11])

	// Stream entries start after the fixed header and program descriptors.
	// The fixed header is 12 bytes (from table_id through program_info_length),
	// then we skip programInfoLength bytes of descriptors.
	streamsStart := tableStart + 12 + programInfoLength

	// Stream entries end 4 bytes before the section end (CRC32).
	streamsEnd := sectionEnd - 4

	if streamsEnd < streamsStart {
		// No streams — unusual but technically valid.
		return pmt, nil
	}

	// Parse each stream entry. Each entry has a 5-byte fixed header
	// followed by ES_info_length bytes of descriptors.
	pos := streamsStart
	for pos+5 <= streamsEnd {
		// Stream entry layout:
		//   Byte 0:    stream_type (8 bits) — identifies the codec
		//   Bytes 1-2: reserved(3) | elementary_PID(13)
		//   Bytes 3-4: reserved(4) | ES_info_length(12)
		stream := PMTStream{
			StreamType: payload[pos],
			PID:        uint16(payload[pos+1]&0x1F)<<8 | uint16(payload[pos+2]),
		}

		// ES_info_length tells us how many bytes of stream-level descriptors
		// follow. We skip over them since we don't need descriptor details.
		esInfoLength := int(payload[pos+3]&0x0F)<<8 | int(payload[pos+4])

		pmt.Streams = append(pmt.Streams, stream)

		// Move past this entry: 5 bytes of fixed header + descriptor bytes.
		pos += 5 + esInfoLength
	}

	return pmt, nil
}

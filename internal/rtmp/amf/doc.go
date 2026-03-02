// Package amf implements the AMF0 (Action Message Format version 0) codec
// used by RTMP for command serialization.
//
// AMF0 is a compact binary format for encoding structured data. RTMP uses it
// to serialize command messages (connect, createStream, publish, play) and
// their responses.
//
// # Supported Types
//
//   - Number (marker 0x00): IEEE 754 double-precision float.
//   - Boolean (marker 0x01): Single byte; any non-zero value is true.
//   - String (marker 0x02): UTF-8 string with 2-byte length prefix (max 65535 bytes).
//   - Object (marker 0x03): Key-value pairs terminated by 0x00 0x00 0x09.
//   - Null (marker 0x05): No payload.
//   - Strict Array (marker 0x0A): 4-byte element count followed by values.
//
// # Usage
//
//	// Encode multiple values into a single byte slice:
//	data, err := amf.EncodeAll("connect", 1.0, map[string]interface{}{
//	    "app": "live",
//	})
//
//	// Decode all values from a byte slice:
//	values, err := amf.DecodeAll(data)
//	// values[0] = "connect", values[1] = 1.0, values[2] = map[...]
//
// Objects are encoded with keys in sorted order for deterministic output,
// which is important for golden-vector testing.
package amf

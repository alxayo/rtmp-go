# AMF0 Encoding Contract

**Feature**: 001-rtmp-server-implementation  
**Package**: `internal/rtmp/amf`  
**Date**: 2025-10-01

## Overview

This contract defines AMF0 (Action Message Format 0) encoding and decoding for RTMP command messages. AMF0 is a binary format for serializing ActionScript objects, used in RTMP for commands like `connect`, `createStream`, `publish`, `play`.

**Reference**: AMF0 Specification (Adobe)

**Scope**: AMF0 only (not AMF3). Sufficient for all standard RTMP commands.

---

## Supported Types

| Type Marker | Type Name | Go Type | Description |
|-------------|-----------|---------|-------------|
| 0x00 | Number | `float64` | IEEE 754 double precision |
| 0x01 | Boolean | `bool` | True or false |
| 0x02 | String | `string` | UTF-8 string |
| 0x03 | Object | `map[string]interface{}` | Key-value pairs |
| 0x05 | Null | `nil` | Null value |
| 0x08 | ECMA Array | `map[string]interface{}` | Associative array (treated as Object) |
| 0x0A | Strict Array | `[]interface{}` | Indexed array |

**Not Implemented** (out of scope):
- 0x04 MovieClip (deprecated)
- 0x06 Undefined (edge case)
- 0x07 Reference (complex, rarely used)
- 0x09 Object End Marker (handled as part of Object parsing)
- 0x0B Date (can be represented as Number)
- 0x0C Long String (AMF3 feature)
- 0x0D Unsupported (reserved)
- 0x0E RecordSet (deprecated)
- 0x0F XML Document (rarely used)
- 0x10 Typed Object (complex)
- AMF3 types (0x11+)

---

## Type 0x00: Number

**Purpose**: Represent numeric values (integers, floats)

**Encoding**:
```
Byte 0:     0x00 (Number marker)
Bytes 1-8:  IEEE 754 double precision (8 bytes, big-endian)
```

**Go Representation**: `float64`

**Examples**:

**Number: 0.0**
```
Hex: 00 00 00 00 00 00 00 00 00
     -- ----------------------
     marker    0.0 (IEEE 754)
```

**Number: 1.0**
```
Hex: 00 3F F0 00 00 00 00 00 00
     -- ----------------------
     marker    1.0 (IEEE 754)
```

**Number: 1234.5**
```
Hex: 00 40 93 48 00 00 00 00 00
     -- ----------------------
     marker    1234.5
```

**Number: -1.0**
```
Hex: 00 BF F0 00 00 00 00 00 00
     -- ----------------------
     marker    -1.0
```

**Encoding Pseudocode**:
```go
func encodeNumber(w io.Writer, n float64) error {
    w.Write([]byte{0x00}) // Marker
    binary.Write(w, binary.BigEndian, n)
    return nil
}
```

**Decoding Pseudocode**:
```go
func decodeNumber(r io.Reader) (float64, error) {
    marker := readByte(r)
    if marker != 0x00 {
        return 0, errors.New("not a number")
    }
    var n float64
    binary.Read(r, binary.BigEndian, &n)
    return n, nil
}
```

**Edge Cases**:
- NaN: Valid IEEE 754 value, encode/decode as-is
- Infinity: Valid IEEE 754 value (0x7FF0000000000000 for +Inf)
- Integers: Encode as float64 (e.g., 42 → 42.0)

---

## Type 0x01: Boolean

**Purpose**: Represent true or false

**Encoding**:
```
Byte 0: 0x01 (Boolean marker)
Byte 1: 0x01 (true) or 0x00 (false)
```

**Go Representation**: `bool`

**Examples**:

**Boolean: true**
```
Hex: 01 01
     -- --
     marker true
```

**Boolean: false**
```
Hex: 01 00
     -- --
     marker false
```

**Encoding Pseudocode**:
```go
func encodeBoolean(w io.Writer, b bool) error {
    w.Write([]byte{0x01}) // Marker
    if b {
        w.Write([]byte{0x01})
    } else {
        w.Write([]byte{0x00})
    }
    return nil
}
```

**Decoding Pseudocode**:
```go
func decodeBoolean(r io.Reader) (bool, error) {
    marker := readByte(r)
    if marker != 0x01 {
        return false, errors.New("not a boolean")
    }
    value := readByte(r)
    return value != 0x00, nil
}
```

**Edge Cases**:
- Value byte > 0x01: Treat as true (lenient decoding)

---

## Type 0x02: String

**Purpose**: Represent UTF-8 encoded text

**Encoding**:
```
Byte 0:     0x02 (String marker)
Bytes 1-2:  Length (uint16, big-endian, 0-65535)
Bytes 3+:   UTF-8 bytes (length bytes)
```

**Go Representation**: `string`

**Examples**:

**String: "test"**
```
Hex: 02 00 04 74 65 73 74
     -- ----- -----------
     marker len=4  "test"
```

**String: "" (empty)**
```
Hex: 02 00 00
     -- -----
     marker len=0
```

**String: "Hello, 世界" (UTF-8 multibyte)**
```
Hex: 02 00 0D 48 65 6C 6C 6F 2C 20 E4 B8 96 E7 95 8C
     -- ----- ----------------------------------------
     marker len=13  "Hello, " + UTF-8 for 世界
```

**Encoding Pseudocode**:
```go
func encodeString(w io.Writer, s string) error {
    if len(s) > 65535 {
        return errors.New("string too long (max 65535 bytes)")
    }
    w.Write([]byte{0x02}) // Marker
    binary.Write(w, binary.BigEndian, uint16(len(s)))
    w.Write([]byte(s))
    return nil
}
```

**Decoding Pseudocode**:
```go
func decodeString(r io.Reader) (string, error) {
    marker := readByte(r)
    if marker != 0x02 {
        return "", errors.New("not a string")
    }
    var length uint16
    binary.Read(r, binary.BigEndian, &length)
    buf := make([]byte, length)
    io.ReadFull(r, buf)
    return string(buf), nil
}
```

**Edge Cases**:
- Empty string: Valid (length=0, no bytes)
- Max length: 65535 bytes (uint16 limit)
- Invalid UTF-8: Decoder should accept (RTMP spec doesn't enforce valid UTF-8)

---

## Type 0x03: Object

**Purpose**: Represent key-value pairs (like JSON object)

**Encoding**:
```
Byte 0:     0x03 (Object marker)
[Key-Value Pairs]:
  Key:      String (without 0x02 marker, just length + UTF-8)
            Length: uint16 (big-endian)
            UTF-8 bytes
  Value:    AMF0 encoded value (with type marker)
[End Marker]:
  0x00 0x00 0x09 (end of object)
```

**Go Representation**: `map[string]interface{}`

**Example**:

**Object: {"app": "live", "flashVer": "FMLE/3.0"}**
```
Hex:
03                          // Object marker
  00 03 61 70 70            // Key "app" (length=3)
  02 00 04 6C 69 76 65      // Value "live" (String)
  00 08 66 6C 61 73 68 56 65 72  // Key "flashVer" (length=8)
  02 00 08 46 4D 4C 45 2F 33 2E 30  // Value "FMLE/3.0" (String)
00 00 09                    // End of object
```

**Breakdown**:
- 0x03: Object marker
- Key "app": 0x00 0x03 "app" (no marker, just length+bytes)
- Value "live": 0x02 0x00 0x04 "live" (String with marker)
- Key "flashVer": 0x00 0x08 "flashVer"
- Value "FMLE/3.0": 0x02 0x00 0x08 "FMLE/3.0"
- 0x00 0x00 0x09: End marker (key length 0, marker 0x09)

**Nested Object Example**:

**Object: {"config": {"bitrate": 1000.0}}**
```
Hex:
03                          // Object marker
  00 06 63 6F 6E 66 69 67  // Key "config"
  03                        // Value: nested Object marker
    00 07 62 69 74 72 61 74 65  // Key "bitrate"
    00 40 8F 40 00 00 00 00 00  // Value: 1000.0 (Number)
  00 00 09                  // End of nested object
00 00 09                    // End of outer object
```

**Encoding Pseudocode**:
```go
func encodeObject(w io.Writer, obj map[string]interface{}) error {
    w.Write([]byte{0x03}) // Object marker
    
    for key, value := range obj {
        // Encode key (no marker, just length + UTF-8)
        binary.Write(w, binary.BigEndian, uint16(len(key)))
        w.Write([]byte(key))
        
        // Encode value (with type marker)
        encodeValue(w, value)
    }
    
    // End marker
    w.Write([]byte{0x00, 0x00, 0x09})
    return nil
}
```

**Decoding Pseudocode**:
```go
func decodeObject(r io.Reader) (map[string]interface{}, error) {
    marker := readByte(r)
    if marker != 0x03 {
        return nil, errors.New("not an object")
    }
    
    obj := make(map[string]interface{})
    
    for {
        // Read key length
        var keyLen uint16
        binary.Read(r, binary.BigEndian, &keyLen)
        
        if keyLen == 0 {
            // End marker check
            endMarker := readByte(r)
            if endMarker == 0x09 {
                break // End of object
            }
            return nil, errors.New("invalid object end")
        }
        
        // Read key
        keyBytes := make([]byte, keyLen)
        io.ReadFull(r, keyBytes)
        key := string(keyBytes)
        
        // Read value
        value, err := decodeValue(r)
        if err != nil {
            return nil, err
        }
        
        obj[key] = value
    }
    
    return obj, nil
}
```

**Edge Cases**:
- Empty object: 0x03 0x00 0x00 0x09 (no key-value pairs)
- Duplicate keys: Last value wins (map behavior)
- Nested objects: Recursively decode

---

## Type 0x05: Null

**Purpose**: Represent null/nil value

**Encoding**:
```
Byte 0: 0x05 (Null marker)
```

**Go Representation**: `nil`

**Example**:
```
Hex: 05
     --
     marker
```

**Encoding Pseudocode**:
```go
func encodeNull(w io.Writer) error {
    w.Write([]byte{0x05})
    return nil
}
```

**Decoding Pseudocode**:
```go
func decodeNull(r io.Reader) (interface{}, error) {
    marker := readByte(r)
    if marker != 0x05 {
        return nil, errors.New("not null")
    }
    return nil, nil
}
```

---

## Type 0x08: ECMA Array

**Purpose**: Associative array (key-value pairs with count prefix)

**Encoding**:
```
Byte 0:     0x08 (ECMA Array marker)
Bytes 1-4:  Array count (uint32, big-endian, advisory only)
[Key-Value Pairs]: Same as Object
[End Marker]: 0x00 0x00 0x09
```

**Go Representation**: `map[string]interface{}` (treated identically to Object)

**Example**:

**ECMA Array: {"key1": "value1", "key2": 2.0}**
```
Hex:
08                          // ECMA Array marker
00 00 00 02                 // Count = 2 (advisory)
  00 04 6B 65 79 31          // Key "key1"
  02 00 06 76 61 6C 75 65 31 // Value "value1" (String)
  00 04 6B 65 79 32          // Key "key2"
  00 40 00 00 00 00 00 00 00 // Value 2.0 (Number)
00 00 09                    // End marker
```

**Note**: Count field is **advisory only** - decoder should read until end marker, not rely on count.

**Encoding Pseudocode**:
```go
func encodeECMAArray(w io.Writer, arr map[string]interface{}) error {
    w.Write([]byte{0x08}) // ECMA Array marker
    binary.Write(w, binary.BigEndian, uint32(len(arr))) // Count
    
    // Encode as Object (key-value pairs)
    for key, value := range arr {
        binary.Write(w, binary.BigEndian, uint16(len(key)))
        w.Write([]byte(key))
        encodeValue(w, value)
    }
    
    w.Write([]byte{0x00, 0x00, 0x09}) // End marker
    return nil
}
```

**Decoding Pseudocode**:
```go
func decodeECMAArray(r io.Reader) (map[string]interface{}, error) {
    marker := readByte(r)
    if marker != 0x08 {
        return nil, errors.New("not an ECMA array")
    }
    
    var count uint32
    binary.Read(r, binary.BigEndian, &count) // Read but ignore count
    
    // Decode as Object (read until end marker)
    arr := make(map[string]interface{})
    for {
        var keyLen uint16
        binary.Read(r, binary.BigEndian, &keyLen)
        
        if keyLen == 0 {
            endMarker := readByte(r)
            if endMarker == 0x09 {
                break
            }
        }
        
        keyBytes := make([]byte, keyLen)
        io.ReadFull(r, keyBytes)
        key := string(keyBytes)
        
        value, _ := decodeValue(r)
        arr[key] = value
    }
    
    return arr, nil
}
```

---

## Type 0x0A: Strict Array

**Purpose**: Indexed array (like JSON array)

**Encoding**:
```
Byte 0:     0x0A (Strict Array marker)
Bytes 1-4:  Array count (uint32, big-endian)
[Values]:   AMF0 encoded values (count times, with type markers)
```

**Go Representation**: `[]interface{}`

**Example**:

**Strict Array: [1.0, "test", true, null]**
```
Hex:
0A                          // Strict Array marker
00 00 00 04                 // Count = 4
  00 3F F0 00 00 00 00 00 00  // [0]: 1.0 (Number)
  02 00 04 74 65 73 74        // [1]: "test" (String)
  01 01                       // [2]: true (Boolean)
  05                          // [3]: null (Null)
```

**Encoding Pseudocode**:
```go
func encodeStrictArray(w io.Writer, arr []interface{}) error {
    w.Write([]byte{0x0A}) // Strict Array marker
    binary.Write(w, binary.BigEndian, uint32(len(arr))) // Count
    
    for _, value := range arr {
        encodeValue(w, value)
    }
    
    return nil
}
```

**Decoding Pseudocode**:
```go
func decodeStrictArray(r io.Reader) ([]interface{}, error) {
    marker := readByte(r)
    if marker != 0x0A {
        return nil, errors.New("not a strict array")
    }
    
    var count uint32
    binary.Read(r, binary.BigEndian, &count)
    
    arr := make([]interface{}, count)
    for i := uint32(0); i < count; i++ {
        value, err := decodeValue(r)
        if err != nil {
            return nil, err
        }
        arr[i] = value
    }
    
    return arr, nil
}
```

**Edge Cases**:
- Empty array: 0x0A 0x00 0x00 0x00 0x00 (count=0)
- Nested arrays: Recursively encode/decode

---

## Generic Encoder/Decoder

**Encoder** (dispatches based on Go type):
```go
func encodeValue(w io.Writer, value interface{}) error {
    switch v := value.(type) {
    case nil:
        return encodeNull(w)
    case float64:
        return encodeNumber(w, v)
    case int, int32, int64:
        return encodeNumber(w, float64(v.(int)))
    case bool:
        return encodeBoolean(w, v)
    case string:
        return encodeString(w, v)
    case map[string]interface{}:
        return encodeObject(w, v) // or encodeECMAArray
    case []interface{}:
        return encodeStrictArray(w, v)
    default:
        return fmt.Errorf("unsupported type: %T", v)
    }
}
```

**Decoder** (reads type marker, dispatches):
```go
func decodeValue(r io.Reader) (interface{}, error) {
    marker := readByte(r)
    
    switch marker {
    case 0x00:
        r.UnreadByte() // Put marker back
        return decodeNumber(r)
    case 0x01:
        r.UnreadByte()
        return decodeBoolean(r)
    case 0x02:
        r.UnreadByte()
        return decodeString(r)
    case 0x03:
        r.UnreadByte()
        return decodeObject(r)
    case 0x05:
        return nil, nil // Null
    case 0x08:
        r.UnreadByte()
        return decodeECMAArray(r)
    case 0x0A:
        r.UnreadByte()
        return decodeStrictArray(r)
    default:
        return nil, fmt.Errorf("unsupported AMF0 type: 0x%02X", marker)
    }
}
```

---

## Test Scenarios

### Golden Tests

| Test Case | Input (Go) | Expected Hex |
|-----------|------------|--------------|
| Number 0.0 | `float64(0.0)` | `00 00 00 00 00 00 00 00 00` |
| Number 1.5 | `float64(1.5)` | `00 3F F8 00 00 00 00 00 00` |
| Boolean true | `bool(true)` | `01 01` |
| Boolean false | `bool(false)` | `01 00` |
| String "test" | `"test"` | `02 00 04 74 65 73 74` |
| Null | `nil` | `05` |
| Object {"key": "value"} | `map[string]interface{}{"key": "value"}` | `03 00 03 6B 65 79 02 00 05 76 61 6C 75 65 00 00 09` |
| Array [1, 2, 3] | `[]interface{}{1.0, 2.0, 3.0}` | `0A 00 00 00 03 00...` |

### Edge Cases

- Empty string: `""` → `02 00 00`
- Empty object: `{}` → `03 00 00 09`
- Empty array: `[]` → `0A 00 00 00 00`
- Nested object: `{"a": {"b": 1}}` → deeply nested encoding
- Unicode string: `"世界"` → `02 00 06 E4 B8 96 E7 95 8C`

### Error Cases

- Invalid type marker (0x99): Expect decode error
- Truncated string (length=10, only 5 bytes): Expect EOF error
- Missing object end marker: Expect decode error

---

## References

- AMF0 Specification: http://download.macromedia.com/pub/labs/amf/amf0_spec_121207.pdf
- FFmpeg libavformat/rtmpproto.c (AMF encoding/decoding)

---

**Status**: Contract complete. Ready for implementation and golden test generation.

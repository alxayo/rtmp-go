//go:build amf0gen

// Code generated for golden test vectors (AMF0 encoding). DO NOT EDIT MANUALLY.
// Generation script for T007: Create Golden Test Vectors for AMF0 Encoding
// Run: go run -tags amf0gen tests/golden/gen_amf0_vectors.go
// Produces the following files in tests/golden/:
//   - amf0_number_0.bin
//   - amf0_number_1_5.bin
//   - amf0_boolean_true.bin
//   - amf0_boolean_false.bin
//   - amf0_string_test.bin
//   - amf0_string_empty.bin
//   - amf0_object_simple.bin
//   - amf0_object_nested.bin
//   - amf0_null.bin
//   - amf0_array_strict.bin
//
// AMF0 Type Markers Used:
//
//	0x00 Number (8-byte IEEE 754 double, big-endian)
//	0x01 Boolean (1 byte 0x00/0x01)
//	0x02 String (2-byte BE length + bytes)
//	0x03 Object (key: 2-byte len + bytes, value: AMF0 value) terminated by 0x00 0x00 0x09
//	0x05 Null
//	0x0A Strict Array (4-byte BE count + elements)
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func encodeNumber(f float64) []byte {
	b := make([]byte, 1+8)
	b[0] = 0x00
	binary.BigEndian.PutUint64(b[1:], math.Float64bits(f))
	return b
}

func encodeBoolean(v bool) []byte {
	b := []byte{0x01, 0x00}
	if v {
		b[1] = 0x01
	}
	return b
}

func encodeString(s string) []byte {
	b := make([]byte, 1+2+len(s))
	b[0] = 0x02
	binary.BigEndian.PutUint16(b[1:3], uint16(len(s)))
	copy(b[3:], []byte(s))
	return b
}

func encodeNull() []byte { return []byte{0x05} }

func encodeObject(kv func(writeKV func(key string, value []byte))) []byte {
	buf := []byte{0x03}
	writeKV := func(key string, value []byte) {
		// key length (2B) + key bytes
		b := make([]byte, 2+len(key))
		binary.BigEndian.PutUint16(b[0:2], uint16(len(key)))
		copy(b[2:], key)
		buf = append(buf, b...)
		buf = append(buf, value...)
	}
	kv(writeKV)
	// end marker: 0x00 0x00 0x09
	buf = append(buf, 0x00, 0x00, 0x09)
	return buf
}

func encodeStrictArray(values ...[]byte) []byte {
	buf := []byte{0x0A}
	arr := make([]byte, 4)
	binary.BigEndian.PutUint32(arr, uint32(len(values)))
	buf = append(buf, arr...)
	for _, v := range values {
		buf = append(buf, v...)
	}
	return buf
}

func write(path string, data []byte) {
	must(os.WriteFile(path, data, 0o644))
	fmt.Printf("Wrote %-30s size=%d\n", filepath.Base(path), len(data))
}

func main() {
	outDir := filepath.Join("tests", "golden")
	must(os.MkdirAll(outDir, 0o755))

	write(filepath.Join(outDir, "amf0_number_0.bin"), encodeNumber(0.0))
	write(filepath.Join(outDir, "amf0_number_1_5.bin"), encodeNumber(1.5))
	write(filepath.Join(outDir, "amf0_boolean_true.bin"), encodeBoolean(true))
	write(filepath.Join(outDir, "amf0_boolean_false.bin"), encodeBoolean(false))
	write(filepath.Join(outDir, "amf0_string_test.bin"), encodeString("test"))
	write(filepath.Join(outDir, "amf0_string_empty.bin"), encodeString(""))

	// Simple object: {"key":"value"}
	objSimple := encodeObject(func(w func(string, []byte)) {
		w("key", encodeString("value"))
	})
	write(filepath.Join(outDir, "amf0_object_simple.bin"), objSimple)

	// Nested object: {"a":{"b":1.0}}
	objNestedInner := encodeObject(func(w func(string, []byte)) { w("b", encodeNumber(1.0)) })
	objNested := encodeObject(func(w func(string, []byte)) { w("a", objNestedInner) })
	write(filepath.Join(outDir, "amf0_object_nested.bin"), objNested)

	write(filepath.Join(outDir, "amf0_null.bin"), encodeNull())

	// Strict array: [1.0, 2.0, 3.0]
	arr := encodeStrictArray(encodeNumber(1.0), encodeNumber(2.0), encodeNumber(3.0))
	write(filepath.Join(outDir, "amf0_array_strict.bin"), arr)

	fmt.Println("AMF0 golden vector files generated in", outDir)
}

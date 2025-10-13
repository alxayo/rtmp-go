# Golden Test Vectors

This directory contains binary golden fixtures used by unit and integration tests to ensure exact wire-format compliance with the RTMP specification and AMF0 encoding rules.

## Regenerating Vectors
Each vector set has its own generation script guarded by a build tag so they are not compiled into regular builds.

| Category    | Script                          | Build Tag  | Output Files (examples) |
|-------------|----------------------------------|------------|-------------------------|
| Handshake   | `gen_handshake_vectors.go`       | `hsgen`    | `handshake_valid_c0c1.bin` |
| Chunking    | `gen_chunk_vectors.go`           | `chunkgen` | `chunk_fmt0_audio.bin` |
| Control     | `gen_control_vectors.go`         | `ctrlgen`  | `control_set_chunk_size_4096.bin` |
| AMF0        | `gen_amf0_vectors.go`            | `amf0gen`  | `amf0_number_0.bin` |

### AMF0 (Task T007)
Run:

```powershell
go run -tags amf0gen tests/golden/gen_amf0_vectors.go
```

Generated files:
- `amf0_number_0.bin` (0x00 + IEEE754 0.0)
- `amf0_number_1_5.bin` (0x00 + IEEE754 1.5)
- `amf0_boolean_true.bin` (0x01 0x01)
- `amf0_boolean_false.bin` (0x01 0x00)
- `amf0_string_test.bin` (0x02 0x00 0x04 'test')
- `amf0_string_empty.bin` (0x02 0x00 0x00)
- `amf0_object_simple.bin` (0x03 'key'→'value' 0x00 0x00 0x09)
- `amf0_object_nested.bin` (0x03 'a'→(object 'b'→1.0) terminators)
- `amf0_null.bin` (0x05)
- `amf0_array_strict.bin` (0x0A count=3 numbers 1.0,2.0,3.0)

### Validation Heuristics
When validating regenerated files, check:
1. File sizes:
   - Numbers: 9 bytes
   - Booleans: 2 bytes
   - Null: 1 byte
   - Empty string: 3 bytes
   - "test" string: 7 bytes
   - Simple object: 17 bytes
   - Nested object: 23 bytes
   - Strict array [1,2,3]: 32 bytes
2. Hex patterns (example for 1.5): `00 3F F8 00 00 00 00 00 00`
3. Object terminators: always `00 00 09`.

### Rationale
Binary golden vectors provide stable, reproducible anchors for protocol-layer tests, ensuring parsers and serializers remain byte-accurate across refactors.

---
Auto-maintained; update only if the underlying spec or contracts change.

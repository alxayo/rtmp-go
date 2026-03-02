
# RTMP v3 Simple Handshake – Technical Breakdown

This document provides a deep technical breakdown of the RTMP v3 Simple Handshake, suitable for software engineers implementing the protocol from scratch. It includes packet structure, byte-level details, edge cases, and pseudocode.

---

## 🧩 Overview of RTMP v3 Simple Handshake

The handshake consists of **three messages from each side**:

- **Client → Server**: `C0`, `C1`, `C2`
- **Server → Client**: `S0`, `S1`, `S2`

Each message has a **specific structure** and purpose.

---

## 🔢 Step-by-Step Breakdown

### 1. **C0 (Client Version Byte)**
- **Size**: 1 byte
- **Value**: `0x03` (RTMP version 3)
- **Purpose**: Indicates the RTMP version the client supports.

```hex
03
```

### 2. **C1 (Client Challenge)**
- **Size**: 1536 bytes
- **Structure**:
  - **Timestamp** (4 bytes): Unix time in milliseconds or zero.
  - **Zero field** (4 bytes): Always `0x00000000`.
  - **Random data** (1528 bytes): Arbitrary bytes for session uniqueness.

```text
C1 = [timestamp (4 bytes)] + [0x00000000] + [random_data (1528 bytes)]
```

#### Example (hex):
```hex
00 00 01 5F   // timestamp
00 00 00 00   // zero field
A3 4F ...     // 1528 bytes of random data
```

### 3. **S0 (Server Version Byte)**
- **Size**: 1 byte
- **Value**: `0x03` (RTMP version 3)
- **Purpose**: Echoes the version byte to confirm compatibility.

```hex
03
```

### 4. **S1 (Server Challenge)**
- **Size**: 1536 bytes
- **Structure**:
  - **Timestamp** (4 bytes): Server time or zero.
  - **Zero field** (4 bytes): Always `0x00000000`.
  - **Random data** (1528 bytes): Arbitrary bytes.

```text
S1 = [timestamp (4 bytes)] + [0x00000000] + [random_data (1528 bytes)]
```

### 5. **S2 (Server Response)**
- **Size**: 1536 bytes
- **Structure**:
  - Echoes the **C1 random data** back to the client.
  - Timestamp may be copied or set to server time.

```text
S2 = [timestamp (4 bytes)] + [0x00000000] + [C1.random_data (1528 bytes)]
```

### 6. **C2 (Client Response)**
- **Size**: 1536 bytes
- **Structure**:
  - Echoes the **S1 random data** back to the server.
  - Timestamp may be copied or set to client time.

```text
C2 = [timestamp (4 bytes)] + [0x00000000] + [S1.random_data (1528 bytes)]
```

---

## 🧠 Edge Cases & Implementation Notes

### ✅ **Timestamp Handling**
- Not strictly validated; can be zero or current time.
- Used for latency measurement in some implementations.

### ✅ **Random Data**
- Should be truly random (e.g., `os.urandom(1528)` in Python).
- Used to prevent replay attacks in more advanced handshakes.

### ✅ **Echo Validation**
- Some servers validate that `C2` matches `S1` and `S2` matches `C1`.
- Others skip validation for performance.

### ✅ **Timeouts**
- Handshake must complete within a few seconds (e.g., 5s).
- Failure to respond leads to connection termination.

### ✅ **Compatibility**
- RTMP v3 is backward-compatible with Flash-era clients.
- Modern servers (e.g., nginx-rtmp) expect this exact sequence.

---

## 🧪 Example in Pseudocode (Client Side)

```python
import time, os

def generate_c1():
    timestamp = int(time.time() * 1000).to_bytes(4, 'big')
    zero = b'    '
    random_data = os.urandom(1528)
    return timestamp + zero + random_data

def generate_c2(s1_random_data):
    timestamp = int(time.time() * 1000).to_bytes(4, 'big')
    zero = b'    '
    return timestamp + zero + s1_random_data
```

---

Would you like a full implementation in Python or C, or a packet capture template for Wireshark to debug RTMP handshakes?

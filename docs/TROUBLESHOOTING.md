# RTMP Server Troubleshooting Guide

## Issue 1: Log Level Not Changing to DEBUG

**Problem:** You run `./rtmp-server -log-level debug` but logs still show INFO level.

**Root Cause:** The logger was looking for `-log.level` (with a dot), but the CLI flag is `-log-level` (with a hyphen).

**Solution:** Use **BOTH** formats - they now work interchangeably:
```bash
# Either format works
./rtmp-server -log-level debug
./rtmp-server -log.level debug

# Or use environment variable
export RTMP_LOG_LEVEL=debug
./rtmp-server
```

**Verify DEBUG is enabled:**
```bash
./rtmp-server -log-level debug 2>&1 | head -20 | grep '"level":"DEBUG"'
```

If you see DEBUG-level messages, it's working!

---

## Issue 2: Server Listening Address Shows `[::]` Not Local IP

**What it looks like:**
```
{"level":"INFO","msg":"RTMP server listening","listen_addr":"[::]:1935"}
```

**What does `[::]` mean?**
- `[::]` = "all IPv6 interfaces" (IPv6 wildcard)
- `0.0.0.0` = "all IPv4 interfaces" (IPv4 wildcard)

**Your Local IPs:**
When you run `./rtmp-server -listen :1935 -srt-listen :10080`, the server listens on:

```
RTMP:  127.0.0.1:1935 (localhost)
       [::1]:1935 (localhost IPv6)
       192.168.0.12:1935 (your local network IP)
       
SRT:   127.0.0.1:10080 (localhost)
       [::1]:10080 (localhost IPv6)
       192.168.0.12:10080 (your local network IP)
```

The server **can** accept connections on all these addresses even though the log shows `[::]`.

---

## Issue 3: Check Server is Actually Listening

**Verify RTMP is listening:**
```bash
# macOS/Linux
lsof -i -P | grep 1935

# Windows (PowerShell)
netstat -ano | findstr :1935
```

Expected output shows process listening on port 1935.

**Verify SRT is listening:**
```bash
lsof -i -P | grep 10080
```

---

## Issue 4: FFmpeg Can't Connect

**Error:** `Connection refused` when trying to publish

**Diagnostics:**
1. **Is server running?**
   ```bash
   # Check if rtmp-server process exists
   ps aux | grep rtmp-server
   ```

2. **Is the right port listening?**
   ```bash
   # For RTMP (default 1935)
   lsof -i :1935 | grep LISTEN
   
   # For SRT (default 10080)
   lsof -i :10080 | grep LISTEN
   ```

3. **Try localhost first:**
   ```bash
   # Test RTMP on localhost
   ffmpeg -f lavfi -i testsrc=s=320x240:d=1 -c:v libx264 -f flv \
     rtmp://127.0.0.1:1935/live/test
   
   # Test SRT on localhost
   ffmpeg -f lavfi -i testsrc=s=320x240:d=1 -c:v libx264 -f mpegts \
     "srt://127.0.0.1:10080?streamid=publish:live/test"
   ```

4. **If localhost works but network IP fails**, the issue is usually:
   - Firewall blocking ports 1935/10080
   - Network interface misconfiguration
   - Server bound to wrong interface

---

## Issue 5: Detailed Connection Logging

**Get verbose connection logs:**
```bash
./rtmp-server -listen 127.0.0.1:1935 -srt-listen 127.0.0.1:10080 -log-level debug 2>&1 | \
  jq 'select(.msg | contains("connection"))'
```

This shows every connection attempt with:
- `conn_id` — unique connection ID
- `remote` — client IP address
- `tls` — whether connection used TLS
- Any errors encountered

**Example log output:**
```json
{"time":"...","level":"DEBUG","msg":"accepting connection","component":"rtmp_server","remote":"127.0.0.1:54321"}
{"time":"...","level":"INFO","msg":"connection registered","component":"rtmp_server","conn_id":"c000001","remote":"127.0.0.1:54321","tls":false}
```

---

## Issue 6: See All Listening IPs in Logs

**The improved logging now shows:**
```
RTMP server listening listen_addr=[::]:1935 \
  accessible_at="IPv6: [::1]:1935 | IPv4: 127.0.0.1:1935, 192.168.0.12:1935"
```

Translation:
- `listen_addr` — what the server bound to (wildcard)
- `accessible_at` — actual IPs where you can connect

**To force IPv4 only:**
```bash
./rtmp-server -listen 127.0.0.1:1935 -srt-listen 127.0.0.1:10080
```

**To force IPv6 only:**
```bash
./rtmp-server -listen "[::1]:1935" -srt-listen "[::1]:10080"
```

**To listen on specific network IP (e.g., 192.168.0.12):**
```bash
./rtmp-server -listen 192.168.0.12:1935 -srt-listen 192.168.0.12:10080
```

---

## Issue 7: Check Metrics

The server exposes metrics at `-metrics-addr` (default disabled):

```bash
./rtmp-server -metrics-addr :8080

# In another terminal, check metrics
curl http://localhost:8080/debug/vars | jq '.' | head -50
```

Look for:
```
"rtmp_connections_active": 1,
"rtmp_connections_total": 5,
"srt_connections_active": 0,
"srt_connections_total": 2,
```

---

## Quick Troubleshooting Checklist

- [ ] Server built with `go build -o rtmp-server ./cmd/rtmp-server`
- [ ] Server running: `./rtmp-server` (check `ps aux`)
- [ ] Ports free: `lsof -i :1935` and `lsof -i :10080`
- [ ] Log level enabled: `-log-level debug`
- [ ] Can reach localhost: `telnet localhost 1935`
- [ ] Can reach local IP: `telnet 192.168.0.12 1935`
- [ ] Firewall not blocking ports
- [ ] FFmpeg installed: `ffmpeg -version`
- [ ] Network connectivity: `ping 192.168.0.12` from remote machine

---

## Common Error Messages

### "Connection refused"
- Server not running
- Port blocked by firewall
- Server listening on wrong IP

**Fix:** Run `./rtmp-server -log-level debug` and check what ports are open

### "Handshake failed"
- FFmpeg is connecting to SRT server as RTMP (or vice versa)
- Protocol mismatch

**Fix:** Check your FFmpeg command - use `-f flv` for RTMP, `-f mpegts` for SRT

### "Stream not found" or "No such stream"
- Publisher hasn't connected yet
- Stream key doesn't match between publisher and subscriber

**Fix:** Start publisher first, wait 1-2 seconds for stream to register

### "TIMED OUT"
- SRT connection slow or unstable
- May need higher latency: `-srt-latency 300`

**Fix:** Try with: `./rtmp-server -srt-latency 300`

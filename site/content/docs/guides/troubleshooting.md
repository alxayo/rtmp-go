---
title: "Troubleshooting"
weight: 3
---

# Troubleshooting

## Quick Reference

| Symptom | Cause | Fix |
|---------|-------|-----|
| "connection refused" | Server not running | Start the server first |
| Black screen in ffplay | Missing sequence headers | Restart publisher; wait 2–3s before starting subscriber |
| "stream not found" | Wrong stream key | Ensure publisher and subscriber use the same `app/streamName` |
| High CPU | Debug logging | Use `-log-level info` instead of `debug` |
| Recording file empty | Publisher disconnected before keyframe | Stream for at least a few seconds |
| Connection dropped after ~90s | TCP read deadline | Normal for idle connections — ensure publisher is actively streaming |
| H.264 "mmco: unref short" warning | Joined mid-GOP | Normal and expected — decoder recovers in <1s |
| Auth failure | Token mismatch | Check stream key and token match exactly |
| Hook not firing | Wrong event name | Verify event type spelling (`connection_accept`, `publish_start`, etc.) |

## Connection Issues

### "Connection Refused"

The server is not running or not listening on the expected address.

**Checklist:**
1. Is the server process running? Check with `ps aux | grep rtmp-server` (Linux/macOS) or Task Manager (Windows)
2. Is the server listening on the correct port? Default is `:1935`
3. Are you connecting to the right address? Use `localhost` for local testing, or the server's IP for remote
4. Is a firewall blocking port 1935? Check with `telnet localhost 1935`

### Connection Drops After ~90 Seconds

The server enforces a **90-second read deadline**. If no data arrives for 90 seconds, the connection is closed as a zombie.

**This is normal for:**
- Idle connections that aren't actively streaming
- Clients that pause sending without disconnecting

**Fix**: Ensure the publisher is actively streaming. If you need to keep an idle connection alive, the client must send periodic ping/pong messages.

### "NetConnection.Connect.Rejected"

The server rejected the connection, usually due to authentication failure.

**Checklist:**
1. Is `-auth-mode` set? If so, you need a valid token
2. Is the token correct? Format: `streamKey?token=value`
3. For callback auth, is the callback URL reachable? Check with `curl`

## Video Issues

### Black Screen on Subscriber

The most common issue. The subscriber connected but doesn't see video.

**Causes:**
1. **Missing sequence headers** — the subscriber joined before the publisher sent SPS/PPS and AAC config. The server caches these, but if the publisher hasn't sent them yet, there's nothing to cache.
2. **Codec mismatch** — the publisher is using H.265 (HEVC) but the player only supports H.264.

**Fix:**
1. Restart the publisher (this forces new sequence headers)
2. Wait 2–3 seconds for the publisher to send keyframes
3. Then start the subscriber
4. Verify the publisher is using H.264, not H.265

### "mmco: unref short failure" Warning

This is a normal H.264 decoder warning that appears when the subscriber joins mid-GOP (between keyframes). The decoder doesn't have the reference frames it needs for the first few inter-frames.

**This is expected and harmless.** The decoder recovers within 1 second (at the next keyframe). No action needed.

### Choppy or Stuttering Video

**Causes:**
1. **Network bandwidth** — the stream bitrate exceeds the network capacity
2. **Slow subscriber** — the subscriber can't decode frames fast enough
3. **CPU overload** — the encoder or decoder is maxed out

**Fix:**
1. Lower the publisher's bitrate (2500 Kbps is a good starting point)
2. Use hardware encoding (NVENC, QuickSync) instead of x264
3. Use ffplay with `-framedrop` to skip frames when behind
4. Check server CPU with `-log-level info` (debug logging adds significant overhead)

## Recording Issues

### Recording File Is Empty

The publisher disconnected before sending a keyframe. FLV recording requires at least one complete keyframe to write valid data.

**Fix:** Stream for at least a few seconds before stopping. Ensure the keyframe interval is set to 2 seconds or less.

### Recording File Won't Play

The FLV file may be incomplete if the server crashed or the publisher disconnected unexpectedly.

**Fix:** Try converting with FFmpeg:
```bash
ffmpeg -i recording.flv -c copy repaired.mp4
```

FFmpeg will attempt to repair the container structure.

## Authentication Issues

### "Auth Failed" in Logs

The token provided doesn't match the server's configuration.

**Checklist:**
1. **Token format**: The stream key URL should be `rtmp://host/app/streamName?token=value`
2. **Exact match**: Tokens are case-sensitive and must match exactly
3. **Auth mode**: Verify `-auth-mode` matches your setup (`token`, `file`, or `callback`)
4. **File auth**: If using `-auth-file`, ensure the JSON file is valid and readable
5. **Callback auth**: If using `-auth-callback`, verify the URL is reachable and returns HTTP 200 for valid tokens

## Hook Issues

### Hook Not Firing

**Checklist:**
1. **Event name**: Verify the event type is spelled correctly. Valid events:
   - `connection_accept`
   - `connection_close`
   - `publish_start`
   - `publish_stop`
   - `play_start`
   - `play_stop`
   - `subscriber_count`
   - `auth_failed`
2. **Flag format**: `event_type=target` (e.g., `-hook-webhook "publish_start=https://example.com/hook"`)
3. **Webhook reachable**: Test the URL with `curl -X POST https://example.com/hook`
4. **Script executable**: For shell hooks, ensure the script has execute permissions (`chmod +x script.sh`)
5. **Timeout**: Check if the hook is timing out (default 30s). Increase with `-hook-timeout`

### Hook Firing But Slow

Hooks are asynchronous and won't block RTMP processing. However, if the hook target is slow:

1. Increase `-hook-concurrency` to allow more parallel executions
2. Decrease `-hook-timeout` to abandon slow hooks sooner
3. Ensure the webhook endpoint responds quickly (< 1s)

## Performance Issues

### High CPU Usage

**On the server:**
1. Switch from `-log-level debug` to `-log-level info` — debug logs every media message (60+ lines/sec per stream)
2. Check the number of concurrent streams and subscribers
3. Monitor with `-metrics-addr :8080` and check `/debug/vars`

**On the publisher (OBS/FFmpeg):**
1. Use hardware encoding (NVENC, QuickSync, AMF) instead of software x264
2. Lower resolution or frame rate
3. Use a faster preset (`veryfast` or `ultrafast`)

### High Memory Usage

Each subscriber maintains a bounded outbound queue (100 messages). With many subscribers, memory usage scales linearly.

**Fix:**
1. Monitor subscriber count via metrics
2. Consider relay architecture: use multiple edge servers instead of one origin with many direct subscribers

# FFmpeg / ffplay Interoperability (T054)

This directory contains automated and manual interoperability tests validating that the RTMP server works with standard FFmpeg tooling.

## Automated Tests

### PowerShell (Windows)

Run all tests (publishing, playback, concurrency, recording):
```powershell
./ffmpeg_test.ps1
```

Run a subset:
```powershell
./ffmpeg_test.ps1 -Include PublishOnly,Recording
```

Parameters:
- `-FFmpegExe` path to ffmpeg (default `ffmpeg`)
- `-FFplayExe` path to ffplay (default `ffplay`)
- `-ServerPort` RTMP port (default 1935)
- `-ServerFlags` extra flags passed to `rtmp-server.exe`
- `-TimeoutSeconds` future extension (not yet enforced everywhere)
- `-KeepWorkDir` keeps temp working directory for inspection

The script will:
1. Build `rtmp-server.exe` if missing
2. Generate a synthetic 1s `test.mp4` using FFmpeg color bars + sine tone (avoids committing media)
3. Execute the following test matrix:
   - PublishOnly: Single stream publish (sanity)
   - PublishAndPlay: Publish + local playback via ffplay
   - Concurrency: Two concurrent publishers + two concurrent players
   - Recording: Server `-record-all` flag, verify recorded FLV decodes

Exit code = number of failed tests (0 means success).

### Bash (Linux / macOS)

Run all:
```bash
./ffmpeg_test.sh
```

Subset with environment variables (comma separated):
```bash
INCLUDE=PublishOnly,Recording SERVER_FLAGS="-log-level debug" ./ffmpeg_test.sh
```

Environment variables:
| Variable | Default | Description |
|----------|---------|-------------|
| INCLUDE | PublishOnly,PublishAndPlay,Concurrency,Recording | Comma list of tests |
| FFMPEG_EXE | ffmpeg | ffmpeg executable name/path |
| FFPLAY_EXE | ffplay | ffplay executable name/path |
| SERVER_PORT | 1935 | RTMP port |
| SERVER_FLAGS | (empty) | Extra flags passed to server |
| KEEP_WORK_DIR | 0 | Set 1 to retain temp dir |

Exit code mirrors number of failed tests.

## Manual Linux / macOS Test (Shell)
*(Equivalent manual steps if you prefer without script)*

Generate sample media:
```bash
ffmpeg -hide_banner -loglevel error -f lavfi -i testsrc=size=640x360:rate=30 -f lavfi -i sine=frequency=1000:sample_rate=44100 -t 2 -c:v libx264 -pix_fmt yuv420p -c:a aac test.mp4
```

Start server:
```bash
./rtmp-server -listen :1935 -log-level debug -record-all -record-dir recordings
```

Publish:
```bash
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/test
```

Play:
```bash
ffplay rtmp://localhost:1935/live/test
```

Concurrent publish (in two terminals):
```bash
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/a
ffmpeg -re -i test.mp4 -c copy -f flv rtmp://localhost:1935/live/b
```

Concurrent play (two more terminals):
```bash
ffplay rtmp://localhost:1935/live/a
ffplay rtmp://localhost:1935/live/b
```

Verify recording:
```bash
ffprobe recordings/*.flv
```

## Troubleshooting
- Ensure `ffmpeg` and `ffplay` are installed and on PATH.
- If playback stalls, verify firewall isn’t blocking port 1935.
- Use `-ServerFlags "-log-level debug"` to get protocol traces.
- Delete any stale recordings if disk is full.

## Next Improvements
- CI matrix integration (Linux + Windows) with conditional skip if ffmpeg absent (added Linux workflow, Windows pending)
- Add latency measurements and throughput stats
- Add packet loss / stress scenarios

---
Task: T054 FFmpeg Interoperability Test (Completed)

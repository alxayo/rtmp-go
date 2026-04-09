# on-publish-hls.ps1 — Shell hook: converts incoming RTMP stream to HLS via ffmpeg
# Called by the go-rtmp server hook system on publish_start events.
# Environment variables set by the hook system:
#   RTMP_EVENT_TYPE   — "publish_start"
#   RTMP_STREAM_KEY   — e.g. "live/test"
#   RTMP_CONN_ID      — connection identifier
#   RTMP_TIMESTAMP    — unix timestamp

$ErrorActionPreference = "Stop"

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
$LogDir = Join-Path $ScriptDir "logs"
$HlsDir = Join-Path (Split-Path -Parent $ScriptDir) "hls-output"
$RtmpHost = if ($env:RTMP_HOST) { $env:RTMP_HOST } else { "localhost" }
$RtmpPort = if ($env:RTMP_PORT) { $env:RTMP_PORT } else { "1935" }

# Validate required environment
$StreamKey = $env:RTMP_STREAM_KEY
if (-not $StreamKey) {
    Write-Error "RTMP_STREAM_KEY not set"
    exit 1
}

# Derive output directory from stream key
$SafeKey = $StreamKey -replace '/', '_'
$OutputDir = Join-Path $HlsDir $SafeKey

New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null
New-Item -ItemType Directory -Path $LogDir -Force | Out-Null

$RtmpUrl = "rtmp://${RtmpHost}:${RtmpPort}/${StreamKey}"
$LogFile = Join-Path $LogDir "hls-${SafeKey}.log"

$timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
Add-Content -Path $LogFile -Value "$timestamp Starting HLS conversion for $StreamKey"
Add-Content -Path $LogFile -Value "  Source: $RtmpUrl"
Add-Content -Path $LogFile -Value "  Output: $OutputDir\playlist.m3u8"

# Start ffmpeg as a background process
$segPattern = Join-Path $OutputDir "segment_%03d.ts"
$playlist = Join-Path $OutputDir "playlist.m3u8"

$proc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "warning",
    "-i", $RtmpUrl,
    "-c", "copy",
    "-f", "hls",
    "-hls_time", "4",
    "-hls_list_size", "5",
    "-hls_flags", "delete_segments",
    "-hls_segment_filename", $segPattern,
    $playlist
) -RedirectStandardError $LogFile -NoNewWindow -PassThru

$proc.Id | Out-File -FilePath (Join-Path $OutputDir ".ffmpeg.pid") -Encoding ascii

Add-Content -Path $LogFile -Value "  ffmpeg PID: $($proc.Id)"
Write-Host "HLS conversion started for $StreamKey (PID: $($proc.Id))"

# on-publish-abr.ps1 — Shell hook: starts parallel FFmpeg instances for ABR HLS
# Called by the go-rtmp server hook system on publish_start events.
#
# Spawns 3 independent FFmpeg transcoders (1080p, 720p, 480p) with aligned
# keyframe parameters for seamless adaptive bitrate switching, plus writes a
# master.m3u8 playlist.
#
# Environment variables set by the hook system:
#   RTMP_EVENT_TYPE   — "publish_start"
#   RTMP_STREAM_KEY   — e.g. "live/test"
#   RTMP_CONN_ID      — connection identifier
#   RTMP_TIMESTAMP    — unix timestamp
#
# Usage:
#   .\rtmp-server.exe -hook-script "publish_start=.\scripts\on-publish-abr.ps1"

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
$RtmpUrl = "rtmp://${RtmpHost}:${RtmpPort}/${StreamKey}"
$LogFile = Join-Path $LogDir "abr-${SafeKey}.log"

# Alignment parameters — must be identical across all instances.
# Uses time-based keyframe forcing so alignment works at any input fps.
$SegTime = 2        # HLS segment duration in seconds
$ListSize = 10      # Number of segments in playlist
$Fps = 30           # Output frame rate (normalized across all renditions)

# Rendition presets
$Presets = @(
    @{ Name = "1080p"; Res = "1920x1080"; VBitrate = "5000k"; MaxRate = "5500k"; BufSize = "10000k"; ABitrate = "192k" },
    @{ Name = "720p";  Res = "1280x720";  VBitrate = "2500k"; MaxRate = "2750k"; BufSize = "5000k";  ABitrate = "128k" },
    @{ Name = "480p";  Res = "854x480";   VBitrate = "1000k"; MaxRate = "1100k"; BufSize = "2000k";  ABitrate = "96k" }
)

New-Item -ItemType Directory -Path $LogDir -Force | Out-Null

$timestamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
Add-Content -Path $LogFile -Value "$timestamp Starting ABR HLS for $StreamKey"
Add-Content -Path $LogFile -Value "  Source: $RtmpUrl"
Add-Content -Path $LogFile -Value "  Output: $OutputDir\"

# Clean stale segments from previous session to prevent playlist corruption
if (Test-Path $OutputDir) {
    Get-ChildItem -Path $OutputDir -Recurse -Include "*.ts" | Remove-Item -Force -ErrorAction SilentlyContinue
    Get-ChildItem -Path $OutputDir -Recurse -Include "*.m3u8" | Where-Object { $_.Name -ne "master.m3u8" } | Remove-Item -Force -ErrorAction SilentlyContinue
    Add-Content -Path $LogFile -Value "  Cleaned stale segments from previous session"
}

$Pids = @()
foreach ($preset in $Presets) {
    $name = $preset.Name
    $renditionDir = Join-Path $OutputDir $name
    $renditionLog = Join-Path $LogDir "abr-${SafeKey}-${name}.log"
    $segPattern = Join-Path $renditionDir "seg_%05d.ts"
    $playlist = Join-Path $renditionDir "index.m3u8"

    New-Item -ItemType Directory -Path $renditionDir -Force | Out-Null

    Add-Content -Path $LogFile -Value "  Starting $name ($($preset.Res) @ $($preset.VBitrate))..."

    $proc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
        "-hide_banner", "-loglevel", "warning",
        "-i", $RtmpUrl,
        "-c:v", "libx264", "-s", $preset.Res, "-b:v", $preset.VBitrate,
        "-maxrate", $preset.MaxRate, "-bufsize", $preset.BufSize,
        "-preset", "veryfast", "-r", $Fps,
        "-force_key_frames", "expr:gte(t,n_forced*${SegTime})",
        "-sc_threshold", "0",
        "-c:a", "aac", "-b:a", $preset.ABitrate, "-ar", "48000",
        "-f", "hls",
        "-hls_time", $SegTime,
        "-hls_list_size", $ListSize,
        "-hls_flags", "delete_segments+temp_file",
        "-hls_segment_filename", $segPattern,
        $playlist
    ) -RedirectStandardError $renditionLog -NoNewWindow -PassThru

    $Pids += $proc.Id
    $proc.Id | Out-File -FilePath (Join-Path $renditionDir ".ffmpeg.pid") -Encoding ascii
    Add-Content -Path $LogFile -Value "  $name PID: $($proc.Id)"
}

# Write master playlist (UTF-8 without BOM for HLS parser compatibility)
$masterContent = @"
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-INDEPENDENT-SEGMENTS

#EXT-X-STREAM-INF:BANDWIDTH=5700000,RESOLUTION=1920x1080
1080p/index.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=2900000,RESOLUTION=1280x720
720p/index.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=1200000,RESOLUTION=854x480
480p/index.m3u8
"@
$masterPath = Join-Path $OutputDir "master.m3u8"
[System.IO.File]::WriteAllText($masterPath, $masterContent, (New-Object System.Text.UTF8Encoding $false))

Add-Content -Path $LogFile -Value "  Master playlist: $OutputDir\master.m3u8"

# Save all PIDs for cleanup
$Pids -join "`n" | Out-File -FilePath (Join-Path $OutputDir ".abr-pids") -Encoding ascii

Write-Host "ABR HLS started for $StreamKey - 3 renditions (PIDs: $($Pids -join ', '))"

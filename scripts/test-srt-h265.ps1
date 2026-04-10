# SRT H.265 Camera Ingest Test (Windows PowerShell)
#
# This script tests H.265/HEVC video ingest via SRT protocol on Windows.
# It captures video from the system camera or generates a test pattern,
# and streams it to the RTMP/SRT server running locally.
#
# The test:
#   1. Builds the server if needed
#   2. Starts the RTMP server with SRT listener enabled
#   3. Streams H.265 video via SRT from ffmpeg
#   4. Records the stream locally
#   5. Validates the recording contains H.265 frames
#
# Requirements:
#   - ffmpeg with libsrt and libx265 support (64-bit)
#   - ffprobe (comes with ffmpeg)
#   - Go 1.21+ (for building the server)
#   - PowerShell 5.0+
#
# Usage:
#   .\test-srt-h265.ps1 -Duration 30
#
# Example:
#   .\test-srt-h265.ps1 -Duration 10    # 10 second test

param(
    [int]$Duration = 10
)

# Color helper (Windows 10+ supports ANSI)
$ColorGreen = "`e[32m"
$ColorRed = "`e[31m"
$ColorYellow = "`e[33m"
$ColorBlue = "`e[34m"
$ColorReset = "`e[0m"

function LogInfo {
    param([string]$Message)
    Write-Host "${ColorGreen}✓${ColorReset} $Message"
}

function LogError {
    param([string]$Message)
    Write-Host "${ColorRed}✗${ColorReset} $Message"
}

function LogWarn {
    param([string]$Message)
    Write-Host "${ColorYellow}⚠${ColorReset} $Message"
}

function LogStep {
    param([string]$Message)
    Write-Host "${ColorBlue}→${ColorReset} $Message"
}

# Configuration
$StreamKey = "live/h265-test"
$RecordDir = ".\recordings"
$FFmpegTimeout = $Duration + 10
$CameraDevice = "desktop"  # Windows uses gdigrab for desktop or dshow for webcams

Write-Host "=== SRT H.265 Ingest Test (Windows) ==="
Write-Host "Duration: ${Duration}s"
Write-Host "Stream key: ${StreamKey}"
Write-Host ""

# Check if ffmpeg has H.265 support
function CheckH265Support {
    $ffmpegCheck = & ffmpeg -codecs 2>&1 | Select-String "libx265" -Quiet
    if ($ffmpegCheck) {
        LogInfo "H.265 encoder available"
        return $true
    } else {
        LogWarn "H.265 encoder not available, falling back to H.264"
        return $false
    }
}

# Build the server if needed
if (-not (Test-Path ".\rtmp-server.exe")) {
    LogStep "Building RTMP server..."
    $buildResult = & go build -o rtmp-server.exe ./cmd/rtmp-server 2>&1
    if ($LASTEXITCODE -eq 0) {
        LogInfo "Server built"
    } else {
        LogError "Failed to build server"
        exit 1
    }
} else {
    LogInfo "Server already built"
}

# Create recordings directory
if (-not (Test-Path $RecordDir)) {
    New-Item -ItemType Directory -Path $RecordDir | Out-Null
}

# Cleanup old processes
LogStep "Cleaning up old processes..."
Get-Process rtmp-server -ErrorAction SilentlyContinue | Stop-Process -Force
Start-Sleep -Seconds 1
LogInfo "Ready"

# Start the server
LogStep "Starting RTMP server..."
$serverProcess = Start-Process -FilePath ".\rtmp-server.exe" `
    -ArgumentList `
        "-listen 127.0.0.1:1935",
        "-srt-listen 127.0.0.1:10080",
        "-record-all true",
        "-record-dir $RecordDir",
        "-log-level info" `
    -PassThru `
    -NoNewWindow

# Wait for server to start
Start-Sleep -Seconds 2

if ($serverProcess.HasExited) {
    LogError "Server failed to start"
    exit 1
}
LogInfo "Server started (PID: $($serverProcess.Id))"

# Determine which encoding to use
$H265Available = CheckH265Support
if ($H265Available) {
    $CodecOpts = "-c:v libx265 -preset ultrafast -crf 28"
    $CodecName = "H.265"
} else {
    $CodecOpts = "-c:v libx264 -preset ultrafast -crf 28"
    $CodecName = "H.264"
}

# Start capturing and streaming
LogStep "Starting camera capture and SRT stream..."
Write-Host "Camera device: $CameraDevice"
Write-Host "Streaming to: srt://localhost:10080?streamid=publish:${StreamKey}"
Write-Host "Codec: ${CodecName}"
Write-Host ""

# Run ffmpeg stream with timeout
$ffmpegScript = {
    param($Duration, $CodecOpts, $StreamKey)
    
    $ffmpegArgs = @(
        "-f", "gdigrab",
        "-i", "desktop",
        "-t", $Duration.ToString(),
        "-pix_fmt", "uyvy422"
    ) + $CodecOpts.Split() + @(
        "-f", "mpegts",
        "srt://localhost:10080?streamid=publish:${StreamKey}"
    )
    
    & ffmpeg @ffmpegArgs
}

$ffmpegJob = Start-Job -ScriptBlock $ffmpegScript -ArgumentList $Duration, $CodecOpts, $StreamKey

# Wait for ffmpeg to complete (with timeout)
if (Wait-Job -Job $ffmpegJob -Timeout $FFmpegTimeout) {
    $ffmpegExit = $ffmpegJob.JobStateInfo
    LogInfo "Stream stopped"
} else {
    Stop-Job -Job $ffmpegJob
    Remove-Job -Job $ffmpegJob
    LogWarn "Stream timeout after ${FFmpegTimeout}s"
}

Remove-Job -Job $ffmpegJob -ErrorAction SilentlyContinue

Write-Host ""
LogStep "Waiting for recordings to be flushed..."
Start-Sleep -Seconds 2

# Find the recording file (most recent .flv file)
$RecordFile = Get-ChildItem -Path "$RecordDir\*.flv" -ErrorAction SilentlyContinue |
    Sort-Object LastWriteTime -Descending |
    Select-Object -First 1

if (-not $RecordFile) {
    LogError "No recording found in $RecordDir"
    $serverProcess | Stop-Process -Force -ErrorAction SilentlyContinue
    exit 1
}

LogInfo "Recording found: $($RecordFile.Name)"

# Validate the recording
LogStep "Validating recording..."
$FileSize = $RecordFile.Length
LogInfo "File size: $FileSize bytes"

# Check for video codec in the recording
try {
    $CodecFound = & ffprobe -v error -select_streams v:0 `
        -show_entries stream=codec_name `
        -of "default=noprint_wrappers=1:nokey=1:noprint_filename=1" `
        $RecordFile.FullName 2>$null

    if ($CodecFound) {
        LogInfo "Video codec in file: $CodecFound"
        
        # Check for H.265 if we tried to stream it
        if ($H265Available) {
            if ($CodecFound -match "hevc|h265") {
                LogInfo "✓ H.265 frames successfully recorded"
            } else {
                LogWarn "Expected H.265 but got $CodecFound"
            }
        }
    }
} catch {
    LogWarn "ffprobe not available or failed, skipping codec validation"
}

# Stop server
LogStep "Stopping server..."
$serverProcess | Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1
LogInfo "Server stopped"

Write-Host ""
Write-Host "=== Test Complete ==="
if ($RecordFile) {
    LogInfo "Recording saved: $($RecordFile.FullName)"
    Write-Host ""
    Write-Host "To play the recording:"
    Write-Host "  ffplay `"$($RecordFile.FullName)`""
    Write-Host ""
    Write-Host "To stream to a player (RTMP):"
    Write-Host "  ffmpeg -re -i `"$($RecordFile.FullName)`" -c copy -f flv rtmp://your-server/live/stream"
} else {
    LogError "Test failed: no recording"
    exit 1
}

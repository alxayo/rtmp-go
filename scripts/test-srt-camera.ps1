# SRT Camera Ingest Test Script (PowerShell)
#
# This script demonstrates SRT ingest from an integrated camera:
# 1. Builds the RTMP server (if not already built)
# 2. Starts the server with SRT enabled and recording
# 3. Captures video from the integrated camera using FFmpeg
# 4. Streams the camera feed via SRT to the server
# 5. Records the stream locally to FLV format
#
# Usage:
#   .\scripts\test-srt-camera.ps1 -Duration 30
#   .\scripts\test-srt-camera.ps1
#
# Requirements:
#   - FFmpeg with camera support (dshow on Windows)
#   - Go 1.21+

param(
    [int]$Duration = 30  # Default 30 seconds
)

$ErrorActionPreference = "Stop"

# Configuration
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$ProjectRoot = Split-Path -Parent $ScriptDir
$ServerBinary = Join-Path $ProjectRoot "rtmp-server.exe"
$RecordingsDir = Join-Path $ProjectRoot "recordings"
$SrtPort = 10080
$RtmpPort = 1935
$StreamKey = "live/camera-test"

Write-Host "=== SRT Camera Ingest Test ===" -ForegroundColor Green
Write-Host "Duration: ${Duration}s"
Write-Host "Stream key: $StreamKey"
Write-Host ""

# Step 1: Build server if needed
if (-not (Test-Path $ServerBinary)) {
    Write-Host "Building server..." -ForegroundColor Yellow
    Push-Location $ProjectRoot
    go build -o rtmp-server.exe .\cmd\rtmp-server
    Pop-Location
    Write-Host "✓ Server built" -ForegroundColor Green
} else {
    Write-Host "✓ Server already built" -ForegroundColor Green
}

# Step 2: Clean up old processes
Write-Host "Cleaning up old processes..." -ForegroundColor Yellow
Get-Process "rtmp-server" -ErrorAction SilentlyContinue | Stop-Process -Force | Out-Null
Get-Process "ffmpeg" -ErrorAction SilentlyContinue | Stop-Process -Force | Out-Null
Start-Sleep -Seconds 1

# Step 3: Create recordings directory
New-Item -ItemType Directory -Path $RecordingsDir -Force | Out-Null

# Step 4: Start server in background job
Write-Host "Starting RTMP server..." -ForegroundColor Yellow
$ServerJob = Start-Job -ScriptBlock {
    param($Binary, $RtmpPort, $SrtPort, $RecordingsDir)
    & $Binary `
        -listen "localhost:$RtmpPort" `
        -srt-listen "localhost:$SrtPort" `
        -record-all true `
        -record-dir "$RecordingsDir" `
        -log-level info
} -ArgumentList $ServerBinary, $RtmpPort, $SrtPort, $RecordingsDir

Write-Host "✓ Server started (Job ID: $($ServerJob.Id))" -ForegroundColor Green
Start-Sleep -Seconds 2

# Step 5: Get list of available cameras
Write-Host "Detecting available cameras..." -ForegroundColor Yellow
$CameraDevices = ffmpeg -list_devices true -f dshow -i dummy 2>&1 | Select-String "video"
if ($CameraDevices) {
    Write-Host "Available cameras:" -ForegroundColor Yellow
    $CameraDevices | ForEach-Object { Write-Host "  $_" }
    Write-Host ""
}

# Step 6: Capture camera and stream via SRT
Write-Host "Starting camera capture and SRT stream..." -ForegroundColor Yellow
Write-Host "Camera device: (Built-in webcam)"
Write-Host "Streaming to: srt://localhost:$SrtPort`?streamid=publish:$StreamKey"
Write-Host ""

# List available video devices to use the first one
$VideoDevices = ffmpeg -list_devices true -f dshow -i dummy 2>&1 | Select-String '"video"' | Select-Object -First 1
$CameraName = "video=""Integrated Camera"""  # Default fallback

# Try dshow (Windows native)
Write-Host "FFmpeg is capturing from camera (this may take a moment)..." -ForegroundColor Yellow
$FfmpegJob = Start-Job -ScriptBlock {
    param($CameraName, $SrtPort, $StreamKey, $Duration)
    $proc = Start-Process -FilePath "ffmpeg" -ArgumentList `
        "-f dshow", `
        "-i `"$CameraName`"", `
        "-c:v libx264", `
        "-preset ultrafast", `
        "-tune zerolatency", `
        "-b:v 2500k", `
        "-c:a aac", `
        "-b:a 128k", `
        "-f mpegts", `
        "`"srt://localhost:$SrtPort`?streamid=publish:$StreamKey`"" `
        -Wait -NoNewWindow -PassThru
    exit $proc.ExitCode
} -ArgumentList $CameraName, $SrtPort, $StreamKey, $Duration

# Wait for FFmpeg to finish
$FfmpegJob | Wait-Job | Out-Null
$FfmpegResult = $FfmpegJob | Receive-Job
Write-Host "FFmpeg finished" -ForegroundColor Yellow
Start-Sleep -Seconds 2

# Step 7: Stop server
Write-Host "Stopping server..." -ForegroundColor Yellow
$ServerJob | Stop-Job -PassThru | Remove-Job
Write-Host "✓ Server stopped" -ForegroundColor Green

# Step 8: Check recordings
Write-Host ""
Write-Host "=== Test Complete ===" -ForegroundColor Green

$RecordedFiles = Get-ChildItem -Path $RecordingsDir -Filter "*.flv" -ErrorAction SilentlyContinue | Sort-Object -Property LastWriteTime -Descending
if ($RecordedFiles) {
    Write-Host "✓ Recording saved:" -ForegroundColor Green
    $RecordedFiles[0] | Format-Table -Property Name, Length, LastWriteTime | Out-String | Write-Host
    
    # Show file info
    $LatestRecording = $RecordedFiles[0].FullName
    Write-Host ""
    Write-Host "Recording info:" -ForegroundColor Yellow
    & ffprobe -show_format -show_streams $LatestRecording 2>&1 | Select-String "duration|codec_name|width|height" | Select-Object -First 10 | Write-Host
    
    Write-Host ""
    Write-Host "To play the recording:" -ForegroundColor Yellow
    Write-Host "ffplay `"$LatestRecording`""
} else {
    Write-Host "✗ No recordings found" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "✓ SRT camera test completed successfully!" -ForegroundColor Green

# ============================================================================
# TEST: camera-srt-h264
# GROUP: Camera Tests
#
# WHAT IS TESTED:
#   SRT ingest from a live system camera with H.264 encoding and AAC audio.
#   Recording is enabled, and the test verifies the resulting FLV file
#   contains valid H.264 video that can be decoded.
#
# EXPECTED RESULT:
#   - Server accepts SRT camera stream, recording .flv created, no panics
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "camera-srt-h264"
$Port = Get-UniquePort $TestName
$SrtPort = $Port + 200

# Allow skipping camera tests via environment variable
if ($env:SKIP_CAMERA_TESTS -eq "1") {
    Write-Host "SKIP: Camera tests disabled (SKIP_CAMERA_TESTS=1)" -ForegroundColor Yellow
    exit 2
}

$protocols = & ffmpeg -hide_banner -protocols 2>&1
if ($protocols -notmatch "srt") {
    Write-Host "SKIP: FFmpeg does not have SRT protocol support" -ForegroundColor Yellow
    exit 2
}

# Detect camera on Windows (dshow)
$devices = & ffmpeg -list_devices true -f dshow -i dummy 2>&1
if ($devices -notmatch "video") {
    Write-Host "SKIP: No camera device detected" -ForegroundColor Yellow
    exit 2
}

Setup $TestName

$recordDir = Join-Path $script:TmpDir "recordings"
New-Item -ItemType Directory -Path $recordDir -Force | Out-Null

if (-not (Start-TestServer -Port $Port -ExtraArgs @(
    "-log-level", "debug",
    "-srt-listen", "localhost:${SrtPort}",
    "-record-all", "true",
    "-record-dir", $recordDir
))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing camera feed via SRT H.264 (5s)..." -ForegroundColor Blue
$log = Join-Path $script:LogDir "$TestName-publish.log"
$proc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error",
    "-f", "dshow", "-framerate", "30", "-i", "video=0",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "64k",
    "-t", "5", "-f", "mpegts",
    "srt://localhost:${SrtPort}?streamid=publish:live/camera-h264&latency=200000"
) -RedirectStandardOutput $log -RedirectStandardError "$log.err" -NoNewWindow -PassThru -Wait

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Waiting for recording flush..." -ForegroundColor Blue
Start-Sleep -Seconds 3

Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

$recording = Get-ChildItem -Path $recordDir -Filter "*.flv" -Recurse | Select-Object -First 1
if (-not $recording) {
    Fail-Check "Recording file created" "No .flv file found in $recordDir"
} else {
    Pass-Check "Recording file created: $($recording.Name)"
    Assert-HasVideo -File $recording.FullName
    Assert-Decodable -File $recording.FullName
}

Teardown
$exitCode = Report-Result $TestName
exit $exitCode

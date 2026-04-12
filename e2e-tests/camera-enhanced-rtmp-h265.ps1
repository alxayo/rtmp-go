# ============================================================================
# TEST: camera-enhanced-rtmp-h265
# GROUP: Camera Tests
#
# WHAT IS TESTED:
#   Enhanced RTMP publish from a live camera using H.265 (HEVC) codec.
#   The server records the stream to an MP4 file. After publishing, the
#   recording is validated to contain HEVC video and be fully decodable.
#
# EXPECTED RESULT:
#   - Camera H.265 stream published via Enhanced RTMP
#   - Server records to MP4 file
#   - Recording contains HEVC video codec
#   - Recording is fully decodable
#   - No server panics
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "camera-enhanced-rtmp-h265"
$Port = Get-UniquePort $TestName

# Skip if camera tests disabled
if ($env:SKIP_CAMERA_TESTS -eq "1") {
    Write-Host "SKIP: Camera tests disabled (SKIP_CAMERA_TESTS=1)" -ForegroundColor Yellow
    exit 2
}

# Check H.265 encoder availability
$codecs = & ffmpeg -codecs 2>&1
if ($codecs -notmatch "libx265") {
    Write-Host "SKIP: libx265 encoder not available" -ForegroundColor Yellow
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

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-record-all", "true", "-record-dir", $recordDir))) { exit 1 }

$streamUrl = "rtmp://localhost:${Port}/live/camera-h265"

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing camera via Enhanced RTMP H.265 (5s)..." -ForegroundColor Blue
$pubLog = Join-Path $script:LogDir "$TestName-publish.log"
$pubProc = Start-Process -FilePath "ffmpeg" -ArgumentList @(
    "-hide_banner", "-loglevel", "error",
    "-f", "dshow", "-framerate", "30", "-i", "video=0:audio=0",
    "-t", "5",
    "-c:v", "libx265", "-preset", "ultrafast",
    "-c:a", "aac", "-b:a", "128k",
    "-f", "flv", $streamUrl
) -RedirectStandardOutput $pubLog -RedirectStandardError "$pubLog.err" -NoNewWindow -PassThru -Wait

Start-Sleep -Seconds 2

# Find recording (H.265 → MP4, fallback to FLV)
$recording = Get-ChildItem -Path $recordDir -Include "live_camera-h265_*.mp4","live_camera-h265_*.flv" -Recurse | Select-Object -First 1

if ($recording) {
    Assert-FileExists -File $recording.FullName -Label "Recording created"
    Assert-VideoCodec -File $recording.FullName -Expected "hevc"
    Assert-Decodable -File $recording.FullName
} else {
    Fail-Check "Recording created" "No recording file found in $recordDir"
}

Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode

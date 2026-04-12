# ============================================================================
# TEST: camera-rtmp-h264
# GROUP: Camera Tests
#
# WHAT IS TESTED:
#   RTMP publish from a live camera device using H.264 codec. The server
#   records the stream to an FLV file. After publishing, the recording
#   is validated with ffprobe to ensure the video codec is H.264, audio
#   is AAC, and the file is fully decodable.
#
# EXPECTED RESULT:
#   - Camera stream published successfully via RTMP
#   - Server records to FLV file in the recording directory
#   - Recording contains H.264 video and AAC audio
#   - No server panics or errors
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "camera-rtmp-h264"
$Port = Get-UniquePort $TestName

# Skip if camera tests disabled
if ($env:SKIP_CAMERA_TESTS -eq "1") {
    Write-Host "SKIP: Camera tests disabled (set SKIP_CAMERA_TESTS=0 to enable)" -ForegroundColor Yellow
    exit 2
}

# Detect camera — Windows uses dshow, macOS uses avfoundation
$CameraArgs = @()
if ($IsMacOS) {
    $devices = & ffmpeg -f avfoundation -list_devices true -i "" 2>&1
    if ($devices -match "\[0\]") {
        $CameraArgs = @("-f", "avfoundation", "-framerate", "30", "-i", "0:0")
    }
} else {
    # Windows: dshow
    $devices = & ffmpeg -list_devices true -f dshow -i dummy 2>&1
    if ($devices -match "video") {
        $CameraArgs = @("-f", "dshow", "-framerate", "30", "-i", "video=0")
    }
}

if ($CameraArgs.Count -eq 0) {
    Write-Host "SKIP: No camera device detected" -ForegroundColor Yellow
    exit 2
}

Setup $TestName

$recordDir = Join-Path $script:TmpDir "recordings"
New-Item -ItemType Directory -Path $recordDir -Force | Out-Null

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-record-all", "true", "-record-dir", $recordDir))) { exit 1 }

$streamUrl = "rtmp://localhost:${Port}/live/camera-h264"

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing camera stream via RTMP H.264 (5s)..." -ForegroundColor Blue
$publishLog = Join-Path $script:LogDir "$TestName-publish.log"
$ffmpegArgs = $CameraArgs + @(
    "-t", "5",
    "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
    "-c:a", "aac", "-b:a", "128k",
    "-f", "flv", $streamUrl
)
$proc = Start-Process -FilePath "ffmpeg" -ArgumentList (@("-hide_banner", "-loglevel", "error") + $ffmpegArgs) `
    -RedirectStandardOutput $publishLog -RedirectStandardError "$publishLog.err" `
    -NoNewWindow -PassThru -Wait

Start-Sleep -Seconds 2  # Wait for recording flush

$recording = Get-ChildItem -Path $recordDir -Filter "*.flv" -Recurse | Select-Object -First 1
if (-not $recording) {
    Fail-Check "FLV recording created" "No .flv file found in $recordDir"
} else {
    Pass-Check "FLV recording created: $($recording.Name)"
    Assert-VideoCodec -File $recording.FullName -Expected "h264"
    Assert-AudioCodec -File $recording.FullName -Expected "aac"
    Assert-Decodable -File $recording.FullName
}

Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode

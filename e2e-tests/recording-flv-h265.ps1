# ============================================================================
# TEST: recording-flv-h265
# GROUP: FLV Recording
#
# WHAT IS TESTED:
#   FLV recording preserves H.265/HEVC codec from Enhanced RTMP publish.
#
# EXPECTED RESULT:
#   - Recording .flv contains HEVC video, decodable
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "recording-flv-h265"
$Port = Get-UniquePort $TestName

$encoders = & ffmpeg -hide_banner -encoders 2>&1
if ($encoders -notmatch "libx265") {
    Write-Host "SKIP: FFmpeg does not have libx265 encoder" -ForegroundColor Yellow
    exit 2
}

Setup $TestName

$recordDir = Join-Path $script:TmpDir "recordings"
New-Item -ItemType Directory -Path $recordDir -Force | Out-Null

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-record-all", "true", "-record-dir", $recordDir))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing H.265 via Enhanced RTMP (5s)..." -ForegroundColor Blue
Publish-H265TestPattern -Url "rtmp://localhost:${Port}/live/h265-rec" -Duration 5
Start-Sleep -Seconds 3

$recording = Get-ChildItem -Path $recordDir -Filter "*.flv" -Recurse | Select-Object -First 1
if (-not $recording) {
    Fail-Check "H.265 recording file created" "No .flv file found"
} else {
    Pass-Check "H.265 recording file created: $($recording.Name)"
    Assert-VideoCodec -File $recording.FullName -Expected "hevc"
    Assert-Decodable -File $recording.FullName
}

Teardown
$exitCode = Report-Result $TestName
exit $exitCode

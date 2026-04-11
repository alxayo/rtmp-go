# ============================================================================
# TEST: recording-flv-audio-video
# GROUP: FLV Recording
#
# WHAT IS TESTED:
#   FLV recording has both audio and video streams interleaved correctly.
#
# EXPECTED RESULT:
#   - Recording has 2 streams (1 video H.264, 1 audio AAC)
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "recording-flv-audio-video"
$Port = Get-UniquePort $TestName

Setup $TestName

$recordDir = Join-Path $script:TmpDir "recordings"
New-Item -ItemType Directory -Path $recordDir -Force | Out-Null

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-record-all", "true", "-record-dir", $recordDir))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing H.264+AAC (5s)..." -ForegroundColor Blue
Publish-TestPattern -Url "rtmp://localhost:${Port}/live/av-test" -Duration 5
Start-Sleep -Seconds 3

$recording = Get-ChildItem -Path $recordDir -Filter "*.flv" -Recurse | Select-Object -First 1
if (-not $recording) {
    Fail-Check "Recording file created" "No .flv file found"
} else {
    Pass-Check "Recording file created"
    $count = & ffprobe -v error -show_entries format=nb_streams -of csv=p=0 $recording.FullName 2>&1
    if ([int]$count -ge 2) { Pass-Check "Recording has $count streams (audio + video)" }
    else { Fail-Check "Recording has audio + video" "only $count stream(s)" }
    Assert-VideoCodec -File $recording.FullName -Expected "h264"
    Assert-AudioCodec -File $recording.FullName -Expected "aac"
    Assert-Duration -File $recording.FullName -Min 2.0 -Max 10.0
}

Teardown
$exitCode = Report-Result $TestName
exit $exitCode

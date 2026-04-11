# ============================================================================
# TEST: recording-flv-h264
# GROUP: FLV Recording
#
# WHAT IS TESTED:
#   Server-side FLV recording with -record-all. Verifies recording file
#   contains H.264+AAC and is fully decodable.
#
# EXPECTED RESULT:
#   - .flv file exists, contains H.264 video + AAC audio, decodable
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "recording-flv-h264"
$Port = Get-UniquePort $TestName

Setup $TestName

$recordDir = Join-Path $script:TmpDir "recordings"
New-Item -ItemType Directory -Path $recordDir -Force | Out-Null

if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-record-all", "true", "-record-dir", $recordDir))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing H.264+AAC test pattern (5s)..." -ForegroundColor Blue
Publish-TestPattern -Url "rtmp://localhost:${Port}/live/rec-test" -Duration 5
Start-Sleep -Seconds 3

$recording = Get-ChildItem -Path $recordDir -Filter "*.flv" -Recurse | Select-Object -First 1
if (-not $recording) {
    Fail-Check "Recording file created" "No .flv file found"
} else {
    Pass-Check "Recording file created: $($recording.Name)"
    Assert-VideoCodec -File $recording.FullName -Expected "h264"
    Assert-AudioCodec -File $recording.FullName -Expected "aac"
    Assert-Duration -File $recording.FullName -Min 2.0 -Max 10.0
    Assert-Decodable -File $recording.FullName
}

Teardown
$exitCode = Report-Result $TestName
exit $exitCode

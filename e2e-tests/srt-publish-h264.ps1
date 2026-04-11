# ============================================================================
# TEST: srt-publish-h264
# GROUP: SRT Ingest
#
# WHAT IS TESTED:
#   SRT ingest with H.264+AAC via MPEG-TS. Verifies the SRT listener
#   accepts the connection.
#
# EXPECTED RESULT:
#   - SRT connection accepted, no panics
# ============================================================================
. "$PSScriptRoot\_lib.ps1"

$TestName = "srt-publish-h264"
$Port = Get-UniquePort $TestName
$SrtPort = $Port + 200

$protocols = & ffmpeg -hide_banner -protocols 2>&1
if ($protocols -notmatch "srt") {
    Write-Host "SKIP: FFmpeg does not have SRT protocol support" -ForegroundColor Yellow
    exit 2
}

Setup $TestName
if (-not (Start-TestServer -Port $Port -ExtraArgs @("-log-level", "debug", "-srt-listen", "localhost:${SrtPort}"))) { exit 1 }

Write-Host "$(Get-Date -Format 'HH:mm:ss') -> Publishing H.264+AAC via SRT (5s)..." -ForegroundColor Blue
Publish-SrtH264 -Url "srt://localhost:${SrtPort}?streamid=publish:live/srt-h264&latency=200000" -Duration 5
Start-Sleep -Seconds 2

Assert-LogNotContains -File $script:ServerLog -Pattern "panic|FATAL" -Label "No server panics"

Teardown
$exitCode = Report-Result $TestName
exit $exitCode
